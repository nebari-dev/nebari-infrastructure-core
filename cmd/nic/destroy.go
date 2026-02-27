package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

var (
	destroyConfigFile  string
	destroyAutoApprove bool
	destroyForce       bool
	destroyTimeout     string
	destroyDryRun      bool

	destroyCmd = &cobra.Command{
		Use:   "destroy",
		Short: "Destroy cloud infrastructure",
		Long: `Destroys all infrastructure resources in reverse order of creation.
This includes node groups, EKS cluster, VPC, and IAM roles.

WARNING: This operation is destructive and cannot be undone. All data will be lost.

By default, you will be prompted to confirm before destruction begins.
Use --auto-approve to skip the confirmation prompt.`,
		RunE: runDestroy,
	}
)

func init() {
	destroyCmd.Flags().StringVarP(&destroyConfigFile, "file", "f", "", "Path to nebari-config.yaml file (required)")
	destroyCmd.Flags().BoolVar(&destroyAutoApprove, "auto-approve", false, "Skip confirmation prompt and destroy immediately")
	destroyCmd.Flags().BoolVar(&destroyForce, "force", false, "Continue destruction even if some resources fail to delete")
	destroyCmd.Flags().StringVar(&destroyTimeout, "timeout", "", "Override default timeout (e.g., '45m', '1h')")
	destroyCmd.Flags().BoolVar(&destroyDryRun, "dry-run", false, "Show what would be destroyed without actually deleting")

	// Panic is appropriate in init() since we cannot return errors and this indicates a programming error
	if err := destroyCmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
}

func runDestroy(cmd *cobra.Command, args []string) error {
	// Get cancellable context from cobra (for signal handling)
	ctx := cmd.Context()
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", destroyConfigFile),
		attribute.Bool("auto_approve", destroyAutoApprove),
		attribute.Bool("force", destroyForce),
		attribute.Bool("dry_run", destroyDryRun),
	)

	slog.Info("Starting infrastructure destruction", "config_file", destroyConfigFile)

	// Parse configuration first to show user what will be destroyed
	cfg, err := config.ParseConfig(ctx, destroyConfigFile)
	if err != nil {
		span.RecordError(err)
		slog.Error("Failed to parse configuration", "error", err, "file", destroyConfigFile)
		return err
	}

	slog.Info("Configuration parsed successfully",
		"provider", cfg.Provider,
		"project_name", cfg.ProjectName,
	)

	// Set runtime options from CLI flags
	cfg.DryRun = destroyDryRun
	cfg.Force = destroyForce

	// Apply custom timeout if specified
	if destroyTimeout != "" {
		duration, err := time.ParseDuration(destroyTimeout)
		if err != nil {
			span.RecordError(err)
			slog.Error("Invalid timeout duration", "error", err, "timeout", destroyTimeout)
			return fmt.Errorf("invalid timeout duration %q: %w", destroyTimeout, err)
		}
		cfg.Timeout = duration
		span.SetAttributes(attribute.String("timeout", destroyTimeout))
		slog.Info("Using custom timeout", "timeout", duration)
	}

	// Get the appropriate provider
	prov, err := registry.Get(ctx, cfg.Provider)
	if err != nil {
		span.RecordError(err)
		slog.Error("Failed to get provider", "error", err, "provider", cfg.Provider)
		return err
	}

	slog.Info("Provider selected", "provider", prov.Name())

	// Show what will be destroyed and get confirmation (skip for dry-run)
	if !destroyAutoApprove && !destroyDryRun {
		if err := confirmDestruction(cfg, prov); err != nil {
			span.RecordError(err)
			slog.Info("Destruction cancelled by user")
			return err
		}
	}

	// Setup status handler for progress updates
	ctx, cleanupStatus := status.StartHandler(ctx, statusLogHandler())
	defer cleanupStatus()

	// Handle context cancellation (from signal interrupt)
	defer func() {
		if ctx.Err() == context.Canceled {
			slog.Warn("Destruction interrupted by user")
		}
	}()

	// Clean up DNS records if a DNS provider is configured
	if cfg.DNS != nil {
		err := destroyDNS(ctx, cfg)
		if err != nil {
			slog.Warn("Failed to clean up DNS records", "error", err)
			slog.Warn("You may need to manually remove DNS records from your provider")
		}
	}

	// Destroy infrastructure
	if err := prov.Destroy(ctx, cfg); err != nil {
		span.RecordError(err)
		slog.Error("Destruction failed", "error", err, "provider", prov.Name())
		if destroyForce {
			slog.Warn("Continuing despite errors due to --force flag")
		} else {
			return err
		}
	}

	slog.Info("Destruction completed successfully", "provider", prov.Name())

	return nil
}

func destroyDNS(ctx context.Context, cfg *config.NebariConfig) error {
	if destroyDryRun {
		slog.Info("Would clean up DNS records (dry-run)", "provider", cfg.DNS.ProviderName(), "domain", cfg.Domain)
		return nil
	}

	dnsProvider, err := dnsRegistry.Get(ctx, cfg.DNS.ProviderName())
	if err != nil {
		return err
	}
	slog.Info("Cleaning up DNS records", "provider", cfg.DNS.ProviderName(), "domain", cfg.Domain)

	if err := dnsProvider.DestroyRecords(ctx, cfg.Domain, cfg.DNS.ProviderConfig()); err != nil {
		return err
	}

	slog.Info("DNS records cleaned up successfully", "domain", cfg.Domain)
	return nil
}

// confirmDestruction prompts the user to confirm before destroying infrastructure
func confirmDestruction(cfg *config.NebariConfig, prov provider.Provider) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(context.Background(), "cmd.confirmDestruction")
	defer span.End()

	// Show warning message
	fmt.Println("\n⚠️  WARNING: You are about to destroy the following infrastructure:")
	fmt.Printf("   Provider:     %s\n", cfg.Provider)
	fmt.Printf("   Project Name: %s\n", cfg.ProjectName)

	// Show provider-specific details
	for key, value := range prov.Summary(cfg) {
		fmt.Printf("   %s:%s%s\n", key, strings.Repeat(" ", 13-len(key)), value)
	}

	fmt.Println("\n❌ This will permanently delete all resources and data.")
	fmt.Println("   This action cannot be undone.")
	fmt.Print("\nDo you want to continue? Type 'yes' to confirm: ")

	// Read user input
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to read user input: %w", err)
	}

	// Trim whitespace and newlines
	response = strings.TrimSpace(response)

	// Check if user confirmed
	if response != "yes" {
		span.SetAttributes(attribute.String("user_response", response))
		return fmt.Errorf("destruction cancelled (user did not type 'yes')")
	}

	span.SetAttributes(attribute.Bool("confirmed", true))
	fmt.Println()
	return nil
}
