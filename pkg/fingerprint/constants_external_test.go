// This file is package fingerprint_test, not package fingerprint, deliberately.
//
// pkg/fingerprint duplicates two constants that pkg/argocd also defines, because
// pkg/fingerprint writes before Argo CD exists and importing a high-level
// package from a leaf one inverts the layering. An external test package can
// import both sides to pin them together without ever putting pkg/argocd in
// pkg/fingerprint's own import graph - which an in-package test would do, and
// which would become a real cycle the moment pkg/argocd wanted anything here.
package fingerprint_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/fingerprint"
)

// TestNamespaceMatchesArgoCD pins the duplicated namespace. If the two diverge,
// the record lands somewhere the foundational AppProject does not declare.
func TestNamespaceMatchesArgoCD(t *testing.T) {
	if fingerprint.Namespace != argocd.NebariSystemNamespace {
		t.Errorf("fingerprint.Namespace = %q, want argocd.NebariSystemNamespace (%q)",
			fingerprint.Namespace, argocd.NebariSystemNamespace)
	}
}

// TestManagedByLabelMatchesArgoCD pins the duplicated managed-by marker, so the
// ConfigMap keeps showing up in the same label selector as every other
// NIC-created object.
func TestManagedByLabelMatchesArgoCD(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	if err := fingerprint.Apply(ctx, client, fingerprint.Info{}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	cm, err := client.CoreV1().ConfigMaps(fingerprint.Namespace).Get(ctx, fingerprint.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := cm.Labels[argocd.ManagedByLabel]; got != argocd.NebariManagedByValue {
		t.Errorf("label %s = %q, want %q", argocd.ManagedByLabel, got, argocd.NebariManagedByValue)
	}
}
