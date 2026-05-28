package aws

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const samplePEM = `-----BEGIN CERTIFICATE-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0123456789==
-----END CERTIFICATE-----
`

func TestTrustBundleResolveBase64(t *testing.T) {
	tmp := t.TempDir()
	pemPath := filepath.Join(tmp, "ca.pem")
	if err := os.WriteFile(pemPath, []byte(samplePEM), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tests := []struct {
		name      string
		bundle    *TrustBundleConfig
		wantEmpty bool
		wantErr   string
	}{
		{
			name:      "nil bundle is empty no-op",
			bundle:    nil,
			wantEmpty: true,
		},
		{
			name:      "empty struct is no-op",
			bundle:    &TrustBundleConfig{},
			wantEmpty: true,
		},
		{
			name:    "both path and inline is an error",
			bundle:  &TrustBundleConfig{Path: pemPath, Inline: samplePEM},
			wantErr: "only one of path or inline",
		},
		{
			name:   "inline PEM is base64-encoded verbatim",
			bundle: &TrustBundleConfig{Inline: samplePEM},
		},
		{
			name:   "path is read from disk",
			bundle: &TrustBundleConfig{Path: pemPath},
		},
		{
			name:    "missing path returns a clear error",
			bundle:  &TrustBundleConfig{Path: filepath.Join(tmp, "does-not-exist.pem")},
			wantErr: "read",
		},
		{
			name:    "inline without a PEM block is rejected",
			bundle:  &TrustBundleConfig{Inline: "not a certificate"},
			wantErr: "no PEM certificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.bundle.ResolveBase64()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty result, got %q", got)
				}
				return
			}
			decoded, err := base64.StdEncoding.DecodeString(got)
			if err != nil {
				t.Fatalf("ResolveBase64 returned a non-base64 value: %v", err)
			}
			if !strings.Contains(string(decoded), "BEGIN CERTIFICATE") {
				t.Errorf("decoded payload did not contain a PEM certificate; got %q", decoded)
			}
		})
	}
}

func TestToTFVarsExtraCABundle(t *testing.T) {
	tests := []struct {
		name        string
		bundle      *TrustBundleConfig
		wantInJSON  bool
		wantDecoded string
	}{
		{
			name:       "unset bundle is omitted from tfvars",
			bundle:     nil,
			wantInJSON: false,
		},
		{
			name:        "inline bundle ends up base64 in tfvars",
			bundle:      &TrustBundleConfig{Inline: samplePEM},
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
				TrustBundle:       tt.bundle,
			}
			vars, err := cfg.toTFVars("test-project")
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
