package cloudflare

import (
	"testing"
)

func TestProviderName(t *testing.T) {
	provider := NewProvider()
	if provider.Name() != "cloudflare" {
		t.Fatalf("Name() = %q, want %q", provider.Name(), "cloudflare")
	}
}
