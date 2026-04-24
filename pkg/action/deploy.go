package action

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// Deploy creates or updates Nebari infrastructure and installs foundational
// software on top.
type Deploy struct {
	// DryRun previews changes without applying them.
	DryRun bool

	// Timeout overrides the provider's default deploy timeout. Zero means the
	// provider chooses.
	Timeout time.Duration

	// RegenApps forces regeneration of ArgoCD application manifests even when
	// the GitOps repository is already bootstrapped.
	RegenApps bool
}

// DeployResult contains useful information from the deploy process that can be used
// by callers after Run completes.
type DeployResult struct {
	// ArgoCDInstalled is true when the Argo CD Helm install completed. False
	// when skipped (dry-run) or when installation failed.
	ArgoCDInstalled bool

	// KeycloakInstalled is true when foundational services (including
	// Keycloak) were installed successfully via Argo CD.
	KeycloakInstalled bool

	// LBEndpoint is the load balancer address for the deployed cluster, if
	// lookup succeeded. Nil when no domain is configured, during dry-run, or
	// when the endpoint was not ready in time.
	LBEndpoint *endpoint.LoadBalancerEndpoint
}

// Run executes the deploy flow by provisioning the cluster described in the config, installing
// the foundational software on top, and then optionally provisioning DNS records when a domain
// and DNS provider are configured. Callers must construct the config or parse it from a file
func (d *Deploy) Run(ctx context.Context, cfg *config.NebariConfig) (*DeployResult, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "action.Deploy")
	defer span.End()

	span.SetAttributes(attribute.Bool("dry_run", d.DryRun))
	if d.Timeout > 0 {
		span.SetAttributes(attribute.String("timeout", d.Timeout.String()))
	}

	if d.DryRun {
		slog.Info("Starting deployment (dry-run)")
	} else {
		slog.Info("Starting deployment")
	}

	reg, err := defaultRegistry(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("build default registry: %w", err)
	}

	if err := cfg.Validate(validateOptions(ctx, reg)); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	slog.Info("Configuration validated",
		"provider", cfg.Cluster.ProviderName(),
		"project_name", cfg.ProjectName,
	)

	ctx, cleanup := status.StartHandler(ctx, defaultStatusHandler())
	defer cleanup()

	clusterProvider, err := reg.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get cluster provider: %w", err)
	}

	slog.Info("Provider selected", "provider", clusterProvider.Name())

	if err := clusterProvider.Deploy(ctx, cfg.ProjectName, cfg.Cluster, provider.DeployOptions{
		DryRun:  d.DryRun,
		Timeout: d.Timeout,
	}); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("provider deploy: %w", err)
	}

	slog.Info("Infrastructure deployment completed", "provider", clusterProvider.Name())

	infraSettings := clusterProvider.InfraSettings(cfg.Cluster)

	if cfg.GitRepository != nil && !d.DryRun {
		if err := bootstrapGitOps(ctx, cfg, d.RegenApps, infraSettings); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("gitops bootstrap: %w", err)
		}
	}

	result := &DeployResult{}

	if d.DryRun {
		slog.Info("Would install Argo CD and foundational services (dry-run mode)")
		return result, nil
	}

	slog.Info("Installing Argo CD on cluster")
	if err := argocd.Install(ctx, cfg, clusterProvider); err != nil {
		slog.Warn("Failed to install Argo CD", "error", err)
		slog.Warn("You can install Argo CD manually with: helm install argocd argo/argo-cd --namespace argocd --create-namespace")
	} else {
		slog.Info("Argo CD installed successfully")
		result.ArgoCDInstalled = true

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
			},
			LandingPage: argocd.LandingPageConfig{
				RedisPassword: generateSecurePassword(rand.Reader),
			},
			MetalLB: argocd.MetalLBConfig{
				Enabled:     infraSettings.NeedsMetalLB,
				AddressPool: infraSettings.MetalLBAddressPool,
			},
		}

		if err := argocd.InstallFoundationalServices(ctx, cfg, clusterProvider, foundationalCfg); err != nil {
			slog.Warn("Failed to install foundational services", "error", err)
			slog.Warn("You can install foundational services manually with: kubectl apply -f pkg/foundational/")
		} else {
			slog.Info("Foundational services installed successfully")
			result.KeycloakInstalled = true
		}
	}

	if cfg.Domain != "" {
		result.LBEndpoint = lookupEndpointAndProvisionDNS(ctx, cfg, clusterProvider, reg)
	}

	slog.Info("Deployment completed successfully", "provider", clusterProvider.Name())
	return result, nil
}

