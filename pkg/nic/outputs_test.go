package nic

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// testTemplateData builds the same TemplateData the deploy path renders
// manifests from, so the resolver and the manifests cannot disagree about
// secret names, keys, or the issuer URL formula.
func testTemplateData(domain string) argocd.TemplateData {
	return argocd.NewTemplateData(
		&config.NebariConfig{Domain: domain},
		nil,
		cluster.InfraSettings{},
	)
}

// secretObj populates Data only. The fake clientset does not perform the
// StringData-to-Data conversion the API server does, so setting StringData as
// well would suggest the resolver reads a field it never sees.
func secretObj(namespace, name string, data map[string]string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       toByteMap(data),
	}
}

func toByteMap(in map[string]string) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		out[k] = []byte(v)
	}
	return out
}

func keycloakAdminSecret() *corev1.Secret {
	return secretObj(argocd.KeycloakDefaultNamespace, argocd.KeycloakDefaultAdminSecretName,
		map[string]string{argocd.KeycloakAdminPasswordKey: "kc-admin-pw"})
}

func realmAdminSecret() *corev1.Secret {
	return secretObj(argocd.KeycloakDefaultNamespace, argocd.NebariRealmAdminSecretName,
		map[string]string{argocd.NebariRealmAdminPasswordKey: "realm-admin-pw"})
}

func argoCDAdminSecret() *corev1.Secret {
	return secretObj(argocd.DefaultNamespace, argocd.ArgoCDInitialAdminSecretName,
		map[string]string{argocd.ArgoCDInitialAdminSecretKey: "argocd-admin-pw"})
}

func gatewayService(ingress ...corev1.LoadBalancerIngress) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "envoy-nebari-gateway",
			Namespace: endpoint.DefaultNamespace,
			Labels: map[string]string{
				"gateway.envoyproxy.io/owning-gateway-name": "nebari-gateway",
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: ingress},
		},
	}
}

func fullyDeployed() []runtime.Object {
	return []runtime.Object{
		keycloakAdminSecret(),
		realmAdminSecret(),
		argoCDAdminSecret(),
		gatewayService(corev1.LoadBalancerIngress{IP: "10.89.0.2"}),
	}
}

// without returns fullyDeployed minus the object at index i, so a case can
// describe exactly one missing piece of the platform.
func without(objs []runtime.Object, i int) []runtime.Object {
	out := make([]runtime.Object, 0, len(objs)-1)
	for j, o := range objs {
		if j != i {
			out = append(out, o)
		}
	}
	return out
}

