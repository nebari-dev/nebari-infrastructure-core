package nic

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// TestExampleConfigsValidate parses and validates every config under examples/.
//
// These files are the documented starting point for users, so a schema change
// that leaves them behind is a user-visible break. Nothing else in the tree
// exercises them: when the provider settings moved from top-level keys
// (`hetzner_cloud:`) to the nested `cluster:` block, the examples were updated
// by hand and there was no test to confirm it.
//
// This covers config-level validation only, which is the same depth as
// `nic validate`. Provider-level validation (for example
// cluster.hetzner.location is required) is not reached by Client.Validate, so a
// green result here does not mean an example would deploy.
func TestExampleConfigsValidate(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.yaml"))
	if err != nil {
		t.Fatalf("glob examples: %v", err)
	}
	// Guard against the glob silently matching nothing if examples/ is moved or
	// renamed, which would leave this test passing while covering zero files.
	if len(paths) == 0 {
		t.Fatal("no example configs found under examples/; update this test if the directory moved")
	}

	ctx := context.Background()
	client, err := NewClient(ctx)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := config.ParseConfig(ctx, path)
			if err != nil {
				t.Fatalf("ParseConfig(%s) error = %v", path, err)
			}
			if err := client.Validate(ctx, cfg); err != nil {
				t.Errorf("Validate(%s) error = %v", path, err)
			}
		})
	}
}
