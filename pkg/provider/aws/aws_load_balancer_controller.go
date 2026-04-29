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
	lbcRepoName       = "eks"
	lbcRepoURL        = "https://aws.github.io/eks-charts"
	lbcChartName      = "eks/aws-load-balancer-controller"
	lbcNamespace      = "kube-system"
	lbcReleaseName    = "aws-load-balancer-controller"
	lbcServiceAccount = "aws-load-balancer-controller"
	lbcInstallTimeout = 5 * time.Minute
)

// awsLoadBalancerControllerHelmValues builds the Helm values for the
// aws-load-balancer-controller chart. The service account is created by the
// chart; authentication is via the EKS Pod Identity association provisioned
// by the terraform-aws-eks-cluster module, which matches by (cluster,
// namespace, service account name) and requires no IRSA annotation.
func awsLoadBalancerControllerHelmValues(cfg *Config, clusterName, vpcID string) map[string]any {
	return map[string]any{
		"clusterName": clusterName,
		"region":      cfg.Region,
		"vpcId":       vpcID,
		"serviceAccount": map[string]any{
			"create": true,
			"name":   lbcServiceAccount,
		},
	}
}

// loadLBCChart locates and loads the AWS Load Balancer Controller Helm chart.
func loadLBCChart(chartPathOptions action.ChartPathOptions) (*chart.Chart, error) {
	chartPath, err := chartPathOptions.LocateChart(lbcChartName, cli.New())
	if err != nil {
		return nil, fmt.Errorf("failed to locate AWS Load Balancer Controller chart: %w", err)
	}

	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS Load Balancer Controller chart: %w", err)
	}

	return loadedChart, nil
}

// installAWSLoadBalancerController installs or upgrades the AWS Load Balancer
// Controller on the cluster via Helm. Safe to call on every deploy; it will
// upgrade an existing release rather than fail.
func installAWSLoadBalancerController(ctx context.Context, kubeconfigBytes []byte, cfg *Config, clusterName, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.installAWSLoadBalancerController")
	defer span.End()

	chartVersion := cfg.LoadBalancerControllerChartVersion()
	span.SetAttributes(
		attribute.String("chart_version", chartVersion),
		attribute.String("cluster_name", clusterName),
		attribute.String("vpc_id", vpcID),
	)

	if clusterName == "" {
		err := fmt.Errorf("cluster_name must not be empty")
		span.RecordError(err)
		return err
	}
	if vpcID == "" {
		err := fmt.Errorf("vpc_id must not be empty")
		span.RecordError(err)
		return err
	}

	kubeconfigPath, cleanup, err := helm.WriteTempKubeconfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer cleanup()

	if err := helm.AddRepo(ctx, lbcRepoName, lbcRepoURL); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to add eks-charts Helm repository: %w", err)
	}

	actionConfig, err := helm.NewActionConfig(kubeconfigPath, lbcNamespace)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Helm action config: %w", err)
	}

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	switch _, err := histClient.Run(lbcReleaseName); {
	case err == nil:
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "AWS Load Balancer Controller already installed, upgrading").
			WithResource("aws-load-balancer-controller").
			WithAction("upgrading"))
		return upgradeAWSLoadBalancerController(ctx, actionConfig, cfg, clusterName, vpcID)
	case errors.Is(err, driver.ErrReleaseNotFound):
		// No existing release; fall through to fresh install.
	default:
		span.RecordError(err)
		return fmt.Errorf("failed to query Helm release history for %s: %w", lbcReleaseName, err)
	}

	helmValues := awsLoadBalancerControllerHelmValues(cfg, clusterName, vpcID)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing AWS Load Balancer Controller").
		WithResource("aws-load-balancer-controller").
		WithAction("installing").
		WithMetadata("chart_version", chartVersion))

	client := action.NewInstall(actionConfig)
	client.Namespace = lbcNamespace
	client.ReleaseName = lbcReleaseName
	client.CreateNamespace = false
	client.Wait = true
	client.Timeout = lbcInstallTimeout
	client.Version = chartVersion

	loadedChart, err := loadLBCChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(loadedChart, helmValues)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install AWS Load Balancer Controller: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "AWS Load Balancer Controller installed").
		WithResource("aws-load-balancer-controller").
		WithAction("installed").
		WithMetadata("chart_version", chartVersion))

	return nil
}

// upgradeAWSLoadBalancerController upgrades an existing release.
func upgradeAWSLoadBalancerController(ctx context.Context, actionConfig *action.Configuration, cfg *Config, clusterName, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.upgradeAWSLoadBalancerController")
	defer span.End()

	chartVersion := cfg.LoadBalancerControllerChartVersion()
	helmValues := awsLoadBalancerControllerHelmValues(cfg, clusterName, vpcID)

	client := action.NewUpgrade(actionConfig)
	client.Namespace = lbcNamespace
	client.Wait = true
	client.Timeout = lbcInstallTimeout
	client.Version = chartVersion

	loadedChart, err := loadLBCChart(client.ChartPathOptions)
	if err != nil {
		span.RecordError(err)
		return err
	}

	release, err := client.Run(lbcReleaseName, loadedChart, helmValues)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to upgrade AWS Load Balancer Controller: %w", err)
	}

	span.SetAttributes(
		attribute.String("release_status", string(release.Info.Status)),
		attribute.Int("release_version", release.Version),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "AWS Load Balancer Controller upgraded").
		WithResource("aws-load-balancer-controller").
		WithAction("upgraded").
		WithMetadata("chart_version", chartVersion))

	return nil
}
