package main

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
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
	ctx := cmd.Context()

	validateConfigFile, err := resolveConfigFile(validateConfigFile)
	if err != nil {
		return err
	}

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.validate")
	defer span.End()

	span.SetAttributes(attribute.String("config.file", validateConfigFile))

	cfg, err := config.ParseConfig(ctx, validateConfigFile)
	if err != nil {
		span.RecordError(err)
		slog.Error("Configuration validation failed", "error", err, "file", validateConfigFile)
		return err
	}

	client, err := nic.NewClient()
	if err != nil {
		span.RecordError(err)
		slog.Error("Failed to create NIC client", "error", err)
		return err
	}
	if err := client.Validate(ctx, cfg); err != nil {
		span.RecordError(err)
		slog.Error("Configuration validation failed", "error", err, "file", validateConfigFile)
		return err
	}

	fmt.Printf("✓ Configuration file is valid\n")
	fmt.Printf("  Provider: %s\n", cfg.Cluster.ProviderName())
	fmt.Printf("  Project: %s\n", cfg.ProjectName)

	return nil
}
