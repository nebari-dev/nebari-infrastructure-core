package aws

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

const (
	longhornRepoName     = "longhorn"
	longhornRepoURL      = "https://charts.longhorn.io"
	longhornChartName    = "longhorn/longhorn"
	longhornChartVersion = "1.8.1"
	longhornNamespace    = "longhorn-system"
	longhornReleaseName  = "longhorn"
	longhornTimeout      = 5 * time.Minute
)

// longhornHelmValues generates the Helm values map for the Longhorn chart
// based on the AWS provider configuration.
func longhornHelmValues(cfg *Config) map[string]any {
	replicaCount := cfg.LonghornReplicaCount()

	persistence := map[string]any{
		"defaultClass":             true,
		"defaultClassReplicaCount": replicaCount,
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

	if cfg.Longhorn != nil && cfg.Longhorn.DedicatedNodes {
		settings["createDefaultDiskLabeledNodes"] = true

		nodeSelector := map[string]string{"node.longhorn.io/storage": "true"}
		if cfg.Longhorn.NodeSelector != nil {
			nodeSelector = cfg.Longhorn.NodeSelector
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

// loadLonghornChart locates and loads the Longhorn Helm chart.
// This is extracted to avoid duplication between install and upgrade operations.
func loadLonghornChart(chartPathOptions action.ChartPathOptions) (*chart.Chart, error) {
	chartPath, err := chartPathOptions.LocateChart(longhornChartName, cli.New())
	if err != nil {
		return nil, fmt.Errorf("failed to locate Longhorn chart: %w", err)
	}

	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load Longhorn chart: %w", err)
	}

	return loadedChart, nil
}

// installLonghorn installs or upgrades Longhorn on the cluster via Helm.
func installLonghorn(ctx context.Context, kubeconfigBytes []byte, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.installLonghorn")
	defer span.End()

	span.SetAttributes(
		attribute.String("chart_version", longhornChartVersion),
		attribute.Int("replica_count", cfg.LonghornReplicaCount()),
		attribute.Bool("dedicated_nodes", cfg.Longhorn != nil && cfg.Longhorn.DedicatedNodes),
	)

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	if err := helm.AddRepo(ctx, longhornRepoName, longhornRepoURL); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to add Longhorn Helm repository: %w", err)
	}

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, longhornNamespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	// Check if release already exists (idempotency)
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if _, err := histClient.Run(longhornReleaseName); err == nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Longhorn already installed, upgrading").
			WithResource("longhorn").
			WithAction("upgrading"))
		return upgradeLonghorn(ctx, actionConfig, cfg)
	}

	helmValues := longhornHelmValues(cfg)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Longhorn storage").
		WithResource("longhorn").
		WithAction("installing").
		WithMetadata("chart_version", longhornChartVersion))

	client := action.NewInstall(actionConfig)
	client.Namespace = longhornNamespace
	client.ReleaseName = longhornReleaseName
	client.CreateNamespace = true
	client.Wait = true
	client.Timeout = longhornTimeout
	client.Version = longhornChartVersion

	loadedChart, err := loadLonghornChart(client.ChartPathOptions)
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
		WithMetadata("chart_version", longhornChartVersion))

	return nil
}

// upgradeLonghorn upgrades an existing Longhorn installation.
func upgradeLonghorn(ctx context.Context, actionConfig *action.Configuration, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.upgradeLonghorn")
	defer span.End()

	helmValues := longhornHelmValues(cfg)

	client := action.NewUpgrade(actionConfig)
	client.Namespace = longhornNamespace
	client.Wait = true
	client.Timeout = longhornTimeout
	client.Version = longhornChartVersion

	loadedChart, err := loadLonghornChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(longhornReleaseName, loadedChart, helmValues)
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
		WithMetadata("chart_version", longhornChartVersion))

	return nil
}
