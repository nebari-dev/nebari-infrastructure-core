package fingerprint

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
)

// TestNamespaceMatchesArgoCDNebariSystem pins this package's namespace to the
// one pkg/argocd owns. The constant is duplicated rather than imported because
// this package writes before ArgoCD exists and must not depend on it - so the
// coupling is enforced here, in a test, where an import would be a design
// mistake. If the two ever diverge, the record lands somewhere the foundational
// AppProject does not declare.
func TestNamespaceMatchesArgoCDNebariSystem(t *testing.T) {
	if Namespace != argocd.NebariSystemNamespace {
		t.Errorf("Namespace = %q, want argocd.NebariSystemNamespace (%q)", Namespace, argocd.NebariSystemNamespace)
	}
}

// TestApplyCreatesNamespace covers the early-write requirement: the record is
// stamped before the foundational install, so nebari-system does not exist yet
// and Apply has to stand it up.
func TestApplyCreatesNamespace(t *testing.T) {
	client := fake.NewSimpleClientset()
	ctx := context.Background()

	if err := Apply(ctx, client, testInfo()); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if _, err := client.CoreV1().Namespaces().Get(ctx, Namespace, metav1.GetOptions{}); err != nil {
		t.Errorf("namespace %s was not created: %v", Namespace, err)
	}
}

// TestApplyToleratesExistingNamespace covers the ordinary redeploy path, where
// the foundational install has already created the namespace.
func TestApplyToleratesExistingNamespace(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: Namespace},
	})

	if err := Apply(ctx, client, testInfo()); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if _, err := client.CoreV1().ConfigMaps(Namespace).Get(ctx, Name, metav1.GetOptions{}); err != nil {
		t.Errorf("ConfigMap not written into the existing namespace: %v", err)
	}
}

// TestApplyToleratesNamespaceCreateRace covers losing the create race with the
// foundational install: AlreadyExists means the namespace is there, which is all
// this needs, so it must not fail the write.
func TestApplyToleratesNamespaceCreateRace(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "namespaces",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewAlreadyExists(
				schema.GroupResource{Resource: "namespaces"}, Namespace)
		})

	if err := Apply(context.Background(), client, testInfo()); err != nil {
		t.Errorf("Apply() error = %v, want the AlreadyExists race tolerated", err)
	}
}

// TestApplySurfacesNamespaceGetFailure is the same reasoning as the ConfigMap
// Get: a Forbidden must not be mistaken for "absent" and fall through to a
// Create that fails with a worse message.
func TestApplySurfacesNamespaceGetFailure(t *testing.T) {
	client := fake.NewSimpleClientset()
	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Resource: "namespaces"}, Namespace, errors.New("nope"))
	client.PrependReactor("get", "namespaces",
		func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, forbidden })

	err := Apply(context.Background(), client, testInfo())
	if !errors.Is(err, forbidden) {
		t.Errorf("Apply() error = %v, want it to wrap the Forbidden namespace Get", err)
	}
}

func testInfo() Info {
	return Info{
		Build: Build{
			Version: "v0.13.0",
			Commit:  "e6d4ae9",
			Date:    "2026-06-04T13:30:00Z",
		},
		ClusterProvider: "aws",
		ProjectName:     "my-nebari-aws",
		LastDeploy:      time.Date(2026, 6, 10, 18, 43, 22, 0, time.UTC),
	}
}

