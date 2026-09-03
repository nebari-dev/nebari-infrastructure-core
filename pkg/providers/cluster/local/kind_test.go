package local

import (
	"testing"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	clusterapi "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

func TestGatewayPortMappings(t *testing.T) {
	tests := []struct {
		name          string
		httpPort      int
		httpsPort     int
		wantHostHTTP  int32
		wantHostHTTPS int32
	}{
		{
			name:          "default ports",
			wantHostHTTP:  80,
			wantHostHTTPS: 443,
		},
		{
			name:          "custom ports for occupied 80/443 or rootless runtimes",
			httpPort:      8080,
			httpsPort:     8443,
			wantHostHTTP:  8080,
			wantHostHTTPS: 8443,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mappings := gatewayPortMappings(tt.httpPort, tt.httpsPort)
			if len(mappings) != 2 {
				t.Fatalf("gatewayPortMappings(%d, %d) returned %d mappings, want 2", tt.httpPort, tt.httpsPort, len(mappings))
			}

			http, https := mappings[0], mappings[1]
			if http.ContainerPort != clusterapi.GatewayHTTPNodePort || http.HostPort != tt.wantHostHTTP {
				t.Errorf("http mapping = %d->%d, want %d->%d", http.HostPort, http.ContainerPort, tt.wantHostHTTP, clusterapi.GatewayHTTPNodePort)
			}
			if https.ContainerPort != clusterapi.GatewayHTTPSNodePort || https.HostPort != tt.wantHostHTTPS {
				t.Errorf("https mapping = %d->%d, want %d->%d", https.HostPort, https.ContainerPort, tt.wantHostHTTPS, clusterapi.GatewayHTTPSNodePort)
			}

			for _, m := range mappings {
				// Loopback on purpose: a development cluster must not be
				// published on the LAN.
				if m.ListenAddress != "127.0.0.1" {
					t.Errorf("mapping %d->%d listens on %q, want 127.0.0.1", m.HostPort, m.ContainerPort, m.ListenAddress)
				}
				if m.Protocol != v1alpha4.PortMappingProtocolTCP {
					t.Errorf("mapping %d->%d protocol = %q, want TCP", m.HostPort, m.ContainerPort, m.Protocol)
				}
			}
		})
	}
}

func TestKindContextName(t *testing.T) {
	if got := kindContextName("my-nebari-local"); got != "kind-my-nebari-local" {
		t.Errorf("kindContextName = %q, want %q", got, "kind-my-nebari-local")
	}
}
