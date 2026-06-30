package openshift

import (
	"context"
	"fmt"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/kubeconfig"
)

// Validate checks the OpenShift configuration before any infrastructure
// operations. It branches on the configured mode: existing-mode validation is
// kubeconfig-centric, provision-mode validation checks ROSA/AWS credentials.
func (p *Provider) Validate(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "openshift.Validate")
	defer span.End()

	cfg, err := extractConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
		attribute.String("mode", cfg.Mode()),
	)

	switch cfg.Mode() {
	case ModeExisting:
		return validateExisting(ctx, cfg)
	case ModeProvision:
		return validateProvision(ctx, cfg)
	default:
		err := fmt.Errorf("invalid openshift mode %q (must be %q or %q)", cfg.Mode(), ModeProvision, ModeExisting)
		span.RecordError(err)
		return err
	}
}

// validateExisting verifies the kubeconfig file loads and the context exists.
func validateExisting(ctx context.Context, cfg *Config) error {
	_ = ctx
	if cfg.Context == "" {
		return fmt.Errorf("context is required for openshift existing mode")
	}
	path, err := cfg.GetKubeconfigPath()
	if err != nil {
		return err
	}
	return kubeconfig.ValidateContext(path, cfg.Context)
}

// validateProvision verifies the required region, the ROSA OCM token, and that
// AWS credentials resolve for the target region.
func validateProvision(ctx context.Context, cfg *Config) error {
	if cfg.Region == "" {
		return fmt.Errorf("region is required for openshift provision mode")
	}
	if os.Getenv("RHCS_TOKEN") == "" {
		return fmt.Errorf("RHCS_TOKEN environment variable is required for openshift provision mode (Red Hat OCM offline token)")
	}
	sdkCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}
	if _, err := sdkCfg.Credentials.Retrieve(ctx); err != nil {
		return fmt.Errorf("failed to retrieve AWS credentials: %w", err)
	}
	return nil
}
