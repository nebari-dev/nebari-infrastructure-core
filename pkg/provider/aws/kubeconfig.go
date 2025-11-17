package aws

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gopkg.in/yaml.v3"
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

// generateKubeconfig generates a kubeconfig file for the EKS cluster
// TODO: This will be called from GetKubeconfig() once Query() is implemented
//
//nolint:unused
func (p *Provider) generateKubeconfig(ctx context.Context, clients *Clients, clusterName string, region string) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
	)

	// Describe the cluster to get endpoint and CA data
	describeInput := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}

	describeOutput, err := clients.EKSClient.DescribeCluster(ctx, describeInput)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to describe EKS cluster %s: %w", clusterName, err)
	}

	cluster := describeOutput.Cluster

	// Validate cluster is active
	if cluster.Status != "ACTIVE" {
		err := fmt.Errorf("cluster %s is not active (status: %s)", clusterName, cluster.Status)
		span.RecordError(err)
		return nil, err
	}

	// Extract endpoint and CA data
	endpoint := aws.ToString(cluster.Endpoint)
	caData := aws.ToString(cluster.CertificateAuthority.Data)

	if endpoint == "" {
		err := fmt.Errorf("cluster %s has no endpoint", clusterName)
		span.RecordError(err)
		return nil, err
	}

	if caData == "" {
		err := fmt.Errorf("cluster %s has no certificate authority data", clusterName)
		span.RecordError(err)
		return nil, err
	}

	// Validate CA data is base64 encoded
	if _, err := base64.StdEncoding.DecodeString(caData); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("cluster %s has invalid certificate authority data: %w", clusterName, err)
	}

	span.SetAttributes(
		attribute.String("cluster_endpoint", endpoint),
		attribute.Int("ca_data_length", len(caData)),
	)

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

	// Marshal to YAML
	kubeconfigBytes, err := yaml.Marshal(&kubeconfig)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to marshal kubeconfig: %w", err)
	}

	span.SetAttributes(
		attribute.Int("kubeconfig_size_bytes", len(kubeconfigBytes)),
	)

	return kubeconfigBytes, nil
}
