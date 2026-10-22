package aws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func csiPV(name, volumeID, ns, claim string, policy corev1.PersistentVolumeReclaimPolicy) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: policy,
			ClaimRef:                      &corev1.ObjectReference{Namespace: ns, Name: claim, UID: k8stypes.UID("uid-" + claim)},
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

	cordonedByUser := node("node-c")
	cordonedByUser.Spec.Unschedulable = true

	objects := []runtime.Object{
		node("node-a"),
		node("node-b"),
		cordonedByUser,
		csiPV("pv-db", "vol-db", "keycloak", "keycloak-db-1", corev1.PersistentVolumeReclaimDelete),
		csiPV("pv-kc-keep", "vol-kc-keep", "keycloak", "keycloak-backup", corev1.PersistentVolumeReclaimRetain),
		csiPV("pv-keep", "vol-keep", "apps", "important", corev1.PersistentVolumeReclaimRetain),
		longhornPV("pv-longhorn", "apps", "lh"),
		pvc("keycloak", "keycloak-db-1"),
		pvc("keycloak", "keycloak-backup"),
		pvc("apps", "important"),
		pvc("apps", "lh"),
		podWithClaim("keycloak", "keycloak-db-1", "keycloak-db-1"),
		// Same namespace as a released claim, but mounts only a Retain claim.
		podWithClaim("keycloak", "keycloak-backup", "keycloak-backup"),
		podWithClaim("apps", "keeper", "important"),
		podWithClaim("apps", "lh-user", "lh"),
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "keycloak", Name: "keycloak-0"}},
	}
	client := fake.NewSimpleClientset(objects...)
	deletePVOnClaimDelete(client)

	inv, cordoned, err := releaseEBSVolumes(ctx, client, ebsReleaseTimeout)
	if err != nil {
		t.Fatalf("releaseEBSVolumes() error = %v", err)
	}

	if !inv.complete {
		t.Error("inventory should be complete")
	}
	if !inv.deleteVolumeIDs["vol-db"] || len(inv.deleteVolumeIDs) != 1 {
		t.Errorf("deleteVolumeIDs = %v, want only vol-db", inv.deleteVolumeIDs)
	}
	if !inv.retainVolumeIDs["vol-keep"] || !inv.retainVolumeIDs["vol-kc-keep"] || len(inv.retainVolumeIDs) != 2 {
		t.Errorf("retainVolumeIDs = %v, want vol-keep and vol-kc-keep", inv.retainVolumeIDs)
	}

	exists := func(err error) bool { return !k8serrors.IsNotFound(err) }

	// Delete-policy EBS claim and the pod mounting it are gone.
	if _, err := client.CoreV1().PersistentVolumeClaims("keycloak").Get(ctx, "keycloak-db-1", metav1.GetOptions{}); exists(err) {
		t.Error("PVC keycloak/keycloak-db-1 should be deleted")
	}
	if _, err := client.CoreV1().Pods("keycloak").Get(ctx, "keycloak-db-1", metav1.GetOptions{}); exists(err) {
		t.Error("pod keycloak/keycloak-db-1 should be deleted")
	}
	// Pods in the same namespace that don't mount a released claim stay,
	// including one that mounts a Retain claim.
	for _, name := range []string{"keycloak-0", "keycloak-backup"} {
		if _, err := client.CoreV1().Pods("keycloak").Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Errorf("pod keycloak/%s should be untouched: %v", name, err)
		}
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("keycloak").Get(ctx, "keycloak-backup", metav1.GetOptions{}); err != nil {
		t.Errorf("PVC keycloak/keycloak-backup should be untouched: %v", err)
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
	// Every node is cordoned, but only the ones NIC cordoned are returned.
	nodes, _ := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	for _, n := range nodes.Items {
		if !n.Spec.Unschedulable {
			t.Errorf("node %s should be cordoned", n.Name)
		}
	}
	sort.Strings(cordoned)
	if strings.Join(cordoned, ",") != "node-a,node-b" {
		t.Errorf("cordoned = %v, want [node-a node-b] (node-c was already cordoned)", cordoned)
	}
}

