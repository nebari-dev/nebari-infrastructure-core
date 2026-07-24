package longhorn

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// podIdentityEnv is the pair the EKS Pod Identity webhook injects.
func podIdentityEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: containerCredentialsURIEnv, Value: "http://169.254.170.23/v1/credentials"},
		{Name: containerCredentialsTokenFileEnv, Value: "/var/run/secrets/pods.eks.amazonaws.com/serviceaccount/eks-pod-identity-token"},
	}
}

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
// soon as the pods carry the injected credentials.
func managerDaemonSet(ready int32) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: ManagerDaemonSetName, Namespace: Namespace},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: ready,
			NumberReady:            ready,
		},
	}
}

func TestHasPodIdentityEnv(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "both vars injected",
			pod:  managerPod("p", podIdentityEnv(), false),
			want: true,
		},
		{
			name: "no env at all (pod predates the association)",
			pod:  managerPod("p", nil, false),
			want: false,
		},
		{
			name: "uri without token file is not usable credentials",
			pod:  managerPod("p", []corev1.EnvVar{{Name: containerCredentialsURIEnv, Value: "http://169.254.170.23/v1/credentials"}}, false),
			want: false,
		},
		{
			name: "empty values do not count as injected",
			pod: managerPod("p", []corev1.EnvVar{
				{Name: containerCredentialsURIEnv, Value: ""},
				{Name: containerCredentialsTokenFileEnv, Value: ""},
			}, false),
			want: false,
		},
		{
			name: "AWS_IAM_ROLE_ARN alone is a sentinel, not credentials",
			pod:  managerPod("p", []corev1.EnvVar{{Name: "AWS_IAM_ROLE_ARN", Value: "arn:aws:iam::1:role/r"}}, false),
			want: false,
		},
		{
			name: "injection found on a sidecar container",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "other", Env: nil},
					{Name: ManagerDaemonSetName, Env: podIdentityEnv()},
				}},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPodIdentityEnv(tt.pod); got != tt.want {
				t.Errorf("hasPodIdentityEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStaleManagerPodsIgnoresTerminating(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerPod("longhorn-manager-old", nil, true), // terminating, stale
		managerPod("longhorn-manager-new", podIdentityEnv(), false),
	)

	stale, total, err := staleManagerPods(context.Background(), client)
	if err != nil {
		t.Fatalf("staleManagerPods() error = %v", err)
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

func TestEnsureManagerPodIdentityEnvNoRestartWhenInjected(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(2),
		managerPod("longhorn-manager-a", podIdentityEnv(), false),
		managerPod("longhorn-manager-b", podIdentityEnv(), false),
	)

	if err := ensureManagerPodIdentityEnv(context.Background(), client, time.Second); err != nil {
		t.Fatalf("ensureManagerPodIdentityEnv() error = %v", err)
	}
	if n := patchCount(client); n != 0 {
		t.Errorf("DaemonSet patched %d times, want 0: already-injected pods must not be rolled", n)
	}
}

func TestEnsureManagerPodIdentityEnvNoPodsIsNoOp(t *testing.T) {
	// Longhorn not running yet (fresh deploy): its pods are created after this
	// point and pass through the webhook, so there is nothing to repair.
	client := fake.NewSimpleClientset(managerDaemonSet(0))

	if err := ensureManagerPodIdentityEnv(context.Background(), client, time.Second); err != nil {
		t.Fatalf("ensureManagerPodIdentityEnv() error = %v", err)
	}
	if n := patchCount(client); n != 0 {
		t.Errorf("DaemonSet patched %d times, want 0", n)
	}
}

func TestEnsureManagerPodIdentityEnvRestartsStaleDaemonSet(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(2),
		managerPod("longhorn-manager-stale", nil, false),
		managerPod("longhorn-manager-ok", podIdentityEnv(), false),
	)

	// The fake client runs no controllers, so the rolled pods never reappear and
	// the post-restart wait times out into the non-fatal warning path. What this
	// asserts is the restart itself: one patch, stamping the pod template.
	if err := ensureManagerPodIdentityEnv(context.Background(), client, 50*time.Millisecond); err != nil {
		t.Fatalf("ensureManagerPodIdentityEnv() error = %v", err)
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

func TestEnsureManagerPodIdentityEnvWarnsInsteadOfFailingWhenRollDoesNotHelp(t *testing.T) {
	// The webhook is absent (no eks-pod-identity-agent), so the rolled pods still
	// lack the credentials. Backup creation and restore keep working, so this must
	// warn rather than fail the deploy.
	client := fake.NewSimpleClientset(
		managerDaemonSet(1),
		managerPod("longhorn-manager-stale", nil, false),
	)

	if err := ensureManagerPodIdentityEnv(context.Background(), client, 50*time.Millisecond); err != nil {
		t.Errorf("ensureManagerPodIdentityEnv() error = %v, want nil (non-fatal warning path)", err)
	}
	if n := patchCount(client); n != 1 {
		t.Errorf("DaemonSet patched %d times, want 1", n)
	}
}

func TestEnsureManagerPodIdentityEnvMissingDaemonSetErrors(t *testing.T) {
	client := fake.NewSimpleClientset(managerPod("longhorn-manager-stale", nil, false))

	err := ensureManagerPodIdentityEnv(context.Background(), client, time.Second)
	if err == nil {
		t.Fatal("ensureManagerPodIdentityEnv() error = nil, want an error when the DaemonSet is absent")
	}
}
