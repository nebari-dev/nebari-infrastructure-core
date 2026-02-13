package cloudflare

import (
	"context"
	"fmt"

	cfapi "github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/dns"
	"github.com/cloudflare/cloudflare-go/v4/option"
	"github.com/cloudflare/cloudflare-go/v4/zones"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// sdkClient wraps the cloudflare-go v4 SDK to implement CloudflareClient.
// This is a thin adapter -- no business logic, only type translation.
type sdkClient struct {
	api *cfapi.Client
}

// NewSDKClient creates a real Cloudflare API client using the provided API token.
func NewSDKClient(apiToken string) (CloudflareClient, error) {
	client := cfapi.NewClient(option.WithAPIToken(apiToken))
	return &sdkClient{api: client}, nil
}

// ResolveZoneID looks up the zone ID for a given zone name.
func (c *sdkClient) ResolveZoneID(ctx context.Context, zoneName string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cloudflare.sdk.ResolveZoneID")
	defer span.End()

	span.SetAttributes(attribute.String("zone_name", zoneName))

	pager := c.api.Zones.ListAutoPaging(ctx, zones.ZoneListParams{
		Name: cfapi.F(zoneName),
	})

	for pager.Next() {
		zone := pager.Current()
		if zone.Name == zoneName {
			span.SetAttributes(attribute.String("zone_id", zone.ID))
			return zone.ID, nil
		}
	}
	if err := pager.Err(); err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to list zones: %w (check that your API token has Zone:Read permission)", err)
	}

	err := fmt.Errorf("no zone found for %q: check that the zone exists and your API token has Zone:Read permission", zoneName)
	span.RecordError(err)
	return "", err
}

// ListDNSRecords returns DNS records matching the given name and type.
func (c *sdkClient) ListDNSRecords(ctx context.Context, zoneID string, name string, recordType string) ([]DNSRecordResult, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cloudflare.sdk.ListDNSRecords")
	defer span.End()

	span.SetAttributes(
		attribute.String("zone_id", zoneID),
		attribute.String("record_name", name),
		attribute.String("record_type", recordType),
	)

	params := dns.RecordListParams{
		ZoneID: cfapi.F(zoneID),
	}
	if name != "" {
		params.Name = cfapi.F(dns.RecordListParamsName{
			Exact: cfapi.F(name),
		})
	}
	if recordType != "" {
		params.Type = cfapi.F(dns.RecordListParamsType(recordType))
	}

	var results []DNSRecordResult
	pager := c.api.DNS.Records.ListAutoPaging(ctx, params)

	for pager.Next() {
		rec := pager.Current()
		results = append(results, DNSRecordResult{
			ID:      rec.ID,
			Name:    rec.Name,
			Type:    string(rec.Type),
			Content: rec.Content,
			TTL:     int(rec.TTL),
		})
	}
	if err := pager.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to list DNS records: %w", err)
	}

	span.SetAttributes(attribute.Int("record_count", len(results)))
	return results, nil
}

// CreateDNSRecord creates a new DNS record in the given zone.
// Only A and CNAME record types are supported.
func (c *sdkClient) CreateDNSRecord(ctx context.Context, zoneID string, name string, recordType string, content string, ttl int) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cloudflare.sdk.CreateDNSRecord")
	defer span.End()

	span.SetAttributes(
		attribute.String("zone_id", zoneID),
		attribute.String("record_name", name),
		attribute.String("record_type", recordType),
		attribute.String("record_content", content),
		attribute.Int("record_ttl", ttl),
	)

	body, err := buildRecordBody(name, recordType, content, ttl)
	if err != nil {
		span.RecordError(err)
		return err
	}

	_, err = c.api.DNS.Records.New(ctx, dns.RecordNewParams{
		ZoneID: cfapi.F(zoneID),
		Body:   body,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create DNS record %s (%s): %w", name, recordType, err)
	}

	return nil
}

// UpdateDNSRecord updates an existing DNS record by ID.
// Only A and CNAME record types are supported.
func (c *sdkClient) UpdateDNSRecord(ctx context.Context, zoneID string, recordID string, name string, recordType string, content string, ttl int) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cloudflare.sdk.UpdateDNSRecord")
	defer span.End()

	span.SetAttributes(
		attribute.String("zone_id", zoneID),
		attribute.String("record_id", recordID),
		attribute.String("record_name", name),
		attribute.String("record_type", recordType),
		attribute.String("record_content", content),
		attribute.Int("record_ttl", ttl),
	)

	body, err := buildUpdateRecordBody(name, recordType, content, ttl)
	if err != nil {
		span.RecordError(err)
		return err
	}

	_, err = c.api.DNS.Records.Update(ctx, recordID, dns.RecordUpdateParams{
		ZoneID: cfapi.F(zoneID),
		Body:   body,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to update DNS record %s (%s, id=%s): %w", name, recordType, recordID, err)
	}

	return nil
}

// DeleteDNSRecord deletes a DNS record by ID.
func (c *sdkClient) DeleteDNSRecord(ctx context.Context, zoneID string, recordID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cloudflare.sdk.DeleteDNSRecord")
	defer span.End()

	span.SetAttributes(
		attribute.String("zone_id", zoneID),
		attribute.String("record_id", recordID),
	)

	_, err := c.api.DNS.Records.Delete(ctx, recordID, dns.RecordDeleteParams{
		ZoneID: cfapi.F(zoneID),
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete DNS record (id=%s): %w", recordID, err)
	}

	return nil
}

// buildRecordBody constructs the appropriate RecordNewParamsBodyUnion for the
// given record type. Only A and CNAME are supported.
func buildRecordBody(name, recordType, content string, ttl int) (dns.RecordNewParamsBodyUnion, error) {
	switch recordType {
	case recordTypeA:
		return dns.ARecordParam{
			Name:    cfapi.F(name),
			Type:    cfapi.F(dns.ARecordTypeA),
			Content: cfapi.F(content),
			TTL:     cfapi.F(dns.TTL(ttl)),
		}, nil
	case recordTypeCNAME:
		return dns.CNAMERecordParam{
			Name:    cfapi.F(name),
			Type:    cfapi.F(dns.CNAMERecordTypeCNAME),
			Content: cfapi.F(content),
			TTL:     cfapi.F(dns.TTL(ttl)),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported record type %q: only A and CNAME are supported", recordType)
	}
}

// buildUpdateRecordBody constructs the appropriate RecordUpdateParamsBodyUnion
// for the given record type. Only A and CNAME are supported.
func buildUpdateRecordBody(name, recordType, content string, ttl int) (dns.RecordUpdateParamsBodyUnion, error) {
	switch recordType {
	case recordTypeA:
		return dns.ARecordParam{
			Name:    cfapi.F(name),
			Type:    cfapi.F(dns.ARecordTypeA),
			Content: cfapi.F(content),
			TTL:     cfapi.F(dns.TTL(ttl)),
		}, nil
	case recordTypeCNAME:
		return dns.CNAMERecordParam{
			Name:    cfapi.F(name),
			Type:    cfapi.F(dns.CNAMERecordTypeCNAME),
			Content: cfapi.F(content),
			TTL:     cfapi.F(dns.TTL(ttl)),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported record type %q: only A and CNAME are supported", recordType)
	}
}
