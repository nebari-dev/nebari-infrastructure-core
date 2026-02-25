package main

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

var (
	validateConfigFile string
	validateCreds      bool

	validateCmd = &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration file",
		Long: `Validate the nebari-config.yaml file without deploying any infrastructure.
This command checks that the configuration file is properly formatted and contains
all required fields.

Use --validate-creds to perform thorough credential validation including permission
checks (currently supported for AWS only).`,
		RunE: runValidate,
	}
)

func init() {
	validateCmd.Flags().StringVarP(&validateConfigFile, "file", "f", "", "Path to nebari-config.yaml file (required)")
	validateCmd.Flags().BoolVar(&validateCreds, "validate-creds", false, "Perform thorough credential validation (AWS only)")
	// Panic is appropriate in init() since we cannot return errors and this indicates a programming error
	if err := validateCmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
}

func runValidate(cmd *cobra.Command, args []string) error {
	// Get cancellable context from cobra (for signal handling)
	ctx := cmd.Context()
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", validateConfigFile),
		attribute.Bool("validate_creds", validateCreds),
	)

	slog.Info("Validating configuration", "config_file", validateConfigFile)

	// Parse configuration
	cfg, err := config.ParseConfig(ctx, validateConfigFile)
	if err != nil {
		span.RecordError(err)
		slog.Error("Configuration validation failed", "error", err, "file", validateConfigFile)
		return err
	}

	slog.Info("Configuration is valid",
		"provider", cfg.Provider,
		"project_name", cfg.ProjectName,
	)

	// Get provider and validate configuration
	p, err := registry.Get(ctx, cfg.Provider)
	if err != nil {
		span.RecordError(err)
		slog.Error("Provider not available", "error", err, "provider", cfg.Provider)
		return err
	}

	// Validate provider-specific configuration
	if err := p.Validate(ctx, cfg); err != nil {
		span.RecordError(err)
		slog.Error("Provider configuration validation failed", "error", err, "provider", cfg.Provider)
		return err
	}

	fmt.Printf("✓ Configuration file is valid\n")
	fmt.Printf("  Provider: %s\n", cfg.Provider)
	fmt.Printf("  Project: %s\n", cfg.ProjectName)

	// Perform thorough credential validation if requested
	if validateCreds {
		if cv, ok := p.(provider.CredentialValidator); ok {
			slog.Info("Performing credential validation", "provider", cfg.Provider)
			if err := cv.ValidateCredentials(ctx, cfg); err != nil {
				span.RecordError(err)
				slog.Error("Credential validation failed", "error", err, "provider", cfg.Provider)
				return err
			}
			fmt.Printf("✓ Credentials are valid with required permissions\n")
		} else {
			fmt.Printf("Note: The %s provider does not support --validate-creds\n", cfg.Provider)
		}
	}

	return nil
}
