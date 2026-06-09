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

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// TestToTFVarsExtraCABundle covers how the top-level trust bundle resolved by
// the orchestration layer is threaded into the extra_ca_bundle tfvar.
func TestToTFVarsExtraCABundle(t *testing.T) {
	tests := []struct {
		name        string
		caBundle    string
		wantInJSON  bool
		wantDecoded string
	}{
		{
			name:       "unset is omitted from tfvars",
			wantInJSON: false,
		},
		{
			name:        "resolved top-level bundle is passed through",
			caBundle:    b64(samplePEM),
			wantInJSON:  true,
			wantDecoded: samplePEM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Region:            "us-west-2",
				KubernetesVersion: "1.33",
				NodeGroups:        map[string]NodeGroup{"general": {Instance: "m5.xlarge"}},
			}
			vars := cfg.toTFVars("test-project", tt.caBundle)

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
