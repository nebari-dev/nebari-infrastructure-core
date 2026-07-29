package longhorn

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// markerEnv satisfies hasMarker. The predicate is deliberately provider-neutral:
// EnsureManagerPods must not care what the caller checks for.
func markerEnv() []corev1.EnvVar {
	return []corev1.EnvVar{{Name: "MARKER", Value: "yes"}}
}

// hasMarker is the test predicate handed to EnsureManagerPods.
func hasMarker(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == "MARKER" && env.Value != "" {
				return true
			}
		}
	}
	return false
}

const markerReason = "the test MARKER variable"

func managerPod(name string, env []corev1.EnvVar, terminating bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: Namespace,
			Labels:    map[string]string{"app": ManagerDaemonSetName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: ManagerDaemonSetName, Env: env}},
		},
	}
	if terminating {
		now := metav1.NewTime(time.Now())
		pod.DeletionTimestamp = &now
		pod.Finalizers = []string{"nic.nebari.dev/test"}
	}
	return pod
}

// managerDaemonSet reports desired == ready so the post-restart wait passes as
// soon as the pods satisfy the predicate.
func managerDaemonSet(ready int32) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: ManagerDaemonSetName, Namespace: Namespace},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: ready,
			NumberReady:            ready,
		},
	}
}

func TestStaleManagerPodsIgnoresTerminating(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerPod("longhorn-manager-old", nil, true), // terminating, stale
		managerPod("longhorn-manager-new", markerEnv(), false),
	)

	stale, total, err := StaleManagerPods(context.Background(), client, hasMarker)
	if err != nil {
		t.Fatalf("StaleManagerPods() error = %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1 (terminating pods excluded)", total)
	}
	if len(stale) != 0 {
		t.Errorf("stale = %v, want none: a terminating pod must not re-trigger a restart", stale)
	}
}

// patchCount counts DaemonSet patches, which is how the restart is issued.
func patchCount(client *fake.Clientset) int {
	n := 0
	for _, action := range client.Actions() {
		if action.Matches("patch", "daemonsets") {
			n++
		}
	}
	return n
}

func TestEnsureManagerPodsNoRestartWhenAllSatisfyWant(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(2),
		managerPod("longhorn-manager-a", markerEnv(), false),
		managerPod("longhorn-manager-b", markerEnv(), false),
	)

	if err := EnsureManagerPods(context.Background(), client, hasMarker, markerReason, time.Second); err != nil {
		t.Fatalf("EnsureManagerPods() error = %v", err)
	}
	if n := patchCount(client); n != 0 {
		t.Errorf("DaemonSet patched %d times, want 0: satisfied pods must not be rolled", n)
	}
}

func TestEnsureManagerPodsNoPodsIsNoOp(t *testing.T) {
	// No live manager pods: nothing to repair. Pods created later get whatever
	// the predicate checks for at pod creation.
	client := fake.NewSimpleClientset(managerDaemonSet(0))

	if err := EnsureManagerPods(context.Background(), client, hasMarker, markerReason, time.Second); err != nil {
		t.Fatalf("EnsureManagerPods() error = %v", err)
	}
	if n := patchCount(client); n != 0 {
		t.Errorf("DaemonSet patched %d times, want 0", n)
	}
}

func TestEnsureManagerPodsRestartsStaleDaemonSet(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(2),
		managerPod("longhorn-manager-stale", nil, false),
		managerPod("longhorn-manager-ok", markerEnv(), false),
	)

	// The fake client runs no controllers, so the rolled pods never reappear and
	// the post-restart wait times out; that error surfacing is asserted by
	// TestEnsureManagerPodsReturnsWaitFailure. What this asserts is the restart
	// itself: one patch, stamping the pod template.
	err := EnsureManagerPods(context.Background(), client, hasMarker, markerReason, 50*time.Millisecond)
	if err == nil {
		t.Fatal("EnsureManagerPods() error = nil, want wait failure (no controller rolls the pods)")
	}

	if n := patchCount(client); n != 1 {
		t.Fatalf("DaemonSet patched %d times, want 1", n)
	}

	ds, err := client.AppsV1().DaemonSets(Namespace).Get(context.Background(), ManagerDaemonSetName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get DaemonSet: %v", err)
	}
	if _, ok := ds.Spec.Template.Annotations[restartedAtAnnotation]; !ok {
		t.Errorf("pod template annotations = %v, want %s stamped to force a rollout",
			ds.Spec.Template.Annotations, restartedAtAnnotation)
	}
}

// The caller owns the fatal-vs-warn decision, so a wait that never converges
// must come back as an error carrying both the deadline and the diagnostic.
func TestEnsureManagerPodsReturnsWaitFailure(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(1),
		managerPod("longhorn-manager-stale", nil, false),
	)

	err := EnsureManagerPods(context.Background(), client, hasMarker, markerReason, 50*time.Millisecond)
	if err == nil {
		t.Fatal("EnsureManagerPods() error = nil, want timeout error when pods never satisfy want")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error chain = %v, want context.DeadlineExceeded so callers can tell timeout from cancellation", err)
	}
}

// A user abort (Ctrl-C) must be distinguishable from a broken rollout: the
// cancellation has to survive into the error chain.
func TestEnsureManagerPodsSurfacesCancellation(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(1),
		managerPod("longhorn-manager-stale", nil, false),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := EnsureManagerPods(ctx, client, hasMarker, markerReason, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error chain = %v, want context.Canceled", err)
	}
}

func TestEnsureManagerPodsMissingDaemonSetErrors(t *testing.T) {
	client := fake.NewSimpleClientset(managerPod("longhorn-manager-stale", nil, false))

	err := EnsureManagerPods(context.Background(), client, hasMarker, markerReason, time.Second)
	if err == nil {
		t.Fatal("EnsureManagerPods() error = nil, want an error when the DaemonSet is absent")
	}
}
