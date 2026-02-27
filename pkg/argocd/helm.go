package argocd

import (
	"context"
	"fmt"

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
	repoName  = "argo"
	repoURL   = "https://argoproj.github.io/argo-helm"
	chartName = "argo/argo-cd"
)

// loadArgoCDChart locates and loads the Argo CD Helm chart.
// This is extracted to avoid duplication between install and upgrade operations.
func loadArgoCDChart(chartPathOptions action.ChartPathOptions) (*chart.Chart, error) {
	chartPath, err := chartPathOptions.LocateChart(chartName, cli.New())
	if err != nil {
		return nil, fmt.Errorf("failed to locate Argo CD chart: %w", err)
	}

	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load Argo CD chart: %w", err)
	}

	return loadedChart, nil
}

// InstallHelm installs Argo CD using the Helm Go SDK
func InstallHelm(ctx context.Context, kubeconfigBytes []byte, config Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.InstallHelm")
	defer span.End()

	span.SetAttributes(
		attribute.String("version", config.Version),
		attribute.String("namespace", config.Namespace),
		attribute.String("release_name", config.ReleaseName),
	)

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, config.Namespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	// Check if release already exists (idempotency)
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if _, err := histClient.Run(config.ReleaseName); err == nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Argo CD already installed, upgrading").
			WithResource("argocd").
			WithAction("upgrading"))
		return upgradeHelm(ctx, actionConfig, config)
	}

	if err := helm.AddRepo(ctx, repoName, repoURL); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to add Argo CD Helm repository: %w", err)
	}

	client := action.NewInstall(actionConfig)
	client.Namespace = config.Namespace
	client.ReleaseName = config.ReleaseName
	client.CreateNamespace = true
	client.Wait = true
	client.Timeout = config.Timeout

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Argo CD Helm chart").
		WithResource("argocd").
		WithAction("installing").
		WithMetadata("chart_version", config.Version))

	chart, err := loadArgoCDChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(chart, config.Values)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install Argo CD: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Argo CD Helm chart installed").
		WithResource("argocd").
		WithAction("installed").
		WithMetadata("chart_version", config.Version).
		WithMetadata("release_version", release.Version))

	return nil
}

// upgradeHelm upgrades an existing Argo CD installation
func upgradeHelm(ctx context.Context, actionConfig *action.Configuration, config Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.upgradeHelm")
	defer span.End()

	client := action.NewUpgrade(actionConfig)
	client.Namespace = config.Namespace
	client.Wait = true
	client.Timeout = config.Timeout

	// Locate and load the chart
	chart, err := loadArgoCDChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Upgrade the chart
	release, err := client.Run(config.ReleaseName, chart, config.Values)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to upgrade Argo CD: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Argo CD Helm chart upgraded").
		WithResource("argocd").
		WithAction("upgraded").
		WithMetadata("chart_version", config.Version).
		WithMetadata("release_version", release.Version))

	return nil
}
