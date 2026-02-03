package argocd

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// getSecretValue retrieves a value from a secret, checking both Data and StringData
// The fake client doesn't convert StringData to Data like the real API server does
func getSecretValue(secret *corev1.Secret, key string) string {
	if val, ok := secret.Data[key]; ok {
		return string(val)
	}
	if val, ok := secret.StringData[key]; ok {
		return val
	}
	return ""
}

func TestCreateNamespace(t *testing.T) {
	ctx := context.Background()

	t.Run("creates new namespace", func(t *testing.T) {
		client := fake.NewSimpleClientset()

		err := createNamespace(ctx, client, "test-namespace")
		if err != nil {
			t.Fatalf("createNamespace() error = %v", err)
		}

		// Verify namespace was created
		ns, err := client.CoreV1().Namespaces().Get(ctx, "test-namespace", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get namespace: %v", err)
		}
		if ns.Name != "test-namespace" {
			t.Errorf("namespace name = %q, want %q", ns.Name, "test-namespace")
		}
	})

	t.Run("succeeds if namespace already exists", func(t *testing.T) {
		// Pre-create the namespace
		existingNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "existing-namespace",
			},
		}
		client := fake.NewSimpleClientset(existingNS)

		err := createNamespace(ctx, client, "existing-namespace")
		if err != nil {
			t.Fatalf("createNamespace() should succeed for existing namespace, got error = %v", err)
		}
	})
}

func TestCreateKeycloakSecrets(t *testing.T) {
	ctx := context.Background()

	t.Run("creates all secrets", func(t *testing.T) {
		// Create namespace first
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "keycloak",
			},
		}
		client := fake.NewSimpleClientset(ns)

		cfg := KeycloakConfig{
			Enabled:       true,
			AdminPassword: "admin-pass-123",
			DBPassword:    "db-pass-456",
		}

		err := createKeycloakSecrets(ctx, client, cfg)
		if err != nil {
			t.Fatalf("createKeycloakSecrets() error = %v", err)
		}

		// Verify admin credentials secret
		adminSecret, err := client.CoreV1().Secrets("keycloak").Get(ctx, "keycloak-admin-credentials", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get admin secret: %v", err)
		}
		if got := getSecretValue(adminSecret, "admin-password"); got != "admin-pass-123" {
			t.Errorf("admin password = %q, want %q", got, "admin-pass-123")
		}

		// Verify keycloak PostgreSQL credentials secret
		keycloakDBSecret, err := client.CoreV1().Secrets("keycloak").Get(ctx, "keycloak-postgresql-credentials", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get keycloak DB secret: %v", err)
		}
		if got := getSecretValue(keycloakDBSecret, "password"); got != "db-pass-456" {
			t.Errorf("keycloak DB password = %q, want %q", got, "db-pass-456")
		}

		// Verify PostgreSQL main credentials secret
		postgresSecret, err := client.CoreV1().Secrets("keycloak").Get(ctx, "postgresql-credentials", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get PostgreSQL secret: %v", err)
		}
		if got := getSecretValue(postgresSecret, "postgres-password"); got != "db-pass-456-admin" {
			t.Errorf("postgres password = %q, want %q", got, "db-pass-456-admin")
		}
	})

	t.Run("creates realm admin secret when password provided", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "keycloak",
			},
		}
		client := fake.NewSimpleClientset(ns)

		cfg := KeycloakConfig{
			Enabled:            true,
			AdminPassword:      "admin-pass",
			DBPassword:         "db-pass",
			RealmAdminPassword: "realm-admin-pass",
		}

		err := createKeycloakSecrets(ctx, client, cfg)
		if err != nil {
			t.Fatalf("createKeycloakSecrets() error = %v", err)
		}

		// Verify realm admin credentials secret was created
		realmAdminSecret, err := client.CoreV1().Secrets("keycloak").Get(ctx, "nebari-realm-admin-credentials", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get realm admin secret: %v", err)
		}
		if got := getSecretValue(realmAdminSecret, "username"); got != "admin" {
			t.Errorf("realm admin username = %q, want %q", got, "admin")
		}
		if got := getSecretValue(realmAdminSecret, "password"); got != "realm-admin-pass" {
			t.Errorf("realm admin password = %q, want %q", got, "realm-admin-pass")
		}
		// Verify labels
		if realmAdminSecret.Labels["app.kubernetes.io/part-of"] != "nebari-foundational" {
			t.Errorf("missing or incorrect part-of label")
		}
	})

	t.Run("skips realm admin secret when password empty", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "keycloak",
			},
		}
		client := fake.NewSimpleClientset(ns)

		cfg := KeycloakConfig{
			Enabled:            true,
			AdminPassword:      "admin-pass",
			DBPassword:         "db-pass",
			RealmAdminPassword: "", // Empty - should not create secret
		}

		err := createKeycloakSecrets(ctx, client, cfg)
		if err != nil {
			t.Fatalf("createKeycloakSecrets() error = %v", err)
		}

		// Verify realm admin secret was NOT created
		_, err = client.CoreV1().Secrets("keycloak").Get(ctx, "nebari-realm-admin-credentials", metav1.GetOptions{})
		if err == nil {
			t.Error("realm admin secret should not be created when password is empty")
		}
	})

	t.Run("does not overwrite existing secrets", func(t *testing.T) {
		// Pre-create namespace and secrets
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "keycloak",
			},
		}
		existingSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "keycloak-admin-credentials",
				Namespace: "keycloak",
			},
			Data: map[string][]byte{
				"admin-password": []byte("existing-password"),
			},
		}
		client := fake.NewSimpleClientset(ns, existingSecret)

		cfg := KeycloakConfig{
			Enabled:       true,
			AdminPassword: "new-password",
			DBPassword:    "db-pass",
		}

		err := createKeycloakSecrets(ctx, client, cfg)
		if err != nil {
			t.Fatalf("createKeycloakSecrets() error = %v", err)
		}

		// Verify existing secret was NOT overwritten
		secret, err := client.CoreV1().Secrets("keycloak").Get(ctx, "keycloak-admin-credentials", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get secret: %v", err)
		}
		if got := getSecretValue(secret, "admin-password"); got != "existing-password" {
			t.Errorf("secret should not be overwritten, got %q, want %q", got, "existing-password")
		}
	})
}

