package argocd

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestApplications(t *testing.T) {
	apps, err := Applications()
	if err != nil {
		t.Fatalf("Applications() error: %v", err)
	}

	// Should not include _example.yaml (underscore prefix)
	for _, app := range apps {
		if strings.HasPrefix(app, "_") {
			t.Errorf("Applications() included underscore-prefixed file: %s", app)
		}
	}
}

func TestWriteApplication_CertManager(t *testing.T) {
	// Test reading an actual application template
	var buf bytes.Buffer
	ctx := context.Background()

	err := WriteApplication(ctx, &buf, "cert-manager")
	if err != nil {
		t.Fatalf("WriteApplication(cert-manager) error: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, "kind: Application") {
		t.Error("expected manifest to contain 'kind: Application'")
	}
	if !strings.Contains(content, "apiVersion: argoproj.io/v1alpha1") {
		t.Error("expected manifest to contain ArgoCD API version")
	}
}

func TestWriteApplication_NotFound(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := WriteApplication(ctx, &buf, "nonexistent-app")
	if err == nil {
		t.Error("WriteApplication(nonexistent-app) should return error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestWriteAll(t *testing.T) {
	ctx := context.Background()

	// Track what gets written
	written := make(map[string]*bytes.Buffer)
	err := WriteAll(ctx, func(appName string) (io.WriteCloser, error) {
		buf := &bytes.Buffer{}
		written[appName] = buf
		return &nopWriteCloser{buf}, nil
	})

	if err != nil {
		t.Fatalf("WriteAll() error: %v", err)
	}

	// Verify we wrote the expected applications
	apps, err := Applications()
	if err != nil {
		t.Fatalf("Applications() error: %v", err)
	}

	if len(written) != len(apps) {
		t.Errorf("WriteAll wrote %d apps, expected %d", len(written), len(apps))
	}

	// Verify each app was written with valid content
	for _, appName := range apps {
		buf, ok := written[appName]
		if !ok {
			t.Errorf("Application %q was not written", appName)
			continue
		}
		content := buf.String()
		if !strings.Contains(content, "kind: Application") {
			t.Errorf("Application %q missing 'kind: Application'", appName)
		}
		if !strings.Contains(content, appName) {
			t.Errorf("Application %q content doesn't contain app name", appName)
		}
	}
}

func TestStorageClassForProvider(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"aws", "gp2"},
		{"gcp", "standard-rwo"},
		{"azure", "managed-csi"},
		{"local", "standard"},
		{"unknown", "standard"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := storageClassForProvider(tt.provider)
			if got != tt.want {
				t.Errorf("storageClassForProvider(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestNeedsMetalLB(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.NebariConfig
		want bool
	}{
		{
			name: "local provider needs MetalLB",
			cfg:  &config.NebariConfig{Provider: "local"},
			want: true,
		},
		{
			name: "aws provider does not need MetalLB",
			cfg:  &config.NebariConfig{Provider: "aws"},
			want: false,
		},
		{
			name: "local provider with DisableMetalLB",
			cfg:  &config.NebariConfig{Provider: "local", DisableMetalLB: true},
			want: false,
		},
		{
			name: "aws provider with DisableMetalLB",
			cfg:  &config.NebariConfig{Provider: "aws", DisableMetalLB: true},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsMetalLB(tt.cfg)
			if got != tt.want {
				t.Errorf("needsMetalLB() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTemplateData_StorageClassOverride(t *testing.T) {
	// Default: uses provider's storage class
	cfg := &config.NebariConfig{
		Provider: "local",
		Domain:   "test.example.com",
	}
	data := NewTemplateData(cfg)
	if data.StorageClass != "standard" {
		t.Errorf("expected default storage class 'standard', got %q", data.StorageClass)
	}

	// Override: uses custom storage class
	cfg.StorageClass = "hcloud-volumes"
	data = NewTemplateData(cfg)
	if data.StorageClass != "hcloud-volumes" {
		t.Errorf("expected overridden storage class 'hcloud-volumes', got %q", data.StorageClass)
	}
}

// nopWriteCloser wraps a bytes.Buffer to satisfy io.WriteCloser
type nopWriteCloser struct {
	*bytes.Buffer
}

func (n *nopWriteCloser) Close() error {
	return nil
}
