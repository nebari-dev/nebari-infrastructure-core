package azure

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestInfraSettings(t *testing.T) {
	p := NewProvider()
	cfg := &config.NebariConfig{
		Provider:    "azure",
		ProjectName: "test",
	}

	settings := p.InfraSettings(cfg)

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"StorageClass", settings.StorageClass, "managed-csi"},
		{"NeedsMetalLB", settings.NeedsMetalLB, false},
		{"LoadBalancerAnnotations is empty", len(settings.LoadBalancerAnnotations), 0},
		{"KeycloakBasePath is empty", settings.KeycloakBasePath, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
