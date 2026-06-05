package longhorn

import (
	"context"
	"fmt"
	"sort"
	"strings"
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

// nodeStorageLabel is the node label Longhorn uses to identify nodes that
// should host its storage components when DedicatedNodes is enabled.
const nodeStorageLabel = "node.longhorn.io/storage"

// nodeStorageTaintToleration is the Longhorn taint-toleration setting value
// matching the recommended dedicated-node taint
// (node.longhorn.io/storage=true:NoSchedule). Unlike the manager/driver
// tolerations, this setting is what lets Longhorn's system-managed components
// (instance-manager, engine-image, CSI plugin) schedule onto tainted nodes.
// https://longhorn.io/docs/1.11.2/advanced-resources/deploy/taint-toleration/
const nodeStorageTaintToleration = nodeStorageLabel + "=true:NoSchedule"

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
		settings["createDefaultDiskLabeledNodes"] = true

		nodeSelector := map[string]string{nodeStorageLabel: "true"}
		// Override only when a non-empty selector is supplied. An explicitly
		// empty map (node_selector: {}) would otherwise clear the
		// system-managed-components node selector, silently unpinning the
		// instance-managers from the storage nodes.
		if len(cfg.NodeSelector) > 0 {
			nodeSelector = cfg.NodeSelector
		}

		tolerations := []map[string]string{
			{
				"key":      nodeStorageLabel,
				"operator": "Exists",
				"effect":   "NoSchedule",
			},
		}

		// longhornManager/longhornDriver tolerations only cover the
		// user-deployed components. The system-managed components
		// (instance-manager, engine-image, CSI plugin) that actually serve
		// replicas tolerate the node taint only via the taint-toleration
		// setting, and are confined to the storage nodes via
		// system-managed-components-node-selector. Without both, tainting the
		// dedicated node group prevents Longhorn from scheduling its storage
		// engines there.
		// https://longhorn.io/docs/1.11.2/advanced-resources/deploy/taint-toleration/
		settings["taintToleration"] = nodeStorageTaintToleration
		settings["systemManagedComponentsNodeSelector"] = formatNodeSelector(nodeSelector)

		// Helm's value coalescing only treats map[string]any as a "table"
		// (chartutil.istable). A map[string]string nested in the values is seen
		// as a non-table, so coalescing it against the chart's nodeSelector: {}
		// default emits "cannot overwrite table with non table" and skips the
		// merge. Convert to map[string]any so longhornManager/longhornDriver
		// actually receive the selector.
		nodeSelectorValue := make(map[string]any, len(nodeSelector))
		for k, v := range nodeSelector {
			nodeSelectorValue[k] = v
		}

		values["longhornManager"] = map[string]any{
			"nodeSelector": nodeSelectorValue,
			"tolerations":  tolerations,
		}
		values["longhornDriver"] = map[string]any{
			"nodeSelector": nodeSelectorValue,
			"tolerations":  tolerations,
		}
	}

	return values
}

// formatNodeSelector renders a label map as Longhorn's
// systemManagedComponentsNodeSelector setting string ("key:value" pairs joined
// by ";"). Keys are sorted so the generated Helm values are deterministic.
func formatNodeSelector(sel map[string]string) string {
	if len(sel) == 0 {
		return ""
	}

	keys := make([]string, 0, len(sel))
	for k := range sel {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+":"+sel[k])
	}
	return strings.Join(parts, ";")
}
