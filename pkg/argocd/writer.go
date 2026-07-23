// Package argocd generates ArgoCD Application manifests for Nebari's
// foundational software stack.
package argocd

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

//go:embed templates
var templates embed.FS

const (
	templateDir = "templates"
	// certificateIssuerSelfSigned is the cert-manager Issuer used when the user has
	// not configured a real ACME provider.
	certificateIssuerSelfSigned = "selfsigned-issuer"
)

// TemplateData holds the dynamic values for template processing
type TemplateData struct {
	// Git repository configuration
	GitRepoURL string
	GitBranch  string
	GitPath    string // Path within the repository (e.g., "clusters/test1")

	// Domain configuration
	Domain string

	StorageClass string // Provider-appropriate storage class for persistent volumes

	// Certificate configuration
	CertificateIssuer string // "selfsigned-issuer" or "letsencrypt-issuer"
	ACMEEmail         string
	ACMEServer        string

	// UseExistingCertificate is true when the user supplies their own TLS cert
	// (certificate.type=existing). When true, the cert-manager Certificate is
	// not rendered.
	UseExistingCertificate bool
	// GatewayTLSSecretName is the name of the TLS secret the gateway listener references.
	GatewayTLSSecretName string
	// GatewayTLSSecretNamespace is the namespace of that secret. Only emitted on the
	// gateway certificateRef (and used for the ReferenceGrant) when GatewayTLSCrossNamespace.
	GatewayTLSSecretNamespace string
	// GatewayTLSCrossNamespace is true when the TLS secret lives outside
	// envoy-gateway-system, requiring a namespace on certificateRefs and a ReferenceGrant.
	GatewayTLSCrossNamespace bool

	// MetalLB configuration (for local provider)
	MetalLBAddressRange string

	// TrustManagerEnabled gates the trust-manager app and Bundle manifest. True
	// when a top-level trust_bundle is configured.
	TrustManagerEnabled bool

	// TrustBundlePEM is the raw PEM of the org CA, rendered inline into the
	// trust-manager Bundle. Empty unless TrustManagerEnabled is true.
	TrustBundlePEM string

	// HTTPSPort is the port used for HTTPS redirects (default: 443).
	HTTPSPort int

	// LoadBalancerAnnotations are added to the Gateway's provisioned LoadBalancer Service.
	LoadBalancerAnnotations map[string]string

	// KeycloakBasePath is appended to the Keycloak in-cluster URL (e.g., "/auth").
	KeycloakBasePath string

	// Keycloak configuration
	KeycloakNamespace            string // Namespace where Keycloak is deployed (e.g., "keycloak")
	KeycloakServiceName          string // Kubernetes service name for Keycloak (e.g., "keycloak-keycloakx-http")
	KeycloakServiceURL           string // In-cluster base URL for the Keycloak service (e.g., "http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080")
	KeycloakIssuerURL            string // External public URL for validating the iss claim in tokens (e.g., "https://keycloak.nebari.example.com" or with base path "https://keycloak.nebari.example.com/auth")
	KeycloakRealm                string // Keycloak realm name (e.g., "nebari")
	KeycloakAdminSecretName      string // Name of the Kubernetes secret containing Keycloak admin credentials
	KeycloakAdminSecretNamespace string // Namespace of the Kubernetes secret containing Keycloak admin credentials

	// LonghornEnabled mirrors InfraSettings.LonghornEnabled. When false, no Longhorn
	// HTTPRoute, SecurityPolicy, cert dnsName entry, or realm-setup snippet is
	// rendered. Keycloak is mandatory infrastructure in this codebase (always
	// provisioned during foundational install), so a separate Keycloak-enabled
	// gate is not part of the conditional.
	LonghornEnabled bool

	// LonghornOIDCSecretName is the name of the Kubernetes secret holding the
	// Longhorn UI OIDC client secret, threaded from LonghornOIDCClientSecretName
	// so the Go constant and the rendered manifests cannot drift.
	LonghornOIDCSecretName string
}

