package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/cmd/nic/renderer"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

var (
	validateConfigFile string

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
	validateCmd.Flags().StringVarP(&validateConfigFile, "file", "f", "", "Path to nebari-config.yaml file (auto-discovered if omitted)")
}

func runValidate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	r := renderer.FromContext(ctx)

	resolved, err := resolveConfigFile(validateConfigFile)
	if err != nil {
		return err
	}
	validateConfigFile = resolved

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.validate")
	defer span.End()

	span.SetAttributes(attribute.String("config.file", validateConfigFile))

	start := time.Now()

	cfg, err := config.ParseConfig(ctx, validateConfigFile)
	if err != nil {
		span.RecordError(err)
		r.Error(err, "Check your config file syntax")
		return err
	}

	if err := cfg.Validate(getValidNames(ctx, reg)); err != nil {
		span.RecordError(err)
		r.Error(err, "")
		return err
	}

	r.EndStep(renderer.StepOK, time.Since(start), "Configuration is valid")
	r.Info("  Provider: " + cfg.Cluster.ProviderName())
	r.Info("  Project:  " + cfg.ProjectName)

	return nil
}

func getValidNames(ctx context.Context, reg *registry.Registry) config.ValidateOptions {
	return config.ValidateOptions{
		ClusterProviders: reg.ClusterProviders.List(ctx),
		DNSProviders:     reg.DNSProviders.List(ctx),
	}
}
