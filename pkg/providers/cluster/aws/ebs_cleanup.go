package aws

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
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
	// ebsCSIDriver is the CSI driver name for EBS volumes, used as the PV's
	// spec.csi.driver.
	ebsCSIDriver = "ebs.csi.aws.com"

	// ebsClusterTag is set by the EBS CSI driver on every volume it creates
	// when it runs with --k8s-tag-cluster-id, which the EKS add-on sets to the
	// cluster name (NIC's project name). The driver has set this tag across
	// all the versions NIC can get from the add-on, unlike
	// ebs.csi.aws.com/cluster-name, which only appears from v1.58.0. It is
	// only used to report leftover volumes; deletion is driven by volume ID.
	ebsClusterTag = "KubernetesCluster"

	// ebsReleaseTimeout bounds how long destroy waits for the EBS CSI driver
	// to delete the volumes behind deleted PVCs. Anything still present
	// afterwards is handled by deleteOrphanedEBSVolumes once the cluster is gone.
	ebsReleaseTimeout = 5 * time.Minute

	// ebsDescribeChunk caps how many volume IDs go into one DescribeVolumes
	// volume-id filter.
	ebsDescribeChunk = 100
)

// EBSClient is the subset of the EC2 API used to find and delete the EBS
// volumes left behind after the cluster is destroyed. *ec2.Client from
// newEC2Client satisfies it.
type EBSClient interface {
	DescribeVolumes(ctx context.Context, params *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error)
	DeleteVolume(ctx context.Context, params *ec2.DeleteVolumeInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVolumeOutput, error)
}

// ebsInventory records the EBS-backed PersistentVolumes seen before teardown.
// The post-destroy fallback only deletes volumes listed in deleteVolumeIDs, so
// a volume NIC never saw (or one whose PV said Retain) is never deleted.
type ebsInventory struct {
	// complete is false when the PV list could not be read; the fallback
	// then deletes nothing and returns an error for any tagged volume left.
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
// later step failed; callers pass it to deleteOrphanedEBSVolumes. The returned
// node names are the nodes this call cordoned (also on error), so a caller
// whose teardown then fails can hand them to uncordonNodes.
func releaseEBSVolumes(ctx context.Context, client kubernetes.Interface, timeout time.Duration) (ebsInventory, []string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.releaseEBSVolumes")
	defer span.End()

	inv := newEBSInventory()

	pvs, err := client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		span.RecordError(err)
		return inv, nil, fmt.Errorf("failed to list PersistentVolumes: %w", err)
	}
	inv.complete = true

	// PVs to wait for, and the claims bound to them, keyed by namespace.
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
		// A PV with no claim can't be released by deleting a PVC, so there is
		// nothing to wait for; the post-destroy sweep deletes its volume.
		ref := pv.Spec.ClaimRef
		if ref == nil {
			continue
		}
		pending[pv.Name] = true
		if claims[ref.Namespace] == nil {
			claims[ref.Namespace] = map[string]bool{}
		}
		claims[ref.Namespace][ref.Name] = true
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
		return inv, nil, nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo,
		fmt.Sprintf("Deleting %d EBS-backed PersistentVolumeClaim(s) so the EBS CSI driver removes their volumes", len(pending))).
		WithResource("ebs-volume").WithAction("cleanup"))

	cordoned, err := cordonAllNodes(ctx, client)
	if err != nil {
		span.RecordError(err)
		return inv, cordoned, err
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
		return inv, cordoned, err
	}

	if err := waitForPVsGone(ctx, client, pending, timeout); err != nil {
		span.RecordError(err)
		return inv, cordoned, err
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess,
		fmt.Sprintf("EBS CSI driver deleted %d volume(s)", len(pending))).
		WithResource("ebs-volume").WithAction("cleanup"))
	return inv, cordoned, nil
}

