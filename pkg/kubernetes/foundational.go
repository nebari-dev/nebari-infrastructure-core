package kubernetes

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

//go:embed foundational/keycloak-application.yaml
var keycloakApplicationManifest string

//go:embed foundational/keycloak-namespace.yaml
var keycloakNamespaceManifest string

//go:embed foundational/postgresql-application.yaml
var postgresqlApplicationManifest string

//go:embed foundational/cert-manager-application.yaml
var certManagerApplicationManifest string

//go:embed foundational/envoy-gateway-application.yaml
var envoyGatewayApplicationManifest string

//go:embed foundational/opentelemetry-collector-application.yaml
var opentelemetryCollectorApplicationManifest string

//go:embed foundational/gatewayclass.yaml
var gatewayClassManifest string

//go:embed foundational/selfsigned-clusterissuer.yaml
var selfsignedClusterIssuerManifest string

//go:embed foundational/letsencrypt-clusterissuer.yaml
var letsencryptClusterIssuerManifest string

//go:embed foundational/gateway-certificate.yaml
var gatewayCertificateManifest string

//go:embed foundational/gateway.yaml
var gatewayManifest string

//go:embed foundational/keycloak-httproute.yaml
var keycloakHTTPRouteManifest string

//go:embed foundational/argocd-httproute.yaml
var argocdHTTPRouteManifest string

//go:embed foundational/metallb-application.yaml
var metallbApplicationManifest string

//go:embed foundational/argocd-project.yaml
var argoCDProjectManifest string

//go:embed foundational/metallb-ipaddresspool.yaml
var metallbIPAddressPoolManifest string

//go:embed foundational/metallb-l2advertisement.yaml
var metallbL2AdvertisementManifest string

// FoundationalConfig holds configuration for foundational services
type FoundationalConfig struct {
	// Keycloak configuration
	Keycloak KeycloakConfig

	// MetalLB configuration (local deployments only)
	MetalLB MetalLBConfig
}

// KeycloakConfig holds Keycloak-specific configuration
type KeycloakConfig struct {
	Enabled       bool
	AdminPassword string
	DBPassword    string
	Hostname      string
}

// MetalLBConfig holds MetalLB-specific configuration
type MetalLBConfig struct {
	Enabled     bool
	AddressPool string // e.g., "192.168.1.100-192.168.1.110"
}

// InstallFoundationalServices installs foundational services (Keycloak, etc.) via Argo CD
func InstallFoundationalServices(ctx context.Context, cfg *config.NebariConfig, prov provider.Provider, foundationalCfg FoundationalConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "kubernetes.InstallFoundationalServices")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", prov.Name()),
		attribute.String("project_name", cfg.ProjectName),
		attribute.Bool("keycloak_enabled", foundationalCfg.Keycloak.Enabled),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Installing foundational services").
		WithResource("foundational").
		WithAction("installing"))

	// Get kubeconfig from provider
	kubeconfigBytes, err := prov.GetKubeconfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	// 0. Install ArgoCD Project for foundational services (Priority: 0)
	if err := installArgoCDProject(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install ArgoCD Project").
			WithResource("argocd-project").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		// Don't fail - project is optional, apps will fall back to default project
	}

	// Create Kubernetes client
	k8sClient, err := NewClientFromKubeconfig(ctx, kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// 1. Install Cert Manager (Priority: 1)
	if err := installCertManager(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install Cert Manager").
			WithResource("cert-manager").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to install Cert Manager: %w", err)
	}

	// 1.2. Install ClusterIssuer (Priority: 1.2, depends on cert-manager)
	if err := installClusterIssuer(ctx, kubeconfigBytes, cfg); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install ClusterIssuer").
			WithResource("clusterissuer").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		// Don't fail - ClusterIssuer is optional for non-TLS setups
	}

	// 1.5. Install MetalLB if enabled (Priority: 1.5, local deployments only)
	if foundationalCfg.MetalLB.Enabled {
		if err := installMetalLB(ctx, kubeconfigBytes, foundationalCfg.MetalLB); err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install MetalLB").
				WithResource("metallb").
				WithAction("install-failed").
				WithMetadata("error", err.Error()))
			// Don't fail - MetalLB is optional for local deployments
		}
	}

	// 2. Install Envoy Gateway (Priority: 2, depends on cert-manager)
	if err := installEnvoyGateway(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install Envoy Gateway").
			WithResource("envoy-gateway").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to install Envoy Gateway: %w", err)
	}

	// 2.1. Install Gateway TLS certificate (Priority: 2.1, depends on envoy-gateway namespace)
	if err := installGatewayCertificate(ctx, kubeconfigBytes, cfg); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install Gateway certificate").
			WithResource("gateway-certificate").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		// Don't fail - Certificate is optional for non-TLS setups
	}

	// 2a. Install GatewayClass (Priority: 2.3, depends on envoy-gateway)
	if err := installGatewayClass(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install GatewayClass").
			WithResource("gatewayclass").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		// Don't fail - GatewayClass is optional
	}

	// 2b. Install Gateway resource (Priority: 2.5, depends on gatewayclass)
	if err := installGateway(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install Gateway").
			WithResource("gateway").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		// Don't fail - Gateway is optional
	}

	// 2c. Install ArgoCD HTTPRoute (Priority: 2.6, depends on gateway)
	if err := installArgoCDHTTPRoute(ctx, kubeconfigBytes, cfg); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install ArgoCD HTTPRoute").
			WithResource("argocd-httproute").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		// Don't fail - HTTPRoute is optional
	}

	// 3. Install OpenTelemetry Collector (Priority: 3)
	if err := installOpenTelemetryCollector(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install OpenTelemetry Collector").
			WithResource("opentelemetry-collector").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		return fmt.Errorf("failed to install OpenTelemetry Collector: %w", err)
	}

	// 4. Install Keycloak if enabled (Priority: 4, depends on envoy-gateway)
	if foundationalCfg.Keycloak.Enabled {
		if err := installKeycloak(ctx, k8sClient, kubeconfigBytes, cfg, foundationalCfg.Keycloak); err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install Keycloak").
				WithResource("keycloak").
				WithAction("install-failed").
				WithMetadata("error", err.Error()))
			return fmt.Errorf("failed to install Keycloak: %w", err)
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Foundational services installed").
		WithResource("foundational").
		WithAction("installed"))

	return nil
}

