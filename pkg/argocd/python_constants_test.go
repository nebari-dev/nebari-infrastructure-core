package argocd

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
)

// pythonConstantsPath is the mirrored constants module the journey suite uses.
const pythonConstantsPath = "../../tests/user_journeys/nebari_journeys/constants.py"

// parsePythonConstants extracts NAME = "value" assignments, ignoring comments
// and any trailing "# noqa: ..." pragma.
func parsePythonConstants(t *testing.T, path string) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	re := regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]*)\s*=\s*"([^"]*)"`)
	out := make(map[string]string)
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = m[2]
	}
	if len(out) == 0 {
		t.Fatalf("no constants parsed from %s; the format may have changed", path)
	}
	return out
}

func TestPythonConstantsMatchGo(t *testing.T) {
	python := parsePythonConstants(t, pythonConstantsPath)

	tests := []struct {
		pythonName string
		goValue    string
	}{
		{"KEYCLOAK_NAMESPACE", KeycloakDefaultNamespace},
		{"NEBARI_SYSTEM_NAMESPACE", NebariSystemNamespace},
		{"LONGHORN_NAMESPACE", LonghornDefaultNamespace},
		{"KEYCLOAK_ADMIN_SECRET", KeycloakDefaultAdminSecretName},
		{"KEYCLOAK_ADMIN_PASSWORD_KEY", KeycloakAdminPasswordKey},
		{"REALM_ADMIN_SECRET", NebariRealmAdminSecretName},
		{"REALM_ADMIN_PASSWORD_KEY", NebariRealmAdminPasswordKey},
		{"LONGHORN_OIDC_CLIENT_SECRET", LonghornOIDCClientSecretName},
		{"PART_OF_LABEL", PartOfLabel},
		{"FOUNDATIONAL_PART_OF", NebariFoundationalPartOf},
		{"GATEWAY_NAMESPACE", endpoint.DefaultNamespace},
		{"GATEWAY_LABEL_SELECTOR", endpoint.DefaultLabelSelector},
		{"GATEWAY_TLS_SECRET", config.DefaultGatewayTLSSecretName},
		{"ARGOCD_NAMESPACE", DefaultNamespace},
	}

	for _, tt := range tests {
		t.Run(tt.pythonName, func(t *testing.T) {
			got, ok := python[tt.pythonName]
			if !ok {
				t.Fatalf("%s missing from %s", tt.pythonName, pythonConstantsPath)
			}
			if got != tt.goValue {
				t.Errorf("%s = %q in Python, %q in Go", tt.pythonName, got, tt.goValue)
			}
		})
	}
}

// TestPythonConstantsEnrollment fails when a Go-sourced constant is added to
// constants.py without a row in TestPythonConstantsMatchGo, which would let it
// drift unnoticed. Suite-owned constants are exempt by explicit listing.
func TestPythonConstantsEnrollment(t *testing.T) {
	suiteOwned := map[string]bool{
		"JOURNEY_LABEL_KEY":   true,
		"JOURNEY_LABEL_VALUE": true,
	}

	enrolled := map[string]bool{}
	for _, name := range []string{
		"KEYCLOAK_NAMESPACE", "NEBARI_SYSTEM_NAMESPACE", "LONGHORN_NAMESPACE",
		"KEYCLOAK_ADMIN_SECRET", "KEYCLOAK_ADMIN_PASSWORD_KEY",
		"REALM_ADMIN_SECRET", "REALM_ADMIN_PASSWORD_KEY",
		"LONGHORN_OIDC_CLIENT_SECRET", "PART_OF_LABEL", "FOUNDATIONAL_PART_OF",
		"GATEWAY_NAMESPACE", "GATEWAY_LABEL_SELECTOR", "GATEWAY_TLS_SECRET",
		"ARGOCD_NAMESPACE",
		"GATEWAY_CERTIFICATE_NAME", "GATEWAY_NAME", "ROOT_APP_NAME",
		"LONGHORN_BACKUP_APP",
		"REALM_NAME", "ARGOCD_ADMINS_GROUP", "ARGOCD_VIEWERS_GROUP",
		"LONGHORN_ADMINS_GROUP", "ARGOCD_OIDC_CLIENT", "LONGHORN_OIDC_CLIENT",
	} {
		enrolled[name] = true
	}

	for name := range parsePythonConstants(t, pythonConstantsPath) {
		if !enrolled[name] && !suiteOwned[name] {
			t.Errorf("%s is in constants.py but not enrolled in "+
				"TestPythonConstantsMatchGo or the suiteOwned list. "+
				"Add a row so it cannot drift, or mark it suite-owned. "+
				"See %s", name, pythonConstantsPath)
		}
	}
}

// yamlNamePattern matches the first `name: <literal>` line of a manifest. The
// templates are Go text/template files whose metadata.name is the first such
// line and is a static literal, so a simple regex is sufficient; there is no
// templated value in that field.
var yamlNamePattern = regexp.MustCompile(`(?m)^\s*name:\s*(\S+)\s*$`)

// extractYAMLNameFrom extracts the metadata.name literal from manifest source.
// source is described by origin, which is only used in failure messages.
func extractYAMLNameFrom(t *testing.T, source, origin string) string {
	t.Helper()

	m := yamlNamePattern.FindStringSubmatch(source)
	if m == nil {
		t.Fatalf("no metadata.name found in %s; it may have changed", origin)
	}
	return m[1]
}

