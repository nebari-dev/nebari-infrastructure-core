package aws

import (
	"testing"
)

// TestProviderName tests the Name method
func TestProviderName(t *testing.T) {
	provider := NewProvider()
	if provider.Name() != "aws" {
		t.Errorf("expected provider name to be 'aws', got %s", provider.Name())
	}
}

// TestNewProvider tests provider creation
func TestNewProvider(t *testing.T) {
	provider := NewProvider()
	if provider == nil {
		t.Fatal("expected provider to be non-nil")
	}
}
