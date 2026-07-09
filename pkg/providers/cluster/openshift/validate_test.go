package openshift

import (
	"context"
	"strings"
	"testing"
)

func TestValidateExistingRequiresContext(t *testing.T) {
	cc := clusterConfig(map[string]any{"mode": "existing", "kubeconfig": "/tmp/kc"})
	err := NewProvider().Validate(context.Background(), "proj", cc)
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("err = %v, want context-required error", err)
	}
}

func TestValidateProvisionRequiresRegion(t *testing.T) {
	t.Setenv("RHCS_TOKEN", "dummy")
	cc := clusterConfig(map[string]any{"mode": "provision"})
	err := NewProvider().Validate(context.Background(), "proj", cc)
	if err == nil || !strings.Contains(err.Error(), "region") {
		t.Fatalf("err = %v, want region-required error", err)
	}
}

func TestValidateProvisionRequiresRHCSToken(t *testing.T) {
	t.Setenv("RHCS_TOKEN", "")
	cc := clusterConfig(map[string]any{"mode": "provision", "region": "us-east-1"})
	err := NewProvider().Validate(context.Background(), "proj", cc)
	if err == nil || !strings.Contains(err.Error(), "RHCS_TOKEN") {
		t.Fatalf("err = %v, want RHCS_TOKEN-required error", err)
	}
}

func TestValidateUnknownMode(t *testing.T) {
	cc := clusterConfig(map[string]any{"mode": "bogus"})
	err := NewProvider().Validate(context.Background(), "proj", cc)
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("err = %v, want unknown-mode error", err)
	}
}
