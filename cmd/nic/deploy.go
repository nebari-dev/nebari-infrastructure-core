package main

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
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
	ctx := context.Background()
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.deploy")
	defer span.End()

	span.SetAttributes(attribute.String("config.file", deployConfigFile))

	slog.Info("Starting deployment", "config_file", deployConfigFile)

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
