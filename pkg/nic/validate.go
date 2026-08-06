package nic

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/dns"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// Validate checks that cfg is well-formed and references providers that are
// actually registered. It performs no I/O against cloud APIs. Returns nil
// when cfg is valid, or an error describing the first validation failure.
func (c *Client) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.Validate")
	defer span.End()

	if err := cfg.Validate(validateOptions(ctx, c.registry)); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Offline DNS provider validation (zone consistency) runs here and in
	// deploy so misconfigurations surface before any infrastructure is
	// provisioned. Destroy and kubeconfig deliberately skip it: destroy
	// treats DNS problems as non-fatal, and kubeconfig never touches DNS.
	if err := validateDNSProvider(ctx, cfg, c.registry); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Reject Longhorn backups on a cluster whose storage layer is not Longhorn.
	// InfraSettings is a pure getter (no cloud I/O), so we can consult the
	// registered provider here and catch the misconfiguration at validate time
	// rather than mid-deploy.
	clusterProvider, err := c.registry.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("get cluster provider %q: %w", cfg.Cluster.ProviderName(), err)
	}
	if err := ensureBackupsHaveLonghorn(cfg, clusterProvider.InfraSettings(cfg.Cluster).StorageClass); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	return nil
}

// validateDNSProvider runs the registered DNS provider's offline validation
// (zone consistency with the deployment domain). No-op when cfg has no dns
// block. Called by validate and deploy only: destroy deliberately treats DNS
// problems as non-fatal so teardown can proceed with a stale DNS config, and
// kubeconfig never touches DNS, so neither gates on this check.
func validateDNSProvider(ctx context.Context, cfg *config.NebariConfig, reg *registry.Registry) error {
	if cfg.DNS == nil {
		return nil
	}
	dnsProvider, err := reg.DNSProviders.Get(ctx, cfg.DNS.ProviderName())
	if err != nil {
		return fmt.Errorf("get dns provider %q: %w", cfg.DNS.ProviderName(), err)
	}
	if err := dnsProvider.Validate(ctx, cfg.Domain, cfg.DNS.ProviderConfig(), dns.ValidateOptions{CheckCreds: false}); err != nil {
		return fmt.Errorf("invalid dns: %w", err)
	}
	return nil
}
