package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

var (
	validateConfigFile string

	validateCmd = &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration file",
		Long: `Validate the nebari-config.yaml file without deploying any infrastructure.
This command checks that the configuration file is properly formatted and contains
all required fields.`,
		RunE: runValidate,
	}
)

func init() {
	validateCmd.Flags().StringVarP(&validateConfigFile, "file", "f", "", "Path to nebari-config.yaml file (auto-discovered if omitted)")
}

func runValidate(cmd *cobra.Command, args []string) error {
	// Get cancellable context from cobra (for signal handling)
	ctx := cmd.Context()

	// Resolve config file path via auto-discovery if not explicitly provided.
	resolved, err := resolveConfigFile(validateConfigFile)
	if err != nil {
		return err
	}
	validateConfigFile = resolved

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.validate")
	defer span.End()

	span.SetAttributes(attribute.String("config.file", validateConfigFile))

	slog.Info("Validating configuration", "config_file", validateConfigFile)

	// Parse configuration
	cfg, err := config.ParseConfig(ctx, validateConfigFile)
	if err != nil {
		span.RecordError(err)
		slog.Error("Configuration validation failed", "error", err, "file", validateConfigFile)
		return err
	}

	// Validate configuration with registered providers
	if err := cfg.Validate(getValidNames(ctx, reg)); err != nil {
		span.RecordError(err)
		slog.Error("Configuration validation failed", "error", err, "file", validateConfigFile)
		return err
	}

	slog.Info("Configuration is valid",
		"provider", cfg.Cluster.ProviderName(),
		"project_name", cfg.ProjectName,
	)

	fmt.Printf("✓ Configuration file is valid\n")
	fmt.Printf("  Provider: %s\n", cfg.Cluster.ProviderName())
	fmt.Printf("  Project: %s\n", cfg.ProjectName)

	return nil
}

func getValidNames(ctx context.Context, reg *registry.Registry) config.ValidateOptions {
	return config.ValidateOptions{
		ClusterProviders: reg.ClusterProviders.List(ctx),
		DNSProviders:     reg.DNSProviders.List(ctx),
	}
}
