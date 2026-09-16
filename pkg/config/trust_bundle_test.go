package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestCertPEM returns a freshly generated self-signed CA certificate as PEM.
// Trust-bundle validation parses certificates for real, so fixtures must be
// genuine DER, not a hand-typed base64 stub.
func newTestCertPEM(t *testing.T, cn string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// Well-formed PEM framing around a body that is not a DER certificate. This is
// what the old substring check let through and the x509 parse must reject.
const malformedBodyPEM = `-----BEGIN CERTIFICATE-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0123456789==
-----END CERTIFICATE-----
`

const samplePrivateKeyPEM = `-----BEGIN PRIVATE KEY-----
MIICfQIBADANBgkqhkiG9w0BAQEFAASCAmcwggJjAgEAAoGBAQ0123456789==
-----END PRIVATE KEY-----
`

func TestTrustBundleResolveBase64(t *testing.T) {
	certPEM := newTestCertPEM(t, "Test Org Root CA")
	tmp := t.TempDir()
	pemPath := filepath.Join(tmp, "ca.pem")
	if err := os.WriteFile(pemPath, []byte(certPEM), 0o600); err != nil {
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
		{name: "both path and inline is an error", bundle: &TrustBundleConfig{Path: pemPath, Inline: certPEM}, wantErr: "only one of path or inline"},
		{name: "inline PEM is base64-encoded verbatim", bundle: &TrustBundleConfig{Inline: certPEM}},
		{name: "path is read from disk", bundle: &TrustBundleConfig{Path: pemPath}},
		{name: "whitespace-padded path is trimmed and read", bundle: &TrustBundleConfig{Path: "  " + pemPath + "  "}},
		{name: "missing path returns a clear error", bundle: &TrustBundleConfig{Path: filepath.Join(tmp, "does-not-exist.pem")}, wantErr: "read"},
		{name: "inline without a PEM block is rejected", bundle: &TrustBundleConfig{Inline: "not a certificate"}, wantErr: "no PEM certificate"},
		{name: "inline cert bundled with a private key is rejected", bundle: &TrustBundleConfig{Inline: certPEM + samplePrivateKeyPEM}, wantErr: "private key"},
		{name: "PEM framing around a non-certificate body is rejected", bundle: &TrustBundleConfig{Inline: malformedBodyPEM}, wantErr: "not a valid X.509 certificate"},
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
			if string(decoded) != certPEM {
				t.Errorf("decoded payload should be the PEM verbatim; got %q", decoded)
			}
		})
	}
}

func TestTrustBundleResolvePEM(t *testing.T) {
	certPEM := newTestCertPEM(t, "Test Org Root CA")
	tests := []struct {
		name   string
		bundle *TrustBundleConfig
		want   string
	}{
		{name: "nil is empty", bundle: nil, want: ""},
		{name: "inline returns raw PEM", bundle: &TrustBundleConfig{Inline: certPEM}, want: certPEM},
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

// TestTrustBundleResolve covers the structured result: where the bundle came
// from and how many certificates it carries, which deploy surfaces to operators.
func TestTrustBundleResolve(t *testing.T) {
	root := newTestCertPEM(t, "Test Org Root CA")
	intermediate := newTestCertPEM(t, "Test Org Issuing CA")
	tmp := t.TempDir()
	pemPath := filepath.Join(tmp, "ca.pem")
	if err := os.WriteFile(pemPath, []byte(root+intermediate), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tests := []struct {
		name       string
		bundle     *TrustBundleConfig
		wantSource string
		wantCerts  int
		wantPEM    string
		wantErr    string
	}{
		{name: "nil is an unset result", bundle: nil},
		{name: "empty struct is an unset result", bundle: &TrustBundleConfig{}},
		{name: "inline single cert", bundle: &TrustBundleConfig{Inline: root}, wantSource: "inline", wantCerts: 1, wantPEM: root},
		{name: "path with two certs", bundle: &TrustBundleConfig{Path: pemPath}, wantSource: pemPath, wantCerts: 2, wantPEM: root + intermediate},
		// Bundles exported by tooling often interleave human-readable subject
		// lines between blocks; pem.Decode skips them and so must we.
		{name: "text between blocks is tolerated", bundle: &TrustBundleConfig{Inline: "subject=CN=Root\n" + root + "subject=CN=Issuing\n" + intermediate}, wantSource: "inline", wantCerts: 2, wantPEM: "subject=CN=Root\n" + root + "subject=CN=Issuing\n" + intermediate},
		{name: "one bad cert among good ones fails the whole bundle", bundle: &TrustBundleConfig{Inline: root + malformedBodyPEM}, wantErr: "not a valid X.509 certificate"},
		{name: "DER (non-PEM) input is rejected", bundle: &TrustBundleConfig{Inline: "\x30\x82\x01\x0a"}, wantErr: "no PEM certificate"},
		{name: "non-certificate blocks alone do not count", bundle: &TrustBundleConfig{Inline: "-----BEGIN EC PARAMETERS-----\nBggqhkjOPQMBBw==\n-----END EC PARAMETERS-----\n"}, wantErr: "no PEM certificate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.bundle.Resolve()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tt.wantSource)
			}
			if got.Certificates != tt.wantCerts {
				t.Errorf("Certificates = %d, want %d", got.Certificates, tt.wantCerts)
			}
			if got.PEM != tt.wantPEM {
				t.Errorf("PEM = %q, want %q", got.PEM, tt.wantPEM)
			}
			if got.IsSet() != (tt.wantPEM != "") {
				t.Errorf("IsSet() = %v, want %v", got.IsSet(), tt.wantPEM != "")
			}
		})
	}
}

func TestTrustBundleValidate(t *testing.T) {
	certPEM := newTestCertPEM(t, "Test Org Root CA")
	tests := []struct {
		name    string
		bundle  *TrustBundleConfig
		wantErr bool
	}{
		{name: "nil is valid", bundle: nil},
		{name: "valid inline", bundle: &TrustBundleConfig{Inline: certPEM}},
		{name: "both set is invalid", bundle: &TrustBundleConfig{Path: "/tmp/x", Inline: certPEM}, wantErr: true},
		{name: "junk inline is invalid", bundle: &TrustBundleConfig{Inline: "nope"}, wantErr: true},
		{name: "inline with a private key is invalid", bundle: &TrustBundleConfig{Inline: certPEM + samplePrivateKeyPEM}, wantErr: true},
		{name: "inline with PEM framing but a non-certificate body is invalid", bundle: &TrustBundleConfig{Inline: malformedBodyPEM}, wantErr: true},
		// Structural-only: a missing path is valid at Validate time. The file read
		// (and any resulting error) is deferred to resolve time so `nic validate`
		// stays environment-independent.
		{name: "missing path is structurally valid", bundle: &TrustBundleConfig{Path: "/does/not/exist.pem"}},
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
