package longhorn

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	helmtime "helm.sh/helm/v3/pkg/time"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

// newTestActionConfig builds a helm action.Configuration backed by an
// in-memory release store and a no-op kube client. Used to exercise
// Uninstall logic without a real cluster.
func newTestActionConfig(t *testing.T) *action.Configuration {
	t.Helper()
	return &action.Configuration{
		Releases:     storage.Init(driver.NewMemory()),
		KubeClient:   &kubefake.PrintingKubeClient{Out: io.Discard},
		Capabilities: chartutil.DefaultCapabilities,
		Log:          func(format string, v ...any) { t.Logf(format, v...) },
	}
}

// newDeletingConfirmationFlagSetting builds the deleting-confirmation-flag
// Setting CR as Longhorn creates it, with the given value.
func newDeletingConfirmationFlagSetting(value string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "longhorn.io", Version: "v1beta2", Kind: "Setting"})
	u.SetNamespace(Namespace)
	u.SetName(deletingConfirmationFlagName)
	_ = unstructured.SetNestedField(u.Object, value, "value")
	return u
}

func newFakeDynamicClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		settingsResource.GroupVersion().WithKind("SettingList"),
		&unstructured.UnstructuredList{},
	)
	listKinds := map[schema.GroupVersionResource]string{settingsResource: "SettingList"}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, objs...)
}

// seedRelease stores a deployed Longhorn release in the action config's
// in-memory release store.
func seedRelease(t *testing.T, cfg *action.Configuration) {
	t.Helper()
	rel := &release.Release{
		Name:      ReleaseName,
		Namespace: Namespace,
		Version:   1,
		Info: &release.Info{
			FirstDeployed: helmtime.Time{Time: time.Now()},
			LastDeployed:  helmtime.Time{Time: time.Now()},
			Status:        release.StatusDeployed,
		},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{Name: "longhorn", Version: "1.0.0"},
		},
	}
	if err := cfg.Releases.Create(rel); err != nil {
		t.Fatalf("seed release: %v", err)
	}
}

func TestUninstallReleaseNoOpWhenAbsent(t *testing.T) {
	cfg := newTestActionConfig(t)
	dyn := newFakeDynamicClient(newDeletingConfirmationFlagSetting("false"))

	if err := uninstallRelease(context.Background(), cfg, dyn); err != nil {
		t.Fatalf("uninstallRelease() with no release present should be a no-op, got error: %v", err)
	}

	// Without a release there is no uninstall to confirm; the guard against
	// accidental deletion must stay in place.
	setting, err := dyn.Resource(settingsResource).Namespace(Namespace).
		Get(context.Background(), deletingConfirmationFlagName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get setting: %v", err)
	}
	if value, _, _ := unstructured.NestedString(setting.Object, "value"); value != "false" {
		t.Errorf("deleting-confirmation-flag = %q after no-op uninstall, want %q untouched", value, "false")
	}
}

func TestUninstallReleaseRemovesExistingRelease(t *testing.T) {
	cfg := newTestActionConfig(t)
	seedRelease(t, cfg)
	dyn := newFakeDynamicClient(newDeletingConfirmationFlagSetting("false"))

	if err := uninstallRelease(context.Background(), cfg, dyn); err != nil {
		t.Fatalf("uninstallRelease() = %v, want nil", err)
	}

	hist := action.NewHistory(cfg)
	hist.Max = 1
	if _, err := hist.Run(ReleaseName); err == nil {
		t.Error("expected release to be gone after uninstallRelease(), still found in history")
	}

	// The pre-delete hook only runs when the flag is "true" (#398).
	setting, err := dyn.Resource(settingsResource).Namespace(Namespace).
		Get(context.Background(), deletingConfirmationFlagName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get setting: %v", err)
	}
	if value, _, _ := unstructured.NestedString(setting.Object, "value"); value != "true" {
		t.Errorf("deleting-confirmation-flag = %q after uninstall, want %q", value, "true")
	}
}

func TestUninstallReleaseProceedsWhenFlagSettingMissing(t *testing.T) {
	cfg := newTestActionConfig(t)
	seedRelease(t, cfg)
	dyn := newFakeDynamicClient()

	if err := uninstallRelease(context.Background(), cfg, dyn); err != nil {
		t.Fatalf("uninstallRelease() with missing flag setting = %v, want nil", err)
	}

	hist := action.NewHistory(cfg)
	hist.Max = 1
	if _, err := hist.Run(ReleaseName); err == nil {
		t.Error("expected release to be gone after uninstallRelease(), still found in history")
	}
}

func TestUninstallReleaseFailsWhenFlagPatchFails(t *testing.T) {
	cfg := newTestActionConfig(t)
	seedRelease(t, cfg)
	dyn := newFakeDynamicClient(newDeletingConfirmationFlagSetting("false"))
	dyn.PrependReactor("patch", "settings", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("admission webhook denied the request")
	})

	err := uninstallRelease(context.Background(), cfg, dyn)
	if err == nil {
		t.Fatal("uninstallRelease() = nil, want error when flag patch fails")
	}

	// The release must survive: failing fast here is the whole point, so the
	// user sees the patch error instead of a hook-job BackoffLimitExceeded.
	hist := action.NewHistory(cfg)
	hist.Max = 1
	if _, err := hist.Run(ReleaseName); err != nil {
		t.Errorf("expected release to remain after failed flag patch, history lookup failed: %v", err)
	}
}