// installKeycloak installs Keycloak via Argo CD
func installKeycloak(ctx context.Context, client *kubernetes.Clientset, kubeconfigBytes []byte, cfg *config.NebariConfig, keycloakCfg KeycloakConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installKeycloak")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Keycloak and PostgreSQL").
		WithResource("keycloak").
		WithAction("installing"))

	// 1. Create namespace
	if err := createKeycloakNamespace(ctx, client); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Keycloak namespace: %w", err)
	}

	// 2. Create secrets
	if err := createKeycloakSecrets(ctx, client, keycloakCfg); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Keycloak secrets: %w", err)
	}

	// 3. Install PostgreSQL first
	if err := installPostgreSQL(ctx, kubeconfigBytes, keycloakCfg); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install PostgreSQL: %w", err)
	}

	// 4. Apply Keycloak Argo CD Application manifest
	appManifest, err := parseKeycloakApplicationManifest(keycloakCfg, cfg.Domain)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to parse Keycloak application manifest: %w", err)
	}

	if err := ApplyArgoCDApplication(ctx, kubeconfigBytes, appManifest); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply Keycloak Argo CD Application: %w", err)
	}

	// 5. Wait for Argo CD Application to be ready (with timeout)
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for Keycloak to be deployed by Argo CD").
		WithResource("keycloak").
		WithAction("waiting"))

	// Use a shorter timeout for non-blocking behavior
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if err := WaitForArgoCDApplication(waitCtx, kubeconfigBytes, "keycloak", "argocd", 2*time.Minute); err != nil {
		// Don't fail, just warn - Argo CD will continue syncing in the background
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Keycloak may not be fully deployed yet").
			WithResource("keycloak").
			WithAction("waiting").
			WithMetadata("info", "Argo CD will continue syncing in the background"))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Keycloak deployed successfully").
			WithResource("keycloak").
			WithAction("deployed"))
	}

	// 6. Install HTTPRoute for Keycloak
	if err := installKeycloakHTTPRoute(ctx, kubeconfigBytes, cfg); err != nil {
		// Don't fail if HTTPRoute fails - just warn
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install Keycloak HTTPRoute").
			WithResource("keycloak-httproute").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
	}

	return nil
}

