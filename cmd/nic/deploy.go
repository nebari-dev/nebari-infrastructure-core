package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

var (
	deployConfigFile string
	deployDryRun     bool
	deployTimeout    string
	deployRegenApps  bool

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
	deployCmd.Flags().BoolVar(&deployRegenApps, "regen-apps", false, "Regenerate ArgoCD application manifests even if already bootstrapped")
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

	slog.Info("Infrastructure deployment completed", "provider", provider.Name())

	// Bootstrap GitOps repository if configured
	if cfg.GitRepository != nil && !deployDryRun {
		if err := bootstrapGitOps(ctx, cfg, deployRegenApps); err != nil {
			span.RecordError(err)
			slog.Error("GitOps bootstrap failed", "error", err)
			return err
		}
	}

	slog.Info("Deployment completed successfully", "provider", provider.Name())

	// Print DNS guidance if no DNS provider is configured
	if cfg.DNSProvider == "" && cfg.Domain != "" && !deployDryRun {
		printDNSGuidance(cfg)
	}

	return nil
}

// bootstrapGitOps initializes the GitOps repository with ArgoCD application manifests.
// This is the orchestrator function that handles all I/O operations.
func bootstrapGitOps(ctx context.Context, cfg *config.NebariConfig, regenApps bool) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.bootstrapGitOps")
	defer span.End()

	span.SetAttributes(
		attribute.String("git.url", cfg.GitRepository.URL),
		attribute.Bool("regen_apps", regenApps),
	)

	slog.Info("Initializing GitOps repository", "url", cfg.GitRepository.URL)

	// Create git client
	gitClient, err := git.NewClient(cfg.GitRepository)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create git client: %w", err)
	}
	defer func() { _ = gitClient.Cleanup() }()

	// Validate authentication before proceeding
	if err := gitClient.ValidateAuth(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("git authentication failed: %w", err)
	}

	// Clone/pull the repository
	if err := gitClient.Init(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to initialize git repository: %w", err)
	}

	// Check if already bootstrapped
	bootstrapped, err := gitClient.IsBootstrapped(ctx)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to check bootstrap status: %w", err)
	}

	if bootstrapped && !regenApps {
		slog.Info("GitOps repository already bootstrapped, skipping manifest generation")
		span.SetAttributes(attribute.Bool("skipped", true))
		return nil
	}

	if regenApps {
		slog.Info("Regenerating ArgoCD application manifests (--regen-apps)")
	} else {
		slog.Info("Bootstrapping GitOps repository with ArgoCD application manifests")
	}

	// Write all ArgoCD application manifests
	// This is the only place where filesystem I/O happens
	workDir := gitClient.WorkDir()
	err = argocd.WriteAll(ctx, func(appName string) (io.WriteCloser, error) {
		path := filepath.Join(workDir, appName+".yaml")
		slog.Debug("Writing application manifest", "app", appName, "path", path)
		return os.Create(path) //nolint:gosec // G304: path is constructed from trusted appName within controlled workDir
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write application manifests: %w", err)
	}

	// Write bootstrap marker
	if err := gitClient.WriteBootstrapMarker(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write bootstrap marker: %w", err)
	}

	// Commit and push
	commitMsg := "Bootstrap foundational ArgoCD applications"
	if regenApps {
		commitMsg = "Regenerate foundational ArgoCD applications"
	}
	if err := gitClient.CommitAndPush(ctx, commitMsg); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	slog.Info("GitOps repository bootstrapped successfully")
	return nil
}

// printDNSGuidance prints instructions for manual DNS configuration
func printDNSGuidance(cfg *config.NebariConfig) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("  DNS CONFIGURATION REQUIRED")
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("  No DNS provider is configured. To access your services, you must manually")
	fmt.Println("  configure the following DNS records with your DNS provider:")
	fmt.Println()
	fmt.Printf("  Domain: %s\n", cfg.Domain)
	fmt.Println()
	fmt.Println("  Required DNS Records:")
	fmt.Println("  ┌─────────────────────────────────────────────────────────────────────────┐")
	fmt.Println("  │ Type  │ Name                          │ Value                          │")
	fmt.Println("  ├─────────────────────────────────────────────────────────────────────────┤")
	fmt.Printf("  │ A/CNAME │ %-29s │ <load-balancer-endpoint>       │\n", cfg.Domain)
	fmt.Printf("  │ A/CNAME │ %-29s │ <load-balancer-endpoint>       │\n", "*."+cfg.Domain)
	fmt.Println("  └─────────────────────────────────────────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("  To get the load balancer endpoint, run:")
	fmt.Println()
	fmt.Printf("    kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'\n")
	fmt.Println()
	fmt.Println("  Or for IP-based load balancers:")
	fmt.Println()
	fmt.Printf("    kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'\n")
	fmt.Println()
	fmt.Println("  Note: Use CNAME records for hostname-based load balancers (AWS),")
	fmt.Println("        or A records for IP-based load balancers (GCP, Azure).")
	fmt.Println()
	fmt.Println("  To automate DNS management, add a dns_provider to your configuration:")
	fmt.Println()
	fmt.Println("    dns_provider: cloudflare")
	fmt.Println("    dns:")
	fmt.Println("      zone_name: example.com")
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
}
