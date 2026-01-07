package kubernetes

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClientFromKubeconfig creates a Kubernetes clientset from kubeconfig bytes
// This handles all authentication methods (AWS IAM exec, certificate-based, token-based, etc.)
// via the standard client-go mechanisms
func NewClientFromKubeconfig(ctx context.Context, kubeconfigBytes []byte) (*kubernetes.Clientset, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.NewClientFromKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.Int("kubeconfig_size_bytes", len(kubeconfigBytes)),
	)

	// Parse kubeconfig - this handles all auth methods via client-go
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	span.SetAttributes(
		attribute.String("host", config.Host),
	)

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return clientset, nil
}
