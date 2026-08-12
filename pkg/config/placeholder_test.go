package config

import (
	"errors"
	"testing"
)

// allProviders lists every cluster provider name so Validate accepts the
// provider used in each test case. Placeholder detection runs before the
// provider lookup, but the PASS cases must clear the full Validate path.
var allProviders = []string{"aws", "gcp", "azure", "hetzner", "local", "existing"}

func validateOpts() ValidateOptions {
	return ValidateOptions{
		ClusterProviders: allProviders,
		DNSProviders:     []string{"cloudflare"},
	}
}

// clusterFor builds a minimal, valid cluster block for the given provider. The
// provider config only needs to be a mapping for config-level Validate to pass;
// provider-specific field validation is not reached here.
func clusterFor(provider string, providerCfg map[string]any) *ClusterConfig {
	if providerCfg == nil {
		providerCfg = map[string]any{}
	}
	return &ClusterConfig{Providers: map[string]any{provider: providerCfg}}
}

// TestCheckPlaceholders covers, per provider, three shapes:
//
//	(a) a placeholder in a required scalar (project_name),
//	(b) a placeholder nested inside the provider block,
//	(c) a legitimately-filled config that must PASS full Validate.
//
// The nested-block cases are the load-bearing ones: they prove the reflection
// walk descends into cluster.<provider>.* maps. Removing checkPlaceholders from
// Validate makes every want-placeholder case fail (the config would otherwise
// validate clean), which is the intended red step.
func TestCheckPlaceholders(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *NebariConfig
		wantField string // non-empty => expect a *PlaceholderError with this FieldPath
		wantOK    bool   // true => expect Validate to return nil
	}{
		// --- placeholder in a required scalar ---
		{
			name: "aws placeholder in project_name",
			cfg: &NebariConfig{
				ProjectName: "CHANGEME",
				Cluster:     clusterFor("aws", map[string]any{"region": "us-west-2"}),
			},
			wantField: "project_name",
		},
		// --- placeholder nested inside the provider block, per provider ---
		{
			name: "aws placeholder nested in provider block",
			cfg: &NebariConfig{
				ProjectName: "my-nebari",
				Cluster:     clusterFor("aws", map[string]any{"region": "CHANGEME"}),
			},
			wantField: "cluster.aws.region",
		},
		{
			name: "gcp placeholder nested in provider block",
			cfg: &NebariConfig{
				ProjectName: "my-nebari",
				Cluster:     clusterFor("gcp", map[string]any{"project": "CHANGEME"}),
			},
			wantField: "cluster.gcp.project",
		},
		{
			name: "azure placeholder nested in provider block",
			cfg: &NebariConfig{
				ProjectName: "my-nebari",
				Cluster:     clusterFor("azure", map[string]any{"resource_group": "CHANGEME"}),
			},
			wantField: "cluster.azure.resource_group",
		},
		{
			name: "hetzner placeholder nested in provider block",
			cfg: &NebariConfig{
				ProjectName: "my-nebari",
				Cluster:     clusterFor("hetzner", map[string]any{"location": "CHANGEME"}),
			},
			wantField: "cluster.hetzner.location",
		},
		{
			name: "local placeholder nested in provider block",
			cfg: &NebariConfig{
				ProjectName: "my-nebari",
				Cluster:     clusterFor("local", map[string]any{"kind_image": "CHANGEME"}),
			},
			wantField: "cluster.local.kind_image",
		},
		{
			name: "existing placeholder nested in provider block",
			cfg: &NebariConfig{
				ProjectName: "my-nebari",
				Cluster:     clusterFor("existing", map[string]any{"kubeconfig": "CHANGEME"}),
			},
			wantField: "cluster.existing.kubeconfig",
		},
		// --- placeholder contained within a larger string ---
		{
			name: "placeholder as substring in domain",
			cfg: &NebariConfig{
				ProjectName: "my-nebari",
				Domain:      "CHANGEME.example.com",
				Cluster:     clusterFor("aws", map[string]any{"region": "us-west-2"}),
			},
			wantField: "domain",
		},
		// --- placeholder in a typed nested pointer struct ---
		{
			name: "placeholder in certificate acme email",
			cfg: &NebariConfig{
				ProjectName: "my-nebari",
				Cluster:     clusterFor("aws", map[string]any{"region": "us-west-2"}),
				Certificate: &CertificateConfig{
					Type: CertificateTypeLetsEncrypt,
					ACME: &ACMEConfig{Email: "CHANGEME@example.com"},
				},
			},
			wantField: "certificate.acme.email",
		},
		// --- legitimately-filled configs that must PASS, per provider ---
		{
			name: "aws filled config passes",
			cfg: &NebariConfig{
				ProjectName: "my-nebari-aws",
				Domain:      "nebari.example.com",
				Cluster:     clusterFor("aws", map[string]any{"region": "us-west-2"}),
			},
			wantOK: true,
		},
		{
			name: "gcp filled config passes",
			cfg: &NebariConfig{
				ProjectName: "my-nebari-gcp",
				Cluster:     clusterFor("gcp", map[string]any{"project": "my-gcp-project"}),
			},
			wantOK: true,
		},
		{
			name: "azure filled config passes",
			cfg: &NebariConfig{
				ProjectName: "my-nebari-azure",
				Cluster:     clusterFor("azure", map[string]any{"resource_group": "my-rg"}),
			},
			wantOK: true,
		},
		{
			name: "hetzner filled config passes",
			cfg: &NebariConfig{
				ProjectName: "my-nebari-hetzner",
				Cluster:     clusterFor("hetzner", map[string]any{"location": "ash"}),
			},
			wantOK: true,
		},
		{
			name: "local filled config passes",
			cfg: &NebariConfig{
				ProjectName: "my-nebari-local",
				Cluster:     clusterFor("local", nil),
			},
			wantOK: true,
		},
		{
			name: "existing filled config passes",
			cfg: &NebariConfig{
				ProjectName: "my-nebari-existing",
				Cluster:     clusterFor("existing", map[string]any{"kubeconfig": "/home/user/.kube/config"}),
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate(validateOpts())

			if tt.wantOK {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}

			var placeholderErr *PlaceholderError
			if !errors.As(err, &placeholderErr) {
				t.Fatalf("Validate() = %v, want *PlaceholderError", err)
			}
			if placeholderErr.FieldPath != tt.wantField {
				t.Errorf("PlaceholderError.FieldPath = %q, want %q", placeholderErr.FieldPath, tt.wantField)
			}
		})
	}
}
