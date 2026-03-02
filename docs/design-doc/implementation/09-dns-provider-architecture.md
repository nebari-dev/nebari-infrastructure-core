# DNS Provider Architecture

**Status:** Implemented (Cloudflare)
**Last Updated:** 2026-02-18

## Overview

The DNS Provider subsystem manages DNS records for Nebari deployments. It automatically provisions root and wildcard DNS records pointing to the cluster load balancer during deploy, and cleans them up during destroy.

**Primary use case:** Point domain names to cluster load balancer endpoints (A records for IPs, CNAME records for hostnames).

**Future use case:** cert-manager DNS-01 challenge configuration for Let's Encrypt TLS certificates (see [#71](https://github.com/nebari-dev/nebari-infrastructure-core/issues/71)).

## Design Principles

### 1. Pluggable Architecture
DNS providers follow the same explicit registration pattern as cloud providers:
- No blank imports or `init()` magic
- Explicit registration in `cmd/nic/main.go`
- Thread-safe registry with read/write locking
- Providers registered independently of cloud providers

### 2. Stateless Interface
The DNS provider interface is **stateless** - domain and DNS config are passed to each call rather than stored in the provider:
- `ProvisionRecords()` creates/updates records for a given domain and endpoint
- `DestroyRecords()` removes records for a given domain
- Provider determines record type automatically (A for IPs, CNAME for hostnames)
- Idempotent: safe to call repeatedly with the same inputs

### 3. Secrets Management
API tokens and credentials are **NEVER** stored in configuration files:
- Read exclusively from environment variables
- `.env` file support for local development (gitignored)
- `.env.example` provided as template

### 4. Configuration Separation
DNS provider configuration contains only **non-secret** data:
- Zone names and provider-specific settings
- Secrets fetched at runtime from environment

### 5. Non-Blocking Errors
DNS errors are treated as warnings and never block deploy or destroy operations. If DNS provisioning fails, the user sees a warning and manual DNS instructions.

## Architecture Components

### DNS Provider Interface

```go
// pkg/dnsprovider/provider.go
type DNSProvider interface {
    // Name returns the DNS provider name (cloudflare, route53, azure-dns, etc.)
    Name() string

    // ProvisionRecords creates or updates DNS records for the deployment.
    // It creates a root domain record and wildcard record pointing to the
    // load balancer endpoint. The provider determines the record type
    // (CNAME for hostnames, A for IPs) from the endpoint value.
    ProvisionRecords(ctx context.Context, domain string, dnsConfig map[string]any, lbEndpoint string) error

    // DestroyRecords removes DNS records that were created during deployment.
    // This is called before infrastructure destruction to clean up stale records.
    // Idempotent - succeeds even if records are already gone.
    DestroyRecords(ctx context.Context, domain string, dnsConfig map[string]any) error
}
```

### CloudflareClient Interface

The Cloudflare provider uses an internal client interface for testability:

```go
// pkg/dnsprovider/cloudflare/client.go
type CloudflareClient interface {
    ResolveZoneID(ctx context.Context, zoneName string) (string, error)
    ListDNSRecords(ctx context.Context, zoneID string, name string, recordType string) ([]DNSRecordResult, error)
    CreateDNSRecord(ctx context.Context, zoneID string, name string, recordType string, content string, ttl int) error
    UpdateDNSRecord(ctx context.Context, zoneID string, recordID string, name string, recordType string, content string, ttl int) error
    DeleteDNSRecord(ctx context.Context, zoneID string, recordID string) error
}
```

The real implementation (`sdkClient`) wraps the `cloudflare-go/v4` SDK. Tests inject a mock via `NewProviderForTesting()`.

### Registry Pattern

DNS providers use a separate registry from cloud providers:

```go
// cmd/nic/main.go
var (
    registry    *provider.Registry     // Cloud providers
    dnsRegistry *dnsprovider.Registry  // DNS providers (separate)
)

func main() {
    // ...
    dnsRegistry = dnsprovider.NewRegistry()
    if err := dnsRegistry.Register(ctx, "cloudflare", cloudflare.NewProvider()); err != nil {
        log.Fatalf("Failed to register Cloudflare DNS provider: %v", err)
    }
}
```

## Configuration

### YAML Configuration

```yaml
# nebari-config.yaml
project_name: my-nebari
provider: aws
domain: nebari.example.com

# Cloud provider config...
amazon_web_services:
  region: us-west-2
  # ...

# DNS configuration (optional)
dns_provider: cloudflare
dns:
  zone_name: example.com
```

### Environment Variables

```bash
# .env (gitignored)
CLOUDFLARE_API_TOKEN=your_token_here
```

### Dynamic Config Parsing

DNS-specific config is stored as `map[string]any` and parsed by each provider:

```go
// In NebariConfig
type NebariConfig struct {
    DNSProvider string         `yaml:"dns_provider,omitempty"`
    DNS         map[string]any `yaml:"dns,omitempty"` // Parsed by specific provider
    // ...
}
```

Each provider converts the map to its own config struct via JSON round-trip:

```go
// cloudflare/config.go
type Config struct {
    ZoneName string `yaml:"zone_name" json:"zone_name"`
}
```

## Supported Providers

### Cloudflare (Implemented)

