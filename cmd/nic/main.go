package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/telemetry"
)

var rootCmd = &cobra.Command{
	Use:   "nic",
	Short: "Nebari Infrastructure Core - Cloud infrastructure management for Nebari",
	Long: `Nebari Infrastructure Core (NIC) is a standalone CLI tool that manages
cloud infrastructure for Nebari using native cloud SDKs with declarative semantics.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
		slog.SetDefault(logger)
	},
}

func init() {
	// Load .env file if it exists (silently ignore if not found)
	// This allows users to optionally use .env for local development
	_ = godotenv.Load()

	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(destroyCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(kubeconfigCmd)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		slog.Warn("Received interrupt signal, initiating graceful shutdown", "signal", sig.String())
		cancel()
	}()

	_, shutdown, err := telemetry.Setup(ctx)
	if err != nil {
		slog.Error("Failed to setup telemetry", "error", err)
		os.Exit(1) //nolint:gocritic // TODO: refactor to run() pattern to allow defers to run
	}
	defer func() {
		shutdownCtx := context.Background()
		if err := shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown telemetry", "error", err)
		}
	}()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		if ctx.Err() == context.Canceled {
			slog.Info("Shutdown complete")
			os.Exit(130)
		}
		slog.Error("Command execution failed", "error", err)
		os.Exit(1)
	}
}
