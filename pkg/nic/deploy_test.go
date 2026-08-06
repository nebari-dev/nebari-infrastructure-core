package nic

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
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

func TestCommittedConfig(t *testing.T) {
	t.Run("rewrites path-based trust_bundle to resolved inline", func(t *testing.T) {
		const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
		cfg := &config.NebariConfig{
			TrustBundle: &config.TrustBundleConfig{Path: "/home/operator/org-ca.pem"},
		}

		committed := committedConfig(cfg, pem)

		if committed.TrustBundle == nil {
			t.Fatal("TrustBundle should be preserved as inline, got nil")
		}
		if committed.TrustBundle.Path != "" {
			t.Errorf("machine-local path should be dropped, got %q", committed.TrustBundle.Path)
		}
		if committed.TrustBundle.Inline != pem {
			t.Errorf("Inline = %q, want resolved PEM", committed.TrustBundle.Inline)
		}
		// Input must not be mutated.
		if cfg.TrustBundle.Path != "/home/operator/org-ca.pem" || cfg.TrustBundle.Inline != "" {
			t.Errorf("input cfg.TrustBundle mutated: %+v", cfg.TrustBundle)
		}
	})

	t.Run("strips trust_bundle that resolved to empty", func(t *testing.T) {
		cfg := &config.NebariConfig{
			TrustBundle: &config.TrustBundleConfig{Path: "   "},
		}

		committed := committedConfig(cfg, "")

		if committed.TrustBundle != nil {
			t.Errorf("TrustBundle should be stripped when resolved PEM is empty, got %+v", committed.TrustBundle)
		}
	})

	t.Run("leaves unset trust_bundle nil", func(t *testing.T) {
		committed := committedConfig(&config.NebariConfig{}, "")

		if committed.TrustBundle != nil {
			t.Errorf("TrustBundle should stay nil, got %+v", committed.TrustBundle)
		}
	})

	t.Run("marshalled output excludes machine-local trust_bundle path", func(t *testing.T) {
		const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
		cfg := &config.NebariConfig{
			ProjectName: "test",
			TrustBundle: &config.TrustBundleConfig{Path: "/home/operator/org-ca.pem"},
		}

		out, err := yaml.Marshal(committedConfig(cfg, pem))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(out)

		if strings.Contains(s, "/home/operator/org-ca.pem") {
			t.Errorf("committed output should not contain the local path:\n%s", s)
		}
		if !strings.Contains(s, "-----BEGIN CERTIFICATE-----") {
			t.Errorf("committed output should contain the resolved inline PEM:\n%s", s)
		}
	})
}
