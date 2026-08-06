// Package local implements the "local" repository provider: it provisions a GitOps
// repository as a directory on disk and returns a repository.LocalSource. NIC commits
// to it in place; ArgoCD's repo-server reads it via a hostPath mount. It is the
// zero-dependency, no-network option for local/dev clusters.
package local

import (
	"cmp"
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

const (
	// ProviderName is the registry key and config block name for this provider.
	ProviderName = "local"

	// defaultBranch is used when the config does not specify a branch.
	defaultBranch = "main"
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
func extractConfig(ctx context.Context, repoConfig *config.RepositoryConfig) (*Config, error) {
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
		return nil, fmt.Errorf("failed to unmarshal local repository config: %w", err)
	}
	return &localCfg, nil
}

// resolveDir returns the configured directory, or the per-project default when
// none is set.
func resolveDir(cfg *Config, projectName string) string {
	if cfg.Path != "" {
		return cfg.Path
	}
	return config.DefaultLocalRepositoryPath(projectName)
}

// ResolveDir returns the directory the provider would provision for the given
// configuration, without creating it. Used after a destroy to report a
// retained local GitOps directory without re-creating one that is gone.
func ResolveDir(ctx context.Context, projectName string, repoConfig *config.RepositoryConfig) (string, error) {
	localCfg, err := extractConfig(ctx, repoConfig)
	if err != nil {
		return "", err
	}
	return resolveDir(localCfg, projectName), nil
}

// Validate checks that the local-repository configuration is valid.
func (p *Provider) Validate(ctx context.Context, projectName string, repoConfig *config.RepositoryConfig) error {
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
func (p *Provider) Provision(ctx context.Context, projectName string, repoConfig *config.RepositoryConfig) (repository.Source, error) {
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
	if err := git.EnsureLocalGitOpsDir(ctx, dir); err != nil {
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(attribute.String("repository.dir", dir))

	return repository.LocalSource{
		Dir:    dir,
		Branch: cmp.Or(localCfg.Branch, defaultBranch),
	}, nil
}
