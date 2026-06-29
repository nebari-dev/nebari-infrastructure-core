package nic

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
			result, err := generateSecurePassword(reader)
			if err != nil {
				t.Fatalf("generateSecurePassword() unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("generateSecurePassword() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGenerateSecurePasswordLength(t *testing.T) {
	input := bytes.Repeat([]byte{0x42}, 32)
	reader := bytes.NewReader(input)
	result, err := generateSecurePassword(reader)
	if err != nil {
		t.Fatalf("generateSecurePassword() unexpected error: %v", err)
	}

	if len(result) != 43 {
		t.Errorf("generateSecurePassword() length = %d, want 43", len(result))
	}
}

// TestGenerateSecurePasswordError verifies that a failing reader yields an
// error rather than a weak, predictable fallback. These strings end up as
// admin credentials on the deployed cluster, so silently substituting a
// timestamp-based string would be a serious security regression.
func TestGenerateSecurePasswordError(t *testing.T) {
	t.Run("empty reader returns error", func(t *testing.T) {
		result, err := generateSecurePassword(bytes.NewReader([]byte{}))
		if err == nil {
			t.Fatalf("generateSecurePassword() expected error, got result = %q", result)
		}
		if result != "" {
			t.Errorf("generateSecurePassword() on error should return empty string, got %q", result)
		}
	})

	t.Run("short reader returns error", func(t *testing.T) {
		// 16 bytes is half of what we need - io.ReadFull must fail.
		result, err := generateSecurePassword(bytes.NewReader(bytes.Repeat([]byte{0x00}, 16)))
		if err == nil {
			t.Fatalf("generateSecurePassword() expected error for short read, got result = %q", result)
		}
		if result != "" {
			t.Errorf("generateSecurePassword() on error should return empty string, got %q", result)
		}
	})
}
