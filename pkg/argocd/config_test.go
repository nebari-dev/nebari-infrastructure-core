package argocd

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Test version is set
	if cfg.Version == "" {
		t.Error("DefaultConfig().Version should not be empty")
	}

	// Test namespace is argocd
	if cfg.Namespace != "argocd" {
		t.Errorf("DefaultConfig().Namespace = %q, want %q", cfg.Namespace, "argocd")
	}

	// Test release name is argocd
	if cfg.ReleaseName != "argocd" {
		t.Errorf("DefaultConfig().ReleaseName = %q, want %q", cfg.ReleaseName, "argocd")
	}

	// Test timeout is reasonable (at least 1 minute)
	if cfg.Timeout < time.Minute {
		t.Errorf("DefaultConfig().Timeout = %v, want at least 1 minute", cfg.Timeout)
	}

	// Test values are set
	if cfg.Values == nil {
		t.Error("DefaultConfig().Values should not be nil")
	}

	// Test server.insecure is set in values
	configs, ok := cfg.Values["configs"].(map[string]any)
	if !ok {
		t.Fatal("DefaultConfig().Values[\"configs\"] should be a map")
	}
	params, ok := configs["params"].(map[string]any)
	if !ok {
		t.Fatal("DefaultConfig().Values[\"configs\"][\"params\"] should be a map")
	}
	insecure, ok := params["server.insecure"].(bool)
	if !ok || !insecure {
		t.Error("DefaultConfig().Values should have server.insecure = true")
	}
}

func TestConfigFields(t *testing.T) {
	// Test that Config struct can be created with custom values
	cfg := Config{
		Version:     "1.0.0",
		Namespace:   "custom-namespace",
		ReleaseName: "custom-release",
		Timeout:     10 * time.Minute,
		Values: map[string]any{
			"key": "value",
		},
	}

	if cfg.Version != "1.0.0" {
		t.Errorf("Config.Version = %q, want %q", cfg.Version, "1.0.0")
	}
	if cfg.Namespace != "custom-namespace" {
		t.Errorf("Config.Namespace = %q, want %q", cfg.Namespace, "custom-namespace")
	}
	if cfg.ReleaseName != "custom-release" {
		t.Errorf("Config.ReleaseName = %q, want %q", cfg.ReleaseName, "custom-release")
	}
	if cfg.Timeout != 10*time.Minute {
		t.Errorf("Config.Timeout = %v, want %v", cfg.Timeout, 10*time.Minute)
	}
	if cfg.Values["key"] != "value" {
		t.Errorf("Config.Values[\"key\"] = %v, want %q", cfg.Values["key"], "value")
	}
}
