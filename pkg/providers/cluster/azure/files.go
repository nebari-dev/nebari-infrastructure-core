package azure

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const filesCSIProvisioner = "file.csi.azure.com"

// filesMountOptions are the NFS mount options AKS recommends for Azure Files.
var filesMountOptions = []string{"nconnect=4", "noresvport", "actimeo=30"}

// subnetRef identifies the node subnet the Azure Files CSI driver opens the
// storage account to.
type subnetRef struct {
	ResourceGroup string
	VNet          string
	Subnet        string
}

// parseSubnetID splits a subnet resource ID into the parts the Azure Files
// CSI driver takes as StorageClass parameters.
func parseSubnetID(id string) (subnetRef, error) {
	rid, err := arm.ParseResourceID(id)
	if err != nil {
		return subnetRef{}, fmt.Errorf("parse subnet ID %q: %w", id, err)
	}
	if !strings.EqualFold(rid.ResourceType.String(), "Microsoft.Network/virtualNetworks/subnets") || rid.Parent == nil {
		return subnetRef{}, fmt.Errorf("%q is not a subnet resource ID", id)
	}
	return subnetRef{
		ResourceGroup: rid.ResourceGroupName,
		VNet:          rid.Parent.Name,
		Subnet:        rid.Name,
	}, nil
}

// filesStorageClass builds the Azure Files NFS StorageClass.
//
// Naming the subnet makes the driver read and update only that subnet. Left
// unset, the driver lists every subnet in the VNet and adds a service endpoint
// to each, which a role scoped to the node subnet does not allow.
//
// Root squash is off so pods running as root can chown directories on the
// share, as the data-science-pack does when it creates /shared/<group>.
func filesStorageClass(cfg *Config, subnet subnetRef) *storagev1.StorageClass {
	reclaimPolicy := corev1.PersistentVolumeReclaimRetain
	bindingMode := storagev1.VolumeBindingImmediate
	allowExpansion := true

	return &storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: cfg.FilesStorageClassName()},
		Provisioner: filesCSIProvisioner,
		Parameters: map[string]string{
			"protocol":          "nfs",
			"skuName":           cfg.FilesSKU(),
			"rootSquashType":    "NoRootSquash",
			"vnetResourceGroup": subnet.ResourceGroup,
			"vnetName":          subnet.VNet,
			"subnetName":        subnet.Subnet,
		},
		MountOptions:         filesMountOptions,
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &bindingMode,
		AllowVolumeExpansion: &allowExpansion,
	}
}

// ensureFilesStorageClass creates the Azure Files NFS StorageClass on the
// cluster described by kubeconfigBytes.
func ensureFilesStorageClass(ctx context.Context, kubeconfigBytes []byte, cfg *Config, subnetID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.ensureFilesStorageClass")
	defer span.End()

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("parse kubeconfig: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("create Kubernetes client: %w", err)
	}

	if err := ensureFilesStorageClassWithClient(ctx, client, cfg, subnetID); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// ensureFilesStorageClassWithClient creates the StorageClass, or leaves it
// alone when one with the same settings exists. StorageClass parameters cannot
// be changed in place, so an existing class with different settings is an
// error that tells the operator how to replace it.
func ensureFilesStorageClassWithClient(ctx context.Context, client kubernetes.Interface, cfg *Config, subnetID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.ensureFilesStorageClassWithClient")
	defer span.End()

	subnet, err := parseSubnetID(subnetID)
	if err != nil {
		span.RecordError(err)
		return err
	}

	want := filesStorageClass(cfg, subnet)
	span.SetAttributes(
		attribute.String("storage_class_name", want.Name),
		attribute.String("sku", cfg.FilesSKU()),
		attribute.String("subnet", subnet.Subnet),
	)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating Azure Files StorageClass").
		WithResource("files-storageclass").
		WithAction("creating").
		WithMetadata("name", want.Name))

	got, err := client.StorageV1().StorageClasses().Get(ctx, want.Name, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		if _, err := client.StorageV1().StorageClasses().Create(ctx, want, metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("create Azure Files StorageClass %q: %w", want.Name, err)
		}
	case err != nil:
		span.RecordError(err)
		return fmt.Errorf("get Azure Files StorageClass %q: %w", want.Name, err)
	case !sameStorageClassSpec(got, want):
		err := fmt.Errorf("StorageClass %q already exists with different settings, and StorageClass settings cannot be changed in place. "+
			"Delete it with `kubectl delete storageclass %s` and run nic deploy again. Existing volumes are not affected", want.Name, want.Name)
		span.RecordError(err)
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Azure Files StorageClass ready").
		WithResource("files-storageclass").
		WithAction("ready").
		WithMetadata("name", want.Name))
	return nil
}

// sameStorageClassSpec compares the StorageClass fields NIC sets.
func sameStorageClassSpec(a, b *storagev1.StorageClass) bool {
	return a.Provisioner == b.Provisioner &&
		maps.Equal(a.Parameters, b.Parameters) &&
		slices.Equal(a.MountOptions, b.MountOptions) &&
		equalPtr(a.ReclaimPolicy, b.ReclaimPolicy) &&
		equalPtr(a.VolumeBindingMode, b.VolumeBindingMode) &&
		equalPtr(a.AllowVolumeExpansion, b.AllowVolumeExpansion)
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
