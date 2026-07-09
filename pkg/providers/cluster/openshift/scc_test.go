package openshift

import "testing"

func TestSCCBindingManifests(t *testing.T) {
	bindings := sccBindingManifests([]string{"keycloak", "cert-manager"}, "privileged")
	if len(bindings) != 2 {
		t.Fatalf("got %d bindings, want 2", len(bindings))
	}

	b := bindings[0]
	if b.Name != "nic-openshift-scc-privileged-keycloak" {
		t.Errorf("Name = %q, want nic-openshift-scc-privileged-keycloak", b.Name)
	}
	if b.RoleRef.Kind != "ClusterRole" || b.RoleRef.Name != "system:openshift:scc:privileged" {
		t.Errorf("RoleRef = %+v, want ClusterRole system:openshift:scc:privileged", b.RoleRef)
	}
	if len(b.Subjects) != 1 {
		t.Fatalf("got %d subjects, want 1", len(b.Subjects))
	}
	if b.Subjects[0].Kind != "Group" || b.Subjects[0].Name != "system:serviceaccounts:keycloak" {
		t.Errorf("Subject = %+v, want Group system:serviceaccounts:keycloak", b.Subjects[0])
	}
}

func TestSCCBindingNameDeterministic(t *testing.T) {
	if got := sccBindingName("anyuid", "nebari"); got != "nic-openshift-scc-anyuid-nebari" {
		t.Errorf("sccBindingName = %q", got)
	}
}

func TestSCCClusterRoleName(t *testing.T) {
	if got := sccClusterRoleName("nonroot-v2"); got != "system:openshift:scc:nonroot-v2" {
		t.Errorf("sccClusterRoleName = %q", got)
	}
}

func TestSCCNamespacesMergesExtras(t *testing.T) {
	c := &Config{}
	c.SCC.ExtraNamespaces = []string{"mlflow", "argocd"} // argocd is already foundational (dup)
	ns := c.sccNamespaces()

	// foundational set is included
	if !containsStr(ns, "keycloak") || !containsStr(ns, "argocd") {
		t.Errorf("expected foundational namespaces in %v", ns)
	}
	// extra is appended
	if !containsStr(ns, "mlflow") {
		t.Errorf("expected mlflow in %v", ns)
	}
	// no duplicates (argocd appears once)
	count := 0
	for _, n := range ns {
		if n == "argocd" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("argocd appears %d times, want 1 (deduped)", count)
	}
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
