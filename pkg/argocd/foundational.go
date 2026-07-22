package argocd

import (
	"context"
	"fmt"
	"reflect"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// KeycloakDefaultNamespace is the namespace where Keycloak is deployed.
	KeycloakDefaultNamespace = "keycloak"

	// NebariSystemNamespace is the namespace where Nebari system services are deployed (e.g., landing page).
	NebariSystemNamespace = "nebari-system"

	// KeycloakDefaultAdminSecretName is the name of the Kubernetes secret containing Keycloak admin credentials.
	KeycloakDefaultAdminSecretName = "keycloak-admin-credentials" //nolint:gosec // This is a secret name reference, not a credential

	// NebariLandingRedisSecretName is the name of the Kubernetes secret containing Redis password for nebari-landing.
	NebariLandingRedisSecretName = "nebari-landing-redis" //nolint:gosec // This is a secret name reference, not a credential

	// PartOfLabel is the app.kubernetes.io/part-of label key.
	PartOfLabel = "app.kubernetes.io/part-of"

	// NebariFoundationalPartOf is the value of the app.kubernetes.io/part-of label for foundational resources.
	NebariFoundationalPartOf = "nebari-foundational"

	// ManagedByLabel is the app.kubernetes.io/managed-by label key.
	ManagedByLabel = "app.kubernetes.io/managed-by"

	// NebariManagedByValue is the value of the app.kubernetes.io/managed-by label for Nebari-managed resources.
	NebariManagedByValue = "nebari-infrastructure-core"

	// LonghornDefaultNamespace is the namespace where Longhorn (and its UI) is deployed.
	LonghornDefaultNamespace = "longhorn-system"

	// LonghornOIDCClientSecretName is the name of the Kubernetes secret holding the
	// pre-generated OIDC client secret for the Longhorn UI Keycloak client. The same
	// value is written into both the keycloak namespace (read by realm-setup-job) and
	// the longhorn-system namespace (read by the SecurityPolicy that fronts the UI).
	LonghornOIDCClientSecretName = "longhorn-oidc-client-secret" //nolint:gosec // Secret name reference, not a credential
)

// FoundationalConfig holds configuration for foundational services
type FoundationalConfig struct {
	// Keycloak configuration
	Keycloak KeycloakConfig

	// ArgoCD SSO configuration
	ArgoCD ArgoCDSSOConfig

	// Longhorn UI SSO configuration
	Longhorn LonghornSSOConfig

	// LandingPage configuration
	LandingPage LandingPageConfig

	// MetalLB configuration (local deployments only)
	MetalLB MetalLBConfig
}

// KeycloakConfig holds Keycloak-specific configuration
type KeycloakConfig struct {
	Enabled               bool
	AdminPassword         string
	AdminUsername         string
	DBPassword            string // Password for keycloak DB user
	PostgresAdminPassword string // Password for postgres superuser
	PostgresUserPassword  string // Password for postgres regular user
	Hostname              string
	RealmAdminUsername    string // Username for the admin user in the nebari realm
	RealmAdminPassword    string // Password for the admin user in the nebari realm
}

// LandingPageConfig holds landing page-specific configuration
type LandingPageConfig struct {
	RedisPassword string // Password for Redis used by nebari-landing
}

// MetalLBConfig holds MetalLB-specific configuration
type MetalLBConfig struct {
	Enabled     bool
	AddressPool string // e.g., "192.168.1.100-192.168.1.110"
}

// ArgoCDSSOConfig holds ArgoCD SSO configuration
type ArgoCDSSOConfig struct {
	ClientSecret string // Pre-generated OIDC client secret for ArgoCD's Keycloak integration
}

// LonghornSSOConfig holds Longhorn UI SSO configuration.
// ClientSecret is the pre-generated OIDC client secret used by the Envoy Gateway
// SecurityPolicy that protects longhorn.<domain>. Empty when Longhorn UI exposure
// is disabled — either because Longhorn is not installed or Keycloak is not enabled.
type LonghornSSOConfig struct {
	ClientSecret string
}