// installPostgreSQL installs PostgreSQL via Argo CD
func installPostgreSQL(ctx context.Context, kubeconfigBytes []byte, keycloakCfg KeycloakConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installPostgreSQL")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing PostgreSQL database").
		WithResource("postgresql").
		WithAction("installing"))

	// Parse PostgreSQL Application manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(postgresqlApplicationManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode PostgreSQL application manifest: %w", err)
	}

	// Apply PostgreSQL Argo CD Application
	if err := ApplyArgoCDApplication(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply PostgreSQL Argo CD Application: %w", err)
	}

	// Wait for PostgreSQL to be ready
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for PostgreSQL to be ready").
		WithResource("postgresql").
		WithAction("waiting"))

	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := WaitForArgoCDApplication(waitCtx, kubeconfigBytes, "postgresql", "argocd", 3*time.Minute); err != nil {
		// Don't fail, just warn
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "PostgreSQL may not be fully ready yet").
			WithResource("postgresql").
			WithAction("waiting").
			WithMetadata("info", "Argo CD will continue syncing in the background"))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "PostgreSQL deployed successfully").
			WithResource("postgresql").
			WithAction("deployed"))
	}

	return nil
}

// createKeycloakNamespace creates the Keycloak namespace
func createKeycloakNamespace(ctx context.Context, client *kubernetes.Clientset) error {
	// Parse namespace manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(keycloakNamespaceManifest), nil, obj)
	if err != nil {
		return fmt.Errorf("failed to decode namespace manifest: %w", err)
	}

	// Convert to corev1.Namespace
	ns := &corev1.Namespace{}
	ns.Name = "keycloak"
	ns.Labels = map[string]string{
		"name":                         "keycloak",
		"app.kubernetes.io/name":       "keycloak",
		"app.kubernetes.io/component":  "authentication",
		"app.kubernetes.io/managed-by": "nebari-infrastructure-core",
	}

	// Create namespace (idempotent)
	return CreateNamespace(ctx, client, ns.Name)
}

// createKeycloakSecrets creates the required secrets for Keycloak
func createKeycloakSecrets(ctx context.Context, client *kubernetes.Clientset, keycloakCfg KeycloakConfig) error {
	namespace := "keycloak"

	// 1. Create admin credentials secret
	adminSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keycloak-admin-credentials",
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"admin-password": keycloakCfg.AdminPassword,
		},
	}

	_, err := client.CoreV1().Secrets(namespace).Get(ctx, adminSecret.Name, metav1.GetOptions{})
	if err != nil {
		// Secret doesn't exist, create it
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, adminSecret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create admin credentials secret: %w", err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Created Keycloak admin credentials secret").
			WithResource("secret").
			WithAction("created").
			WithMetadata("secret_name", adminSecret.Name))
	}

	// 2. Create Keycloak PostgreSQL user credentials secret
	keycloakDBSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keycloak-postgresql-credentials",
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"password": keycloakCfg.DBPassword, // Keycloak database user password
		},
	}

	_, err = client.CoreV1().Secrets(namespace).Get(ctx, keycloakDBSecret.Name, metav1.GetOptions{})
	if err != nil {
		// Secret doesn't exist, create it
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, keycloakDBSecret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create Keycloak PostgreSQL credentials secret: %w", err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Created Keycloak PostgreSQL credentials secret").
			WithResource("secret").
			WithAction("created").
			WithMetadata("secret_name", keycloakDBSecret.Name))
	}

	// 3. Create PostgreSQL main credentials secret (for PostgreSQL deployment)
	postgresSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "postgresql-credentials",
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"postgres-password": keycloakCfg.DBPassword + "-admin", // PostgreSQL admin password
			"user-password":     keycloakCfg.DBPassword + "-user",  // PostgreSQL default user password
		},
	}

	_, err = client.CoreV1().Secrets(namespace).Get(ctx, postgresSecret.Name, metav1.GetOptions{})
	if err != nil {
		// Secret doesn't exist, create it
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, postgresSecret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create PostgreSQL credentials secret: %w", err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Created PostgreSQL credentials secret").
			WithResource("secret").
			WithAction("created").
			WithMetadata("secret_name", postgresSecret.Name))
	}

	return nil
}

