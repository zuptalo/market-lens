package events_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"market-lens/server/internal/db"
	clientevents "market-lens/server/internal/events"
	"market-lens/server/internal/testdb"
)

func TestClientEventAuthorizationMigrationEnforcesExplicitScopesAndReplayIndexes(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var subjectType string
	if err := pool.QueryRow(ctx, `SELECT data_type FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='client_events' AND column_name='subject_user_id'`).Scan(&subjectType); err != nil {
		t.Fatal(err)
	}
	if subjectType != "uuid" {
		t.Fatalf("client event subject type = %q", subjectType)
	}
	for _, index := range []string{
		"client_events_shared_replay_idx", "client_events_user_replay_idx", "client_events_owner_replay_idx",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, index).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("replay index %s is absent", index)
		}
	}

	userID := "10000000-0000-4000-8000-000000000001"
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO users
		(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
		VALUES ($1,'member@example.test','member@example.test','Member','member','active',$2,$2,$2)`, userID, now); err != nil {
		t.Fatal(err)
	}

	valid := []struct {
		eventType, scope string
		subject          any
	}{
		{eventType: "daily_bar.changed.v1", scope: "shared"},
		{eventType: "member.locked.v1", scope: "owner"},
		{eventType: "session.revoked.v1", scope: "user", subject: userID},
	}
	for index, event := range valid {
		if _, err := pool.Exec(ctx, `INSERT INTO client_events
			(event_type,version,scope,subject_user_id,entity_type,entity_id,payload,occurred_at)
			VALUES ($1,1,$2,$3,'fixture',$4,'{}'::jsonb,$5)`,
			event.eventType, event.scope, event.subject, event.eventType, now.Add(time.Duration(index)*time.Second)); err != nil {
			t.Fatalf("valid %s event rejected: %v", event.scope, err)
		}
	}

	invalid := []struct {
		scope   string
		subject any
	}{
		{scope: "shared", subject: userID},
		{scope: "owner", subject: userID},
		{scope: "user"},
		{scope: "private", subject: userID},
	}
	for _, event := range invalid {
		if _, err := pool.Exec(ctx, `INSERT INTO client_events
			(event_type,version,scope,subject_user_id,entity_type,entity_id,payload,occurred_at)
			VALUES ('session.revoked.v1',1,$1,$2,'fixture','invalid','{}'::jsonb,$3)`,
			event.scope, event.subject, now); err == nil {
			t.Fatalf("invalid scope/subject combination was accepted: scope=%s subject=%v", event.scope, event.subject)
		}
	}
}

func TestAuthorizedEventReplayReturnsSharedAndOnlyPermittedAccountScopes(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	ownerID := "10000000-0000-4000-8000-000000000001"
	memberOneID := "10000000-0000-4000-8000-000000000002"
	memberTwoID := "10000000-0000-4000-8000-000000000003"
	for _, user := range []struct {
		id, email, role string
	}{
		{id: ownerID, email: "owner@example.test", role: "owner"},
		{id: memberOneID, email: "one@example.test", role: "member"},
		{id: memberTwoID, email: "two@example.test", role: "member"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO users
			(id,email,normalized_email,display_name,role,status,email_verified_at,created_at,updated_at)
			VALUES ($1,$2,$2,$2,$3,'active',$4,$4,$4)`, user.id, user.email, user.role, now); err != nil {
			t.Fatal(err)
		}
	}
	type fixture struct {
		typeName, scope string
		subject         any
	}
	fixtures := []fixture{
		{typeName: "daily_bar.changed.v1", scope: "shared"},
		{typeName: "member.locked.v1", scope: "owner"},
		{typeName: "session.created.v1", scope: "user", subject: ownerID},
		{typeName: "session.revoked.v1", scope: "user", subject: memberOneID},
		{typeName: "session.revoked.v1", scope: "user", subject: memberTwoID},
	}
	for index, event := range fixtures {
		if _, err := pool.Exec(ctx, `INSERT INTO client_events
			(event_type,version,scope,subject_user_id,entity_type,entity_id,payload,occurred_at)
			VALUES ($1,1,$2,$3,'fixture',$4,'{}'::jsonb,$5)`, event.typeName, event.scope, event.subject,
			event.typeName, now.Add(time.Duration(index)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	repository := clientevents.NewRepository(pool)
	tests := []struct {
		name     string
		audience clientevents.Audience
		after    int64
		want     []string
	}{
		{name: "member one", audience: clientevents.Audience{UserID: memberOneID, Role: "member"}, want: []string{"daily_bar.changed.v1", "session.revoked.v1"}},
		{name: "member two after shared", audience: clientevents.Audience{UserID: memberTwoID, Role: "member"}, after: 1, want: []string{"session.revoked.v1"}},
		{name: "owner", audience: clientevents.Audience{UserID: ownerID, Role: "owner"}, want: []string{"daily_bar.changed.v1", "member.locked.v1", "session.created.v1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events, err := repository.ListAuthorized(ctx, test.audience, test.after, 20)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(events))
			for _, event := range events {
				got = append(got, event.Type)
				if event.SubjectUserID != "" {
					if event.Scope != "user" || event.SubjectUserID != test.audience.UserID {
						t.Fatalf("unauthorized event = %#v", event)
					}
				}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("event types = %#v, want %#v", got, test.want)
			}
		})
	}

	if _, err := repository.ListAuthorized(ctx, clientevents.Audience{}, 0, 20); err == nil {
		t.Fatal("anonymous audience was accepted for event replay")
	}
}
