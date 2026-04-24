package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/action"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
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

	destroyConfigFile, err := resolveConfigFile(destroyConfigFile)
	if err != nil {
		return err
	}

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", destroyConfigFile),
		attribute.Bool("auto_approve", destroyAutoApprove),
		attribute.Bool("force", destroyForce),
		attribute.Bool("dry_run", destroyDryRun),
	)

	var timeout time.Duration
	if destroyTimeout != "" {
		timeout, err = time.ParseDuration(destroyTimeout)
		if err != nil {
			span.RecordError(err)
			slog.Error("Invalid timeout duration", "error", err, "timeout", destroyTimeout)
			return fmt.Errorf("invalid timeout duration %q: %w", destroyTimeout, err)
		}
		span.SetAttributes(attribute.String("timeout", destroyTimeout))
	}

	cfg, err := config.ParseConfig(ctx, destroyConfigFile)
	if err != nil {
		span.RecordError(err)
		slog.Error("Failed to parse configuration", "error", err, "file", destroyConfigFile)
		return err
	}

	destroy := action.Destroy{
		DryRun:  destroyDryRun,
		Force:   destroyForce,
		Timeout: timeout,
	}
	if !destroyAutoApprove {
		destroy.Confirm = confirmDestruction
	}

	if err := destroy.Run(ctx, cfg); err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}

// confirmDestruction renders the destroy warning panel and reads a yes/no
// response from stdin. Returns a non-nil error when the user does not type
// "yes", which causes action.Destroy.Run to abort.
func confirmDestruction(_ context.Context, s action.DestroySummary) error {
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
