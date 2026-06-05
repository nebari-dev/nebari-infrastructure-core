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

// nodeStorageTaintToleration is the Longhorn taint-toleration setting value
// matching the recommended dedicated-node taint
// (node.longhorn.io/storage=true:NoSchedule). This setting is what lets
// Longhorn's system-managed components (instance-manager, engine-image,
// CSI plugin) tolerate the storage-node taint so the replica-serving
// components can run on the dedicated storage nodes.
// https://longhorn.io/docs/1.11.2/advanced-resources/deploy/taint-toleration/
const nodeStorageTaintToleration = NodeStorageLabel + "=true:NoSchedule"

// Install installs (or upgrades, if a release exists) Longhorn on the cluster
// the kubeconfigBytes connect to.
//
// cfg may be nil; receiver methods on *Config are nil-safe and a nil cfg
// means "use defaults" (the AWS provider relies on this).
//
// On a fresh cluster, the iSCSI prerequisite DaemonSet is deployed and waited
// on before the Helm install. The iSCSI step also runs on the upgrade path —
// the DaemonSet is idempotent and re-asserting it protects against drift
// (e.g., manual cleanup that left the release behind).
//
// Install is idempotent: re-running on an installed cluster is a no-op modulo
// any Config changes that would shift the rendered Helm values.
func Install(ctx context.Context, kubeconfigBytes []byte, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.Install")
	defer span.End()

	if cfg == nil {
		cfg = &Config{}
	}

	span.SetAttributes(
		attribute.String("chart_version", ChartVersion),
		attribute.Int("replica_count", cfg.Replicas()),
		attribute.Bool("dedicated_nodes", cfg.DedicatedNodes),
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

	// Re-assert the iSCSI DaemonSet on every Install call (install and upgrade
	// alike). The DaemonSet apply is idempotent and the readiness wait is
	// near-instant when the DS is already healthy on every node, so the cost
	// on the upgrade path is small. Running it unconditionally protects
	// against drift (e.g. someone manually deleted the DaemonSet while the
	// Helm release stayed intact).
	if err := ensureISCSI(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install iSCSI prerequisites: %w", err)
	}

	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	if _, err := histClient.Run(ReleaseName); err == nil {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Longhorn already installed, upgrading").
			WithResource("longhorn").
			WithAction("upgrading"))
		return upgrade(ctx, actionConfig, kubeconfigBytes, cfg)
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

	if err := ensureSoleDefaultStorageClass(ctx, kubeconfigBytes, StorageClassName); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to demote previous default StorageClass: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Longhorn storage installed").
		WithResource("longhorn").
		WithAction("installed").
		WithMetadata("chart_version", ChartVersion))

	return nil
}

func upgrade(ctx context.Context, actionConfig *action.Configuration, kubeconfigBytes []byte, cfg *Config) error {
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

	if err := ensureSoleDefaultStorageClass(ctx, kubeconfigBytes, StorageClassName); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to demote previous default StorageClass: %w", err)
	}

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

	// Only render the autoscaler setting when a provider explicitly sets it.
	// Leaving it unset keeps Longhorn's default.
	if cfg != nil && cfg.ClusterAutoscalerEnabled != nil {
		settings["kubernetesClusterAutoscalerEnabled"] = *cfg.ClusterAutoscalerEnabled
	}

	values := map[string]any{
		"persistence":     persistence,
		"defaultSettings": settings,
	}

	if cfg != nil && cfg.DedicatedNodes {
		// Storage nodes auto-provision a Longhorn disk: createDefaultDiskLabeledNodes
		// makes Longhorn create a default disk only on nodes carrying the
		// CreateDefaultDiskLabel (the AWS provider adds it to storage node groups;
		// other providers must label their storage pool — see config.go). Because
		// only storage nodes get a disk, replicas can only ever land on storage
		// nodes. We therefore do NOT pin the system components by node selector:
		// doing so kept longhorn-csi-plugin and longhorn-manager off workload
		// nodes and broke PVC mounts there (#366).
		settings["createDefaultDiskLabeledNodes"] = true

		// System-managed components (replica-role instance-managers, engine-image,
		// share-manager) must tolerate the storage-node taint so they can run on
		// the dedicated storage nodes that host replicas.
		settings["taintToleration"] = nodeStorageTaintToleration

		// longhorn-manager (DaemonSet) and the driver deployer must run on EVERY
		// node so a volume can be served to a workload anywhere — they are
		// node-level infrastructure, not workloads, so they tolerate all taints
		// (same rationale as the embedded iSCSI prerequisite DaemonSet). They are
		// deliberately NOT given a nodeSelector (#366).
		tolerateAll := []map[string]any{{"operator": "Exists"}}
		values["longhornManager"] = map[string]any{"tolerations": tolerateAll}
		values["longhornDriver"] = map[string]any{"tolerations": tolerateAll}
	}

	return values
}