// InstallFoundationalServices installs foundational services via GitOps.
// This function handles the bootstrap phase:
// 1. Creates the ArgoCD Project for foundational services
// 2. Creates required secrets (Keycloak, PostgreSQL credentials)
// 3. Applies the root App-of-Apps which triggers ArgoCD to sync all other resources
//
// All other resources (cert-manager, envoy-gateway, keycloak, etc.) are managed
// via ArgoCD from the git repository. src may be a local or remote repository
func InstallFoundationalServices(ctx context.Context, cfg *config.NebariConfig, clusterProvider cluster.Provider, src repository.Source, foundationalCfg FoundationalConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.InstallFoundationalServices")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", clusterProvider.Name()),
		attribute.String("project_name", cfg.ProjectName),
		attribute.Bool("keycloak_enabled", foundationalCfg.Keycloak.Enabled),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Installing foundational services via GitOps").
		WithResource("foundational").
		WithAction("installing"))

	// Get kubeconfig from provider
	kubeconfigBytes, err := clusterProvider.GetKubeconfig(ctx, cfg.ProjectName, cfg.Cluster)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	// 1. Install ArgoCD AppProjects (foundational scoped, nebari-apps, default deny-all)
	settings := clusterProvider.InfraSettings(cfg.Cluster)
	projectData := NewTemplateData(cfg, src, settings)
	if err := InstallProject(ctx, kubeconfigBytes, projectData); err != nil {
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
		if err := createNamespace(ctx, k8sClient, KeycloakDefaultNamespace); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Keycloak namespace: %w", err)
		}

		// Create secrets for Keycloak and PostgreSQL
		if err := createKeycloakSecrets(ctx, k8sClient, foundationalCfg.Keycloak, foundationalCfg.ArgoCD); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Keycloak secrets: %w", err)
		}

		// Create namespace for Nebari system services
		if err := createNamespace(ctx, k8sClient, NebariSystemNamespace); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Nebari system namespace: %w", err)
		}

		// Create Redis secret for landing page
		if err := createLandingPageSecrets(ctx, k8sClient, foundationalCfg.LandingPage); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create landing page secrets: %w", err)
		}

		// Create namespace + dual OIDC client-secret Secret for Longhorn UI exposure.
		// No-op when foundationalCfg.Longhorn.ClientSecret == "" (Longhorn disabled or Keycloak off).
		if foundationalCfg.Longhorn.ClientSecret != "" {
			if err := createNamespace(ctx, k8sClient, LonghornDefaultNamespace); err != nil {
				span.RecordError(err)
				return fmt.Errorf("failed to create Longhorn namespace: %w", err)
			}
			if err := createLonghornSecrets(ctx, k8sClient, foundationalCfg.Longhorn); err != nil {
				span.RecordError(err)
				return fmt.Errorf("failed to create Longhorn secrets: %w", err)
			}
		}
	}

	// 3. Apply root App-of-Apps if git configuration is available
	if src != nil {
		if err := ApplyRootAppOfApps(ctx, kubeconfigBytes, src); err != nil {
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

// createOrUpdateConfigMap creates the ConfigMap, or reconciles its data and
// labels when it already exists with different contents. Unlike createSecret
// (create-only by design, for generated one-time credentials), an org CA bundle
// is operator-supplied and rotates, so this upserts to make a changed
// trust_bundle actually propagate.
func createOrUpdateConfigMap(ctx context.Context, client kubernetes.Interface, cm *corev1.ConfigMap) error {
	namespace := cm.Namespace
	existing, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, cm.Name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			// A transient/permission error must not be mistaken for "absent" —
			// otherwise we'd blindly Create and mask the real failure.
			return fmt.Errorf("failed to get configmap %s: %w", cm.Name, err)
		}
		if _, err := client.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("failed to create configmap %s: %w", cm.Name, err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Created ConfigMap %s", cm.Name)).
			WithResource("configmap").
			WithAction("created").
			WithMetadata("configmap_name", cm.Name))
		return nil
	}

	// Reconcile both data and our managed labels; no-op when already in sync.
	if reflect.DeepEqual(existing.Data, cm.Data) && labelsContain(existing.Labels, cm.Labels) {
		return nil
	}
	existing.Data = cm.Data
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	for k, v := range cm.Labels {
		existing.Labels[k] = v
	}
	if _, err := client.CoreV1().ConfigMaps(namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update configmap %s: %w", cm.Name, err)
	}
	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Updated ConfigMap %s", cm.Name)).
		WithResource("configmap").
		WithAction("updated").
		WithMetadata("configmap_name", cm.Name))
	return nil
}

