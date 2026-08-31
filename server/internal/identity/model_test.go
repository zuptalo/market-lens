package identity

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	ownerID  = "40000000-0000-4000-8000-000000000001"
	memberID = "40000000-0000-4000-8000-000000000002"
)

func TestNormalizeEmailPreservesLocalPartAndBuildsComparisonKey(t *testing.T) {
	delivery, normalized, err := NormalizeEmail("  Owner.Local@EXAMPLE.COM  ")
	if err != nil {
		t.Fatal(err)
	}
	if delivery != "Owner.Local@example.com" {
		t.Fatalf("delivery email = %q", delivery)
	}
	if normalized != "owner.local@example.com" {
		t.Fatalf("normalized email = %q", normalized)
	}

	for _, value := range []string{
		"Owner <owner@example.com>", "owner@example.com\nBcc: attacker@example.com",
		"missing-at.example.com", "@example.com", "owner@", string(bytes.Repeat([]byte{'a'}, 321)),
	} {
		if _, _, err := NormalizeEmail(value); err == nil {
			t.Fatalf("unsafe email %q unexpectedly succeeded", value)
		}
	}
}

func TestUserValidationEnforcesVerifiedLifecycleAndPermanentOwner(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	valid := User{
		ID: ownerID, Email: "Owner@example.com", NormalizedEmail: "owner@example.com",
		DisplayName: "Market Owner", Role: RoleOwner, Status: StatusActive,
		EmailVerifiedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid owner rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*User)
	}{
		{name: "unverified active", mutate: func(user *User) { user.EmailVerifiedAt = nil }},
		{name: "deactivated owner", mutate: func(user *User) { user.Status = StatusDeactivated; user.DeactivatedAt = &now }},
		{name: "untrimmed display name", mutate: func(user *User) { user.DisplayName = " Owner " }},
		{name: "incorrect normalized email", mutate: func(user *User) { user.NormalizedEmail = "Owner@example.com" }},
		{name: "invalid ID", mutate: func(user *User) { user.ID = "not-a-uuid" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := valid
			tt.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid user unexpectedly passed validation")
			}
		})
	}
}

func TestBootstrapStateClosesExactlyOnce(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	state := BootstrapState{}
	if !state.Open() {
		t.Fatal("fresh bootstrap state is closed")
	}
	if err := state.Close(ownerID, now); err != nil {
		t.Fatal(err)
	}
	if state.Open() || state.OwnerUserID != ownerID || state.ClosedAt == nil || !state.ClosedAt.Equal(now) {
		t.Fatalf("closed bootstrap state = %#v", state)
	}
	if err := state.Close(memberID, now.Add(time.Minute)); err == nil {
		t.Fatal("closed bootstrap state reopened for another owner")
	}
}

func TestCapabilityValidationAndSingleUseTransitions(t *testing.T) {
	created := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	capability := Capability{
		ID: "40000000-0000-4000-8000-000000000003", Kind: CapabilityOwnerSetup,
		TokenDigest: bytes.Repeat([]byte{0x41}, 32), CreatedAt: created, ExpiresAt: created.Add(15 * time.Minute),
	}
	if err := capability.Validate(); err != nil {
		t.Fatalf("valid setup capability rejected: %v", err)
	}
	if !capability.UsableAt(created.Add(14*time.Minute)) || capability.UsableAt(capability.ExpiresAt) {
		t.Fatal("capability expiry boundary is incorrect")
	}
	consumedAt := created.Add(5 * time.Minute)
	if err := capability.Consume(consumedAt); err != nil {
		t.Fatal(err)
	}
	if capability.UsableAt(consumedAt) || capability.ConsumedAt == nil {
		t.Fatal("consumed capability remained usable")
	}
	if err := capability.Consume(consumedAt); err == nil {
		t.Fatal("capability replay unexpectedly succeeded")
	}

	recovery := Capability{
		ID: "40000000-0000-4000-8000-000000000004", Kind: CapabilityOwnerRecovery,
		TokenDigest: bytes.Repeat([]byte{0x42}, 32), CreatedAt: created, ExpiresAt: created.Add(30 * time.Minute),
	}
	if err := recovery.Validate(); err == nil {
		t.Fatal("owner recovery capability without owner user ID unexpectedly validated")
	}
}

func TestOwnerCredentialAuditAndDeliveryModelsRejectUnsafeState(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	credential := OwnerCredential{
		UserID: ownerID, PasswordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaA",
		CreatedAt: now, ChangedAt: now,
	}
	if err := credential.Validate(); err != nil {
		t.Fatalf("valid owner credential rejected: %v", err)
	}
	credential.PasswordHash = "plaintext-password"
	if err := credential.Validate(); err == nil {
		t.Fatal("plaintext owner credential unexpectedly validated")
	}

	audit := SecurityAuditEvent{
		OccurredAt: now, EventType: "owner.setup.v1", SubjectUserID: ownerID,
		Outcome: AuditSucceeded, Metadata: json.RawMessage(`{"source":"host"}`),
	}
	if err := audit.Validate(); err != nil {
		t.Fatalf("valid audit event rejected: %v", err)
	}
	audit.Metadata = json.RawMessage(`[]`)
	if err := audit.Validate(); err == nil {
		t.Fatal("non-object audit metadata unexpectedly validated")
	}

	delivery := EmailDelivery{
		ID: "40000000-0000-4000-8000-000000000005", Kind: DeliveryOwnerRecovery,
		RecipientEmail: "Owner@example.com", SubjectUserID: ownerID, State: DeliveryPending, CreatedAt: now,
	}
	if err := delivery.Validate(); err != nil {
		t.Fatalf("valid pending delivery rejected: %v", err)
	}
	if err := delivery.MarkSent(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if delivery.State != DeliverySent || delivery.SentAt == nil || delivery.ErrorCode != "" {
		t.Fatalf("sent delivery = %#v", delivery)
	}

	deliveryType := reflect.TypeOf(delivery)
	for index := range deliveryType.NumField() {
		name := deliveryType.Field(index).Name
		for _, forbidden := range []string{"Token", "Capability", "CodeDigest", "Body"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("delivery persistence model exposes secret-bearing field %s", name)
			}
		}
	}
}
