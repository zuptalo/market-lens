package httpx_test

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"market-lens/server/internal/httpx"
)

func prefixes(t *testing.T, values ...string) []netip.Prefix {
	t.Helper()
	parsed, err := httpx.ParseTrustedProxies(values)
	if err != nil {
		t.Fatalf("parse trusted proxies %v: %v", values, err)
	}
	return parsed
}

func TestClientAddress(t *testing.T) {
	cluster := prefixes(t, "10.42.0.0/16")

	cases := []struct {
		name      string
		remote    string
		forwarded string
		trusted   []netip.Prefix
		want      string
	}{
		{
			// The deployment this feature exists for. Traefik is the peer; the client is in the
			// header it appended.
			name:   "behind a trusted proxy the forwarded client is the client",
			remote: "10.42.0.31:41234", forwarded: "203.0.113.12", trusted: cluster,
			want: "203.0.113.12",
		},
		{
			// Nothing configured, so nothing vouches for the header. The peer is all there is.
			name:   "an untrusted peer's forwarded header is ignored entirely",
			remote: "203.0.113.12:41234", forwarded: "192.0.2.7", trusted: nil,
			want: "203.0.113.12",
		},
		{
			// The forgery D1 rejects: a client that sets the header itself, reaching a product
			// that does not trust it.
			name:   "a forged header from an untrusted peer changes nothing",
			remote: "198.51.100.9:443", forwarded: "10.42.0.31, 192.0.2.7", trusted: cluster,
			want: "198.51.100.9",
		},
		{
			// Read right to left: each real proxy appends, so the rightmost untrusted hop is the
			// furthest point the deployment can actually vouch for.
			name:   "the rightmost untrusted hop wins, not the leftmost claim",
			remote: "10.42.0.31:41234", forwarded: "192.0.2.7, 203.0.113.12, 10.42.0.9", trusted: cluster,
			want: "203.0.113.12",
		},
		{
			name:   "a chain of nothing but trusted proxies falls back to the peer",
			remote: "10.42.0.31:41234", forwarded: "10.42.0.9, 10.42.0.7", trusted: cluster,
			want: "10.42.0.31",
		},
		{
			name:   "no header behind a trusted proxy is the proxy itself",
			remote: "10.42.0.31:41234", forwarded: "", trusted: cluster,
			want: "10.42.0.31",
		},
		{
			// A partially parsed chain is not evidence.
			name:   "a malformed chain is discarded whole",
			remote: "10.42.0.31:41234", forwarded: "203.0.113.12, not-an-address", trusted: cluster,
			want: "10.42.0.31",
		},
		{
			name:   "IPv6 survives, with its port and its brackets removed",
			remote: "[2001:db8::1]:41234", forwarded: "", trusted: nil,
			want: "2001:db8::1",
		},
		{
			name:   "an IPv4-mapped IPv6 client is recorded as the IPv4 address it is",
			remote: "10.42.0.31:41234", forwarded: "::ffff:203.0.113.12", trusted: cluster,
			want: "203.0.113.12",
		},
		{
			// A zone on a link-local address is a property of the machine that received it and
			// means nothing to the person reading the screen.
			name:   "an address zone is dropped",
			remote: "[fe80::1%eth0]:41234", forwarded: "", trusted: nil,
			want: "fe80::1",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/api/v1/account/sessions", nil)
			request.RemoteAddr = testCase.remote
			if testCase.forwarded != "" {
				request.Header.Set("X-Forwarded-For", testCase.forwarded)
			}
			got, ok := httpx.ClientAddress(request, testCase.trusted)
			if !ok {
				t.Fatalf("expected an address, got none")
			}
			if got.String() != testCase.want {
				t.Fatalf("expected %s, got %s", testCase.want, got.String())
			}
		})
	}
}

// A request whose peer is not an address at all is not a reason to guess one.
func TestClientAddressReportsWhenThereIsNone(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/account/sessions", nil)
	request.RemoteAddr = ""
	request.Header.Set("X-Forwarded-For", "203.0.113.12")
	if got, ok := httpx.ClientAddress(request, nil); ok {
		t.Fatalf("expected no address, got %s", got)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	// A bare address is the single-host prefix a person means when they write it.
	parsed, err := httpx.ParseTrustedProxies([]string{"10.42.0.31", "2001:db8::/32"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed) != 2 || parsed[0].String() != "10.42.0.31/32" || parsed[1].String() != "2001:db8::/32" {
		t.Fatalf("unexpected prefixes: %v", parsed)
	}

	// Refused rather than quietly dropped: a trust list that silently failed to parse produces a
	// screen full of plausible internal addresses nobody would ever question (FR-006).
	if _, err := httpx.ParseTrustedProxies([]string{"10.42.0.0/16", "not-a-network"}); err == nil {
		t.Fatalf("expected a malformed entry to be refused")
	}
}
