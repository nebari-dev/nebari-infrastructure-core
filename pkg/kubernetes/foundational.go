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

// FoundationalConfig holds configuration for foundational services
type FoundationalConfig struct {
	// Keycloak configuration
	Keycloak KeycloakConfig
}

// KeycloakConfig holds Keycloak-specific configuration
type KeycloakConfig struct {
	Enabled       bool
	AdminPassword string
	DBPassword    string
	Hostname      string
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

	// Create Kubernetes client
	k8sClient, err := NewClientFromKubeconfig(ctx, kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Install Keycloak if enabled
	if foundationalCfg.Keycloak.Enabled {
		if err := installKeycloak(ctx, k8sClient, kubeconfigBytes, cfg, foundationalCfg.Keycloak); err != nil {
			span.RecordError(err)
			// Don't fail the entire deployment if Keycloak installation fails
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