func TestReleaseEBSVolumesNothingToRelease(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		node("node-a"),
		csiPV("pv-keep", "vol-keep", "apps", "important", corev1.PersistentVolumeReclaimRetain),
	)

	inv, cordoned, err := releaseEBSVolumes(ctx, client, ebsReleaseTimeout)
	if err != nil {
		t.Fatalf("releaseEBSVolumes() error = %v", err)
	}
	if !inv.complete || len(inv.deleteVolumeIDs) != 0 || !inv.retainVolumeIDs["vol-keep"] {
		t.Errorf("unexpected inventory %+v", inv)
	}
	if len(cordoned) != 0 {
		t.Errorf("cordoned = %v, want none", cordoned)
	}
	n, _ := client.CoreV1().Nodes().Get(ctx, "node-a", metav1.GetOptions{})
	if n.Spec.Unschedulable {
		t.Error("nodes should not be cordoned when there is nothing to release")
	}
}

func TestReleaseEBSVolumesUnboundPVIsReportedNotDeleted(t *testing.T) {
	ctx := context.Background()
	unclaimed := csiPV("pv-free", "vol-free", "", "", corev1.PersistentVolumeReclaimDelete)
	unclaimed.Spec.ClaimRef = nil
	// Pre-bound: reserved for a claim by name, but no claim UID, so not bound.
	prebound := csiPV("pv-reserved", "vol-reserved", "apps", "future", corev1.PersistentVolumeReclaimDelete)
	prebound.Spec.ClaimRef.UID = ""
	client := fake.NewSimpleClientset(
		node("node-a"),
		unclaimed,
		prebound,
		pvc("apps", "future"),
		csiPV("pv-db", "vol-db", "keycloak", "keycloak-db-1", corev1.PersistentVolumeReclaimDelete),
		pvc("keycloak", "keycloak-db-1"),
	)
	deletePVOnClaimDelete(client)

	// Zero timeout: the first poll must already find nothing to wait for, so
	// this fails if the claimless PV is waited on.
	inv, _, err := releaseEBSVolumes(ctx, client, 0)
	if err != nil {
		t.Fatalf("releaseEBSVolumes() error = %v, want no wait on the unclaimed PV", err)
	}
	// An unbound PV is a hand-made static PV that can point at any volume, so
	// its volume is reported, never queued for the post-destroy delete.
	if inv.deleteVolumeIDs["vol-free"] || !inv.deleteVolumeIDs["vol-db"] || len(inv.deleteVolumeIDs) != 1 {
		t.Errorf("deleteVolumeIDs = %v, want only vol-db", inv.deleteVolumeIDs)
	}
	if !inv.unboundVolumeIDs["vol-free"] || !inv.unboundVolumeIDs["vol-reserved"] || len(inv.unboundVolumeIDs) != 2 {
		t.Errorf("unboundVolumeIDs = %v, want vol-free and vol-reserved", inv.unboundVolumeIDs)
	}
	// The claim a pre-bound PV is reserved for is left alone.
	if _, err := client.CoreV1().PersistentVolumeClaims("apps").Get(ctx, "future", metav1.GetOptions{}); err != nil {
		t.Errorf("PVC apps/future should be untouched: %v", err)
	}
}

func TestReleaseEBSVolumesTimeout(t *testing.T) {
	// No reactor: the PV never goes away, as if the driver were stuck.
	client := fake.NewSimpleClientset(
		node("node-a"),
		csiPV("pv-db", "vol-db", "keycloak", "keycloak-db-1", corev1.PersistentVolumeReclaimDelete),
		pvc("keycloak", "keycloak-db-1"),
	)

	inv, cordoned, err := releaseEBSVolumes(context.Background(), client, 0)
	if err == nil || !strings.Contains(err.Error(), "pv-db") {
		t.Fatalf("releaseEBSVolumes() error = %v, want timeout naming pv-db", err)
	}
	// The inventory still drives the post-destroy fallback.
	if !inv.complete || !inv.deleteVolumeIDs["vol-db"] {
		t.Errorf("inventory should still record vol-db, got %+v", inv)
	}
	// The cordoned nodes are still reported so a failed teardown can undo them.
	if len(cordoned) != 1 || cordoned[0] != "node-a" {
		t.Errorf("cordoned = %v, want [node-a]", cordoned)
	}
}

func TestReleaseEBSVolumesListFailure(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "persistentvolumes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api down")
	})

	inv, _, err := releaseEBSVolumes(context.Background(), client, ebsReleaseTimeout)
	if err == nil {
		t.Fatal("expected error")
	}
	if inv.complete {
		t.Error("inventory must be incomplete when PVs could not be listed")
	}
}

