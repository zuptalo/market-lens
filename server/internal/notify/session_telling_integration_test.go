package notify_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"market-lens/server/internal/notify"
)

// seedChangedSignals writes instruments whose latest stored view differs from the one before it,
// which is the shape the survey looks for: two sessions of signals per instrument, the second
// disagreeing with the first.
//
// The tickers are fixture-only on purpose. The Nordic universe is reference data carried by
// migration, so DANSKE and NHY already exist in a clean database — seeding a second instrument
// under a real ticker makes every count in this file double, which is exactly what it did.
func (f *fixture) seedChangedSignals(tickers ...string) {
	f.t.Helper()
	f.exec(`INSERT INTO exchanges (id, mic, name, country, currency, timezone)
		VALUES (gen_random_uuid(), 'XSIG', 'Signal Exchange', 'SE', 'SEK', 'Europe/Stockholm')
		ON CONFLICT (mic) DO NOTHING`)
	f.exec(`INSERT INTO research_universes (id, code, name, description)
		VALUES ('70000000-0029-4000-8000-0000000000b1', 'signal-fixture', 'Signal fixture', 'fixture')
		ON CONFLICT (code) DO NOTHING`)
	// The strategy itself is reference data carried by migration, so the fixture borrows the real
	// one rather than inventing a second momentum_trend the product would not recognise.
	f.exec(`INSERT INTO strategy_runs
		(id, strategy_id, kind, status, universe_id, started_at, finished_at, app_version)
		SELECT '70000000-0029-4000-8000-0000000000c1', s.id, 'strategy', 'succeeded',
		       '70000000-0029-4000-8000-0000000000b1', now(), now(), 'test'
		FROM strategies s WHERE s.name = 'momentum_trend' ORDER BY s.version DESC LIMIT 1
		ON CONFLICT DO NOTHING`)
	for index, ticker := range tickers {
		label := strconv.Itoa(index)
		f.exec(`INSERT INTO instruments
			(id, exchange_id, isin, ticker, name, currency, country, instrument_type, active,
			 purchasability_status)
			SELECT gen_random_uuid(), e.id, 'SE000000003' || $2, $1,
			       'Signal Fixture', 'SEK', 'SE', 'common_stock', true, 'unverified'
			FROM exchanges e WHERE e.mic = 'XSIG'`, ticker, label)
		// Yesterday it said HOLD; today it says BUY. One change, per instrument, on one session.
		for _, view := range []struct {
			offset int
			action string
		}{{1, "HOLD"}, {0, "BUY"}} {
			f.exec(`INSERT INTO signals
				(instrument_id, session_date, strategy_id, score, action, confidence,
				 contributions, divisor, computed_at, run_id)
				SELECT i.id, current_date - $2::int, r.strategy_id, 0, $3, 0.5,
				       '[]'::jsonb, 1, now(), r.id
				FROM instruments i, strategy_runs r
				WHERE i.ticker = $1 AND r.id = '70000000-0029-4000-8000-0000000000c1'`,
				ticker, view.offset, view.action)
		}
	}
}

// The defect this feature exists for: eleven instruments changed view on one session and eleven
// byte-identical pushes arrived on one phone. Feature 027 keeps instrument names out of a push
// payload — rightly, it rests on a third party's server — so the field that told them apart is
// stripped and what is left repeats.
func TestASessionOfChangesIsOneTelling(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindSignalChange, notify.ChannelEmail)
	f.ask(memberID, notify.KindSignalChange, notify.ChannelWebPush)
	f.subscribe(memberID, "member phone")
	f.seedChangedSignals("SIGA", "SIGB", "SIGC")

	if err := f.service().Survey(f.ctx); err != nil {
		t.Fatalf("survey: %v", err)
	}

	for _, channel := range []string{"email", "web_push"} {
		var raised, count int
		if err := f.pool.QueryRow(f.ctx,
			`SELECT count(*), COALESCE(max(count), 0) FROM notifications
			 WHERE user_id = $1 AND kind = 'signal_change' AND channel = $2`,
			memberID.String(), channel).Scan(&raised, &count); err != nil {
			t.Fatal(err)
		}
		if raised != 1 {
			t.Errorf("%d %s tellings for three changed instruments, want one", raised, channel)
		}
		if count != 3 {
			t.Errorf("the %s telling says %d changed, want 3", channel, count)
		}
	}

	f.deliver()

	// The email names every one of them, with both views, and says once that it is not advice.
	message := f.mail.last()
	text := strings.ToLower(prose(message.Text))
	for _, expected := range []string{"siga", "sigb", "sigc", "hold", "buy", "momentum_trend"} {
		if !strings.Contains(text, expected) {
			t.Errorf("the message does not name %q:\n%s", expected, message.Text)
		}
	}
	if !strings.Contains(text, "not advice") {
		t.Errorf("the message does not say it is not advice:\n%s", message.Text)
	}
	if strings.Count(text, "not advice") != 1 {
		t.Errorf("the caveat appears %d times, want once:\n%s", strings.Count(text, "not advice"), message.Text)
	}
	if f.mail.count() != 1 {
		t.Errorf("%d emails were sent for one session, want one", f.mail.count())
	}
	if f.pusher.count() != 1 {
		t.Errorf("%d pushes were sent for one session, want one", f.pusher.count())
	}
}

