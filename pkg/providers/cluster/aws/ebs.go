package aws

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/storageclass"
)

const (
	ebsCSIProvisioner = "ebs.csi.aws.com"

	// ebsInTreeProvisioner is the provisioner on the gp2 class EKS ships.
	// EKS routes it to the CSI driver through CSI migration.
	ebsInTreeProvisioner = "kubernetes.io/aws-ebs"

	// ebsEKSDefaultClassName is the name of the in-tree gp2 class EKS ships.
	ebsEKSDefaultClassName = "gp2"

	// managedByLabel marks the StorageClass as NIC's, which lets NIC replace
	// it when the ebs config changes. NIC never replaces a class without it,
	// but may still mark one as the cluster default when no default exists.
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "nebari-infrastructure-core"

	// gp3 baseline performance. AWS rejects lower values.
	gp3MinIOPS       = 3000
	gp3MinThroughput = 125

	// ioMinIOPS is the EBS CSI driver's minimum IOPS for io1 and io2. Unless
	// allowAutoIOPSPerGBIncrease is set, the driver passes lower values to
	// AWS, which rejects them when a PVC is provisioned.
	ioMinIOPS = 100
)

// validateEBSConfig checks the ebs block. Upper limits on IOPS and throughput
// are left to AWS, which raises them from time to time.
func validateEBSConfig(c *Config) error {
	if c.EBS == nil {
		return nil
	}
	e := c.EBS
	volumeType := c.EBSVolumeType()

	if !slices.Contains(validEBSVolumeTypes, volumeType) {
		return fmt.Errorf("ebs.type: invalid volume type %q (must be one of: %v)", volumeType, validEBSVolumeTypes)
	}

	if e.IOPS != nil {
		switch volumeType {
		case "gp3":
			if *e.IOPS < gp3MinIOPS {
				return fmt.Errorf("ebs.iops: %d is below the gp3 baseline of %d", *e.IOPS, gp3MinIOPS)
			}
		case "io1", "io2":
			if *e.IOPS < ioMinIOPS {
				return fmt.Errorf("ebs.iops: %d is below the %s minimum of %d", *e.IOPS, volumeType, ioMinIOPS)
			}
		default:
			return fmt.Errorf("ebs.iops: not supported for volume type %q (only gp3, io1 and io2)", volumeType)
		}
	} else if volumeType == "io1" || volumeType == "io2" {
		return fmt.Errorf("ebs.iops: required for volume type %q", volumeType)
	}

	if e.Throughput != nil {
		if volumeType != "gp3" {
			return fmt.Errorf("ebs.throughput: not supported for volume type %q (only gp3)", volumeType)
		}
		if *e.Throughput < gp3MinThroughput {
			return fmt.Errorf("ebs.throughput: %d MiB/s is below the gp3 baseline of %d MiB/s", *e.Throughput, gp3MinThroughput)
		}
	}

	if e.KMSKeyARN != "" && !kmsKeyARNPattern.MatchString(e.KMSKeyARN) {
		return fmt.Errorf("ebs.kms_key_arn: %q is not a KMS key ARN (arn:<partition>:kms:<region>:<account>:key/<key-id>); alias ARNs are not supported", e.KMSKeyARN)
	}

	name := c.EBSStorageClassName()
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return fmt.Errorf("ebs.storage_class_name: %q is not a valid StorageClass name: %v", name, errs)
	}
	if name == longhorn.StorageClassName {
		return fmt.Errorf("ebs.storage_class_name: %q is reserved for Longhorn", name)
	}
	if c.EFS != nil && c.EFS.Enabled && name == c.EFSStorageClassName() {
		return fmt.Errorf("ebs.storage_class_name: %q is already used by the EFS StorageClass", name)
	}

	return nil
}

// ebsCSIKMSKeyARNs returns the configured keys the EBS CSI driver must be
// granted: ebs_kms_key_arns plus the StorageClass's ebs.kms_key_arn, so a
// key set only on the class still works.
func (c *Config) ebsCSIKMSKeyARNs() []string {
	if c.EBS == nil || c.EBS.KMSKeyARN == "" {
		return c.EBSKMSKeyARNs
	}
	return mergeKMSKeyARNs(c.EBSKMSKeyARNs, []string{c.EBS.KMSKeyARN})
}

// ebsStorageClass returns the desired EBS StorageClass for c. WaitForFirstConsumer
// keeps each volume in the AZ the scheduler picked for its first pod, which
// is what makes single-AZ EBS usable for RWO workloads on multi-AZ clusters.
func ebsStorageClass(c *Config) *storagev1.StorageClass {
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	bindingMode := storagev1.VolumeBindingWaitForFirstConsumer
	allowExpansion := true

	params := map[string]string{
		"type":      c.EBSVolumeType(),
		"encrypted": "true",
	}
	if c.EBS != nil {
		if c.EBS.IOPS != nil {
			params["iops"] = strconv.Itoa(*c.EBS.IOPS)
		}
		if c.EBS.Throughput != nil {
			params["throughput"] = strconv.Itoa(*c.EBS.Throughput)
		}
		if c.EBS.KMSKeyARN != "" {
			params["kmsKeyId"] = c.EBS.KMSKeyARN
		}
	}

	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   c.EBSStorageClassName(),
			Labels: map[string]string{managedByLabel: managedByValue},
		},
		Provisioner:          ebsCSIProvisioner,
		Parameters:           params,
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &bindingMode,
		AllowVolumeExpansion: &allowExpansion,
	}
}

