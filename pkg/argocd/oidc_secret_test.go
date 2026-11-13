package argocd

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// storedArgoCDSecret is the keycloak namespace copy of the ArgoCD OIDC client
// secret as an earlier deploy left it.
func storedArgoCDSecret(data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ArgoCDOIDCClientSecretName, Namespace: KeycloakDefaultNamespace},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
}

// installedArgoCDSecret is argocd-secret as the Argo CD Helm chart leaves it.
func installedArgoCDSecret(data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: argoCDSecretName, Namespace: DefaultNamespace},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
}

// failSecrets makes every verb request on secrets in namespace fail with err.
func failSecrets(verb, namespace string, err error) func(*fake.Clientset) {
	return func(client *fake.Clientset) {
		client.PrependReactor(verb, "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return action.GetNamespace() == namespace, nil, err
		})
	}
}

// secretWrites lists the create and update calls made on secrets.
func secretWrites(client *fake.Clientset) []string {
	var writes []string
	for _, a := range client.Actions() {
		if a.GetResource().Resource == "secrets" && (a.GetVerb() == "create" || a.GetVerb() == "update") {
			writes = append(writes, a.GetVerb())
		}
	}
	return writes
}

func TestEnsureArgoCDClientSecret(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", nil)
	unavailable := apierrors.NewServiceUnavailable("etcd is down")
	value := func(s string) map[string][]byte { return map[string][]byte{"client-secret": []byte(s)} }
	argoCD := func(s string) map[string][]byte { return map[string][]byte{"oidc.keycloak.clientSecret": []byte(s)} }

	tests := []struct {
		name       string
		objects    []runtime.Object
		react      func(*fake.Clientset)
		want       string
		wantErr    string
		wantStored map[string]string // keycloak namespace Secret data afterwards; nil: no Secret
		wantWrites []string
	}{
		{
			name:       "first deploy stores the generated secret",
			want:       "generated",
			wantStored: map[string]string{"client-secret": "generated"},
			wantWrites: []string{"create"},
		},
		{
			name: "the stored secret wins over the generated one and Argo CD's copy",
			objects: []runtime.Object{
				storedArgoCDSecret(value("stored")),
				installedArgoCDSecret(argoCD("drifted")),
			},
			want:       "stored",
			wantStored: map[string]string{"client-secret": "stored"},
		},
		{
			name:       "with nothing stored, the value Argo CD is installed with is kept",
			objects:    []runtime.Object{installedArgoCDSecret(argoCD("installed"))},
			want:       "installed",
			wantStored: map[string]string{"client-secret": "installed"},
			wantWrites: []string{"create"},
		},
		{
			name: "an empty stored secret is filled in from Argo CD",
			objects: []runtime.Object{
				storedArgoCDSecret(map[string][]byte{"client-secret": {}, "note": []byte("kept")}),
				installedArgoCDSecret(argoCD("installed")),
			},
			want:       "installed",
			wantStored: map[string]string{"client-secret": "installed", "note": "kept"},
			wantWrites: []string{"update"},
		},
		{
			name:       "an empty stored secret without Argo CD gets the generated one",
			objects:    []runtime.Object{storedArgoCDSecret(nil)},
			want:       "generated",
			wantStored: map[string]string{"client-secret": "generated"},
			wantWrites: []string{"update"},
		},
		{
			name:       "an Argo CD install without an OIDC secret falls back to the generated one",
			objects:    []runtime.Object{installedArgoCDSecret(map[string][]byte{"server.secretkey": []byte("x")})},
			want:       "generated",
			wantStored: map[string]string{"client-secret": "generated"},
			wantWrites: []string{"create"},
		},
		{
			name:    "an error reading the stored secret is returned, not treated as missing",
			react:   failSecrets("get", KeycloakDefaultNamespace, forbidden),
			wantErr: "failed to get secret argocd-oidc-client-secret",
		},
		{
			name:    "an error reading Argo CD's secret is returned, not treated as missing",
			react:   failSecrets("get", DefaultNamespace, unavailable),
			wantErr: "failed to get secret argocd-secret",
		},
		{
			name:    "a failed create is returned",
			react:   failSecrets("create", KeycloakDefaultNamespace, unavailable),
			wantErr: "failed to create secret argocd-oidc-client-secret",
			// The fake records the attempt even though the reactor rejected it.
			wantWrites: []string{"create"},
		},
		{
			name:       "a failed update is returned",
			objects:    []runtime.Object{storedArgoCDSecret(nil)},
			react:      failSecrets("update", KeycloakDefaultNamespace, unavailable),
			wantErr:    "failed to update secret argocd-oidc-client-secret",
			wantStored: map[string]string{},
			wantWrites: []string{"update"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)
			if tt.react != nil {
				tt.react(client)
			}

			got, err := ensureArgoCDClientSecret(context.Background(), client, "generated")

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ensureArgoCDClientSecret() error = %v, want it to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("ensureArgoCDClientSecret() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ensureArgoCDClientSecret() = %q, want %q", got, tt.want)
			}
			if writes := secretWrites(client); strings.Join(writes, ",") != strings.Join(tt.wantWrites, ",") {
				t.Errorf("secret writes = %v, want %v", writes, tt.wantWrites)
			}

			secret, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("secrets"), KeycloakDefaultNamespace, ArgoCDOIDCClientSecretName)
			if tt.wantStored == nil {
				if !apierrors.IsNotFound(err) {
					t.Errorf("stored secret exists, want none (get error: %v)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("get stored secret: %v", err)
			}
			data := secret.(*corev1.Secret).Data
			if len(data) != len(tt.wantStored) {
				t.Errorf("stored data has %d keys, want %d: %v", len(data), len(tt.wantStored), data)
			}
			for key, want := range tt.wantStored {
				if got := string(data[key]); got != want {
					t.Errorf("stored %s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

// TestEnsureArgoCDClientSecretAcrossDeploys runs the lookup the way two
// deploys in a row do: each generates a fresh value, and both must end up with
// the first one, because Keycloak registered the client with it.
func TestEnsureArgoCDClientSecretAcrossDeploys(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()

	first, err := ensureArgoCDClientSecret(ctx, client, "first-deploy")
	if err != nil {
		t.Fatalf("first deploy: %v", err)
	}
	second, err := ensureArgoCDClientSecret(ctx, client, "second-deploy")
	if err != nil {
		t.Fatalf("second deploy: %v", err)
	}
	if first != "first-deploy" || second != "first-deploy" {
		t.Errorf("deploys got %q then %q, want the first deploy's value both times", first, second)
	}

	// The create-only write in createKeycloakSecrets must then leave it alone.
	if err := createKeycloakSecrets(ctx, client, KeycloakConfig{AdminPassword: "pw"}, ArgoCDSSOConfig{ClientSecret: second}); err != nil {
		t.Fatalf("createKeycloakSecrets() error = %v", err)
	}
	secret, err := client.CoreV1().Secrets(KeycloakDefaultNamespace).Get(ctx, ArgoCDOIDCClientSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := getSecretValue(secret, "client-secret"); got != "first-deploy" {
		t.Errorf("stored client-secret = %q, want %q", got, "first-deploy")
	}
}

// TestConfigWithOIDCUsesTheKeyEnsureReads pins the Helm value that carries the
// client secret to the argocd-secret key installedArgoCDClientSecret reads.
func TestConfigWithOIDCUsesTheKeyEnsureReads(t *testing.T) {
	cfg := ConfigWithOIDC("example.com", "", "the-secret")
	extra := cfg.Values["configs"].(map[string]any)["secret"].(map[string]any)["extra"].(map[string]any)
	if got := extra[argoCDOIDCClientSecretKey]; got != "the-secret" {
		t.Errorf("configs.secret.extra[%q] = %v, want %q", argoCDOIDCClientSecretKey, got, "the-secret")
	}
	oidc := cfg.Values["configs"].(map[string]any)["cm"].(map[string]any)["oidc.config"].(string)
	if !strings.Contains(oidc, "$"+argoCDOIDCClientSecretKey) {
		t.Errorf("oidc.config does not reference $%s:\n%s", argoCDOIDCClientSecretKey, oidc)
	}
}
