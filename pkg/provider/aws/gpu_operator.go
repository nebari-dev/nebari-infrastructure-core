package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/storage/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	// instanceTypeLabel is the well-known node label EKS sets to the EC2
	// instance type (e.g. "g4dn.2xlarge").
	instanceTypeLabel = "node.kubernetes.io/instance-type"

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

// reconcileGPUOperator brings the GPU operator into the state the cluster
// requires: installed when the cluster has (or is configured to have) GPU
// nodes, removed otherwise. Safe to call on every deploy.
//
// "Has GPU nodes" is the union of the desired config (any node group tagged
// gpu: true, which selects the AL2023 NVIDIA AMI) and the live cluster (any
// Ready-or-not node whose instance type is a GPU family). The config covers a
// GPU group scaled to zero; the live check covers nodes the config didn't
// declare. Either is enough to keep the operator installed.
func reconcileGPUOperator(ctx context.Context, kubeconfigBytes []byte, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.reconcileGPUOperator")
	defer span.End()

	// Config gpu: true is the primary, no-API-call signal and short-circuits the
	// live check. For clusters that don't declare GPU node groups we fall back
	// to inspecting live nodes (catches nodes the config didn't declare). That
	// fallback is advisory: a transient node-list error must not fail an
	// otherwise-successful deploy, since the operator has no cloud resources and
	// uninstall is a no-op when absent. On error we warn and treat the cluster
	// as GPU-free. Install/uninstall errors below still surface as real failures.
	want := cfg.hasGPUNodeGroups()
	if !want {
		live, err := clusterHasGPUNodes(ctx, kubeconfigBytes)
		if err != nil {
			status.Send(ctx, status.NewUpdate(status.LevelWarning,
				fmt.Sprintf("Skipping GPU node check — could not list nodes: %v", err)).
				WithResource("gpu-operator").WithAction("reconciling"))
		} else {
			want = live
		}
	}

	span.SetAttributes(attribute.Bool("gpu_wanted", want))

	if want {
		return installGPUOperator(ctx, kubeconfigBytes)
	}
	return uninstallGPUOperator(ctx, kubeconfigBytes)
}

// clusterHasGPUNodes reports whether any node currently registered in the
// cluster runs on a GPU instance type. It reads the node's instance-type label
// rather than nvidia.com/gpu allocatable, because the latter only appears once
// the device plugin (i.e. this operator) is already running.
func clusterHasGPUNodes(ctx context.Context, kubeconfigBytes []byte) (bool, error) {
	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		return false, err
	}

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list nodes: %w", err)
	}

	for i := range nodes.Items {
		if isGPUInstanceType(nodes.Items[i].Labels[instanceTypeLabel]) {
			return true, nil
		}
	}
	return false, nil
}

// isGPUInstanceType reports whether an EC2 instance type carries an NVIDIA GPU.
// NVIDIA GPU instances are the accelerated "g" (graphics) and "p" (compute)
// families - e.g. g4dn.2xlarge, g5.xlarge, gr6.4xlarge, p4d.24xlarge. No
// general-purpose family starts with g or p, so a leading g/p plus a digit in
// the family (which every real GPU family has, including gr6) is a reliable
// signal while still excluding non-instance strings like "graviton".
//
// g4ad is the one exception: it carries an AMD Radeon Pro V520, not an NVIDIA
// GPU, so the NVIDIA operator's device plugin would find nothing and never
// advertise nvidia.com/gpu. It's the only AMD GPU family on EC2, so an explicit
// exclusion is enough.
func isGPUInstanceType(instanceType string) bool {
	family, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(instanceType)), ".")
	if family == "" || (family[0] != 'g' && family[0] != 'p') {
		return false
	}
	if family == "g4ad" {
		return false
	}
	return strings.ContainsAny(family, "0123456789")
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

	release, err := client.Run(loadedChart, gpuOperatorHelmValues())
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

	release, err := client.Run(gpuOperatorReleaseName, loadedChart, gpuOperatorHelmValues())
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
// missing release is not an error. Called both during reconcile (when GPU nodes
// disappear from the config) and during cluster destroy, before tofu tears the
// cluster down.
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

	client := action.NewUninstall(actionConfig)
	client.Wait = true
	client.Timeout = gpuOperatorInstallTimeout

	if _, err := client.Run(gpuOperatorReleaseName); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to uninstall GPU Operator: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "GPU Operator uninstalled").
		WithResource("gpu-operator").
		WithAction("uninstalled"))

	return nil
}
