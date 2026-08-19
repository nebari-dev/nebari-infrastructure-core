package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

func sampleOutputs() *nic.PlatformOutputs {
	return &nic.PlatformOutputs{
		Domain:                     "nebari.example.com",
		KeycloakIssuerURL:          "https://keycloak.nebari.example.com",
		KeycloakAdminPassword:      "kc-admin-pw",
		KeycloakRealmAdminPassword: "realm-admin-pw",
		ArgoCDAdminPassword:        "argocd-admin-pw",
		GatewayAddress:             "10.89.0.2",
	}
}

func TestFormatOutputs(t *testing.T) {
	secretValues := []string{"kc-admin-pw", "realm-admin-pw", "argocd-admin-pw"}

	tests := []struct {
		name           string
		format         string
		showSecrets    bool
		wantContains   []string
		wantNoContains []string
		errContains    string
	}{
		{
			name:         "json with secrets emits the values",
			format:       formatJSON,
			showSecrets:  true,
			wantContains: append([]string{`"domain": "nebari.example.com"`}, secretValues...),
			// A "redacted" list would mislead a consumer into thinking
			// something was withheld.
			wantNoContains: []string{"redacted"},
		},
		{
			name:        "json without secrets nulls the values and lists them as redacted",
			format:      formatJSON,
			showSecrets: false,
			wantContains: []string{
				`"keycloak_admin_password": null`,
				`"redacted"`,
				"keycloak_realm_admin_password",
			},
			wantNoContains: secretValues,
		},
		{
			name:         "json always emits the non-secret fields",
			format:       formatJSON,
			showSecrets:  false,
			wantContains: []string{`"gateway_address": "10.89.0.2"`, `"keycloak_issuer_url": "https://keycloak.nebari.example.com"`},
		},
		{
			name:           "table without secrets shows how to reveal them",
			format:         formatTable,
			showSecrets:    false,
			wantContains:   []string{"nebari.example.com", "--show-secrets", "10.89.0.2"},
			wantNoContains: secretValues,
		},
		{
			name:         "table with secrets emits the values",
			format:       formatTable,
			showSecrets:  true,
			wantContains: append([]string{"nebari.example.com"}, secretValues...),
		},
		{
			name:        "rejects an unknown format",
			format:      "yaml",
			errContains: "unsupported output format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatOutputs(sampleOutputs(), tt.format, tt.showSecrets)

			if tt.errContains != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			out := string(got)
			for _, want := range tt.wantContains {
				if !strings.Contains(out, want) {
					t.Errorf("output does not contain %q:\n%s", want, out)
				}
			}
			for _, unwanted := range tt.wantNoContains {
				if strings.Contains(out, unwanted) {
					t.Errorf("output leaks %q:\n%s", unwanted, out)
				}
			}
		})
	}
}

// A consumer that forgets --show-secrets must get a structurally obvious
// signal, not a plausible-looking string that flows into a login attempt.
func TestFormatOutputsRedactedJSONDecodesToNull(t *testing.T) {
	got, err := formatOutputs(sampleOutputs(), formatJSON, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded struct {
		KeycloakAdminPassword *string  `json:"keycloak_admin_password"`
		Redacted              []string `json:"redacted"`
	}
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if decoded.KeycloakAdminPassword != nil {
		t.Errorf("keycloak_admin_password = %q, want null", *decoded.KeycloakAdminPassword)
	}
	want := []string{"argocd_admin_password", "keycloak_admin_password", "keycloak_realm_admin_password"}
	if len(decoded.Redacted) != len(want) {
		t.Fatalf("redacted = %v, want %v", decoded.Redacted, want)
	}
	for i, w := range want {
		if decoded.Redacted[i] != w {
			t.Errorf("redacted[%d] = %q, want %q", i, decoded.Redacted[i], w)
		}
	}
}
