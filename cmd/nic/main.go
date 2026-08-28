package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/cli"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/telemetry"
)

func init() {
	// Load .env file if it exists (silently ignore if not found)
	// This allows users to optionally use .env for local development
	_ = godotenv.Load()
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

	if err := cli.Execute(ctx); err != nil {
		if ctx.Err() == context.Canceled {
			slog.Info("Shutdown complete")
			os.Exit(130)
		}
		// Log only runtime failures (those that occur once RunE is reached) and
		// leave usage-class errors (bad flag, unknown command, bad args) to
		// cobra, which already printed them.
		var runErr *cli.RunError
		if errors.As(err, &runErr) {
			slog.Error("Command execution failed", "error", runErr.Err)
		}
		os.Exit(1)
	}
}
