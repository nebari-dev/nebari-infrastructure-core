package quickstart

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Resolver looks up a host's addresses. *net.Resolver satisfies it.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// ErrNotLoopback means the lookup answered, but not with an address on this
// machine.
var ErrNotLoopback = errors.New("does not resolve to this machine")

// ResolvesToLoopback returns nil if host resolves to a loopback address on
// this machine, which is where kind publishes the gateway. Otherwise it returns
// an error saying why. Some ISP and router resolvers drop public DNS answers
// that point at 127.0.0.1 (DNS rebinding protection), so a public wildcard such
// as lvh.me can fail on one network and work on another.
func ResolvesToLoopback(ctx context.Context, r Resolver, host string) error {
	addrs, err := r.LookupHost(ctx, host)
	if err != nil {
		return fmt.Errorf("look up %s: %w", host, err)
	}
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%s resolves to no addresses: %w", host, ErrNotLoopback)
	}
	return fmt.Errorf("%s resolves to %s: %w", host, strings.Join(addrs, ", "), ErrNotLoopback)
}

// ResolverChangedAnswer reports whether err from ResolvesToLoopback shows the
// resolver answered, either with addresses that aren't on this machine or by
// saying the host doesn't exist. For a host that resolves to 127.0.0.1 in
// public DNS, both mean the resolver altered the answer, which DNS rebinding
// protection does. Any other error, such as a timeout, means the lookup
// couldn't finish, so nothing is known about the resolver.
func ResolverChangedAnswer(err error) bool {
	if errors.Is(err, ErrNotLoopback) {
		return true
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// Hostnames returns every hostname the quickstart serves: the landing page,
// the foundational apps and the apps of the packs it installs.
func Hostnames(domain string) []string {
	return []string{
		domain,
		"keycloak." + domain,
		"argocd." + domain,
		"hub." + domain,
		"nebi." + domain,
	}
}
