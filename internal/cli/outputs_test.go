package cli

import (
	"encoding/json"
	"slices"
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

func TestValidateOutputsFlags(t *testing.T) {
	tests := []struct {
		name           string
		format         string
		wait           bool
		timeoutChanged bool
		errContains    string
	}{
		{
			name:   "table format",
			format: formatTable,
		},
		{
			name:   "json format",
			format: formatJSON,
		},
		{
			// Caught before any cluster work, so a typo does not surface only
			// after --wait has spent the whole polling window.
			name:        "unsupported format",
			format:      "yaml",
			errContains: `unsupported output format "yaml"`,
		},
		{
			name:           "timeout without wait",
			format:         formatTable,
			timeoutChanged: true,
			errContains:    "--timeout has no effect without --wait",
		},
		{
			name:           "timeout with wait",
			format:         formatTable,
			wait:           true,
			timeoutChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOutputsFlags(tt.format, tt.wait, tt.timeoutChanged)

			if tt.errContains != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestOutputFieldsMatchJSONPayload ties the descriptor table to the JSON
// schema. Without it, a field added to one and forgotten in the other would
// render a table row that never appears in --format json (or the reverse) -
// exactly the silent divergence this command exists to eliminate downstream.
func TestOutputFieldsMatchJSONPayload(t *testing.T) {
	encoded, err := formatOutputs(sampleOutputs(), formatJSON, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}

	for _, field := range outputFields {
		if _, ok := payload[field.key]; !ok {
			t.Errorf("field %q is in outputFields but absent from --format json", field.key)
		}
	}

	described := make(map[string]bool, len(outputFields))
	for _, field := range outputFields {
		described[field.key] = true
	}
	for key := range payload {
		if key == "redacted" {
			continue
		}
		if !described[key] {
			t.Errorf("field %q is in --format json but absent from outputFields", key)
		}
	}
}

// TestRedactedListMatchesSecretFields pins the redacted list to the fields the
// table actually withholds, so a new secret field cannot be reported in the
// clear under --format json while the table redacts it.
func TestRedactedListMatchesSecretFields(t *testing.T) {
	encoded, err := formatOutputs(sampleOutputs(), formatJSON, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload struct {
		Redacted []string `json:"redacted"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}

	var wantSecret []string
	for _, field := range outputFields {
		if field.secret {
			wantSecret = append(wantSecret, field.key)
		}
	}

	if len(payload.Redacted) != len(wantSecret) {
		t.Fatalf("redacted = %v, want the %d secret fields %v", payload.Redacted, len(wantSecret), wantSecret)
	}
	for _, key := range wantSecret {
		if !slices.Contains(payload.Redacted, key) {
			t.Errorf("secret field %q is not listed under \"redacted\"", key)
		}
	}
}

// TestRedactedOutputNeverContainsSecretValues drives its assertions off
// outputFields rather than a hardcoded list, so a secret field added later is
// covered automatically instead of needing to be remembered here. The failure
// it guards against is the worst kind: the JSON payload's "redacted" list is
// derived from outputFields, but the pointer-nulling in marshalOutputsJSON is
// not, so a new secret could be advertised as withheld while its value was
// emitted in the clear.
func TestRedactedOutputNeverContainsSecretValues(t *testing.T) {
	sample := sampleOutputs()

	for _, format := range []string{formatTable, formatJSON} {
		t.Run(format, func(t *testing.T) {
			rendered, err := formatOutputs(sample, format, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, field := range outputFields {
				if !field.secret {
					continue
				}

				value := field.value(sample)
				if value == "" {
					t.Fatalf("secret field %q has no value in sampleOutputs: populate one, or this test cannot prove the value is withheld",
						field.key)
				}
				if strings.Contains(string(rendered), value) {
					t.Errorf("redacted %s output contains the value of secret field %q", format, field.key)
				}
			}
		})
	}
}
