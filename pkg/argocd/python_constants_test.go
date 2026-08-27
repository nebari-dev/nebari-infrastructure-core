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
		"JOURNEY_LABEL_KEY":     true,
		"JOURNEY_LABEL_VALUE":   true,
		"ARGOCD_NAMESPACE":      true,
		"REALM_NAME":            true,
		"LONGHORN_ADMINS_GROUP": true,
	}

	enrolled := map[string]bool{}
	for _, name := range []string{
		"KEYCLOAK_NAMESPACE", "NEBARI_SYSTEM_NAMESPACE", "LONGHORN_NAMESPACE",
		"KEYCLOAK_ADMIN_SECRET", "KEYCLOAK_ADMIN_PASSWORD_KEY",
		"REALM_ADMIN_SECRET", "REALM_ADMIN_PASSWORD_KEY",
		"LONGHORN_OIDC_CLIENT_SECRET", "PART_OF_LABEL", "FOUNDATIONAL_PART_OF",
		"GATEWAY_NAMESPACE", "GATEWAY_LABEL_SELECTOR", "GATEWAY_TLS_SECRET",
		"GATEWAY_CERTIFICATE_NAME",
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

// extractYAMLName extracts the metadata.name literal from a manifest template.
// The templates are Go text/template files with a static metadata.name, so a
// simple regex is sufficient; there is no templated value in that field.
func extractYAMLName(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	re := regexp.MustCompile(`(?m)^\s*name:\s*(\S+)\s*$`)
	m := re.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("no metadata.name found in %s; the template may have changed", path)
	}
	return m[1]
}

// TestPythonGatewayCertificateNameMatchesTemplate fails when the gateway
// Certificate's hardcoded name drifts between the manifest template that
// deploys it and the Python constant the journey suite reads it by. The
// suite has no other way to find the Certificate: it is looked up by this
// exact name, so a silent rename here breaks domain() on every real cluster.
func TestPythonGatewayCertificateNameMatchesTemplate(t *testing.T) {
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
