package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

var (
	deployConfigFile string
	deployDryRun     bool
	deployTimeout    string
	deployRegenApps  bool
	deployLogFile    logFileFlags

	deployCmd = &cobra.Command{
		Use:   "deploy",
		Short: "Deploy infrastructure based on configuration file",
		Long: `Deploy cloud infrastructure and Kubernetes resources based on the
provided nebari-config.yaml file. This command will create all necessary
resources to establish a fully functional Nebari cluster.

Configs that still contain the reserved CHANGEME placeholder are rejected before
any provider API call, so an unedited starter cannot provision infrastructure.

Use --dry-run to preview changes without applying them.`,
		RunE: runDeploy,
	}
)

func init() {
	deployCmd.Flags().StringVarP(&deployConfigFile, "file", "f", "", "Path to nebari-config.yaml file (auto-discovered if omitted)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "Show what would be deployed without making changes")
	deployCmd.Flags().StringVar(&deployTimeout, "timeout", "", "Override default timeout (e.g., '45m', '1h')")
	deployCmd.Flags().BoolVar(&deployRegenApps, "regen-apps", false, "Regenerate ArgoCD application manifests even if already bootstrapped")
	addLogFileFlags(deployCmd, &deployLogFile)
}

func runDeploy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	configFile, err := resolveConfigFile(deployConfigFile)
	if err != nil {
		return err
	}

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", configFile),
		attribute.Bool("dry_run", deployDryRun),
	)

	var timeout time.Duration
	if deployTimeout != "" {
		timeout, err = time.ParseDuration(deployTimeout)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("invalid timeout duration %q: %w", deployTimeout, err)
		}
		span.SetAttributes(attribute.String("timeout", deployTimeout))
	}

	cfg, err := config.ParseConfig(ctx, configFile)
	if err != nil {
		span.RecordError(err)
		return err
	}

	client, err := nic.NewClient(ctx)
	if err != nil {
		span.RecordError(err)
		return err
	}

	ctx, out := startCommandOutput(ctx, "deploy", cfg, deployHeader(cfg, configFile), deployLogFile, deployDryRun)

	result, err := client.Deploy(ctx, cfg, nic.DeployOptions{
		DryRun:    deployDryRun,
		Timeout:   timeout,
		RegenApps: deployRegenApps,
	})
	// Flush pending status messages and print the result line before the
	// guidance, so the two never interleave.
	out.finish(err)
	if err != nil {
		span.RecordError(err)
		return err
	}

	writeGuidance(deployGuidance(cfg, result, deployDryRun))
	return nil
}

// deployHeader names the deploy for the renderer's header and result line.
func deployHeader(cfg *config.NebariConfig, configFile string) output.Header {
	h := output.Header{
		Command: "nic deploy",
		Title:   "Deploying " + cfg.ProjectName,
		Context: []string{cfg.Cluster.ProviderName(), configFile},
		Done:    "Deployed " + cfg.ProjectName,
		Failed:  "Deploy failed",
	}
	if deployDryRun {
		h.Command += " --dry-run"
		h.Title = "Planning the deploy of " + cfg.ProjectName
		h.Done = "Dry run finished for " + cfg.ProjectName
		h.Failed = "Dry run failed"
	}
	return h
}
