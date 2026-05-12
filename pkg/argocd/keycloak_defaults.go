package argocd

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

// keycloakDefaultsRaw is the default keycloak-config-cli input for the nebari
// realm. It is embedded as a Go template and rendered with TemplateData at
// deploy time before being written to the in-cluster Secret.
//
//go:embed keycloak_defaults.yaml
var keycloakDefaultsRaw []byte

const (
	// KeycloakImportSecretName is the in-cluster Secret carrying the kcc
	// input that the realm-setup Job applies. The content lives only inside
	// the cluster's Secret store; the gitops repo never holds realm
	// structure or inline credentials.
	KeycloakImportSecretName = "keycloak-config-import" //nolint:gosec // secret name reference

	// KeycloakImportSecretKey is the key inside KeycloakImportSecretName
	// under which the rendered kcc input lives.
	KeycloakImportSecretKey = "realm.yaml"
)

// RenderKeycloakDefaults renders the embedded default kcc input with the
// supplied TemplateData. Only the Domain field is substituted today (used
// to build the argocd client's redirect URI); future fields can be wired
// in as the schema grows.
func RenderKeycloakDefaults(data TemplateData) ([]byte, error) {
	tmpl, err := template.New("keycloak_defaults.yaml").Parse(string(keycloakDefaultsRaw))
	if err != nil {
		return nil, fmt.Errorf("parse keycloak defaults template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render keycloak defaults: %w", err)
	}
	return buf.Bytes(), nil
}
