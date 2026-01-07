package kubernetes

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// InstallArgoCD installs Argo CD on a Kubernetes cluster
// This is the main entry point called from cmd/nic/deploy.go
func InstallArgoCD(ctx context.Context, cfg *config.NebariConfig, prov provider.Provider) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "kubernetes.InstallArgoCD")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", prov.Name()),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Installing Argo CD on cluster").
		WithResource("argocd").
		WithAction("installing").
		WithMetadata("cluster_name", cfg.ProjectName))

	// Get kubeconfig from provider
	kubeconfigBytes, err := prov.GetKubeconfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, "Failed to get kubeconfig").
			WithResource("argocd").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	span.SetAttributes(
		attribute.Int("kubeconfig_size_bytes", len(kubeconfigBytes)),
	)

	// Create Kubernetes client
	k8sClient, err := NewClientFromKubeconfig(ctx, kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, "Failed to create Kubernetes client").
			WithResource("argocd").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Wait for cluster to be ready
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for cluster to be ready").
		WithResource("cluster").
		WithAction("waiting"))

	if err := WaitForClusterReady(ctx, k8sClient, 5*time.Minute); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, "Cluster not ready").
			WithResource("cluster").
			WithAction("not-ready").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("cluster not ready: %w", err)
	}

	// Get Argo CD configuration
	argoCDCfg := defaultArgoCDConfig()

	// Create namespace
	if err := CreateNamespace(ctx, k8sClient, argoCDCfg.Namespace); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Failed to create namespace %s", argoCDCfg.Namespace)).
			WithResource("namespace").
			WithAction("create-failed").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Install Argo CD using Helm
	if err := installArgoCDHelm(ctx, kubeconfigBytes, argoCDCfg); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, "Failed to install Argo CD").
			WithResource("argocd").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to install Argo CD: %w", err)
	}

	// Wait for Argo CD to be ready
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for Argo CD to be ready").
		WithResource("argocd").
		WithAction("waiting"))

	if err := WaitForArgoCDReady(ctx, k8sClient, argoCDCfg.Namespace, 5*time.Minute); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Argo CD may not be fully ready yet").
			WithResource("argocd").
			WithAction("not-ready").
			WithMetadata("error", err.Error()))
		// Don't fail - Argo CD may still come up, just warn the user
	}

	span.SetAttributes(
		attribute.String("argocd_version", argoCDCfg.Version),
		attribute.String("argocd_namespace", argoCDCfg.Namespace),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Argo CD installed successfully").
		WithResource("argocd").
		WithAction("installed").
		WithMetadata("version", argoCDCfg.Version).
		WithMetadata("namespace", argoCDCfg.Namespace))

	return nil
}
