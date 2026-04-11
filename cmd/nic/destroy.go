package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/cmd/nic/renderer"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

var (
	destroyConfigFile  string
	destroyAutoApprove bool
	destroyForce       bool
	destroyTimeout     string
	destroyDryRun      bool

	destroyCmd = &cobra.Command{
		Use:   "destroy",
		Short: "Destroy cloud infrastructure",
		Long: `Destroys all infrastructure resources in reverse order of creation.
This includes node groups, EKS cluster, VPC, and IAM roles.

WARNING: This operation is destructive and cannot be undone. All data will be lost.

By default, you will be prompted to confirm before destruction begins.
Use --auto-approve to skip the confirmation prompt.`,
		RunE: runDestroy,
	}
)

func init() {
	destroyCmd.Flags().StringVarP(&destroyConfigFile, "file", "f", "", "Path to nebari-config.yaml file (auto-discovered if omitted)")
	destroyCmd.Flags().BoolVar(&destroyAutoApprove, "auto-approve", false, "Skip confirmation prompt and destroy immediately")
	destroyCmd.Flags().BoolVar(&destroyForce, "force", false, "Continue destruction even if some resources fail to delete")
	destroyCmd.Flags().StringVar(&destroyTimeout, "timeout", "", "Override default timeout (e.g., '45m', '1h')")
	destroyCmd.Flags().BoolVar(&destroyDryRun, "dry-run", false, "Show what would be destroyed without actually deleting")
}

func runDestroy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	r := renderer.FromContext(ctx)

	resolved, err := resolveConfigFile(destroyConfigFile)
	if err != nil {
		return err
	}
	destroyConfigFile = resolved

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", destroyConfigFile),
		attribute.Bool("auto_approve", destroyAutoApprove),
		attribute.Bool("force", destroyForce),
		attribute.Bool("dry_run", destroyDryRun),
	)

	cfg, err := config.ParseConfig(ctx, destroyConfigFile)
	if err != nil {
		span.RecordError(err)
		r.Error(err, "")
		return err
	}

	if err := cfg.Validate(getValidNames(ctx, reg)); err != nil {
		span.RecordError(err)
		r.Error(err, "")
		return err
	}

	var timeout time.Duration
	if destroyTimeout != "" {
		timeout, err = time.ParseDuration(destroyTimeout)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("invalid timeout duration %q: %w", destroyTimeout, err)
		}
		span.SetAttributes(attribute.String("timeout", destroyTimeout))
	}

	prov, err := reg.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Confirmation prompt
	if !destroyAutoApprove && !destroyDryRun {
		details := map[string]string{
			"Provider":     cfg.Cluster.ProviderName(),
			"Project Name": cfg.ProjectName,
		}
		for k, v := range prov.Summary(cfg.Cluster) {
			details[k] = v
		}
		confirmed, err := r.Confirm(
			fmt.Sprintf("You are about to destroy: %s (%s)", cfg.ProjectName, cfg.Cluster.ProviderName()),
			details,
			"yes",
		)
		if err != nil {
			span.RecordError(err)
			return err
		}
		if !confirmed {
			r.Info("Destruction cancelled by user")
			return fmt.Errorf("destruction cancelled (user did not type 'yes')")
		}
	}

	// Setup status handler
	ctx, cleanupStatus := status.StartHandler(ctx, statusRendererHandler(r))
	defer cleanupStatus()

	defer func() {
		if ctx.Err() == context.Canceled {
			r.Warn("Destruction interrupted by user")
		}
	}()

	// DNS cleanup
	if cfg.DNS != nil {
		r.StartPhase("DNS Cleanup")
		dnsStart := time.Now()
		if err := destroyDNS(ctx, cfg, r); err != nil {
			r.Warn(fmt.Sprintf("Failed to clean up DNS records: %v", err))
			r.Warn("You may need to manually remove DNS records from your provider")
		}
		r.EndPhase(renderer.PhaseOK, time.Since(dnsStart))
	}

	// Infrastructure destruction
	r.StartPhase("Infrastructure")
	destroyStart := time.Now()

	if err := prov.Destroy(ctx, cfg.ProjectName, cfg.Cluster, provider.DestroyOptions{DryRun: destroyDryRun, Force: destroyForce, Timeout: timeout}); err != nil {
		span.RecordError(err)
		r.Error(err, "")
		if destroyForce {
			r.Warn("Continuing despite errors due to --force flag")
		} else {
			r.EndPhase(renderer.PhaseFailed, time.Since(destroyStart))
			return err
		}
	}

	r.EndStep(renderer.StepOK, time.Since(destroyStart), "Infrastructure destroyed")
	r.EndPhase(renderer.PhaseOK, time.Since(destroyStart))

	r.EndStep(renderer.StepOK, 0, "Destruction completed successfully")
	return nil
}

func destroyDNS(ctx context.Context, cfg *config.NebariConfig, r renderer.Renderer) error {
	if destroyDryRun {
		r.Info(fmt.Sprintf("Would clean up DNS records (dry-run): %s", cfg.Domain))
		return nil
	}

	dnsProvider, err := reg.DNSProviders.Get(ctx, cfg.DNS.ProviderName())
	if err != nil {
		return err
	}

	dnsStart := time.Now()
	if err := dnsProvider.DestroyRecords(ctx, cfg.Domain, cfg.DNS.ProviderConfig()); err != nil {
		return err
	}

	r.EndStep(renderer.StepOK, time.Since(dnsStart), fmt.Sprintf("DNS records cleaned up (%s)", cfg.Domain))
	return nil
}
