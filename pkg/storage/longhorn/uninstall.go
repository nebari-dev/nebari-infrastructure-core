package longhorn

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"helm.sh/helm/v3/pkg/action"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/helm"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const uninstallTimeout = 10 * time.Minute

// Uninstall removes the Longhorn Helm release from the cluster the
// kubeconfigBytes connect to. Idempotent: returns nil if no release exists.
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

	if err := uninstallRelease(ctx, actionConfig); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// uninstallRelease is the testable inner form of Uninstall. It checks for
// an existing release and runs `helm uninstall` only if one is present.
func uninstallRelease(ctx context.Context, actionConfig *action.Configuration) error {
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if _, err := histClient.Run(ReleaseName); err != nil {
		// No release found — nothing to uninstall.
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Longhorn release not present, skipping uninstall").
			WithResource("longhorn").
			WithAction("uninstalling"))
		return nil
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
