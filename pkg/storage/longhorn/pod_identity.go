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
	// environment decides whether keyless backup deletion can authenticate.
	ManagerDaemonSetName = "longhorn-manager"

	// managerPodSelector selects the longhorn-manager DaemonSet's pods. Matches
	// the chart's DaemonSet selector.
	managerPodSelector = "app=" + ManagerDaemonSetName

	// containerCredentialsURIEnv and containerCredentialsTokenFileEnv are the two
	// environment variables the EKS Pod Identity mutating webhook injects into a
	// pod whose service account carries a Pod Identity association. They are the
	// credentials: AWS_IAM_ROLE_ARN in the credential Secret only unlocks
	// Longhorn's keyless mode, it is never handed to an AWS SDK.
	containerCredentialsURIEnv       = "AWS_CONTAINER_CREDENTIALS_FULL_URI"     //nolint:gosec // env var name, not a credential
	containerCredentialsTokenFileEnv = "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE" //nolint:gosec // env var name, not a credential

	// restartedAtAnnotation forces a DaemonSet rollout by changing the pod
	// template. Same key kubectl rollout restart uses, so a manual restart and
	// this one are indistinguishable to Longhorn and to anyone reading the
	// DaemonSet.
	restartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

	// managerRolloutTimeout bounds the wait for the rolled longhorn-manager pods
	// to come back carrying the injected credentials.
	managerRolloutTimeout = 5 * time.Minute
)

// EnsureManagerPodIdentityEnv makes keyless (EKS Pod Identity) Longhorn backup
// deletion work by guaranteeing every longhorn-manager pod has been mutated by
// the Pod Identity webhook, restarting the DaemonSet when any pod has not.
//
// Why this is needed (#500): longhorn-manager runs backup inspect/delete by
// exec'ing the engine binary, and go-common-libs builds that subprocess env as
// append(os.Environ(), <credential-Secret-derived vars>...) — so the subprocess
// authenticates with whatever ambient AWS credentials longhorn-manager's own pod
// environment carries. Under keyless auth the credential Secret holds only
// AWS_IAM_ROLE_ARN, which Longhorn never forwards to the SDK, so the *pod's*
// injected AWS_CONTAINER_CREDENTIALS_FULL_URI is the only credential source.
//
// A longhorn-manager pod created *before* the Pod Identity association existed
// never got that injection, and Longhorn's Helm chart does not roll the
// DaemonSet when only the association changed. That pod's AWS SDK then falls
// through to EC2 IMDS, which pods cannot reach, and every backup it owns fails
// with "no EC2 IMDS role found" — retention pruning silently never completes and
// Backup CRs pile up in state=Deleting. Backup *upload* is unaffected because it
// runs inside the long-lived instance-manager pod, which inherits its own env.
// This bites the enable-backups-on-an-existing-cluster path, where Longhorn was
// installed on an earlier deploy.
//
// Call only for keyless targets: static-key targets carry usable credentials in
// the Secret itself and need no ambient environment.
//
// Idempotent: on a cluster whose pods already carry the injection (the common
// case, including every fresh deploy) this is two API reads and no writes.
func EnsureManagerPodIdentityEnv(ctx context.Context, client kubernetes.Interface) error {
	return ensureManagerPodIdentityEnv(ctx, client, managerRolloutTimeout)
}

