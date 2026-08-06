package argocd

import (
	"strconv"
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
			// Both bare and full-path group names must be mapped: the Keycloak
			// group-membership mapper's full.path setting differs depending on
			// whether the realm-setup job (false) or the data-science-pack
			// rbac-bootstrap job (true) ran last.
			for _, mapping := range []string{
				"g, argocd-admins, role:admin",
				"g, /argocd-admins, role:admin",
				"g, argocd-viewers, role:readonly",
				"g, /argocd-viewers, role:readonly",
			} {
				if !strings.Contains(policyCSV, mapping) {
					t.Errorf("policy.csv should contain %q", mapping)
				}
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

// TestDefaultConfigResources pins the ArgoCD resource defaults from #457.
// Upstream argo-cd ships resources: {} for every component, so without these
// values every ArgoCD pod runs BestEffort and is first evicted under pressure.
func TestDefaultConfigResources(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		component string
		cpuReq    string
		memReq    string
		cpuLim    string
		memLim    string
	}{
		// The controller and repo-server carry more headroom than the rest on
		// purpose: a 512Mi limit OOMKilled both on EKS. See goMemLimited.
		{"controller", "100m", "512Mi", "500m", "1024Mi"},
		{"repoServer", "25m", "128Mi", "500m", "1024Mi"},
		{"server", "25m", "64Mi", "200m", "128Mi"},
		{"applicationSet", "25m", "64Mi", "200m", "128Mi"},
		{"redis", "25m", "64Mi", "200m", "128Mi"},
		{"notifications", "25m", "64Mi", "200m", "128Mi"},
	}

	for _, tt := range tests {
		t.Run(tt.component, func(t *testing.T) {
			comp, ok := cfg.Values[tt.component].(map[string]any)
			if !ok {
				t.Fatalf("Values[%q] missing or not a map", tt.component)
			}
			res, ok := comp["resources"].(map[string]any)
			if !ok {
				t.Fatalf("Values[%q][resources] missing or not a map", tt.component)
			}
			req, ok := res["requests"].(map[string]any)
			if !ok {
				t.Fatalf("Values[%q] requests missing", tt.component)
			}
			lim, ok := res["limits"].(map[string]any)
			if !ok {
				t.Fatalf("Values[%q] limits missing", tt.component)
			}
			if req["cpu"] != tt.cpuReq || req["memory"] != tt.memReq {
				t.Errorf("requests = %v/%v, want %s/%s", req["cpu"], req["memory"], tt.cpuReq, tt.memReq)
			}
			if lim["cpu"] != tt.cpuLim || lim["memory"] != tt.memLim {
				t.Errorf("limits = %v/%v, want %s/%s", lim["cpu"], lim["memory"], tt.cpuLim, tt.memLim)
			}
		})
	}
}

// TestGoMemLimitComponents guards the second half of the OOMKill fix for both
// components that carry it. Raising a memory limit alone only moves the
// threshold; the soft ceiling is what makes the Go runtime collect before the
// kubelet kills the pod. Each case fails on a distinct way that protection can
// be lost, and a component missing from this table means it silently lost its
// GOMEMLIMIT.
func TestGoMemLimitComponents(t *testing.T) {
	components := []struct {
		name        string
		memLimitMiB int
	}{
		{"controller", controllerMemLimitMiB},
		{"repoServer", repoServerMemLimitMiB},
	}

	for _, c := range components {
		t.Run(c.name, func(t *testing.T) {
			comp, ok := DefaultConfig().Values[c.name].(map[string]any)
			if !ok {
				t.Fatalf("Values[%s] missing or not a map", c.name)
			}
			env, ok := comp["env"].([]map[string]any)
			if !ok {
				t.Fatalf("Values[%s][env] missing or not []map[string]any, got %T", c.name, comp["env"])
			}
			var goMemLimit string
			for _, e := range env {
				if e["name"] == "GOMEMLIMIT" {
					goMemLimit, _ = e["value"].(string)
				}
			}
			// Parsed rather than recomputed from the constants, so the assertions
			// describe the rendered value instead of restating how it was built.
			goMemLimitMiB, _ := strconv.Atoi(strings.TrimSuffix(goMemLimit, "MiB"))

			tests := []struct {
				name string
				got  any
				want any
				why  string
			}{
				{
					name: "GOMEMLIMIT is set",
					got:  goMemLimit != "",
					want: true,
					why:  "without it a memory spike is OOMKilled instead of collected",
				},
				{
					name: "uses the Go runtime byte suffix",
					got:  strings.HasSuffix(goMemLimit, "MiB"),
					want: true,
					why:  "the Go runtime throws on a malformed value, so Kubernetes' Mi crash-loops the container at startup",
				},
				{
					name: "stays below the memory limit",
					got:  goMemLimitMiB > 0 && goMemLimitMiB < c.memLimitMiB,
					want: true,
					why:  "a soft ceiling at or above the hard limit collects too late to help",
				},
				{
					name: "clears the measured EKS peak",
					// Measured cold-render/reconcile peaks were 600MiB and 661MiB.
					// A ceiling below that guarantees GC thrashing at best.
					got:  goMemLimitMiB > 700,
					want: true,
					why:  "the ceiling must sit above the measured peak or GC fights the limit on every spike",
				},
				{
					name: "percentage matches Argo CD's guidance",
					got:  goMemLimitPercent >= 80 && goMemLimitPercent <= 90,
					want: true,
					why:  "Argo CD's HA guide recommends 80-90%; higher risks OOM, lower risks GC thrashing",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					if tt.got != tt.want {
						t.Errorf("got %v, want %v: %s", tt.got, tt.want, tt.why)
					}
				})
			}
		})
	}
}

// TestDefaultConfigDisablesDex: NIC wires ArgoCD OIDC directly to Keycloak,
// so the dex pod the chart deploys by default is never referenced (#457).
func TestDefaultConfigDisablesDex(t *testing.T) {
	cfg := DefaultConfig()
	dex, ok := cfg.Values["dex"].(map[string]any)
	if !ok {
		t.Fatal("Values[dex] missing or not a map")
	}
	// The chart default is enabled=true, so a missing key means dex deploys.
	enabled, ok := dex["enabled"].(bool)
	if !ok {
		t.Fatal("dex.enabled missing or not a bool; the chart would deploy dex by default")
	}
	if enabled {
		t.Error("dex should be disabled: NIC wires OIDC directly to Keycloak (#457)")
	}
}
