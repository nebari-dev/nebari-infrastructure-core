package openshift

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// foundationalNamespaces are the namespaces whose service accounts run Nebari's
// foundational workloads. On OpenShift the default "restricted-v2" SCC assigns
// pods an arbitrary high UID and forbids the fixed UIDs several upstream charts
// (Keycloak, ArgoCD, Envoy Gateway, the nebari-operator) bake into their images,
// so those pods fail to start until granted a more permissive SCC. This list was
// confirmed against a live ROSA HCP cluster (Phase A A5 smoke test).
var foundationalNamespaces = []string{
	"argocd",
	"cert-manager",
	"envoy-gateway-system",
	"keycloak",
	"nebari-system",
	"nebari-operator-system",
	"monitoring",
	"nebari",
}

// sccNamespaces returns the full set of namespaces to grant the SCC to: the
// built-in foundational namespaces plus any extras from config (pack
// namespaces). Deduplicated, preserving order.
func (c *Config) sccNamespaces() []string {
	seen := make(map[string]bool, len(foundationalNamespaces))
	out := make([]string, 0, len(foundationalNamespaces)+len(c.SCC.ExtraNamespaces))
	for _, ns := range append(append([]string{}, foundationalNamespaces...), c.SCC.ExtraNamespaces...) {
		if ns == "" || seen[ns] {
			continue
		}
		seen[ns] = true
		out = append(out, ns)
	}
	return out
}

// defaultSCCName is the SecurityContextConstraints granted to foundational
// service accounts.
//
// It is "privileged" rather than the lighter "anyuid" because Nebari's upstream
// foundational charts pin BOTH a fixed UID (runAsUser: 999) AND a seccomp
// profile (RuntimeDefault). No stock OpenShift SCC permits that combination
// except privileged: anyuid allows the fixed UID but forbids any seccomp
// profile, while restricted-v2 allows the seccomp profile but forbids the fixed
// UID. This was proven on a live ROSA HCP cluster, where argocd-redis (UID 999 +
// seccomp) could not schedule under anyuid. Operators wanting least privilege can
// ship a custom SCC (RunAsAny + seccomp RuntimeDefault) and set scc.name to it.
const defaultSCCName = "privileged"

// sccClusterRoleName returns the auto-generated ClusterRole that backs an SCC.
// OpenShift exposes each SCC as a ClusterRole named system:openshift:scc:<name>;
// binding it to a subject grants use of that SCC.
func sccClusterRoleName(scc string) string {
	return "system:openshift:scc:" + scc
}

// sccBindingName is the deterministic ClusterRoleBinding name for an
// (scc, namespace) pair, so repeated applies are idempotent.
func sccBindingName(scc, namespace string) string {
	return fmt.Sprintf("nic-openshift-scc-%s-%s", scc, namespace)
}

// sccBindingManifests builds one ClusterRoleBinding per namespace that grants the
// given SCC to every service account in that namespace (the
// system:serviceaccounts:<ns> virtual group). ClusterRoleBindings are used
// rather than namespaced RoleBindings so they can be created before ArgoCD
// creates the namespaces. Pure function — unit tested.
func sccBindingManifests(namespaces []string, scc string) []rbacv1.ClusterRoleBinding {
	out := make([]rbacv1.ClusterRoleBinding, 0, len(namespaces))
	for _, ns := range namespaces {
		out = append(out, rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: sccBindingName(scc, ns),
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "nebari-infrastructure-core",
					"app.kubernetes.io/part-of":    "nebari-foundational",
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     sccClusterRoleName(scc),
			},
			Subjects: []rbacv1.Subject{{
				Kind:     rbacv1.GroupKind,
				APIGroup: rbacv1.GroupName,
				Name:     "system:serviceaccounts:" + ns,
			}},
		})
	}
	return out
}

// applySCCBindings creates (or updates) the SCC ClusterRoleBindings for the
// foundational namespaces. Idempotent: existing bindings are updated in place.
func applySCCBindings(ctx context.Context, kubeconfigBytes []byte, namespaces []string, scc string) error {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to build REST config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to build kubernetes client: %w", err)
	}

	crbClient := clientset.RbacV1().ClusterRoleBindings()
	bindings := sccBindingManifests(namespaces, scc)
	for i := range bindings {
		crb := bindings[i]
		_, err := crbClient.Create(ctx, &crb, metav1.CreateOptions{})
		switch {
		case err == nil:
			status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Granted SCC %q to namespace %q", scc, namespaces[i])).
				WithResource("scc").WithAction("granting"))
		case apierrors.IsAlreadyExists(err):
			if _, uerr := crbClient.Update(ctx, &crb, metav1.UpdateOptions{}); uerr != nil {
				return fmt.Errorf("failed to update SCC binding %s: %w", crb.Name, uerr)
			}
		default:
			return fmt.Errorf("failed to create SCC binding %s: %w", crb.Name, err)
		}
	}
	return nil
}
