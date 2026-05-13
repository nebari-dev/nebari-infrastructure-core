package nic

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// DestroySummary describes the infrastructure a Destroy is about to tear
// down. It is passed to DestroyOptions.Confirm so callers can render a
// confirmation prompt without reaching into provider internals themselves.
type DestroySummary struct {
	// Provider is the cluster provider identifier (e.g., "aws", "gcp").
	Provider string

	// ProjectName is the Nebari project name from the config.
	ProjectName string

	// Details is the provider-specific key/value summary returned by
	// Provider.Summary (region, cluster name, node group sizes, etc.).
	Details map[string]string
}

// DestroyOptions configures a Destroy call.
type DestroyOptions struct {
	// DryRun previews deletions without applying them.
	DryRun bool

	// Force continues destruction even when the provider reports errors on
	// individual resources. Partial failures are logged rather than returned.
	Force bool

	// Timeout overrides the provider's default destroy timeout. Zero means
	// the provider chooses.
	Timeout time.Duration

	// Confirm, when non-nil, is invoked after the provider has been resolved
	// but before any destructive call. Returning a non-nil error aborts
	// Destroy with that error, allowing callers to implement interactive
	// confirmation prompts or policy checks. Skipped when DryRun is true.
	// Leave nil for programmatic callers that do not need a prompt.
	Confirm func(ctx context.Context, summary DestroySummary) error
}

// Destroy tears down the cluster described by cfg and cleans up any DNS
// records provisioned alongside it. The caller owns any confirmation
// prompt via opts.Confirm; Destroy assumes consent has already been
// granted by the time it is invoked.
//
// When cfg.DNS is set, DNS records are cleaned up before the cluster is
// torn down. DNS cleanup failures are logged but do not abort the destroy,
// since orphaned DNS records are cheaper to fix manually than a
// half-destroyed cluster. Provider errors abort the run unless Force is
// true, in which case they are logged and execution continues.
func (c *Client) Destroy(ctx context.Context, cfg *config.NebariConfig, opts DestroyOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.Bool("dry_run", opts.DryRun),
		attribute.Bool("force", opts.Force),
	)
	if opts.Timeout > 0 {
		span.SetAttributes(attribute.String("timeout", opts.Timeout.String()))
	}

	if opts.DryRun {
		c.logger.Info("Starting destruction (dry-run)")
	} else {
		c.logger.Info("Starting destruction")
	}

	reg := c.registry

	if err := cfg.Validate(validateOptions(ctx, reg)); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	c.logger.Info("Configuration validated",
		"provider", cfg.Cluster.ProviderName(),
		"project_name", cfg.ProjectName,
	)

	prov, err := reg.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("get cluster provider: %w", err)
	}

	c.logger.Info("Provider selected", "provider", prov.Name())

	if opts.Confirm != nil && !opts.DryRun {
		summary := DestroySummary{
			Provider:    cfg.Cluster.ProviderName(),
			ProjectName: cfg.ProjectName,
			Details:     prov.Summary(cfg.Cluster),
		}
		if err := opts.Confirm(ctx, summary); err != nil {
			span.RecordError(err)
			return err
		}
	}

	ctx, cleanup := status.StartHandler(ctx, c.statusLogHandler())
	defer cleanup()

	if cfg.DNS != nil {
		if err := c.destroyDNS(ctx, cfg, reg, opts.DryRun); err != nil {
			c.logger.Warn("Failed to clean up DNS records", "error", err)
			c.logger.Warn("You may need to manually remove DNS records from your provider")
		}
	}

	if err := prov.Destroy(ctx, cfg.ProjectName, cfg.Cluster, provider.DestroyOptions{
		DryRun:  opts.DryRun,
		Force:   opts.Force,
		Timeout: opts.Timeout,
	}); err != nil {
		span.RecordError(err)
		if opts.Force {
			c.logger.Warn("Continuing despite errors due to Force=true", "error", err)
		} else {
			return fmt.Errorf("provider destroy: %w", err)
		}
	}

	c.logger.Info("Destruction completed successfully", "provider", prov.Name())
	return nil
}

// destroyDNS removes DNS records associated with the cluster's domain.
// Split from Destroy so failures here can be warned about without aborting
// the cluster teardown.
func (c *Client) destroyDNS(ctx context.Context, cfg *config.NebariConfig, reg *registry.Registry, dryRun bool) error {
	if dryRun {
		c.logger.Info("Would clean up DNS records (dry-run)", "provider", cfg.DNS.ProviderName(), "domain", cfg.Domain)
		return nil
	}

	dnsProvider, err := reg.DNSProviders.Get(ctx, cfg.DNS.ProviderName())
	if err != nil {
		return err
	}
	c.logger.Info("Cleaning up DNS records", "provider", cfg.DNS.ProviderName(), "domain", cfg.Domain)

	if err := dnsProvider.DestroyRecords(ctx, cfg.Domain, cfg.DNS.ProviderConfig()); err != nil {
		return err
	}

	c.logger.Info("DNS records cleaned up successfully", "domain", cfg.Domain)
	return nil
}
