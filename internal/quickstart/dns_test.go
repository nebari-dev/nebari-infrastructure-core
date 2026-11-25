package quickstart

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fakeResolver struct {
	addrs []string
	err   error
}

func (f fakeResolver) LookupHost(context.Context, string) ([]string, error) {
	return f.addrs, f.err
}

func TestResolvesToLoopback(t *testing.T) {
	tests := []struct {
		name    string
		addrs   []string
		err     error
		wantErr bool
	}{
		{name: "IPv4 loopback", addrs: []string{"127.0.0.1"}},
		{name: "other address in 127.0.0.0/8", addrs: []string{"127.0.1.1"}},
		{name: "IPv6 loopback", addrs: []string{"::1"}},
		{name: "loopback among other addresses", addrs: []string{"203.0.113.10", "127.0.0.1"}},
		{name: "public address only", addrs: []string{"203.0.113.10"}, wantErr: true},
		{name: "private address only", addrs: []string{"192.168.1.10"}, wantErr: true},
		{name: "no addresses", addrs: nil, wantErr: true},
		{name: "lookup error", err: errors.New("network is unreachable"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ResolvesToLoopback(context.Background(), fakeResolver{addrs: tt.addrs, err: tt.err}, "keycloak.nebari.lvh.me")
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolvesToLoopback() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolverChangedAnswer(t *testing.T) {
	tests := []struct {
		name  string
		addrs []string
		err   error
		want  bool
	}{
		{name: "public address only", addrs: []string{"203.0.113.10"}, want: true},
		{name: "no addresses", addrs: nil, want: true},
		{name: "not found", err: &net.DNSError{Err: "no such host", Name: "keycloak.nebari.lvh.me", IsNotFound: true}, want: true},
		{name: "timeout", err: &net.DNSError{Err: "i/o timeout", Name: "keycloak.nebari.lvh.me", IsTimeout: true}, want: false},
		{name: "context deadline", err: context.DeadlineExceeded, want: false},
		{name: "other network error", err: errors.New("network is unreachable"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ResolvesToLoopback(context.Background(), fakeResolver{addrs: tt.addrs, err: tt.err}, "keycloak.nebari.lvh.me")
			if err == nil {
				t.Fatal("ResolvesToLoopback() error = nil, want an error")
			}
			if got := ResolverChangedAnswer(err); got != tt.want {
				t.Errorf("ResolverChangedAnswer(%v) = %v, want %v", err, got, tt.want)
			}
		})
	}
}