func TestResolveOutputs(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		objects []runtime.Object
		want    *PlatformOutputs
		// errContains are substrings that must all appear in the error.
		errContains []string
	}{
		{
			name:    "resolves every field on a fully deployed platform",
			domain:  "nebari.example.com",
			objects: fullyDeployed(),
			want: &PlatformOutputs{
				Domain:                     "nebari.example.com",
				KeycloakIssuerURL:          "https://keycloak.nebari.example.com",
				KeycloakAdminPassword:      "kc-admin-pw",
				KeycloakRealmAdminPassword: "realm-admin-pw",
				ArgoCDAdminPassword:        "argocd-admin-pw",
				GatewayAddress:             "10.89.0.2",
			},
		},
		{
			name:    "prefers the LoadBalancer hostname over its IP",
			domain:  "nebari.example.com",
			objects: append(without(fullyDeployed(), 3), gatewayService(corev1.LoadBalancerIngress{Hostname: "abc.elb.amazonaws.com", IP: "10.0.0.1"})),
			want: &PlatformOutputs{
				Domain:                     "nebari.example.com",
				KeycloakIssuerURL:          "https://keycloak.nebari.example.com",
				KeycloakAdminPassword:      "kc-admin-pw",
				KeycloakRealmAdminPassword: "realm-admin-pw",
				ArgoCDAdminPassword:        "argocd-admin-pw",
				GatewayAddress:             "abc.elb.amazonaws.com",
			},
		},
		{
			name:        "errors when the keycloak admin secret is absent",
			domain:      "nebari.example.com",
			objects:     without(fullyDeployed(), 0),
			errContains: []string{"keycloak_admin_password", argocd.KeycloakDefaultAdminSecretName, "not found"},
		},
		{
			name:        "errors when the realm admin secret is absent",
			domain:      "nebari.example.com",
			objects:     without(fullyDeployed(), 1),
			errContains: []string{"keycloak_realm_admin_password", argocd.NebariRealmAdminSecretName},
		},
		{
			name:        "errors when the argocd admin secret is absent",
			domain:      "nebari.example.com",
			objects:     without(fullyDeployed(), 2),
			errContains: []string{"argocd_admin_password", argocd.ArgoCDInitialAdminSecretName},
		},
		{
			// The silent-staleness case a renamed key produces downstream: the
			// secret exists, so kubectl exits 0 and jsonpath yields "".
			name:   "errors when the secret exists but the expected key does not",
			domain: "nebari.example.com",
			objects: append(without(fullyDeployed(), 0),
				secretObj(argocd.KeycloakDefaultNamespace, argocd.KeycloakDefaultAdminSecretName,
					map[string]string{"adminPassword": "renamed"})),
			errContains: []string{"keycloak_admin_password", "key", argocd.KeycloakAdminPasswordKey},
		},
		{
			name:        "errors when the gateway has no LoadBalancer ingress yet",
			domain:      "nebari.example.com",
			objects:     append(without(fullyDeployed(), 3), gatewayService()),
			errContains: []string{"gateway_address", "load balancer not ready"},
		},
		{
			name:        "names every unresolved field in a single error",
			domain:      "nebari.example.com",
			objects:     nil,
			errContains: []string{"keycloak_admin_password", "keycloak_realm_admin_password", "argocd_admin_password", "gateway_address"},
		},
		{
			name:        "errors when no domain is configured so the issuer URL cannot be derived",
			domain:      "",
			objects:     fullyDeployed(),
			errContains: []string{"keycloak_issuer_url"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.objects...)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			got, err := resolveOutputs(ctx, client, testTemplateData(tt.domain), OutputsOptions{})

			if len(tt.errContains) > 0 {
				if err == nil {
					t.Fatalf("expected an error, got outputs %+v", got)
				}
				for _, want := range tt.errContains {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err.Error(), want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *got != *tt.want {
				t.Errorf("outputs =\n%+v\nwant\n%+v", *got, *tt.want)
			}
		})
	}
}

