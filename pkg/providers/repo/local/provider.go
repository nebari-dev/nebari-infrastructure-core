// Package local implements the "local" repo provider: it provisions a GitOps
// repository as a directory on disk and returns a repo.LocalSource. NIC commits
// to it in place; ArgoCD's repo-server reads it via a hostPath mount. It is the
// zero-dependency, no-network option for local/dev clusters.
package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repo"
)

const (
	// ProviderName is the registry key and config block name for this provider.
	ProviderName = "local"

	dirPerm = 0o750
)

// Provider implements the local-repository provider.
type Provider struct{}

// NewProvider creates a new local-repository provider.
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return ProviderName
}

// extractConfig converts the generic provider config to the local Config type.
// An empty or absent provider config is valid and yields the zero Config, which
// Provision fills with per-project defaults.
func extractConfig(ctx context.Context, repoConfig *config.RepoConfig) (*Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.extractConfig")
	defer span.End()

	var localCfg Config
	rawCfg := repoConfig.ProviderConfig()
	if rawCfg == nil {
		return &localCfg, nil
	}
	if err := config.UnmarshalProviderConfig(ctx, rawCfg, &localCfg); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal local repo config: %w", err)
	}
	return &localCfg, nil
}

// resolveDir returns the configured directory, or a per-project default under
// the OS temp dir when none is set.
func resolveDir(cfg *Config, projectName string) string {
	if cfg.Path != "" {
		return cfg.Path
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("nebari-gitops-%s", projectName))
}

// Validate checks that the local-repository configuration is valid.
func (p *Provider) Validate(ctx context.Context, projectName string, repoConfig *config.RepoConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
	)

	localCfg, err := extractConfig(ctx, repoConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if err := localCfg.Validate(); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// Provision resolves the repository directory, creates it if needed, and returns
// a LocalSource.
func (p *Provider) Provision(ctx context.Context, projectName string, repoConfig *config.RepoConfig) (repo.Source, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Provision")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
	)

	localCfg, err := extractConfig(ctx, repoConfig)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := localCfg.Validate(); err != nil {
		span.RecordError(err)
		return nil, err
	}

	dir := resolveDir(localCfg, projectName)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("create local repo directory %s: %w", dir, err)
	}

	span.SetAttributes(attribute.String("repo.dir", dir))

	return repo.LocalSource{
		Dir:    dir,
		Branch: localCfg.Branch,
	}, nil
}
