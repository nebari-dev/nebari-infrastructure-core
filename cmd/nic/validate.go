package main

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider"
)

var (
	validateConfigFile string
	validateCheckCreds bool

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
	validateCmd.Flags().StringVarP(&validateConfigFile, "file", "f", "", "Path to nebari-config.yaml file (required)")
	validateCmd.Flags().BoolVar(&validateCheckCreds, "check-creds", false, "Also verify DNS credentials against the live provider API (requires network access)")
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

	span.SetAttributes(attribute.String("config.file", validateConfigFile))

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

	// Verify provider is registered
	if _, err := registry.Get(ctx, cfg.Provider); err != nil {
		span.RecordError(err)
		slog.Error("Provider not available", "error", err, "provider", cfg.Provider)
		return err
	}

	// Validate DNS configuration when present
	if cfg.DNS != nil {
		providerName, dnsConfig, err := cfg.DNS.Single()
		if err != nil {
			span.RecordError(err)
			slog.Error("Invalid DNS configuration", "error", err)
			return fmt.Errorf("DNS configuration error: %w", err)
		}

		dnsProvider, err := dnsRegistry.Get(ctx, providerName)
		if err != nil {
			span.RecordError(err)
			slog.Error("DNS provider not available", "error", err, "dns_provider", providerName)
			return err
		}

		opts := dnsprovider.ValidateOptions{CheckCreds: validateCheckCreds}
		if err := dnsProvider.Validate(ctx, cfg.Domain, dnsConfig, opts); err != nil {
			span.RecordError(err)
			slog.Error("DNS validation failed", "error", err, "dns_provider", providerName)
			return fmt.Errorf("DNS validation failed: %w", err)
		}

		if validateCheckCreds {
			slog.Info("DNS credentials validated", "dns_provider", providerName)
		} else {
			slog.Info("DNS configuration validated (use --check-creds to also verify credentials)", "dns_provider", providerName)
		}
	}

	fmt.Printf("✓ Configuration file is valid\n")
	fmt.Printf("  Provider: %s\n", cfg.Provider)
	fmt.Printf("  Project: %s\n", cfg.ProjectName)
	if cfg.DNS != nil {
		fmt.Printf("  DNS:      %s\n", cfg.DNS.ProviderName())
	}

	return nil
}
