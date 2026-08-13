package argocd

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// applicationControllerStatefulSet is the workload that reconciles
	// Application resources into cluster state.
	applicationControllerStatefulSet = "argocd-application-controller"

	// applicationControllerPodSelector matches the controller's pods via the
	// argo-cd chart's component name label.
	applicationControllerPodSelector = "app.kubernetes.io/name=" + applicationControllerStatefulSet

	// defaultSuspendTimeout bounds the wait for the controller pods to
	// terminate after scaling to zero.
	defaultSuspendTimeout = 2 * time.Minute

	// defaultSuspendPollInterval is how often the pod termination wait
	// re-lists. Injectable in suspendReconciliation so tests can drive the
	// loop without real sleeps.
	defaultSuspendPollInterval = 2 * time.Second
)

// SuspendReconciliation scales the Argo CD application controller to zero and
// waits for its pods to terminate, so that nothing recreates resources deleted
// during teardown. Returns nil when the controller does not exist (Argo CD
// absent or already removed).
func SuspendReconciliation(ctx context.Context, kubeconfig []byte) error {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return fmt.Errorf("parse kubeconfig: %w", err)
	}
	k8s, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("build kubernetes client: %w", err)
	}
	return suspendReconciliation(ctx, k8s, defaultSuspendTimeout, defaultSuspendPollInterval)
}

// suspendReconciliation runs the scale-down against a pre-built client.
// Exposed for tests.
func suspendReconciliation(ctx context.Context, client kubernetes.Interface, timeout, pollInterval time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.suspendReconciliation")
	defer span.End()
	span.SetAttributes(attribute.String("timeout", timeout.String()))

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Suspending Argo CD reconciliation").
		WithResource("argocd").WithAction("suspending"))

	scaleToZero := []byte(`{"spec":{"replicas":0}}`)
	if _, err := client.AppsV1().StatefulSets(defaultNamespace).Patch(ctx, applicationControllerStatefulSet, types.StrategicMergePatchType, scaleToZero, metav1.PatchOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			span.SetAttributes(attribute.String("suspend_result", "controller_absent"))
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Argo CD application controller not found; nothing to suspend").
				WithResource("argocd").WithAction("suspending"))
			return nil
		}
		span.RecordError(err)
		return fmt.Errorf("scale Argo CD application controller to zero: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		pods, err := client.CoreV1().Pods(defaultNamespace).List(ctx, metav1.ListOptions{LabelSelector: applicationControllerPodSelector})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("list Argo CD application controller pods: %w", err)
		}

		if len(pods.Items) == 0 {
			span.SetAttributes(attribute.String("suspend_result", "suspended"))
			status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Argo CD reconciliation suspended").
				WithResource("argocd").WithAction("suspending"))
			return nil
		}

		if time.Now().After(deadline) {
			err := fmt.Errorf("%d Argo CD application controller pod(s) still running after %s", len(pods.Items), timeout)
			span.RecordError(err)
			return err
		}

		status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Waiting for %d Argo CD application controller pod(s) to terminate", len(pods.Items))).
			WithResource("argocd").WithAction("suspending"))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
