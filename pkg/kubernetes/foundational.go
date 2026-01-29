package kubernetes

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

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

//go:embed foundational/argocd-project.yaml
var argoCDProjectManifest string

// rootAppOfAppsTemplate is the template for the root App-of-Apps Application
const rootAppOfAppsTemplate = `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nebari-root
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: nebari-foundational
    app.kubernetes.io/managed-by: nebari-infrastructure-core
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: foundational
  source:
    repoURL: {{ .GitRepoURL }}
    targetRevision: {{ .GitBranch }}
    path: {{ if .GitPath }}{{ .GitPath }}/{{ end }}apps
    directory:
      recurse: false
      include: '*.yaml'
      exclude: 'root.yaml'
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
      allowEmpty: false
    syncOptions:
      - CreateNamespace=true
    retry:
      limit: 5
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 3m
`

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

// InstallFoundationalServices installs foundational services via GitOps.
// This function handles the bootstrap phase:
// 1. Creates the ArgoCD Project for foundational services
// 2. Creates required secrets (Keycloak, PostgreSQL credentials)
// 3. Applies the root App-of-Apps which triggers ArgoCD to sync all other resources
//
// All other resources (cert-manager, envoy-gateway, keycloak, etc.) are managed
// via ArgoCD from the git repository.
func InstallFoundationalServices(ctx context.Context, cfg *config.NebariConfig, prov provider.Provider, foundationalCfg FoundationalConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "kubernetes.InstallFoundationalServices")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", prov.Name()),
		attribute.String("project_name", cfg.ProjectName),
		attribute.Bool("keycloak_enabled", foundationalCfg.Keycloak.Enabled),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Installing foundational services via GitOps").
		WithResource("foundational").
		WithAction("installing"))

	// Get kubeconfig from provider
	kubeconfigBytes, err := prov.GetKubeconfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	// 1. Install ArgoCD Project for foundational services
	if err := installArgoCDProject(ctx, kubeconfigBytes); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install ArgoCD Project").
			WithResource("argocd-project").
			WithAction("install-failed").
			WithMetadata("error", err.Error()))
		// Don't fail - project is optional, apps will fall back to default project
	}

	// 2. Create secrets if Keycloak is enabled
	if foundationalCfg.Keycloak.Enabled {
		// Create Kubernetes client for secret management
		k8sClient, err := NewClientFromKubeconfig(ctx, kubeconfigBytes)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Kubernetes client: %w", err)
		}

		// Create namespace for Keycloak
		if err := createKeycloakNamespace(ctx, k8sClient); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Keycloak namespace: %w", err)
		}

		// Create secrets for Keycloak and PostgreSQL
		if err := createKeycloakSecrets(ctx, k8sClient, foundationalCfg.Keycloak); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Keycloak secrets: %w", err)
		}
	}

	// 3. Apply root App-of-Apps if git repository is configured
	if cfg.GitRepository != nil {
		if err := ApplyRootAppOfApps(ctx, kubeconfigBytes, cfg); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to apply root App-of-Apps: %w", err)
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Foundational services bootstrap completed").
		WithResource("foundational").
		WithAction("installed").
		WithMetadata("info", "ArgoCD will sync remaining resources from git"))

	return nil
}

// ApplyRootAppOfApps applies the root App-of-Apps Application which triggers
// ArgoCD to sync all child applications from the git repository.
func ApplyRootAppOfApps(ctx context.Context, kubeconfigBytes []byte, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "kubernetes.ApplyRootAppOfApps")
	defer span.End()

	if cfg.GitRepository == nil {
		return fmt.Errorf("git repository configuration is required for root App-of-Apps")
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Applying root App-of-Apps").
		WithResource("nebari-root").
		WithAction("installing"))

	// Prepare template data
	data := struct {
		GitRepoURL string
		GitBranch  string
		GitPath    string
	}{
		GitRepoURL: cfg.GitRepository.URL,
		GitBranch:  cfg.GitRepository.GetBranch(),
		GitPath:    cfg.GitRepository.Path,
	}

	// Parse and execute template
	tmpl, err := template.New("root-app").Parse(rootAppOfAppsTemplate)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to parse root App-of-Apps template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to execute root App-of-Apps template: %w", err)
	}

	// Parse the rendered manifest
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	_, _, err = decoder.Decode(buf.Bytes(), nil, obj)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to decode root App-of-Apps manifest: %w", err)
	}

	// Apply the root App-of-Apps
	if err := ApplyArgoCDApplication(ctx, kubeconfigBytes, obj); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to apply root App-of-Apps: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Root App-of-Apps applied").
		WithResource("nebari-root").
		WithAction("applied").
		WithMetadata("info", "ArgoCD will now sync all applications from git"))

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

// createKeycloakNamespace creates the Keycloak namespace
func createKeycloakNamespace(ctx context.Context, client *kubernetes.Clientset) error {
	return CreateNamespace(ctx, client, "keycloak")
}

// createKeycloakSecrets creates the required secrets for Keycloak and PostgreSQL
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
			"password": keycloakCfg.DBPassword,
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
			"postgres-password": keycloakCfg.DBPassword + "-admin",
			"user-password":     keycloakCfg.DBPassword + "-user",
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

// pluralizeKind converts a Kubernetes Kind to its plural resource name
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
