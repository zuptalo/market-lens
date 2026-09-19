package httpx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// forwardedHeader is the only forwarded header this product reads. Traefik sets it, and RFC 7239's
// Forwarded is deliberately not read: a header nothing in this deployment sends would be untested
// code on a security path.
const forwardedHeader = "X-Forwarded-For"

// maxForwardedHops bounds the work a client can cause by sending a very long chain. Nothing
// legitimate reaches this product through thirty-two proxies.
const maxForwardedHops = 32

// ClientAddress is the address the request came from, as far as this deployment can vouch for it.
//
// The peer is the starting point and, unless the peer is a proxy the operator named, it is also the
// answer — a forwarded header from an untrusted peer is not read at all, because any client can set
// one. Behind a trusted peer the chain is walked right to left and the first hop that is not itself
// trusted is the client: each real proxy appends, so the rightmost untrusted hop is the furthest
// point the deployment actually saw, while the leftmost is whatever the client chose to claim.
//
// Returns false when there is no address to be had. A request whose peer does not parse is not a
// reason to guess one, and the caller stores nothing.
func ClientAddress(request *http.Request, trusted []netip.Prefix) (netip.Addr, bool) {
	peer, ok := peerAddress(request)
	if !ok || !trustedAddress(peer, trusted) {
		return peer, ok
	}
	forwarded := strings.TrimSpace(request.Header.Get(forwardedHeader))
	if forwarded == "" {
		return peer, true
	}
	hops := strings.Split(forwarded, ",")
	if len(hops) > maxForwardedHops {
		return peer, true
	}
	for index := len(hops) - 1; index >= 0; index-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[index]))
		if err != nil {
			// A partially parsed chain is not evidence. Discard the whole thing and keep the one
			// address this process observed itself.
			return peer, true
		}
		hop = normalize(hop)
		if !trustedAddress(hop, trusted) {
			return hop, true
		}
	}
	// Every hop was a proxy this deployment trusts, so none of them is the client. The peer is the
	// honest answer.
	return peer, true
}

// ParseTrustedProxies reads the configured trust list. A bare address is the single-host prefix a
// person means when they write it.
//
// A malformed entry is an error rather than a skipped line: a trust list that silently failed to
// parse produces a screen full of plausible internal addresses that nobody would ever question.
func ParseTrustedProxies(values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		address, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("%q is not an address or CIDR network", value)
		}
		address = normalize(address)
		prefixes = append(prefixes, netip.PrefixFrom(address, address.BitLen()))
	}
	if len(prefixes) == 0 {
		return nil, nil
	}
	return prefixes, nil
}

func trustedAddress(address netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func peerAddress(request *http.Request) (netip.Addr, bool) {
	host := request.RemoteAddr
	if split, _, err := net.SplitHostPort(host); err == nil {
		host = split
	}
	address, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}, false
	}
	return normalize(address), true
}

// normalize strips what is true of the connection rather than of the client: an IPv4-mapped IPv6
// address is the IPv4 address it carries, and a zone identifies an interface on this machine.
func normalize(address netip.Addr) netip.Addr {
	return address.Unmap().WithZone("")
}

type clientAddressContextKey struct{}

// ResolveClientAddress settles the client address once, at the edge, and carries it on the request.
//
// Every handler that records an address — sign-in, member verification, owner setup, and the
// authentication middleware — must agree on what the client's address is, and each of them reading
// the header itself would be four chances to disagree. Resolution stays explicit where it matters:
// the middleware reads this back and passes it to the authentication service as an argument, so a
// security-relevant input is never invisible at the call site.
func ResolveClientAddress(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			address, ok := ClientAddress(request, trusted)
			if !ok {
				next.ServeHTTP(writer, request)
				return
			}
			next.ServeHTTP(writer, request.WithContext(
				context.WithValue(request.Context(), clientAddressContextKey{}, address)))
		})
	}
}

// ClientAddressFromContext is the address resolved at the edge. An invalid address means the
// request had none to resolve, which is recorded as nothing rather than guessed at.
func ClientAddressFromContext(request *http.Request) netip.Addr {
	address, _ := request.Context().Value(clientAddressContextKey{}).(netip.Addr)
	return address
}
