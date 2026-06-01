package aws

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const samplePEM = `-----BEGIN CERTIFICATE-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0123456789==
-----END CERTIFICATE-----
`

const altPEM = `-----BEGIN CERTIFICATE-----
ZZZZZZZZZZ9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0123456789==
-----END CERTIFICATE-----
`

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// TestToTFVarsExtraCABundle covers the precedence between the deprecated
// provider-scoped trust_bundle and the top-level bundle passed as a fallback.
func TestToTFVarsExtraCABundle(t *testing.T) {
	tests := []struct {
		name        string
		bundle      *TrustBundleConfig
		fallback    string
		wantInJSON  bool
		wantDecoded string
	}{
		{
			name:       "neither set is omitted from tfvars",
			wantInJSON: false,
		},
		{
			name:        "provider-scoped inline ends up base64 in tfvars",
			bundle:      &TrustBundleConfig{Inline: samplePEM},
			wantInJSON:  true,
			wantDecoded: samplePEM,
		},
		{
			name:        "top-level fallback is used when provider-scoped is unset",
			fallback:    b64(samplePEM),
			wantInJSON:  true,
			wantDecoded: samplePEM,
		},
		{
			name:        "provider-scoped takes precedence over fallback",
			bundle:      &TrustBundleConfig{Inline: altPEM},
			fallback:    b64(samplePEM),
			wantInJSON:  true,
			wantDecoded: altPEM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Region:            "us-west-2",
				KubernetesVersion: "1.33",
				NodeGroups:        map[string]NodeGroup{"general": {Instance: "m5.xlarge"}},
				TrustBundle:       tt.bundle,
			}
			vars, err := cfg.toTFVars("test-project", tt.fallback)
			if err != nil {
				t.Fatalf("toTFVars: %v", err)
			}

			raw, err := json.Marshal(vars)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			hasField := strings.Contains(string(raw), `"extra_ca_bundle"`)
			if hasField != tt.wantInJSON {
				t.Fatalf("extra_ca_bundle present=%v, want %v\n%s", hasField, tt.wantInJSON, raw)
			}

			if !tt.wantInJSON {
				if vars.ExtraCABundle != nil {
					t.Errorf("expected ExtraCABundle nil, got %q", *vars.ExtraCABundle)
				}
				return
			}
			if vars.ExtraCABundle == nil {
				t.Fatal("expected ExtraCABundle non-nil")
			}
			decoded, err := base64.StdEncoding.DecodeString(*vars.ExtraCABundle)
			if err != nil {
				t.Fatalf("ExtraCABundle is not valid base64: %v", err)
			}
			if string(decoded) != tt.wantDecoded {
				t.Errorf("decoded extra_ca_bundle mismatch:\n got: %q\nwant: %q", decoded, tt.wantDecoded)
			}
		})
	}
}
