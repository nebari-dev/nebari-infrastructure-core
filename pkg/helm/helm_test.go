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

// TestNewActionConfigNamespaceFallback checks the namespace Helm uses for chart
// objects that set no metadata.namespace. Helm's kube client reads it from the
// getter's ToRawKubeConfigLoader, so it must be the release namespace, as with
// `helm install -n`, and not the kubeconfig context's. The FSx for OpenZFS CSI
// chart leaves the namespace off its controller, which landed it in "default".
func TestNewActionConfigNamespaceFallback(t *testing.T) {
	tests := []struct {
		name             string
		contextNamespace string
		releaseNamespace string
		want             string
	}{
		{
			name:             "context without a namespace",
			contextNamespace: "",
			releaseNamespace: "kube-system",
			want:             "kube-system",
		},
		{
			name:             "release namespace wins over the context's",
			contextNamespace: "team-a",
			releaseNamespace: "kube-system",
			want:             "kube-system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeconfig := "apiVersion: v1\nkind: Config\n" +
				"clusters:\n- name: c\n  cluster:\n    server: https://127.0.0.1:6443\n" +
				"users:\n- name: u\n  user:\n    token: t\n" +
				"contexts:\n- name: ctx\n  context:\n    cluster: c\n    user: u\n" +
				"    namespace: \"" + tt.contextNamespace + "\"\n" +
				"current-context: ctx\n"

			path, cleanup, err := WriteTempKubeconfig([]byte(kubeconfig))
			if err != nil {
				t.Fatalf("WriteTempKubeconfig() error: %v", err)
			}
			defer cleanup()

			actionConfig, err := NewActionConfig(path, tt.releaseNamespace)
			if err != nil {
				t.Fatalf("NewActionConfig() error: %v", err)
			}

			getter, ok := actionConfig.RESTClientGetter.(*KubeconfigGetter)
			if !ok {
				t.Fatalf("RESTClientGetter is %T, want *KubeconfigGetter", actionConfig.RESTClientGetter)
			}
			got, _, err := getter.ToRawKubeConfigLoader().Namespace()
			if err != nil {
				t.Fatalf("Namespace() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("fallback namespace = %q, want %q", got, tt.want)
			}
		})
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
