package config

import (
	"testing"
)

func TestIsValidProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     bool
	}{
		{
			name:     "aws is valid",
			provider: "aws",
			want:     true,
		},
		{
			name:     "gcp is valid",
			provider: "gcp",
			want:     true,
		},
		{
			name:     "azure is valid",
			provider: "azure",
			want:     true,
		},
		{
			name:     "local is valid",
			provider: "local",
			want:     true,
		},
		{
			name:     "empty string is invalid",
			provider: "",
			want:     false,
		},
		{
			name:     "unknown provider is invalid",
			provider: "unknown",
			want:     false,
		},
		{
			name:     "AWS uppercase is invalid",
			provider: "AWS",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidProvider(tt.provider)
			if got != tt.want {
				t.Errorf("IsValidProvider(%q) = %v, want %v", tt.provider, got, tt.want)
			}
		})
	}
}

func TestProviderConfig_NilMapAccess(t *testing.T) {
	// Verify that accessing a nil ProviderConfig map returns nil (Go behavior)
	cfg := &NebariConfig{
		ProjectName: "test",
		Provider:    "aws",
		// ProviderConfig is nil
	}

	// Reading from nil map should return nil, not panic
	got := cfg.ProviderConfig["amazon_web_services"]
	if got != nil {
		t.Errorf("Expected nil from nil map access, got %v", got)
	}
}

func TestProviderConfig_DirectAccess(t *testing.T) {
	type mockConfig struct {
		Region string
		Zone   string
	}

	cfg := &NebariConfig{
		ProjectName: "test",
		Provider:    "aws",
		ProviderConfig: map[string]any{
			"amazon_web_services": &mockConfig{Region: "us-west-2", Zone: "a"},
		},
	}

	// Access existing key
	rawCfg := cfg.ProviderConfig["amazon_web_services"]
	if rawCfg == nil {
		t.Fatal("Expected non-nil config for existing key")
	}

	awsCfg, ok := rawCfg.(*mockConfig)
	if !ok {
		t.Fatalf("Expected *mockConfig, got %T", rawCfg)
	}
	if awsCfg.Region != "us-west-2" {
		t.Errorf("Region = %q, want %q", awsCfg.Region, "us-west-2")
	}

	// Access non-existing key
	missing := cfg.ProviderConfig["nonexistent"]
	if missing != nil {
		t.Errorf("Expected nil for missing key, got %v", missing)
	}
}

func TestProviderConfig_MultipleProviders(t *testing.T) {
	// Verify multiple provider configs can coexist
	cfg := &NebariConfig{
		ProjectName: "test",
		Provider:    "aws",
		ProviderConfig: map[string]any{
			"amazon_web_services":   map[string]any{"region": "us-west-2"},
			"google_cloud_platform": map[string]any{"project": "my-project"},
			"azure":                 map[string]any{"region": "eastus"},
		},
	}

	// All should be accessible
	if cfg.ProviderConfig["amazon_web_services"] == nil {
		t.Error("AWS config should not be nil")
	}
	if cfg.ProviderConfig["google_cloud_platform"] == nil {
		t.Error("GCP config should not be nil")
	}
	if cfg.ProviderConfig["azure"] == nil {
		t.Error("Azure config should not be nil")
	}
}

func TestNebariConfig_RuntimeOptions(t *testing.T) {
	// Verify runtime options are independent of YAML parsing
	cfg := &NebariConfig{
		ProjectName:    "test",
		Provider:       "aws",
		ProviderConfig: map[string]any{"amazon_web_services": map[string]any{}},
		DryRun:         true,
		Force:          true,
	}

	if !cfg.DryRun {
		t.Error("DryRun should be true")
	}
	if !cfg.Force {
		t.Error("Force should be true")
	}
}
