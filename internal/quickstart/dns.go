package quickstart

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// Resolver looks up a host's addresses. *net.Resolver satisfies it.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

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
		return fmt.Errorf("%s resolves to no addresses", host)
	}
	return fmt.Errorf("%s resolves to %s, not to this machine", host, strings.Join(addrs, ", "))
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
