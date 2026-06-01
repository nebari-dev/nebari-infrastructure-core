package config

import (
	"encoding/base64"
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
		{name: "nil bundle is empty no-op", bundle: nil, wantEmpty: true},
		{name: "empty struct is no-op", bundle: &TrustBundleConfig{}, wantEmpty: true},
		{name: "whitespace-only path is treated as unset", bundle: &TrustBundleConfig{Path: "   "}, wantEmpty: true},
		{name: "both path and inline is an error", bundle: &TrustBundleConfig{Path: pemPath, Inline: samplePEM}, wantErr: "only one of path or inline"},
		{name: "inline PEM is base64-encoded verbatim", bundle: &TrustBundleConfig{Inline: samplePEM}},
		{name: "path is read from disk", bundle: &TrustBundleConfig{Path: pemPath}},
		{name: "missing path returns a clear error", bundle: &TrustBundleConfig{Path: filepath.Join(tmp, "does-not-exist.pem")}, wantErr: "read"},
		{name: "inline without a PEM block is rejected", bundle: &TrustBundleConfig{Inline: "not a certificate"}, wantErr: "no PEM certificate"},
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

func TestTrustBundleResolvePEM(t *testing.T) {
	tests := []struct {
		name   string
		bundle *TrustBundleConfig
		want   string
	}{
		{name: "nil is empty", bundle: nil, want: ""},
		{name: "inline returns raw PEM", bundle: &TrustBundleConfig{Inline: samplePEM}, want: samplePEM},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.bundle.ResolvePEM()
			if err != nil {
				t.Fatalf("ResolvePEM: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolvePEM = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTrustBundleValidate(t *testing.T) {
	tests := []struct {
		name    string
		bundle  *TrustBundleConfig
		wantErr bool
	}{
		{name: "nil is valid", bundle: nil},
		{name: "valid inline", bundle: &TrustBundleConfig{Inline: samplePEM}},
		{name: "both set is invalid", bundle: &TrustBundleConfig{Path: "/tmp/x", Inline: samplePEM}, wantErr: true},
		{name: "junk inline is invalid", bundle: &TrustBundleConfig{Inline: "nope"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.bundle.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