// ensureEBSStorageClass creates the configured EBS StorageClass if it is
// missing and marks it as the cluster default when no other class is the
// default. This requires the EBS CSI driver, which the Terraform EKS module
// installs as an EKS addon.
func ensureEBSStorageClass(ctx context.Context, kubeconfigBytes []byte, c *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.ensureEBSStorageClass")
	defer span.End()

	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	if err := ensureEBSStorageClassWithClient(ctx, client, c); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// ensureEBSStorageClassWithClient is the testable inner form of
// ensureEBSStorageClass.
//
// Provisioner and parameters are immutable on a StorageClass, so when the
// ebs config changes NIC deletes and recreates its own class (the one with
// managedByLabel). Bound PVs keep the settings they were provisioned with.
// A class NIC didn't create, such as EKS's gp2 or a hand-made gp3, is left
// as-is so it isn't replaced out from under the PVCs that use it.
func ensureEBSStorageClassWithClient(ctx context.Context, client kubernetes.Interface, c *Config) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.ensureEBSStorageClassWithClient")
	defer span.End()

	desired := ebsStorageClass(c)
	name := desired.Name
	span.SetAttributes(
		attribute.String("storage_class_name", name),
		attribute.String("volume_type", desired.Parameters["type"]),
	)

	existing, err := client.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Creating %s StorageClass", name)).
			WithResource("storageclass").
			WithAction("creating").
			WithMetadata("name", name))
		if _, err := client.StorageV1().StorageClasses().Create(ctx, desired, metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create %s StorageClass: %w", name, err)
		}
	case err != nil:
		span.RecordError(err)
		return fmt.Errorf("failed to get %s StorageClass: %w", name, err)
	case ebsStorageClassMatches(existing, desired):
		// Nothing to change.
	case existing.Labels[managedByLabel] == managedByValue:
		if err := replaceStorageClass(ctx, client, existing, desired); err != nil {
			span.RecordError(err)
			return err
		}
	case isEKSDefaultGP2Reuse(c, existing):
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Using the %s StorageClass EKS ships as-is. It does not set encrypted, so its volumes are encrypted only if EBS encryption by default is enabled in the account; set ebs.storage_class_name to have NIC create its own encrypted gp2 class", name)).
			WithResource("storageclass").
			WithAction("skipped").
			WithMetadata("name", name))
	default:
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			fmt.Sprintf("Existing StorageClass %q was not created by NIC and does not match the ebs config (provisioner %q, parameters %v; want %q, %v). Leaving it unchanged; set ebs.storage_class_name to a new name to have NIC create its own class",
				name, existing.Provisioner, existing.Parameters, desired.Provisioner, desired.Parameters)).
			WithResource("storageclass").
			WithAction("skipped").
			WithMetadata("name", name))
	}

	isDefault, err := storageclass.MarkDefaultIfNone(ctx, client, name)
	if err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.Bool("is_default", isDefault))

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("%s StorageClass ready", name)).
		WithResource("storageclass").
		WithAction("ready").
		WithMetadata("name", name))

	return nil
}

// ebsStorageClassMatches reports whether existing provisions volumes the way
// desired does. The in-tree EBS provisioner counts as a match because EKS
// routes it to the CSI driver.
func ebsStorageClassMatches(existing, desired *storagev1.StorageClass) bool {
	if existing.Provisioner != ebsCSIProvisioner && existing.Provisioner != ebsInTreeProvisioner {
		return false
	}
	return maps.Equal(existing.Parameters, desired.Parameters)
}

// isEKSDefaultGP2Reuse reports whether the config deliberately resolves to
// the gp2 class EKS ships: type gp2 under the default name, with no KMS key
// (which the in-tree class can't provide). The parameter mismatch is then
// expected (EKS's gp2 sets fsType and not encrypted), so NIC reuses the
// class without warning about it.
func isEKSDefaultGP2Reuse(c *Config, existing *storagev1.StorageClass) bool {
	if c.EBSVolumeType() != "gp2" || c.EBSStorageClassName() != ebsEKSDefaultClassName {
		return false
	}
	if c.EBS != nil && c.EBS.KMSKeyARN != "" {
		return false
	}
	return existing.Provisioner == ebsInTreeProvisioner && existing.Parameters["type"] == "gp2"
}

// replaceStorageClass deletes existing and creates desired in its place,
// carrying over the default-class annotation so the cluster default doesn't
// move.
func replaceStorageClass(ctx context.Context, client kubernetes.Interface, existing, desired *storagev1.StorageClass) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.replaceStorageClass")
	defer span.End()

	name := desired.Name
	span.SetAttributes(attribute.String("storage_class_name", name))
	status.Send(ctx, status.NewUpdate(status.LevelProgress,
		fmt.Sprintf("Recreating %s StorageClass to apply the ebs config (existing volumes keep their current settings)", name)).
		WithResource("storageclass").
		WithAction("updating").
		WithMetadata("name", name))

	replacement := desired.DeepCopy()
	if v, ok := existing.Annotations[storageclass.DefaultAnnotation]; ok {
		replacement.Annotations = map[string]string{storageclass.DefaultAnnotation: v}
	}

	if err := client.StorageV1().StorageClasses().Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		span.RecordError(err)
		return fmt.Errorf("failed to delete %s StorageClass for replacement: %w", name, err)
	}
	if _, err := client.StorageV1().StorageClasses().Create(ctx, replacement, metav1.CreateOptions{}); err != nil {
		span.RecordError(err)
		return fmt.Errorf("deleted the %s StorageClass but failed to recreate it, so the cluster has no %s class (and may have no default class) until deploy is rerun, which recreates it: %w", name, name, err)
	}
	return nil
}