// extractYAMLName extracts the metadata.name literal from a manifest template
// on disk.
func extractYAMLName(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return extractYAMLNameFrom(t, string(raw), path)
}

// TestPythonResourceNamesMatchTemplates fails when a resource name hardcoded in
// a manifest template drifts from the Python constant the journey suite looks
// it up by. The suite has no other way to find these objects: each is fetched
// by this exact name, so a silent rename breaks a journey on every real
// cluster, and the failure looks like broken infrastructure rather than a
// renamed resource.
func TestPythonResourceNamesMatchTemplates(t *testing.T) {
	tests := []struct {
		name         string
		templatePath string
		pythonName   string
	}{
		{
			name:         "gateway certificate name",
			templatePath: "templates/manifests/security/certificates/gateway-certificate.yaml",
			pythonName:   "GATEWAY_CERTIFICATE_NAME",
		},
		{
			name:         "gateway name",
			templatePath: "templates/manifests/networking/gateway.yaml",
			pythonName:   "GATEWAY_NAME",
		},
		{
			name:         "longhorn backup application name",
			templatePath: "templates/apps/longhorn-backup.yaml",
			pythonName:   "LONGHORN_BACKUP_APP",
		},
	}

	python := parsePythonConstants(t, pythonConstantsPath)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templateName := extractYAMLName(t, tt.templatePath)

			pythonValue, ok := python[tt.pythonName]
			if !ok {
				t.Fatalf("%s missing from %s", tt.pythonName, pythonConstantsPath)
			}

			if templateName != pythonValue {
				t.Errorf("%s = %q in %s, %q in %s",
					tt.pythonName, templateName, tt.templatePath,
					pythonValue, pythonConstantsPath)
			}
		})
	}
}

// TestPythonRootAppNameMatchesTemplate pins ROOT_APP_NAME against the
// app-of-apps Application that bootstrap.go actually creates. The suite
// excludes this name from the foundational set, so a rename here would make
// foundational_applications() start asserting on the app-of-apps, whose sync
// status is not a foundational-software fault.
func TestPythonRootAppNameMatchesTemplate(t *testing.T) {
	python := parsePythonConstants(t, pythonConstantsPath)

	templateName := extractYAMLNameFrom(t, rootAppOfAppsTemplate,
		"rootAppOfAppsTemplate in bootstrap.go")

	pythonValue, ok := python["ROOT_APP_NAME"]
	if !ok {
		t.Fatalf("ROOT_APP_NAME missing from %s", pythonConstantsPath)
	}
	if templateName != pythonValue {
		t.Errorf("ROOT_APP_NAME = %q in rootAppOfAppsTemplate, %q in %s",
			templateName, pythonValue, pythonConstantsPath)
	}
}

// realmSetupJobPath is the manifest template whose shell script creates the
// realm, its groups and its OIDC clients. Those names are shell literals, not
// Go constants, so they are pinned against the template text directly.
const realmSetupJobPath = "templates/manifests/keycloak/realm-setup-job.yaml"

// TestPythonRealmLiteralsMatchTemplate pins the realm name, the group names and
// the OIDC client ids the journey suite asserts on against the kcadm
// invocations that create them. Each row extracts the set of literals the
// template passes to a particular kcadm flag and requires the Python constant
// to be one of them, so a rename on either side is caught. Containment rather
// than equality is deliberate: adding a new group or client to the platform is
// a legitimate change that must not fail this test, while renaming one the
// suite depends on must.
func TestPythonRealmLiteralsMatchTemplate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(realmSetupJobPath))
	if err != nil {
		t.Fatalf("read %s: %v", realmSetupJobPath, err)
	}
	template := string(raw)

	python := parsePythonConstants(t, pythonConstantsPath)

	realmPattern := regexp.MustCompile(`-s realm=(\S+)`)
	groupPattern := regexp.MustCompile(`create groups -r \S+ -s name=(\S+)`)
	clientPattern := regexp.MustCompile(`-s clientId=(\S+)`)

	tests := []struct {
		pythonName string
		pattern    *regexp.Regexp
		what       string
	}{
		{"REALM_NAME", realmPattern, "realm created by kcadm"},
		{"ARGOCD_ADMINS_GROUP", groupPattern, "group created by kcadm"},
		{"ARGOCD_VIEWERS_GROUP", groupPattern, "group created by kcadm"},
		{"LONGHORN_ADMINS_GROUP", groupPattern, "group created by kcadm"},
		{"ARGOCD_OIDC_CLIENT", clientPattern, "OIDC client created by kcadm"},
		{"LONGHORN_OIDC_CLIENT", clientPattern, "OIDC client created by kcadm"},
	}

	for _, tt := range tests {
		t.Run(tt.pythonName, func(t *testing.T) {
			matches := tt.pattern.FindAllStringSubmatch(template, -1)
			if len(matches) == 0 {
				t.Fatalf("no %s found in %s; the template may have changed",
					tt.what, realmSetupJobPath)
			}

			found := make([]string, 0, len(matches))
			for _, m := range matches {
				found = append(found, m[1])
			}

			pythonValue, ok := python[tt.pythonName]
			if !ok {
				t.Fatalf("%s missing from %s", tt.pythonName, pythonConstantsPath)
			}

			for _, candidate := range found {
				if candidate == pythonValue {
					return
				}
			}
			t.Errorf("%s = %q in %s, but the %ss in %s are %q",
				tt.pythonName, pythonValue, pythonConstantsPath,
				tt.what, realmSetupJobPath, found)
		})
	}
}
