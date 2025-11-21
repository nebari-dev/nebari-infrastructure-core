package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

var (
	deployConfigFile string
	deployDryRun     bool
	deployTimeout    string

	deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy infrastructure based on configuration file",
		Long: `Deploy cloud infrastructure and Kubernetes resources based on the
provided nebari-config.yaml file. This command will create all necessary
resources to establish a fully functional Nebari cluster.

Use --dry-run to preview changes without applying them.`,
		RunE: runDeploy,
	}
)

func init() {
	deployCmd.Flags().StringVarP(&deployConfigFile, "file", "f", "", "Path to nebari-config.yaml file (required)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "Show what would be deployed without making changes")
	deployCmd.Flags().StringVar(&deployTimeout, "timeout", "", "Override default timeout (e.g., '45m', '1h')")
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

	span.SetAttributes(
		attribute.String("config.file", deployConfigFile),
		attribute.Bool("dry_run", deployDryRun),
	)

	if deployDryRun {
		slog.Info("Starting deployment (dry-run)", "config_file", deployConfigFile)
	} else {
		slog.Info("Starting deployment", "config_file", deployConfigFile)
	}

	// Setup status handler for progress updates
	ctx, cleanupStatus := status.StartHandler(ctx, statusLogHandler())
	defer cleanupStatus()

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

	// Set runtime options from CLI flags
	cfg.DryRun = deployDryRun

	// Apply custom timeout if specified
	if deployTimeout != "" {
		duration, err := time.ParseDuration(deployTimeout)
		if err != nil {
			span.RecordError(err)
			slog.Error("Invalid timeout duration", "error", err, "timeout", deployTimeout)
			return fmt.Errorf("invalid timeout duration %q: %w", deployTimeout, err)
		}
		cfg.Timeout = duration
		span.SetAttributes(attribute.String("timeout", deployTimeout))
		slog.Info("Using custom timeout", "timeout", duration)
	}

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