Uses the `cloudflare-go/v4` SDK for direct API calls.

**Configuration:**
```yaml
dns_provider: cloudflare
dns:
  zone_name: example.com
```

**Environment Variables:**
- `CLOUDFLARE_API_TOKEN` - API token with Zone:Read and DNS:Edit permissions

**Behavior:**
- On deploy: creates root domain (A or CNAME) and wildcard (`*.domain`) records pointing to the LB endpoint
- On destroy: removes both root and wildcard records (checks both A and CNAME types)
- Idempotent: handles create, update, duplicate cleanup, and no-op cases

### Future Providers

**AWS Route53:**
```yaml
dns_provider: route53
dns:
  hosted_zone_id: Z1234567890ABC  # Optional
  zone_name: example.com
```

**Azure DNS:**
```yaml
dns_provider: azure-dns
dns:
  resource_group: my-rg
  zone_name: example.com
```

**Google Cloud DNS:**
```yaml
dns_provider: google-dns
dns:
  project: my-project
  zone_name: example.com
```

## Deploy/Destroy Integration

### Deploy Flow

After infrastructure is provisioned and the load balancer is ready:

1. Retrieve kubeconfig from the cloud provider
2. Poll for a LoadBalancer Service in the `envoy-gateway-system` namespace for an external IP or hostname
3. Determine record type: A for IP addresses, CNAME for hostnames
4. Call `ProvisionRecords()` to create/update root and wildcard records
5. If DNS provisioning fails, log a warning and print manual DNS instructions

### Destroy Flow

Before infrastructure destruction begins:

1. Call `DestroyRecords()` to remove root and wildcard records
2. Both A and CNAME record types are checked (since the original type is not stored)
3. If DNS cleanup fails, log a warning and continue with infrastructure destruction

### Error Handling

DNS operations never block the deploy or destroy workflow. All DNS errors produce warnings:

```go
if err := dnsProvider.ProvisionRecords(ctx, cfg.Domain, cfg.DNS, lbEndpointStr); err != nil {
    slog.Warn("Failed to provision DNS records", "error", err)
    slog.Warn("You can configure DNS manually - see instructions below")
}
```

## Known Limitations

### Orphaned Records on Domain Change

When the `domain` field in the configuration is changed and the deployment is redeployed, new DNS records are created for the new domain but the previous domain's records are **not** automatically cleaned up. This is a consequence of the stateless design - the provider has no knowledge of previous domain values.

**Workaround:** Manually delete the old DNS records from your DNS provider's dashboard or API before or after changing the domain.

**Potential future solutions:**
- Use DNS record comments/tags to identify NIC-managed records for cleanup
- Store the previous domain in local state for diff-based cleanup

## OpenTelemetry Instrumentation

All DNS provider methods include telemetry spans:

```go
func (p *Provider) ProvisionRecords(ctx context.Context, domain string, dnsConfig map[string]any, lbEndpoint string) error {
    tracer := otel.Tracer("nebari-infrastructure-core")
    ctx, span := tracer.Start(ctx, "cloudflare.ProvisionRecords")
    defer span.End()

    span.SetAttributes(
        attribute.String("domain", domain),
        attribute.String("zone_name", cfCfg.ZoneName),
        attribute.String("endpoint", lbEndpoint),
        attribute.String("record_type", recType),
    )

    // ... implementation ...
}
```

The SDK adapter layer (`sdkClient`) is also instrumented, providing fine-grained spans for individual API calls (ResolveZoneID, ListDNSRecords, CreateDNSRecord, etc.).

## Testing

### Unit Tests

18 table-driven tests with full mock coverage via the `CloudflareClient` interface:

```bash
# Test DNS provider registry
go test ./pkg/dnsprovider -v -cover

# Test Cloudflare provider
go test ./pkg/dnsprovider/cloudflare -v -cover
```

Test cases cover:
- Provision: create, update, no-op, duplicate cleanup, IP vs hostname detection
- Destroy: existing records, already-gone records, mixed record types
- Error handling: missing token, missing zone, API failures
- Config validation: missing zone_name, domain outside zone

### Integration Tests (Future)

Test against actual DNS provider APIs:
- Create test zones
- Perform CRUD operations on records
- Verify record propagation
- Clean up test resources

## Security Considerations

1. **Never commit `.env` files** - Gitignored by default
2. **Use scoped API tokens** - Minimum required permissions (Zone:Read, DNS:Edit)
3. **Rotate credentials regularly** - Especially for production
4. **Audit DNS changes** - All record modifications logged via slog and traced via OpenTelemetry
5. **Validate zone ownership** - Domain must be within the configured zone (suffix check with dot separator)
6. **Use TLS for API calls** - The cloudflare-go SDK uses HTTPS

## Dependencies

```go
require (
    github.com/cloudflare/cloudflare-go/v4 v4.6.0  // Cloudflare API SDK
)
```

Note: Future providers (AWS Route53, Azure DNS, Google Cloud DNS) may be managed via OpenTofu modules rather than direct SDK calls.

## Related Documentation

- [OpenTofu Module Architecture](./06-opentofu-module-architecture.md) - Infrastructure module design
- [Configuration Design](./07-configuration-design.md) - Config parsing
- [State Management](../architecture/05-state-management.md) - Terraform state backends
