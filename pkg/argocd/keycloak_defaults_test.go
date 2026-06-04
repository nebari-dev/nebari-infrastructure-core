package argocd

import (
	"strings"
	"testing"
)

// TestRenderKeycloakDefaults asserts the load-bearing fields of the
// rendered kcc input that NIC writes to the keycloak-config-import
// Secret. The rendered content is what makes the realm-setup Job
// produce a valid Keycloak state; regressions here are silent (the YAML
// renders fine but Keycloak gets misconfigured), so we check structure.
func TestRenderKeycloakDefaults(t *testing.T) {
	rendered, err := RenderKeycloakDefaults(TemplateData{Domain: "test.example.com"})
	if err != nil {
		t.Fatalf("RenderKeycloakDefaults error: %v", err)
	}
	output := string(rendered)

	// kcc realm payload — load-bearing fields the realm-setup-job depends on
	for _, want := range []string{
		"realm: nebari",
		"defaultDefaultClientScopes:",
		// kcc applies list fields as full-replace, so the built-in default
		// scopes must be present here or tokens lose email/profile/roles.
		"- basic",
		"- profile",
		"- email",
		"- roles",
		"- web-origins",
		"- acr",
		"- groups",
		"- name: argocd-admins",
		"- name: argocd-viewers",
		"username: admin",
		"$(env:REALM_ADMIN_PASSWORD)",
		"- /argocd-admins",
		"protocolMapper: oidc-group-membership-mapper",
		"claim.name: groups",
		"clientId: argocd",
		"$(env:ARGOCD_CLIENT_SECRET)",
		"https://argocd.test.example.com/auth/callback",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in rendered realm config, got:\n%s", want, output)
		}
	}

	// The argocd client must explicitly list the full set of default
	// scopes. Keycloak's realm default-defaults auto-apply only at client
	// creation, not on subsequent updates — so we need this redundancy to
	// keep kcc's replace semantics from stripping scopes on existing
	// clients during a re-run.
	if !strings.Contains(output, "    defaultClientScopes:\n      - basic") {
		t.Errorf("argocd client must explicitly list defaultClientScopes starting with built-ins, got:\n%s", output)
	}

	// The rendered content must not contain any unresolved Go-template
	// markers. If a future edit introduces a new {{ . }} reference and
	// forgets to add the field, this catches it instead of letting it
	// land in Keycloak as a literal.
	if strings.Contains(output, "{{") || strings.Contains(output, "}}") {
		t.Errorf("rendered output contains unresolved template markers, got:\n%s", output)
	}
}
