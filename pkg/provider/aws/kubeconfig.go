package aws

import (
	"encoding/base64"
	"fmt"

	"github.com/goccy/go-yaml"
)

// KubeconfigCluster represents the cluster section of kubeconfig
type KubeconfigCluster struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data"`
}

// KubeconfigUser represents the user section of kubeconfig
type KubeconfigUser struct {
	Exec KubeconfigExec `yaml:"exec"`
}

// KubeconfigExec represents the exec section for AWS IAM authentication
type KubeconfigExec struct {
	APIVersion string          `yaml:"apiVersion"`
	Command    string          `yaml:"command"`
	Args       []string        `yaml:"args"`
	Env        []KubeconfigEnv `yaml:"env,omitempty"`
}

// KubeconfigEnv represents environment variables for exec
type KubeconfigEnv struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

// KubeconfigContext represents a context in kubeconfig
type KubeconfigContext struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

// KubeconfigNamedCluster represents a named cluster
type KubeconfigNamedCluster struct {
	Name    string            `yaml:"name"`
	Cluster KubeconfigCluster `yaml:"cluster"`
}

// KubeconfigNamedContext represents a named context
type KubeconfigNamedContext struct {
	Name    string            `yaml:"name"`
	Context KubeconfigContext `yaml:"context"`
}

// KubeconfigNamedUser represents a named user
type KubeconfigNamedUser struct {
	Name string         `yaml:"name"`
	User KubeconfigUser `yaml:"user"`
}

// Kubeconfig represents a Kubernetes configuration file
type Kubeconfig struct {
	APIVersion     string                   `yaml:"apiVersion"`
	Kind           string                   `yaml:"kind"`
	Clusters       []KubeconfigNamedCluster `yaml:"clusters"`
	Contexts       []KubeconfigNamedContext `yaml:"contexts"`
	CurrentContext string                   `yaml:"current-context"`
	Users          []KubeconfigNamedUser    `yaml:"users"`
}

func buildKubeconfig(clusterName, endpoint, caData, region string) ([]byte, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("cluster endpoint is required")
	}

	if caData == "" {
		return nil, fmt.Errorf("cluster certificate authority data is required")
	}

	// Validate CA data is base64 encoded
	if _, err := base64.StdEncoding.DecodeString(caData); err != nil {
		return nil, fmt.Errorf("invalid certificate authority data: %w", err)
	}

	// Build kubeconfig using AWS IAM Authenticator
	// https://docs.aws.amazon.com/eks/latest/userguide/create-kubeconfig.html
	kubeconfig := Kubeconfig{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: []KubeconfigNamedCluster{
			{
				Name: clusterName,
				Cluster: KubeconfigCluster{
					Server:                   endpoint,
					CertificateAuthorityData: caData,
				},
			},
		},
		Contexts: []KubeconfigNamedContext{
			{
				Name: clusterName,
				Context: KubeconfigContext{
					Cluster: clusterName,
					User:    clusterName,
				},
			},
		},
		CurrentContext: clusterName,
		Users: []KubeconfigNamedUser{
			{
				Name: clusterName,
				User: KubeconfigUser{
					Exec: KubeconfigExec{
						APIVersion: "client.authentication.k8s.io/v1beta1",
						Command:    "aws",
						Args: []string{
							"eks",
							"get-token",
							"--cluster-name",
							clusterName,
							"--region",
							region,
						},
					},
				},
			},
		},
	}

	kubeconfigBytes, err := yaml.Marshal(&kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal kubeconfig: %w", err)
	}

	return kubeconfigBytes, nil
}
