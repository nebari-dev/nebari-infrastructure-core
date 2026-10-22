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
	fsxOpenZFSCSIProvisioner = "fsx.openzfs.csi.aws.com"

	// The driver expects every parameter except ResourceType to be JSON, so
	// string values carry their own quotes.
	// See https://github.com/kubernetes-sigs/aws-fsx-openzfs-csi-driver/blob/main/docs/parameters.md

	// LZ4 gives up early on data that does not compress, so it costs little
	// even on model weights or archives.
	fsxOpenZFSDataCompression = `"LZ4"`

	// Clients is "*" because the module's security group already limits NFS
	// to the node security group. A VPC CIDR here would refuse nodes in
	// subnets from a secondary VPC CIDR, and their mounts hang rather than
	// fail. no_root_squash lets kubelet apply the pod's fsGroup to a new
	// volume, whose root directory is otherwise root:root 755 and unwritable
	// by non-root pods.
	fsxOpenZFSNFSExports = `[{"ClientConfigurations":[{"Clients":"*","Options":["rw","crossmnt","no_root_squash"]}]}]`

	// Deleting a PVC also deletes its volume's snapshots, which would
	// otherwise make the delete fail.
	fsxOpenZFSOptionsOnDeletion = `["DELETE_CHILD_VOLUMES_AND_SNAPSHOTS"]`
)

// fsxOpenZFSMountOptions follow the driver's dynamic provisioning example.
var fsxOpenZFSMountOptions = []string{"nfsvers=4.1", "rsize=1048576", "wsize=1048576", "timeo=600"}

// createFSxOpenZFSStorageClass creates or updates a Kubernetes StorageClass
// that provisions each PVC as a child volume of the FSx for OpenZFS root
// volume. This requires the FSx for OpenZFS CSI driver to be installed on the
// cluster (see installFSxOpenZFSCSIDriver).
func createFSxOpenZFSStorageClass(ctx context.Context, kubeconfigBytes []byte, cfg *Config, rootVolumeID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.createFSxOpenZFSStorageClass")
	defer span.End()

	span.SetAttributes(
		attribute.String("storage_class_name", cfg.FSxOpenZFSStorageClassName()),
		attribute.String("root_volume_id", rootVolumeID),
	)

	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return createFSxOpenZFSStorageClassWithClient(ctx, client, cfg, rootVolumeID)
}

// createFSxOpenZFSStorageClassWithClient performs the StorageClass creation
// using the provided Kubernetes client interface. Separated from
// createFSxOpenZFSStorageClass to allow testing with fake clients.
func createFSxOpenZFSStorageClassWithClient(ctx context.Context, client kubernetes.Interface, cfg *Config, rootVolumeID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.createFSxOpenZFSStorageClassWithClient")
	defer span.End()

	if rootVolumeID == "" {
		err := fmt.Errorf("fsx_openzfs_root_volume_id must not be empty")
		span.RecordError(err)
		return err
	}

	storageClassName := cfg.FSxOpenZFSStorageClassName()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating FSx for OpenZFS StorageClass").
		WithResource("fsx-openzfs-storageclass").
		WithAction("creating").
		WithMetadata("name", storageClassName))

	// Delete, unlike the EFS StorageClass: a retained child volume keeps FSx
	// from deleting the filesystem, so it would block destroy. A single volume
	// can still be kept by patching its PV's reclaim policy.
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	bindingMode := storagev1.VolumeBindingImmediate
	allowExpansion := false

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: storageClassName,
		},
		Provisioner: fsxOpenZFSCSIProvisioner,
		Parameters: map[string]string{
			"ResourceType":        "volume",
			"ParentVolumeId":      fmt.Sprintf("%q", rootVolumeID),
			"DataCompressionType": fsxOpenZFSDataCompression,
			"NfsExports":          fsxOpenZFSNFSExports,
			"OptionsOnDeletion":   fsxOpenZFSOptionsOnDeletion,
		},
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &bindingMode,
		AllowVolumeExpansion: &allowExpansion,
		MountOptions:         fsxOpenZFSMountOptions,
	}

	existing, err := client.StorageV1().StorageClasses().Get(ctx, storageClassName, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		if _, err := client.StorageV1().StorageClasses().Create(ctx, sc, metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create FSx for OpenZFS StorageClass: %w", err)
		}
	case err != nil:
		span.RecordError(err)
		return fmt.Errorf("failed to get FSx for OpenZFS StorageClass: %w", err)
	default:
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Updating existing FSx for OpenZFS StorageClass").
			WithResource("fsx-openzfs-storageclass").
			WithAction("updating").
			WithMetadata("name", storageClassName))
		sc.ResourceVersion = existing.ResourceVersion
		if _, err := client.StorageV1().StorageClasses().Update(ctx, sc, metav1.UpdateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update FSx for OpenZFS StorageClass: %w", err)
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "FSx for OpenZFS StorageClass ready").
		WithResource("fsx-openzfs-storageclass").
		WithAction("ready").
		WithMetadata("name", storageClassName))

	return nil
}
