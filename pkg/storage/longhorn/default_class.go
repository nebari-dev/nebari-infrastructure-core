package longhorn

import (
	"context"
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/storageclass"
)

// ensureSoleDefaultStorageClass demotes any StorageClass other than keep
// that still carries the default-class annotation, leaving keep as the
// cluster's only default StorageClass. Hetzner ships hcloud-volumes
// pre-annotated, so installing Longhorn with persistence.defaultClass=true
// leaves two defaults until we demote the previous one.
func ensureSoleDefaultStorageClass(ctx context.Context, kubeconfigBytes []byte, keep string) error {
	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	return storageclass.EnsureSoleDefault(ctx, client, keep)
}
