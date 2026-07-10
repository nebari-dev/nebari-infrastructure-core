package aws

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/storage/driver"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/helm"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	gpuOperatorRepoName  = "nvidia"
	gpuOperatorRepoURL   = "https://helm.ngc.nvidia.com/nvidia"
	gpuOperatorChartName = "nvidia/gpu-operator"
	// gpuOperatorChartVersion pins the upstream NVIDIA gpu-operator Helm chart.
	// The NGC registry lists this chart with a leading "v" (e.g. "v26.3.2"), so
	// the prefix is intentional and required for LocateChart to resolve it -
	// unlike the bare-semver aws-load-balancer-controller chart. Check the NGC
	// catalog (https://catalog.ngc.nvidia.com/orgs/nvidia/helm-charts/gpu-operator)
	// for the current version when bumping.
	gpuOperatorChartVersion   = "v26.3.2"
	gpuOperatorNamespace      = "gpu-operator"
	gpuOperatorReleaseName    = "gpu-operator"
	gpuOperatorInstallTimeout = 10 * time.Minute

	// enabledKey is the gpu-operator chart's per-component on/off value key.
	enabledKey = "enabled"
)

// gpuOperatorHelmValues builds the Helm values for the gpu-operator chart.
//
// These are the AWS defaults: the EKS-optimized AL2023 NVIDIA AMI already ships
// the NVIDIA driver and the container toolkit, so the operator only needs to
// layer on the device plugin (what advertises nvidia.com/gpu). MOFED is
// disabled so the device plugin doesn't claim all /dev/infiniband/uverbs*
// devices and conflict with the AWS EFA device plugin on EFA-enabled nodes.
//
// When the operator grows to other providers these values will need to vary by
// provider; for now they are AWS-specific.
func gpuOperatorHelmValues() map[string]any {
	return map[string]any{
		"driver":  map[string]any{enabledKey: false},
		"toolkit": map[string]any{enabledKey: false},
		"devicePlugin": map[string]any{
			enabledKey: true,
			"env": []any{
				map[string]any{"name": "MOFED_ENABLED", "value": "false"},
			},
		},
	}
}

// loadGPUOperatorChart locates and loads the gpu-operator Helm chart.
func loadGPUOperatorChart(chartPathOptions action.ChartPathOptions) (*chart.Chart, error) {
	chartPath, err := chartPathOptions.LocateChart(gpuOperatorChartName, cli.New())
	if err != nil {
		return nil, fmt.Errorf("failed to locate GPU Operator chart: %w", err)
	}

	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load GPU Operator chart: %w", err)
	}

	return loadedChart, nil
}

// installGPUOperator installs or upgrades the NVIDIA GPU Operator via Helm.
// Safe to call on every deploy; it upgrades an existing release rather than
// failing. The operator needs no cloud IAM - it manages only in-cluster
// resources - so unlike the LBC and autoscaler there is no Pod Identity here.
func installGPUOperator(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.installGPUOperator")
	defer span.End()

	span.SetAttributes(attribute.String("chart_version", gpuOperatorChartVersion))

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	if err := helm.AddRepo(ctx, gpuOperatorRepoName, gpuOperatorRepoURL); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to add nvidia Helm repository: %w", err)
	}

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, gpuOperatorNamespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	switch _, err := histClient.Run(gpuOperatorReleaseName); {
	case err == nil:
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "GPU Operator already installed, upgrading").
			WithResource("gpu-operator").
			WithAction("upgrading"))
		return upgradeGPUOperator(ctx, actionConfig)
	case errors.Is(err, driver.ErrReleaseNotFound):
		// No existing release; fall through to fresh install.
	default:
		span.RecordError(err)
		return fmt.Errorf("failed to query Helm release history for %s: %w", gpuOperatorReleaseName, err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing GPU Operator").
		WithResource("gpu-operator").
		WithAction("installing").
		WithMetadata("chart_version", gpuOperatorChartVersion))

	client := action.NewInstall(actionConfig)
	client.Namespace = gpuOperatorNamespace
	client.ReleaseName = gpuOperatorReleaseName
	client.CreateNamespace = true
	client.Wait = true
	client.Timeout = gpuOperatorInstallTimeout
	client.Version = gpuOperatorChartVersion

	loadedChart, err := loadGPUOperatorChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// RunWithContext, not Run: Install.Run builds its own context.Background()
	// and ignores ours, so a cancellation/timeout during the install window
	// wouldn't propagate to Helm.
	release, err := client.RunWithContext(ctx, loadedChart, gpuOperatorHelmValues())
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install GPU Operator: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "GPU Operator installed").
		WithResource("gpu-operator").
		WithAction("installed").
		WithMetadata("chart_version", gpuOperatorChartVersion))

	return nil
}

// upgradeGPUOperator upgrades an existing gpu-operator release.
func upgradeGPUOperator(ctx context.Context, actionConfig *action.Configuration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.upgradeGPUOperator")
	defer span.End()

	span.SetAttributes(attribute.String("chart_version", gpuOperatorChartVersion))

	client := action.NewUpgrade(actionConfig)
	client.Namespace = gpuOperatorNamespace
	client.Wait = true
	client.Timeout = gpuOperatorInstallTimeout
	client.Version = gpuOperatorChartVersion

	loadedChart, err := loadGPUOperatorChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// RunWithContext, not Run: Upgrade.Run ignores the caller's context (it
	// builds its own context.Background()).
	release, err := client.RunWithContext(ctx, gpuOperatorReleaseName, loadedChart, gpuOperatorHelmValues())
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to upgrade GPU Operator: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "GPU Operator upgraded").
		WithResource("gpu-operator").
		WithAction("upgraded").
		WithMetadata("chart_version", gpuOperatorChartVersion))

	return nil
}

// uninstallGPUOperator removes the gpu-operator Helm release if present. A
// missing release is not an error. Called during cluster destroy, before tofu
// tears the cluster down. (Uninstall.Run has no RunWithContext in this Helm
// version, so it stays on Run.)
func uninstallGPUOperator(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.uninstallGPUOperator")
	defer span.End()

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, gpuOperatorNamespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	switch _, err := histClient.Run(gpuOperatorReleaseName); {
	case errors.Is(err, driver.ErrReleaseNotFound):
		// Nothing installed; nothing to do.
		return nil
	case err != nil:
		span.RecordError(err)
		return fmt.Errorf("failed to query Helm release history for %s: %w", gpuOperatorReleaseName, err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Uninstalling GPU Operator").
		WithResource("gpu-operator").
		WithAction("uninstalling"))

	// Wait=false on teardown: this only runs during Destroy, right before tofu
	// deletes the nodes, and the operator has no cloud resources to drain, so
	// blocking on graceful removal only adds latency and a failure surface.
	client := action.NewUninstall(actionConfig)
	client.Wait = false

	if _, err := client.Run(gpuOperatorReleaseName); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to uninstall GPU Operator: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "GPU Operator uninstalled").
		WithResource("gpu-operator").
		WithAction("uninstalled"))

	return nil
}
