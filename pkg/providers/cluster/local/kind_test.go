package local

import (
	"testing"
)

func TestDeriveAddressPool(t *testing.T) {
	tests := []struct {
		name    string
		subnet  string
		want    string
		wantErr bool
	}{
		{
			name:   "kind default /16",
			subnet: "172.18.0.0/16",
			want:   "172.18.255.100-172.18.255.110",
		},
		{
			name:   "/24 subnet",
			subnet: "192.168.1.0/24",
			want:   "192.168.1.100-192.168.1.110",
		},
		{
			// kindNodeAddressPool feeds "<nodeIP>/16"; ParseCIDR must mask the
			// host bits so a node IP yields the same pool as the bare network.
			name:   "node IP with host bits is masked to the network",
			subnet: "172.19.0.2/16",
			want:   "172.19.255.100-172.19.255.110",
		},
		{
			name:   "/12 subnet",
			subnet: "10.96.0.0/12",
			want:   "10.111.255.100-10.111.255.110",
		},
		{
			name:    "smaller than /24",
			subnet:  "192.168.1.0/25",
			wantErr: true,
		},
		{
			name:    "IPv6 subnet",
			subnet:  "fc00:f853:ccd:e793::/64",
			wantErr: true,
		},
		{
			name:    "not a CIDR",
			subnet:  "172.18.0.0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deriveAddressPool(tt.subnet)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("deriveAddressPool(%q) = %q, want error", tt.subnet, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("deriveAddressPool(%q) returned error: %v", tt.subnet, err)
			}
			if got != tt.want {
				t.Errorf("deriveAddressPool(%q) = %q, want %q", tt.subnet, got, tt.want)
			}
		})
	}
}

func TestKindContextName(t *testing.T) {
	if got := kindContextName("my-nebari-local"); got != "kind-my-nebari-local" {
		t.Errorf("kindContextName = %q, want %q", got, "kind-my-nebari-local")
	}
}
