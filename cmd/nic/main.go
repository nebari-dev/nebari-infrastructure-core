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

// reachedRunE reports whether cobra parsed flags and validated args
// successfully, i.e. PersistentPreRun ran and a command's RunE is about to (or
// did) execute. main() uses it to distinguish runtime failures (which we log)
// from usage-class errors (bad flag, unknown command, wrong number of args),
// which surface before PersistentPreRun and are already printed by cobra.
var reachedRunE bool

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

		// PersistentPreRun runs only after cobra has parsed flags and validated
		// args. Any failure from here on is a runtime error and not a misuse,
		// so silence cobra's own error/usage output and let main() report it
		// once via slog. Usage-class errors (bad flag, unknown command, wrong
		// number of args) surface before this hook runs, so cobra still prints
		// the error and usage block for those.
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		reachedRunE = true
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
		// Log only runtime failures (those that occur once RunE is reached) and
		// leave usage-class errors (bad flag, unknown command, bad args) to
		// cobra, which already printed them.
		if reachedRunE {
			slog.Error("Command execution failed", "error", err)
		}
		os.Exit(1)
	}
}
