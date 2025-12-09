package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/aws"
)

var (
	kubeconfigConfigFile string
	kubeconfigOutputFile string

	kubeconfigCmd = &cobra.Command{
		Use:   "kubeconfig",
		Short: "Generate kubeconfig for the deployed Nebari cluster",
		Long: `Generate and output the kubeconfig file for accessing the Kubernetes
cluster deployed by Nebari. This command retrieves the necessary cluster
information and constructs a kubeconfig file that can be used with kubectl
or other Kubernetes clients.`,
		RunE: runKubeconfig,
	}
)

func init() {
	kubeconfigCmd.Flags().StringVarP(&kubeconfigConfigFile, "file", "f", "", "Path to nebari-config.yaml file (required)")
	kubeconfigCmd.Flags().StringVarP(&kubeconfigOutputFile, "output", "o", "", "Path to output kubeconfig file (defaults to stdout)")
	// Panic is appropriate in init() since we cannot return errors and this indicates a programming error
	if err := kubeconfigCmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
}

func runKubeconfig(cmd *cobra.Command, args []string) error {
	// Get cancellable context from cobra (for signal handling)
	ctx := cmd.Context()
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.kubeconfig")
	defer span.End()

	span.SetAttributes(attribute.String("config.file", kubeconfigConfigFile))

	// Parse configuration
	cfg, err := config.ParseConfig(ctx, kubeconfigConfigFile)
	if err != nil {
		span.RecordError(err)
		slog.Error("Failed to parse configuration", "error", err, "file", kubeconfigConfigFile)
		return err
	}

	slog.Info("Configuration parsed successfully",
		"provider", cfg.Provider,
		"project_name", cfg.ProjectName,
	)

	var awsCfg aws.Config
    if err := config.UnmarshalProviderConfig(ctx, cfg.AmazonWebServices, &awsCfg); err != nil {
		slog.Error("Failed to unmarshal AWS configuration", "error", err)
		return err
    }
	
	awsProvider := aws.NewProvider()

    kubeconfigBytes, err := awsProvider.GetKubeconfigWithRegion(ctx, cfg.ProjectName, awsCfg.Region)
    if err != nil {
        slog.Error("Failed to generate kubeconfig", "error", err)
		return err
    }

	if kubeconfigOutputFile != "" {
		if err := os.WriteFile(kubeconfigOutputFile, kubeconfigBytes, 0600); err != nil {
			span.RecordError(err)
			slog.Error("Failed to write kubeconfig file", "error", err, "file", kubeconfigOutputFile)
			return err
		}
		slog.Info("Kubeconfig written successfully", "file", kubeconfigOutputFile)
	} else {
		slog.Info("Kubeconfig written successfully to stdout")
    	os.Stdout.Write(kubeconfigBytes)
	}

	return nil
}
