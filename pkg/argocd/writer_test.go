package argocd

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
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

// nopWriteCloser wraps a bytes.Buffer to satisfy io.WriteCloser
type nopWriteCloser struct {
	*bytes.Buffer
}

func (n *nopWriteCloser) Close() error {
	return nil
}

func TestSyncWaveOrdering(t *testing.T) {
	ctx := context.Background()

	// Read cert-manager and envoy-gateway templates
	tests := []struct {
		appName      string
		expectedWave string
	}{
		{"envoy-gateway", `sync-wave: "1"`},
		{"cert-manager", `sync-wave: "2"`},
	}

	for _, tt := range tests {
		t.Run(tt.appName, func(t *testing.T) {
			var buf bytes.Buffer
			err := WriteApplication(ctx, &buf, tt.appName)
			if err != nil {
				t.Fatalf("WriteApplication(%s) error: %v", tt.appName, err)
			}

			content := buf.String()
			if !strings.Contains(content, tt.expectedWave) {
				t.Errorf("%s should have %s, got:\n%s", tt.appName, tt.expectedWave, content)
			}
		})
	}
}

func TestEnvoyGatewayBeforeCertManager(t *testing.T) {
	ctx := context.Background()

	// Extract sync waves
	getSyncWave := func(appName string) string {
		var buf bytes.Buffer
		if err := WriteApplication(ctx, &buf, appName); err != nil {
			t.Fatalf("WriteApplication(%s) error: %v", appName, err)
		}
		content := buf.String()
		for _, line := range strings.Split(content, "\n") {
			if strings.Contains(line, "sync-wave") {
				return strings.TrimSpace(line)
			}
		}
		t.Fatalf("%s has no sync-wave annotation", appName)
		return ""
	}

	envoyWave := getSyncWave("envoy-gateway")
	certWave := getSyncWave("cert-manager")

	// envoy-gateway must come before cert-manager (lower wave number)
	// because cert-manager needs Gateway API CRDs that envoy-gateway installs
	if envoyWave >= certWave {
		t.Errorf("envoy-gateway (%s) must have a lower sync-wave than cert-manager (%s)", envoyWave, certWave)
	}
}
