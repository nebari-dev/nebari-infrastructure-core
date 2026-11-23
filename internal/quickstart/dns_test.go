package quickstart

import (
	"context"
	"errors"
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
		{name: "lookup error", err: errors.New("no such host"), wantErr: true},
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