// ensureManagerPodIdentityEnv is the testable inner form, with the rollout wait
// bounded by an injectable timeout.
func ensureManagerPodIdentityEnv(ctx context.Context, client kubernetes.Interface, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.EnsureManagerPodIdentityEnv")
	defer span.End()

	stale, total, err := staleManagerPods(ctx, client)
	if err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(
		attribute.Int("manager_pods", total),
		attribute.Int("stale_manager_pods", len(stale)),
	)

	// No manager pods at all means Longhorn is not running yet (its pods are
	// created later in the deploy). Pods created from here on are mutated by the
	// webhook, so there is nothing to repair.
	if total == 0 || len(stale) == 0 {
		return nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress,
		"Restarting longhorn-manager so keyless backup credentials reach the backup engine").
		WithResource(ManagerDaemonSetName).
		WithAction("restarting").
		WithMetadata("stale_pods", strings.Join(stale, ",")))

	if err := restartManagerDaemonSet(ctx, client); err != nil {
		span.RecordError(err)
		return err
	}

	if err := waitForManagerPodIdentityEnv(ctx, client, timeout); err != nil {
		// Deliberately non-fatal. Backup creation and restore still work, so
		// failing the deploy here would be a worse outcome than a loud warning —
		// and the most likely cause is a cluster-level prerequisite (the
		// eks-pod-identity-agent addon or its webhook) that NIC cannot fix by
		// retrying.
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			"longhorn-manager restarted but keyless backup credentials are still not injected; backup retention pruning will not delete backups. Check that the eks-pod-identity-agent addon is installed and healthy").
			WithResource(ManagerDaemonSetName).
			WithMetadata("error", err.Error()))
		return nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess,
		"longhorn-manager restarted with keyless backup credentials injected").
		WithResource(ManagerDaemonSetName).
		WithAction("ready"))
	return nil
}

// staleManagerPods returns the names of live longhorn-manager pods that the Pod
// Identity webhook did not inject credentials into, alongside the number of live
// pods considered. Pods already terminating are ignored: during a rollout the
// outgoing pods are legitimately stale and must not re-trigger a restart.
func staleManagerPods(ctx context.Context, client kubernetes.Interface) (stale []string, total int, err error) {
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
		if !hasPodIdentityEnv(pod) {
			stale = append(stale, pod.Name)
		}
	}
	return stale, total, nil
}

// hasPodIdentityEnv reports whether the Pod Identity webhook injected container
// credentials into pod. The webhook mutates every container, so one container
// carrying both variables is proof the pod was mutated. Both are required: the
// URI without the token file cannot be authenticated against.
func hasPodIdentityEnv(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		var haveURI, haveToken bool
		for _, env := range container.Env {
			switch env.Name {
			case containerCredentialsURIEnv:
				haveURI = env.Value != ""
			case containerCredentialsTokenFileEnv:
				haveToken = env.Value != ""
			}
		}
		if haveURI && haveToken {
			return true
		}
	}
	return false
}

// restartManagerDaemonSet triggers a rolling restart of the longhorn-manager
// DaemonSet by stamping the pod template, so the recreated pods pass through the
// Pod Identity webhook. Restarting longhorn-manager is not disruptive to the
// data path: volume I/O is served by the instance-manager pods, which are left
// alone precisely because restarting *those* would detach live volumes.
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
			return fmt.Errorf("DaemonSet %s/%s not found; cannot restart it to pick up keyless backup credentials", Namespace, ManagerDaemonSetName)
		}
		return fmt.Errorf("restart DaemonSet %s/%s: %w", Namespace, ManagerDaemonSetName, err)
	}
	return nil
}

// waitForManagerPodIdentityEnv blocks until every live longhorn-manager pod
// carries the injected credentials and the DaemonSet reports all pods ready, or
// timeout elapses.
func waitForManagerPodIdentityEnv(ctx context.Context, client kubernetes.Interface, timeout time.Duration) error {
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
		stale, total, err := staleManagerPods(ctx, client)
		if err != nil {
			lastErr = err
			return false
		}
		if total == 0 {
			lastErr = fmt.Errorf("no live %s pods found", ManagerDaemonSetName)
			return false
		}
		if len(stale) > 0 {
			lastErr = fmt.Errorf("%s missing %s", strings.Join(stale, ","), containerCredentialsURIEnv)
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
			return fmt.Errorf("timeout waiting for %s pods to carry keyless backup credentials: %w (last state: %v)",
				ManagerDaemonSetName, ctx.Err(), lastErr)
		case <-ticker.C:
			if check() {
				return nil
			}
		}
	}
}