func cordonedNodes(names ...string) []runtime.Object {
	var objs []runtime.Object
	for _, name := range names {
		n := node(name)
		n.Spec.Unschedulable = true
		objs = append(objs, n)
	}
	return objs
}

func TestUncordonNodes(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(cordonedNodes("node-a", "node-b", "node-c")...)

	// node-gone no longer exists; node-c was cordoned by someone else.
	n, err := uncordonNodes(ctx, client, []string{"node-a", "node-b", "node-gone"})
	if err != nil {
		t.Fatalf("uncordonNodes() error = %v", err)
	}
	if n != 2 {
		t.Errorf("uncordonNodes() = %d, want 2", n)
	}
	want := map[string]bool{"node-a": false, "node-b": false, "node-c": true}
	for name, unschedulable := range want {
		got, err := client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get node %s: %v", name, err)
		}
		if got.Spec.Unschedulable != unschedulable {
			t.Errorf("node %s unschedulable = %v, want %v", name, got.Spec.Unschedulable, unschedulable)
		}
	}
}

func TestUncordonNodesAPIServerErrors(t *testing.T) {
	// client-go wraps transport failures in a *url.Error.
	nxdomain := &url.Error{Op: "Patch", URL: "https://gone.eks.amazonaws.com", Err: &net.DNSError{Err: "no such host", Name: "gone.eks.amazonaws.com", IsNotFound: true}}
	dnsTimeout := &url.Error{Op: "Patch", URL: "https://live.eks.amazonaws.com", Err: &net.DNSError{Err: "i/o timeout", Name: "live.eks.amazonaws.com", IsTimeout: true}}
	refused := &url.Error{Op: "Patch", URL: "https://10.0.0.1", Err: &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}}
	forbidden := errors.New("forbidden")

	tests := []struct {
		name     string
		nodeErrs map[string]error
		wantErr  string
		wantN    int
	}{
		{name: "DNS name gone is nothing to uncordon", nodeErrs: map[string]error{"node-a": nxdomain}},
		{name: "connection refused is nothing to uncordon", nodeErrs: map[string]error{"node-a": refused}},
		{name: "DNS timeout is returned", nodeErrs: map[string]error{"node-a": dnsTimeout, "node-b": dnsTimeout}, wantErr: "i/o timeout"},
		{name: "other errors are returned", nodeErrs: map[string]error{"node-a": forbidden, "node-b": forbidden}, wantErr: "forbidden"},
		{
			name:     "earlier errors survive an unreachable API server",
			nodeErrs: map[string]error{"node-a": forbidden, "node-b": nxdomain},
			wantErr:  "uncordon node node-a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(cordonedNodes("node-a", "node-b")...)
			client.PrependReactor("patch", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
				if err := tt.nodeErrs[action.(k8stesting.PatchAction).GetName()]; err != nil {
					return true, nil, err
				}
				return false, nil, nil
			})
			n, err := uncordonNodes(context.Background(), client, []string{"node-a", "node-b"})
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("uncordonNodes() error = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("uncordonNodes() error = %v, want one containing %q", err, tt.wantErr)
			}
			if n != tt.wantN {
				t.Errorf("uncordonNodes() = %d, want %d", n, tt.wantN)
			}
		})
	}
}

// nodePatchServer is a minimal API server that answers node PATCHes, so a
// real clientset (which, unlike the fake one, honors ctx) can be used.
func nodePatchServer(t *testing.T) (kubernetes.Interface, *[]string) {
	t.Helper()
	var patched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := strings.CutPrefix(r.URL.Path, "/api/v1/nodes/")
		if r.Method != http.MethodPatch || !ok {
			http.NotFound(w, r)
			return
		}
		patched = append(patched, name)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&corev1.Node{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{Name: name},
		})
	}))
	t.Cleanup(srv.Close)
	client, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatalf("new clientset: %v", err)
	}
	return client, &patched
}

