package cli

import (
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

// stepsText flattens steps for substring checks.
func stepsText(steps []output.Step) string {
	var b strings.Builder
	for _, s := range steps {
		b.WriteString(s.Title + "\n")
		for _, l := range s.Lines {
			b.WriteString(l.Label + " " + l.Value + " " + l.After + l.Note + "\n")
		}
	}
	return b.String()
}

func TestDeploySteps(t *testing.T) {
	loopback := &endpoint.LoadBalancerEndpoint{IP: "127.0.0.1", Port: 8443}
	tests := []struct {
		name       string
		cfg        *config.NebariConfig
		result     nic.DeployResult
		dryRun     bool
		wantTitles []string
		want       []string
		absent     []string
	}{
		{
			name:       "local kind cluster",
			cfg:        &config.NebariConfig{Domain: "nebari.local"},
			result:     nic.DeployResult{ArgoCDInstalled: true, KeycloakInstalled: true, LBEndpoint: loopback},
			wantTitles: []string{"Add this line to /etc/hosts", "Open the services", "Sign in as admin"},
			want: []string{
				"127.0.0.1 nebari.local keycloak.nebari.local argocd.nebari.local",
				"Nebari https://nebari.local:8443",
				"Argo CD https://argocd.nebari.local:8443",
				"Keycloak https://keycloak.nebari.local:8443 ready in a few minutes",
				"password nic outputs --show-secrets",
			},
			// Hosts files map names to addresses: the port belongs in the URLs only.
			absent: []string{"nebari.local:8443 keycloak"},
		},
		{
			name:       "load balancer hostname without a dns provider",
			cfg:        &config.NebariConfig{Domain: "example.com"},
			result:     nic.DeployResult{ArgoCDInstalled: true, KeycloakInstalled: true, LBEndpoint: &endpoint.LoadBalancerEndpoint{Hostname: "lb.example.net"}},
			wantTitles: []string{"Create these DNS records", "Open the services", "Sign in as admin"},
			want: []string{
				"Type CNAME", "Name example.com", "Name *.example.com", "Value lb.example.net",
				"Nebari https://example.com \n", "zone_name: <zone>", "kubectl get pods -n keycloak",
			},
		},
		{
			name:       "load balancer IP without a dns provider",
			cfg:        &config.NebariConfig{Domain: "example.com"},
			result:     nic.DeployResult{ArgoCDInstalled: true, LBEndpoint: &endpoint.LoadBalancerEndpoint{IP: "34.102.136.180"}},
			wantTitles: []string{"Create these DNS records", "Open the services", "Sign in as admin"},
			want:       []string{"Type A", "Value 34.102.136.180"},
			absent:     []string{"CNAME"},
		},
		{
			name:       "load balancer not ready",
			cfg:        &config.NebariConfig{Domain: "example.com"},
			result:     nic.DeployResult{ArgoCDInstalled: true},
			wantTitles: []string{"Point DNS at the load balancer", "Open the services", "Sign in as admin"},
			want:       []string{"nic outputs --wait", "Argo CD https://argocd.example.com"},
			absent:     []string{"nic status", "Keycloak", "Nebari"},
		},
		{
			name:       "a dns provider manages the records",
			cfg:        &config.NebariConfig{Domain: "example.com", DNS: &config.DNSConfig{}},
			result:     nic.DeployResult{ArgoCDInstalled: true, KeycloakInstalled: true},
			wantTitles: []string{"Open the services", "Sign in as admin"},
		},
		{
			name:       "no domain falls back to port-forwarding",
			cfg:        &config.NebariConfig{},
			result:     nic.DeployResult{ArgoCDInstalled: true, KeycloakInstalled: true},
			wantTitles: []string{"Reach the services with port-forwarding", "Sign in as admin"},
			want:       []string{"kubectl port-forward svc/argocd-server -n argocd 8080:443", "kubectl port-forward svc/keycloak -n keycloak 8081:80"},
		},
		{
			name:   "dry run prints nothing",
			cfg:    &config.NebariConfig{Domain: "example.com"},
			dryRun: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := deploySteps(tt.cfg, &tt.result, tt.dryRun)
			var titles []string
			for _, s := range steps {
				titles = append(titles, s.Title)
			}
			if strings.Join(titles, ",") != strings.Join(tt.wantTitles, ",") {
				t.Errorf("steps = %v, want %v", titles, tt.wantTitles)
			}
			text := stepsText(steps)
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("steps should contain %q, got:\n%s", want, text)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(text, absent) {
					t.Errorf("steps should NOT contain %q, got:\n%s", absent, text)
				}
			}
		})
	}
}

func TestPlatformURL(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *config.NebariConfig
		result nic.DeployResult
		dryRun bool
		want   string
	}{
		{name: "standard port", cfg: &config.NebariConfig{Domain: "example.com"}, want: "https://example.com"},
		{name: "host port", cfg: &config.NebariConfig{Domain: "nebari.local"}, result: nic.DeployResult{LBEndpoint: &endpoint.LoadBalancerEndpoint{IP: "127.0.0.1", Port: 8443}}, want: "https://nebari.local:8443"},
		{name: "no domain", cfg: &config.NebariConfig{}},
		{name: "dry run", cfg: &config.NebariConfig{Domain: "example.com"}, dryRun: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := platformURL(tt.cfg, &tt.result, tt.dryRun); got != tt.want {
				t.Errorf("platformURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
