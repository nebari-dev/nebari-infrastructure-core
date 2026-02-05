package argocd

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// Install installs Argo CD on a Kubernetes cluster
// This is the main entry point called from cmd/nic/deploy.go
func Install(ctx context.Context, cfg *config.NebariConfig, prov provider.Provider) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.Install")
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
	k8sClient, err := newK8sClient(kubeconfigBytes)
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

	if err := waitForClusterReady(ctx, k8sClient, 5*time.Minute); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, "Cluster not ready").
			WithResource("cluster").
			WithAction("not-ready").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("cluster not ready: %w", err)
	}

	// Get Argo CD configuration
	argoCDCfg := DefaultConfig()

	// Create namespace
	if err := createNamespace(ctx, k8sClient, argoCDCfg.Namespace); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Failed to create namespace %s", argoCDCfg.Namespace)).
			WithResource("namespace").
			WithAction("create-failed").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Install Argo CD using Helm
	if err := InstallHelm(ctx, kubeconfigBytes, argoCDCfg); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, "Failed to install Argo CD").
			WithResource("argocd").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to install Argo CD: %w", err)
	}

	// Wait for Argo CD to be ready
	if err := waitForArgoCDReady(ctx, k8sClient, argoCDCfg.Namespace, 5*time.Minute); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Argo CD may not be fully ready yet").
			WithResource("argocd").
			WithAction("not-ready").
			WithMetadata("error", err.Error()))
		// Don't fail - Argo CD may still come up, just warn the user
	}

	// Configure OCI registry access for Argo CD (unauthenticated)
	if err := ConfigureOCIAccess(ctx, k8sClient, argoCDCfg.Namespace); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to configure OCI registry access").
			WithResource("argocd").
			WithAction("oci-config-failed").
			WithMetadata("error", err.Error()))
		// Don't fail - user can configure this manually if needed
	}

	// Configure Git repository access if GitOps is configured
	if cfg.GitRepository != nil {
		if err := ConfigureGitRepoAccess(ctx, k8sClient, cfg, argoCDCfg.Namespace); err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to configure Git repository access").
				WithResource("argocd").
				WithAction("git-config-failed").
				WithMetadata("error", err.Error()))
			// Don't fail - but this will prevent GitOps from working
		}
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

// NodeListFunc is a function type for listing nodes, allowing for dependency injection in tests.
type NodeListFunc func(ctx context.Context) ([]corev1.Node, error)

// isClusterReady checks if at least one node in the cluster is ready.
// This is a pure function that can be easily tested.
func isClusterReady(nodes []corev1.Node) bool {
	for _, node := range nodes {
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				return true
			}
		}
	}
	return false
}

// waitForClusterReadyWithLister waits for the cluster to be ready using the provided node lister.
// This function separates the polling logic from the Kubernetes client, making it testable.
func waitForClusterReadyWithLister(ctx context.Context, listNodes NodeListFunc, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.waitForClusterReady")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for cluster to be ready").
		WithResource("cluster").
		WithAction("waiting"))

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Check function to avoid duplication
	checkReady := func() bool {
		nodes, err := listNodes(ctx)
		if err != nil {
			return false
		}
		return isClusterReady(nodes)
	}

	// Immediate check before starting the ticker
	if checkReady() {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Cluster is ready").
			WithResource("cluster").
			WithAction("ready"))
		return nil
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for cluster: %w", ctx.Err())
		case <-ticker.C:
			if checkReady() {
				status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Cluster is ready").
					WithResource("cluster").
					WithAction("ready"))
				return nil
			}
		}
	}
}

// waitForClusterReady waits for the cluster to be ready using a Kubernetes client.
func waitForClusterReady(ctx context.Context, client *kubernetes.Clientset, timeout time.Duration) error {
	listNodes := func(ctx context.Context) ([]corev1.Node, error) {
		nodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return nodeList.Items, nil
	}
	return waitForClusterReadyWithLister(ctx, listNodes, timeout)
}

// waitForArgoCDReady waits for Argo CD deployments to be ready
func waitForArgoCDReady(ctx context.Context, client *kubernetes.Clientset, namespace string, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.waitForArgoCDReady")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for Argo CD to be ready").
		WithResource("argocd").
		WithAction("waiting"))

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Deployments to wait for
	requiredDeployments := []string{
		"argocd-server",
		"argocd-repo-server",
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Argo CD: %w", ctx.Err())
		case <-ticker.C:
			allReady := true
			for _, deployName := range requiredDeployments {
				deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
				if err != nil {
					allReady = false
					break
				}
				if !isDeploymentReady(deploy) {
					allReady = false
					break
				}
			}
			if allReady {
				status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Argo CD is ready").
					WithResource("argocd").
					WithAction("ready"))
				return nil
			}
		}
	}
}

// isDeploymentReady checks if a deployment has all replicas ready
func isDeploymentReady(deploy *appsv1.Deployment) bool {
	return deploy.Status.ReadyReplicas >= *deploy.Spec.Replicas
}
