package longhorn

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/helm"
)

// IsInstalled reports whether the Longhorn Helm release NIC installs
// (ReleaseName in Namespace) is present on the cluster. A release whose latest
// revision is uninstalled (`helm uninstall --keep-history`) does not count.
// Unlike the history lookups in Install and Uninstall, any error other than "release
// not found" is returned, so callers that block on the answer never mistake an
// unreachable cluster for one without Longhorn.
func IsInstalled(ctx context.Context, kubeconfigBytes []byte) (bool, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.IsInstalled")
	defer span.End()

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return false, err
	}
	defer cleanup()

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, Namespace)
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to create Helm action config: %w", err)
	}

	installed, err := releaseInstalled(ctx, actionConfig)
	if err != nil {
		span.RecordError(err)
		return false, err
	}
	span.SetAttributes(attribute.Bool("installed", installed))
	return installed, nil
}

// releaseInstalled is the testable inner form of IsInstalled.
func releaseInstalled(ctx context.Context, actionConfig *action.Configuration) (bool, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "longhorn.releaseInstalled")
	defer span.End()

	history, err := action.NewHistory(actionConfig).Run(ReleaseName)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return false, nil
	}
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to read Longhorn Helm release history: %w", err)
	}
	// `helm uninstall --keep-history` marks only the latest revision as
	// uninstalled, so that revision alone decides.
	var latest *release.Release
	for _, rel := range history {
		if latest == nil || rel.Version > latest.Version {
			latest = rel
		}
	}
	return latest != nil && latest.Info != nil && latest.Info.Status != release.StatusUninstalled, nil
}
