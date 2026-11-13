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
// with, after making sure the keycloak namespace stores that same value.
//
// Argo CD gets the secret through its Helm values, while the realm-setup job
// registers the Keycloak client from the stored Secret. A value stored by an
// earlier deploy therefore wins over generated: handing Argo CD a fresh value
// on every deploy is how the two drifted apart (#463). generated is stored and
// returned only when neither the stored Secret nor an installed Argo CD holds
// a value yet.
func EnsureArgoCDClientSecret(ctx context.Context, cfg *config.NebariConfig, clusterProvider cluster.Provider, generated string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.EnsureArgoCDClientSecret")
	defer span.End()

	kubeconfigBytes, err := clusterProvider.GetKubeconfig(ctx, cfg.ProjectName, cfg.Cluster)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	client, err := newK8sClient(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// A cluster created moments ago may not answer yet. Wait for it the way
	// Install does instead of giving up on an API error that would clear up.
	if err := waitForClusterReady(ctx, client, 5*time.Minute); err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("cluster not ready: %w", err)
	}
	if err := createNamespace(ctx, client, KeycloakDefaultNamespace); err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to create Keycloak namespace: %w", err)
	}

	secret, err := ensureArgoCDClientSecret(ctx, client, generated)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	return secret, nil
}

// ensureArgoCDClientSecret returns the client secret stored in the keycloak
// namespace, which wins because the realm-setup job registers the Keycloak
// client from it. When it holds no value, the Secret is created or filled in
// with the value Argo CD is already installed with, and with generated only
// when there is none. Errors other than NotFound are returned: treating them
// as "nothing stored" could give Argo CD a value Keycloak never sees.
func ensureArgoCDClientSecret(ctx context.Context, client kubernetes.Interface, generated string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.ensureArgoCDClientSecret")
	defer span.End()

	secrets := client.CoreV1().Secrets(KeycloakDefaultNamespace)
	stored, err := secrets.Get(ctx, ArgoCDOIDCClientSecretName, metav1.GetOptions{})
	found := err == nil
	if err != nil && !apierrors.IsNotFound(err) {
		span.RecordError(err)
		return "", fmt.Errorf("failed to get secret %s: %w", ArgoCDOIDCClientSecretName, err)
	}
	if found {
		if value := stored.Data[oidcClientSecretKey]; len(value) > 0 {
			return string(value), nil
		}
	}

	// Nothing usable is stored. If Argo CD is already installed, keep the value
	// it has: a Helm upgrade that gets skipped this run would otherwise leave it
	// on a different value from the one the realm-setup job registers.
	value, err := installedArgoCDClientSecret(ctx, client)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	if value == "" {
		value = generated
	}

	if !found {
		if _, err := secrets.Create(ctx, argoCDOIDCSecret(value), metav1.CreateOptions{}); err != nil {
			span.RecordError(err)
			return "", fmt.Errorf("failed to create secret %s: %w", ArgoCDOIDCClientSecretName, err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Created secret %s", ArgoCDOIDCClientSecretName)).
			WithResource("secret").
			WithAction("created").
			WithMetadata("secret_name", ArgoCDOIDCClientSecretName))
		return value, nil
	}

	if stored.Data == nil {
		stored.Data = map[string][]byte{}
	}
	stored.Data[oidcClientSecretKey] = []byte(value)
	if _, err := secrets.Update(ctx, stored, metav1.UpdateOptions{}); err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to update secret %s: %w", ArgoCDOIDCClientSecretName, err)
	}
	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Stored a client secret in %s, which had none", ArgoCDOIDCClientSecretName)).
		WithResource("secret").
		WithAction("updated").
		WithMetadata("secret_name", ArgoCDOIDCClientSecretName))
	return value, nil
}

// installedArgoCDClientSecret returns the OIDC client secret Argo CD is
// installed with, or "" when Argo CD is not installed or holds none.
func installedArgoCDClientSecret(ctx context.Context, client kubernetes.Interface) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "argocd.installedArgoCDClientSecret")
	defer span.End()

	secret, err := client.CoreV1().Secrets(DefaultNamespace).Get(ctx, argoCDSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to get secret %s: %w", argoCDSecretName, err)
	}
	return string(secret.Data[argoCDOIDCClientSecretKey]), nil
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
