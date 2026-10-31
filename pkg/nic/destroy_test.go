package nic

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// fakeClusterProvider is a minimal cluster.Provider whose Destroy returns a
// configurable error, for exercising Client.Destroy error handling.
type fakeClusterProvider struct {
	destroyErr  error
	destroyOpts cluster.DestroyOptions
}

func (f *fakeClusterProvider) Name() string { return "fake" }

func (f *fakeClusterProvider) Validate(context.Context, string, *config.ClusterConfig) error {
	return nil
}

func (f *fakeClusterProvider) Deploy(context.Context, string, *config.ClusterConfig, cluster.DeployOptions) error {
	return nil
}

func (f *fakeClusterProvider) Destroy(_ context.Context, _ string, _ *config.ClusterConfig, opts cluster.DestroyOptions) error {
	f.destroyOpts = opts
	return f.destroyErr
}

func (f *fakeClusterProvider) GetKubeconfig(context.Context, string, *config.ClusterConfig) ([]byte, error) {
	return nil, errors.New("no kubeconfig")
}

func (f *fakeClusterProvider) Summary(*config.ClusterConfig) map[string]string {
	return nil
}

func (f *fakeClusterProvider) InfraSettings(*config.ClusterConfig) cluster.InfraSettings {
	return cluster.InfraSettings{}
}

// TestDestroyForceStillReturnsError pins the fix for #534: --force keeps the
// teardown going past provider errors, but Destroy must still return a
// non-nil error at the end so the CLI exits non-zero instead of reporting a
// partial teardown as clean.
func TestDestroyForceStillReturnsError(t *testing.T) {
	tests := []struct {
		name       string
		destroyErr error
		force      bool
		wantErr    bool
	}{
		{
			name:       "provider error with force returns error",
			destroyErr: errors.New("VPC has dependencies"),
			force:      true,
			wantErr:    true,
		},
		{
			name:       "provider error without force returns error",
			destroyErr: errors.New("VPC has dependencies"),
			force:      false,
			wantErr:    true,
		},
		{
			name:       "clean destroy returns nil",
			destroyErr: nil,
			force:      true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			reg := registry.NewRegistry()
			if err := reg.ClusterProviders.Register(ctx, "fake", &fakeClusterProvider{destroyErr: tt.destroyErr}); err != nil {
				t.Fatalf("register fake cluster provider: %v", err)
			}
			client := &Client{registry: reg}

			cfg := &config.NebariConfig{
				ProjectName: "test-project",
				Cluster: &config.ClusterConfig{
					Providers: map[string]any{"fake": map[string]any{}},
				},
				Repository: &config.RepositoryConfig{
					Providers: map[string]any{"existing": map[string]any{}},
				},
			}

			err := client.Destroy(ctx, cfg, DestroyOptions{Force: tt.force})

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Destroy() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Destroy() expected error, got nil")
			}
			if !errors.Is(err, tt.destroyErr) {
				t.Errorf("Destroy() error = %v, want it to wrap %v", err, tt.destroyErr)
			}
		})
	}
}

// testCAPEM returns a freshly generated self-signed CA certificate as PEM, since
// trust-bundle resolution parses certificates for real.
func testCAPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Org Root CA"},
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

// TestDestroyTrustBundleFallback pins that a path-based trust bundle which is
// gone or no longer validates does not block teardown: the applied value
// already lives in Terraform state, so destroy continues with an empty bundle.
func TestDestroyTrustBundleFallback(t *testing.T) {
	tmp := t.TempDir()
	certPEM := testCAPEM(t)
	validPath := filepath.Join(tmp, "valid.pem")
	invalidPath := filepath.Join(tmp, "invalid.pem")
	if err := os.WriteFile(validPath, []byte(certPEM), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// A non-CERTIFICATE block alongside the cert: accepted by older nic
	// releases, rejected now because trust-manager rejects it.
	invalid := certPEM + "-----BEGIN EC PARAMETERS-----\nBggqhkjOPQMBBw==\n-----END EC PARAMETERS-----\n"
	if err := os.WriteFile(invalidPath, []byte(invalid), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "valid file is passed through", path: validPath, want: base64.StdEncoding.EncodeToString([]byte(certPEM))},
		{name: "missing file falls back to empty", path: filepath.Join(tmp, "gone.pem"), want: ""},
		{name: "invalid file falls back to empty", path: invalidPath, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			provider := &fakeClusterProvider{}
			reg := registry.NewRegistry()
			if err := reg.ClusterProviders.Register(ctx, "fake", provider); err != nil {
				t.Fatalf("register fake cluster provider: %v", err)
			}
			client := &Client{registry: reg}

			cfg := &config.NebariConfig{
				ProjectName: "test-project",
				Cluster: &config.ClusterConfig{
					Providers: map[string]any{"fake": map[string]any{}},
				},
				Repository: &config.RepositoryConfig{
					Providers: map[string]any{"existing": map[string]any{}},
				},
				TrustBundle: &config.TrustBundleConfig{Path: tt.path},
			}

			if err := client.Destroy(ctx, cfg, DestroyOptions{}); err != nil {
				t.Fatalf("Destroy() unexpected error: %v", err)
			}
			if provider.destroyOpts.TrustBundle != tt.want {
				t.Errorf("DestroyOptions.TrustBundle = %q, want %q", provider.destroyOpts.TrustBundle, tt.want)
			}
		})
	}
}