// lookupEndpointAndProvisionDNS gets the load balancer endpoint from the
// cluster and provisions DNS records if a DNS provider is configured. Returns
// the LB endpoint for use in manual DNS guidance (may be nil if lookup failed).
func lookupEndpointAndProvisionDNS(ctx context.Context, cfg *config.NebariConfig, prov provider.Provider, reg *registry.Registry) *endpoint.LoadBalancerEndpoint {
	kubeconfigBytes, err := prov.GetKubeconfig(ctx, cfg.ProjectName, cfg.Cluster)
	if err != nil {
		slog.Warn("Could not get kubeconfig for endpoint lookup", "error", err)
		return nil
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		slog.Warn("Could not parse kubeconfig for endpoint lookup", "error", err)
		return nil
	}

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		slog.Warn("Could not create k8s client for endpoint lookup", "error", err)
		return nil
	}

	slog.Info("Waiting for load balancer endpoint...")
	lbEndpoint, err := endpoint.GetLoadBalancerEndpoint(ctx, k8sClient)
	if err != nil {
		slog.Warn("Could not retrieve load balancer endpoint", "error", err)
		return nil
	}

	if cfg.DNS == nil {
		return lbEndpoint
	}

	if lbEndpoint == nil {
		slog.Warn("Skipping DNS provisioning: load balancer endpoint not available")
		return nil
	}

	lbEndpointStr := lbEndpoint.Hostname
	if lbEndpointStr == "" {
		lbEndpointStr = lbEndpoint.IP
	}
	if lbEndpointStr == "" {
		slog.Warn("Load balancer endpoint has no hostname or IP, skipping DNS provisioning")
		return lbEndpoint
	}

	dnsProvider, err := reg.DNSProviders.Get(ctx, cfg.DNS.ProviderName())
	if err != nil {
		slog.Warn("DNS provider not found, skipping DNS provisioning", "provider", cfg.DNS.ProviderName(), "error", err)
		return lbEndpoint
	}

	slog.Info("Provisioning DNS records", "provider", cfg.DNS.ProviderName(), "domain", cfg.Domain)
	if err := dnsProvider.ProvisionRecords(ctx, cfg.Domain, cfg.DNS.ProviderConfig(), lbEndpointStr); err != nil {
		slog.Warn("Failed to provision DNS records", "error", err)
		slog.Warn("You can configure DNS manually - see instructions below")
	} else {
		slog.Info("DNS records provisioned successfully", "domain", cfg.Domain)
	}

	return lbEndpoint
}

// bootstrapGitOps initializes the GitOps repository with ArgoCD application
// manifests. Handles all git I/O.
func bootstrapGitOps(ctx context.Context, cfg *config.NebariConfig, regenApps bool, settings provider.InfraSettings) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "action.bootstrapGitOps")
	defer span.End()

	span.SetAttributes(
		attribute.String("git.url", cfg.GitRepository.URL),
		attribute.Bool("regen_apps", regenApps),
	)

	slog.Info("Initializing GitOps repository", "url", cfg.GitRepository.URL)

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

	if err := gitClient.Init(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to initialize git repository: %w", err)
	}

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

	slog.Info("Writing ArgoCD application manifests to git repository")
	if err := argocd.WriteAllToGit(ctx, gitClient, cfg, settings); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write application manifests: %w", err)
	}

	if err := gitClient.WriteBootstrapMarker(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write bootstrap marker: %w", err)
	}

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

// generateSecurePassword generates a cryptographically secure random password.
// It accepts an io.Reader to allow for deterministic testing with known bytes.
func generateSecurePassword(r io.Reader) string {
	b := make([]byte, 32)
	if _, err := r.Read(b); err != nil {
		return fmt.Sprintf("nebari-%d", time.Now().UnixNano())
	}
	return base64.URLEncoding.EncodeToString(b)[:43]
}

// validateOptions builds config.ValidateOptions from a registry. Shared by
// actions that need to validate config against the registered providers.
func validateOptions(ctx context.Context, reg *registry.Registry) config.ValidateOptions {
	return config.ValidateOptions{
		ClusterProviders: reg.ClusterProviders.List(ctx),
		DNSProviders:     reg.DNSProviders.List(ctx),
	}
}
