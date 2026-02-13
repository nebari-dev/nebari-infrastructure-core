package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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
	rootCmd.AddCommand(destroyCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(kubeconfigCmd)
}

func main() {
	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Launch goroutine to handle signals
	go func() {
		sig := <-sigChan
		slog.Warn("Received interrupt signal, initiating graceful shutdown", "signal", sig.String())
		cancel() // Cancel context to stop all operations
	}()

	// Setup OpenTelemetry
	_, shutdown, err := telemetry.Setup(ctx)
	if err != nil {
		slog.Error("Failed to setup telemetry", "error", err)
		os.Exit(1) //nolint:gocritic // TODO: refactor to run() pattern to allow defers to run
	}
	defer func() {
		// Use a fresh context for shutdown in case main context is cancelled
		shutdownCtx := context.Background()
		if err := shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown telemetry", "error", err)
		}
	}()

	// Execute root command with cancellable context
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// Don't log error if it was due to context cancellation (graceful shutdown)
		if ctx.Err() == context.Canceled {
			slog.Info("Shutdown complete")
			os.Exit(130) // Exit code 130 indicates terminated by Ctrl+C
		}
		slog.Error("Command execution failed", "error", err)
		os.Exit(1)
	}
}
