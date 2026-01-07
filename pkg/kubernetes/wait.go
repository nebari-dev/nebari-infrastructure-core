package kubernetes

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// WaitForClusterReady waits for the cluster API to be responsive and core components ready
func WaitForClusterReady(ctx context.Context, client *kubernetes.Clientset, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "kubernetes.WaitForClusterReady")
	defer span.End()

	span.SetAttributes(
		attribute.String("timeout", timeout.String()),
	)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for cluster to be ready").
		WithResource("cluster").
		WithAction("waiting"))

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			err := fmt.Errorf("timeout waiting for cluster to be ready: %w", ctx.Err())
			span.RecordError(err)
			return err
		case <-ticker.C:
			// Check if API server responds
			_, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
			if err != nil {
				// API server not ready yet, continue waiting
				continue
			}

			// Check if at least one node is Ready
			if ready, err := isAnyNodeReady(ctx, client); err == nil && ready {
				status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Cluster is ready").
					WithResource("cluster").
					WithAction("ready"))
				return nil
			}
		}
	}
}

// isAnyNodeReady checks if at least one node in the cluster is in Ready state
func isAnyNodeReady(ctx context.Context, client *kubernetes.Clientset) (bool, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}

	for _, node := range nodes.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
	}

	return false, nil
}

// WaitForArgoCDReady waits for Argo CD pods to be ready
func WaitForArgoCDReady(ctx context.Context, client *kubernetes.Clientset, namespace string, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "kubernetes.WaitForArgoCDReady")
	defer span.End()

	span.SetAttributes(
		attribute.String("namespace", namespace),
		attribute.String("timeout", timeout.String()),
	)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for Argo CD to be ready").
		WithResource("argocd").
		WithAction("waiting"))

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Core Argo CD deployments that must be ready
	requiredDeployments := []string{
		"argocd-server",
		"argocd-repo-server",
	}

	// Core Argo CD statefulsets that must be ready
	requiredStatefulSets := []string{
		"argocd-application-controller",
	}

	for {
		select {
		case <-ctx.Done():
			err := fmt.Errorf("timeout waiting for Argo CD to be ready: %w", ctx.Err())
			span.RecordError(err)
			return err
		case <-ticker.C:
			allReady := true

			// Check deployments
			for _, name := range requiredDeployments {
				deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
				if err != nil {
					allReady = false
					break
				}
				if deployment.Status.ReadyReplicas < 1 {
					allReady = false
					break
				}
			}

			// Check statefulsets
			if allReady {
				for _, name := range requiredStatefulSets {
					statefulset, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
					if err != nil {
						allReady = false
						break
					}
					if statefulset.Status.ReadyReplicas < 1 {
						allReady = false
						break
					}
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
