package argocd

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// FoundationalConfig holds configuration for foundational services
type FoundationalConfig struct {
	// Keycloak configuration
	Keycloak KeycloakConfig

	// MetalLB configuration (local deployments only)
	MetalLB MetalLBConfig
}

// KeycloakConfig holds Keycloak-specific configuration
type KeycloakConfig struct {
	Enabled              bool
	AdminPassword        string
	DBPassword           string // Password for keycloak DB user
	PostgresAdminPassword string // Password for postgres superuser
	PostgresUserPassword  string // Password for postgres regular user
	Hostname             string
	RealmAdminPassword   string // Password for the admin user in the nebari realm
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
	ctx, span := tracer.Start(ctx, "argocd.InstallFoundationalServices")
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
	if err := InstallProject(ctx, kubeconfigBytes); err != nil {
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
		k8sClient, err := newK8sClient(kubeconfigBytes)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Kubernetes client: %w", err)
		}

		// Create namespace for Keycloak
		if err := createNamespace(ctx, k8sClient, "keycloak"); err != nil {
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

// newK8sClient creates a Kubernetes clientset from kubeconfig bytes
func newK8sClient(kubeconfigBytes []byte) (*kubernetes.Clientset, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	return kubernetes.NewForConfig(restConfig)
}

// createNamespace creates a namespace if it doesn't exist
func createNamespace(ctx context.Context, client kubernetes.Interface, namespace string) error {
	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Creating namespace: %s", namespace)).
		WithResource("namespace").
		WithAction("creating").
		WithMetadata("namespace", namespace))

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		// Namespace already exists
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Namespace %s already exists", namespace)).
			WithResource("namespace").
			WithAction("exists").
			WithMetadata("namespace", namespace))
		return nil
	}

	_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create namespace %s: %w", namespace, err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Created namespace: %s", namespace)).
		WithResource("namespace").
		WithAction("created").
		WithMetadata("namespace", namespace))

	return nil
}

// createKeycloakSecrets creates the required secrets for Keycloak and PostgreSQL
func createKeycloakSecrets(ctx context.Context, client kubernetes.Interface, keycloakCfg KeycloakConfig) error {
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
			"postgres-password": keycloakCfg.PostgresAdminPassword,
			"user-password":     keycloakCfg.PostgresUserPassword,
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

	// 4. Create Nebari realm admin credentials secret
	if keycloakCfg.RealmAdminPassword != "" {
		realmAdminSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nebari-realm-admin-credentials",
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/part-of":    "nebari-foundational",
					"app.kubernetes.io/managed-by": "nebari-infrastructure-core",
				},
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"username": "admin",
				"password": keycloakCfg.RealmAdminPassword,
			},
		}

		_, err = client.CoreV1().Secrets(namespace).Get(ctx, realmAdminSecret.Name, metav1.GetOptions{})
		if err != nil {
			// Secret doesn't exist, create it
			_, err = client.CoreV1().Secrets(namespace).Create(ctx, realmAdminSecret, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create Nebari realm admin credentials secret: %w", err)
			}
			status.Send(ctx, status.NewUpdate(status.LevelInfo, "Created Nebari realm admin credentials secret").
				WithResource("secret").
				WithAction("created").
				WithMetadata("secret_name", realmAdminSecret.Name))
		}
	}

	return nil
}
