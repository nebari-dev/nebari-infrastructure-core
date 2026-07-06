package argocd

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

// getSecretVal retrieves a value from a secret, checking both Data and StringData
// The fake client doesn't convert StringData to Data like the real API server does
func getSecretVal(secret *corev1.Secret, key string) string {
	if val, ok := secret.Data[key]; ok {
		return string(val)
	}
	if val, ok := secret.StringData[key]; ok {
		return val
	}
	return ""
}

func TestConfigureOCIAccess(t *testing.T) {
	ctx := context.Background()
	namespace := "argocd"

	t.Run("creates OCI secret", func(t *testing.T) {
		// Create namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}
		client := fake.NewSimpleClientset(ns) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

		err := ConfigureOCIAccess(ctx, client, namespace)
		if err != nil {
			t.Fatalf("ConfigureOCIAccess() error = %v", err)
		}

		// Verify secret was created
		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, "docker-oci-repo", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get secret: %v", err)
		}

		// Check secret fields
		if secret.Labels["argocd.argoproj.io/secret-type"] != "repository" {
			t.Error("secret should have argocd repository label")
		}
		if got := getSecretVal(secret, "name"); got != "docker-oci" {
			t.Errorf("secret name = %q, want %q", got, "docker-oci")
		}
		if got := getSecretVal(secret, "type"); got != "helm" {
			t.Errorf("secret type = %q, want %q", got, "helm")
		}
		if got := getSecretVal(secret, "url"); got != "oci://docker.io" {
			t.Errorf("secret url = %q, want %q", got, "oci://docker.io")
		}
		if got := getSecretVal(secret, "enableOCI"); got != "true" {
			t.Errorf("secret enableOCI = %q, want %q", got, "true")
		}
	})

	t.Run("updates existing OCI secret", func(t *testing.T) {
		// Create namespace and existing secret
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}
		existingSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "docker-oci-repo",
				Namespace: namespace,
			},
			Data: map[string][]byte{
				"old-key": []byte("old-value"),
			},
		}
		client := fake.NewSimpleClientset(ns, existingSecret) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

		err := ConfigureOCIAccess(ctx, client, namespace)
		if err != nil {
			t.Fatalf("ConfigureOCIAccess() error = %v", err)
		}

		// Verify secret was updated
		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, "docker-oci-repo", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get secret: %v", err)
		}

		// Should have new fields (check StringData since that's what Update sets)
		if got := getSecretVal(secret, "enableOCI"); got != "true" {
			t.Errorf("secret should have been updated with enableOCI, got %q", got)
		}
	})
}

func TestConfigureGitRepoAccess(t *testing.T) {
	ctx := context.Background()
	namespace := "argocd"

	t.Run("creates git secret with token auth", func(t *testing.T) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		client := fake.NewSimpleClientset(ns) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

		src := repository.RemoteSource{
			URL:      "https://github.com/example/repo.git",
			PushAuth: repository.TokenAuth{Token: "ghp_test_token_123"},
		}

		if err := ConfigureGitRepoAccess(ctx, client, src, namespace); err != nil {
			t.Fatalf("ConfigureGitRepoAccess() error = %v", err)
		}

		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, "gitops-repo-creds", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get secret: %v", err)
		}
		if secret.Labels["argocd.argoproj.io/secret-type"] != "repository" {
			t.Error("secret should have argocd repository label")
		}
		if got := getSecretVal(secret, "name"); got != "gitops-repo" {
			t.Errorf("secret name = %q, want %q", got, "gitops-repo")
		}
		if got := getSecretVal(secret, "type"); got != "git" {
			t.Errorf("secret type = %q, want %q", got, "git")
		}
		if got := getSecretVal(secret, "url"); got != "https://github.com/example/repo.git" {
			t.Errorf("secret url = %q, want %q", got, "https://github.com/example/repo.git")
		}
		if got := getSecretVal(secret, "password"); got != "ghp_test_token_123" {
			t.Errorf("secret password = %q, want %q", got, "ghp_test_token_123")
		}
		if got := getSecretVal(secret, "username"); got != "git" {
			t.Errorf("secret username = %q, want %q", got, "git")
		}
	})

	t.Run("returns error when no credentials provided", func(t *testing.T) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		client := fake.NewSimpleClientset(ns) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

		src := repository.RemoteSource{URL: "https://github.com/example/repo.git"}

		if err := ConfigureGitRepoAccess(ctx, client, src, namespace); err == nil {
			t.Error("ConfigureGitRepoAccess() should return error when no credentials provided")
		}
	})

	t.Run("updates existing git secret", func(t *testing.T) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		existingSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "gitops-repo-creds", Namespace: namespace},
			Data:       map[string][]byte{"old-key": []byte("old-value")},
		}
		client := fake.NewSimpleClientset(ns, existingSecret) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

		src := repository.RemoteSource{
			URL:      "https://github.com/example/new-repo.git",
			PushAuth: repository.TokenAuth{Token: "new_token"},
		}

		if err := ConfigureGitRepoAccess(ctx, client, src, namespace); err != nil {
			t.Fatalf("ConfigureGitRepoAccess() error = %v", err)
		}
		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, "gitops-repo-creds", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get secret: %v", err)
		}
		if got := getSecretVal(secret, "url"); got != "https://github.com/example/new-repo.git" {
			t.Errorf("secret url should be updated, got %q", got)
		}
	})

	t.Run("prefers ArgoCD read credentials over push credentials", func(t *testing.T) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		client := fake.NewSimpleClientset(ns) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

		src := repository.RemoteSource{
			URL:      "https://github.com/example/repo.git",
			PushAuth: repository.TokenAuth{Token: "push_token"},
			ReadAuth: repository.TokenAuth{Token: "read_token"},
		}

		if err := ConfigureGitRepoAccess(ctx, client, src, namespace); err != nil {
			t.Fatalf("ConfigureGitRepoAccess() error = %v", err)
		}
		secret, _ := client.CoreV1().Secrets(namespace).Get(ctx, "gitops-repo-creds", metav1.GetOptions{})
		if got := getSecretVal(secret, "password"); got != "read_token" {
			t.Errorf("secret password = %q, want read_token (ReadAuth wins)", got)
		}
	})

	t.Run("creates git secret with SSH key auth", func(t *testing.T) {
		sshKey := "-----BEGIN OPENSSH PRIVATE KEY-----\ntest-ssh-key-content\n-----END OPENSSH PRIVATE KEY-----"
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		client := fake.NewSimpleClientset(ns) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

		src := repository.RemoteSource{
			URL:      "git@github.com:example/repo.git",
			PushAuth: repository.SSHKeyAuth{Key: sshKey},
		}

		if err := ConfigureGitRepoAccess(ctx, client, src, namespace); err != nil {
			t.Fatalf("ConfigureGitRepoAccess() error = %v", err)
		}
		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, "gitops-repo-creds", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get secret: %v", err)
		}
		if got := getSecretVal(secret, "sshPrivateKey"); got != sshKey {
			t.Error("secret should contain SSH private key")
		}
	})
}
