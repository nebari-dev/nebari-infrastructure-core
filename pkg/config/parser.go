package config

import (
	"context"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// ParseConfig parses a nebari-config.yaml file and returns the configuration.
// This function uses lenient parsing - it only validates that the provider field
// exists and is valid. Additional validation can be added later.
func ParseConfig(ctx context.Context, filePath string) (*NebariConfig, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "config.ParseConfig")
	defer span.End()

	span.SetAttributes(attribute.String("config.file", filePath))

	// Read the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to read config file %s: %w", filePath, err)
	}

	// Parse YAML
	var config NebariConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to parse config file %s: %w", filePath, err)
	}

	// Validate provider field (lenient - only check this required field)
	if config.Provider == "" {
		err := fmt.Errorf("provider field is required in config")
		span.RecordError(err)
		return nil, err
	}

	if !IsValidProvider(config.Provider) {
		err := fmt.Errorf("invalid provider %q, must be one of: %v", config.Provider, ValidProviders)
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(
		attribute.String("config.provider", config.Provider),
		attribute.String("config.project_name", config.ProjectName),
	)

	return &config, nil
}

// UnmarshalProviderConfig converts the any provider config to a concrete type.
// The target parameter should be a pointer to the provider-specific config struct.
// This function re-marshals and unmarshals to handle the type conversion properly.
func UnmarshalProviderConfig(ctx context.Context, providerConfig any, target any) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "config.UnmarshalProviderConfig")
	defer span.End()

	if providerConfig == nil {
		err := fmt.Errorf("provider config is nil")
		span.RecordError(err)
		return err
	}

	// Convert to YAML and back to properly unmarshal into the target type
	data, err := yaml.Marshal(providerConfig)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal provider config: %w", err)
	}

	if err := yaml.Unmarshal(data, target); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to unmarshal provider config: %w", err)
	}

	return nil
}
