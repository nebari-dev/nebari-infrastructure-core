package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

// sectionText flattens a section for substring checks.
func sectionText(s output.Section) string {
	return s.Title + "\n" + strings.Join(s.Body, "\n")
}

func TestDNSSection(t *testing.T) {
	tests := []struct {
		name         string
		domain       string
		endpoint     *endpoint.LoadBalancerEndpoint
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "with hostname endpoint shows CNAME record",
			domain:       "example.com",
			endpoint:     &endpoint.LoadBalancerEndpoint{Hostname: "abc123.us-west-2.elb.amazonaws.com"},
			wantContains: []string{"CNAME", "abc123.us-west-2.elb.amazonaws.com", "example.com", "*.example.com"},
			wantAbsent:   []string{"ingress-nginx", "kubectl", "<load-balancer-endpoint>"},
		},
		{
			name:         "with IP endpoint shows A record",
			domain:       "example.com",
			endpoint:     &endpoint.LoadBalancerEndpoint{IP: "34.102.136.180"},
			wantContains: []string{"A", "34.102.136.180", "example.com"},
			wantAbsent:   []string{"ingress-nginx", "kubectl"},
		},
		{
			name:     "with loopback endpoint shows hosts file guidance",
			domain:   "nebari.local",
			endpoint: &endpoint.LoadBalancerEndpoint{IP: "127.0.0.1"},
			wantContains: []string{
				"HOSTS FILE CONFIGURATION REQUIRED",
				"127.0.0.1 nebari.local keycloak.nebari.local argocd.nebari.local",
				"open https://nebari.local in your browser",
			},
			wantAbsent: []string{"DNS CONFIGURATION REQUIRED", "nebari.local:443"},
		},
		{
			name:     "with loopback endpoint and custom port the URL carries the port",
			domain:   "nebari.local",
			endpoint: &endpoint.LoadBalancerEndpoint{IP: "127.0.0.1", Port: 8443},
			wantContains: []string{
				"127.0.0.1 nebari.local keycloak.nebari.local argocd.nebari.local",
				"open https://nebari.local:8443 in your browser",
			},
			wantAbsent: []string{"nebari.local:8443 keycloak"},
		},
		{
			name:         "with nil endpoint shows fallback instructions",
			domain:       "example.com",
			wantContains: []string{"example.com", "nic status"},
			wantAbsent:   []string{"ingress-nginx"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sectionText(dnsSection(&config.NebariConfig{Domain: tt.domain}, tt.endpoint))
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("section should contain %q, got:\n%s", want, got)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("section should NOT contain %q, got:\n%s", absent, got)
				}
			}
		})
	}
}

// TestGuidanceJSONBanners pins the JSON format's post-deploy banners
// (testdata/guidance/*.golden). The goldens were recorded from the print
// functions this guidance replaced, so the banners read exactly as before,
// with one deliberate change: dns-hostname and dns-ip list each DNS record as
// Type/Name/Value lines instead of a fixed-width table that a load balancer
// hostname overflowed.
func TestGuidanceJSONBanners(t *testing.T) {
	example := &config.NebariConfig{Domain: "example.com"}
	local := &config.NebariConfig{Domain: "nebari.local"}
	noDomain := &config.NebariConfig{}

	tests := []struct {
		golden  string
		section output.Section
	}{
		{"dns-hostname", dnsSection(example, &endpoint.LoadBalancerEndpoint{Hostname: "abc123.us-west-2.elb.amazonaws.com"})},
		{"dns-ip", dnsSection(example, &endpoint.LoadBalancerEndpoint{IP: "34.102.136.180"})},
		{"dns-pending", dnsSection(example, nil)},
		{"hosts-443", dnsSection(local, &endpoint.LoadBalancerEndpoint{IP: "127.0.0.1", Port: 443})},
		{"hosts-8443", dnsSection(local, &endpoint.LoadBalancerEndpoint{IP: "127.0.0.1", Port: 8443})},
		{"argocd-domain", argoCDSection(example)},
		{"argocd-none", argoCDSection(noDomain)},
		{"keycloak-domain", keycloakSection(example)},
		{"keycloak-none", keycloakSection(noDomain)},
	}

	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "guidance", tt.golden+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			output.WriteBanners(&got, []output.Section{tt.section})
			if got.String() != string(want) {
				t.Errorf("banner differs from %s.golden\n--- got\n%s\n--- want\n%s", tt.golden, got.String(), want)
			}
		})
	}
}

func TestDeployGuidance(t *testing.T) {
	lb := &endpoint.LoadBalancerEndpoint{Hostname: "lb.example.com"}
	tests := []struct {
		name   string
		cfg    *config.NebariConfig
		result nic.DeployResult
		dryRun bool
		want   []string
	}{
		{
			name:   "full deploy without a dns provider",
			cfg:    &config.NebariConfig{Domain: "example.com"},
			result: nic.DeployResult{ArgoCDInstalled: true, KeycloakInstalled: true, LBEndpoint: lb},
			want:   []string{"ARGO CD INSTALLED", "KEYCLOAK INSTALLED", "DNS CONFIGURATION REQUIRED"},
		},
		{
			name:   "a dns provider manages the records",
			cfg:    &config.NebariConfig{Domain: "example.com", DNS: &config.DNSConfig{}},
			result: nic.DeployResult{ArgoCDInstalled: true, KeycloakInstalled: true, LBEndpoint: lb},
			want:   []string{"ARGO CD INSTALLED", "KEYCLOAK INSTALLED"},
		},
		{
			name:   "no domain, no dns guidance",
			cfg:    &config.NebariConfig{},
			result: nic.DeployResult{ArgoCDInstalled: true},
			want:   []string{"ARGO CD INSTALLED"},
		},
		{
			name:   "dry run prints nothing",
			cfg:    &config.NebariConfig{Domain: "example.com"},
			dryRun: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, s := range deployGuidance(tt.cfg, &tt.result, tt.dryRun) {
				got = append(got, s.Title)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("sections = %v, want %v", got, tt.want)
			}
		})
	}
}

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
		},
		{
			name:       "load balancer hostname without a dns provider",
			cfg:        &config.NebariConfig{Domain: "example.com"},
			result:     nic.DeployResult{ArgoCDInstalled: true, KeycloakInstalled: true, LBEndpoint: &endpoint.LoadBalancerEndpoint{Hostname: "lb.example.net"}},
			wantTitles: []string{"Create these DNS records", "Open the services", "Sign in as admin"},
			want:       []string{"Type CNAME", "Name example.com", "Name *.example.com", "Value lb.example.net", "Nebari https://example.com \n"},
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
