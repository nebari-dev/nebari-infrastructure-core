package main

import (
	"bytes"
	"testing"
)

func TestGenerateSecurePassword(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "deterministic output with known bytes",
			input:    bytes.Repeat([]byte{0x00}, 32),
			expected: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		},
		{
			name:     "deterministic output with different bytes",
			input:    bytes.Repeat([]byte{0xFF}, 32),
			expected: "__________________________________________8",
		},
		{
			name:     "mixed bytes produce consistent output",
			input:    []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20},
			expected: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader(tt.input)
			result := generateSecurePassword(reader)

			if result != tt.expected {
				t.Errorf("generateSecurePassword() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGenerateSecurePasswordLength(t *testing.T) {
	input := bytes.Repeat([]byte{0x42}, 32)
	reader := bytes.NewReader(input)
	result := generateSecurePassword(reader)

	if len(result) != 43 {
		t.Errorf("generateSecurePassword() length = %d, want 43", len(result))
	}
}

func TestGenerateSecurePasswordFallback(t *testing.T) {
	// Empty reader will cause Read to fail
	reader := bytes.NewReader([]byte{})
	result := generateSecurePassword(reader)

	// Should return fallback format "nebari-<timestamp>"
	if len(result) < 7 || result[:7] != "nebari-" {
		t.Errorf("generateSecurePassword() fallback = %q, want prefix 'nebari-'", result)
	}
}
