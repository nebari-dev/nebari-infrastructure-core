package argocd

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"text/template"

	"go.opentelemetry.io/otel"
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
//
// missingkey=error makes a stray template field (e.g. a future {{ .Typo }})
// fail loudly here rather than silently emitting "<no value>" into the realm
// config, where it would surface only as a misconfigured Keycloak at runtime.
func RenderKeycloakDefaults(ctx context.Context, data TemplateData) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.RenderKeycloakDefaults")
	defer span.End()

	tmpl, err := template.New("keycloak_defaults.yaml").Option("missingkey=error").Parse(string(keycloakDefaultsRaw))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("parse keycloak defaults template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("render keycloak defaults: %w", err)
	}
	return buf.Bytes(), nil
}
