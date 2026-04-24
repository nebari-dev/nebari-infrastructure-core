package action

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// DestroySummary describes the infrastructure a Destroy is about to tear
// down. It is passed to Destroy.Confirm so callers can render a confirmation
// prompt without reaching into provider internals themselves.
type DestroySummary struct {
	// Provider is the cluster provider identifier (e.g., "aws", "gcp").
	Provider string

	// ProjectName is the Nebari project name from the config.
	ProjectName string

	// Details is the provider-specific key/value summary returned by
	// Provider.Summary (region, cluster name, node group sizes, etc.).
	Details map[string]string
}

// Destroy tears down the cluster described by NebariConfig and cleans up any
// DNS records provisioned alongside it.
type Destroy struct {
	// DryRun previews deletions without applying them.
	DryRun bool

	// Force continues destruction even when the provider reports errors on
	// individual resources. Partial failures are logged rather than returned.
	Force bool

	// Timeout overrides the provider's default destroy timeout. Zero means the
	// provider chooses.
	Timeout time.Duration

	// Confirm, when non-nil, is invoked after the provider has been resolved
	// but before any destructive call. Returning a non-nil error aborts Run
	// with that error, allowing callers to implement interactive confirmation
	// prompts or policy checks. Skipped when DryRun is true. Leave nil for
	// programmatic callers that do not need a prompt.
	Confirm func(ctx context.Context, summary DestroySummary) error
}

// Run destroys the infrastructure described by cfg. The caller owns any
// confirmation prompt; Run assumes consent has already been granted by the
// time it is invoked.
//
// When cfg.DNS is set, DNS records are cleaned up before the cluster is torn
// down. DNS cleanup failures are logged but do not abort the destroy, since
// orphaned DNS records are cheaper to fix manually than a half-destroyed
// cluster. Provider errors abort the run unless Force is true, in which case
// they are logged and execution continues.
func (d *Destroy) Run(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "action.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.Bool("dry_run", d.DryRun),
		attribute.Bool("force", d.Force),
	)
	if d.Timeout > 0 {
		span.SetAttributes(attribute.String("timeout", d.Timeout.String()))
	}

	if d.DryRun {
		slog.Info("Starting destruction (dry-run)")
	} else {
		slog.Info("Starting destruction")
	}

	reg, err := defaultRegistry(ctx)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("build default registry: %w", err)
	}

	if err := cfg.Validate(validateOptions(ctx, reg)); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	slog.Info("Configuration validated",
		"provider", cfg.Cluster.ProviderName(),
		"project_name", cfg.ProjectName,
	)

	prov, err := reg.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("get cluster provider: %w", err)
	}

	slog.Info("Provider selected", "provider", prov.Name())

	if d.Confirm != nil && !d.DryRun {
		summary := DestroySummary{
			Provider:    cfg.Cluster.ProviderName(),
			ProjectName: cfg.ProjectName,
			Details:     prov.Summary(cfg.Cluster),
		}
		if err := d.Confirm(ctx, summary); err != nil {
			span.RecordError(err)
			return err
		}
	}

	ctx, cleanup := status.StartHandler(ctx, statusLogHandler())
	defer cleanup()

	if cfg.DNS != nil {
		if err := destroyDNS(ctx, cfg, reg, d.DryRun); err != nil {
			slog.Warn("Failed to clean up DNS records", "error", err)
			slog.Warn("You may need to manually remove DNS records from your provider")
		}
	}

	if err := prov.Destroy(ctx, cfg.ProjectName, cfg.Cluster, provider.DestroyOptions{
		DryRun:  d.DryRun,
		Force:   d.Force,
		Timeout: d.Timeout,
	}); err != nil {
		span.RecordError(err)
		if d.Force {
			slog.Warn("Continuing despite errors due to Force=true", "error", err)
		} else {
			return fmt.Errorf("provider destroy: %w", err)
		}
	}

	slog.Info("Destruction completed successfully", "provider", prov.Name())
	return nil
}

// destroyDNS removes DNS records associated with the cluster's domain. Split
// from Run so failures here can be warned about without aborting the cluster
// teardown.
func destroyDNS(ctx context.Context, cfg *config.NebariConfig, reg *registry.Registry, dryRun bool) error {
	if dryRun {
		slog.Info("Would clean up DNS records (dry-run)", "provider", cfg.DNS.ProviderName(), "domain", cfg.Domain)
		return nil
	}

	dnsProvider, err := reg.DNSProviders.Get(ctx, cfg.DNS.ProviderName())
	if err != nil {
		return err
	}
	slog.Info("Cleaning up DNS records", "provider", cfg.DNS.ProviderName(), "domain", cfg.Domain)

	if err := dnsProvider.DestroyRecords(ctx, cfg.Domain, cfg.DNS.ProviderConfig()); err != nil {
		return err
	}

	slog.Info("DNS records cleaned up successfully", "domain", cfg.Domain)
	return nil
}
