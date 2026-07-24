package argocd

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

// staleManagerPod is a longhorn-manager pod created before the Pod Identity
// association existed, so the webhook never injected container credentials into
// it. It is the state that breaks keyless backup deletion (#500).
func staleManagerPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "longhorn-manager-stale",
			Namespace: longhorn.Namespace,
			Labels:    map[string]string{"app": longhorn.ManagerDaemonSetName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: longhorn.ManagerDaemonSetName}},
		},
	}
}

func managerDaemonSet() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      longhorn.ManagerDaemonSetName,
			Namespace: longhorn.Namespace,
		},
		Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, NumberReady: 1},
	}
}

func daemonSetPatched(client *fake.Clientset) bool {
	for _, action := range client.Actions() {
		if action.Matches("patch", "daemonsets") {
			return true
		}
	}
	return false
}

// podsGVR addresses pods in the fake clientset's object tracker.
var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// simulateManagerRollout stands in for the DaemonSet controller, which the fake
// clientset does not run: when the longhorn-manager DaemonSet is patched, replace
// the stale pod with one carrying the credentials the Pod Identity webhook would
// have injected. Without this the post-restart wait would block for its full
// real-world timeout.
//
// The tracker is mutated directly rather than through the typed client: a reactor
// runs while the fake clientset holds its own lock, so a client call from here
// would deadlock.
func simulateManagerRollout(t *testing.T, client *fake.Clientset) {
	t.Helper()
	client.PrependReactor("patch", "daemonsets", func(k8stesting.Action) (bool, runtime.Object, error) {
		rolled := staleManagerPod()
		rolled.Name = "longhorn-manager-rolled"
		rolled.Spec.Containers[0].Env = []corev1.EnvVar{
			{Name: "AWS_CONTAINER_CREDENTIALS_FULL_URI", Value: "http://169.254.170.23/v1/credentials"},
			{Name: "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE", Value: "/var/run/secrets/pods.eks.amazonaws.com/serviceaccount/eks-pod-identity-token"},
		}
		tracker := client.Tracker()
		if err := tracker.Delete(podsGVR, longhorn.Namespace, staleManagerPod().Name); err != nil {
			t.Errorf("simulate rollout: delete stale pod: %v", err)
		}
		if err := tracker.Add(rolled); err != nil {
			t.Errorf("simulate rollout: add rolled pod: %v", err)
		}
		// Fall through to the default reactor so the patch itself still applies.
		return false, nil, nil
	})
}

// A keyless target's only usable credentials are the ones the Pod Identity
// webhook injects into longhorn-manager, so a stale pod must be rolled.
func TestCreateLonghornBackupSecretKeylessRestartsStaleManager(t *testing.T) {
	client := fake.NewSimpleClientset(managerDaemonSet(), staleManagerPod())
	simulateManagerRollout(t, client)
	backupCfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{Bucket: "b", Region: "us-east-1"},
	}

	err := createLonghornBackupSecret(context.Background(), client, backupCfg, "arn:aws:iam::123456789012:role/longhorn-backup")
	if err != nil {
		t.Fatalf("createLonghornBackupSecret() error = %v", err)
	}
	if !daemonSetPatched(client) {
		t.Error("longhorn-manager DaemonSet was not restarted; keyless backup deletion stays broken (#500)")
	}
}

// A static-key target carries usable credentials in the Secret itself, which
// Longhorn does forward to the engine subprocess. Rolling longhorn-manager would
// be churn for no gain.
func TestCreateLonghornBackupSecretStaticKeysDoesNotRestartManager(t *testing.T) {
	t.Setenv("TEST_LONGHORN_AK", "AKIAEXAMPLE")
	t.Setenv("TEST_LONGHORN_SK", "secret")

	client := fake.NewSimpleClientset(managerDaemonSet(), staleManagerPod())
	backupCfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{
			Bucket:             "b",
			Region:             "us-east-1",
			AccessKeyIDEnv:     "TEST_LONGHORN_AK",
			SecretAccessKeyEnv: "TEST_LONGHORN_SK",
		},
	}

	err := createLonghornBackupSecret(context.Background(), client, backupCfg, "")
	if err != nil {
		t.Fatalf("createLonghornBackupSecret() error = %v", err)
	}
	if daemonSetPatched(client) {
		t.Error("longhorn-manager DaemonSet was restarted for a static-key target; no ambient credentials are needed there")
	}
}

// The credential Secret is what the BackupTarget binds to, so a failure to
// repair pods must not prevent it from being applied.
func TestCreateLonghornBackupSecretAppliesSecretEvenWhenManagerRepairFails(t *testing.T) {
	// No DaemonSet in the cluster: EnsureManagerPodIdentityEnv cannot restart it.
	client := fake.NewSimpleClientset(staleManagerPod())
	backupCfg := &config.LonghornBackupConfig{
		S3: &config.S3BackupTarget{Bucket: "b", Region: "us-east-1"},
	}

	err := createLonghornBackupSecret(context.Background(), client, backupCfg, "arn:aws:iam::123456789012:role/longhorn-backup")
	if err != nil {
		t.Fatalf("createLonghornBackupSecret() error = %v, want nil (repair failure is a warning, not fatal)", err)
	}

	secret, err := client.CoreV1().Secrets(longhorn.Namespace).Get(
		context.Background(), longhorn.BackupCredentialSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("credential Secret was not applied: %v", err)
	}
	if got := getSecretValue(secret, "AWS_IAM_ROLE_ARN"); got == "" {
		t.Error("credential Secret is missing AWS_IAM_ROLE_ARN; Longhorn rejects a keyless secret without it")
	}
}
