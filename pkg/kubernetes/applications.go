package kubernetes

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

var (
	// Argo CD Application GVR
	applicationGVR = schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
)

// ApplyArgoCDApplication creates or updates an Argo CD Application
func ApplyArgoCDApplication(ctx context.Context, kubeconfigBytes []byte, appManifest *unstructured.Unstructured) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.ApplyArgoCDApplication")
	defer span.End()

	appName := appManifest.GetName()
	appNamespace := appManifest.GetNamespace()

	span.SetAttributes(
		attribute.String("application_name", appName),
		attribute.String("application_namespace", appNamespace),
	)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Applying Argo CD Application: %s", appName)).
		WithResource("argocd-application").
		WithAction("applying").
		WithMetadata("application", appName))

	// Create Kubernetes REST config
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Try to get existing application
	existingApp, err := dynamicClient.Resource(applicationGVR).Namespace(appNamespace).Get(ctx, appName, metav1.GetOptions{})
	if err == nil {
		// Application exists, update it
		// Preserve resourceVersion from existing application
		appManifest.SetResourceVersion(existingApp.GetResourceVersion())

		_, err = dynamicClient.Resource(applicationGVR).Namespace(appNamespace).Update(ctx, appManifest, metav1.UpdateOptions{})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update Argo CD Application: %w", err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Updated Argo CD Application: %s", appName)).
			WithResource("argocd-application").
			WithAction("updated").
			WithMetadata("application", appName))
	} else {
		// Application doesn't exist, create it
		_, err = dynamicClient.Resource(applicationGVR).Namespace(appNamespace).Create(ctx, appManifest, metav1.CreateOptions{})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Argo CD Application: %w", err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Created Argo CD Application: %s", appName)).
			WithResource("argocd-application").
			WithAction("created").
			WithMetadata("application", appName))
	}

	return nil
}

// WaitForArgoCDApplication waits for an Argo CD Application to reach a healthy and synced state
func WaitForArgoCDApplication(ctx context.Context, kubeconfigBytes []byte, appName, appNamespace string, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "kubernetes.WaitForArgoCDApplication")
	defer span.End()

	span.SetAttributes(
		attribute.String("application_name", appName),
		attribute.String("application_namespace", appNamespace),
		attribute.String("timeout", timeout.String()),
	)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Waiting for Argo CD Application to be ready: %s", appName)).
		WithResource("argocd-application").
		WithAction("waiting").
		WithMetadata("application", appName))

	// Create Kubernetes REST config
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			err := fmt.Errorf("timeout waiting for Argo CD Application %s: %w", appName, ctx.Err())
			span.RecordError(err)
			return err
		case <-ticker.C:
			app, err := dynamicClient.Resource(applicationGVR).Namespace(appNamespace).Get(ctx, appName, metav1.GetOptions{})
			if err != nil {
				// Application not found yet, continue waiting
				continue
			}

			// Check application status
			statusObj, found, err := unstructured.NestedMap(app.Object, "status")
			if err != nil || !found {
				continue
			}

			// Check health status
			healthStatus, found, err := unstructured.NestedString(statusObj, "health", "status")
			if err != nil || !found {
				continue
			}

			// Check sync status
			syncStatus, found, err := unstructured.NestedString(statusObj, "sync", "status")
			if err != nil || !found {
				continue
			}

			// Application is ready if it's Healthy and Synced
			if healthStatus == "Healthy" && syncStatus == "Synced" {
				status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Argo CD Application is ready: %s", appName)).
					WithResource("argocd-application").
					WithAction("ready").
					WithMetadata("application", appName).
					WithMetadata("health", healthStatus).
					WithMetadata("sync", syncStatus))
				return nil
			}
		}
	}
}

// GetArgoCDApplicationStatus returns the health and sync status of an Argo CD Application
func GetArgoCDApplicationStatus(ctx context.Context, client *kubernetes.Clientset, kubeconfigBytes []byte, appName, appNamespace string) (health, sync string, err error) {
	// Create Kubernetes REST config
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return "", "", fmt.Errorf("failed to create dynamic client: %w", err)
	}

	app, err := dynamicClient.Resource(applicationGVR).Namespace(appNamespace).Get(ctx, appName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get application: %w", err)
	}

	statusObj, found, err := unstructured.NestedMap(app.Object, "status")
	if err != nil || !found {
		return "", "", fmt.Errorf("application has no status")
	}

	health, _, _ = unstructured.NestedString(statusObj, "health", "status")
	sync, _, _ = unstructured.NestedString(statusObj, "sync", "status")

	return health, sync, nil
}
