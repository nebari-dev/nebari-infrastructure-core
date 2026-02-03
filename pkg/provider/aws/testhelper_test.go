package aws

import "github.com/nebari-dev/nebari-infrastructure-core/pkg/config"

// newTestConfig creates a NebariConfig with AWS configuration for testing.
func newTestConfig(projectName string, awsCfg *Config) *config.NebariConfig {
	return &config.NebariConfig{
		ProjectName:    projectName,
		Provider:       "aws",
		ProviderConfig: map[string]any{"amazon_web_services": awsCfg},
	}
}

// newDryRunTestConfig creates a NebariConfig with DryRun enabled for testing.
func newDryRunTestConfig(projectName string, awsCfg *Config) *config.NebariConfig {
	return &config.NebariConfig{
		ProjectName:    projectName,
		Provider:       "aws",
		ProviderConfig: map[string]any{"amazon_web_services": awsCfg},
		DryRun:         true,
	}
}
