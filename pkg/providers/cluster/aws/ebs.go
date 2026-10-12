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
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/storageclass"
)

const (
	ebsCSIProvisioner = "ebs.csi.aws.com"

	// storageClassGP3 is the EBS gp3 StorageClass NIC creates on every AWS
	// cluster. It is the RWO class (and cluster default) when Longhorn is
	// disabled.
	storageClassGP3 = "gp3"
)

// gp3StorageClass returns the desired gp3 StorageClass. WaitForFirstConsumer
// keeps each volume in the AZ the scheduler picked for its first pod, which
// is what makes single-AZ EBS usable for RWO workloads on multi-AZ clusters.
func gp3StorageClass() *storagev1.StorageClass {
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	bindingMode := storagev1.VolumeBindingWaitForFirstConsumer
	allowExpansion := true

	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: storageClassGP3,
		},
		Provisioner: ebsCSIProvisioner,
		Parameters: map[string]string{
			"type":      "gp3",
			"encrypted": "true",
		},
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &bindingMode,
		AllowVolumeExpansion: &allowExpansion,
	}
}

// ensureGP3StorageClass creates the gp3 StorageClass if it is missing and
// marks it as the cluster default when no other class is the default. This
// requires the EBS CSI driver, which the Terraform EKS module installs as an
// EKS addon.
func ensureGP3StorageClass(ctx context.Context, kubeconfigBytes []byte) error {
	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	return ensureGP3StorageClassWithClient(ctx, client)
}

// ensureGP3StorageClassWithClient is the testable inner form of
// ensureGP3StorageClass.
//
// An existing class named gp3 is left as-is rather than updated: provisioner
// and parameters are immutable on a StorageClass, and a gp3 class an admin
// created by hand before NIC managed one should not be replaced out from
// under the PVCs that use it.
func ensureGP3StorageClassWithClient(ctx context.Context, client kubernetes.Interface) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.ensureGP3StorageClass")
	defer span.End()

	span.SetAttributes(attribute.String("storage_class_name", storageClassGP3))

	existing, err := client.StorageV1().StorageClasses().Get(ctx, storageClassGP3, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating gp3 StorageClass").
			WithResource("storageclass").
			WithAction("creating").
			WithMetadata("name", storageClassGP3))
		if _, err := client.StorageV1().StorageClasses().Create(ctx, gp3StorageClass(), metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create gp3 StorageClass: %w", err)
		}
	case err != nil:
		span.RecordError(err)
		return fmt.Errorf("failed to get gp3 StorageClass: %w", err)
	default:
		if existing.Provisioner != ebsCSIProvisioner || existing.Parameters["type"] != "gp3" {
			status.Send(ctx, status.NewUpdate(status.LevelWarning,
				fmt.Sprintf("Existing StorageClass %q uses provisioner %q with type %q, not %q with type \"gp3\"; leaving it unchanged",
					storageClassGP3, existing.Provisioner, existing.Parameters["type"], ebsCSIProvisioner)).
				WithResource("storageclass").
				WithAction("skipped").
				WithMetadata("name", storageClassGP3))
		}
	}

	isDefault, err := storageclass.MarkDefaultIfNone(ctx, client, storageClassGP3)
	if err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.Bool("is_default", isDefault))

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "gp3 StorageClass ready").
		WithResource("storageclass").
		WithAction("ready").
		WithMetadata("name", storageClassGP3))

	return nil
}
