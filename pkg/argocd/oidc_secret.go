package argocd

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// oidcClientSecretKey is the key holding the client secret in the OIDC
	// client Secrets that the realm-setup job reads.
	oidcClientSecretKey = "client-secret"

	// argoCDSecretName is the Secret the Argo CD Helm chart renders
	// configs.secret.extra into.
	argoCDSecretName = "argocd-secret"

	// argoCDOIDCClientSecretKey is the key in argoCDSecretName holding the
	// OIDC client secret; the OIDC config refers to it as
	// $oidc.keycloak.clientSecret.
	argoCDOIDCClientSecretKey = "oidc.keycloak.clientSecret" //nolint:gosec // Secret key reference, not a credential
)

// EnsureArgoCDClientSecret returns the OIDC client secret to install Argo CD
// with, after making sure the keycloak namespace stores that same value. It
// also reports whether an installed Argo CD holds a different value, in which
// case the caller has to upgrade Argo CD even if its chart version matches.
//
// Argo CD gets the secret through its Helm values, while the realm-setup job
// registers the Keycloak client from the stored Secret. A value stored by an
// earlier deploy therefore wins over generated: handing Argo CD a fresh value
// on every deploy is how the two drifted apart (#463). generated is stored and
// returned only when neither the stored Secret nor an installed Argo CD holds
// a value yet.
func EnsureArgoCDClientSecret(ctx context.Context, cfg *config.NebariConfig, clusterProvider cluster.Provider, generated string) (secret string, argoCDDiffers bool, err error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.EnsureArgoCDClientSecret")
	defer span.End()

	kubeconfigBytes, err := clusterProvider.GetKubeconfig(ctx, cfg.ProjectName, cfg.Cluster)
	if err != nil {
		span.RecordError(err)
		return "", false, fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return "", false, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// A cluster created moments ago may not answer yet. Wait for it the way
	// Install does instead of giving up on an API error that would clear up.
	if err := waitForClusterReady(ctx, client, 5*time.Minute); err != nil {
		span.RecordError(err)
		return "", false, fmt.Errorf("cluster not ready: %w", err)
	}
	if err := createNamespace(ctx, client, KeycloakDefaultNamespace); err != nil {
		span.RecordError(err)
		return "", false, fmt.Errorf("failed to create Keycloak namespace: %w", err)
	}

	secret, argoCDDiffers, err = ensureArgoCDClientSecret(ctx, client, generated)
	if err != nil {
		span.RecordError(err)
		return "", false, err
	}
	return secret, argoCDDiffers, nil
}

// ensureArgoCDClientSecret returns the client secret stored in the keycloak
// namespace, which wins because the realm-setup job registers the Keycloak
// client from it. When it holds no value, the Secret is created or filled in
// with the value Argo CD is already installed with, and with generated only
// when there is none. The bool reports whether an installed Argo CD holds a
// value other than the returned one. Errors other than NotFound are returned:
// treating them as "nothing stored" could give Argo CD a value Keycloak never
// sees.
func ensureArgoCDClientSecret(ctx context.Context, client kubernetes.Interface, generated string) (string, bool, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.ensureArgoCDClientSecret")
	defer span.End()

	secrets := client.CoreV1().Secrets(KeycloakDefaultNamespace)
	stored, err := secrets.Get(ctx, ArgoCDOIDCClientSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		stored = nil
	} else if err != nil {
		span.RecordError(err)
		return "", false, fmt.Errorf("failed to get secret %s: %w", ArgoCDOIDCClientSecretName, err)
	}

	installed, argoCDInstalled, err := installedArgoCDClientSecret(ctx, client)
	if err != nil {
		span.RecordError(err)
		return "", false, err
	}

	var value string
	if stored != nil && len(stored.Data[oidcClientSecretKey]) > 0 {
		value = string(stored.Data[oidcClientSecretKey])
	} else {
		// Nothing usable is stored. If Argo CD is already installed, keep the
		// value it has, so a deploy that skips the Helm upgrade can't leave it
		// on a different value from the one the realm-setup job registers.
		value = installed
		if value == "" {
			value = generated
		}
		if err := storeArgoCDClientSecret(ctx, client, stored, value); err != nil {
			span.RecordError(err)
			return "", false, err
		}
	}

	// An upgrade skipped because the chart version matches would leave a
	// drifted Argo CD on its old value until the next chart bump.
	argoCDDiffers := argoCDInstalled && installed != value
	if argoCDDiffers {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Argo CD has a different OIDC client secret from the stored one, upgrading it to match").
			WithResource("secret").
			WithAction("drift-detected").
			WithMetadata("secret_name", argoCDSecretName))
	}
	return value, argoCDDiffers, nil
}

// storeArgoCDClientSecret creates the keycloak namespace Secret holding value,
// or fills value into stored when the Secret exists without one.
func storeArgoCDClientSecret(ctx context.Context, client kubernetes.Interface, stored *corev1.Secret, value string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.storeArgoCDClientSecret")
	defer span.End()

	secrets := client.CoreV1().Secrets(KeycloakDefaultNamespace)
	if stored == nil {
		if _, err := secrets.Create(ctx, argoCDOIDCSecret(value), metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create secret %s: %w", ArgoCDOIDCClientSecretName, err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Created secret %s", ArgoCDOIDCClientSecretName)).
			WithResource("secret").
			WithAction("created").
			WithMetadata("secret_name", ArgoCDOIDCClientSecretName))
		return nil
	}

	if stored.Data == nil {
		stored.Data = map[string][]byte{}
	}
	stored.Data[oidcClientSecretKey] = []byte(value)
	if _, err := secrets.Update(ctx, stored, metav1.UpdateOptions{}); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to update secret %s: %w", ArgoCDOIDCClientSecretName, err)
	}
	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Stored a client secret in %s, which had none", ArgoCDOIDCClientSecretName)).
		WithResource("secret").
		WithAction("updated").
		WithMetadata("secret_name", ArgoCDOIDCClientSecretName))
	return nil
}

// installedArgoCDClientSecret returns the OIDC client secret Argo CD is
// installed with, and whether Argo CD's Secret exists at all. The value is ""
// when Argo CD is not installed or holds none.
func installedArgoCDClientSecret(ctx context.Context, client kubernetes.Interface) (string, bool, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.installedArgoCDClientSecret")
	defer span.End()

	secret, err := client.CoreV1().Secrets(DefaultNamespace).Get(ctx, argoCDSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		span.RecordError(err)
		return "", false, fmt.Errorf("failed to get secret %s: %w", argoCDSecretName, err)
	}
	return string(secret.Data[argoCDOIDCClientSecretKey]), true, nil
}

// argoCDOIDCSecret builds the Secret the realm-setup job reads the ArgoCD
// OIDC client secret from.
func argoCDOIDCSecret(clientSecret string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDOIDCClientSecretName,
			Namespace: KeycloakDefaultNamespace,
			Labels: map[string]string{
				PartOfLabel:    NebariFoundationalPartOf,
				ManagedByLabel: NebariManagedByValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{oidcClientSecretKey: []byte(clientSecret)},
	}
}
