package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

// podIdentityEnv is the pair the EKS Pod Identity webhook injects.
func podIdentityEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: containerCredentialsURIEnv, Value: "http://169.254.170.23/v1/credentials"},
		{Name: containerCredentialsTokenFileEnv, Value: "/var/run/secrets/pods.eks.amazonaws.com/serviceaccount/eks-pod-identity-token"},
	}
}

func managerPod(name string, env []corev1.EnvVar) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: longhorn.Namespace,
			Labels:    map[string]string{"app": longhorn.ManagerDaemonSetName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: longhorn.ManagerDaemonSetName, Env: env}},
		},
	}
}

// managerDaemonSet reports desired == ready so the post-restart wait passes as
// soon as the pods carry the injected credentials.
func managerDaemonSet(ready int32) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: longhorn.ManagerDaemonSetName, Namespace: longhorn.Namespace},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: ready,
			NumberReady:            ready,
		},
	}
}

// podIdentityAgentDaemonSet is the eks-pod-identity-agent addon's DaemonSet in
// kube-system, whose presence gates whether a longhorn-manager roll can help.
func podIdentityAgentDaemonSet(ready int32) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: podIdentityAgentDaemonSetName, Namespace: podIdentityAgentNamespace},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: ready,
			NumberReady:            ready,
		},
	}
}

// managerPatchCount counts longhorn-manager DaemonSet patches, which is how the
// restart is issued.
func managerPatchCount(client *fake.Clientset) int {
	n := 0
	for _, action := range client.Actions() {
		if action.Matches("patch", "daemonsets") {
			n++
		}
	}
	return n
}

// podsGVR addresses pods in the fake clientset's object tracker.
var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// simulateManagerRollout stands in for the DaemonSet controller, which the fake
// clientset does not run: when the longhorn-manager DaemonSet is patched,
// replace the stale pod with one carrying the credentials the Pod Identity
// webhook would have injected. Without this the post-restart wait would block
// for its full timeout.
//
// The tracker is mutated directly rather than through the typed client: a
// reactor runs while the fake clientset holds its own lock, so a client call
// from here would deadlock.
func simulateManagerRollout(t *testing.T, client *fake.Clientset, stalePodName string) {
	t.Helper()
	client.PrependReactor("patch", "daemonsets", func(k8stesting.Action) (bool, runtime.Object, error) {
		rolled := managerPod("longhorn-manager-rolled", podIdentityEnv())
		tracker := client.Tracker()
		if err := tracker.Delete(podsGVR, longhorn.Namespace, stalePodName); err != nil {
			t.Errorf("simulate rollout: delete stale pod: %v", err)
		}
		if err := tracker.Add(rolled); err != nil {
			t.Errorf("simulate rollout: add rolled pod: %v", err)
		}
		// Fall through to the default reactor so the patch itself still applies.
		return false, nil, nil
	})
}

func TestHasPodIdentityEnv(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "both vars injected",
			pod:  managerPod("p", podIdentityEnv()),
			want: true,
		},
		{
			name: "no env at all (pod predates the association)",
			pod:  managerPod("p", nil),
			want: false,
		},
		{
			name: "uri without token file is not usable credentials",
			pod:  managerPod("p", []corev1.EnvVar{{Name: containerCredentialsURIEnv, Value: "http://169.254.170.23/v1/credentials"}}),
			want: false,
		},
		{
			name: "empty values do not count as injected",
			pod: managerPod("p", []corev1.EnvVar{
				{Name: containerCredentialsURIEnv, Value: ""},
				{Name: containerCredentialsTokenFileEnv, Value: ""},
			}),
			want: false,
		},
		{
			name: "AWS_IAM_ROLE_ARN alone is a sentinel, not credentials",
			pod:  managerPod("p", []corev1.EnvVar{{Name: "AWS_IAM_ROLE_ARN", Value: "arn:aws:iam::1:role/r"}}),
			want: false,
		},
		{
			name: "injection found on a sidecar container",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{Name: "other", Env: nil},
					{Name: longhorn.ManagerDaemonSetName, Env: podIdentityEnv()},
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

func TestRepairLonghornBackupPodIdentityNoRestartWhenInjected(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(2),
		podIdentityAgentDaemonSet(2),
		managerPod("longhorn-manager-a", podIdentityEnv()),
		managerPod("longhorn-manager-b", podIdentityEnv()),
	)

	if err := repairLonghornBackupPodIdentityWithin(context.Background(), client, time.Second); err != nil {
		t.Fatalf("repairLonghornBackupPodIdentityWithin() error = %v", err)
	}
	if n := managerPatchCount(client); n != 0 {
		t.Errorf("DaemonSet patched %d times, want 0: already-injected pods must not be rolled", n)
	}
}

