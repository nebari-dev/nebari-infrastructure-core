# DNS Provider Architecture

**Status:** Implemented (v0.1.0 - Stub)
**Last Updated:** 2025-11-13

## Overview

The DNS Provider subsystem manages DNS records for Nebari deployments, enabling two primary use cases:

1. **Load Balancer DNS Records** - Point domain names to cluster load balancer IPs (A/AAAA or CNAME records)
2. **cert-manager Integration** - Provide DNS-01 challenge configuration for Let's Encrypt TLS certificates

## Design Principles

### 1. Pluggable Architecture
DNS providers follow the same explicit registration pattern as cloud providers:
- No blank imports or `init()` magic
- Explicit registration in `cmd/nic/main.go`
- Thread-safe registry with read/write locking
- Providers registered independently of cloud providers

### 2. Operational Interface
Unlike traditional declarative DNS management, the DNS provider interface is **operational**:
- Methods like `GetRecord()`, `AddRecord()`, `UpdateRecord()`, `DeleteRecord()`
- `EnsureRecord()` as the primary reconciliation method
- Query actual state from DNS provider APIs (stateless operation)
- No static list of records in configuration

### 3. Secrets Management
API tokens and credentials are **NEVER** stored in configuration files:
- Read exclusively from environment variables
- `.env` file support for local development (gitignored)
- `.env.example` provided as template
- Future: Kubernetes secrets, cloud provider secret managers

### 4. Configuration Separation
DNS provider configuration contains only **non-secret** data:
- Zone names, email addresses for cert-manager
- Provider-specific settings (TTLs, proxy settings)
- Secrets fetched at runtime from environment

## Architecture Components

### DNS Provider Interface

```go
type DNSProvider interface {
    // Name returns the DNS provider name (cloudflare, route53, azure-dns, etc.)
    Name() string

    // Initialize sets up the DNS provider with credentials from config
    // Validates credentials and zone access
    Initialize(ctx context.Context, config *config.NebariConfig) error

    // GetRecord retrieves a specific DNS record by name and type
    // Returns nil if record doesn't exist (not an error)
    GetRecord(ctx context.Context, name string, recordType string) (*DNSRecord, error)

    // AddRecord creates a new DNS record
    // Returns error if record already exists
    AddRecord(ctx context.Context, record DNSRecord) error

    // UpdateRecord updates an existing DNS record
    // Returns error if record doesn't exist
    UpdateRecord(ctx context.Context, record DNSRecord) error

    // DeleteRecord deletes a DNS record by name and type
    // Returns error if record doesn't exist
    DeleteRecord(ctx context.Context, name string, recordType string) error

    // EnsureRecord ensures a record exists with the given properties
    // Creates if missing, updates if different, no-op if matches
    // This is the recommended method for most use cases
    EnsureRecord(ctx context.Context, record DNSRecord) error

    // GetCertManagerConfig returns the configuration needed for cert-manager
    // to perform DNS-01 challenges (e.g., API tokens, zone info)
    // Returns provider-specific configuration as a map
    GetCertManagerConfig(ctx context.Context) (map[string]string, error)
}
```

### DNSRecord Struct

```go
type DNSRecord struct {
    Name    string // Record name (e.g., "www", "@" for root, "*" for wildcard)
    Type    string // Record type (A, AAAA, CNAME, TXT, etc.)
    Content string // Record content/value (IP address, domain, etc.)
    TTL     int    // Time to live in seconds
}
```

### Registry Pattern

DNS providers use a separate registry from cloud providers:

```go
// cmd/nic/main.go
var (
    registry    *provider.Registry       // Cloud providers
    dnsRegistry *dnsprovider.Registry   // DNS providers (separate)
)

func init() {
    // ... cloud provider registration ...

    // Initialize DNS provider registry
    dnsRegistry = dnsprovider.NewRegistry()

    // Register DNS providers explicitly
    if err := dnsRegistry.Register(ctx, "cloudflare", cloudflare.NewProvider()); err != nil {
        log.Fatalf("Failed to register Cloudflare DNS provider: %v", err)
    }
}
```

## Configuration Structure

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
  zone_name: example.com              # Your Cloudflare zone/domain
  email: admin@example.com            # Email for Let's Encrypt notifications
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

Each provider converts the map to its own config struct:

```go
// cloudflare/config.go
type Config struct {
    ZoneName string `yaml:"zone_name" json:"zone_name"`
    Email    string `yaml:"email,omitempty" json:"email,omitempty"`
}
```

## Supported Providers (v0.1.0)

### Cloudflare (Implemented - Stub)

**Configuration:**
```yaml
dns_provider: cloudflare
dns:
  zone_name: example.com
  email: admin@example.com
```

**Environment Variables:**
- `CLOUDFLARE_API_TOKEN` - API token with Zone:Read and DNS:Edit permissions

**cert-manager Integration:**
Returns configuration for Cloudflare DNS-01 solver:
```go
{
    "apiTokenSecretRef": "cloudflare-api-token",
    "email": "admin@example.com"
}
```

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

