package nic

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// TestExampleConfigsValidate parses and validates every config under
// examples/ and every CI deployment-test fixture under
// .github/fixtures/deploy/.
//
// The examples are the documented starting point for users, so a schema
// change that leaves them behind is a user-visible break. Nothing else in the
// tree exercises them: when the provider settings moved from top-level keys
// (`hetzner_cloud:`) to the nested `cluster:` block, the examples were updated
// by hand and there was no test to confirm it. The fixtures have the same rot
// exposure with worse economics: without this test, a fixture broken by a
// schema change only fails when a cloud deployment test dispatches that
// provider.
//
// This covers config-level validation only, which is the same depth as
// `nic validate`. Provider-level validation (for example
// cluster.hetzner.location is required) is not reached by Client.Validate, so a
// green result here does not mean an example would deploy.
func TestExampleConfigsValidate(t *testing.T) {
	globs := map[string]string{
		"examples":                filepath.Join("..", "..", "examples", "*.yaml"),
		".github/fixtures/deploy": filepath.Join("..", "..", ".github", "fixtures", "deploy", "*.yaml"),
	}
	var paths []string
	for dir, pattern := range globs {
		matched, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		// Guard against a glob silently matching nothing if a directory is
		// moved or renamed, which would leave this test passing while covering
		// zero files.
		if len(matched) == 0 {
			t.Fatalf("no configs found under %s; update this test if the directory moved", dir)
		}
		paths = append(paths, matched...)
	}

	ctx := context.Background()
	client, err := NewClient(ctx)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	for _, path := range paths {
		// Include the parent directory: examples and fixtures share basenames
		// (aws-config.yaml exists in both), and a bare basename would report
		// failures ambiguously.
		name := filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path))
		t.Run(name, func(t *testing.T) {
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
