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
	fsxCSIRepoName       = "aws-fsx-openzfs-csi-driver"
	fsxCSIRepoURL        = "https://kubernetes-sigs.github.io/aws-fsx-openzfs-csi-driver"
	fsxCSIChartName      = "aws-fsx-openzfs-csi-driver/aws-fsx-openzfs-csi-driver"
	fsxCSINamespace      = "kube-system"
	fsxCSIReleaseName    = "aws-fsx-openzfs-csi-driver"
	fsxCSIInstallTimeout = 5 * time.Minute

	// fsxCSIControllerServiceAccount must match the service account the
	// terraform-aws-eks-cluster module's Pod Identity association targets.
	fsxCSIControllerServiceAccount = "fsx-openzfs-csi-controller-sa"
)

// fsxOpenZFSCSIHelmValues builds the Helm values for the FSx for OpenZFS CSI
// driver chart. The controller authenticates through the EKS Pod Identity
// association the module provisions, which matches on (cluster, namespace,
// service account name), so the name is set explicitly rather than relying on
// the chart default. The region is set so the controller does not fall back
// to IMDS, which the EKS node hop limit can block from pods. The node
// DaemonSet tolerates every taint so pods on GPU or dedicated storage nodes can
// still mount volumes.
func fsxOpenZFSCSIHelmValues(cfg *Config) map[string]any {
	return map[string]any{
		"controller": map[string]any{
			"region": cfg.Region,
			"serviceAccount": map[string]any{
				"create": true,
				"name":   fsxCSIControllerServiceAccount,
			},
		},
		"node": map[string]any{
			"tolerateAllTaints": true,
		},
	}
}

// loadFSxOpenZFSCSIChart locates and loads the FSx for OpenZFS CSI driver Helm chart.
func loadFSxOpenZFSCSIChart(chartPathOptions action.ChartPathOptions) (*chart.Chart, error) {
	chartPath, err := chartPathOptions.LocateChart(fsxCSIChartName, cli.New())
	if err != nil {
		return nil, fmt.Errorf("failed to locate FSx for OpenZFS CSI driver chart: %w", err)
	}

	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load FSx for OpenZFS CSI driver chart: %w", err)
	}

	return loadedChart, nil
}

// installFSxOpenZFSCSIDriver installs or upgrades the FSx for OpenZFS CSI
// driver on the cluster via Helm. Safe to call on every deploy; it will
// upgrade an existing release rather than fail.
func installFSxOpenZFSCSIDriver(ctx context.Context, kubeconfigBytes []byte, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.installFSxOpenZFSCSIDriver")
	defer span.End()

	chartVersion := cfg.FSxOpenZFSCSIChartVersion()
	span.SetAttributes(attribute.String("chart_version", chartVersion))

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	if err := helm.AddRepo(ctx, fsxCSIRepoName, fsxCSIRepoURL); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to add FSx for OpenZFS CSI driver Helm repository: %w", err)
	}

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, fsxCSINamespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	switch _, err := histClient.Run(fsxCSIReleaseName); {
	case err == nil:
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "FSx for OpenZFS CSI driver already installed, upgrading").
			WithResource("fsx-openzfs-csi-driver").
			WithAction("upgrading"))
		return upgradeFSxOpenZFSCSIDriver(ctx, actionConfig, cfg)
	case errors.Is(err, driver.ErrReleaseNotFound):
		// No existing release; fall through to fresh install.
	default:
		span.RecordError(err)
		return fmt.Errorf("failed to query Helm release history for %s: %w", fsxCSIReleaseName, err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing FSx for OpenZFS CSI driver").
		WithResource("fsx-openzfs-csi-driver").
		WithAction("installing").
		WithMetadata("chart_version", chartVersion))

	client := action.NewInstall(actionConfig)
	client.Namespace = fsxCSINamespace
	client.ReleaseName = fsxCSIReleaseName
	client.CreateNamespace = false
	client.Wait = true
	client.Timeout = fsxCSIInstallTimeout
	client.Version = chartVersion

	loadedChart, err := loadFSxOpenZFSCSIChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(loadedChart, fsxOpenZFSCSIHelmValues(cfg))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install FSx for OpenZFS CSI driver: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "FSx for OpenZFS CSI driver installed").
		WithResource("fsx-openzfs-csi-driver").
		WithAction("installed").
		WithMetadata("chart_version", chartVersion))

	return nil
}

// upgradeFSxOpenZFSCSIDriver upgrades an existing release.
func upgradeFSxOpenZFSCSIDriver(ctx context.Context, actionConfig *action.Configuration, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.upgradeFSxOpenZFSCSIDriver")
	defer span.End()

	chartVersion := cfg.FSxOpenZFSCSIChartVersion()

	client := action.NewUpgrade(actionConfig)
	client.Namespace = fsxCSINamespace
	client.Wait = true
	client.Timeout = fsxCSIInstallTimeout
	client.Version = chartVersion

	loadedChart, err := loadFSxOpenZFSCSIChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(fsxCSIReleaseName, loadedChart, fsxOpenZFSCSIHelmValues(cfg))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to upgrade FSx for OpenZFS CSI driver: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "FSx for OpenZFS CSI driver upgraded").
		WithResource("fsx-openzfs-csi-driver").
		WithAction("upgraded").
		WithMetadata("chart_version", chartVersion))

	return nil
}
