package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/cmd/nic/renderer"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	providerPkg "github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
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
	deployCmd.Flags().StringVarP(&deployConfigFile, "file", "f", "", "Path to nebari-config.yaml file (auto-discovered if omitted)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "Show what would be deployed without making changes")
	deployCmd.Flags().StringVar(&deployTimeout, "timeout", "", "Override default timeout (e.g., '45m', '1h')")
	deployCmd.Flags().BoolVar(&deployRegenApps, "regen-apps", false, "Regenerate ArgoCD application manifests even if already bootstrapped")
}

func runDeploy(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	r := renderer.FromContext(ctx)

	resolved, err := resolveConfigFile(deployConfigFile)
	if err != nil {
		return err
	}
	deployConfigFile = resolved

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", deployConfigFile),
		attribute.Bool("dry_run", deployDryRun),
	)

	// Setup status handler routed through the renderer
	ctx, cleanupStatusFn := status.StartHandler(ctx, statusRendererHandler(r))
	var statusCleanedUp bool
	cleanupStatus := func() {
		if !statusCleanedUp {
			statusCleanedUp = true
			cleanupStatusFn()
		}
	}
	defer cleanupStatus()

	defer func() {
		if ctx.Err() == context.Canceled {
			r.Warn("Deployment interrupted by user")
		}
	}()

	// --- Configuration phase ---
	start := time.Now()
	cfg, err := config.ParseConfig(ctx, deployConfigFile)
	if err != nil {
		span.RecordError(err)
		r.Error(err, "Check your config file syntax")
		return err
	}
	if err := cfg.Validate(getValidNames(ctx, reg)); err != nil {
		span.RecordError(err)
		r.Error(err, "Run 'nic validate -f "+deployConfigFile+"' for details")
		return err
	}
	r.EndStep(renderer.StepOK, time.Since(start), "Configuration validated")

	mode := ""
	if deployDryRun {
		mode = " (dry-run)"
	}
	r.Info(fmt.Sprintf("Deploying %s (%s)%s", cfg.ProjectName, cfg.Cluster.ProviderName(), mode))

	// Parse custom timeout
	var timeout time.Duration
	if deployTimeout != "" {
		timeout, err = time.ParseDuration(deployTimeout)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("invalid timeout duration %q: %w", deployTimeout, err)
		}
		span.SetAttributes(attribute.String("timeout", deployTimeout))
	}

	// --- Infrastructure phase ---
	r.StartPhase("Infrastructure")
	infraStart := time.Now()

	provider, err := reg.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		r.EndPhase(renderer.PhaseFailed, time.Since(infraStart))
		return err
	}

	if err := provider.Deploy(ctx, cfg.ProjectName, cfg.Cluster, providerPkg.DeployOptions{DryRun: deployDryRun, Timeout: timeout}); err != nil {
		span.RecordError(err)
		r.Error(err, "")
		r.EndPhase(renderer.PhaseFailed, time.Since(infraStart))
		return err
	}
	r.EndStep(renderer.StepOK, time.Since(infraStart), "Cluster created")
	r.EndPhase(renderer.PhaseOK, time.Since(infraStart))

	infraSettings := provider.InfraSettings(cfg.Cluster)

	// --- GitOps phase ---
	if cfg.GitRepository != nil && !deployDryRun {
		r.StartPhase("GitOps")
		gitStart := time.Now()
		if err := bootstrapGitOps(ctx, cfg, deployRegenApps, infraSettings); err != nil {
			span.RecordError(err)
			r.Error(err, "")
			r.EndPhase(renderer.PhaseFailed, time.Since(gitStart))
			return err
		}
		r.EndStep(renderer.StepOK, time.Since(gitStart), "Repository bootstrapped")
		r.EndPhase(renderer.PhaseOK, time.Since(gitStart))
	}

	// Track what was installed for final summary
	var argoCDInstalled, keycloakInstalled bool
	var summaryItems []renderer.SummaryItem

	// --- ArgoCD phase ---
	if !deployDryRun {
		r.StartPhase("ArgoCD")
		argoStart := time.Now()
		if err := argocd.Install(ctx, cfg, provider); err != nil {
			r.Warn(fmt.Sprintf("Failed to install Argo CD: %v", err))
			r.Warn("You can install Argo CD manually with: helm install argocd argo/argo-cd --namespace argocd --create-namespace")
			r.EndPhase(renderer.PhaseFailed, time.Since(argoStart))
		} else {
			r.EndStep(renderer.StepOK, time.Since(argoStart), "Helm chart installed")
			argoCDInstalled = true
			if cfg.Domain != "" {
				summaryItems = append(summaryItems, renderer.SummaryItem{Label: "ArgoCD", Value: fmt.Sprintf("https://argocd.%s", cfg.Domain)})
			}

			// --- Foundational Services phase ---
			r.StartPhase("Foundational Services")
			foundStart := time.Now()
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
					Hostname:              "",
				},
				LandingPage: argocd.LandingPageConfig{
					RedisPassword: generateSecurePassword(rand.Reader),
				},
				MetalLB: argocd.MetalLBConfig{
					Enabled:     infraSettings.NeedsMetalLB,
					AddressPool: infraSettings.MetalLBAddressPool,
				},
			}

			if err := argocd.InstallFoundationalServices(ctx, cfg, provider, foundationalCfg); err != nil {
				r.Warn(fmt.Sprintf("Failed to install foundational services: %v", err))
				r.EndPhase(renderer.PhaseFailed, time.Since(foundStart))
			} else {
				r.EndStep(renderer.StepOK, time.Since(foundStart), "Foundational services installed")
				r.EndPhase(renderer.PhaseOK, time.Since(foundStart))
				keycloakInstalled = true
				if cfg.Domain != "" {
					summaryItems = append(summaryItems, renderer.SummaryItem{Label: "Keycloak", Value: fmt.Sprintf("https://keycloak.%s", cfg.Domain)})
				}
			}

			if !argoCDInstalled {
				r.EndPhase(renderer.PhaseFailed, time.Since(argoStart))
			} else {
				r.EndPhase(renderer.PhaseOK, time.Since(argoStart))
			}
		}
	} else {
		r.Info("Would install Argo CD and foundational services (dry-run mode)")
	}

	// --- DNS phase ---
	var lbEndpoint *endpoint.LoadBalancerEndpoint
	if cfg.Domain != "" && !deployDryRun {
		r.StartPhase("DNS")
		dnsStart := time.Now()
		lbEndpoint = lookupEndpointAndProvisionDNS(ctx, cfg, provider, r)
		r.EndPhase(renderer.PhaseOK, time.Since(dnsStart))
	}

	// Flush pending status messages before final summary
	cleanupStatus()

	// --- Final summary ---
	_ = lbEndpoint
	_ = keycloakInstalled

	r.EndStep(renderer.StepOK, time.Since(start), "Deployment complete")

	if len(summaryItems) > 0 {
		r.Summary(summaryItems)
	}

	return nil
}