// NewTemplateData creates TemplateData from NebariConfig, the effective git
// configuration, and provider InfraSettings. gitConfig may be nil when no
// GitOps repository is configured; in that case Git* fields are left empty.
func NewTemplateData(cfg *config.NebariConfig, gitConfig *git.Config, settings cluster.InfraSettings) TemplateData {
	keycloakServiceName := "keycloak-keycloakx-http"

	httpsPort := settings.HTTPSPort
	if httpsPort == 0 {
		httpsPort = 443
	}

	data := TemplateData{
		Domain:                  cfg.Domain,
		StorageClass:            settings.StorageClass,
		HTTPSPort:               httpsPort,
		MetalLBAddressRange:     settings.MetalLBAddressPool,
		LoadBalancerAnnotations: settings.LoadBalancerAnnotations,
		KeycloakBasePath:        settings.KeycloakBasePath,
		LonghornEnabled:         settings.LonghornEnabled,
		LonghornOIDCSecretName:  LonghornOIDCClientSecretName,

		KeycloakNamespace:            KeycloakDefaultNamespace,
		KeycloakServiceName:          keycloakServiceName,
		KeycloakServiceURL:           fmt.Sprintf("http://%s.%s.svc.cluster.local:8080%s", keycloakServiceName, KeycloakDefaultNamespace, settings.KeycloakBasePath),
		KeycloakIssuerURL:            "", // set after Domain is resolved below
		KeycloakRealm:                "nebari",
		KeycloakAdminSecretName:      KeycloakDefaultAdminSecretName,
		KeycloakAdminSecretNamespace: KeycloakDefaultNamespace,
	}

	// Set git repository info
	if gitConfig != nil {
		data.GitRepoURL = gitConfig.URL
		data.GitBranch = gitConfig.GetBranch()
		data.GitPath = gitConfig.Path
	}

	// Set certificate configuration
	if cfg.Certificate != nil && cfg.Certificate.Type == config.CertificateTypeLetsEncrypt {
		data.CertificateIssuer = "letsencrypt-issuer"
		if cfg.Certificate.ACME != nil {
			data.ACMEEmail = cfg.Certificate.ACME.Email
			data.ACMEServer = cfg.Certificate.ACME.Server
			if data.ACMEServer == "" {
				data.ACMEServer = "https://acme-v02.api.letsencrypt.org/directory"
			}
		}
	} else {
		data.CertificateIssuer = certificateIssuerSelfSigned
	}

	// Resolve the gateway TLS secret reference. The methods are nil-safe, so
	// they return sensible defaults when no certificate block is configured.
	data.UseExistingCertificate = cfg.Certificate != nil && cfg.Certificate.Type == config.CertificateTypeExisting
	data.GatewayTLSSecretName, data.GatewayTLSSecretNamespace = cfg.Certificate.GatewaySecretRef()
	data.GatewayTLSCrossNamespace = cfg.Certificate.IsCrossNamespaceSecret()

	// Default domain if not set
	if data.Domain == "" {
		data.Domain = "nebari.local"
	}

	// External Keycloak URL - what Keycloak embeds in the iss claim of tokens.
	// Clients inside the cluster fetch JWKs via KeycloakServiceURL (in-cluster)
	// and validate the iss claim against this public URL.
	// Only set when a real domain is configured. When no domain is provided
	// (e.g. cloud deployments using a bare LoadBalancer IP), KeycloakIssuerURL
	// is left empty and KEYCLOAK_ISSUER_URL is not injected into workloads.
	//
	// NOTE: The nebari-landingpage template uses KeycloakIssuerURL to construct
	// oidcIssuerUrl for oauth2-proxy (rendered as "<KeycloakIssuerURL>/realms/<realm>").
	// If KeycloakIssuerURL is empty this collapses to a relative path like
	// "/realms/nebari", which oauth2-proxy would reject. In practice this
	// function defaults Domain to "nebari.local" (see above), so
	// KeycloakIssuerURL is always populated through normal code paths. However,
	// if bare-LB-IP deployments (cfg.Domain == "") are ever supported, the
	// template will need a guard or a separate value for the OIDC issuer URL.
	if cfg.Domain != "" {
		data.KeycloakIssuerURL = fmt.Sprintf("https://keycloak.%s%s", data.Domain, settings.KeycloakBasePath)
	}

	return data
}

