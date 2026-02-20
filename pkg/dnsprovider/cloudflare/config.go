package cloudflare

// Config represents Cloudflare DNS provider configuration.
// API credentials (CLOUDFLARE_API_TOKEN) must be set via environment variables.
type Config struct {
	// ZoneName is the DNS zone/domain to manage (e.g., example.com)
	ZoneName string `yaml:"zone_name" json:"zone_name"`
	// AdditionalFields captures any extra Cloudflare-specific configuration
	AdditionalFields map[string]any `yaml:",inline" json:"-"`
}