// A quiet session must not become vaguer than the form it replaces: one change still names its
// instrument and both views (US2).
func TestASingleChangeStillNamesItsInstrument(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindSignalChange, notify.ChannelEmail)
	f.seedChangedSignals("SIGA")

	if err := f.service().Survey(f.ctx); err != nil {
		t.Fatalf("survey: %v", err)
	}
	f.deliver()

	text := strings.ToLower(prose(f.mail.last().Text))
	for _, expected := range []string{"siga", "hold", "buy", "momentum_trend"} {
		if !strings.Contains(text, expected) {
			t.Errorf("the message does not state %q:\n%s", expected, f.mail.last().Text)
		}
	}
}

// Running the pass twice over one session says nothing twice (US3). A restart, a retry or a second
// invocation must not repeat what has already been said.
func TestASessionAlreadyToldAboutIsNotToldAgain(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindSignalChange, notify.ChannelEmail)
	f.seedChangedSignals("SIGA", "SIGB")

	if err := f.service().Survey(f.ctx); err != nil {
		t.Fatalf("first survey: %v", err)
	}
	if err := f.service().Survey(f.ctx); err != nil {
		t.Fatalf("second survey: %v", err)
	}
	if raised := f.count(`SELECT count(*) FROM notifications WHERE kind = 'signal_change'`); raised != 1 {
		t.Errorf("%d tellings after two passes over one session, want one", raised)
	}
}

// Consent is not retroactive, and idempotency must not turn into a backlog (US4). Somebody who
// enables the kind after a session was raised hears about the next one, not that one.
func TestConsentAfterASessionHearsNothingAboutIt(t *testing.T) {
	f := newFixture(t)
	f.seedChangedSignals("SIGA")

	if err := f.service().Survey(f.ctx); err != nil {
		t.Fatalf("survey before consent: %v", err)
	}
	f.ask(memberID, notify.KindSignalChange, notify.ChannelEmail)
	if err := f.service().Survey(f.ctx); err != nil {
		t.Fatalf("survey after consent: %v", err)
	}
	if raised := f.count(`SELECT count(*) FROM notifications WHERE kind = 'signal_change'`); raised != 0 {
		t.Errorf("%d tellings reached somebody who consented afterwards, want none", raised)
	}
}

// A collapsed telling must not be a way around the payload rule. The email may list eleven
// instruments; the push that accompanies it may still name none of them (FR-008).
func TestACollapsedPushStillNamesNoInstrument(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindSignalChange, notify.ChannelWebPush)
	f.subscribe(memberID, "member phone")
	f.seedChangedSignals("SIGA", "SIGB", "SIGC")

	if err := f.service().Survey(f.ctx); err != nil {
		t.Fatalf("survey: %v", err)
	}
	f.deliver()

	f.pusher.mu.Lock()
	payloads := append([][]byte(nil), f.pusher.payloads...)
	f.pusher.mu.Unlock()
	if len(payloads) != 1 {
		t.Fatalf("%d pushes for one session, want one", len(payloads))
	}
	text := strings.ToLower(string(payloads[0]))
	for _, word := range []string{"siga", "sigb", "sigc", "momentum", "hold", "buy"} {
		if strings.Contains(text, word) {
			t.Errorf("the push names %q: %s", word, payloads[0])
		}
	}
	// And it says how many, so one message is not read as one change.
	if !strings.Contains(text, "3 views changed") {
		t.Errorf("the push does not say how many changed: %s", payloads[0])
	}
}

// The schema is the guard, not the template — and a list is not a way around it (FR-007).
func TestAForbiddenFieldInsideTheListIsRefused(t *testing.T) {
	f := newFixture(t)
	f.ask(memberID, notify.KindSignalChange, notify.ChannelEmail)

	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = notify.RaiseIn(f.ctx, tx, notify.Raise{
		Kind: notify.KindSignalChange, SubjectKey: "signals:2026-09-19", Count: 2,
		Detail: map[string]string{"strategy": "momentum_trend"},
		Items: []map[string]string{
			{"ticker": "SIGA", "from": "HOLD", "to": "BUY", "strategy": "momentum_trend"},
			{"ticker": "SIGB", "from": "HOLD", "to": "BUY", "strategy": "momentum_trend",
				"value": "12 400.00"},
		},
	}, time.Now().UTC())
	_ = tx.Rollback(f.ctx)
	if err == nil {
		t.Error("a list item carrying a valuation was accepted")
	}
}