// parseKeycloakApplicationManifest parses and customizes the Keycloak Argo CD Application manifest
func parseKeycloakApplicationManifest(keycloakCfg KeycloakConfig, domain string) (*unstructured.Unstructured, error) {
	// Parse the embedded manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(keycloakApplicationManifest), nil, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to decode Keycloak application manifest: %w", err)
	}

	// Customize hostname if domain is provided
	if domain != "" && keycloakCfg.Hostname == "" {
		keycloakCfg.Hostname = fmt.Sprintf("keycloak.%s", domain)
	}

	if keycloakCfg.Hostname != "" {
		// Update the hostname in the Helm values
		helmValues, found, err := unstructured.NestedString(obj.Object, "spec", "source", "helm", "values")
		if err == nil && found {
			// Simple string replacement for hostname
			// In a more robust implementation, you'd parse the YAML, modify it, and re-serialize
			helmValues = strings.ReplaceAll(helmValues, "keycloak.nebari.local", keycloakCfg.Hostname)
			_ = unstructured.SetNestedField(obj.Object, helmValues, "spec", "source", "helm", "values")
		}
	}

	return obj, nil
}

// installCertManager installs Cert Manager via Argo CD
func installCertManager(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installCertManager")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Cert Manager").
		WithResource("cert-manager").
		WithAction("installing"))

	// Parse Cert Manager Application manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(certManagerApplicationManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode Cert Manager application manifest: %w", err)
	}

	// Apply Cert Manager Argo CD Application
	if err := ApplyArgoCDApplication(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply Cert Manager Argo CD Application: %w", err)
	}

	// Wait for Cert Manager to be ready
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for Cert Manager to be ready").
		WithResource("cert-manager").
		WithAction("waiting"))

	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := WaitForArgoCDApplication(waitCtx, kubeconfigBytes, "cert-manager", "argocd", 3*time.Minute); err != nil {
		// Don't fail, just warn
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Cert Manager may not be fully ready yet").
			WithResource("cert-manager").
			WithAction("waiting").
			WithMetadata("info", "Argo CD will continue syncing in the background"))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Cert Manager deployed successfully").
			WithResource("cert-manager").
			WithAction("deployed"))
	}

	return nil
}

// installMetalLB installs MetalLB via Argo CD for local deployments
func installMetalLB(ctx context.Context, kubeconfigBytes []byte, metallbCfg MetalLBConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installMetalLB")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing MetalLB").
		WithResource("metallb").
		WithAction("installing"))

	// 1. Parse and apply MetalLB Argo CD Application manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(metallbApplicationManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode MetalLB application manifest: %w", err)
	}

	if err := ApplyArgoCDApplication(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply MetalLB Argo CD Application: %w", err)
	}

	// 2. Wait for MetalLB to be deployed (with timeout)
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for MetalLB to be ready").
		WithResource("metallb").
		WithAction("waiting"))

	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if err := WaitForArgoCDApplication(waitCtx, kubeconfigBytes, "metallb", "argocd", 2*time.Minute); err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "MetalLB may not be fully ready yet").
			WithResource("metallb").
			WithAction("waiting").
			WithMetadata("info", "Argo CD will continue syncing in the background"))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "MetalLB deployed successfully").
			WithResource("metallb").
			WithAction("deployed"))
	}

	// 3. Apply IPAddressPool configuration
	if err := installMetalLBIPAddressPool(ctx, kubeconfigBytes, metallbCfg); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install MetalLB IPAddressPool: %w", err)
	}

	// 4. Apply L2Advertisement configuration
	if err := installMetalLBL2Advertisement(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to install MetalLB L2Advertisement: %w", err)
	}

	return nil
}

// installMetalLBIPAddressPool installs the IPAddressPool resource
func installMetalLBIPAddressPool(ctx context.Context, kubeconfigBytes []byte, metallbCfg MetalLBConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installMetalLBIPAddressPool")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating MetalLB IP address pool").
		WithResource("metallb-ipaddresspool").
		WithAction("installing"))

	// Parse IPAddressPool manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(metallbIPAddressPoolManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode IPAddressPool manifest: %w", err)
	}

	// Customize address pool if configured
	if metallbCfg.AddressPool != "" {
		addresses := []interface{}{metallbCfg.AddressPool}
		if err := unstructured.SetNestedSlice(obj.Object, addresses, "spec", "addresses"); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to set address pool: %w", err)
		}
	}

	// Apply IPAddressPool resource using dynamic client
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply IPAddressPool: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "MetalLB IP address pool created").
		WithResource("metallb-ipaddresspool").
		WithAction("created"))

	return nil
}