// Applications returns the list of available application names from the apps/ directory.
// Names are derived from filenames (without .yaml extension).
func Applications() ([]string, error) {
	entries, err := fs.ReadDir(templates, filepath.Join(templateDir, "apps"))
	if err != nil {
		return nil, fmt.Errorf("failed to read apps directory: %w", err)
	}

	var apps []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		// Skip files starting with underscore (used for examples/documentation)
		if strings.HasPrefix(e.Name(), "_") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		apps = append(apps, name)
	}
	return apps, nil
}

// WriteApplication writes the ArgoCD Application manifest for appName to out.
// Returns an error if the application template is not found.
func WriteApplication(ctx context.Context, out io.Writer, appName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.WriteApplication")
	defer span.End()

	span.SetAttributes(attribute.String("application", appName))

	filename := filepath.Join(templateDir, "apps", appName+".yaml")
	content, err := templates.ReadFile(filename)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("template not found for application %q: %w", appName, err)
	}

	_, err = out.Write(content)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write application %q: %w", appName, err)
	}

	return nil
}

// WriteAll writes all application manifests by calling fn for each application.
// The fn callback receives the application name and should return an io.WriteCloser
// for that application's manifest. WriteAll will close the writer after writing.
//
// Example:
//
//	err := argocd.WriteAll(ctx, func(appName string) (io.WriteCloser, error) {
//	    return os.Create(filepath.Join(dir, appName+".yaml"))
//	})
func WriteAll(ctx context.Context, fn func(appName string) (io.WriteCloser, error)) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.WriteAll")
	defer span.End()

	apps, err := Applications()
	if err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(attribute.Int("application_count", len(apps)))

	for _, appName := range apps {
		w, err := fn(appName)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create writer for %q: %w", appName, err)
		}

		err = WriteApplication(ctx, w, appName)
		closeErr := w.Close()

		if err != nil {
			return err
		}
		if closeErr != nil {
			span.RecordError(closeErr)
			return fmt.Errorf("failed to close writer for %q: %w", appName, closeErr)
		}
	}

	return nil
}

