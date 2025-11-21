package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

var (
	deployConfigFile string

	deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy infrastructure based on configuration file",
		Long: `Deploy cloud infrastructure and Kubernetes resources based on the
provided nebari-config.yaml file. This command will create all necessary
resources to establish a fully functional Nebari cluster.`,
		RunE: runDeploy,
	}
)

func init() {
	deployCmd.Flags().StringVarP(&deployConfigFile, "file", "f", "", "Path to nebari-config.yaml file (required)")
	// Panic is appropriate in init() since we cannot return errors and this indicates a programming error
	if err := deployCmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
}

func runDeploy(cmd *cobra.Command, args []string) error {
	// Get cancellable context from cobra (for signal handling)
	ctx := cmd.Context()
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.deploy")
	defer span.End()

	span.SetAttributes(attribute.String("config.file", deployConfigFile))

	slog.Info("Starting deployment", "config_file", deployConfigFile)

	// Create status channel for progress updates
	statusCh := make(chan status.Update, 100)
	ctx = status.WithChannel(ctx, statusCh)

	// Start goroutine to log status updates
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		logUpdates(statusCh)
	}()

	// Ensure status channel is closed and all messages are logged before exit
	defer func() {
		close(statusCh)

		// Wait for logger goroutine with timeout to prevent hanging
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All status messages logged successfully
		case <-time.After(5 * time.Second):
			slog.Warn("Timeout waiting for status messages to flush, some messages may be lost")
		}
	}()

	// Handle context cancellation (from signal interrupt)
	defer func() {
		if ctx.Err() == context.Canceled {
			slog.Warn("Deployment interrupted by user")
		}
	}()

	// Parse configuration
	cfg, err := config.ParseConfig(ctx, deployConfigFile)
	if err != nil {
		span.RecordError(err)
		slog.Error("Failed to parse configuration", "error", err, "file", deployConfigFile)
		return err
	}

	slog.Info("Configuration parsed successfully",
		"provider", cfg.Provider,
		"project_name", cfg.ProjectName,
	)

	// Get the appropriate provider
	provider, err := registry.Get(ctx, cfg.Provider)
	if err != nil {
		span.RecordError(err)
		slog.Error("Failed to get provider", "error", err, "provider", cfg.Provider)
		return err
	}

	slog.Info("Provider selected", "provider", provider.Name())

	// Deploy infrastructure
	if err := provider.Deploy(ctx, cfg); err != nil {
		span.RecordError(err)
		slog.Error("Deployment failed", "error", err, "provider", provider.Name())
		return err
	}

	slog.Info("Deployment completed successfully", "provider", provider.Name())

	return nil
}

// logUpdates reads status updates from the channel and logs them
// This function runs in a goroutine and exits when the channel is closed
func logUpdates(statusCh <-chan status.Update) {
	for update := range statusCh {
		// Build structured logging attributes
		attrs := []any{
			"message", update.Message,
		}

		if update.Resource != "" {
			attrs = append(attrs, "resource", update.Resource)
		}

		if update.Action != "" {
			attrs = append(attrs, "action", update.Action)
		}

		// Add metadata as individual attributes
		for key, value := range update.Metadata {
			attrs = append(attrs, key, value)
		}

		// Log at appropriate level
		switch update.Level {
		case status.LevelInfo:
			slog.Info("Status", attrs...)
		case status.LevelProgress:
			slog.Info("Progress", attrs...)
		case status.LevelSuccess:
			slog.Info("Success", attrs...)
		case status.LevelWarning:
			slog.Warn("Warning", attrs...)
		case status.LevelError:
			slog.Error("Error", attrs...)
		default:
			slog.Info("Status", attrs...)
		}
	}
}