// TestInfoData pins the ConfigMap keys and their sources. These keys are read by
// runbooks and support scripts outside this repo, so a rename here is a breaking
// change and must fail a test rather than pass silently.
func TestInfoData(t *testing.T) {
	want := map[string]string{
		"nic-version":           "v0.13.0",
		"nic-commit":            "e6d4ae9",
		"nic-build-date":        "2026-06-04T13:30:00Z",
		"cluster-provider":      "aws",
		"project-name":          "my-nebari-aws",
		"last-deploy-timestamp": "2026-06-10T18:43:22Z",
	}
	got := testInfo().Data()
	if len(got) != len(want) {
		t.Errorf("Data() has %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Data()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// TestInfoDataNormalizesTimestampToUTC guards the timestamp against the reader's
// local zone: an operator comparing two clusters' records must not have to
// account for the offset of whichever machine ran each deploy.
func TestInfoDataNormalizesTimestampToUTC(t *testing.T) {
	info := testInfo()
	info.LastDeploy = time.Date(2026, 6, 10, 20, 43, 22, 0, time.FixedZone("CEST", 2*60*60))

	if got := info.Data()["last-deploy-timestamp"]; got != "2026-06-10T18:43:22Z" {
		t.Errorf("last-deploy-timestamp = %q, want the UTC form %q", got, "2026-06-10T18:43:22Z")
	}
}

// TestInfoDataWritesEmptyValues pins that a missing field is recorded as an
// empty value rather than an absent key. A reader can then distinguish "NIC did
// not know this" from "this key postdates the NIC that deployed the cluster".
func TestInfoDataWritesEmptyValues(t *testing.T) {
	data := Info{}.Data()
	for _, key := range []string{"nic-version", "nic-commit", "nic-build-date", "cluster-provider", "project-name"} {
		if _, ok := data[key]; !ok {
			t.Errorf("Data() is missing key %q for a zero Info", key)
		}
	}
}

func TestApplyCreatesConfigMap(t *testing.T) {
	client := fake.NewSimpleClientset()

	if err := Apply(context.Background(), client, testInfo()); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	cm, err := client.CoreV1().ConfigMaps(Namespace).Get(context.Background(), Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(%s/%s) error = %v", Namespace, Name, err)
	}
	if got := cm.Data["nic-version"]; got != "v0.13.0" {
		t.Errorf("nic-version = %q, want %q", got, "v0.13.0")
	}
	if got := cm.Labels[managedByLabel]; got != managedByValue {
		t.Errorf("label %s = %q, want %q", managedByLabel, got, managedByValue)
	}
}

// TestApplyIsIdempotent covers the DoD requirement that a redeploy patches the
// single ConfigMap rather than duplicating it, and that the new build's values
// win.
func TestApplyIsIdempotent(t *testing.T) {
	client := fake.NewSimpleClientset()
	ctx := context.Background()

	first := testInfo()
	if err := Apply(ctx, client, first); err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}

	second := testInfo()
	second.Build.Version = "v0.14.0"
	second.Build.Commit = "abc1234"
	second.LastDeploy = first.LastDeploy.Add(24 * time.Hour)
	if err := Apply(ctx, client, second); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}

	list, err := client.CoreV1().ConfigMaps(Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("got %d ConfigMaps, want exactly 1 (redeploy must patch, not duplicate)", len(list.Items))
	}

	cm := list.Items[0]
	if got := cm.Data["nic-version"]; got != "v0.14.0" {
		t.Errorf("nic-version = %q, want the redeploying build %q", got, "v0.14.0")
	}
	if got := cm.Data["nic-commit"]; got != "abc1234" {
		t.Errorf("nic-commit = %q, want %q", got, "abc1234")
	}
	if got := cm.Data["last-deploy-timestamp"]; got != "2026-06-11T18:43:22Z" {
		t.Errorf("last-deploy-timestamp = %q, want the second deploy's time", got)
	}
}

// TestApplyPreservesForeignLabels pins that reconciling our label does not strip
// labels someone else put on the ConfigMap (a cost-allocation or policy label,
// say). NIC owns its own marker, not the whole object's metadata.
func TestApplyPreservesForeignLabels(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Name,
			Namespace: Namespace,
			Labels:    map[string]string{"team": "platform"},
		},
		Data: map[string]string{"nic-version": "v0.12.0"},
	})

	if err := Apply(ctx, client, testInfo()); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	cm, err := client.CoreV1().ConfigMaps(Namespace).Get(ctx, Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := cm.Labels["team"]; got != "platform" {
		t.Errorf("foreign label team = %q, want it preserved as %q", got, "platform")
	}
	if got := cm.Labels[managedByLabel]; got != managedByValue {
		t.Errorf("label %s = %q, want %q", managedByLabel, got, managedByValue)
	}
}

// TestApplyDoesNotCreateOnNonNotFoundGetError is the important negative case: a
// Get that fails for any reason other than absence must surface, not be treated
// as "the ConfigMap does not exist yet". Otherwise an RBAC denial on Get would
// be masked by a Create that fails for the same reason with a worse message.
func TestApplyDoesNotCreateOnNonNotFoundGetError(t *testing.T) {
	client := fake.NewSimpleClientset()
	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Resource: "configmaps"}, Name, errors.New("nope"))

	client.PrependReactor("get", "configmaps",
		func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, forbidden })
	var created bool
	client.PrependReactor("create", "configmaps",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			created = true
			return false, nil, nil
		})

	err := Apply(context.Background(), client, testInfo())
	if err == nil {
		t.Fatal("Apply() error = nil, want the Get failure surfaced")
	}
	if !errors.Is(err, forbidden) {
		t.Errorf("Apply() error = %v, want it to wrap the Forbidden error", err)
	}
	if created {
		t.Error("Apply() attempted a Create after a non-NotFound Get, masking the real failure")
	}
}

// TestApplyReturnsCreateAndUpdateErrors keeps both write paths from silently
// swallowing a failure - callers warn on the error, so losing it would mean the
// operator is told nothing while the record is missing.
func TestApplyReturnsCreateAndUpdateErrors(t *testing.T) {
	tests := []struct {
		name     string
		verb     string
		existing []runtime.Object
	}{
		{name: "create fails", verb: "create"},
		{
			name: "update fails",
			verb: "update",
			existing: []runtime.Object{&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: Name, Namespace: Namespace},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existing...)
			boom := errors.New("boom")
			client.PrependReactor(tt.verb, "configmaps",
				func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, boom })

			err := Apply(context.Background(), client, testInfo())
			if !errors.Is(err, boom) {
				t.Errorf("Apply() error = %v, want it to wrap %v", err, boom)
			}
		})
	}
}
