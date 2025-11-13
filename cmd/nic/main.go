package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider/cloudflare"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/aws"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/azure"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/gcp"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/local"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/telemetry"
)

var (
	// Global provider registry
	registry *provider.Registry

	// Global DNS provider registry
	dnsRegistry *dnsprovider.Registry

	// Root command
	rootCmd = &cobra.Command{
		Use:   "nic",
		Short: "Nebari Infrastructure Core - Cloud infrastructure management for Nebari",
		Long: `Nebari Infrastructure Core (NIC) is a standalone CLI tool that manages
cloud infrastructure for Nebari using native cloud SDKs with declarative semantics.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Setup structured logging
			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))
			slog.SetDefault(logger)
		},
	}
)

func init() {
	// Load .env file if it exists (silently ignore if not found)
	// This allows users to optionally use .env for local development
	_ = godotenv.Load()

	// Initialize provider registry
	registry = provider.NewRegistry()

	// Register all providers explicitly (no blank imports or init() magic)
	ctx := context.Background()

	if err := registry.Register(ctx, "aws", aws.NewProvider()); err != nil {
		log.Fatalf("Failed to register AWS provider: %v", err)
	}

	if err := registry.Register(ctx, "gcp", gcp.NewProvider()); err != nil {
		log.Fatalf("Failed to register GCP provider: %v", err)
	}

	if err := registry.Register(ctx, "azure", azure.NewProvider()); err != nil {
		log.Fatalf("Failed to register Azure provider: %v", err)
	}

	if err := registry.Register(ctx, "local", local.NewProvider()); err != nil {
		log.Fatalf("Failed to register local provider: %v", err)
	}

	// Initialize DNS provider registry
	dnsRegistry = dnsprovider.NewRegistry()

	// Register DNS providers explicitly
	if err := dnsRegistry.Register(ctx, "cloudflare", cloudflare.NewProvider()); err != nil {
		log.Fatalf("Failed to register Cloudflare DNS provider: %v", err)
	}

	// Add subcommands
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	ctx := context.Background()

	// Setup OpenTelemetry
	_, shutdown, err := telemetry.Setup(ctx)
	if err != nil {
		slog.Error("Failed to setup telemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			slog.Error("Failed to shutdown telemetry", "error", err)
		}
	}()

	// Execute root command
	if err := rootCmd.Execute(); err != nil {
		slog.Error("Command execution failed", "error", err)
		os.Exit(1)
	}
}