// installMetalLBL2Advertisement installs the L2Advertisement resource
func installMetalLBL2Advertisement(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installMetalLBL2Advertisement")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating MetalLB L2 advertisement").
		WithResource("metallb-l2advertisement").
		WithAction("installing"))

	// Parse L2Advertisement manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(metallbL2AdvertisementManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode L2Advertisement manifest: %w", err)
	}

	// Apply L2Advertisement resource using dynamic client
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply L2Advertisement: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "MetalLB L2 advertisement created").
		WithResource("metallb-l2advertisement").
		WithAction("created"))

	return nil
}

// installEnvoyGateway installs Envoy Gateway via Argo CD
func installEnvoyGateway(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installEnvoyGateway")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Envoy Gateway").
		WithResource("envoy-gateway").
		WithAction("installing"))

	// Parse Envoy Gateway Application manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(envoyGatewayApplicationManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode Envoy Gateway application manifest: %w", err)
	}

	// Apply Envoy Gateway Argo CD Application
	if err := ApplyArgoCDApplication(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply Envoy Gateway Argo CD Application: %w", err)
	}

	// Wait for Envoy Gateway to be ready
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for Envoy Gateway to be ready").
		WithResource("envoy-gateway").
		WithAction("waiting"))

	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := WaitForArgoCDApplication(waitCtx, kubeconfigBytes, "envoy-gateway", "argocd", 3*time.Minute); err != nil {
		// Don't fail, just warn
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Envoy Gateway may not be fully ready yet").
			WithResource("envoy-gateway").
			WithAction("waiting").
			WithMetadata("info", "Argo CD will continue syncing in the background"))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Envoy Gateway deployed successfully").
			WithResource("envoy-gateway").
			WithAction("deployed"))
	}

	return nil
}

// installOpenTelemetryCollector installs OpenTelemetry Collector via Argo CD
func installOpenTelemetryCollector(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installOpenTelemetryCollector")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing OpenTelemetry Collector").
		WithResource("opentelemetry-collector").
		WithAction("installing"))

	// Parse OpenTelemetry Collector Application manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(opentelemetryCollectorApplicationManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode OpenTelemetry Collector application manifest: %w", err)
	}

	// Apply OpenTelemetry Collector Argo CD Application
	if err := ApplyArgoCDApplication(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply OpenTelemetry Collector Argo CD Application: %w", err)
	}

	// Wait for OpenTelemetry Collector to be ready
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for OpenTelemetry Collector to be ready").
		WithResource("opentelemetry-collector").
		WithAction("waiting"))

	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := WaitForArgoCDApplication(waitCtx, kubeconfigBytes, "opentelemetry-collector", "argocd", 3*time.Minute); err != nil {
		// Don't fail, just warn
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "OpenTelemetry Collector may not be fully ready yet").
			WithResource("opentelemetry-collector").
			WithAction("waiting").
			WithMetadata("info", "Argo CD will continue syncing in the background"))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "OpenTelemetry Collector deployed successfully").
			WithResource("opentelemetry-collector").
			WithAction("deployed"))
	}

	return nil
}

// installClusterIssuer installs the appropriate ClusterIssuer based on config
func installClusterIssuer(ctx context.Context, kubeconfigBytes []byte, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installClusterIssuer")
	defer span.End()

	// Determine issuer type from config
	issuerType := "selfsigned"
	if cfg.Certificate != nil && cfg.Certificate.Type != "" {
		issuerType = cfg.Certificate.Type
	}

	var manifest string
	var issuerName string

	switch issuerType {
	case "letsencrypt":
		if cfg.Certificate == nil || cfg.Certificate.ACME == nil || cfg.Certificate.ACME.Email == "" {
			return fmt.Errorf("Let's Encrypt requires certificate.acme.email to be configured")
		}

		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Let's Encrypt ClusterIssuer").
			WithResource("letsencrypt-clusterissuer").
			WithAction("installing"))

		// Set default ACME server if not specified
		acmeServer := cfg.Certificate.ACME.Server
		if acmeServer == "" {
			acmeServer = "https://acme-v02.api.letsencrypt.org/directory"
		}

		// Replace placeholders in manifest
		manifest = strings.ReplaceAll(letsencryptClusterIssuerManifest, "ACME_EMAIL_PLACEHOLDER", cfg.Certificate.ACME.Email)
		manifest = strings.ReplaceAll(manifest, "ACME_SERVER_PLACEHOLDER", acmeServer)
		issuerName = "letsencrypt-clusterissuer"

	default: // selfsigned
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing self-signed ClusterIssuer").
			WithResource("selfsigned-clusterissuer").
			WithAction("installing"))
		manifest = selfsignedClusterIssuerManifest
		issuerName = "selfsigned-clusterissuer"
	}

	// Parse ClusterIssuer manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(manifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode ClusterIssuer manifest: %w", err)
	}

	// Apply ClusterIssuer resource
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply ClusterIssuer: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("%s ClusterIssuer created", issuerType)).
		WithResource(issuerName).
		WithAction("created"))

	return nil
}

