package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	defaultTTL      = 300
	recordTypeA     = "A"
	recordTypeCNAME = "CNAME"
)

// Provider implements the Cloudflare DNS provider.
// Stateless -- config is parsed on each call, matching the cloud provider pattern.
type Provider struct {
	client CloudflareClient // nil = use real SDK client; set via NewProviderForTesting
}

// NewProvider creates a new Cloudflare DNS provider.
func NewProvider() *Provider {
	return &Provider{}
}

// NewProviderForTesting creates a provider with an injected mock client.
func NewProviderForTesting(client CloudflareClient) *Provider {
	return &Provider{client: client}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "cloudflare"
}

// ProvisionRecords creates or updates DNS records for the deployment.
// It creates a root domain record and wildcard record pointing to the
// load balancer endpoint. The record type (A or CNAME) is determined
// automatically from the endpoint value.
func (p *Provider) ProvisionRecords(ctx context.Context, cfg *config.NebariConfig, lbEndpoint string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cloudflare.ProvisionRecords")
	defer span.End()

	// Validate domain
	if cfg.Domain == "" {
		return fmt.Errorf("domain is required for DNS provisioning")
	}
	span.SetAttributes(attribute.String("domain", cfg.Domain))

	// Parse Cloudflare-specific config from the DNS map
	cfCfg, err := extractCloudflareConfig(cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.String("zone_name", cfCfg.ZoneName))

	// Get API token from environment
	apiToken, err := getAPIToken()
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Get client (mock for tests, real SDK for production)
	client, err := p.getClient(apiToken)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Resolve zone ID from zone name
	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Resolving Cloudflare zone ID for %s", cfCfg.ZoneName)).
		WithResource("dns-zone").
		WithAction("resolving"))

	zoneID, err := client.ResolveZoneID(ctx, cfCfg.ZoneName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("zone not found for %q: %w", cfCfg.ZoneName, err)
	}
	span.SetAttributes(attribute.String("zone_id", zoneID))

	// Determine record type from endpoint
	recType := recordTypeForEndpoint(lbEndpoint)
	span.SetAttributes(
		attribute.String("endpoint", lbEndpoint),
		attribute.String("record_type", recType),
	)

	// Ensure root domain record
	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Ensuring DNS record for %s", cfg.Domain)).
		WithResource("dns-record").
		WithAction("ensuring"))

	if err := ensureRecord(ctx, client, zoneID, cfg.Domain, recType, lbEndpoint); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to ensure record for %s: %w", cfg.Domain, err)
	}

	// Ensure wildcard record
	wildcardName := "*." + cfg.Domain
	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Ensuring DNS record for %s", wildcardName)).
		WithResource("dns-record").
		WithAction("ensuring"))

	if err := ensureRecord(ctx, client, zoneID, wildcardName, recType, lbEndpoint); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to ensure record for %s: %w", wildcardName, err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("DNS records provisioned for %s", cfg.Domain)).
		WithResource("dns-records").
		WithAction("provisioned"))

	return nil
}

// DestroyRecords removes DNS records created during deployment.
// It checks for both A and CNAME record types since the original record type
// is not stored. Idempotent -- succeeds even if records are already gone.
func (p *Provider) DestroyRecords(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cloudflare.DestroyRecords")
	defer span.End()

	// Validate domain
	if cfg.Domain == "" {
		return fmt.Errorf("domain is required for DNS record destruction")
	}
	span.SetAttributes(attribute.String("domain", cfg.Domain))

	// Parse Cloudflare-specific config from the DNS map
	cfCfg, err := extractCloudflareConfig(cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.String("zone_name", cfCfg.ZoneName))

	// Get API token from environment
	apiToken, err := getAPIToken()
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Get client (mock for tests, real SDK for production)
	client, err := p.getClient(apiToken)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Resolve zone ID from zone name
	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Resolving Cloudflare zone ID for %s", cfCfg.ZoneName)).
		WithResource("dns-zone").
		WithAction("resolving"))

	zoneID, err := client.ResolveZoneID(ctx, cfCfg.ZoneName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("zone not found for %q: %w", cfCfg.ZoneName, err)
	}
	span.SetAttributes(attribute.String("zone_id", zoneID))

	// Delete records for both root domain and wildcard
	names := []string{cfg.Domain, "*." + cfg.Domain}
	recordTypes := []string{recordTypeA, recordTypeCNAME}

	for _, name := range names {
		for _, recType := range recordTypes {
			if err := p.deleteRecordIfExists(ctx, client, zoneID, name, recType); err != nil {
				span.RecordError(err)
				return fmt.Errorf("failed to delete %s record for %s: %w", recType, name, err)
			}
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("DNS records destroyed for %s", cfg.Domain)).
		WithResource("dns-records").
		WithAction("destroyed"))

	return nil
}

