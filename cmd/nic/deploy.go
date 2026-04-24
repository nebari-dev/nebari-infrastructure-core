package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/action"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
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
	deployCmd.Flags().StringVarP(&deployConfigFile, "file", "f", "", "Path to nebari-config.yaml file (auto-discovered if omitted)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "Show what would be deployed without making changes")
	deployCmd.Flags().StringVar(&deployTimeout, "timeout", "", "Override default timeout (e.g., '45m', '1h')")
	deployCmd.Flags().BoolVar(&deployRegenApps, "regen-apps", false, "Regenerate ArgoCD application manifests even if already bootstrapped")
}

func runDeploy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	deployConfigFile, err := resolveConfigFile(deployConfigFile)
	if err != nil {
		return err
	}

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", deployConfigFile),
		attribute.Bool("dry_run", deployDryRun),
	)

	var timeout time.Duration
	if deployTimeout != "" {
		timeout, err = time.ParseDuration(deployTimeout)
		if err != nil {
			span.RecordError(err)
			slog.Error("Invalid timeout duration", "error", err, "timeout", deployTimeout)
			return fmt.Errorf("invalid timeout duration %q: %w", deployTimeout, err)
		}
		span.SetAttributes(attribute.String("timeout", deployTimeout))
	}

	cfg, err := config.ParseConfig(ctx, deployConfigFile)
	if err != nil {
		span.RecordError(err)
		slog.Error("Failed to parse configuration", "error", err, "file", deployConfigFile)
		return err
	}

	deploy := action.Deploy{
		DryRun:    deployDryRun,
		Timeout:   timeout,
		RegenApps: deployRegenApps,
	}

	result, err := deploy.Run(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if result.ArgoCDInstalled {
		printArgoCDInstructions(cfg)
	}
	if result.KeycloakInstalled {
		printKeycloakInstructions(cfg)
	}
	if cfg.DNS == nil && cfg.Domain != "" && !deployDryRun {
		printDNSGuidance(cfg, result.LBEndpoint)
	}

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
	fmt.Println("  To automate DNS management, add a dns block to your configuration:")
	fmt.Println()
	fmt.Println("    dns:")
	fmt.Println("      cloudflare:")
	fmt.Println("        zone_name: example.com")
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
