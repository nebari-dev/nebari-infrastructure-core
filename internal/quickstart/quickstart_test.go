package quickstart

import (
	"testing"

	"github.com/goccy/go-yaml"
)

func TestOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{name: "defaults", opts: Options{}},
		{name: "custom ports", opts: Options{HTTPPort: 8080, HTTPSPort: 8443}},
		{name: "only https set", opts: Options{HTTPSPort: 8443}},
		{name: "lowest and highest ports", opts: Options{HTTPPort: 1, HTTPSPort: 65535}},
		{name: "negative http port", opts: Options{HTTPPort: -5}, wantErr: "--http-port must be between 1 and 65535, got -5"},
		{name: "https port too high", opts: Options{HTTPSPort: 99999}, wantErr: "--https-port must be between 1 and 65535, got 99999"},
		{name: "equal ports", opts: Options{HTTPPort: 8443, HTTPSPort: 8443}, wantErr: "--http-port and --https-port must differ, both are 8443"},
		{name: "http port equals default https", opts: Options{HTTPPort: 443}, wantErr: "--http-port and --https-port must differ, both are 443"},
		{name: "https port equals default http", opts: Options{HTTPSPort: 80}, wantErr: "--http-port and --https-port must differ, both are 80"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
			if _, err := RenderConfig(tt.opts); err == nil {
				t.Errorf("RenderConfig() rendered a config for invalid options")
			}
		})
	}
}

// TestRenderPacks_RetryIndefinitely verifies every pack Application retries
// failed syncs without a limit and on the latest revision, like the
// foundational apps (see pkg/argocd/retry_test.go). A pack can lose the same
// races as a foundational app, for example with the trust bundle, and with a
// fixed limit it would stay broken until someone syncs it by hand.
// See https://github.com/nebari-dev/nebari-infrastructure-core/issues/450
func TestRenderPacks_RetryIndefinitely(t *testing.T) {
	packs, err := RenderPacks(DefaultDomain)
	if err != nil {
		t.Fatalf("RenderPacks() error: %v", err)
	}
	if len(packs) == 0 {
		t.Fatal("RenderPacks() returned no packs")
	}

	for name, content := range packs {
		t.Run(name, func(t *testing.T) {
			var app struct {
				Kind string `yaml:"kind"`
				Spec struct {
					SyncPolicy struct {
						Retry *struct {
							Limit   *int64 `yaml:"limit"`
							Refresh bool   `yaml:"refresh"`
						} `yaml:"retry"`
					} `yaml:"syncPolicy"`
				} `yaml:"spec"`
			}
			if err := yaml.Unmarshal(content, &app); err != nil {
				t.Fatalf("rendered pack is not valid YAML: %v\n%s", err, content)
			}
			if app.Kind != "Application" {
				t.Fatalf("kind = %q, want Application", app.Kind)
			}
			retry := app.Spec.SyncPolicy.Retry
			if retry == nil || retry.Limit == nil {
				t.Fatalf("spec.syncPolicy.retry.limit is not set")
			}
			if *retry.Limit >= 0 {
				t.Errorf("spec.syncPolicy.retry.limit = %d, want a negative value (retry indefinitely)", *retry.Limit)
			}
			if !retry.Refresh {
				t.Errorf("spec.syncPolicy.retry.refresh is not true, so retries keep applying the failed revision")
			}
		})
	}
}