// deleteRecordIfExists lists DNS records matching the given name and type,
// then deletes each one found. No records found is a no-op (idempotent).
func (p *Provider) deleteRecordIfExists(ctx context.Context, client CloudflareClient, zoneID, name, recordType string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cloudflare.deleteRecordIfExists")
	defer span.End()

	span.SetAttributes(
		attribute.String("record_name", name),
		attribute.String("record_type", recordType),
	)

	existing, err := client.ListDNSRecords(ctx, zoneID, name, recordType)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to list %s records for %s: %w", recordType, name, err)
	}

	for _, rec := range existing {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Deleting %s record %s (%s)", rec.Type, rec.Name, rec.ID)).
			WithResource("dns-record").
			WithAction("deleting"))

		if err := client.DeleteDNSRecord(ctx, zoneID, rec.ID); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete record %s (%s): %w", rec.Name, rec.ID, err)
		}
		span.SetAttributes(attribute.String("deleted_record_id", rec.ID))
	}

	return nil
}

// extractCloudflareConfig parses the cfg.DNS map into a Config struct.
// Uses JSON marshal/unmarshal for robust conversion from map[string]any.
// Validates that cfg.Domain is a subdomain of the zone name.
func extractCloudflareConfig(cfg *config.NebariConfig) (*Config, error) {
	if cfg.DNS == nil {
		return nil, fmt.Errorf("dns configuration is missing for cloudflare provider")
	}

	data, err := json.Marshal(cfg.DNS)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dns config: %w", err)
	}

	var cfCfg Config
	if err := json.Unmarshal(data, &cfCfg); err != nil {
		return nil, fmt.Errorf("failed to parse cloudflare dns config: %w", err)
	}

	if cfCfg.ZoneName == "" {
		return nil, fmt.Errorf("dns configuration is missing zone_name for cloudflare provider")
	}

	// Validate that domain is within the configured zone
	// Must match exactly or be a subdomain (with dot separator) to prevent
	// "notexample.com" from matching zone "example.com".
	if cfg.Domain != "" && cfg.Domain != cfCfg.ZoneName && !strings.HasSuffix(cfg.Domain, "."+cfCfg.ZoneName) {
		return nil, fmt.Errorf("domain %q is not within zone %q", cfg.Domain, cfCfg.ZoneName)
	}

	return &cfCfg, nil
}

// getAPIToken reads the Cloudflare API token from the environment.
func getAPIToken() (string, error) {
	token := os.Getenv("CLOUDFLARE_API_TOKEN")
	if token == "" {
		return "", fmt.Errorf("CLOUDFLARE_API_TOKEN environment variable is required")
	}
	return token, nil
}

// getClient returns the injected mock client or creates a real SDK client.
func (p *Provider) getClient(apiToken string) (CloudflareClient, error) {
	if p.client != nil {
		return p.client, nil
	}
	return NewSDKClient(apiToken)
}

// isIPAddress returns true if the endpoint string is an IP address.
func isIPAddress(endpoint string) bool {
	return net.ParseIP(endpoint) != nil
}

// recordTypeForEndpoint returns "A" for IP addresses, "CNAME" for hostnames.
func recordTypeForEndpoint(endpoint string) string {
	if isIPAddress(endpoint) {
		return recordTypeA
	}
	return recordTypeCNAME
}

// ensureRecord creates or updates a DNS record to match the desired state.
// If a single record exists with the correct content, it is a no-op.
// If a single record exists with different content, it is updated.
// If multiple records exist, duplicates are deleted and the first is updated.
// If no record exists, one is created.
func ensureRecord(ctx context.Context, client CloudflareClient, zoneID, name, recordType, content string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cloudflare.ensureRecord")
	defer span.End()

	span.SetAttributes(
		attribute.String("record_name", name),
		attribute.String("record_type", recordType),
		attribute.String("record_content", content),
	)

	// Check for existing records
	existing, err := client.ListDNSRecords(ctx, zoneID, name, recordType)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to list records for %s: %w", name, err)
	}

	if len(existing) > 0 {
		// Delete duplicates (all but the first record)
		for _, dup := range existing[1:] {
			if err := client.DeleteDNSRecord(ctx, zoneID, dup.ID); err != nil {
				span.RecordError(err)
				return fmt.Errorf("failed to delete duplicate record %s (ID: %s): %w", name, dup.ID, err)
			}
		}

		rec := existing[0]
		if rec.Content == content {
			// Record already matches -- no-op
			span.SetAttributes(attribute.String("action", "no-op"))
			return nil
		}
		// Content differs -- update
		span.SetAttributes(attribute.String("action", "update"))
		if err := client.UpdateDNSRecord(ctx, zoneID, rec.ID, name, recordType, content, defaultTTL); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update record %s: %w", name, err)
		}
		return nil
	}

	// No existing record -- create
	span.SetAttributes(attribute.String("action", "create"))
	if err := client.CreateDNSRecord(ctx, zoneID, name, recordType, content, defaultTTL); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create record %s: %w", name, err)
	}

	return nil
}
