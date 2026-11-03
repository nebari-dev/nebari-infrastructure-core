package longhorn

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/storageclass"
)

// ensureSoleDefaultStorageClass demotes any StorageClass other than the
// longhorn class that still carries the default-class annotation, leaving
// longhorn as the cluster's only default StorageClass. Hetzner ships
// hcloud-volumes pre-annotated, so installing Longhorn with
// persistence.defaultClass=true leaves two defaults until we demote the
// previous one. It does nothing when cfg sets default_class: false.
func ensureSoleDefaultStorageClass(ctx context.Context, kubeconfigBytes []byte, cfg *Config) error {
	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	return claimDefaultStorageClass(ctx, client, cfg)
}

// claimDefaultStorageClass is the testable inner form of
// ensureSoleDefaultStorageClass. With default_class: false it leaves every
// class's default annotation alone, so a class the admin made the default
// (for example gp3 while moving off Longhorn) keeps it across deploys.
func claimDefaultStorageClass(ctx context.Context, client kubernetes.Interface, cfg *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "longhorn.claimDefaultStorageClass")
	defer span.End()

	isDefault := cfg.IsDefaultClass()
	span.SetAttributes(attribute.Bool("default_class", isDefault))
	if !isDefault {
		return nil
	}
	if err := storageclass.EnsureSoleDefault(ctx, client, StorageClassName); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}
