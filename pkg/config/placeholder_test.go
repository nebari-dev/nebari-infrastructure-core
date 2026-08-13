package config

import (
	"errors"
	"reflect"
	"testing"
)

// TestCheckPlaceholders drives the YAML node scan directly on raw config bytes.
// It covers, per provider, a placeholder in a required scalar and one nested in
// the provider block, plus the cases the node walk exists to handle that a
// struct walk could not: a placeholder in a mapping KEY, a CHANGEME inside a
// comment (must NOT trip), and multiple placeholders reported together.
func TestCheckPlaceholders(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantFields []string // nil => expect no error
	}{
		// --- placeholder in a required scalar ---
		{
			name: "aws placeholder in project_name",
			raw: `
project_name: CHANGEME
cluster:
  aws:
    region: us-west-2
`,
			wantFields: []string{"project_name"},
		},
		// --- placeholder nested inside the provider block, per provider ---
		{
			name: "aws placeholder nested in provider block",
			raw: `
project_name: my-nebari
cluster:
  aws:
    region: CHANGEME
`,
			wantFields: []string{"cluster.aws.region"},
		},
		{
			name: "gcp placeholder nested in provider block",
			raw: `
project_name: my-nebari
cluster:
  gcp:
    project: CHANGEME
`,
			wantFields: []string{"cluster.gcp.project"},
		},
		{
			name: "azure placeholder nested in provider block",
			raw: `
project_name: my-nebari
cluster:
  azure:
    resource_group: CHANGEME
`,
			wantFields: []string{"cluster.azure.resource_group"},
		},
		{
			name: "hetzner placeholder nested in provider block",
			raw: `
project_name: my-nebari
cluster:
  hetzner:
    location: CHANGEME
`,
			wantFields: []string{"cluster.hetzner.location"},
		},
		{
			name: "local placeholder nested in provider block",
			raw: `
project_name: my-nebari
cluster:
  local:
    kind_image: CHANGEME
`,
			wantFields: []string{"cluster.local.kind_image"},
		},
		{
			name: "existing placeholder nested in provider block",
			raw: `
project_name: my-nebari
cluster:
  existing:
    kubeconfig: CHANGEME
`,
			wantFields: []string{"cluster.existing.kubeconfig"},
		},
		// --- placeholder contained within a larger string ---
		{
			name: "placeholder as substring in domain",
			raw: `
project_name: my-nebari
domain: CHANGEME.example.com
cluster:
  aws:
    region: us-west-2
`,
			wantFields: []string{"domain"},
		},
		// --- placeholder in a typed nested block ---
		{
			name: "placeholder in certificate acme email",
			raw: `
project_name: my-nebari
cluster:
  aws:
    region: us-west-2
certificate:
  type: letsencrypt
  acme:
    email: CHANGEME@example.com
`,
			wantFields: []string{"certificate.acme.email"},
		},
		// --- placeholder inside a sequence element ---
		{
			name: "placeholder inside a list element",
			raw: `
project_name: my-nebari
cluster:
  aws:
    region: us-west-2
    availability_zones:
      - us-west-2a
      - CHANGEME
`,
			wantFields: []string{"cluster.aws.availability_zones[1]"},
		},
		// --- NEW: placeholder in a MAP KEY (a struct walk misses this) ---
		{
			name: "placeholder in a mapping key",
			raw: `
project_name: my-nebari
cluster:
  aws:
    region: us-west-2
    node_groups:
      CHANGEME:
        instance: m5.large
`,
			wantFields: []string{"cluster.aws.node_groups.CHANGEME"},
		},
		// --- NEW: CHANGEME inside a comment must NOT trip ---
		{
			name: "placeholder only in a comment passes",
			raw: `
# Remember to change this later: CHANGEME
project_name: my-nebari # CHANGEME once you pick a name
cluster:
  aws:
    region: us-west-2
`,
			wantFields: nil,
		},
		// --- NEW: multiple placeholders reported together ---
		{
			name: "multiple placeholders reported together",
			raw: `
project_name: CHANGEME
domain: CHANGEME.example.com
cluster:
  aws:
    region: CHANGEME
`,
			wantFields: []string{"cluster.aws.region", "domain", "project_name"},
		},
		// --- legitimately-filled config passes ---
		{
			name: "filled config passes",
			raw: `
project_name: my-nebari-aws
domain: nebari.example.com
cluster:
  aws:
    region: us-west-2
    node_groups:
      general:
        instance: m5.large
`,
			wantFields: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPlaceholders([]byte(tt.raw))

			if tt.wantFields == nil {
				if err != nil {
					t.Fatalf("CheckPlaceholders() = %v, want nil", err)
				}
				return
			}

			var placeholderErr *PlaceholderError
			if !errors.As(err, &placeholderErr) {
				t.Fatalf("CheckPlaceholders() = %v, want *PlaceholderError", err)
			}
			if !reflect.DeepEqual(placeholderErr.FieldPaths, tt.wantFields) {
				t.Errorf("FieldPaths = %v, want %v", placeholderErr.FieldPaths, tt.wantFields)
			}
		})
	}
}

// TestValidateDoesNotRejectPlaceholders proves the placeholder check was moved
// off NebariConfig.Validate: a config whose values contain CHANGEME still
// validates clean. This is what keeps destroy/kubeconfig — which only call
// cfg.Validate, never CheckPlaceholders — from rejecting an unedited config.
func TestValidateDoesNotRejectPlaceholders(t *testing.T) {
	cfg := &NebariConfig{
		ProjectName: "CHANGEME",
		Cluster:     &ClusterConfig{Providers: map[string]any{"aws": map[string]any{"region": "CHANGEME"}}},
	}
	opts := ValidateOptions{ClusterProviders: []string{"aws"}}
	if err := cfg.Validate(opts); err != nil {
		t.Fatalf("Validate() = %v, want nil (placeholder check must not gate Validate)", err)
	}
}

// TestPlaceholderErrorMessage checks the message lists a single field with the
// singular "field" and multiple fields with the plural "fields".
func TestPlaceholderErrorMessage(t *testing.T) {
	single := (&PlaceholderError{FieldPaths: []string{"project_name"}}).Error()
	if want := `field "project_name"`; !contains(single, want) {
		t.Errorf("single-field message %q does not contain %q", single, want)
	}
	multi := (&PlaceholderError{FieldPaths: []string{"domain", "project_name"}}).Error()
	if want := `fields "domain", "project_name"`; !contains(multi, want) {
		t.Errorf("multi-field message %q does not contain %q", multi, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
