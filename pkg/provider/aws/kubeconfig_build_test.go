package aws

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestBuildKubeconfig(t *testing.T) {
	validCA := base64.StdEncoding.EncodeToString([]byte("dummy-ca-bytes"))

	tests := []struct {
		name        string
		clusterName string
		endpoint    string
		caData      string
		region      string
		wantErr     bool
		errContains string
	}{
		{
			name:        "success",
			clusterName: "test-cluster",
			endpoint:    "https://test.eks.amazonaws.com",
			caData:      validCA,
			region:      "us-west-2",
			wantErr:     false,
		},
		{
			name:        "empty endpoint",
			clusterName: "test-cluster",
			endpoint:    "",
			caData:      validCA,
			region:      "us-west-2",
			wantErr:     true,
			errContains: "cluster endpoint is required",
		},
		{
			name:        "empty CA data",
			clusterName: "test-cluster",
			endpoint:    "https://test.eks.amazonaws.com",
			caData:      "",
			region:      "us-west-2",
			wantErr:     true,
			errContains: "cluster certificate authority data is required",
		},
		{
			name:        "invalid base64 CA data",
			clusterName: "test-cluster",
			endpoint:    "https://test.eks.amazonaws.com",
			caData:      "not-valid-base64!@#$",
			region:      "us-west-2",
			wantErr:     true,
			errContains: "invalid certificate authority data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildKubeconfig(tt.clusterName, tt.endpoint, tt.caData, tt.region)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) == 0 {
				t.Fatalf("expected kubeconfig bytes, got empty output")
			}

			// Validate YAML structure by unmarshalling into the struct.
			var kc Kubeconfig
			if err := yaml.Unmarshal(got, &kc); err != nil {
				t.Fatalf("failed to unmarshal kubeconfig YAML: %v\nkubeconfig:\n%s", err, string(got))
			}

			// Basic top-level assertions
			if kc.APIVersion != "v1" {
				t.Fatalf("unexpected apiVersion: got %q, want %q", kc.APIVersion, "v1")
			}
			if kc.Kind != "Config" {
				t.Fatalf("unexpected kind: got %q, want %q", kc.Kind, "Config")
			}
			if kc.CurrentContext != tt.clusterName {
				t.Fatalf("unexpected current-context: got %q, want %q", kc.CurrentContext, tt.clusterName)
			}

			// Validate clusters
			if len(kc.Clusters) != 1 {
				t.Fatalf("expected 1 cluster entry, got %d", len(kc.Clusters))
			}
			if kc.Clusters[0].Name != tt.clusterName {
				t.Fatalf("unexpected cluster name: got %q, want %q", kc.Clusters[0].Name, tt.clusterName)
			}
			if kc.Clusters[0].Cluster.Server != tt.endpoint {
				t.Fatalf("unexpected cluster server: got %q, want %q", kc.Clusters[0].Cluster.Server, tt.endpoint)
			}
			if kc.Clusters[0].Cluster.CertificateAuthorityData != tt.caData {
				t.Fatalf("unexpected CA data: got %q, want %q", kc.Clusters[0].Cluster.CertificateAuthorityData, tt.caData)
			}

			// Validate contexts
			if len(kc.Contexts) != 1 {
				t.Fatalf("expected 1 context entry, got %d", len(kc.Contexts))
			}
			if kc.Contexts[0].Name != tt.clusterName {
				t.Fatalf("unexpected context name: got %q, want %q", kc.Contexts[0].Name, tt.clusterName)
			}
			if kc.Contexts[0].Context.Cluster != tt.clusterName {
				t.Fatalf("unexpected context cluster: got %q, want %q", kc.Contexts[0].Context.Cluster, tt.clusterName)
			}
			if kc.Contexts[0].Context.User != tt.clusterName {
				t.Fatalf("unexpected context user: got %q, want %q", kc.Contexts[0].Context.User, tt.clusterName)
			}

			// Validate users + exec args
			if len(kc.Users) != 1 {
				t.Fatalf("expected 1 user entry, got %d", len(kc.Users))
			}
			if kc.Users[0].Name != tt.clusterName {
				t.Fatalf("unexpected user name: got %q, want %q", kc.Users[0].Name, tt.clusterName)
			}

			exec := kc.Users[0].User.Exec
			if exec.Command != "aws" {
				t.Fatalf("unexpected exec command: got %q, want %q", exec.Command, "aws")
			}

			// Reviewer request: validate exec.APIVersion (kubectl auth plugin selection)
			if exec.APIVersion != "client.authentication.k8s.io/v1beta1" {
				t.Fatalf(
					"unexpected exec apiVersion: got %q, want %q",
					exec.APIVersion,
					"client.authentication.k8s.io/v1beta1",
				)
			}

			wantArgs := []string{
				"eks",
				"get-token",
				"--cluster-name",
				tt.clusterName,
				"--region",
				tt.region,
			}
			if len(exec.Args) != len(wantArgs) {
				t.Fatalf("unexpected exec args length: got %d, want %d; args=%v", len(exec.Args), len(wantArgs), exec.Args)
			}
			for i := range wantArgs {
				if exec.Args[i] != wantArgs[i] {
					t.Fatalf("unexpected exec arg[%d]: got %q, want %q; args=%v", i, exec.Args[i], wantArgs[i], exec.Args)
				}
			}
		})
	}
}