func TestAfterFailedTeardown(t *testing.T) {
	ctx := context.Background()
	destroyErr := errors.New("tofu destroy failed")

	t.Run("uncordons the cordoned nodes and returns the destroy error", func(t *testing.T) {
		client := fake.NewSimpleClientset(cordonedNodes("node-a", "node-b")...)
		err := afterFailedTeardown(ctx, destroyErr, client, []string{"node-a"})
		if !errors.Is(err, destroyErr) || err.Error() != destroyErr.Error() {
			t.Fatalf("afterFailedTeardown() = %v, want only the destroy error", err)
		}
		a, _ := client.CoreV1().Nodes().Get(ctx, "node-a", metav1.GetOptions{})
		b, _ := client.CoreV1().Nodes().Get(ctx, "node-b", metav1.GetOptions{})
		if a.Spec.Unschedulable {
			t.Error("node-a should be uncordoned")
		}
		if !b.Spec.Unschedulable {
			t.Error("node-b was not cordoned by NIC and should stay cordoned")
		}
	})

	t.Run("still uncordons when ctx is cancelled", func(t *testing.T) {
		// A real clientset against a test server: it fails requests on a
		// cancelled ctx, so this only passes if the uncordon is detached.
		client, patched := nodePatchServer(t)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := afterFailedTeardown(cctx, destroyErr, client, []string{"node-a"})
		if err.Error() != destroyErr.Error() {
			t.Fatalf("afterFailedTeardown() = %v, want only the destroy error", err)
		}
		if strings.Join(*patched, ",") != "node-a" {
			t.Errorf("patched nodes = %v, want [node-a] even after an interrupt", *patched)
		}
	})

	t.Run("uncordon failure is joined with the destroy error", func(t *testing.T) {
		client := fake.NewSimpleClientset(cordonedNodes("node-a")...)
		client.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("forbidden")
		})
		err := afterFailedTeardown(ctx, destroyErr, client, []string{"node-a"})
		if !errors.Is(err, destroyErr) || !strings.Contains(err.Error(), "uncordon node node-a") {
			t.Fatalf("afterFailedTeardown() = %v, want destroy and uncordon errors", err)
		}
	})

	t.Run("nothing cordoned needs no client", func(t *testing.T) {
		if err := afterFailedTeardown(ctx, destroyErr, nil, nil); err != destroyErr {
			t.Fatalf("afterFailedTeardown() = %v, want the destroy error", err)
		}
	})
}

// mockEBSClient evaluates the single filter deleteOrphanedEBSVolumes sends
// (volume-id or tag:<key>) against volumes.
type mockEBSClient struct {
	volumes   []ec2types.Volume
	deleted   []string
	deleteErr map[string]error
	filters   []string
}

func (m *mockEBSClient) DescribeVolumes(_ context.Context, params *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	if len(params.Filters) != 1 || len(params.VolumeIds) != 0 {
		return nil, errors.New("mock expects exactly one filter and no VolumeIds")
	}
	f := params.Filters[0]
	name := aws.ToString(f.Name)
	m.filters = append(m.filters, name)
	values := map[string]bool{}
	for _, v := range f.Values {
		values[v] = true
	}
	var out []ec2types.Volume
	for _, v := range m.volumes {
		switch {
		case name == "volume-id":
			if values[aws.ToString(v.VolumeId)] {
				out = append(out, v)
			}
		case strings.HasPrefix(name, "tag:"):
			key := strings.TrimPrefix(name, "tag:")
			for _, tag := range v.Tags {
				if aws.ToString(tag.Key) == key && values[aws.ToString(tag.Value)] {
					out = append(out, v)
					break
				}
			}
		default:
			return nil, errors.New("unexpected filter " + name)
		}
	}
	return &ec2.DescribeVolumesOutput{Volumes: out}, nil
}

func (m *mockEBSClient) DeleteVolume(_ context.Context, params *ec2.DeleteVolumeInput, _ ...func(*ec2.Options)) (*ec2.DeleteVolumeOutput, error) {
	id := aws.ToString(params.VolumeId)
	if err := m.deleteErr[id]; err != nil {
		return nil, err
	}
	m.deleted = append(m.deleted, id)
	return &ec2.DeleteVolumeOutput{}, nil
}

// vol is an EBS volume tagged by the CSI driver for the named cluster.
func vol(id string, state ec2types.VolumeState, cluster string) ec2types.Volume {
	return ec2types.Volume{
		VolumeId: aws.String(id),
		State:    state,
		Tags:     []ec2types.Tag{{Key: aws.String(ebsClusterTag), Value: aws.String(cluster)}},
	}
}

// untaggedVol is an EBS volume with no cluster tag.
func untaggedVol(id string, state ec2types.VolumeState) ec2types.Volume {
	return ec2types.Volume{VolumeId: aws.String(id), State: state}
}