// TestResolveOutputsHostPortGateway pins that a host-port gateway (local kind
// clusters) reports 127.0.0.1 without inspecting any LoadBalancer status: a
// NodePort service never gets ingress entries, so the LB path would fail (and
// --wait would poll to timeout) on a healthy local cluster.
func TestResolveOutputsHostPortGateway(t *testing.T) {
	// No gateway service object at all: the address must not come from the cluster.
	client := fake.NewSimpleClientset(without(fullyDeployed(), 3)...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data := argocd.NewTemplateData(
		&config.NebariConfig{Domain: "nebari.local"},
		nil,
		cluster.InfraSettings{GatewayHostAddress: "127.0.0.1"},
	)

	got, err := resolveOutputs(ctx, client, data, OutputsOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GatewayAddress != "127.0.0.1" {
		t.Errorf("gateway address = %q, want 127.0.0.1", got.GatewayAddress)
	}
}

func TestResolveOutputsWaitsForAsyncFields(t *testing.T) {
	// The Argo CD server writes argocd-initial-admin-secret itself, some
	// seconds after the Helm release lands; the gateway address arrives when
	// the cloud LB is provisioned. Both are absent at first here.
	client := fake.NewSimpleClientset(keycloakAdminSecret(), realmAdminSecret(), gatewayService())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = client.CoreV1().Secrets(argocd.DefaultNamespace).Create(ctx, argoCDAdminSecret(), metav1.CreateOptions{})
		svc := gatewayService(corev1.LoadBalancerIngress{IP: "10.89.0.2"})
		_, _ = client.CoreV1().Services(endpoint.DefaultNamespace).Update(ctx, svc, metav1.UpdateOptions{})
	}()

	got, err := resolveOutputs(ctx, client, testTemplateData("nebari.example.com"), OutputsOptions{
		Wait:         true,
		Timeout:      5 * time.Second,
		PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ArgoCDAdminPassword != "argocd-admin-pw" {
		t.Errorf("argocd password = %q, want %q", got.ArgoCDAdminPassword, "argocd-admin-pw")
	}
	if got.GatewayAddress != "10.89.0.2" {
		t.Errorf("gateway address = %q, want %q", got.GatewayAddress, "10.89.0.2")
	}
}

// A missing domain is a configuration error, not a condition the cluster will
// converge on. Polling it for the full timeout would report a config mistake as
// a deployment that never came up.
func TestResolveOutputsDoesNotWaitOnConfigurationErrors(t *testing.T) {
	client := fake.NewSimpleClientset(fullyDeployed()...)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	timeout := 3 * time.Second
	start := time.Now()
	_, err := resolveOutputs(ctx, client, testTemplateData(""), OutputsOptions{
		Wait:         true,
		Timeout:      timeout,
		PollInterval: 10 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "keycloak_issuer_url") {
		t.Errorf("error %q does not name keycloak_issuer_url", err.Error())
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Errorf("configuration error reported as a timeout: %q", err.Error())
	}
	if elapsed > timeout/2 {
		t.Errorf("waited %s before failing; expected to fail immediately", elapsed)
	}
}

func TestResolveOutputsWaitTimesOut(t *testing.T) {
	client := fake.NewSimpleClientset(keycloakAdminSecret(), realmAdminSecret())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := resolveOutputs(ctx, client, testTemplateData("nebari.example.com"), OutputsOptions{
		Wait:         true,
		Timeout:      60 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	for _, want := range []string{"timed out", "argocd_admin_password", "gateway_address"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err.Error(), want)
		}
	}
}

// slowSecretReads makes every secret Get take delay, so a test can observe
// whether the polling deadline interrupts work already under way or is only
// consulted between polls.
func slowSecretReads(client *fake.Clientset, delay time.Duration) {
	client.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		time.Sleep(delay)
		return false, nil, nil
	})
}

func TestResolveOutputsTimeoutInterruptsWorkInFlight(t *testing.T) {
	const readDelay = 80 * time.Millisecond

	client := fake.NewSimpleClientset(keycloakAdminSecret(), realmAdminSecret())
	slowSecretReads(client, readDelay)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	_, err := resolveOutputs(ctx, client, testTemplateData("nebari.example.com"), OutputsOptions{
		Wait:         true,
		Timeout:      30 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error %q does not report a timeout", err.Error())
	}

	// One read may already be under way when the deadline passes, so allow a
	// single delay plus scheduling slack. Consulting the deadline only between
	// polls would let a full pass of three reads complete first.
	if limit := 2 * readDelay; elapsed > limit {
		t.Errorf("resolveOutputs took %s, want under %s: the deadline did not stop reads already in flight",
			elapsed, limit)
	}
}

func TestResolveOutputsReportsCallerCancellationDistinctly(t *testing.T) {
	client := fake.NewSimpleClientset(keycloakAdminSecret(), realmAdminSecret())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := resolveOutputs(ctx, client, testTemplateData("nebari.example.com"), OutputsOptions{
		Wait:         true,
		Timeout:      10 * time.Second,
		PollInterval: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("error %q does not report caller cancellation", err.Error())
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Errorf("error %q reports a timeout for a cancelled caller", err.Error())
	}
}

func TestRestConfigFromKubeconfig(t *testing.T) {
	const validKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`

	tests := []struct {
		name        string
		kubeconfig  string
		wantErr     bool
		wantTimeout time.Duration
	}{
		{
			name:        "valid kubeconfig bounds every request",
			kubeconfig:  validKubeconfig,
			wantTimeout: apiRequestTimeout,
		},
		{
			name:       "malformed kubeconfig",
			kubeconfig: "\tnot: [valid",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := restConfigFromKubeconfig(context.Background(), []byte(tt.kubeconfig))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Timeout != tt.wantTimeout {
				t.Errorf("Timeout = %s, want %s: an unbounded request can outlast --timeout",
					got.Timeout, tt.wantTimeout)
			}
		})
	}
}
