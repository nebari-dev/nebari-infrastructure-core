package existing

import (
	"cmp"
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

// ProviderName is the registry key and config block name for this provider.
const ProviderName = "existing"

// defaultBranch is used when the config does not specify a branch.
const defaultBranch = "main"

// Provider implements the existing-repository provider.
type Provider struct{}

// NewProvider creates a new existing-repository provider.
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return ProviderName
}

// extractConfig converts the generic provider config to the existing Config type.
func extractConfig(ctx context.Context, repoConfig *config.RepositoryConfig) (*Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "existing.extractConfig")
	defer span.End()

	rawCfg := repoConfig.ProviderConfig()
	if rawCfg == nil {
		err := fmt.Errorf("existing repository configuration is required")
		span.RecordError(err)
		return nil, err
	}

	var existingCfg Config
	if err := config.UnmarshalProviderConfig(ctx, rawCfg, &existingCfg); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal existing repository config: %w", err)
	}

	return &existingCfg, nil
}

// Validate checks that the existing-repository configuration is valid.
func (p *Provider) Validate(ctx context.Context, projectName string, repoConfig *config.RepositoryConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "existing.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
	)

	existingCfg, err := extractConfig(ctx, repoConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if err := existingCfg.Validate(); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// Provision resolves the configured remote repository and returns its
// RemoteSource. The repository must already exist; this provider does not
// create one. Credentials are resolved from their configured environment
// variables.
func (p *Provider) Provision(ctx context.Context, projectName string, repoConfig *config.RepositoryConfig) (repository.Source, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "existing.Provision")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
	)

	existingCfg, err := extractConfig(ctx, repoConfig)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := existingCfg.Validate(); err != nil {
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(attribute.String("repository.url", existingCfg.URL))

	pushAuth, err := existingCfg.Auth.resolve()
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("resolve push credentials: %w", err)
	}

	readAuth, err := existingCfg.GetArgoCDAuth().resolve()
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("resolve argocd credentials: %w", err)
	}

	return repository.RemoteSource{
		URL:      existingCfg.URL,
		Branch:   cmp.Or(existingCfg.Branch, defaultBranch),
		Path:     existingCfg.Path,
		PushAuth: pushAuth,
		ReadAuth: readAuth,
	}, nil
}