func TestDeleteOrphanedEBSVolumes(t *testing.T) {
	const cluster = "tylergp3"

	inv := newEBSInventory()
	inv.complete = true
	inv.deleteVolumeIDs["vol-db"] = true
	inv.deleteVolumeIDs["vol-redis"] = true
	inv.deleteVolumeIDs["vol-gone"] = true
	inv.deleteVolumeIDs["vol-vanished"] = true
	inv.retainVolumeIDs["vol-keep"] = true
	inv.unboundVolumeIDs["vol-static"] = true
	inv.unboundVolumeIDs["vol-static-2"] = true

	// The !inv.complete guard is the only thing keeping vol-db here.
	incomplete := newEBSInventory()
	incomplete.deleteVolumeIDs["vol-db"] = true

	tests := []struct {
		name        string
		inv         ebsInventory
		volumes     []ec2types.Volume
		deleteErr   map[string]error
		wantDeleted []string
		wantErr     string
	}{
		{
			name: "deletes recorded Delete-policy volumes only",
			inv:  inv,
			volumes: []ec2types.Volume{
				vol("vol-db", ec2types.VolumeStateAvailable, cluster),
				vol("vol-keep", ec2types.VolumeStateAvailable, cluster),        // Retain
				vol("vol-static", ec2types.VolumeStateAvailable, cluster),      // unbound PV
				untaggedVol("vol-static-2", ec2types.VolumeStateAvailable),     // unbound PV, any volume
				vol("vol-old-cluster", ec2types.VolumeStateAvailable, cluster), // never seen
				vol("vol-gone", ec2types.VolumeStateDeleting, cluster),         // driver got it
				vol("vol-other", ec2types.VolumeStateAvailable, "other"),       // another cluster
				// vol-vanished was already deleted by the driver: absent.
			},
			wantDeleted: []string{"vol-db"},
		},
		{
			name: "recorded volume without the cluster tag is still deleted",
			inv:  inv,
			volumes: []ec2types.Volume{
				untaggedVol("vol-redis", ec2types.VolumeStateAvailable), // older driver
				untaggedVol("vol-stranger", ec2types.VolumeStateAvailable),
			},
			wantDeleted: []string{"vol-redis"},
		},
		{
			name:        "incomplete inventory deletes nothing and errors on tagged volumes",
			inv:         incomplete,
			volumes:     []ec2types.Volume{vol("vol-db", ec2types.VolumeStateAvailable, cluster)},
			wantDeleted: nil,
			wantErr:     "vol-db",
		},
		{
			name:        "incomplete inventory with nothing tagged is clean",
			inv:         incomplete,
			volumes:     []ec2types.Volume{vol("vol-other", ec2types.VolumeStateAvailable, "other")},
			wantDeleted: nil,
		},
		{
			name:        "volume still attached is an error, not a delete",
			inv:         inv,
			volumes:     []ec2types.Volume{vol("vol-db", ec2types.VolumeStateInUse, cluster)},
			wantDeleted: nil,
			wantErr:     "vol-db is in-use",
		},
		{
			name: "delete failure is returned and other volumes still go",
			inv:  inv,
			volumes: []ec2types.Volume{
				vol("vol-db", ec2types.VolumeStateAvailable, cluster),
				vol("vol-redis", ec2types.VolumeStateAvailable, cluster),
			},
			deleteErr:   map[string]error{"vol-db": errors.New("boom")},
			wantDeleted: []string{"vol-redis"},
			wantErr:     "delete volume vol-db",
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
			n, err := deleteOrphanedEBSVolumes(context.Background(), client, cluster, tt.inv)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("deleteOrphanedEBSVolumes() error = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("deleteOrphanedEBSVolumes() error = %v, want one containing %q", err, tt.wantErr)
			}
			sort.Strings(client.deleted)
			if strings.Join(client.deleted, ",") != strings.Join(tt.wantDeleted, ",") {
				t.Errorf("deleted = %v, want %v", client.deleted, tt.wantDeleted)
			}
			if n != len(tt.wantDeleted) {
				t.Errorf("returned count = %d, want %d", n, len(tt.wantDeleted))
			}
			// The cluster-tag listing always runs, for the report.
			if last := client.filters[len(client.filters)-1]; last != "tag:"+ebsClusterTag {
				t.Errorf("last DescribeVolumes filter = %q, want tag:%s", last, ebsClusterTag)
			}
		})
	}
}
