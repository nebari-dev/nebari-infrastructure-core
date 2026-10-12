package aws

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func csiPV(name, volumeID, ns, claim string, policy corev1.PersistentVolumeReclaimPolicy) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: policy,
			ClaimRef:                      &corev1.ObjectReference{Namespace: ns, Name: claim},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{Driver: ebsCSIDriver, VolumeHandle: volumeID},
			},
		},
	}
}

func longhornPV(name, ns, claim string) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			ClaimRef:                      &corev1.ObjectReference{Namespace: ns, Name: claim},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{Driver: "driver.longhorn.io", VolumeHandle: "pvc-" + claim},
			},
		},
	}
}

func pvc(ns, name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func podWithClaim(ns, name, claim string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim},
				},
			}},
		},
	}
}

func node(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestEBSVolumeID(t *testing.T) {
	tests := []struct {
		name string
		pv   *corev1.PersistentVolume
		want string
	}{
		{
			name: "CSI EBS volume",
			pv:   csiPV("pv", "vol-0abc", "ns", "c", corev1.PersistentVolumeReclaimDelete),
			want: "vol-0abc",
		},
		{
			name: "in-tree EBS volume with aws:// prefix",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{VolumeID: "aws://us-east-1a/vol-0def"},
			}}},
			want: "vol-0def",
		},
		{
			name: "in-tree EBS volume with bare ID",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{VolumeID: "vol-0123"},
			}}},
			want: "vol-0123",
		},
		{
			name: "non-EBS CSI volume",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{Driver: "driver.longhorn.io", VolumeHandle: "pvc-1"},
			}}},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ebsVolumeID(tt.pv); got != tt.want {
				t.Errorf("ebsVolumeID() = %q, want %q", got, tt.want)
			}
		})
	}
}

// deletePVOnClaimDelete makes the fake clientset behave like the EBS CSI
// driver: deleting a PVC deletes the PV bound to it.
func deletePVOnClaimDelete(client *fake.Clientset) {
	client.PrependReactor("delete", "persistentvolumeclaims", func(action k8stesting.Action) (bool, runtime.Object, error) {
		del := action.(k8stesting.DeleteAction)
		pvs, _ := client.Tracker().List(corev1.SchemeGroupVersion.WithResource("persistentvolumes"),
			corev1.SchemeGroupVersion.WithKind("PersistentVolume"), "")
		for _, pv := range pvs.(*corev1.PersistentVolumeList).Items {
			if ref := pv.Spec.ClaimRef; ref != nil && ref.Namespace == del.GetNamespace() && ref.Name == del.GetName() {
				_ = client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("persistentvolumes"), "", pv.Name)
			}
		}
		return false, nil, nil
	})
}

