package quickstart

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

// TestRenderConfig renders the quickstart config for each port combination,
// parses it the way `nic deploy` does and validates it, so a template change
// that produces a broken config fails here instead of on a user's machine.
func TestRenderConfig(t *testing.T) {
	tests := []struct {
		name          string
		opts          Options
		wantHTTPPort  any
		wantHTTPSPort any
	}{
		{name: "default ports", opts: Options{Domain: DefaultDomain}},
		{name: "http port only", opts: Options{Domain: DefaultDomain, HTTPPort: 8080}, wantHTTPPort: uint64(8080)},
		{name: "https port only", opts: Options{Domain: DefaultDomain, HTTPSPort: 8443}, wantHTTPSPort: uint64(8443)},
		{name: "both ports", opts: Options{Domain: DefaultDomain, HTTPPort: 8080, HTTPSPort: 8443}, wantHTTPPort: uint64(8080), wantHTTPSPort: uint64(8443)},
		{name: "custom domain", opts: Options{Domain: "nebari.example.test"}},
	}

	ctx := context.Background()
	client, err := nic.NewClient(ctx)
	if err != nil {
		t.Fatalf("nic.NewClient() error: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := RenderConfig(tt.opts)
			if err != nil {
				t.Fatalf("RenderConfig() error: %v", err)
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}

			cfg, err := config.ParseConfig(ctx, path)
			if err != nil {
				t.Fatalf("config.ParseConfig() error: %v\n%s", err, content)
			}
			if err := client.Validate(ctx, cfg); err != nil {
				t.Fatalf("Validate() error: %v\n%s", err, content)
			}

			if cfg.ProjectName != ProjectName {
				t.Errorf("project_name = %q, want %q", cfg.ProjectName, ProjectName)
			}
			if cfg.Domain != tt.opts.Domain {
				t.Errorf("domain = %q, want %q", cfg.Domain, tt.opts.Domain)
			}
			if got := cfg.Cluster.ProviderName(); got != "local" {
				t.Errorf("cluster provider = %q, want local", got)
			}
			if got := cfg.Repository.ProviderName(); got != "local" {
				t.Errorf("repository provider = %q, want local", got)
			}
			if cfg.Certificate == nil || cfg.Certificate.Type != "selfsigned" {
				t.Errorf("certificate = %+v, want type selfsigned", cfg.Certificate)
			}

			local := cfg.Cluster.ProviderConfig()
			if got := local["http_port"]; got != tt.wantHTTPPort {
				t.Errorf("cluster.local.http_port = %v (%T), want %v", got, got, tt.wantHTTPPort)
			}
			if got := local["https_port"]; got != tt.wantHTTPSPort {
				t.Errorf("cluster.local.https_port = %v (%T), want %v", got, got, tt.wantHTTPSPort)
			}
		})
	}
}
