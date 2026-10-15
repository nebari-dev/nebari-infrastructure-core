package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

var (
	destroyConfigFile  string
	destroyAutoApprove bool
	destroyForce       bool
	destroyTimeout     string
	destroyDryRun      bool
	destroyLogFile     logFileFlags

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
	addLogFileFlags(destroyCmd, &destroyLogFile)
}

func runDestroy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	configFile, err := resolveConfigFile(destroyConfigFile)
	if err != nil {
		return err
	}

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", configFile),
		attribute.Bool("auto_approve", destroyAutoApprove),
		attribute.Bool("force", destroyForce),
		attribute.Bool("dry_run", destroyDryRun),
	)

	var timeout time.Duration
	if destroyTimeout != "" {
		timeout, err = time.ParseDuration(destroyTimeout)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("invalid timeout duration %q: %w", destroyTimeout, err)
		}
		span.SetAttributes(attribute.String("timeout", destroyTimeout))
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

	ctx, out := startCommandOutput(ctx, "destroy", cfg, destroyHeader(cfg, configFile), destroyLogFile, destroyDryRun)

	opts := nic.DestroyOptions{
		DryRun:  destroyDryRun,
		Force:   destroyForce,
		Timeout: timeout,
	}
	if !destroyAutoApprove {
		opts.Confirm = func(ctx context.Context, s nic.DestroySummary) error {
			// The prompt shares the terminal with the live progress line.
			out.pause()
			defer out.resume()
			return confirmDestruction(ctx, s)
		}
	}

	err = client.Destroy(ctx, cfg, opts)
	out.finish(err)
	if err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// destroyHeader names the destroy for the renderer's header and result line.
func destroyHeader(cfg *config.NebariConfig, configFile string) output.Header {
	h := output.Header{
		Command: "nic destroy",
		Title:   "Destroying " + cfg.ProjectName,
		Context: []string{cfg.Cluster.ProviderName(), configFile},
		Done:    "Destroyed " + cfg.ProjectName,
		Failed:  "Destroy failed",
	}
	if destroyDryRun {
		h.Command += " --dry-run"
		h.Title = "Planning the destroy of " + cfg.ProjectName
		h.Done = "Dry run finished for " + cfg.ProjectName
		h.Failed = "Dry run failed"
	}
	return h
}

// confirmDestruction renders the destroy warning panel and reads a yes/no
// response from stdin. Returns a non-nil error when the user does not type
// "yes", which causes Client.Destroy to abort.
func confirmDestruction(_ context.Context, s nic.DestroySummary) error {
	fmt.Println("\n⚠️  WARNING: You are about to destroy the following infrastructure:")
	fmt.Printf("   Provider:     %s\n", s.Provider)
	fmt.Printf("   Project Name: %s\n", s.ProjectName)

	for key, value := range s.Details {
		pad := max(13-len(key), 1)
		fmt.Printf("   %s:%s%s\n", key, strings.Repeat(" ", pad), value)
	}

	fmt.Println("\n❌ This will permanently delete all resources and data.")
	fmt.Println("   This action cannot be undone.")
	fmt.Print("\nDo you want to continue? Type 'yes' to confirm: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read user input: %w", err)
	}

	if strings.TrimSpace(response) != "yes" {
		return fmt.Errorf("destruction cancelled (user did not type 'yes')")
	}

	fmt.Println()
	return nil
}
