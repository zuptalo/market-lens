package notify

import (
	"strings"
	"testing"
)

// A person looking at a list of devices has to be able to find the one in their hand.
//
// The endpoint itself is never returned — it is where somebody reads their mail, and a page has no
// use for it. A digest of it is enough: the browser knows its own endpoint and can compute the same
// digest, and every other row stays opaque.
func TestADeviceCanBeRecognisedWithoutHandingOutItsEndpoint(t *testing.T) {
	const endpoint = "https://push.example.invalid/subscription/abcdef"

	digest := EndpointDigest(endpoint)
	if digest == "" {
		t.Fatal("no digest was produced")
	}
	if strings.Contains(endpoint, digest) || strings.Contains(digest, "push.example") {
		t.Errorf("the digest carries part of the endpoint: %q", digest)
	}
	// Short enough to be cheap, long enough that two devices will not collide.
	if len(digest) != 16 {
		t.Errorf("the digest is %d characters", len(digest))
	}

	// The same endpoint always gives the same answer, or a browser could never match its own row.
	if EndpointDigest(endpoint) != digest {
		t.Errorf("the digest is not stable")
	}
	// A different endpoint gives a different answer.
	if EndpointDigest(endpoint+"x") == digest {
		t.Errorf("two endpoints share a digest")
	}
}
