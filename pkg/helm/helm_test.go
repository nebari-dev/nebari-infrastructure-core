package helm

import (
	"os"
	"testing"
)

func TestWriteTempKubeconfig(t *testing.T) {
	kubeconfigBytes := []byte("apiVersion: v1\nkind: Config\n")

	path, cleanup, err := WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		t.Fatalf("WriteTempKubeconfig() error: %v", err)
	}

	content, err := os.ReadFile(path) //nolint:gosec // reading back our own temp file in test
	if err != nil {
		t.Fatalf("failed to read temp kubeconfig: %v", err)
	}
	if string(content) != string(kubeconfigBytes) {
		t.Errorf("content mismatch: got %q, want %q", string(content), string(kubeconfigBytes))
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("cleanup() should have removed the temp file")
	}
}

func TestWriteTempKubeconfigEmptyBytes(t *testing.T) {
	path, cleanup, err := WriteTempKubeconfig([]byte{})
	if err != nil {
		t.Fatalf("WriteTempKubeconfig() error: %v", err)
	}
	defer cleanup()

	content, err := os.ReadFile(path) //nolint:gosec // reading back our own temp file in test
	if err != nil {
		t.Fatalf("failed to read temp kubeconfig: %v", err)
	}
	if len(content) != 0 {
		t.Errorf("expected empty content, got %q", string(content))
	}
}
