package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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
	ctx, cleanupStatusFn := status.StartHandler(ctx, statusLogHandler())
	var statusCleanedUp bool
	cleanupStatus := func() {
		if !statusCleanedUp {
			statusCleanedUp = true
			cleanupStatusFn()
		}
	}
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

	// Track what was installed so we can print instructions after flushing status messages
	var argoCDInstalled, keycloakInstalled bool

	// Install Argo CD (skip in dry-run mode)
	if !cfg.DryRun {
		slog.Info("Installing Argo CD on cluster")
		if err := argocd.Install(ctx, cfg, provider); err != nil {
			// Log error but don't fail deployment
			slog.Warn("Failed to install Argo CD", "error", err)
			slog.Warn("You can install Argo CD manually with: helm install argocd argo/argo-cd --namespace argocd --create-namespace")
		} else {
			slog.Info("Argo CD installed successfully")
			argoCDInstalled = true

			// Install foundational services via Argo CD
			slog.Info("Installing foundational services")
			foundationalCfg := argocd.FoundationalConfig{
				Keycloak: argocd.KeycloakConfig{
					Enabled:               true,
					AdminUsername:         "admin",
					AdminPassword:         generateSecurePassword(rand.Reader),
					DBPassword:            generateSecurePassword(rand.Reader),
					PostgresAdminPassword: generateSecurePassword(rand.Reader),
					PostgresUserPassword:  generateSecurePassword(rand.Reader),
					RealmAdminUsername:    "admin",
					RealmAdminPassword:    generateSecurePassword(rand.Reader),
					Hostname:              "", // Will be auto-generated from domain
				},
				// Enable MetalLB only for local deployments
				MetalLB: argocd.MetalLBConfig{
					Enabled:     cfg.Provider == "local",
					AddressPool: "192.168.1.100-192.168.1.110", // Default range for local dev
				},
			}

			if err := argocd.InstallFoundationalServices(ctx, cfg, provider, foundationalCfg); err != nil {
				// Log warning but don't fail deployment
				slog.Warn("Failed to install foundational services", "error", err)
				slog.Warn("You can install foundational services manually with: kubectl apply -f pkg/foundational/")
			} else {
				slog.Info("Foundational services installed successfully")
				keycloakInstalled = true
			}
		}
	} else {
		slog.Info("Would install Argo CD and foundational services (dry-run mode)")
	}

	// Flush all pending status messages before printing instructions
	// This prevents log messages from appearing in the middle of the instructions
	cleanupStatus()

	// Print instructions after status handler is cleaned up
	if argoCDInstalled {
		printArgoCDInstructions(cfg)
	}
	if keycloakInstalled {
		printKeycloakInstructions(cfg)
	}

	// Print DNS guidance if no DNS provider is configured
	if cfg.DNSProvider == "" && cfg.Domain != "" && !deployDryRun {
		var lbEndpoint *endpoint.LoadBalancerEndpoint

		kubeconfigBytes, err := provider.GetKubeconfig(ctx, cfg)
		if err != nil {
			slog.Warn("Could not get kubeconfig for endpoint lookup", "error", err)
		} else {
			restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
			if err != nil {
				slog.Warn("Could not parse kubeconfig for endpoint lookup", "error", err)
			} else {
				k8sClient, err := kubernetes.NewForConfig(restConfig)
				if err != nil {
					slog.Warn("Could not create k8s client for endpoint lookup", "error", err)
				} else {
					slog.Info("Waiting for load balancer endpoint...")
					lbEndpoint, err = endpoint.GetLoadBalancerEndpoint(ctx, k8sClient)
					if err != nil {
						slog.Warn("Could not retrieve load balancer endpoint", "error", err)
					}
				}
			}
		}

		printDNSGuidance(cfg, lbEndpoint)
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

	// Write all ArgoCD application manifests and raw K8s manifests to git
	slog.Info("Writing ArgoCD application manifests to git repository")
	if err := argocd.WriteAllToGit(ctx, gitClient, cfg); err != nil {
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
func printDNSGuidance(cfg *config.NebariConfig, lb *endpoint.LoadBalancerEndpoint) {
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

	if lb != nil {
		var recordType, value string
		if lb.Hostname != "" {
			recordType = "CNAME"
			value = lb.Hostname
		} else {
			recordType = "A"
			value = lb.IP
		}

		fmt.Println("  Required DNS Records:")
		fmt.Println("  ┌─────────────────────────────────────────────────────────────────────────┐")
		fmt.Printf("  │ Type  : %-65s │\n", recordType)
		fmt.Printf("  │ Name  : %-65s │\n", cfg.Domain)
		fmt.Printf("  │ Value : %-65s │\n", value)
		fmt.Println("  ├─────────────────────────────────────────────────────────────────────────┤")
		fmt.Printf("  │ Type  : %-65s │\n", recordType)
		fmt.Printf("  │ Name  : %-65s │\n", "*."+cfg.Domain)
		fmt.Printf("  │ Value : %-65s │\n", value)
		fmt.Println("  └─────────────────────────────────────────────────────────────────────────┘")
	} else {
		fmt.Println("  The load balancer endpoint is not yet available.")
		fmt.Println("  Run the following command to check when it's ready:")
		fmt.Println()
		fmt.Println("    nic status -f <config-file>")
		fmt.Println()
		fmt.Println("  Once the endpoint is available, create A (for IP) or CNAME (for hostname)")
		fmt.Printf("  records pointing %s and *.%s to the endpoint.\n", cfg.Domain, cfg.Domain)
	}

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

// printArgoCDInstructions prints instructions for accessing Argo CD
func printArgoCDInstructions(cfg *config.NebariConfig) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("  ARGO CD INSTALLED")
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("  Argo CD has been successfully installed on your cluster.")
	fmt.Println()
	fmt.Println("  To access Argo CD:")
	fmt.Println()
	if cfg.Domain != "" {
		fmt.Printf("    UI: https://argocd.%s (after DNS configuration)\n", cfg.Domain)
		fmt.Println()
		fmt.Println("  Or use port-forwarding:")
		fmt.Println()
	}
	fmt.Println("    kubectl port-forward svc/argocd-server -n argocd 8080:443")
	fmt.Println("    Then visit: https://localhost:8080")
	fmt.Println()
	fmt.Println("  Get the admin password:")
	fmt.Println()
	fmt.Println("    kubectl -n argocd get secret argocd-initial-admin-secret \\")
	fmt.Println("      -o jsonpath=\"{.data.password}\" | base64 -d")
	fmt.Println()
	fmt.Println("  Login credentials:")
	fmt.Println("    Username: admin")
	fmt.Println("    Password: <from command above>")
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
}

// printKeycloakInstructions prints instructions for accessing Keycloak
func printKeycloakInstructions(cfg *config.NebariConfig) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("  KEYCLOAK INSTALLED")
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("  Keycloak has been configured for installation via Argo CD.")
	fmt.Println("  It may take several minutes for Keycloak to be fully deployed and ready.")
	fmt.Println()
	fmt.Println("  Check deployment status:")
	fmt.Println("    kubectl get pods -n keycloak")
	fmt.Println()
	fmt.Println("  To access Keycloak after deployment:")
	fmt.Println()
	if cfg.Domain != "" {
		fmt.Printf("    UI: https://keycloak.%s (after DNS configuration)\n", cfg.Domain)
		fmt.Println()
		fmt.Println("  Or use port-forwarding:")
		fmt.Println()
	}
	fmt.Println("    kubectl port-forward svc/keycloak -n keycloak 8080:80")
	fmt.Println("    Then visit: http://localhost:8080")
	fmt.Println()
	fmt.Println("  Get the admin password:")
	fmt.Println()
	fmt.Println("    kubectl -n keycloak get secret keycloak-admin-credentials \\")
	fmt.Println("      -o jsonpath=\"{.data.admin-password}\" | base64 -d")
	fmt.Println()
	fmt.Println("  Login credentials:")
	fmt.Println("    Username: admin")
	fmt.Println("    Password: <from command above>")
	fmt.Println()
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
}

// generateSecurePassword generates a cryptographically secure random password.
// It accepts an io.Reader to allow for deterministic testing with known bytes.
func generateSecurePassword(r io.Reader) string {
	// Generate 32 bytes of random data
	b := make([]byte, 32)
	if _, err := r.Read(b); err != nil {
		// Fallback to timestamp-based generation (not ideal but better than nothing)
		return fmt.Sprintf("nebari-%d", time.Now().UnixNano())
	}
	// Encode to base64 and take first 43 characters (removes padding)
	return base64.URLEncoding.EncodeToString(b)[:43]
}