// WriteAllToGit writes all templates (apps and manifests) to the git repository.
// Templates are processed with Go template syntax for dynamic values.
// trustBundlePEM is the top-level CA bundle already resolved by the orchestration
// layer (empty when no bundle is configured); it is not re-read from disk here.
func WriteAllToGit(ctx context.Context, gitClient git.Client, cfg *config.NebariConfig, gitConfig *git.Config, settings cluster.InfraSettings, trustBundlePEM string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.WriteAllToGit")
	defer span.End()

	workDir := gitClient.WorkDir()
	data := NewTemplateData(cfg, gitConfig, settings)

	if trustBundlePEM != "" {
		data.TrustManagerEnabled = true
		data.TrustBundlePEM = trustBundlePEM
	}

	span.SetAttributes(
		attribute.String("work_dir", workDir),
		attribute.String("domain", data.Domain),
		attribute.String("git_repo_url", data.GitRepoURL),
	)

	// Walk all files in the templates directory
	err := fs.WalkDir(templates, templateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root templates directory
		if path == templateDir {
			return nil
		}

		// Get the relative path from templates/
		relPath := strings.TrimPrefix(path, templateDir+"/")

		// Skip hidden files and underscore-prefixed files
		if strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		destPath := filepath.Join(workDir, relPath)

		// Gated templates are removed (not just skipped) when their gate is
		// off, so toggling a feature off on an already-bootstrapped repo
		// deletes its manifests and lets ArgoCD prune the resources, instead
		// of leaving them orphaned in git.

		// MetalLB templates only apply to providers that need it
		if isMetalLBPath(relPath) && !settings.NeedsMetalLB {
			return removeStaleTemplate(destPath, d)
		}

		// Longhorn-only templates are gated on LonghornEnabled. The
		// securitypolicies Application targets manifests/networking/policies,
		// whose only content is the Longhorn SecurityPolicy; writing the app
		// without its manifest would create an Application with zero resources
		// (rejected by allowEmpty: false).
		if isLonghornOnlyPath(relPath) && !settings.LonghornEnabled {
			return removeStaleTemplate(destPath, d)
		}

		// trust-manager templates only apply when a trust bundle is configured
		if isTrustBundlePath(relPath) && !data.TrustManagerEnabled {
			return removeStaleTemplate(destPath, d)
		}

		// Crossplane is provider/profile-conditional foundational software
		// (ADR-0012 §3). When no capability is authorized the entire install is
		// absent, not merely the per-capability layers: the core chart, the
		// providers Application, and every provider/config manifest are removed.
		// Only once at least one capability is enabled are the foundational
		// manifests written and the per-capability gate below applied to the
		// un-opted-in layers.
		if isCrossplanePath(relPath) && !settings.CrossplaneEnabled() {
			return removeStaleTemplate(destPath, d)
		}

		// Crossplane capability templates are an explicit provider capability.
		// Other providers and clusters without the opt-in must not receive the
		// provider package or its cloud-credentials config.
		if id := crossplaneCapabilityForPath(relPath); id != "" && !settings.CrossplaneCapabilities[id] {
			return removeStaleTemplate(destPath, d)
		}

		// Certificate templates that don't apply to the configured cert source.
		if !d.IsDir() && skipCertificateTemplate(relPath, data) {
			return removeStaleTemplate(destPath, d)
		}

		if d.IsDir() {
			return os.MkdirAll(destPath, git.GitOpsDirMode)
		}

		// Read template content
		content, err := templates.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", path, err)
		}

		// Process template
		processed, err := processTemplate(relPath, content, data)
		if err != nil {
			return fmt.Errorf("failed to process template %s: %w", path, err)
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(destPath), git.GitOpsDirMode); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", destPath, err)
		}

		// Write processed content
		if err := os.WriteFile(destPath, processed, git.GitOpsFileMode); err != nil {
			return fmt.Errorf("failed to write %s: %w", destPath, err)
		}

		return nil
	})

	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write templates to git: %w", err)
	}

	return nil
}

// removeStaleTemplate deletes the previously written output of a template
// whose gate is now off, so a feature toggled from enabled to disabled has its
// files removed from the gitops repo rather than skipped-but-retained. Missing
// files are a no-op (the common case: the feature was never enabled). Returns
// fs.SkipDir for directories so the walk does not descend into them.
func removeStaleTemplate(destPath string, d fs.DirEntry) error {
	if d.IsDir() {
		if err := os.RemoveAll(destPath); err != nil {
			return fmt.Errorf("failed to remove stale directory %s: %w", destPath, err)
		}
		return fs.SkipDir
	}
	if err := os.Remove(destPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove stale file %s: %w", destPath, err)
	}
	return nil
}

// isMetalLBPath returns true if the relative path is a MetalLB-related template.
func isMetalLBPath(relPath string) bool {
	return relPath == "apps/metallb.yaml" ||
		relPath == "apps/metallb-config.yaml" ||
		strings.HasPrefix(relPath, "manifests/metallb")
}

// isLonghornOnlyPath returns true if the relative path is a template that only
// produces Longhorn resources and must be skipped entirely when Longhorn is
// disabled.
func isLonghornOnlyPath(relPath string) bool {
	return relPath == "apps/securitypolicies.yaml" ||
		strings.HasPrefix(relPath, "manifests/networking/policies")
}

