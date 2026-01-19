package kubernetes

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// restConfigFromKubeconfig is a shared helper that parses kubeconfig bytes into a REST config
// This handles all authentication methods (AWS IAM exec, certificate-based, token-based, etc.)
// via the standard client-go mechanisms
func restConfigFromKubeconfig(ctx context.Context, kubeconfigBytes []byte, spanName string) (*rest.Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, spanName)
	defer span.End()

	span.SetAttributes(
		attribute.Int("kubeconfig_size_bytes", len(kubeconfigBytes)),
	)

	// Parse kubeconfig
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	span.SetAttributes(
		attribute.String("host", config.Host),
	)

	return config, nil
}

// NewClientFromKubeconfig creates a Kubernetes clientset from kubeconfig bytes
func NewClientFromKubeconfig(ctx context.Context, kubeconfigBytes []byte) (*kubernetes.Clientset, error) {
	config, err := restConfigFromKubeconfig(ctx, kubeconfigBytes, "kubernetes.NewClientFromKubeconfig")
	if err != nil {
		return nil, err
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return clientset, nil
}

// NewDynamicClientFromKubeconfig creates a dynamic Kubernetes client from kubeconfig bytes
// Dynamic clients can work with any Kubernetes resource type
func NewDynamicClientFromKubeconfig(ctx context.Context, kubeconfigBytes []byte) (dynamic.Interface, error) {
	config, err := restConfigFromKubeconfig(ctx, kubeconfigBytes, "kubernetes.NewDynamicClientFromKubeconfig")
	if err != nil {
		return nil, err
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic Kubernetes client: %w", err)
	}

	return dynamicClient, nil
}
