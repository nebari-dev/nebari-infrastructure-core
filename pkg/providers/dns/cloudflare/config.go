package cloudflare

// Config represents Cloudflare-specific DNS configuration
// Secrets like API tokens are read from environment variables, not config
type Config struct {
	ZoneName         string         `yaml:"zone_name" json:"zone_name"` // Domain zone (e.g., example.com)
	AdditionalFields map[string]any `yaml:",inline" json:"-"`
}