func TestReleaseEBSVolumes(t *testing.T) {
	ctx := context.Background()

	objects := []runtime.Object{
		node("node-a"),
		node("node-b"),
		csiPV("pv-db", "vol-db", "keycloak", "keycloak-db-1", corev1.PersistentVolumeReclaimDelete),
		csiPV("pv-keep", "vol-keep", "apps", "important", corev1.PersistentVolumeReclaimRetain),
		longhornPV("pv-longhorn", "apps", "lh"),
		pvc("keycloak", "keycloak-db-1"),
		pvc("apps", "important"),
		pvc("apps", "lh"),
		podWithClaim("keycloak", "keycloak-db-1", "keycloak-db-1"),
		podWithClaim("apps", "keeper", "important"),
		podWithClaim("apps", "lh-user", "lh"),
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "keycloak", Name: "keycloak-0"}},
	}
	client := fake.NewSimpleClientset(objects...)
	deletePVOnClaimDelete(client)

	inv, err := releaseEBSVolumes(ctx, client, ebsReleaseTimeout)
	if err != nil {
		t.Fatalf("releaseEBSVolumes() error = %v", err)
	}

	if !inv.complete {
		t.Error("inventory should be complete")
	}
	if !inv.deleteVolumeIDs["vol-db"] || len(inv.deleteVolumeIDs) != 1 {
		t.Errorf("deleteVolumeIDs = %v, want only vol-db", inv.deleteVolumeIDs)
	}
	if !inv.retainVolumeIDs["vol-keep"] || len(inv.retainVolumeIDs) != 1 {
		t.Errorf("retainVolumeIDs = %v, want only vol-keep", inv.retainVolumeIDs)
	}

	exists := func(err error) bool { return !k8serrors.IsNotFound(err) }

	// Delete-policy EBS claim and the pod mounting it are gone.
	if _, err := client.CoreV1().PersistentVolumeClaims("keycloak").Get(ctx, "keycloak-db-1", metav1.GetOptions{}); exists(err) {
		t.Error("PVC keycloak/keycloak-db-1 should be deleted")
	}
	if _, err := client.CoreV1().Pods("keycloak").Get(ctx, "keycloak-db-1", metav1.GetOptions{}); exists(err) {
		t.Error("pod keycloak/keycloak-db-1 should be deleted")
	}
	// Pods that don't mount a released claim stay.
	if _, err := client.CoreV1().Pods("keycloak").Get(ctx, "keycloak-0", metav1.GetOptions{}); err != nil {
		t.Errorf("pod keycloak/keycloak-0 should be untouched: %v", err)
	}
	// Retain and non-EBS claims, and their pods, are untouched.
	for _, name := range []string{"important", "lh"} {
		if _, err := client.CoreV1().PersistentVolumeClaims("apps").Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Errorf("PVC apps/%s should be untouched: %v", name, err)
		}
	}
	for _, name := range []string{"keeper", "lh-user"} {
		if _, err := client.CoreV1().Pods("apps").Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Errorf("pod apps/%s should be untouched: %v", name, err)
		}
	}
	// Every node is cordoned.
	nodes, _ := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	for _, n := range nodes.Items {
		if !n.Spec.Unschedulable {
			t.Errorf("node %s should be cordoned", n.Name)
		}
	}
}

func TestReleaseEBSVolumesNothingToRelease(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		node("node-a"),
		csiPV("pv-keep", "vol-keep", "apps", "important", corev1.PersistentVolumeReclaimRetain),
	)

	inv, err := releaseEBSVolumes(ctx, client, ebsReleaseTimeout)
	if err != nil {
		t.Fatalf("releaseEBSVolumes() error = %v", err)
	}
	if !inv.complete || len(inv.deleteVolumeIDs) != 0 || !inv.retainVolumeIDs["vol-keep"] {
		t.Errorf("unexpected inventory %+v", inv)
	}
	n, _ := client.CoreV1().Nodes().Get(ctx, "node-a", metav1.GetOptions{})
	if n.Spec.Unschedulable {
		t.Error("nodes should not be cordoned when there is nothing to release")
	}
}

func TestReleaseEBSVolumesTimeout(t *testing.T) {
	// No reactor: the PV never goes away, as if the driver were stuck.
	client := fake.NewSimpleClientset(
		node("node-a"),
		csiPV("pv-db", "vol-db", "keycloak", "keycloak-db-1", corev1.PersistentVolumeReclaimDelete),
		pvc("keycloak", "keycloak-db-1"),
	)

	inv, err := releaseEBSVolumes(context.Background(), client, 0)
	if err == nil || !strings.Contains(err.Error(), "pv-db") {
		t.Fatalf("releaseEBSVolumes() error = %v, want timeout naming pv-db", err)
	}
	// The inventory still drives the post-destroy fallback.
	if !inv.complete || !inv.deleteVolumeIDs["vol-db"] {
		t.Errorf("inventory should still record vol-db, got %+v", inv)
	}
}

func TestReleaseEBSVolumesListFailure(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "persistentvolumes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api down")
	})

	inv, err := releaseEBSVolumes(context.Background(), client, ebsReleaseTimeout)
	if err == nil {
		t.Fatal("expected error")
	}
	if inv.complete {
		t.Error("inventory must be incomplete when PVs could not be listed")
	}
}

type mockEBSClient struct {
	volumes   []ec2types.Volume
	deleted   []string
	deleteErr map[string]error
	filters   []ec2types.Filter
}