// installGatewayCertificate installs the TLS certificate for the gateway
func installGatewayCertificate(ctx context.Context, kubeconfigBytes []byte, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installGatewayCertificate")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Gateway TLS certificate").
		WithResource("gateway-certificate").
		WithAction("installing"))

	// Parse Certificate manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(gatewayCertificateManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode Certificate manifest: %w", err)
	}

	// Determine issuer name based on config
	issuerName := "selfsigned-issuer"
	if cfg.Certificate != nil && cfg.Certificate.Type == "letsencrypt" {
		issuerName = "letsencrypt-issuer"
	}

	// Update the issuerRef in the certificate spec
	if err := unstructured.SetNestedField(obj.Object, issuerName, "spec", "issuerRef", "name"); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to set issuer reference: %w", err)
	}

	// Update DNS names if domain is configured
	if cfg.Domain != "" {
		dnsNames := []any{
			"*." + cfg.Domain,
			cfg.Domain,
			"keycloak." + cfg.Domain,
			"argocd." + cfg.Domain,
		}
		if err := unstructured.SetNestedSlice(obj.Object, dnsNames, "spec", "dnsNames"); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to set DNS names: %w", err)
		}
		// Also update commonName
		if err := unstructured.SetNestedField(obj.Object, "*."+cfg.Domain, "spec", "commonName"); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to set common name: %w", err)
		}
	}

	// Apply Certificate resource
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply Certificate: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Gateway TLS certificate created").
		WithResource("gateway-certificate").
		WithAction("created").
		WithMetadata("issuer", issuerName))

	return nil
}

// installGatewayClass installs the GatewayClass resource for Envoy Gateway
func installGatewayClass(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installGatewayClass")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing GatewayClass resource").
		WithResource("gatewayclass").
		WithAction("installing"))

	// Parse GatewayClass manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(gatewayClassManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode GatewayClass manifest: %w", err)
	}

	// Apply GatewayClass resource using dynamic client
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply GatewayClass: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "GatewayClass resource created").
		WithResource("gatewayclass").
		WithAction("created"))

	return nil
}

// installGateway installs the Gateway API Gateway resource
func installGateway(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installGateway")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Gateway resource").
		WithResource("gateway").
		WithAction("installing"))

	// Parse Gateway manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(gatewayManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode Gateway manifest: %w", err)
	}

	// Apply Gateway resource using dynamic client
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply Gateway: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Gateway resource created").
		WithResource("gateway").
		WithAction("created"))

	return nil
}

// installArgoCDHTTPRoute installs HTTPRoute for ArgoCD
func installArgoCDHTTPRoute(ctx context.Context, kubeconfigBytes []byte, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installArgoCDHTTPRoute")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing ArgoCD HTTPRoute").
		WithResource("argocd-httproute").
		WithAction("installing"))

	// Parse HTTPRoute manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(argocdHTTPRouteManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode ArgoCD HTTPRoute manifest: %w", err)
	}

	// Customize hostname if domain is provided
	if cfg.Domain != "" {
		hostname := fmt.Sprintf("argocd.%s", cfg.Domain)
		// Update the hostnames in the HTTPRoute spec
		hostnames := []interface{}{hostname}
		if err := unstructured.SetNestedSlice(obj.Object, hostnames, "spec", "hostnames"); err == nil {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Configured ArgoCD hostname: %s", hostname)).
				WithResource("argocd-httproute").
				WithAction("configuring"))
		}
	}

	// Apply HTTPRoute resource
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply ArgoCD HTTPRoute: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "ArgoCD HTTPRoute created").
		WithResource("argocd-httproute").
		WithAction("created"))

	return nil
}

