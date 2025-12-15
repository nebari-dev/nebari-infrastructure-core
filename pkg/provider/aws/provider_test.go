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

// Note: All validation tests have been consolidated into provider_validate_test.go
// using table-driven test patterns for better maintainability

// isCredentialError checks if an error is related to AWS credentials
// This helps us distinguish validation errors from credential errors in tests
func isCredentialError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsSubstring([]string{msg}, "credentials") ||
		containsSubstring([]string{msg}, "AWS_ACCESS_KEY_ID") ||
		containsSubstring([]string{msg}, "no EC2 IMDS role found") ||
		containsSubstring([]string{msg}, "failed to initialize AWS clients")
}
