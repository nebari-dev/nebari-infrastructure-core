package main

import (
	"bytes"
	"strings"
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

func TestScrubSensitiveFields(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string // strings that should be in output
		excludes []string // strings that should NOT be in output
	}{
		{
			name: "scrubs auth block from git_repository",
			input: `project_name: test
git_repository:
  url: "git@github.com:org/repo.git"
  branch: main
  auth:
    ssh_key_env: MY_SSH_KEY
    token_env: MY_TOKEN
provider: aws`,
			contains: []string{
				"project_name: test",
				"url: \"git@github.com:org/repo.git\"",
				"branch: main",
				"provider: aws",
				"# auth: <scrubbed for security>",
			},
			excludes: []string{
				"ssh_key_env",
				"MY_SSH_KEY",
				"token_env",
				"MY_TOKEN",
			},
		},
		{
			name: "preserves config without auth block",
			input: `project_name: test
git_repository:
  url: "file:///tmp/repo"
  branch: main
provider: local`,
			contains: []string{
				"project_name: test",
				"url: \"file:///tmp/repo\"",
				"branch: main",
				"provider: local",
			},
			excludes: []string{},
		},
		{
			name: "handles nested auth with multiple fields",
			input: `git_repository:
  url: test
  auth:
    ssh_key_env: KEY
    token_env: TOKEN
    other_field: value
  path: clusters/prod`,
			contains: []string{
				"url: test",
				"path: clusters/prod",
				"# auth: <scrubbed for security>",
			},
			excludes: []string{
				"ssh_key_env",
				"token_env",
				"other_field",
				"KEY",
				"TOKEN",
				"value",
			},
		},
		{
			name:     "handles empty config",
			input:    "",
			contains: []string{},
			excludes: []string{},
		},
		{
			name: "preserves auth blocks outside git_repository",
			input: `amazon_web_services:
  auth:
    role_arn: arn:aws:iam::123456:role/deploy
git_repository:
  url: "git@github.com:org/repo.git"
  auth:
    ssh_key_env: MY_SSH_KEY
provider: aws`,
			contains: []string{
				"amazon_web_services:",
				"auth:",
				"role_arn: arn:aws:iam::123456:role/deploy",
				"url: \"git@github.com:org/repo.git\"",
				"# auth: <scrubbed for security>",
				"provider: aws",
			},
			excludes: []string{
				"ssh_key_env",
				"MY_SSH_KEY",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(scrubSensitiveFields([]byte(tt.input)))

			for _, s := range tt.contains {
				if !containsString(result, s) {
					t.Errorf("scrubSensitiveFields() should contain %q, got:\n%s", s, result)
				}
			}

			for _, s := range tt.excludes {
				if containsString(result, s) {
					t.Errorf("scrubSensitiveFields() should NOT contain %q, got:\n%s", s, result)
				}
			}
		})
	}
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
