package aws

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// ebsCSIDriver is the CSI driver name for EBS volumes, used both as the
	// PV's spec.csi.driver and as the prefix of the tags the driver puts on
	// the volumes it creates.
	ebsCSIDriver = "ebs.csi.aws.com"

	// ebsClusterNameTag is set by the EBS CSI driver on every volume it
	// creates, with the EKS cluster name (NIC's project name) as the value.
	ebsClusterNameTag = "ebs.csi.aws.com/cluster-name"

	// ebsReleaseTimeout bounds how long destroy waits for the EBS CSI driver
	// to delete the volumes behind deleted PVCs. Anything still present
	// afterwards is handled by deleteOrphanedEBSVolumes once the cluster is gone.
	ebsReleaseTimeout = 5 * time.Minute
)

// EBSClient is the subset of the EC2 API used to find and delete the EBS
// volumes left behind after the cluster is destroyed.
type EBSClient interface {
	DescribeVolumes(ctx context.Context, params *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error)
	DeleteVolume(ctx context.Context, params *ec2.DeleteVolumeInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVolumeOutput, error)
}

func newEBSClient(ctx context.Context, region string) (EBSClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return ec2.NewFromConfig(cfg), nil
}

// ebsInventory records the EBS-backed PersistentVolumes seen before teardown.
// The post-destroy fallback only deletes volumes listed in deleteVolumeIDs, so
// a volume NIC never saw (or one whose PV said Retain) is never deleted.
type ebsInventory struct {
	// complete is false when the PV list could not be read; the fallback
	// then reports leftover volumes but deletes none.
	complete bool
	// deleteVolumeIDs are EBS volume IDs whose PV had reclaimPolicy Delete.
	deleteVolumeIDs map[string]bool
	// retainVolumeIDs are EBS volume IDs whose PV had reclaimPolicy Retain
	// (or Recycle); the user asked to keep the data.
	retainVolumeIDs map[string]bool
}

func newEBSInventory() ebsInventory {
	return ebsInventory{
		deleteVolumeIDs: map[string]bool{},
		retainVolumeIDs: map[string]bool{},
	}
}

// ebsVolumeID returns the EBS volume ID behind pv, or "" if pv is not EBS.
// Handles both CSI PVs and in-tree kubernetes.io/aws-ebs PVs (EKS's gp2
// class), whose volumeID may be in the aws://<az>/<vol-id> form.
func ebsVolumeID(pv *corev1.PersistentVolume) string {
	switch {
	case pv.Spec.CSI != nil && pv.Spec.CSI.Driver == ebsCSIDriver:
		return pv.Spec.CSI.VolumeHandle
	case pv.Spec.AWSElasticBlockStore != nil:
		id := pv.Spec.AWSElasticBlockStore.VolumeID
		return id[strings.LastIndex(id, "/")+1:]
	default:
		return ""
	}
}

// releaseEBSVolumes deletes the PVCs bound to EBS volumes with reclaimPolicy
// Delete so the EBS CSI driver deletes the volumes while the cluster is still
// up, then waits up to timeout for those PVs to disappear. It must run after
// anything that needs to schedule new pods (e.g. the Longhorn uninstall job),
// because it cordons every node: StatefulSets and operators recreate deleted
// PVCs, and cordoning keeps those WaitForFirstConsumer claims from ever
// getting a volume.
//
// The returned inventory is valid whenever the PV list succeeded, even if a
// later step failed; callers pass it to deleteOrphanedEBSVolumes.
func releaseEBSVolumes(ctx context.Context, client kubernetes.Interface, timeout time.Duration) (ebsInventory, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.releaseEBSVolumes")
	defer span.End()

	inv := newEBSInventory()

	pvs, err := client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		span.RecordError(err)
		return inv, fmt.Errorf("failed to list PersistentVolumes: %w", err)
	}
	inv.complete = true

	// PVs to release, and the claims bound to them, keyed by namespace.
	pending := map[string]bool{}
	claims := map[string]map[string]bool{}
	for i := range pvs.Items {
		pv := &pvs.Items[i]
		volumeID := ebsVolumeID(pv)
		if volumeID == "" {
			continue
		}
		if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimDelete {
			inv.retainVolumeIDs[volumeID] = true
			continue
		}
		inv.deleteVolumeIDs[volumeID] = true
		pending[pv.Name] = true
		if ref := pv.Spec.ClaimRef; ref != nil {
			if claims[ref.Namespace] == nil {
				claims[ref.Namespace] = map[string]bool{}
			}
			claims[ref.Namespace][ref.Name] = true
		}
	}

	span.SetAttributes(
		attribute.Int("ebs_delete_volumes", len(inv.deleteVolumeIDs)),
		attribute.Int("ebs_retain_volumes", len(inv.retainVolumeIDs)),
	)
	if len(inv.retainVolumeIDs) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Keeping %d EBS volume(s) whose PersistentVolume reclaim policy is Retain", len(inv.retainVolumeIDs))).
			WithResource("ebs-volume").WithAction("cleanup"))
	}
	if len(pending) == 0 {
		return inv, nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo,
		fmt.Sprintf("Deleting %d EBS-backed PersistentVolumeClaim(s) so the EBS CSI driver removes their volumes", len(pending))).
		WithResource("ebs-volume").WithAction("cleanup"))

	if err := cordonAllNodes(ctx, client); err != nil {
		span.RecordError(err)
		return inv, err
	}

	var errs []error
	for ns, names := range claims {
		for name := range names {
			err := client.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, name, metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				errs = append(errs, fmt.Errorf("delete PVC %s/%s: %w", ns, name, err))
			}
		}
		// PVC protection keeps a claim until no pod mounts it, so the pods go too.
		if err := deletePodsUsingClaims(ctx, client, ns, names); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		err := errors.Join(errs...)
		span.RecordError(err)
		return inv, err
	}

	if err := waitForPVsGone(ctx, client, pending, timeout); err != nil {
		span.RecordError(err)
		return inv, err
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess,
		fmt.Sprintf("EBS CSI driver deleted %d volume(s)", len(pending))).
		WithResource("ebs-volume").WithAction("cleanup"))
	return inv, nil
}

