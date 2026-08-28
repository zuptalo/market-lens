package instruments

import (
	"strings"
	"testing"
)

func TestUUIDRoundTripsCanonicalIdentity(t *testing.T) {
	const canonical = "df19f27e-d6d8-4eb1-a559-3e55ebfc2193"
	id, err := ParseUUID(canonical)
	if err != nil {
		t.Fatalf("parse canonical UUID: %v", err)
	}
	if !id.Valid() || id.String() != canonical {
		t.Fatalf("UUID = %q, valid=%t; want canonical valid identity", id, id.Valid())
	}

	generated, err := NewUUID()
	if err != nil {
		t.Fatalf("generate UUID: %v", err)
	}
	if !generated.Valid() || generated == id {
		t.Fatalf("generated UUID = %q, valid=%t", generated, generated.Valid())
	}
}

func TestUUIDRejectsMalformedIdentity(t *testing.T) {
	for _, value := range []string{"", "same-ticker", strings.Repeat("a", 36), "df19f27e-d6d8-0eb1-a559-3e55ebfc2193"} {
		if _, err := ParseUUID(value); err == nil {
			t.Fatalf("ParseUUID(%q) succeeded", value)
		}
	}
}
