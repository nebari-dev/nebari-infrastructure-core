package config

import (
	"context"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// ParseConfigBytes parses YAML configuration from bytes.
// This is the core parsing logic, separated from file I/O for testability.
// Call Validate on the returned config to check for semantic errors.
//
// The source bytes are retained on the returned config so checks that must read
// the original document still run for this entrypoint. Without that, a caller
// parsing bytes directly and then deploying would silently skip placeholder
// rejection: the gate treats an absent source as "no YAML to scan" and returns
// nil, which is right for a config built in Go but wrong for one parsed here.
// There is no path to record, so such an error names the fields but not a file.
func ParseConfigBytes(data []byte) (*NebariConfig, error) {
	var config NebariConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	config.sourceRaw = data

	return &config, nil
}

// ParseConfig reads and parses a nebari-config.yaml file.
// This is a convenience wrapper around ParseConfigBytes that handles file I/O.
func ParseConfig(ctx context.Context, filePath string) (*NebariConfig, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "config.ParseConfig")
	defer span.End()

	span.SetAttributes(attribute.String("config.file", filePath))

	data, err := os.ReadFile(filePath)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to read config file %s: %w", filePath, err)
	}

	config, err := ParseConfigBytes(data)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("config file %s: %w", filePath, err)
	}

	// ParseConfigBytes already retained the bytes; add the path so errors can
	// name the file to edit. Keeping the source on the parse seam is what lets
	// placeholder rejection live in pkg/nic next to the other per-command
	// validators instead of above it in cmd/nic.
	config.sourcePath = filePath

	span.SetAttributes(
		attribute.String("config.provider", config.Cluster.ProviderName()),
		attribute.String("config.project_name", config.ProjectName),
	)

	return config, nil
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
