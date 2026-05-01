package longhorn

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/helm"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const installTimeout = 10 * time.Minute

// Install installs (or upgrades, if a release exists) Longhorn on the cluster
// the kubeconfigBytes connect to.
//
// On a fresh cluster, the iSCSI prerequisite DaemonSet is deployed and waited
// on before the Helm install. On clusters with an existing release, the iSCSI
// step is skipped (it was already provisioned on first install) and the Helm
// release is upgraded in place.
//
// Install is idempotent: re-running on an installed cluster is a no-op modulo
// any Config changes that would shift the rendered Helm values.
func Install(ctx context.Context, kubeconfigBytes []byte, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.Install")
	defer span.End()

	span.SetAttributes(
		attribute.String("chart_version", ChartVersion),
		attribute.Int("replica_count", cfg.Replicas()),
		attribute.Bool("dedicated_nodes", cfg != nil && cfg.DedicatedNodes),
	)

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	if err := helm.AddRepo(ctx, chartRepoName, chartRepoURL); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to add Longhorn Helm repository: %w", err)
	}

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, Namespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if _, err := histClient.Run(ReleaseName); err == nil {
		// Release exists — iSCSI was provisioned on initial install, so skip
		// the 3-minute DaemonSet readiness wait and go straight to upgrade.
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Longhorn already installed, upgrading").
			WithResource("longhorn").
			WithAction("upgrading"))
		return upgrade(ctx, actionConfig, cfg)
	}

	if err := ensureISCSI(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install iSCSI prerequisites: %w", err)
	}

	helmValues := buildHelmValues(cfg)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Longhorn storage").
		WithResource("longhorn").
		WithAction("installing").
		WithMetadata("chart_version", ChartVersion))

	client := action.NewInstall(actionConfig)
	client.Namespace = Namespace
	client.ReleaseName = ReleaseName
	client.CreateNamespace = true
	client.Wait = true
	client.Timeout = installTimeout
	client.Version = ChartVersion

	loadedChart, err := loadChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(loadedChart, helmValues)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install Longhorn: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Longhorn storage installed").
		WithResource("longhorn").
		WithAction("installed").
		WithMetadata("chart_version", ChartVersion))

	return nil
}

func upgrade(ctx context.Context, actionConfig *action.Configuration, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "longhorn.upgrade")
	defer span.End()

	helmValues := buildHelmValues(cfg)

	client := action.NewUpgrade(actionConfig)
	client.Namespace = Namespace
	client.Wait = true
	client.Timeout = installTimeout
	client.Version = ChartVersion

	loadedChart, err := loadChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(ReleaseName, loadedChart, helmValues)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to upgrade Longhorn: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Longhorn storage upgraded").
		WithResource("longhorn").
		WithAction("upgraded").
		WithMetadata("chart_version", ChartVersion))

	return nil
}

func loadChart(chartPathOptions action.ChartPathOptions) (*chart.Chart, error) {
	chartPath, err := chartPathOptions.LocateChart(chartName, cli.New())
	if err != nil {
		return nil, fmt.Errorf("failed to locate Longhorn chart: %w", err)
	}

	loaded, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load Longhorn chart: %w", err)
	}

	return loaded, nil
}

// buildHelmValues turns a Config into the values map passed to the Longhorn
// Helm chart.
func buildHelmValues(cfg *Config) map[string]any {
	persistence := map[string]any{
		"defaultClass":             true,
		"defaultClassReplicaCount": cfg.Replicas(),
		"defaultFsType":            "ext4",
	}

	settings := map[string]any{
		"replicaZoneSoftAntiAffinity": "true",
		"replicaAutoBalance":          "best-effort",
	}

	values := map[string]any{
		"persistence":     persistence,
		"defaultSettings": settings,
	}

	if cfg != nil && cfg.DedicatedNodes {
		settings["createDefaultDiskLabeledNodes"] = true

		nodeSelector := map[string]string{"node.longhorn.io/storage": "true"}
		if cfg.NodeSelector != nil {
			nodeSelector = cfg.NodeSelector
		}

		tolerations := []map[string]string{
			{
				"key":      "node.longhorn.io/storage",
				"operator": "Exists",
				"effect":   "NoSchedule",
			},
		}

		values["longhornManager"] = map[string]any{
			"nodeSelector": nodeSelector,
			"tolerations":  tolerations,
		}
		values["longhornDriver"] = map[string]any{
			"nodeSelector": nodeSelector,
			"tolerations":  tolerations,
		}
	}

	return values
}
