package argocd

import (
	"strings"
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

func TestConfigWithOIDC(t *testing.T) {
	tests := []struct {
		name             string
		domain           string
		keycloakBasePath string
		clientSecret     string
		wantIssuer       string
		wantURL          string
	}{
		{
			name:             "standard domain with no base path",
			domain:           "nebari.example.com",
			keycloakBasePath: "",
			clientSecret:     "test-secret-123",
			wantIssuer:       "https://keycloak.nebari.example.com/realms/nebari",
			wantURL:          "https://argocd.nebari.example.com",
		},
		{
			name:             "domain with keycloak base path",
			domain:           "nebari.example.com",
			keycloakBasePath: "/auth",
			clientSecret:     "test-secret-456",
			wantIssuer:       "https://keycloak.nebari.example.com/auth/realms/nebari",
			wantURL:          "https://argocd.nebari.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConfigWithOIDC(tt.domain, tt.keycloakBasePath, tt.clientSecret)

			// Should preserve defaults
			if cfg.Version == "" {
				t.Error("Version should not be empty")
			}
			if cfg.Namespace != "argocd" {
				t.Errorf("Namespace = %q, want %q", cfg.Namespace, "argocd")
			}

			// Should still have server.insecure
			configs := cfg.Values["configs"].(map[string]any)
			params := configs["params"].(map[string]any)
			if insecure, ok := params["server.insecure"].(bool); !ok || !insecure {
				t.Error("server.insecure should be true")
			}

			// Check OIDC config in configs.cm
			cm := configs["cm"].(map[string]any)
			if cm["url"] != tt.wantURL {
				t.Errorf("cm.url = %q, want %q", cm["url"], tt.wantURL)
			}
			oidcConfig, ok := cm["oidc.config"].(string)
			if !ok {
				t.Fatal("cm[oidc.config] should be a string")
			}
			if !strings.Contains(oidcConfig, "name: Keycloak") {
				t.Error("oidc.config should contain 'name: Keycloak'")
			}
			if !strings.Contains(oidcConfig, "issuer: "+tt.wantIssuer) {
				t.Errorf("oidc.config should contain issuer %q, got:\n%s", tt.wantIssuer, oidcConfig)
			}
			if !strings.Contains(oidcConfig, "clientID: argocd") {
				t.Error("oidc.config should contain 'clientID: argocd'")
			}
			if !strings.Contains(oidcConfig, "$oidc.keycloak.clientSecret") {
				t.Error("oidc.config should reference $oidc.keycloak.clientSecret")
			}
			if !strings.Contains(oidcConfig, "groups") {
				t.Error("oidc.config should request groups scope")
			}

			// Check RBAC config
			rbac := configs["rbac"].(map[string]any)
			if rbac["policy.default"] != "" {
				t.Errorf("rbac.policy.default = %q, want empty string", rbac["policy.default"])
			}
			if rbac["scopes"] != "[groups]" {
				t.Errorf("rbac.scopes = %q, want %q", rbac["scopes"], "[groups]")
			}
			policyCSV, ok := rbac["policy.csv"].(string)
			if !ok {
				t.Fatal("rbac.policy.csv should be a string")
			}
			if !strings.Contains(policyCSV, "g, argocd-admins, role:admin") {
				t.Error("policy.csv should map argocd-admins to role:admin")
			}
			if !strings.Contains(policyCSV, "g, argocd-viewers, role:readonly") {
				t.Error("policy.csv should map argocd-viewers to role:readonly")
			}

			// Check secret injection
			secret := configs["secret"].(map[string]any)
			extra := secret["extra"].(map[string]any)
			if extra["oidc.keycloak.clientSecret"] != tt.clientSecret {
				t.Errorf("secret.extra[oidc.keycloak.clientSecret] = %q, want %q",
					extra["oidc.keycloak.clientSecret"], tt.clientSecret)
			}
		})
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
