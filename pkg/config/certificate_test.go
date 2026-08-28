package config

import (
	"strings"
	"testing"
)

func TestCertificateConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *CertificateConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "nil receiver is valid",
			cfg:  nil,
		},
		{
			name: "empty type is valid",
			cfg:  &CertificateConfig{},
		},
		{
			name: "selfsigned is valid",
			cfg:  &CertificateConfig{Type: "selfsigned"},
		},
		{
			name: "letsencrypt is valid",
			cfg:  &CertificateConfig{Type: "letsencrypt", ACME: &ACMEConfig{Email: "a@b.com"}},
		},
		{
			name:        "letsencrypt without acme is rejected",
			cfg:         &CertificateConfig{Type: "letsencrypt"},
			wantErr:     true,
			errContains: "requires acme.email",
		},
		{
			name:        "letsencrypt without email is rejected",
			cfg:         &CertificateConfig{Type: "letsencrypt", ACME: &ACMEConfig{}},
			wantErr:     true,
			errContains: "requires acme.email",
		},
		{
			name:        "letsencrypt with blank email is rejected",
			cfg:         &CertificateConfig{Type: "letsencrypt", ACME: &ACMEConfig{Email: "  "}},
			wantErr:     true,
			errContains: "requires acme.email",
		},
		{
			name:        "unknown type rejected",
			cfg:         &CertificateConfig{Type: "bogus"},
			wantErr:     true,
			errContains: "invalid certificate type",
		},
		{
			name:        "existing with no source rejected",
			cfg:         &CertificateConfig{Type: "existing"},
			wantErr:     true,
			errContains: "exactly one of",
		},
		{
			name: "existing with multiple sources rejected",
			cfg: &CertificateConfig{
				Type:           "existing",
				ExistingSecret: &ExistingSecretRef{Name: "my-tls"},
				Files:          &CertFiles{CertFile: "/c", KeyFile: "/k"},
			},
			wantErr:     true,
			errContains: "exactly one of",
		},
		{
			name: "existing_secret without name rejected",
			cfg: &CertificateConfig{
				Type:           "existing",
				ExistingSecret: &ExistingSecretRef{},
			},
			wantErr:     true,
			errContains: "existing_secret.name",
		},
		{
			name: "existing_secret with name is valid",
			cfg: &CertificateConfig{
				Type:           "existing",
				ExistingSecret: &ExistingSecretRef{Name: "my-tls"},
			},
		},
		{
			name: "existing_secret cross-namespace is valid",
			cfg: &CertificateConfig{
				Type:           "existing",
				ExistingSecret: &ExistingSecretRef{Name: "my-tls", Namespace: "my-ns"},
			},
		},
		{
			name: "files missing key_file rejected",
			cfg: &CertificateConfig{
				Type:  "existing",
				Files: &CertFiles{CertFile: "/path/tls.crt"},
			},
			wantErr:     true,
			errContains: "files requires both",
		},
		{
			name: "files missing cert_file rejected",
			cfg: &CertificateConfig{
				Type:  "existing",
				Files: &CertFiles{KeyFile: "/path/tls.key"},
			},
			wantErr:     true,
			errContains: "files requires both",
		},
		{
			name: "files with both is valid",
			cfg: &CertificateConfig{
				Type:  "existing",
				Files: &CertFiles{CertFile: "/path/tls.crt", KeyFile: "/path/tls.key"},
			},
		},
		{
			name: "env missing key_env rejected",
			cfg: &CertificateConfig{
				Type: "existing",
				Env:  &CertEnv{CertEnv: "NEBARI_TLS_CERT"},
			},
			wantErr:     true,
			errContains: "env requires both",
		},
		{
			name: "env with both is valid",
			cfg: &CertificateConfig{
				Type: "existing",
				Env:  &CertEnv{CertEnv: "NEBARI_TLS_CERT", KeyEnv: "NEBARI_TLS_KEY"},
			},
		},
		{
			name: "secret_name combined with existing_secret rejected",
			cfg: &CertificateConfig{
				Type:           "existing",
				SecretName:     "custom-tls",
				ExistingSecret: &ExistingSecretRef{Name: "user-tls"},
			},
			wantErr:     true,
			errContains: "secret_name",
		},
		{
			name: "secret_name with files is valid",
			cfg: &CertificateConfig{
				Type:       "existing",
				SecretName: "custom-tls",
				Files:      &CertFiles{CertFile: "/c", KeyFile: "/k"},
			},
		},
		{
			name: "acme combined with existing rejected",
			cfg: &CertificateConfig{
				Type:           "existing",
				ExistingSecret: &ExistingSecretRef{Name: "my-tls"},
				ACME:           &ACMEConfig{Email: "a@b.com"},
			},
			wantErr:     true,
			errContains: "acme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() = nil, want error containing %q", tt.errContains)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestCertificateConfigParsing(t *testing.T) {
	yaml := []byte(`
project_name: test
cluster:
  aws:
    region: us-west-2
certificate:
  type: existing
  secret_name: my-gateway-tls
  existing_secret:
    name: user-tls
    namespace: user-ns
`)
	cfg, err := ParseConfigBytes(yaml)
	if err != nil {
		t.Fatalf("ParseConfigBytes() error: %v", err)
	}
	if cfg.Certificate == nil {
		t.Fatal("Certificate is nil")
	}
	if cfg.Certificate.Type != "existing" {
		t.Errorf("Type = %q, want existing", cfg.Certificate.Type)
	}
	if cfg.Certificate.SecretName != "my-gateway-tls" {
		t.Errorf("SecretName = %q, want my-gateway-tls", cfg.Certificate.SecretName)
	}
	if cfg.Certificate.ExistingSecret == nil {
		t.Fatal("ExistingSecret is nil")
	}
	if cfg.Certificate.ExistingSecret.Name != "user-tls" {
		t.Errorf("ExistingSecret.Name = %q, want user-tls", cfg.Certificate.ExistingSecret.Name)
	}
	if cfg.Certificate.ExistingSecret.Namespace != "user-ns" {
		t.Errorf("ExistingSecret.Namespace = %q, want user-ns", cfg.Certificate.ExistingSecret.Namespace)
	}
}

