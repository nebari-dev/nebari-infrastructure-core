package aws

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	efsCSIProvisioner = "efs.csi.aws.com"
)

// createEFSStorageClass creates or updates a Kubernetes StorageClass for EFS
// dynamic provisioning using access points. This requires the EFS CSI driver
// to be installed on the cluster (handled by the Terraform EKS module).
func createEFSStorageClass(ctx context.Context, kubeconfigBytes []byte, cfg *Config, efsID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.createEFSStorageClass")
	defer span.End()

	storageClassName := cfg.EFSStorageClassName()

	span.SetAttributes(
		attribute.String("storage_class_name", storageClassName),
		attribute.String("efs_id", efsID),
	)

	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return createEFSStorageClassWithClient(ctx, client, cfg, efsID)
}

// createEFSStorageClassWithClient performs the StorageClass creation using the
// provided Kubernetes client interface. Separated from createEFSStorageClass
// to allow testing with fake clients.
func createEFSStorageClassWithClient(ctx context.Context, client kubernetes.Interface, cfg *Config, efsID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.createEFSStorageClassWithClient")
	defer span.End()

	if efsID == "" {
		err := fmt.Errorf("efs_id must not be empty")
		span.RecordError(err)
		return err
	}

	storageClassName := cfg.EFSStorageClassName()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating EFS StorageClass").
		WithResource("efs-storageclass").
		WithAction("creating").
		WithMetadata("name", storageClassName))

	reclaimPolicy := corev1.PersistentVolumeReclaimRetain
	bindingMode := storagev1.VolumeBindingImmediate

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: storageClassName,
		},
		Provisioner: efsCSIProvisioner,
		Parameters: map[string]string{
			"provisioningMode": "efs-ap",
			"fileSystemId":     efsID,
			"directoryPerms":   "700",
		},
		ReclaimPolicy:     &reclaimPolicy,
		VolumeBindingMode: &bindingMode,
	}

	existing, err := client.StorageV1().StorageClasses().Get(ctx, storageClassName, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		if _, err := client.StorageV1().StorageClasses().Create(ctx, sc, metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create EFS StorageClass: %w", err)
		}
	case err != nil:
		span.RecordError(err)
		return fmt.Errorf("failed to get EFS StorageClass: %w", err)
	default:
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Updating existing EFS StorageClass").
			WithResource("efs-storageclass").
			WithAction("updating").
			WithMetadata("name", storageClassName))
		sc.ResourceVersion = existing.ResourceVersion
		if _, err := client.StorageV1().StorageClasses().Update(ctx, sc, metav1.UpdateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update EFS StorageClass: %w", err)
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "EFS StorageClass ready").
		WithResource("efs-storageclass").
		WithAction("ready").
		WithMetadata("name", storageClassName))

	return nil
}