## Use Cases

### 1. Load Balancer DNS Records

When cloud provider deploys infrastructure:

```go
// Get load balancer IP from cloud provider
lbIP := "203.0.113.42"

// Initialize DNS provider
dnsProvider, _ := dnsRegistry.Get(ctx, cfg.DNSProvider)
dnsProvider.Initialize(ctx, cfg)

// Ensure A record points to load balancer
dnsProvider.EnsureRecord(ctx, dnsprovider.DNSRecord{
    Name:    "@",  // or "nebari" for subdomain
    Type:    "A",
    Content: lbIP,
    TTL:     300,
})

// Ensure wildcard for ingress
dnsProvider.EnsureRecord(ctx, dnsprovider.DNSRecord{
    Name:    "*",
    Type:    "A",
    Content: lbIP,
    TTL:     300,
})
```

### 2. cert-manager DNS-01 Challenges

When deploying cert-manager to cluster:

```go
// Get cert-manager configuration
dnsProvider, _ := dnsRegistry.Get(ctx, cfg.DNSProvider)
dnsProvider.Initialize(ctx, cfg)

certConfig, _ := dnsProvider.GetCertManagerConfig(ctx)

// Apply cert-manager ClusterIssuer with DNS-01 solver
// certConfig provides the necessary API token references and zone info
```

## Implementation Status (v0.1.0)

All DNS provider methods are **stub implementations**:

1. Parse configuration from YAML
2. Read credentials from environment variables
3. Print method calls with parameters to stdout
4. Return success without making actual API calls

### Example Output

```bash
$ export CLOUDFLARE_API_TOKEN=test123
$ ./nic deploy -f config.yaml

cloudflare.Initialize called with zone: example.com
cloudflare.EnsureRecord called:
{
  "Name": "nebari",
  "Type": "A",
  "Content": "203.0.113.42",
  "TTL": 300
}
```

## Future Enhancements (Post-v0.1.0)

### Phase 2: Native SDK Integration

1. **Cloudflare SDK** - `github.com/cloudflare/cloudflare-go`
2. **AWS Route53** - `github.com/aws/aws-sdk-go-v2/service/route53`
3. **Azure DNS** - `github.com/Azure/azure-sdk-for-go/services/dns`
4. **Google Cloud DNS** - `cloud.google.com/go/dns`

### Phase 3: Advanced Features

1. **Health Checks** - Monitor endpoint health before updating DNS
2. **Weighted Records** - Blue/green deployments with weighted routing
3. **Geo-routing** - Location-based DNS responses
4. **DNSSEC** - Cryptographic signing of DNS records
5. **External DNS Integration** - Sync with Kubernetes ExternalDNS

### Phase 4: Secrets Management

1. **Kubernetes Secrets** - Read API tokens from cluster secrets
2. **Cloud Secret Managers:**
   - AWS Secrets Manager
   - GCP Secret Manager
   - Azure Key Vault
3. **HashiCorp Vault** - Enterprise secret management

## OpenTelemetry Instrumentation

All DNS provider methods include telemetry spans:

```go
func (p *Provider) EnsureRecord(ctx context.Context, record DNSRecord) error {
    tracer := otel.Tracer("nebari-infrastructure-core")
    ctx, span := tracer.Start(ctx, "cloudflare.EnsureRecord")
    defer span.End()

    span.SetAttributes(
        attribute.String("cloudflare.zone_name", p.config.ZoneName),
        attribute.String("record.name", record.Name),
        attribute.String("record.type", record.Type),
        attribute.String("record.content", record.Content),
    )

    // ... implementation ...
}
```

## Testing

### Unit Tests

```bash
# Test DNS provider registry
go test ./pkg/dnsprovider -v -cover

# Test Cloudflare provider
go test ./pkg/dnsprovider/cloudflare -v -cover
```

### Integration Tests (Future)

Test against actual DNS provider APIs:
- Create test zones
- Perform CRUD operations on records
- Verify record propagation
- Clean up test resources

## Security Considerations

1. **Never commit `.env` files** - Gitignored by default
2. **Use scoped API tokens** - Minimum required permissions
3. **Rotate credentials regularly** - Especially for production
4. **Audit DNS changes** - Log all record modifications
5. **Validate zone ownership** - Check permissions before operations
6. **Use TLS for API calls** - All DNS provider SDKs use HTTPS

## Dependencies

### Current (v0.1.0)

```go
require (
    github.com/joho/godotenv v1.5.1  // .env file parsing
)
```

### Future (Native SDKs)

```go
require (
    github.com/cloudflare/cloudflare-go v0.x.x
    github.com/aws/aws-sdk-go-v2/service/route53 v1.x.x
    github.com/Azure/azure-sdk-for-go/services/dns v1.x.x
    cloud.google.com/go/dns v1.x.x
)
```

## Related Documentation

- [Provider Architecture](./08-provider-architecture.md) - Cloud provider design
- [Configuration Design](./07-configuration-design.md) - Config parsing
- [Stateless Operation](../architecture/06-stateless-operation.md) - Query-based design
