package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

const (
	// containerCredentialsURIEnv and containerCredentialsTokenFileEnv are the two
	// environment variables the EKS Pod Identity mutating webhook injects into a
	// pod whose service account carries a Pod Identity association. They are the
	// credentials: AWS_IAM_ROLE_ARN in Longhorn's credential Secret only unlocks
	// its keyless mode, it is never handed to an AWS SDK.
	containerCredentialsURIEnv       = "AWS_CONTAINER_CREDENTIALS_FULL_URI"     //nolint:gosec // env var name, not a credential
	containerCredentialsTokenFileEnv = "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE" //nolint:gosec // env var name, not a credential

	// podIdentityAgentNamespace / podIdentityAgentDaemonSetName locate the EKS
	// Pod Identity Agent addon's DaemonSet. Its presence is the prerequisite for
	// the credential injection this file repairs; AWS's own verification step is
	// checking for these pods in kube-system.
	podIdentityAgentNamespace     = "kube-system"
	podIdentityAgentDaemonSetName = "eks-pod-identity-agent"
)

// podIdentityCredentialsDesc names the injected pair in operator-facing
// messages. Both variables are named because both are required: the URI
// without the token file cannot be authenticated against.
const podIdentityCredentialsDesc = "EKS Pod Identity container credentials (" +
	containerCredentialsURIEnv + " and " + containerCredentialsTokenFileEnv + ")"

// repairLonghornBackupPodIdentity makes keyless (EKS Pod Identity) Longhorn
// backup deletion work by guaranteeing every longhorn-manager pod has been
// mutated by the Pod Identity webhook, restarting the DaemonSet when any has
// not.
//
// Why this is needed (#500): longhorn-manager runs backup inspect/delete by
// exec'ing the engine binary, and go-common-libs builds that subprocess env as
// append(os.Environ(), <credential-Secret-derived vars>...), so the subprocess
// authenticates with whatever ambient AWS credentials longhorn-manager's own
// pod environment carries. Under keyless auth the credential Secret holds only
// AWS_IAM_ROLE_ARN, which Longhorn never forwards to the SDK, so the *pod's*
// injected AWS_CONTAINER_CREDENTIALS_FULL_URI is the only credential source.
//
// A longhorn-manager pod created *before* the Pod Identity association existed
// never got that injection, and Longhorn's Helm chart does not roll the
// DaemonSet when only the association changed. That pod's AWS SDK then falls
// through to EC2 IMDS, which pods cannot reach, and every backup it owns fails
// with "no EC2 IMDS role found": retention pruning silently never completes
// and Backup CRs pile up in state=Deleting. Backup *upload* is unaffected
// because it runs inside the long-lived instance-manager pod, which inherits
// its own env. This bites the enable-backups-on-an-existing-cluster path,
// where Longhorn was installed on an earlier deploy.
//
// Called from Deploy after tf.Apply (which ensures the association exists) and
// after longhorn.Install, and only for keyless targets: static-key targets
// carry usable credentials in the Secret itself and need no ambient
// environment.
//
// Returns an error only when the deploy should stop (the context was
// canceled). Every other failure degrades to a warning: backup creation and
// restore still work without the repair, so failing the deploy would be a
// worse outcome, and the likely causes are cluster prerequisites NIC cannot
// fix by retrying.
func repairLonghornBackupPodIdentity(ctx context.Context, client kubernetes.Interface) error {
	return repairLonghornBackupPodIdentityWithin(ctx, client, 0)
}

// repairLonghornBackupPodIdentityWithin is the testable inner form; timeout <= 0
// uses longhorn's default rollout timeout.
func repairLonghornBackupPodIdentityWithin(ctx context.Context, client kubernetes.Interface, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.repairLonghornBackupPodIdentity")
	defer span.End()

	stale, total, err := longhorn.StaleManagerPods(ctx, client, hasPodIdentityEnv)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			"Could not check longhorn-manager pods for keyless backup credentials; backup retention pruning may not delete backups").
			WithResource(longhorn.ManagerDaemonSetName).
			WithMetadata("error", err.Error()))
		return nil
	}
	// Deploy calls this after longhorn.Install has waited for the chart, so the
	// manager pods normally exist already; total == 0 just means there is
	// nothing to repair, and any pod created later is mutated by the webhook.
	if total == 0 || len(stale) == 0 {
		return nil
	}

	// Rolling the DaemonSet only helps if the Pod Identity webhook will mutate
	// the replacement pods, and that requires the eks-pod-identity-agent addon.
	// Checking it first turns "wait five minutes, then guess" into a fact stated
	// immediately, and keeps this function read-only when a roll cannot succeed.
	// An inconclusive check (API error) proceeds with the roll as before.
	if healthy, detail, err := podIdentityAgentHealthy(ctx, client); err == nil && !healthy {
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			fmt.Sprintf("longhorn-manager pods (%s) are missing %s, but the %s addon is %s, so restarting them cannot inject the credentials. Install or repair the addon and re-run nic deploy; backup retention pruning will not delete backups until then",
				strings.Join(stale, ","), podIdentityCredentialsDesc, podIdentityAgentDaemonSetName, detail)).
			WithResource(longhorn.ManagerDaemonSetName))
		return nil
	} else if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		span.RecordError(err)
	}

	if err := longhorn.EnsureManagerPods(ctx, client, hasPodIdentityEnv, podIdentityCredentialsDesc, timeout); err != nil {
		// A user abort is not a broken cluster: propagate it instead of blaming
		// the addon.
		if errors.Is(err, context.Canceled) {
			return err
		}
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			"longhorn-manager restarted but keyless backup credentials are still not injected; backup retention pruning will not delete backups. Check that the eks-pod-identity-agent addon is installed and healthy").
			WithResource(longhorn.ManagerDaemonSetName).
			WithMetadata("error", err.Error()))
	}
	return nil
}

// podIdentityAgentHealthy reports whether the EKS Pod Identity Agent addon's
// DaemonSet exists and has at least one ready pod. detail is an operator-facing
// description of what is wrong when healthy is false.
func podIdentityAgentHealthy(ctx context.Context, client kubernetes.Interface) (healthy bool, detail string, err error) {
	ds, err := client.AppsV1().DaemonSets(podIdentityAgentNamespace).Get(ctx, podIdentityAgentDaemonSetName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return false, "not installed", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("get DaemonSet %s/%s: %w", podIdentityAgentNamespace, podIdentityAgentDaemonSetName, err)
	}
	if ds.Status.NumberReady == 0 {
		return false, "installed but has no ready pods", nil
	}
	return true, "", nil
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
