package longhorn

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// ManagerDaemonSetName is the Longhorn chart's DaemonSet that runs
	// longhorn-manager on every node. longhorn-manager is the process that execs
	// the engine binary for `backup inspect` / `backup rm`, so its pod
	// environment decides what ambient credentials the backup engine sees.
	ManagerDaemonSetName = "longhorn-manager"

	// managerPodSelector selects the longhorn-manager DaemonSet's pods. Matches
	// the chart's DaemonSet selector.
	managerPodSelector = "app=" + ManagerDaemonSetName

	// restartedAtAnnotation forces a DaemonSet rollout by changing the pod
	// template. Same key kubectl rollout restart uses, so a manual restart and
	// this one are indistinguishable to Longhorn and to anyone reading the
	// DaemonSet.
	restartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

	// managerRolloutTimeout bounds the wait for rolled longhorn-manager pods to
	// come back satisfying the caller's predicate.
	managerRolloutTimeout = 5 * time.Minute
)

// EnsureManagerPods guarantees every live longhorn-manager pod satisfies want,
// rolling the DaemonSet and waiting for the replacement pods when any does not.
// want is provider-supplied (e.g. "carries the credentials my platform injects
// at pod creation"); this package contributes only the Longhorn-shaped
// knowledge: which DaemonSet, its pod selector, that terminating pods must be
// ignored, and that restarting longhorn-manager is safe because volume I/O is
// served by the instance-manager pods, which are deliberately left alone
// (restarting *those* would detach live volumes).
//
// reason is a short noun phrase for what want checks, embedded in status
// messages and errors ("EKS Pod Identity container credentials (...)").
//
// timeout bounds the post-restart wait; timeout <= 0 uses the default rollout
// timeout. A wait failure is returned to the caller, which owns the decision
// of whether that is fatal and what to tell the operator.
//
// Idempotent: when every live pod already satisfies want (the common case)
// this is one API read and no writes.
func EnsureManagerPods(ctx context.Context, client kubernetes.Interface, want func(*corev1.Pod) bool, reason string, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.EnsureManagerPods")
	defer span.End()

	if timeout <= 0 {
		timeout = managerRolloutTimeout
	}

	stale, total, err := StaleManagerPods(ctx, client, want)
	if err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(
		attribute.Int("manager_pods", total),
		attribute.Int("stale_manager_pods", len(stale)),
	)

	// No live pods failing want, or no pods at all: nothing to repair. Pods
	// created from here on get whatever want checks for at pod creation.
	if total == 0 || len(stale) == 0 {
		return nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress,
		fmt.Sprintf("Restarting %s: pods missing %s", ManagerDaemonSetName, reason)).
		WithResource(ManagerDaemonSetName).
		WithAction("restarting").
		WithMetadata("stale_pods", strings.Join(stale, ",")))

	if err := restartManagerDaemonSet(ctx, client); err != nil {
		span.RecordError(err)
		return err
	}

	if err := waitForManagerPods(ctx, client, want, reason, timeout); err != nil {
		span.RecordError(err)
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess,
		fmt.Sprintf("%s restarted; every pod now has %s", ManagerDaemonSetName, reason)).
		WithResource(ManagerDaemonSetName).
		WithAction("ready"))
	return nil
}

// StaleManagerPods returns the names of live longhorn-manager pods that fail
// want, alongside the number of live pods considered. Pods already terminating
// are ignored: during a rollout the outgoing pods legitimately fail the check
// and must not re-trigger a restart.
func StaleManagerPods(ctx context.Context, client kubernetes.Interface, want func(*corev1.Pod) bool) (stale []string, total int, err error) {
	pods, err := client.CoreV1().Pods(Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: managerPodSelector,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list %s pods in %s: %w", ManagerDaemonSetName, Namespace, err)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		total++
		if !want(pod) {
			stale = append(stale, pod.Name)
		}
	}
	return stale, total, nil
}

// restartManagerDaemonSet triggers a rolling restart of the longhorn-manager
// DaemonSet by stamping the pod template, so the recreated pods pick up
// whatever their environment injects at pod creation.
func restartManagerDaemonSet(ctx context.Context, client kubernetes.Interface) error {
	patch := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{%q:%q}}}}}`,
		restartedAtAnnotation, time.Now().UTC().Format(time.RFC3339),
	)
	_, err := client.AppsV1().DaemonSets(Namespace).Patch(
		ctx, ManagerDaemonSetName, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("DaemonSet %s/%s not found; cannot restart it: %w", Namespace, ManagerDaemonSetName, err)
		}
		return fmt.Errorf("restart DaemonSet %s/%s: %w", Namespace, ManagerDaemonSetName, err)
	}
	return nil
}

// waitForManagerPods blocks until every live longhorn-manager pod satisfies
// want and the DaemonSet reports all pods ready, or timeout elapses.
func waitForManagerPods(ctx context.Context, client kubernetes.Interface, want func(*corev1.Pod) bool, reason string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// lastErr keeps the most recent reason the check did not pass so the timeout
	// error explains *what* was still wrong rather than only that time ran out.
	var lastErr error
	check := func() bool {
		ds, err := client.AppsV1().DaemonSets(Namespace).Get(ctx, ManagerDaemonSetName, metav1.GetOptions{})
		if err != nil {
			lastErr = fmt.Errorf("get DaemonSet %s/%s: %w", Namespace, ManagerDaemonSetName, err)
			return false
		}
		if ds.Status.DesiredNumberScheduled == 0 || ds.Status.NumberReady != ds.Status.DesiredNumberScheduled {
			lastErr = fmt.Errorf("%d/%d %s pods ready", ds.Status.NumberReady, ds.Status.DesiredNumberScheduled, ManagerDaemonSetName)
			return false
		}
		stale, total, err := StaleManagerPods(ctx, client, want)
		if err != nil {
			lastErr = err
			return false
		}
		if total == 0 {
			lastErr = fmt.Errorf("no live %s pods found", ManagerDaemonSetName)
			return false
		}
		if len(stale) > 0 {
			lastErr = fmt.Errorf("%s missing %s", strings.Join(stale, ","), reason)
			return false
		}
		return true
	}

	if check() {
		return nil
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Both errors go into the chain: ctx.Err() so callers can tell a user
			// cancellation from a timeout, lastErr because it is the diagnostic.
			return fmt.Errorf("gave up waiting for %s pods to have %s: %w (last state: %w)",
				ManagerDaemonSetName, reason, ctx.Err(), lastErr)
		case <-ticker.C:
			if check() {
				return nil
			}
		}
	}
}
