package nic

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/dns"
	repositorylocal "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository/local"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// Validate checks that cfg is well-formed and references providers that are
// actually registered. It performs no I/O against cloud APIs. Returns nil
// when cfg is valid, or an error describing the first validation failure.
func (c *Client) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.Validate")
	defer span.End()

	// Reject unfilled CHANGEME placeholders before anything else looks at the
	// values, so an unedited starter config fails naming the fields to fill in
	// rather than failing later on a nonsense value.
	if err := rejectPlaceholders(cfg); err != nil {
		span.RecordError(err)
		return err
	}

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

	// Offline repository provider validation (well-formed provider config)
	// runs here and in deploy for the same reason.
	if err := validateRepositoryProvider(ctx, cfg, c.registry); err != nil {
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
	infraSettings := clusterProvider.InfraSettings(cfg.Cluster)
	if err := ensureBackupsHaveLonghorn(cfg, infraSettings.StorageClass); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	if err := ensureLocalRepositorySupported(cfg, infraSettings.SupportsLocalGitOps); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	if err := ensureDNSSupported(cfg, infraSettings.GatewayHostPorts); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	return nil
}

// rejectPlaceholders rejects a config that still carries the CHANGEME sentinel
// in any scalar value or mapping key. It reads the YAML the config was parsed
// from, because the sentinel can sit in places the decoded struct cannot show:
// mapping keys such as node_groups, and values whose field is not a string.
// A config built programmatically in Go has no source and is a no-op here.
//
// Called by validate and deploy only, for the same reason as the validators
// below: destroy and kubeconfig must keep working against a config that was
// already edited to deploy the cluster, and blocking a teardown because someone
// reintroduced a placeholder would be the wrong trade.
//
// The file path is attached here rather than in pkg/config, so the error names
// both the fields to edit and the file to edit them in.
func rejectPlaceholders(cfg *config.NebariConfig) error {
	raw := cfg.SourceRaw()
	if len(raw) == 0 {
		return nil
	}
	if err := config.CheckPlaceholders(raw); err != nil {
		if path := cfg.SourcePath(); path != "" {
			return fmt.Errorf("%w (in config file %q)", err, path)
		}
		return err
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

// validateRepositoryProvider runs the registered repository provider's offline
// validation (e.g. url and exactly one auth method for existing, absolute path
// for local). Called by validate and deploy (including dry-run) so misconfigurations
// surface before any infrastructure is provisioned. Destroy and kubeconfig deliberately
// skip it so a cluster with a stale repository config remains destroyable. Presence of
// the repository block itself is enforced by cfg.Validate, so a nil block is a no-op here.
func validateRepositoryProvider(ctx context.Context, cfg *config.NebariConfig, reg *registry.Registry) error {
	if cfg.Repository == nil {
		return nil
	}
	repoProvider, err := reg.RepositoryProviders.Get(ctx, cfg.Repository.ProviderName())
	if err != nil {
		return fmt.Errorf("get repository provider %q: %w", cfg.Repository.ProviderName(), err)
	}
	if err := repoProvider.Validate(ctx, cfg.ProjectName, cfg.Repository); err != nil {
		return fmt.Errorf("invalid repository: %w", err)
	}
	return nil
}

// ensureDNSSupported rejects a dns block on a cluster whose gateway is
// published on loopback host ports (local kind clusters). Public DNS records
// cannot usefully point at 127.0.0.1, and deploy would skip provisioning them
// anyway, so the block is dead configuration at best and at worst suppresses
// the /etc/hosts guidance the user actually needs.
func ensureDNSSupported(cfg *config.NebariConfig, gatewayHostPorts bool) error {
	if cfg.DNS != nil && gatewayHostPorts {
		return fmt.Errorf("a dns provider is not supported by cluster provider %q: the gateway is published on host ports of 127.0.0.1, which DNS records cannot usefully point to. Remove the dns block and use the /etc/hosts instructions printed by deploy", cfg.Cluster.ProviderName())
	}
	return nil
}

// ensureLocalRepositorySupported rejects the local repository provider on a
// cluster that cannot host its directory (only local/kind clusters can mount
// one). The name-based check catches this at validate time; deploy re-checks
// the provisioned source kind, which also covers out-of-tree providers that
// return a LocalSource.
func ensureLocalRepositorySupported(cfg *config.NebariConfig, supportsLocalGitOps bool) error {
	if cfg.Repository != nil && cfg.Repository.ProviderName() == repositorylocal.ProviderName && !supportsLocalGitOps {
		return fmt.Errorf("a local repository is not supported by cluster provider %q; use a remote repository provider", cfg.Cluster.ProviderName())
	}
	return nil
}