// installKeycloakHTTPRoute installs HTTPRoute for Keycloak
func installKeycloakHTTPRoute(ctx context.Context, kubeconfigBytes []byte, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installKeycloakHTTPRoute")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing Keycloak HTTPRoute").
		WithResource("keycloak-httproute").
		WithAction("installing"))

	// Parse HTTPRoute manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(keycloakHTTPRouteManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode Keycloak HTTPRoute manifest: %w", err)
	}

	// Customize hostname if domain is provided
	if cfg.Domain != "" {
		hostname := fmt.Sprintf("keycloak.%s", cfg.Domain)
		// Update the hostnames in the HTTPRoute spec
		hostnames := []interface{}{hostname}
		if err := unstructured.SetNestedSlice(obj.Object, hostnames, "spec", "hostnames"); err == nil {
			status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Configured Keycloak hostname: %s", hostname)).
				WithResource("keycloak-httproute").
				WithAction("configuring"))
		}
	}

	// Apply HTTPRoute resource
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply Keycloak HTTPRoute: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Keycloak HTTPRoute created").
		WithResource("keycloak-httproute").
		WithAction("created"))

	return nil
}

// pluralizeKind converts a Kubernetes Kind to its plural resource name
// Handles special cases like GatewayClass -> gatewayclasses
func pluralizeKind(kind string) string {
	lower := strings.ToLower(kind)

	// Special cases for common Kubernetes resources
	switch lower {
	case "gatewayclass":
		return "gatewayclasses"
	case "ingressclass":
		return "ingressclasses"
	case "storageclass":
		return "storageclasses"
	case "priorityclass":
		return "priorityclasses"
	}

	// Handle words ending in 's', 'x', 'z', 'ch', 'sh' -> add 'es'
	if strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "x") ||
		strings.HasSuffix(lower, "z") || strings.HasSuffix(lower, "ch") ||
		strings.HasSuffix(lower, "sh") {
		return lower + "es"
	}

	// Handle words ending in 'y' preceded by consonant -> 'ies'
	if strings.HasSuffix(lower, "y") && len(lower) > 1 {
		beforeY := lower[len(lower)-2]
		if beforeY != 'a' && beforeY != 'e' && beforeY != 'i' && beforeY != 'o' && beforeY != 'u' {
			return lower[:len(lower)-1] + "ies"
		}
	}

	// Default: just add 's'
	return lower + "s"
}

// applyResource applies a Kubernetes resource using the dynamic client
func applyResource(ctx context.Context, kubeconfigBytes []byte, obj *unstructured.Unstructured) error {
	// Create dynamic client
	dynamicClient, err := NewDynamicClientFromKubeconfig(ctx, kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	gvk := obj.GroupVersionKind()
	// Pluralize the kind properly (e.g., Gateway -> gateways, GatewayClass -> gatewayclasses)
	resourceName := pluralizeKind(gvk.Kind)
	gvr := gvk.GroupVersion().WithResource(resourceName)
	namespace := obj.GetNamespace()

	// Try to get existing resource
	var existingObj *unstructured.Unstructured
	if namespace != "" {
		existingObj, err = dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
	} else {
		existingObj, err = dynamicClient.Resource(gvr).Get(ctx, obj.GetName(), metav1.GetOptions{})
	}

	if err == nil {
		// Resource exists, update it
		obj.SetResourceVersion(existingObj.GetResourceVersion())
		if namespace != "" {
			_, err = dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
		} else {
			_, err = dynamicClient.Resource(gvr).Update(ctx, obj, metav1.UpdateOptions{})
		}
		if err != nil {
			return fmt.Errorf("failed to update resource: %w", err)
		}
	} else {
		// Resource doesn't exist, create it
		if namespace != "" {
			_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
		} else {
			_, err = dynamicClient.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
		}
		if err != nil {
			return fmt.Errorf("failed to create resource: %w", err)
		}
	}

	return nil
}

// installArgoCDProject installs the ArgoCD AppProject for foundational services
func installArgoCDProject(ctx context.Context, kubeconfigBytes []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.installArgoCDProject")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Installing ArgoCD Project for foundational services").
		WithResource("argocd-project").
		WithAction("installing"))

	// Parse ArgoCD Project manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err := decoder.Decode([]byte(argoCDProjectManifest), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode ArgoCD Project manifest: %w", err)
	}

	// Apply ArgoCD Project resource
	if err := applyResource(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply ArgoCD Project: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "ArgoCD Project created").
		WithResource("argocd-project").
		WithAction("created"))

	return nil
}
