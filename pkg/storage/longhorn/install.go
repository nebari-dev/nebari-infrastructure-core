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

// tolerateAllTaints is the Longhorn taint-toleration setting value that tells
// Longhorn's system-managed components (longhorn-csi-plugin, the replica/engine
// instance-managers, engine-image, share-manager) to tolerate EVERY taint.
// Longhorn parses the bare ":" as a toleration with an empty key and
// Operator=Exists, which matches all taints regardless of key, value, or effect.
// The csi-plugin is the per-node mount driver, so it must run on every node a
// workload pod might land on - including tainted pools (the storage taint, a GPU
// pool's nvidia.com/gpu:NoSchedule, any config-driven taint) - or PVC mounts
// fail there (#366). Replica DATA is still confined to storage nodes because
// only they carry CreateDefaultDiskLabel and thus get a Longhorn disk (#369).
// https://github.com/longhorn/longhorn-manager/blob/v1.11.2/types/setting.go
// https://longhorn.io/docs/1.11.2/advanced-resources/deploy/taint-toleration/
const tolerateAllTaints = ":"

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

	if err := warnIfMissingStorageDiskLabel(ctx, kubeconfigBytes, cfg); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to verify storage-node disk label: %w", err)
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

	if err := warnIfMissingStorageDiskLabel(ctx, kubeconfigBytes, cfg); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to verify storage-node disk label: %w", err)
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

	// Longhorn setting values are strings; this one is a per-engine JSON map.
	// Only render it when a provider or user sets the knob, so the Longhorn
	// default (12% of node CPU per instance-manager pod) stays untouched.
	if cfg != nil && cfg.InstanceManagerCPUPercent != nil {
		settings["guaranteedInstanceManagerCPU"] = fmt.Sprintf(
			`{"v1":"%d","v2":"%d"}`, *cfg.InstanceManagerCPUPercent, *cfg.InstanceManagerCPUPercent)
	}

	// The chart ships no resources for the CSI sidecars, leaving them
	// BestEffort (#456). csi-plugin (the per-node mount path) is deliberately
	// left alone: capping it risks slow mounts; see docs/resource-sizing.md.
	settings["systemManagedCSIComponentsResourceLimits"] = `{` +
		`"csi-attacher":{"requests":{"cpu":"10m","memory":"32Mi"},"limits":{"cpu":"100m","memory":"128Mi"}},` +
		`"csi-provisioner":{"requests":{"cpu":"10m","memory":"32Mi"},"limits":{"cpu":"100m","memory":"128Mi"}},` +
		`"csi-resizer":{"requests":{"cpu":"10m","memory":"32Mi"},"limits":{"cpu":"100m","memory":"128Mi"}},` +
		`"csi-snapshotter":{"requests":{"cpu":"10m","memory":"32Mi"},"limits":{"cpu":"100m","memory":"128Mi"}}}`

	// The chart ships no resources for longhorn-manager either (#456). The
	// manager coordinates rebuilds but does not carry volume data, so a CPU
	// limit is safe.
	longhornManager := map[string]any{
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "50m", "memory": "128Mi"},
			"limits":   map[string]any{"cpu": "500m", "memory": "512Mi"},
		},
	}

	values := map[string]any{
		"persistence":     persistence,
		"defaultSettings": settings,
		"longhornManager": longhornManager,
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

		// System-managed components - crucially the per-node longhorn-csi-plugin,
		// plus the replica/engine instance-managers, engine-image and share-manager
		// - tolerate EVERY taint via the tolerate-all taintToleration. The csi-plugin
		// must run wherever a workload pod can be scheduled, including tainted pools
		// (storage taint, a GPU pool's nvidia.com/gpu:NoSchedule, any config-driven
		// taint), or PVC mounts fail there (#366). Replica DATA stays on storage
		// nodes regardless, because only they get a Longhorn disk (#369).
		settings["taintToleration"] = tolerateAllTaints

		// longhorn-manager (DaemonSet) and the driver deployer are node-level
		// infrastructure, not workloads, so they also tolerate all taints (same
		// rationale as the embedded iSCSI prerequisite DaemonSet) and carry no
		// nodeSelector (#366).
		tolerateAll := []map[string]any{{"operator": "Exists"}}
		longhornManager["tolerations"] = tolerateAll
		values["longhornDriver"] = map[string]any{"tolerations": tolerateAll}
	}

	return values
}
