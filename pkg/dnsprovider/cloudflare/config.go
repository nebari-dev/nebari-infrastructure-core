package cloudflare

// Config represents Cloudflare-specific DNS configuration
// Secrets like API tokens are read from environment variables, not config
type Config struct {
	ZoneName         string         `yaml:"zone_name" json:"zone_name"`             // Domain zone (e.g., example.com)
	Email            string         `yaml:"email,omitempty" json:"email,omitempty"` // Email for Let's Encrypt notifications
	AdditionalFields map[string]any `yaml:",inline" json:"-"`
}
