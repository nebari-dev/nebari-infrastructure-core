package argocd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// genTestCert returns PEM-encoded cert and key for the given SANs.
func genTestCert(t *testing.T, sans ...string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     sans,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func TestMissingSANs(t *testing.T) {
	tests := []struct {
		name    string
		sans    []string
		domain  string
		want    []string
		wantErr bool
	}{
		{
			name:   "all present",
			sans:   []string{"example.com", "keycloak.example.com", "argocd.example.com"},
			domain: "example.com",
			want:   nil,
		},
		{
			name:   "wildcard plus apex covers all",
			sans:   []string{"example.com", "*.example.com"},
			domain: "example.com",
			want:   nil,
		},
		{
			name:   "missing keycloak and argocd",
			sans:   []string{"example.com"},
			domain: "example.com",
			want:   []string{"keycloak.example.com", "argocd.example.com"},
		},
		{
			name:    "unparseable cert",
			domain:  "example.com",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var certPEM []byte
			if tt.wantErr {
				certPEM = []byte("not a pem")
			} else {
				certPEM, _ = genTestCert(t, tt.sans...)
			}
			got, err := missingSANs(certPEM, tt.domain)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("missingSANs() error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("missingSANs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("missingSANs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestConfigureGatewayTLS_FilesSource(t *testing.T) {
	ctx := context.Background()
	certPEM, keyPEM := genTestCert(t, "example.com", "keycloak.example.com", "argocd.example.com")

	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.NebariConfig{
		Domain: "example.com",
		Certificate: &config.CertificateConfig{
			Type:  "existing",
			Files: &config.CertFiles{CertFile: certPath, KeyFile: keyPath},
		},
	}

	client := fake.NewSimpleClientset() //nolint:staticcheck // SA1019: fine for tests
	if err := ConfigureGatewayTLS(ctx, client, cfg); err != nil {
		t.Fatalf("ConfigureGatewayTLS() error: %v", err)
	}

	secret, err := client.CoreV1().Secrets("envoy-gateway-system").Get(ctx, "nebari-gateway-tls", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected secret to be created: %v", err)
	}
	if secret.Type != corev1.SecretTypeTLS {
		t.Errorf("secret type = %q, want %q", secret.Type, corev1.SecretTypeTLS)
	}
	if string(secret.Data["tls.crt"]) != string(certPEM) {
		t.Error("tls.crt mismatch")
	}
	if string(secret.Data["tls.key"]) != string(keyPEM) {
		t.Error("tls.key mismatch")
	}
}

func TestConfigureGatewayTLS_EnvSource_CustomName(t *testing.T) {
	ctx := context.Background()
	certPEM, keyPEM := genTestCert(t, "example.com")

	t.Setenv("TEST_TLS_CERT", string(certPEM))
	t.Setenv("TEST_TLS_KEY", string(keyPEM))

	cfg := &config.NebariConfig{
		Domain: "example.com",
		Certificate: &config.CertificateConfig{
			Type:       "existing",
			SecretName: "custom-tls",
			Env:        &config.CertEnv{CertEnv: "TEST_TLS_CERT", KeyEnv: "TEST_TLS_KEY"},
		},
	}

	client := fake.NewSimpleClientset() //nolint:staticcheck // SA1019: fine for tests
	if err := ConfigureGatewayTLS(ctx, client, cfg); err != nil {
		t.Fatalf("ConfigureGatewayTLS() error: %v", err)
	}

	if _, err := client.CoreV1().Secrets("envoy-gateway-system").Get(ctx, "custom-tls", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected secret custom-tls to be created: %v", err)
	}
}

func TestConfigureGatewayTLS_Idempotent(t *testing.T) {
	ctx := context.Background()
	certPEM, keyPEM := genTestCert(t, "example.com")
	t.Setenv("TEST_TLS_CERT", string(certPEM))
	t.Setenv("TEST_TLS_KEY", string(keyPEM))

	cfg := &config.NebariConfig{
		Domain: "example.com",
		Certificate: &config.CertificateConfig{
			Type: "existing",
			Env:  &config.CertEnv{CertEnv: "TEST_TLS_CERT", KeyEnv: "TEST_TLS_KEY"},
		},
	}
	client := fake.NewSimpleClientset() //nolint:staticcheck // SA1019: fine for tests
	if err := ConfigureGatewayTLS(ctx, client, cfg); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	// Second call should update without error.
	if err := ConfigureGatewayTLS(ctx, client, cfg); err != nil {
		t.Fatalf("second call error: %v", err)
	}
}

func TestConfigureGatewayTLS_MissingFile(t *testing.T) {
	ctx := context.Background()
	cfg := &config.NebariConfig{
		Domain: "example.com",
		Certificate: &config.CertificateConfig{
			Type:  "existing",
			Files: &config.CertFiles{CertFile: "/nonexistent/tls.crt", KeyFile: "/nonexistent/tls.key"},
		},
	}
	client := fake.NewSimpleClientset() //nolint:staticcheck // SA1019: fine for tests
	if err := ConfigureGatewayTLS(ctx, client, cfg); err == nil {
		t.Fatal("expected error for missing cert file, got nil")
	}
}

func TestConfigureGatewayTLS_NonExistingTypeIsNoop(t *testing.T) {
	ctx := context.Background()
	cfg := &config.NebariConfig{Domain: "example.com", Certificate: &config.CertificateConfig{Type: "selfsigned"}}
	client := fake.NewSimpleClientset() //nolint:staticcheck // SA1019: fine for tests
	if err := ConfigureGatewayTLS(ctx, client, cfg); err != nil {
		t.Fatalf("ConfigureGatewayTLS() should be a no-op for selfsigned, got: %v", err)
	}
	secrets, _ := client.CoreV1().Secrets("envoy-gateway-system").List(ctx, metav1.ListOptions{})
	if len(secrets.Items) != 0 {
		t.Errorf("expected no secrets created for non-existing type, got %d", len(secrets.Items))
	}
}

func TestPreflightGatewayTLS(t *testing.T) {
	certPEM, keyPEM := genTestCert(t, "example.com")
	otherKeyPEM := func() []byte {
		_, k := genTestCert(t, "other.com")
		return k
	}()

	dir := t.TempDir()
	goodCert := filepath.Join(dir, "tls.crt")
	goodKey := filepath.Join(dir, "tls.key")
	mismatchKey := filepath.Join(dir, "other.key")
	for path, data := range map[string][]byte{goodCert: certPEM, goodKey: keyPEM, mismatchKey: otherKeyPEM} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name    string
		cfg     *config.CertificateConfig
		setEnv  map[string]string
		wantErr bool
	}{
		{name: "nil certificate", cfg: nil},
		{name: "non-existing type", cfg: &config.CertificateConfig{Type: "selfsigned"}},
		{name: "existing_secret deferred to cluster", cfg: &config.CertificateConfig{Type: "existing", ExistingSecret: &config.ExistingSecretRef{Name: "x"}}},
		{
			name: "valid files",
			cfg:  &config.CertificateConfig{Type: "existing", Files: &config.CertFiles{CertFile: goodCert, KeyFile: goodKey}},
		},
		{
			name:    "missing file",
			cfg:     &config.CertificateConfig{Type: "existing", Files: &config.CertFiles{CertFile: "/nope.crt", KeyFile: "/nope.key"}},
			wantErr: true,
		},
		{
			name:    "mismatched keypair",
			cfg:     &config.CertificateConfig{Type: "existing", Files: &config.CertFiles{CertFile: goodCert, KeyFile: mismatchKey}},
			wantErr: true,
		},
		{
			name:    "unset env",
			cfg:     &config.CertificateConfig{Type: "existing", Env: &config.CertEnv{CertEnv: "PREFLIGHT_UNSET_CERT", KeyEnv: "PREFLIGHT_UNSET_KEY"}},
			wantErr: true,
		},
		{
			name:   "valid env",
			cfg:    &config.CertificateConfig{Type: "existing", Env: &config.CertEnv{CertEnv: "PREFLIGHT_CERT", KeyEnv: "PREFLIGHT_KEY"}},
			setEnv: map[string]string{"PREFLIGHT_CERT": string(certPEM), "PREFLIGHT_KEY": string(keyPEM)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.setEnv {
				t.Setenv(k, v)
			}
			cfg := &config.NebariConfig{Domain: "example.com", Certificate: tt.cfg}
			err := PreflightGatewayTLS(cfg)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigureGatewayTLS_ExistingSecretSANCheck(t *testing.T) {
	ctx := context.Background()
	certPEM, _ := genTestCert(t, "example.com", "keycloak.example.com", "argocd.example.com")

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "user-tls", Namespace: "envoy-gateway-system"},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": []byte("ignored")},
	}
	cfg := &config.NebariConfig{
		Domain: "example.com",
		Certificate: &config.CertificateConfig{
			Type:           "existing",
			ExistingSecret: &config.ExistingSecretRef{Name: "user-tls"},
		},
	}
	client := fake.NewSimpleClientset(existing) //nolint:staticcheck // SA1019: fine for tests
	// existing_secret path must not error and must not create/overwrite a secret.
	if err := ConfigureGatewayTLS(ctx, client, cfg); err != nil {
		t.Fatalf("ConfigureGatewayTLS() error: %v", err)
	}
}
