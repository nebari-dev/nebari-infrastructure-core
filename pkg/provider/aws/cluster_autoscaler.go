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
	caRepoName       = "autoscaler"
	caRepoURL        = "https://kubernetes.github.io/autoscaler"
	caChartName      = "autoscaler/cluster-autoscaler"
	caNamespace      = "kube-system"
	caReleaseName    = "cluster-autoscaler"
	caServiceAccount = "cluster-autoscaler"
	caInstallTimeout = 5 * time.Minute
)

// clusterAutoscalerHelmValues builds the Helm values for the cluster-autoscaler
// chart. Authentication is via the EKS Pod Identity association provisioned by
// the terraform-aws-eks-cluster module, which matches by (cluster, namespace,
// service account name) and requires no IRSA annotation - so the service
// account name must match the one the association targets.
//
// Auto-discovery finds node groups by the `k8s.io/cluster-autoscaler/enabled`
// and `k8s.io/cluster-autoscaler/<cluster>=owned` tags, which EKS managed node
// groups apply to their Auto Scaling Groups automatically. The image tag is
// pinned to match the cluster's Kubernetes minor version, as AWS requires.
func clusterAutoscalerHelmValues(cfg *Config, clusterName string) map[string]any {
	values := map[string]any{
		"cloudProvider": "aws",
		"awsRegion":     cfg.Region,
		"autoDiscovery": map[string]any{
			"clusterName": clusterName,
		},
		"rbac": map[string]any{
			"serviceAccount": map[string]any{
				"create": true,
				"name":   caServiceAccount,
			},
		},
		// balance-similar-node-groups keeps multi-AZ groups evenly sized;
		// least-waste picks the group that leaves the fewest idle resources
		// after a scale up. Both are AWS-recommended general-purpose defaults.
		"extraArgs": map[string]any{
			"balance-similar-node-groups": true,
			"expander":                    "least-waste",
		},
	}

	if tag := cfg.ClusterAutoscalerImageTag(); tag != "" {
		values["image"] = map[string]any{
			"tag": tag,
		}
	}

	return values
}

// loadClusterAutoscalerChart locates and loads the cluster-autoscaler Helm chart.
func loadClusterAutoscalerChart(chartPathOptions action.ChartPathOptions) (*chart.Chart, error) {
	chartPath, err := chartPathOptions.LocateChart(caChartName, cli.New())
	if err != nil {
		return nil, fmt.Errorf("failed to locate Cluster Autoscaler chart: %w", err)
	}

	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load Cluster Autoscaler chart: %w", err)
	}

	return loadedChart, nil
}

// installClusterAutoscaler installs or upgrades the Kubernetes Cluster
// Autoscaler on the cluster via Helm. Safe to call on every deploy; it will
// upgrade an existing release rather than fail.
func installClusterAutoscaler(ctx context.Context, kubeconfigBytes []byte, cfg *Config, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.installClusterAutoscaler")
	defer span.End()

	chartVersion := cfg.ClusterAutoscalerChartVersion()
	span.SetAttributes(
		attribute.String("chart_version", chartVersion),
		attribute.String("cluster_name", clusterName),
		attribute.String("image_tag", cfg.ClusterAutoscalerImageTag()),
	)

	if clusterName == "" {
		err := fmt.Errorf("cluster_name must not be empty")
		span.RecordError(err)
		return err
	}

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	if err := helm.AddRepo(ctx, caRepoName, caRepoURL); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to add autoscaler Helm repository: %w", err)
	}

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, caNamespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	switch _, err := histClient.Run(caReleaseName); {
	case err == nil:
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cluster Autoscaler already installed, upgrading").
			WithResource("cluster-autoscaler").
			WithAction("upgrading"))
		return upgradeClusterAutoscaler(ctx, actionConfig, cfg, clusterName)
	case errors.Is(err, driver.ErrReleaseNotFound):
		// No existing release; fall through to fresh install.
	default:
		span.RecordError(err)
		return fmt.Errorf("failed to query Helm release history for %s: %w", caReleaseName, err)
	}

	helmValues := clusterAutoscalerHelmValues(cfg, clusterName)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Cluster Autoscaler").
		WithResource("cluster-autoscaler").
		WithAction("installing").
		WithMetadata("chart_version", chartVersion))

	client := action.NewInstall(actionConfig)
	client.Namespace = caNamespace
	client.ReleaseName = caReleaseName
	client.CreateNamespace = false
	client.Wait = true
	client.Timeout = caInstallTimeout
	client.Version = chartVersion

	loadedChart, err := loadClusterAutoscalerChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(loadedChart, helmValues)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install Cluster Autoscaler: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Cluster Autoscaler installed").
		WithResource("cluster-autoscaler").
		WithAction("installed").
		WithMetadata("chart_version", chartVersion))

	return nil
}

// upgradeClusterAutoscaler upgrades an existing release.
func upgradeClusterAutoscaler(ctx context.Context, actionConfig *action.Configuration, cfg *Config, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.upgradeClusterAutoscaler")
	defer span.End()

	chartVersion := cfg.ClusterAutoscalerChartVersion()
	helmValues := clusterAutoscalerHelmValues(cfg, clusterName)

	client := action.NewUpgrade(actionConfig)
	client.Namespace = caNamespace
	client.Wait = true
	client.Timeout = caInstallTimeout
	client.Version = chartVersion

	loadedChart, err := loadClusterAutoscalerChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(caReleaseName, loadedChart, helmValues)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to upgrade Cluster Autoscaler: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Cluster Autoscaler upgraded").
		WithResource("cluster-autoscaler").
		WithAction("upgraded").
		WithMetadata("chart_version", chartVersion))

	return nil
}