// labelsContain reports whether have already includes every key/value in want.
func labelsContain(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// createOrgCAConfigMap upserts the install-time argocd-org-ca ConfigMap holding
// the operator's org CA, labeled as a foundational resource. It is the org-CA
// analogue of createKeycloakSecrets — keeping the resource construction beside
// the other foundational create helpers rather than inline in Install.
func createOrgCAConfigMap(ctx context.Context, client kubernetes.Interface, namespace, orgCABundlePEM string) error {
	return createOrUpdateConfigMap(ctx, client, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orgCAConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				PartOfLabel:    NebariFoundationalPartOf,
				ManagedByLabel: NebariManagedByValue,
			},
		},
		Data: map[string]string{orgCAConfigMapKey: orgCABundlePEM},
	})
}

// createSecret creates a Kubernetes secret if it doesn't already exist.
// Create-only by design: these are generated one-time credentials that must
// never be overwritten on re-deploy. For operator-supplied data that can rotate
// (e.g. a CA bundle), use createOrUpdateConfigMap / createOrgCAConfigMap instead.
func createSecret(ctx context.Context, client kubernetes.Interface, secret *corev1.Secret) error {
	namespace := secret.Namespace
	_, err := client.CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if err != nil {
		// Secret doesn't exist, create it
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create secret %s: %w", secret.Name, err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Created secret %s", secret.Name)).
			WithResource("secret").
			WithAction("created").
			WithMetadata("secret_name", secret.Name))
	}
	return nil
}

// createKeycloakSecrets creates the required secrets for Keycloak and PostgreSQL
func createKeycloakSecrets(ctx context.Context, client kubernetes.Interface, keycloakCfg KeycloakConfig, argocdSSO ArgoCDSSOConfig) error {
	namespace := KeycloakDefaultNamespace

	// 1. Create admin credentials secret
	if err := createSecret(ctx, client, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KeycloakDefaultAdminSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"admin-username": keycloakCfg.AdminUsername,
			"admin-password": keycloakCfg.AdminPassword,
		},
	}); err != nil {
		return err
	}

	// 2. Create Keycloak PostgreSQL user credentials secret
	if err := createSecret(ctx, client, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keycloak-postgresql-credentials",
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"password": keycloakCfg.DBPassword,
		},
	}); err != nil {
		return err
	}

	// 3. Create PostgreSQL main credentials secret (for PostgreSQL deployment)
	if err := createSecret(ctx, client, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "postgresql-credentials",
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"postgres-password": keycloakCfg.PostgresAdminPassword,
			"user-password":     keycloakCfg.PostgresUserPassword,
		},
	}); err != nil {
		return err
	}

	// 4. Create Nebari realm admin credentials secret
	if keycloakCfg.RealmAdminPassword != "" {
		if err := createSecret(ctx, client, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nebari-realm-admin-credentials",
				Namespace: namespace,
				Labels: map[string]string{
					PartOfLabel:    NebariFoundationalPartOf,
					ManagedByLabel: NebariManagedByValue,
				},
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"username": keycloakCfg.RealmAdminUsername,
				"password": keycloakCfg.RealmAdminPassword,
			},
		}); err != nil {
			return err
		}
	}

	// 5. Create ArgoCD OIDC client secret (used by realm-setup job to configure the Keycloak client)
	if argocdSSO.ClientSecret != "" {
		if err := createSecret(ctx, client, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argocd-oidc-client-secret",
				Namespace: namespace,
				Labels: map[string]string{
					PartOfLabel:    NebariFoundationalPartOf,
					ManagedByLabel: NebariManagedByValue,
				},
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"client-secret": argocdSSO.ClientSecret,
			},
		}); err != nil {
			return err
		}
	}

	return nil
}