// isTrustBundlePath returns true if the relative path is a trust-manager-related
// template (the chart Application, the Bundle Application, or the Bundle manifest).
func isTrustBundlePath(relPath string) bool {
	return relPath == "apps/trust-manager.yaml" ||
		relPath == "apps/trust-bundle.yaml" ||
		strings.HasPrefix(relPath, "manifests/security/trust-bundle")
}

// isCrossplanePath returns true if the relative path is any Crossplane
// template — the core chart Application, the providers Application, or any
// manifest under the crossplane tree (foundational or per-capability). The
// whole tree is gated on Crossplane being enabled at all (ADR-0012 §3); the
// per-capability gate then trims individual layers when it is.
func isCrossplanePath(relPath string) bool {
	return strings.HasPrefix(relPath, "apps/crossplane") ||
		strings.HasPrefix(relPath, "manifests/crossplane")
}

// crossplaneCapabilities enumerates the gateable Crossplane provider
// capabilities. Each id maps by convention to one per-capability template:
//   - manifests/crossplane/providers/provider-<id>.yaml (the provider package)
//
// ADR-0012's dedicated-account model shares one account-local role and one
// ProviderConfig across all providers, so the cloud-credentials config
// (manifests/crossplane/configs/aws + apps/crossplane-aws-config.yaml) is
// foundational, not per-capability: it is written whenever Crossplane is
// enabled at all (see isCrossplanePath and CrossplaneEnabled). Only the provider
// packages themselves are gated per-capability, so a cluster installs just the
// controllers it opted into. Adding a capability is one entry here plus the
// matching provider manifest and IAM — the gating logic does not change.
var crossplaneCapabilities = []string{"aws-s3", "aws-iam", "aws-eks", "aws-rds"}

// crossplaneCapabilityForPath returns the capability id that owns relPath, or ""
// if the path does not belong to a gateable Crossplane capability.
func crossplaneCapabilityForPath(relPath string) string {
	for _, id := range crossplaneCapabilities {
		if relPath == "manifests/crossplane/providers/provider-"+id+".yaml" {
			return id
		}
	}
	return ""
}

const (
	gatewayCertificatePath    = "manifests/security/certificates/gateway-certificate.yaml"
	certificatesAppPath       = "apps/certificates.yaml"
	gatewayReferenceGrantPath = "manifests/networking/gateway-tls-referencegrant.yaml"
)

// skipCertificateTemplate reports whether a cert-related template should be
// omitted for the configured certificate source. The cert-manager Certificate
// (and its Argo CD Application) is only rendered for cert-manager-issued certs
// (selfsigned/letsencrypt); the ReferenceGrant is only rendered for a
// cross-namespace existing secret.
//
// The certificates Application is skipped alongside the Certificate because the
// gateway cert is the only resource in manifests/security/certificates. Leaving
// the Application would point it at an empty directory (allowEmpty: false) and
// it would report as failed.
func skipCertificateTemplate(relPath string, data TemplateData) bool {
	switch relPath {
	case gatewayCertificatePath, certificatesAppPath:
		return data.UseExistingCertificate
	case gatewayReferenceGrantPath:
		return !data.GatewayTLSCrossNamespace
	default:
		return false
	}
}

// templateFuncs is the single extension point for helpers available to every
// template; it is consumed only by processTemplate, not a broader public surface.
// indent and nindent mirror the common Helm helpers for embedding multi-line
// values (e.g. a PEM bundle) at a fixed YAML indentation.
var templateFuncs = template.FuncMap{
	"indent":  indentLines,
	"nindent": func(spaces int, s string) string { return "\n" + indentLines(spaces, s) },
}

// indentLines prefixes every non-empty line of s with the given number of spaces.
func indentLines(spaces int, s string) string {
	pad := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}

// processTemplate processes a template file with the given data.
// Only YAML files are processed as templates; other files are returned as-is.
func processTemplate(name string, content []byte, data TemplateData) ([]byte, error) {
	// Only process YAML files as templates
	if !strings.HasSuffix(name, ".yaml") {
		return content, nil
	}

	tmpl, err := template.New(name).Funcs(templateFuncs).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.Bytes(), nil
}
