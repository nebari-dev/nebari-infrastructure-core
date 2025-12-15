package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// Provider implements the Cloudflare DNS provider
type Provider struct {
	config   *Config
	apiToken string // Read from CLOUDFLARE_API_TOKEN env var
}

// NewProvider creates a new Cloudflare DNS provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "cloudflare"
}

// Initialize sets up the Cloudflare DNS provider with credentials (stub implementation)
func (p *Provider) Initialize(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "cloudflare.Initialize")
	defer span.End()

	span.SetAttributes(
		attribute.String("dns_provider", "cloudflare"),
		attribute.String("project_name", cfg.ProjectName),
	)

	// Parse Cloudflare-specific config from the DNS map
	if cfg.DNS == nil {
		err := fmt.Errorf("dns configuration is missing")
		span.RecordError(err)
		return err
	}

	// Convert map to JSON and back to struct (simple way to parse dynamic config)
	configJSON, err := json.Marshal(cfg.DNS)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal DNS config: %w", err)
	}

	var cloudflareConfig Config
	if err := json.Unmarshal(configJSON, &cloudflareConfig); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to unmarshal Cloudflare config: %w", err)
	}

	// Read API token from environment variable (NEVER from config)
	p.apiToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	if p.apiToken == "" {
		err := fmt.Errorf("CLOUDFLARE_API_TOKEN environment variable is not set")
		span.RecordError(err)
		return err
	}

	p.config = &cloudflareConfig

	span.SetAttributes(attribute.String("cloudflare.zone_name", p.config.ZoneName))

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cloudflare DNS provider initialized (stub)").
		WithResource("dns-provider").
		WithAction("initialize").
		WithMetadata("zone_name", p.config.ZoneName))

	return nil
}

// GetRecord retrieves a specific DNS record by name and type (stub implementation)
func (p *Provider) GetRecord(ctx context.Context, name string, recordType string) (*dnsprovider.DNSRecord, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "cloudflare.GetRecord")
	defer span.End()

	if p.config == nil {
		err := fmt.Errorf("provider not initialized")
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(
		attribute.String("cloudflare.zone_name", p.config.ZoneName),
		attribute.String("record.name", name),
		attribute.String("record.type", recordType),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Getting DNS record (stub)").
		WithResource("dns-record").
		WithAction("get").
		WithMetadata("zone_name", p.config.ZoneName).
		WithMetadata("name", name).
		WithMetadata("type", recordType))

	// Stub: return nil (record not found)
	return nil, nil
}

// AddRecord creates a new DNS record (stub implementation)
func (p *Provider) AddRecord(ctx context.Context, record dnsprovider.DNSRecord) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "cloudflare.AddRecord")
	defer span.End()

	if p.config == nil {
		err := fmt.Errorf("provider not initialized")
		span.RecordError(err)
		return err
	}

	span.SetAttributes(
		attribute.String("cloudflare.zone_name", p.config.ZoneName),
		attribute.String("record.name", record.Name),
		attribute.String("record.type", record.Type),
		attribute.String("record.content", record.Content),
	)

	recordJSON, _ := json.MarshalIndent(record, "", "  ")
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Adding DNS record (stub)").
		WithResource("dns-record").
		WithAction("add").
		WithMetadata("zone_name", p.config.ZoneName).
		WithMetadata("name", record.Name).
		WithMetadata("type", record.Type).
		WithMetadata("content", record.Content).
		WithMetadata("record_json", string(recordJSON)))

	return nil
}

// UpdateRecord updates an existing DNS record (stub implementation)
func (p *Provider) UpdateRecord(ctx context.Context, record dnsprovider.DNSRecord) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "cloudflare.UpdateRecord")
	defer span.End()

	if p.config == nil {
		err := fmt.Errorf("provider not initialized")
		span.RecordError(err)
		return err
	}

	span.SetAttributes(
		attribute.String("cloudflare.zone_name", p.config.ZoneName),
		attribute.String("record.name", record.Name),
		attribute.String("record.type", record.Type),
		attribute.String("record.content", record.Content),
	)

	recordJSON, _ := json.MarshalIndent(record, "", "  ")
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Updating DNS record (stub)").
		WithResource("dns-record").
		WithAction("update").
		WithMetadata("zone_name", p.config.ZoneName).
		WithMetadata("name", record.Name).
		WithMetadata("type", record.Type).
		WithMetadata("content", record.Content).
		WithMetadata("record_json", string(recordJSON)))

	return nil
}

// DeleteRecord deletes a DNS record by name and type (stub implementation)
func (p *Provider) DeleteRecord(ctx context.Context, name string, recordType string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "cloudflare.DeleteRecord")
	defer span.End()

	if p.config == nil {
		err := fmt.Errorf("provider not initialized")
		span.RecordError(err)
		return err
	}

	span.SetAttributes(
		attribute.String("cloudflare.zone_name", p.config.ZoneName),
		attribute.String("record.name", name),
		attribute.String("record.type", recordType),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Deleting DNS record (stub)").
		WithResource("dns-record").
		WithAction("delete").
		WithMetadata("zone_name", p.config.ZoneName).
		WithMetadata("name", name).
		WithMetadata("type", recordType))

	return nil
}

// EnsureRecord ensures a record exists with the given properties (stub implementation)
func (p *Provider) EnsureRecord(ctx context.Context, record dnsprovider.DNSRecord) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "cloudflare.EnsureRecord")
	defer span.End()

	if p.config == nil {
		err := fmt.Errorf("provider not initialized")
		span.RecordError(err)
		return err
	}

	span.SetAttributes(
		attribute.String("cloudflare.zone_name", p.config.ZoneName),
		attribute.String("record.name", record.Name),
		attribute.String("record.type", record.Type),
		attribute.String("record.content", record.Content),
	)

	recordJSON, _ := json.MarshalIndent(record, "", "  ")
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Ensuring DNS record (stub)").
		WithResource("dns-record").
		WithAction("ensure").
		WithMetadata("zone_name", p.config.ZoneName).
		WithMetadata("name", record.Name).
		WithMetadata("type", record.Type).
		WithMetadata("content", record.Content).
		WithMetadata("record_json", string(recordJSON)))

	return nil
}

// GetCertManagerConfig returns configuration for cert-manager (stub implementation)
func (p *Provider) GetCertManagerConfig(ctx context.Context) (map[string]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "cloudflare.GetCertManagerConfig")
	defer span.End()

	if p.config == nil {
		err := fmt.Errorf("provider not initialized")
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(attribute.String("cloudflare.zone_name", p.config.ZoneName))

	// Return Cloudflare-specific cert-manager configuration
	certManagerConfig := map[string]string{
		"apiTokenSecretRef": "cloudflare-api-token",
		"email":             p.config.Email,
	}

	configJSON, _ := json.MarshalIndent(certManagerConfig, "", "  ")
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Getting cert-manager configuration (stub)").
		WithResource("cert-manager").
		WithAction("get-config").
		WithMetadata("zone_name", p.config.ZoneName).
		WithMetadata("config_json", string(configJSON)))

	return certManagerConfig, nil
}
