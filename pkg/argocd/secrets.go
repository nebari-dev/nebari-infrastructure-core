package argocd

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// ConfigureGitRepoAccess configures Argo CD to access the GitOps repository
func ConfigureGitRepoAccess(ctx context.Context, client *kubernetes.Clientset, cfg *config.NebariConfig, namespace string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.ConfigureGitRepoAccess")
	defer span.End()

	if cfg.GitRepository == nil {
		return nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Configuring Git repository access for Argo CD").
		WithResource("argocd").
		WithAction("configuring-git"))

	// Get the ArgoCD auth config (falls back to main auth if not specified)
	authCfg := cfg.GitRepository.GetArgoCDAuth()

	// Create repository secret data
	secretData := map[string]string{
		"name": "gitops-repo",
		"type": "git",
		"url":  cfg.GitRepository.URL,
	}

	// Try SSH key first, then token
	if sshKey, err := authCfg.GetSSHKey(); err == nil && sshKey != "" {
		secretData["sshPrivateKey"] = sshKey
	} else if token, err := authCfg.GetToken(); err == nil && token != "" {
		secretData["password"] = token
		secretData["username"] = "git" // GitHub uses 'git' as username for token auth
	} else {
		return fmt.Errorf("no valid credentials found for git repository")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gitops-repo-creds",
			Namespace: namespace,
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type": "repository",
			},
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: secretData,
	}

	// Check if secret already exists
	_, err := client.CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if err != nil {
		// Secret doesn't exist, create it
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create Git repository secret: %w", err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Git repository access configured").
			WithResource("argocd").
			WithAction("git-configured").
			WithMetadata("repo_url", cfg.GitRepository.URL))
	} else {
		// Secret exists, update it
		_, err = client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update Git repository secret: %w", err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Git repository access updated").
			WithResource("argocd").
			WithAction("git-updated").
			WithMetadata("repo_url", cfg.GitRepository.URL))
	}

	return nil
}

// ConfigureOCIAccess configures Argo CD to allow unauthenticated access to OCI registries
func ConfigureOCIAccess(ctx context.Context, client *kubernetes.Clientset, namespace string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "argocd.ConfigureOCIAccess")
	defer span.End()

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Configuring OCI registry access for Argo CD").
		WithResource("argocd").
		WithAction("configuring-oci"))

	// Create secret for OCI registry access
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "docker-oci-repo",
			Namespace: namespace,
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type": "repository",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"name":      "docker-oci",
			"type":      "helm",
			"url":       "oci://docker.io",
			"enableOCI": "true",
		},
	}

	// Check if secret already exists
	_, err := client.CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if err != nil {
		// Secret doesn't exist, create it
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create OCI registry secret: %w", err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "OCI registry access configured").
			WithResource("argocd").
			WithAction("oci-configured"))
	} else {
		// Secret exists, update it
		_, err = client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update OCI registry secret: %w", err)
		}
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "OCI registry access updated").
			WithResource("argocd").
			WithAction("oci-updated"))
	}

	return nil
}