func (m *mockEBSClient) DescribeVolumes(_ context.Context, params *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	m.filters = params.Filters
	return &ec2.DescribeVolumesOutput{Volumes: m.volumes}, nil
}

func (m *mockEBSClient) DeleteVolume(_ context.Context, params *ec2.DeleteVolumeInput, _ ...func(*ec2.Options)) (*ec2.DeleteVolumeOutput, error) {
	id := aws.ToString(params.VolumeId)
	if err := m.deleteErr[id]; err != nil {
		return nil, err
	}
	m.deleted = append(m.deleted, id)
	return &ec2.DeleteVolumeOutput{}, nil
}

func vol(id string, state ec2types.VolumeState) ec2types.Volume {
	return ec2types.Volume{VolumeId: aws.String(id), State: state}
}

func TestDeleteOrphanedEBSVolumes(t *testing.T) {
	inv := newEBSInventory()
	inv.complete = true
	inv.deleteVolumeIDs["vol-db"] = true
	inv.deleteVolumeIDs["vol-redis"] = true
	inv.deleteVolumeIDs["vol-gone"] = true
	inv.retainVolumeIDs["vol-keep"] = true

	incomplete := newEBSInventory()

	tests := []struct {
		name        string
		inv         ebsInventory
		volumes     []ec2types.Volume
		deleteErr   map[string]error
		wantDeleted []string
		wantErr     bool
	}{
		{
			name: "deletes recorded Delete-policy volumes only",
			inv:  inv,
			volumes: []ec2types.Volume{
				vol("vol-db", ec2types.VolumeStateAvailable),
				vol("vol-redis", ec2types.VolumeStateAvailable),
				vol("vol-keep", ec2types.VolumeStateAvailable),        // Retain
				vol("vol-old-cluster", ec2types.VolumeStateAvailable), // never seen
				vol("vol-gone", ec2types.VolumeStateDeleting),         // driver got it
			},
			wantDeleted: []string{"vol-db", "vol-redis"},
		},
		{
			name:        "incomplete inventory deletes nothing",
			inv:         incomplete,
			volumes:     []ec2types.Volume{vol("vol-db", ec2types.VolumeStateAvailable)},
			wantDeleted: nil,
		},
		{
			name:        "volume still attached is an error, not a delete",
			inv:         inv,
			volumes:     []ec2types.Volume{vol("vol-db", ec2types.VolumeStateInUse)},
			wantDeleted: nil,
			wantErr:     true,
		},
		{
			name: "delete failure is returned and other volumes still go",
			inv:  inv,
			volumes: []ec2types.Volume{
				vol("vol-db", ec2types.VolumeStateAvailable),
				vol("vol-redis", ec2types.VolumeStateAvailable),
			},
			deleteErr:   map[string]error{"vol-db": errors.New("boom")},
			wantDeleted: []string{"vol-redis"},
			wantErr:     true,
		},
		{
			name:        "no volumes is a no-op",
			inv:         inv,
			volumes:     nil,
			wantDeleted: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockEBSClient{volumes: tt.volumes, deleteErr: tt.deleteErr}
			n, err := deleteOrphanedEBSVolumes(context.Background(), client, "tylergp3", tt.inv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("deleteOrphanedEBSVolumes() error = %v, wantErr %v", err, tt.wantErr)
			}
			sort.Strings(client.deleted)
			if strings.Join(client.deleted, ",") != strings.Join(tt.wantDeleted, ",") {
				t.Errorf("deleted = %v, want %v", client.deleted, tt.wantDeleted)
			}
			if n != len(tt.wantDeleted) {
				t.Errorf("returned count = %d, want %d", n, len(tt.wantDeleted))
			}
			if len(client.filters) != 1 || aws.ToString(client.filters[0].Name) != "tag:"+ebsClusterNameTag ||
				len(client.filters[0].Values) != 1 || client.filters[0].Values[0] != "tylergp3" {
				t.Errorf("unexpected DescribeVolumes filters %+v", client.filters)
			}
		})
	}
}