// cordonAllNodes marks every node unschedulable and returns the names of the
// nodes it cordoned. Nodes that were already unschedulable are skipped and
// not returned, so uncordonNodes never undoes a cordon NIC didn't make. On
// error the nodes cordoned so far are still returned.
func cordonAllNodes(ctx context.Context, client kubernetes.Interface) ([]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.cordonAllNodes")
	defer span.End()

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	patch := []byte(`{"spec":{"unschedulable":true}}`)
	var cordoned []string
	for i := range nodes.Items {
		node := &nodes.Items[i]
		if node.Spec.Unschedulable {
			continue
		}
		if _, err := client.CoreV1().Nodes().Patch(ctx, node.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			err = fmt.Errorf("failed to cordon node %s: %w", node.Name, err)
			span.RecordError(err)
			return cordoned, err
		}
		cordoned = append(cordoned, node.Name)
	}
	span.SetAttributes(attribute.Int("nodes_cordoned", len(cordoned)))
	return cordoned, nil
}

// uncordonNodes marks the named nodes schedulable again. Nodes that no longer
// exist are skipped.
func uncordonNodes(ctx context.Context, client kubernetes.Interface, names []string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.uncordonNodes")
	defer span.End()
	span.SetAttributes(attribute.Int("nodes", len(names)))

	patch := []byte(`{"spec":{"unschedulable":false}}`)
	var errs []error
	for _, name := range names {
		_, err := client.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("uncordon node %s: %w", name, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// deletePodsUsingClaims deletes the pods in ns that mount any of claims.
func deletePodsUsingClaims(ctx context.Context, client kubernetes.Interface, ns string, claims map[string]bool) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.deletePodsUsingClaims")
	defer span.End()
	span.SetAttributes(attribute.String("namespace", ns))

	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		span.RecordError(err)
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
	if err := errors.Join(errs...); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// waitForPVsGone polls until none of the named PVs exist, or timeout elapses.
func waitForPVsGone(ctx context.Context, client kubernetes.Interface, names map[string]bool, timeout time.Duration) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.waitForPVsGone")
	defer span.End()
	span.SetAttributes(
		attribute.Int("pvs", len(names)),
		attribute.String("timeout", timeout.String()),
	)

	deadline := time.Now().Add(timeout)
	const pollInterval = 5 * time.Second

	for {
		pvs, err := client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
		if err != nil {
			err = fmt.Errorf("failed to list PersistentVolumes while polling: %w", err)
			span.RecordError(err)
			return err
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
			err := fmt.Errorf("%d EBS PersistentVolume(s) still present after %s: %s",
				len(remaining), timeout, strings.Join(remaining, ", "))
			span.RecordError(err)
			return err
		}

		select {
		case <-ctx.Done():
			span.RecordError(ctx.Err())
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// describeVolumes returns every volume matching filter, following pagination.
func describeVolumes(ctx context.Context, client EBSClient, filter ec2types.Filter) ([]ec2types.Volume, error) {
	var volumes []ec2types.Volume
	paginator := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{
		Filters: []ec2types.Filter{filter},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, page.Volumes...)
	}
	return volumes, nil
}

// volumeGone reports whether vol is already on its way out.
func volumeGone(vol ec2types.Volume) bool {
	return vol.State == ec2types.VolumeStateDeleting || vol.State == ec2types.VolumeStateDeleted
}

// deleteOrphanedEBSVolumes runs after the cluster is destroyed and deletes the
// detached EBS volumes the CSI driver did not get to.
//
// Deletion is driven by volume ID: the volumes recorded in inv.deleteVolumeIDs
// are looked up by ID (not by tag, so it works whatever tags the driver
// version wrote), and any that still exist are deleted. One that can't be
// deleted (still attached, or the delete call fails) is returned as an error.
//
// Volumes tagged KubernetesCluster=<clusterName> are then listed for the
// report and never deleted. With a complete inventory, a Retain volume is
// reported as kept and a volume NIC never saw (e.g. left by an earlier
// cluster with the same name) as a warning. With an incomplete inventory NIC
// can't tell Delete volumes from Retain ones, so any tagged volume left is
// returned as an error for the user to review.
func deleteOrphanedEBSVolumes(ctx context.Context, client EBSClient, clusterName string, inv ebsInventory) (int, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.deleteOrphanedEBSVolumes")
	defer span.End()
	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.Bool("inventory_complete", inv.complete),
	)

	var deleted int
	var errs []error

	// Pass 1: delete the recorded Delete-policy volumes that still exist.
	if inv.complete && len(inv.deleteVolumeIDs) > 0 {
		ids := make([]string, 0, len(inv.deleteVolumeIDs))
		for id := range inv.deleteVolumeIDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		// A volume-id filter, unlike the VolumeIds parameter, doesn't fail
		// the whole call when some IDs (the ones the driver already deleted)
		// no longer exist.
		var found []ec2types.Volume
		for start := 0; start < len(ids); start += ebsDescribeChunk {
			end := min(start+ebsDescribeChunk, len(ids))
			vols, err := describeVolumes(ctx, client, ec2types.Filter{Name: aws.String("volume-id"), Values: ids[start:end]})
			if err != nil {
				err = fmt.Errorf("failed to describe recorded EBS volumes: %w", err)
				span.RecordError(err)
				return 0, err
			}
			found = append(found, vols...)
		}
		for _, vol := range found {
			id := aws.ToString(vol.VolumeId)
			if volumeGone(vol) || !inv.deleteVolumeIDs[id] {
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
	}

	// Pass 2: report the other volumes tagged with this cluster's name.
	tagged, err := describeVolumes(ctx, client, ec2types.Filter{Name: aws.String("tag:" + ebsClusterTag), Values: []string{clusterName}})
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to list EBS volumes tagged %s=%s: %w", ebsClusterTag, clusterName, err))
	}
	var unknown, retained []string
	for _, vol := range tagged {
		id := aws.ToString(vol.VolumeId)
		switch {
		case volumeGone(vol):
		case inv.complete && inv.deleteVolumeIDs[id]:
			// Handled (and, on failure, reported) in pass 1.
		case inv.retainVolumeIDs[id]:
			retained = append(retained, id)
		default:
			unknown = append(unknown, id)
		}
	}
	sort.Strings(retained)
	sort.Strings(unknown)

	span.SetAttributes(
		attribute.Int("ebs_volumes_deleted", deleted),
		attribute.Int("ebs_volumes_unknown", len(unknown)),
		attribute.Int("ebs_volumes_retained", len(retained)),
	)

	if len(retained) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Kept %d EBS volume(s) whose PersistentVolume reclaim policy was Retain: %s",
				len(retained), strings.Join(retained, ", "))).
			WithResource("ebs-volume").WithAction("cleanup"))
	}
	if len(unknown) > 0 {
		if inv.complete {
			status.Send(ctx, status.NewUpdate(status.LevelWarning,
				fmt.Sprintf("Left %d EBS volume(s) tagged %s=%s in place (not recorded with reclaim policy Delete before teardown): %s",
					len(unknown), ebsClusterTag, clusterName, strings.Join(unknown, ", "))).
				WithResource("ebs-volume").WithAction("cleanup"))
		} else {
			errs = append(errs, fmt.Errorf("%d EBS volume(s) tagged %s=%s remain and were not deleted because the cluster's PersistentVolumes could not be read before teardown; review and delete any you don't need: %s",
				len(unknown), ebsClusterTag, clusterName, strings.Join(unknown, ", ")))
		}
	}

	if err := errors.Join(errs...); err != nil {
		span.RecordError(err)
		return deleted, err
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Orphaned EBS volume cleanup complete: %d deleted", deleted)).
		WithResource("ebs-volume").WithAction("cleanup"))
	return deleted, nil
}