// cordonAllNodes marks every node unschedulable.
func cordonAllNodes(ctx context.Context, client kubernetes.Interface) error {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}
	patch := []byte(`{"spec":{"unschedulable":true}}`)
	for i := range nodes.Items {
		node := &nodes.Items[i]
		if node.Spec.Unschedulable {
			continue
		}
		if _, err := client.CoreV1().Nodes().Patch(ctx, node.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("failed to cordon node %s: %w", node.Name, err)
		}
	}
	return nil
}

// deletePodsUsingClaims deletes the pods in ns that mount any of claims.
func deletePodsUsingClaims(ctx context.Context, client kubernetes.Interface, ns string, claims map[string]bool) error {
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods in %s: %w", ns, err)
	}
	var errs []error
	for i := range pods.Items {
		pod := &pods.Items[i]
		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim == nil || !claims[vol.PersistentVolumeClaim.ClaimName] {
				continue
			}
			err := client.CoreV1().Pods(ns).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				errs = append(errs, fmt.Errorf("delete pod %s/%s: %w", ns, pod.Name, err))
			}
			break
		}
	}
	return errors.Join(errs...)
}

// waitForPVsGone polls until none of the named PVs exist, or timeout elapses.
func waitForPVsGone(ctx context.Context, client kubernetes.Interface, names map[string]bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	const pollInterval = 5 * time.Second

	for {
		pvs, err := client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to list PersistentVolumes while polling: %w", err)
		}
		var remaining []string
		for i := range pvs.Items {
			if names[pvs.Items[i].Name] {
				remaining = append(remaining, pvs.Items[i].Name)
			}
		}
		if len(remaining) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			sort.Strings(remaining)
			return fmt.Errorf("%d EBS PersistentVolume(s) still present after %s: %s",
				len(remaining), timeout, strings.Join(remaining, ", "))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// deleteOrphanedEBSVolumes runs after the cluster is destroyed and deletes the
// detached EBS volumes the CSI driver did not get to. Only volumes recorded in
// inv.deleteVolumeIDs are deleted; any other volume tagged with this cluster's
// name (a Retain PV, or a volume left by an earlier cluster with the same
// name) is reported, never deleted.
func deleteOrphanedEBSVolumes(ctx context.Context, client EBSClient, clusterName string, inv ebsInventory) (int, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.deleteOrphanedEBSVolumes")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.Bool("inventory_complete", inv.complete),
	)

	var volumes []ec2types.Volume
	paginator := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:" + ebsClusterNameTag), Values: []string{clusterName}},
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			span.RecordError(err)
			return 0, fmt.Errorf("failed to describe EBS volumes: %w", err)
		}
		volumes = append(volumes, page.Volumes...)
	}

	var deleted int
	var kept, retained []string
	var errs []error
	for _, vol := range volumes {
		id := aws.ToString(vol.VolumeId)
		switch {
		case vol.State == ec2types.VolumeStateDeleting || vol.State == ec2types.VolumeStateDeleted:
			continue
		case inv.retainVolumeIDs[id]:
			retained = append(retained, id)
			continue
		case !inv.complete || !inv.deleteVolumeIDs[id]:
			kept = append(kept, id)
			continue
		}
		if vol.State != ec2types.VolumeStateAvailable {
			errs = append(errs, fmt.Errorf("volume %s is %s, not available", id, vol.State))
			continue
		}
		if _, err := client.DeleteVolume(ctx, &ec2.DeleteVolumeInput{VolumeId: aws.String(id)}); err != nil {
			errs = append(errs, fmt.Errorf("delete volume %s: %w", id, err))
			continue
		}
		deleted++
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleted orphaned EBS volume %s", id)).
			WithResource("ebs-volume").WithAction("deleted"))
	}

	span.SetAttributes(
		attribute.Int("ebs_volumes_deleted", deleted),
		attribute.Int("ebs_volumes_kept", len(kept)),
		attribute.Int("ebs_volumes_retained", len(retained)),
	)

	if len(retained) > 0 {
		sort.Strings(retained)
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Kept %d EBS volume(s) whose PersistentVolume reclaim policy was Retain: %s",
				len(retained), strings.Join(retained, ", "))).
			WithResource("ebs-volume").WithAction("cleanup"))
	}
	if len(kept) > 0 {
		sort.Strings(kept)
		reason := "not recorded with reclaim policy Delete before teardown"
		if !inv.complete {
			reason = "the cluster's PersistentVolumes could not be read before teardown"
		}
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			fmt.Sprintf("Left %d EBS volume(s) tagged %s=%s in place (%s): %s",
				len(kept), ebsClusterNameTag, clusterName, reason, strings.Join(kept, ", "))).
			WithResource("ebs-volume").WithAction("cleanup"))
	}

	if err := errors.Join(errs...); err != nil {
		span.RecordError(err)
		return deleted, err
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Orphaned EBS volume cleanup complete: %d deleted", deleted)).
		WithResource("ebs-volume").WithAction("cleanup"))
	return deleted, nil
}
