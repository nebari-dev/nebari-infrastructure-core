package longhorn

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// StorageSelector returns the label set that identifies the dedicated Longhorn
// storage node group: the configured NodeSelector, or the default
// {NodeStorageLabel: "true"} when none is set. Providers use it to find which
// of their node groups is the storage pool. Matching is an exact key/value
// comparison, so the configured values are load-bearing.
func StorageSelector(c *Config) map[string]string {
	if c != nil && len(c.NodeSelector) > 0 {
		return c.NodeSelector
	}
	return map[string]string{NodeStorageLabel: "true"}
}

// StorageNodeLabels returns every label a dedicated storage node group must
// carry for Longhorn to work: the storage selector labels plus
// CreateDefaultDiskLabel (so Longhorn auto-provisions a disk there). This is the
// label contract a provider applies to its storage pool; the longhorn package
// owns the policy, providers only apply it.
func StorageNodeLabels(c *Config) map[string]string {
	labels := map[string]string{CreateDefaultDiskLabel: "true"}
	for k, v := range StorageSelector(c) {
		labels[k] = v
	}
	return labels
}

// warnIfMissingStorageDiskLabel emits a loud warning when DedicatedNodes is set
// but no node in the cluster carries CreateDefaultDiskLabel. Without that label,
// createDefaultDiskLabeledNodes provisions zero disks and every volume faults
// with ReplicaSchedulingFailure (#369). The AWS provider injects the label onto
// storage node groups automatically; other providers (Hetzner, existing) must
// label their storage pool themselves, and this check turns the otherwise-silent
// failure into an actionable signal.
func warnIfMissingStorageDiskLabel(ctx context.Context, kubeconfigBytes []byte, cfg *Config) error {
	if cfg == nil || !cfg.DedicatedNodes {
		return nil
	}
	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	return warnIfMissingStorageDiskLabelWithClient(ctx, client, cfg)
}

func warnIfMissingStorageDiskLabelWithClient(ctx context.Context, client kubernetes.Interface, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.warnIfMissingStorageDiskLabel")
	defer span.End()

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: CreateDefaultDiskLabel})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to list nodes for storage-disk-label check: %w", err)
	}
	if len(nodes.Items) == 0 {
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			"dedicated_nodes is set but no node carries the "+CreateDefaultDiskLabel+
				" label; Longhorn will create no disks and every volume will fault. "+
				"Label your storage nodes with "+CreateDefaultDiskLabel+"=true "+
				"(the AWS provider does this automatically).").
			WithResource("longhorn").
			WithAction("validating"))
	}
	return nil
}