func TestFoundationalConfig(t *testing.T) {
	t.Run("KeycloakConfig defaults", func(t *testing.T) {
		cfg := KeycloakConfig{}
		if cfg.Enabled {
			t.Error("KeycloakConfig.Enabled should default to false")
		}
		if cfg.AdminPassword != "" {
			t.Error("KeycloakConfig.AdminPassword should default to empty")
		}
		if cfg.RealmAdminPassword != "" {
			t.Error("KeycloakConfig.RealmAdminPassword should default to empty")
		}
	})

	t.Run("MetalLBConfig defaults", func(t *testing.T) {
		cfg := MetalLBConfig{}
		if cfg.Enabled {
			t.Error("MetalLBConfig.Enabled should default to false")
		}
		if cfg.AddressPool != "" {
			t.Error("MetalLBConfig.AddressPool should default to empty")
		}
	})

	t.Run("FoundationalConfig with values", func(t *testing.T) {
		cfg := FoundationalConfig{
			Keycloak: KeycloakConfig{
				Enabled:            true,
				AdminPassword:      "admin123",
				DBPassword:         "db123",
				Hostname:           "keycloak.example.com",
				RealmAdminPassword: "realm-admin123",
			},
			MetalLB: MetalLBConfig{
				Enabled:     true,
				AddressPool: "192.168.1.100-192.168.1.110",
			},
		}

		if !cfg.Keycloak.Enabled {
			t.Error("Keycloak.Enabled should be true")
		}
		if cfg.Keycloak.AdminPassword != "admin123" {
			t.Errorf("Keycloak.AdminPassword = %q, want %q", cfg.Keycloak.AdminPassword, "admin123")
		}
		if cfg.Keycloak.Hostname != "keycloak.example.com" {
			t.Errorf("Keycloak.Hostname = %q, want %q", cfg.Keycloak.Hostname, "keycloak.example.com")
		}
		if cfg.Keycloak.RealmAdminPassword != "realm-admin123" {
			t.Errorf("Keycloak.RealmAdminPassword = %q, want %q", cfg.Keycloak.RealmAdminPassword, "realm-admin123")
		}
		if !cfg.MetalLB.Enabled {
			t.Error("MetalLB.Enabled should be true")
		}
		if cfg.MetalLB.AddressPool != "192.168.1.100-192.168.1.110" {
			t.Errorf("MetalLB.AddressPool = %q, want %q", cfg.MetalLB.AddressPool, "192.168.1.100-192.168.1.110")
		}
	})
}

func TestNewK8sClient(t *testing.T) {
	t.Run("fails with invalid kubeconfig", func(t *testing.T) {
		_, err := newK8sClient([]byte("invalid kubeconfig"))
		if err == nil {
			t.Error("newK8sClient() should fail with invalid kubeconfig")
		}
	})

	t.Run("fails with empty kubeconfig", func(t *testing.T) {
		_, err := newK8sClient([]byte{})
		if err == nil {
			t.Error("newK8sClient() should fail with empty kubeconfig")
		}
	})
}
