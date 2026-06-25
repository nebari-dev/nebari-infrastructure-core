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