// createLonghornSecrets ensures the OIDC client secret used to protect the
// Longhorn UI holds a single value in both the keycloak namespace (read by
// realm-setup-job) and the longhorn-system namespace (read by the Envoy
// Gateway SecurityPolicy). A fresh random value is generated on every deploy,
// so a value already present in either namespace is canonical and wins over
// the generated one: a partial failure on a prior deploy (one namespace
// written, the other not) must reconcile both namespaces to the surviving
// value, not leave the Keycloak client and the SecurityPolicy presenting
// different secrets. The keycloak copy takes precedence because realm-setup-job
// registers the Keycloak client from it. When longhornSSO.ClientSecret is
// empty, nothing is created.
func createLonghornSecrets(ctx context.Context, client kubernetes.Interface, longhornSSO LonghornSSOConfig) error {
	if longhornSSO.ClientSecret == "" {
		return nil
	}

	namespaces := []string{KeycloakDefaultNamespace, LonghornDefaultNamespace}

	existing := make(map[string]*corev1.Secret, len(namespaces))
	for _, ns := range namespaces {
		secret, err := client.CoreV1().Secrets(ns).Get(ctx, LonghornOIDCClientSecretName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			// A transient/permission error must not be mistaken for "absent" —
			// creating with a fresh value here is exactly the divergence we
			// are trying to prevent.
			return fmt.Errorf("failed to get %s in %s: %w", LonghornOIDCClientSecretName, ns, err)
		}
		existing[ns] = secret
	}

	canonical := longhornSSO.ClientSecret
	for _, ns := range namespaces {
		if secret, ok := existing[ns]; ok {
			if v := secret.Data["client-secret"]; len(v) > 0 {
				canonical = string(v)
				break
			}
		}
	}

	for _, ns := range namespaces {
		secret, ok := existing[ns]
		if !ok {
			if _, err := client.CoreV1().Secrets(ns).Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      LonghornOIDCClientSecretName,
					Namespace: ns,
					Labels: map[string]string{
						PartOfLabel:    NebariFoundationalPartOf,
						ManagedByLabel: NebariManagedByValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					"client-secret": canonical,
				},
			}, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create %s in %s: %w", LonghornOIDCClientSecretName, ns, err)
			}
			status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Created secret %s in %s", LonghornOIDCClientSecretName, ns)).
				WithResource("secret").
				WithAction("created").
				WithMetadata("secret_name", LonghornOIDCClientSecretName))
			continue
		}

		if string(secret.Data["client-secret"]) == canonical {
			continue
		}
		secret.Data = nil
		secret.StringData = map[string]string{"client-secret": canonical}
		if _, err := client.CoreV1().Secrets(ns).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update %s in %s: %w", LonghornOIDCClientSecretName, ns, err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Reconciled secret %s in %s to the canonical value", LonghornOIDCClientSecretName, ns)).
			WithResource("secret").
			WithAction("updated").
			WithMetadata("secret_name", LonghornOIDCClientSecretName))
	}

	return nil
}

// createLandingPageSecrets creates the required secrets for the nebari-landing service
func createLandingPageSecrets(ctx context.Context, client kubernetes.Interface, landingCfg LandingPageConfig) error {
	namespace := NebariSystemNamespace

	// Create Redis password secret for nebari-landing
	// This secret is referenced by the helm chart to prevent password regeneration on every sync
	if err := createSecret(ctx, client, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      NebariLandingRedisSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				PartOfLabel:    NebariFoundationalPartOf,
				ManagedByLabel: NebariManagedByValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"redis-password": landingCfg.RedisPassword,
		},
	}); err != nil {
		return err
	}

	return nil
}
