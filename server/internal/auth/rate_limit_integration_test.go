package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"market-lens/server/internal/auth"
	"market-lens/server/internal/db"
	"market-lens/server/internal/testdb"
)

func TestMemberCodeDeliveryAllowsOnePerMinuteAndFivePerHour(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	_, secrets, _ := provisionOwner(t, pool)
	repository := auth.NewRepository(pool)
	account := secrets.Digest(auth.PurposeMemberCode, "member@example.com")
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)

	first, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, account, start, auth.MemberCodeDeliveryLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Allowed {
		t.Fatalf("first delivery = %#v, want allowed", first)
	}
	// A second request inside the same minute is refused with a coarse hint.
	second, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, account, start.Add(30*time.Second), auth.MemberCodeDeliveryLimits)
	if err != nil {
		t.Fatal(err)
	}
	if second.Allowed {
		t.Fatal("a second delivery inside one minute was allowed")
	}
	if second.RetryAfter%auth.RateRetryGranularity != 0 || second.RetryAfter <= 0 {
		t.Fatalf("retry hint = %v, want a positive coarse multiple of %v", second.RetryAfter, auth.RateRetryGranularity)
	}

	// Four more spaced requests fill the hourly ceiling of five.
	now := start
	for delivery := 2; delivery <= 5; delivery++ {
		now = now.Add(2 * time.Minute)
		decision, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, account, now, auth.MemberCodeDeliveryLimits)
		if err != nil {
			t.Fatal(err)
		}
		if !decision.Allowed {
			t.Fatalf("delivery %d = %#v, want allowed", delivery, decision)
		}
	}
	now = now.Add(2 * time.Minute)
	sixth, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, account, now, auth.MemberCodeDeliveryLimits)
	if err != nil {
		t.Fatal(err)
	}
	if sixth.Allowed {
		t.Fatal("a sixth delivery inside one hour was allowed")
	}

	// The window slides rather than resetting on a fixed boundary.
	slid, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, account, start.Add(time.Hour+time.Second), auth.MemberCodeDeliveryLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !slid.Allowed {
		t.Fatalf("delivery after the oldest event aged out = %#v, want allowed", slid)
	}
}

func TestAccountAndOriginRateBucketsThrottleIndependently(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	_, secrets, _ := provisionOwner(t, pool)
	repository := auth.NewRepository(pool)
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.7")

	// Exhausting one account's delivery bucket must not throttle a different account.
	first := secrets.Digest(auth.PurposeMemberCode, "first@example.com")
	second := secrets.Digest(auth.PurposeMemberCode, "second@example.com")
	if decision, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, first, start, auth.MemberCodeDeliveryLimits); err != nil || !decision.Allowed {
		t.Fatalf("first account delivery = %#v err=%v", decision, err)
	}
	if decision, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, first, start, auth.MemberCodeDeliveryLimits); err != nil || decision.Allowed {
		t.Fatalf("repeat first account delivery = %#v err=%v, want refused", decision, err)
	}
	if decision, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, second, start, auth.MemberCodeDeliveryLimits); err != nil || !decision.Allowed {
		t.Fatalf("second account delivery = %#v err=%v, want allowed", decision, err)
	}

	// Spraying many accounts from one origin is bounded by the origin bucket alone.
	allowed := 0
	for attempt := range 40 {
		decision, err := repository.AllowRate(context.Background(), auth.RateOriginCodeRequest, origin,
			start.Add(time.Duration(attempt)*time.Second), auth.OriginCodeRequestLimits)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Allowed {
			allowed++
		}
	}
	if allowed != auth.OriginCodeRequestLimits[0].Limit {
		t.Fatalf("origin requests allowed in one minute = %d, want %d", allowed, auth.OriginCodeRequestLimits[0].Limit)
	}
	// A different origin is unaffected by the first origin's exhaustion.
	other := secrets.Digest(auth.PurposeOrigin, "198.51.100.9")
	if decision, err := repository.AllowRate(context.Background(), auth.RateOriginCodeRequest, other, start.Add(time.Second), auth.OriginCodeRequestLimits); err != nil || !decision.Allowed {
		t.Fatalf("second origin request = %#v err=%v, want allowed", decision, err)
	}
	// Verification is a separate bucket from request, so exhausting one leaves the other usable.
	if decision, err := repository.AllowRate(context.Background(), auth.RateOriginCodeVerify, origin, start.Add(time.Second), auth.OriginCodeVerifyLimits); err != nil || !decision.Allowed {
		t.Fatalf("origin verify after request exhaustion = %#v err=%v, want allowed", decision, err)
	}
}

func TestConcurrentRateDecisionsNeverExceedTheCeiling(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	_, secrets, _ := provisionOwner(t, pool)
	repository := auth.NewRepository(pool)
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	origin := secrets.Digest(auth.PurposeOrigin, "203.0.113.11")

	const workers = 30
	var group sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	var failure error
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			decision, err := repository.AllowRate(context.Background(), auth.RateOriginCodeRequest, origin, start, auth.OriginCodeRequestLimits)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failure = err
				return
			}
			if decision.Allowed {
				allowed++
			}
		}()
	}
	group.Wait()
	if failure != nil {
		t.Fatal(failure)
	}
	if allowed != auth.OriginCodeRequestLimits[0].Limit {
		t.Fatalf("concurrent allowances = %d, want exactly %d", allowed, auth.OriginCodeRequestLimits[0].Limit)
	}
	var recorded int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM auth_rate_events WHERE bucket_kind='origin_code_request'`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != auth.OriginCodeRequestLimits[0].Limit {
		t.Fatalf("recorded rate events = %d, want %d (refused attempts must not consume budget)",
			recorded, auth.OriginCodeRequestLimits[0].Limit)
	}
}

func TestRateEventPruningRemovesOnlyElapsedWindows(t *testing.T) {
	pool := testdb.Open(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	_, secrets, _ := provisionOwner(t, pool)
	repository := auth.NewRepository(pool)
	start := time.Date(2026, time.August, 30, 10, 0, 0, 0, time.UTC)
	account := secrets.Digest(auth.PurposeMemberCode, "member@example.com")

	if _, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, account, start, auth.MemberCodeDeliveryLimits); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AllowRate(context.Background(), auth.RateMemberCodeDelivery, account, start.Add(90*time.Minute), auth.MemberCodeDeliveryLimits); err != nil {
		t.Fatal(err)
	}
	removed, err := repository.PruneRateEvents(context.Background(), start.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("pruned rate events = %d, want 1", removed)
	}
	var remaining int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM auth_rate_events`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining rate events = %d, want the in-window event retained", remaining)
	}
}

func TestCoarseRetryHintsDoNotRevealBucketPosition(t *testing.T) {
	for _, remaining := range []time.Duration{time.Nanosecond, time.Second, 59 * time.Second, time.Minute} {
		if hint := auth.CoarsenRetryAfter(remaining); hint != time.Minute {
			t.Fatalf("CoarsenRetryAfter(%v) = %v, want %v", remaining, hint, time.Minute)
		}
	}
	if hint := auth.CoarsenRetryAfter(61 * time.Second); hint != 2*time.Minute {
		t.Fatalf("CoarsenRetryAfter(61s) = %v, want 2m", hint)
	}
	if hint := auth.CoarsenRetryAfter(0); hint != time.Minute {
		t.Fatalf("CoarsenRetryAfter(0) = %v, want 1m", hint)
	}
}
