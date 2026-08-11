package longhorn

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"helm.sh/helm/v3/pkg/action"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/helm"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const uninstallTimeout = 10 * time.Minute

// deletingConfirmationFlagName is the Longhorn setting the chart's pre-delete
// hook (the longhorn-uninstall job) checks before agreeing to run
// `longhorn-manager uninstall`. Longhorn defaults it to "false" and NIC
// deliberately leaves it that way at install so a manual `helm uninstall
// longhorn` cannot wipe volume data.
const deletingConfirmationFlagName = "deleting-confirmation-flag"

// settingsResource identifies Longhorn's Setting custom resource, which is
// how every Longhorn setting (including the deleting-confirmation-flag) is
// stored on the cluster.
var settingsResource = schema.GroupVersionResource{
	Group:    "longhorn.io",
	Version:  "v1beta2",
	Resource: "settings",
}

// Uninstall removes the Longhorn Helm release from the cluster the
// kubeconfigBytes connect to. Idempotent: returns nil if no release exists.
//
// Before running `helm uninstall`, it sets Longhorn's
// deleting-confirmation-flag to "true", so the chart's pre-delete hook refuses
// to deprovision Longhorn otherwise. The flag is deliberately left at
// Longhorn's default ("false") during the cluster's life so a manual
// `helm uninstall longhorn` cannot wipe volume data.
//
// Per ADR-0002 §"Destroy Flow", this must run before infrastructure
// teardown — Longhorn-backed PVs left in the cluster can block node group
// deletion (CSI finalizers wait for the engine to clean up volumes).
func Uninstall(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.Uninstall")
	defer span.End()

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, Namespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	dyn, err := newDynamicClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create dynamic Kubernetes client: %w", err)
	}

	if err := uninstallRelease(ctx, actionConfig, dyn); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// uninstallRelease is the testable inner form of Uninstall. It checks for
// an existing release, sets Longhorn's deleting-confirmation-flag, and runs
// `helm uninstall` only if a release is present.
func uninstallRelease(ctx context.Context, actionConfig *action.Configuration, dyn dynamic.Interface) error {
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if _, err := histClient.Run(ReleaseName); err != nil {
		// No release found — nothing to uninstall.
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Longhorn release not present, skipping uninstall").
			WithResource("longhorn").
			WithAction("uninstalling"))
		return nil
	}

	if err := setDeletingConfirmationFlag(ctx, dyn); err != nil {
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Uninstalling Longhorn storage").
		WithResource("longhorn").
		WithAction("uninstalling"))

	client := action.NewUninstall(actionConfig)
	client.Wait = true
	client.Timeout = uninstallTimeout

	if _, err := client.Run(ReleaseName); err != nil {
		return fmt.Errorf("failed to uninstall Longhorn: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Longhorn storage uninstalled").
		WithResource("longhorn").
		WithAction("uninstalled"))
	return nil
}

// setDeletingConfirmationFlag patches the deleting-confirmation-flag Setting
// to "true" so the chart's pre-delete hook is allowed to deprovision Longhorn.
// The hook reads the Setting at the moment it runs, so the patch takes effect
// immediately — no reconciliation wait is needed. A missing Setting (or the
// Setting CRD itself already gone) is treated as success: there is nothing
// left for the flag to guard. Any other failure is returned so the destroy
// surfaces a clear error instead of the hook job's opaque
// BackoffLimitExceeded after a 10-minute timeout.
func setDeletingConfirmationFlag(ctx context.Context, dyn dynamic.Interface) error {
	patch := []byte(`{"value":"true"}`)
	_, err := dyn.Resource(settingsResource).Namespace(Namespace).
		Patch(ctx, deletingConfirmationFlagName, types.MergePatchType, patch, metav1.PatchOptions{})
	if apierrors.IsNotFound(err) {
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			"Longhorn deleting-confirmation-flag setting not found, continuing uninstall").
			WithResource("longhorn").
			WithAction("uninstalling"))
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to set Longhorn %s setting to true: %w", deletingConfirmationFlagName, err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Enabled Longhorn deleting-confirmation-flag").
		WithResource("longhorn").
		WithAction("uninstalling"))
	return nil
}

// newDynamicClient builds a dynamic Kubernetes client from raw kubeconfig
// bytes. Longhorn settings are custom resources, so the typed clientset from
// newK8sClient cannot reach them.
func newDynamicClient(kubeconfigBytes []byte) (dynamic.Interface, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	return dynamic.NewForConfig(restConfig)
}