func TestRepairLonghornBackupPodIdentityNoPodsIsNoOp(t *testing.T) {
	// No manager pods: nothing to repair. Deploy calls this after
	// longhorn.Install, and any pod created later passes through the webhook.
	client := fake.NewSimpleClientset(managerDaemonSet(0), podIdentityAgentDaemonSet(1))

	if err := repairLonghornBackupPodIdentityWithin(context.Background(), client, time.Second); err != nil {
		t.Fatalf("repairLonghornBackupPodIdentityWithin() error = %v", err)
	}
	if n := managerPatchCount(client); n != 0 {
		t.Errorf("DaemonSet patched %d times, want 0", n)
	}
}

// A stale pod with a healthy webhook agent is the #500 state: the roll repairs
// it, and the repaired cluster comes back clean.
func TestRepairLonghornBackupPodIdentityRestartsStaleManager(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(1),
		podIdentityAgentDaemonSet(1),
		managerPod("longhorn-manager-stale", nil),
	)
	simulateManagerRollout(t, client, "longhorn-manager-stale")

	if err := repairLonghornBackupPodIdentityWithin(context.Background(), client, time.Second); err != nil {
		t.Fatalf("repairLonghornBackupPodIdentityWithin() error = %v", err)
	}
	if n := managerPatchCount(client); n != 1 {
		t.Fatalf("DaemonSet patched %d times, want 1: stale pod must trigger a roll (#500)", n)
	}
}

// Rolling cannot inject anything when the eks-pod-identity-agent addon is
// absent, so the repair must say so immediately instead of rolling and waiting
// out the timeout — and must stay read-only.
func TestRepairLonghornBackupPodIdentitySkipsRollWhenAgentMissing(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(1),
		managerPod("longhorn-manager-stale", nil),
	)

	if err := repairLonghornBackupPodIdentityWithin(context.Background(), client, time.Second); err != nil {
		t.Fatalf("repairLonghornBackupPodIdentityWithin() error = %v, want nil (warning, not failure)", err)
	}
	if n := managerPatchCount(client); n != 0 {
		t.Errorf("DaemonSet patched %d times, want 0: a roll without the webhook agent is futile churn", n)
	}
}

// An installed addon with no ready pods cannot mutate anything either.
func TestRepairLonghornBackupPodIdentitySkipsRollWhenAgentUnhealthy(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(1),
		podIdentityAgentDaemonSet(0),
		managerPod("longhorn-manager-stale", nil),
	)

	if err := repairLonghornBackupPodIdentityWithin(context.Background(), client, time.Second); err != nil {
		t.Fatalf("repairLonghornBackupPodIdentityWithin() error = %v, want nil (warning, not failure)", err)
	}
	if n := managerPatchCount(client); n != 0 {
		t.Errorf("DaemonSet patched %d times, want 0", n)
	}
}

// The webhook agent looks healthy but the rolled pods still come back without
// the credentials (e.g. the association was deleted out of band). Backup
// creation and restore still work, so this must warn rather than fail the
// deploy.
func TestRepairLonghornBackupPodIdentityWarnsWhenRollDoesNotHelp(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(1),
		podIdentityAgentDaemonSet(1),
		managerPod("longhorn-manager-stale", nil),
	)

	if err := repairLonghornBackupPodIdentityWithin(context.Background(), client, 50*time.Millisecond); err != nil {
		t.Errorf("repairLonghornBackupPodIdentityWithin() error = %v, want nil (non-fatal warning path)", err)
	}
	if n := managerPatchCount(client); n != 1 {
		t.Errorf("DaemonSet patched %d times, want 1", n)
	}
}

// A user abort is not a broken cluster: cancellation must propagate as an
// error instead of turning into an addon-blaming warning.
func TestRepairLonghornBackupPodIdentitySurfacesCancellation(t *testing.T) {
	client := fake.NewSimpleClientset(
		managerDaemonSet(1),
		podIdentityAgentDaemonSet(1),
		managerPod("longhorn-manager-stale", nil),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := repairLonghornBackupPodIdentityWithin(ctx, client, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled in the chain", err)
	}
}

// A missing longhorn-manager DaemonSet (while its pods somehow linger) cannot
// be restarted; that degrades to a warning because the credential Secret and
// backup creation are unaffected.
func TestRepairLonghornBackupPodIdentityMissingManagerDaemonSetWarns(t *testing.T) {
	client := fake.NewSimpleClientset(
		podIdentityAgentDaemonSet(1),
		managerPod("longhorn-manager-stale", nil),
	)

	if err := repairLonghornBackupPodIdentityWithin(context.Background(), client, time.Second); err != nil {
		t.Errorf("repairLonghornBackupPodIdentityWithin() error = %v, want nil (repair failure is a warning, not fatal)", err)
	}
}