// lookupEndpointAndProvisionDNS gets the load balancer endpoint from the cluster
// and provisions DNS records if a DNS provider is configured. Returns the LB
// endpoint for use in manual DNS guidance (may be nil if lookup failed).
func lookupEndpointAndProvisionDNS(ctx context.Context, cfg *config.NebariConfig, prov providerPkg.Provider, r renderer.Renderer) *endpoint.LoadBalancerEndpoint {
	kubeconfigBytes, err := prov.GetKubeconfig(ctx, cfg.ProjectName, cfg.Cluster)
	if err != nil {
		r.Warn(fmt.Sprintf("Could not get kubeconfig for endpoint lookup: %v", err))
		return nil
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		r.Warn(fmt.Sprintf("Could not parse kubeconfig for endpoint lookup: %v", err))
		return nil
	}

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		r.Warn(fmt.Sprintf("Could not create k8s client for endpoint lookup: %v", err))
		return nil
	}

	r.StartStep("Waiting for load balancer endpoint")
	lbEndpoint, err := endpoint.GetLoadBalancerEndpoint(ctx, k8sClient)
	if err != nil {
		r.Warn(fmt.Sprintf("Could not retrieve load balancer endpoint: %v", err))
		return nil
	}

	if cfg.DNS == nil {
		return lbEndpoint
	}

	if lbEndpoint == nil {
		r.Warn("Skipping DNS provisioning: load balancer endpoint not available")
		return nil
	}

	lbEndpointStr := lbEndpoint.Hostname
	if lbEndpointStr == "" {
		lbEndpointStr = lbEndpoint.IP
	}
	if lbEndpointStr == "" {
		r.Warn("Load balancer endpoint has no hostname or IP, skipping DNS provisioning")
		return lbEndpoint
	}

	dnsProvider, err := reg.DNSProviders.Get(ctx, cfg.DNS.ProviderName())
	if err != nil {
		r.Warn(fmt.Sprintf("DNS provider not found: %v", err))
		return lbEndpoint
	}

	dnsStart := time.Now()
	if err := dnsProvider.ProvisionRecords(ctx, cfg.Domain, cfg.DNS.ProviderConfig(), lbEndpointStr); err != nil {
		r.Warn(fmt.Sprintf("Failed to provision DNS records: %v", err))
	} else {
		r.EndStep(renderer.StepOK, time.Since(dnsStart), fmt.Sprintf("DNS records provisioned (%s)", cfg.Domain))
	}

	return lbEndpoint
}

// bootstrapGitOps initializes the GitOps repository with ArgoCD application manifests.
// This is the orchestrator function that handles all I/O operations.
func bootstrapGitOps(ctx context.Context, cfg *config.NebariConfig, regenApps bool, settings providerPkg.InfraSettings) error {
	r := renderer.FromContext(ctx)

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.bootstrapGitOps")
	defer span.End()

	span.SetAttributes(
		attribute.String("git.url", cfg.GitRepository.URL),
		attribute.Bool("regen_apps", regenApps),
	)

	gitClient, err := git.NewClient(cfg.GitRepository)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create git client: %w", err)
	}
	defer func() { _ = gitClient.Cleanup() }()

	if err := gitClient.ValidateAuth(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("git authentication failed: %w", err)
	}

	start := time.Now()
	if err := gitClient.Init(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to initialize git repository: %w", err)
	}
	r.EndStep(renderer.StepOK, time.Since(start), "Repository cloned")

	bootstrapped, err := gitClient.IsBootstrapped(ctx)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to check bootstrap status: %w", err)
	}

	if bootstrapped && !regenApps {
		r.EndStep(renderer.StepSkipped, 0, "Already bootstrapped, skipping")
		span.SetAttributes(attribute.Bool("skipped", true))
		return nil
	}

	writeStart := time.Now()
	if err := argocd.WriteAllToGit(ctx, gitClient, cfg, settings); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write application manifests: %w", err)
	}

	if err := gitClient.WriteBootstrapMarker(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write bootstrap marker: %w", err)
	}
	r.EndStep(renderer.StepOK, time.Since(writeStart), "Manifests generated")

	pushStart := time.Now()
	commitMsg := "Bootstrap foundational ArgoCD applications"
	if regenApps {
		commitMsg = "Regenerate foundational ArgoCD applications"
	}
	if err := gitClient.CommitAndPush(ctx, commitMsg); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to commit and push: %w", err)
	}
	r.EndStep(renderer.StepOK, time.Since(pushStart), "Changes pushed")

	return nil
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