func TestCertificateConfigParsingFilesAndEnv(t *testing.T) {
	yaml := []byte(`
project_name: test
cluster:
  aws:
    region: us-west-2
certificate:
  type: existing
  files:
    cert_file: /path/to/tls.crt
    key_file: /path/to/tls.key
`)
	cfg, err := ParseConfigBytes(yaml)
	if err != nil {
		t.Fatalf("ParseConfigBytes() error: %v", err)
	}
	if cfg.Certificate.Files == nil {
		t.Fatal("Files is nil")
	}
	if cfg.Certificate.Files.CertFile != "/path/to/tls.crt" {
		t.Errorf("CertFile = %q", cfg.Certificate.Files.CertFile)
	}
	if cfg.Certificate.Files.KeyFile != "/path/to/tls.key" {
		t.Errorf("KeyFile = %q", cfg.Certificate.Files.KeyFile)
	}
}

func TestCertificateConfigResolvedSecretName(t *testing.T) {
	tests := []struct {
		name string
		cfg  *CertificateConfig
		want string
	}{
		{name: "nil defaults", cfg: nil, want: "nebari-gateway-tls"},
		{name: "empty defaults", cfg: &CertificateConfig{}, want: "nebari-gateway-tls"},
		{name: "override", cfg: &CertificateConfig{SecretName: "custom-tls"}, want: "custom-tls"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolvedSecretName(); got != tt.want {
				t.Errorf("ResolvedSecretName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCertificateConfigGatewaySecretRef(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *CertificateConfig
		wantName      string
		wantNamespace string
		wantCrossNS   bool
	}{
		{
			name:          "nil uses defaults",
			cfg:           nil,
			wantName:      "nebari-gateway-tls",
			wantNamespace: "envoy-gateway-system",
			wantCrossNS:   false,
		},
		{
			name:          "files source uses resolved secret name in default ns",
			cfg:           &CertificateConfig{Type: "existing", Files: &CertFiles{CertFile: "/c", KeyFile: "/k"}},
			wantName:      "nebari-gateway-tls",
			wantNamespace: "envoy-gateway-system",
			wantCrossNS:   false,
		},
		{
			name:          "files source honors custom secret name",
			cfg:           &CertificateConfig{Type: "existing", SecretName: "my-tls", Files: &CertFiles{CertFile: "/c", KeyFile: "/k"}},
			wantName:      "my-tls",
			wantNamespace: "envoy-gateway-system",
			wantCrossNS:   false,
		},
		{
			name:          "existing_secret same namespace",
			cfg:           &CertificateConfig{Type: "existing", ExistingSecret: &ExistingSecretRef{Name: "user-tls"}},
			wantName:      "user-tls",
			wantNamespace: "envoy-gateway-system",
			wantCrossNS:   false,
		},
		{
			name:          "existing_secret cross namespace",
			cfg:           &CertificateConfig{Type: "existing", ExistingSecret: &ExistingSecretRef{Name: "user-tls", Namespace: "user-ns"}},
			wantName:      "user-tls",
			wantNamespace: "user-ns",
			wantCrossNS:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, namespace := tt.cfg.GatewaySecretRef()
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if namespace != tt.wantNamespace {
				t.Errorf("namespace = %q, want %q", namespace, tt.wantNamespace)
			}
			if got := tt.cfg.IsCrossNamespaceSecret(); got != tt.wantCrossNS {
				t.Errorf("IsCrossNamespaceSecret() = %v, want %v", got, tt.wantCrossNS)
			}
		})
	}
}
