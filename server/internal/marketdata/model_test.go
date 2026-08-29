package marketdata

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecimalPreservesExactCanonicalValue(t *testing.T) {
	decimal, err := ParseDecimal("00154.32150000")
	if err != nil {
		t.Fatal(err)
	}
	if got := decimal.String(); got != "154.3215" {
		t.Fatalf("decimal = %q, want exact canonical value", got)
	}
	encoded, err := json.Marshal(decimal)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `"154.3215"` {
		t.Fatalf("decimal JSON = %s", encoded)
	}

	for _, value := range []string{"", "NaN", "1e3", "1.123456789", "1234567890123.00"} {
		if _, err := ParseDecimal(value); err == nil {
			t.Fatalf("ParseDecimal(%q) succeeded", value)
		}
	}
}

func TestSessionDateRetainsExchangeLocalMeaning(t *testing.T) {
	session, err := ParseSessionDate("2026-03-27")
	if err != nil {
		t.Fatal(err)
	}
	stockholm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatal(err)
	}
	local := session.Time(stockholm)
	if local.Format("2006-01-02") != session.String() || local.Location() != stockholm {
		t.Fatalf("session became %s in %s", local, local.Location())
	}
	if _, err := ParseSessionDate("2026-02-30"); err == nil {
		t.Fatal("invalid calendar date was accepted")
	}
}

func TestImportStatusCountsAndErrorsAreSafe(t *testing.T) {
	if ImportRunning.Terminal() || !ImportSucceeded.Terminal() || !ImportPartial.Terminal() || !ImportFailed.Terminal() || !ImportCancelled.Terminal() {
		t.Fatal("terminal import statuses are classified incorrectly")
	}
	if !(ImportCounts{Processed: 4, Accepted: 2, Rejected: 1, Flagged: 1}).Valid() {
		t.Fatal("valid import counts were rejected")
	}
	if (ImportCounts{Processed: 1, Accepted: 2}).Valid() || (ImportCounts{Processed: -1}).Valid() {
		t.Fatal("impossible import counts were accepted")
	}

	const secret = "provider-token-123"
	safe := SanitizeError("provider request failed: https://example.test/eod?api_token="+secret, secret)
	combined := safe.Code + " " + safe.Summary
	if safe.Code == "" || safe.Summary == "" {
		t.Fatal("sanitized errors require a stable code and useful summary")
	}
	if strings.Contains(combined, secret) || strings.Contains(combined, "api_token") || strings.Contains(combined, "https://") {
		t.Fatalf("sanitized error leaked sensitive request data: %q", combined)
	}
}
