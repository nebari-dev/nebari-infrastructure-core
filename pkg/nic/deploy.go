package nic

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gopkg.in/yaml.v3"
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

// DeployOptions configures a Deploy call.
type DeployOptions struct {
	// DryRun previews changes without applying them.
	DryRun bool

	// Timeout overrides the provider's default deploy timeout. Zero means
	// the provider chooses.
	Timeout time.Duration

	// RegenApps forces regeneration of ArgoCD application manifests even
	// when the GitOps repository is already bootstrapped.
	RegenApps bool
}

// DeployResult contains useful information from the deploy process that
// can be used by callers after Deploy completes.
type DeployResult struct {
	// ArgoCDInstalled is true when the Argo CD Helm install completed.
	// False when skipped (dry-run) or when installation failed.
	ArgoCDInstalled bool

	// KeycloakInstalled is true when foundational services (including
	// Keycloak) were installed successfully via Argo CD.
	KeycloakInstalled bool

	// LBEndpoint is the load balancer address for the deployed cluster, if
	// lookup succeeded. Nil when no domain is configured, during dry-run,
	// or when the endpoint was not ready in time.
	LBEndpoint *endpoint.LoadBalancerEndpoint
}

// Deploy creates or updates Nebari infrastructure and installs foundational
// software on top. It provisions the cluster described in cfg, installs the
// foundational software, and (optionally) provisions DNS records when a
// domain and DNS provider are configured.
func (c *Client) Deploy(ctx context.Context, cfg *config.NebariConfig, opts DeployOptions) (*DeployResult, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.Deploy")
	defer span.End()

	span.SetAttributes(attribute.Bool("dry_run", opts.DryRun))

	if opts.DryRun {
		c.logger.Info("Starting deployment (dry-run)")
	} else {
		c.logger.Info("Starting deployment")
	}

	reg, err := defaultRegistry(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("build default registry: %w", err)
	}

	// Setup status handler for progress updates
	ctx, cleanup := status.StartHandler(ctx, c.statusLogHandler())
	defer cleanup()

	// Handle context cancellation (from signal interrupt)
	defer func() {
		if ctx.Err() == context.Canceled {
			c.logger.Warn("Deployment interrupted by user")
		}
	}()

	// Validate configuration with registered providers
	if err := cfg.Validate(validateOptions(ctx, reg)); err != nil {
		span.RecordError(err)
		c.logger.Error("Configuration validation failed", "error", err)
		return nil, err
	}

	c.logger.Info("Configuration parsed successfully",
		"provider", cfg.Cluster.ProviderName(),
		"project_name", cfg.ProjectName,
	)

	if opts.Timeout > 0 {
		span.SetAttributes(attribute.String("timeout", opts.Timeout.String()))
		c.logger.Info("Using custom timeout", "timeout", opts.Timeout)
	}

	// Get the appropriate provider
	clusterProvider, err := reg.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		c.logger.Error("Failed to get provider", "error", err, "provider", cfg.Cluster.ProviderName())
		return nil, err
	}

	c.logger.Info("Provider selected", "provider", clusterProvider.Name())

	// Deploy infrastructure
	if err := clusterProvider.Deploy(ctx, cfg.ProjectName, cfg.Cluster, provider.DeployOptions{DryRun: opts.DryRun, Timeout: opts.Timeout}); err != nil {
		span.RecordError(err)
		c.logger.Error("Deployment failed", "error", err, "provider", clusterProvider.Name())
		return nil, err
	}

	c.logger.Info("Infrastructure deployment completed", "provider", clusterProvider.Name())

	// Get provider infrastructure settings for GitOps and foundational services
	infraSettings := clusterProvider.InfraSettings(cfg.Cluster)

	// Bootstrap GitOps (auto-create local directory for providers that support it)
	gitConfig, err := c.getOrCreateGitConfig(cfg, infraSettings.SupportsLocalGitOps)
	if err != nil {
		span.RecordError(err)
		c.logger.Error("GitOps configuration failed", "error", err)
		return nil, err
	}
	if gitConfig != nil {
		// Set on cfg so downstream code (Install, InstallFoundationalServices) can use cfg.GitRepository
		cfg.GitRepository = gitConfig
	}
	if cfg.GitRepository != nil && !opts.DryRun {
		if err := c.bootstrapGitOps(ctx, cfg, opts.RegenApps, infraSettings); err != nil {
			span.RecordError(err)
			c.logger.Error("GitOps bootstrap failed", "error", err)
			return nil, err
		}
	}

	c.logger.Info("Deployment completed successfully", "provider", clusterProvider.Name())

	result := &DeployResult{}

	// Install Argo CD (skip in dry-run mode)
	if !opts.DryRun {
		c.logger.Info("Installing Argo CD on cluster")

		// Generate OIDC client secret upfront - needed by both ArgoCD Helm values
		// and the Keycloak realm-setup job
		argoCDClientSecret := generateSecurePassword(rand.Reader)

		// Build ArgoCD config with Keycloak OIDC SSO
		argoCDConfig := argocd.ConfigWithOIDC(cfg.Domain, infraSettings.KeycloakBasePath, argoCDClientSecret)

		if err := argocd.Install(ctx, cfg, clusterProvider, argoCDConfig); err != nil {
			// Log error but don't fail deployment
			c.logger.Warn("Failed to install Argo CD", "error", err)
			c.logger.Warn("You can install Argo CD manually with: helm install argocd argo/argo-cd --namespace argocd --create-namespace")
		} else {
			c.logger.Info("Argo CD installed successfully")
			result.ArgoCDInstalled = true

			// Install foundational services via Argo CD
			c.logger.Info("Installing foundational services")
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
				ArgoCD: argocd.ArgoCDSSOConfig{
					ClientSecret: argoCDClientSecret,
				},
				LandingPage: argocd.LandingPageConfig{
					RedisPassword: generateSecurePassword(rand.Reader),
				},
				// Enable MetalLB only for providers that need it
				MetalLB: argocd.MetalLBConfig{
					Enabled:     infraSettings.NeedsMetalLB,
					AddressPool: infraSettings.MetalLBAddressPool,
				},
			}

			if err := argocd.InstallFoundationalServices(ctx, cfg, clusterProvider, foundationalCfg); err != nil {
				// Log warning but don't fail deployment
				c.logger.Warn("Failed to install foundational services", "error", err)
				c.logger.Warn("You can install foundational services manually with: kubectl apply -f pkg/foundational/")
			} else {
				c.logger.Info("Foundational services installed successfully")
				result.KeycloakInstalled = true
			}
		}
	} else {
		c.logger.Info("Would install Argo CD and foundational services (dry-run mode)")
	}

	// Look up LB endpoint and provision DNS records if configured
	if cfg.Domain != "" && !opts.DryRun {
		result.LBEndpoint = c.lookupEndpointAndProvisionDNS(ctx, cfg, clusterProvider, reg)
	}

	return result, nil
}

// defaultGitConfig returns a default local git configuration for development workflows.
// This is a pure function with no side effects — directory creation happens separately.
func defaultGitConfig(projectName string) *git.Config {
	localPath := filepath.Join(os.TempDir(), fmt.Sprintf("nebari-gitops-%s", projectName))
	return &git.Config{
		URL:    fmt.Sprintf("file://%s", localPath),
		Branch: git.DefaultBranch,
		Path:   "",
		Auth:   git.AuthConfig{},
	}
}

// getOrCreateGitConfig returns the git configuration, creating a default local one if none is configured.
// For providers that support local gitops without explicit git_repository config, this auto-creates
// /tmp/nebari-gitops-{project_name}. For other providers, explicit git_repository config is required.
// The supportsLocalGitOps parameter comes from provider.InfraSettings().SupportsLocalGitOps.
func (c *Client) getOrCreateGitConfig(cfg *config.NebariConfig, supportsLocalGitOps bool) (*git.Config, error) {
	if cfg.GitRepository != nil {
		return cfg.GitRepository, nil
	}

	// Only auto-create local gitops for providers that support it (e.g., local, kind, k3s)
	// Cloud providers without explicit git_repository config skip GitOps bootstrapping
	if !supportsLocalGitOps {
		c.logger.Info("No git_repository configured and provider does not support local gitops, skipping GitOps bootstrap",
			"provider", cfg.Cluster.ProviderName())
		return nil, nil
	}

	gitCfg := defaultGitConfig(cfg.ProjectName)
	localPath, err := gitCfg.GetLocalPath()
	if err != nil {
		return nil, fmt.Errorf("invalid local path in auto-generated git config: %w", err)
	}

	c.logger.Info("No git_repository configured, using auto-generated local directory",
		"path", localPath)

	if err := os.MkdirAll(localPath, 0750); err != nil {
		return nil, fmt.Errorf("failed to create auto-generated directory %s: %w", localPath, err)
	}

	return gitCfg, nil
}

// lookupEndpointAndProvisionDNS gets the load balancer endpoint from the cluster
// and provisions DNS records if a DNS provider is configured. Returns the LB
// endpoint for use in manual DNS guidance (may be nil if lookup failed).
func (c *Client) lookupEndpointAndProvisionDNS(ctx context.Context, cfg *config.NebariConfig, prov provider.Provider, reg *registry.Registry) *endpoint.LoadBalancerEndpoint {
	kubeconfigBytes, err := prov.GetKubeconfig(ctx, cfg.ProjectName, cfg.Cluster)
	if err != nil {
		c.logger.Warn("Could not get kubeconfig for endpoint lookup", "error", err)
		return nil
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		c.logger.Warn("Could not parse kubeconfig for endpoint lookup", "error", err)
		return nil
	}

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		c.logger.Warn("Could not create k8s client for endpoint lookup", "error", err)
		return nil
	}

	c.logger.Info("Waiting for load balancer endpoint...")
	lbEndpoint, err := endpoint.GetLoadBalancerEndpoint(ctx, k8sClient)
	if err != nil {
		c.logger.Warn("Could not retrieve load balancer endpoint", "error", err)
		return nil
	}

	// Provision DNS records if a provider is configured
	if cfg.DNS == nil {
		return lbEndpoint
	}

	if lbEndpoint == nil {
		c.logger.Warn("Skipping DNS provisioning: load balancer endpoint not available")
		return nil
	}

	lbEndpointStr := lbEndpoint.Hostname
	if lbEndpointStr == "" {
		lbEndpointStr = lbEndpoint.IP
	}
	if lbEndpointStr == "" {
		c.logger.Warn("Load balancer endpoint has no hostname or IP, skipping DNS provisioning")
		return lbEndpoint
	}

	dnsProvider, err := reg.DNSProviders.Get(ctx, cfg.DNS.ProviderName())
	if err != nil {
		c.logger.Warn("DNS provider not found, skipping DNS provisioning", "provider", cfg.DNS.ProviderName(), "error", err)
		return lbEndpoint
	}

	c.logger.Info("Provisioning DNS records", "provider", cfg.DNS.ProviderName(), "domain", cfg.Domain)
	if err := dnsProvider.ProvisionRecords(ctx, cfg.Domain, cfg.DNS.ProviderConfig(), lbEndpointStr); err != nil {
		c.logger.Warn("Failed to provision DNS records", "error", err)
		c.logger.Warn("You can configure DNS manually - see instructions below")
	} else {
		c.logger.Info("DNS records provisioned successfully", "domain", cfg.Domain)
	}

	return lbEndpoint
}

// bootstrapGitOps initializes the GitOps repository with ArgoCD application manifests.
// This is the orchestrator function that handles all I/O operations.
// cfg.GitRepository must be set before calling this function.
func (c *Client) bootstrapGitOps(ctx context.Context, cfg *config.NebariConfig, regenApps bool, settings provider.InfraSettings) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.bootstrapGitOps")
	defer span.End()

	gitConfig := cfg.GitRepository
	span.SetAttributes(
		attribute.String("git.url", gitConfig.URL),
		attribute.Bool("regen_apps", regenApps),
	)

	isLocal := gitConfig.IsLocalPath()
	var localPath string
	if isLocal {
		var err error
		localPath, err = gitConfig.GetLocalPath()
		if err != nil {
			return fmt.Errorf("invalid local git path: %w", err)
		}
		c.logger.Info("Initializing local GitOps directory", "path", localPath)
	} else {
		c.logger.Info("Initializing GitOps repository", "url", gitConfig.URL)
	}

	// Create git client
	gitClient, err := git.NewClient(gitConfig)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create git client: %w", err)
	}
	defer func() { _ = gitClient.Cleanup() }()

	// Validate authentication before proceeding (skipped for local paths)
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
		c.logger.Info("GitOps repository already bootstrapped, skipping manifest generation")
		span.SetAttributes(attribute.Bool("skipped", true))
		return nil
	}

	if regenApps {
		c.logger.Info("Regenerating ArgoCD application manifests (--regen-apps)")
	} else {
		c.logger.Info("Bootstrapping GitOps repository with ArgoCD application manifests")
	}

	if err := c.writeConfigToRepo(cfg, gitClient.WorkDir()); err != nil {
		span.RecordError(err)
		return err
	}

	// Write all ArgoCD application manifests and raw K8s manifests to git
	c.logger.Info("Writing ArgoCD application manifests to git repository")
	if err := argocd.WriteAllToGit(ctx, gitClient, cfg, settings); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write application manifests: %w", err)
	}

	// Write bootstrap marker
	if err := gitClient.WriteBootstrapMarker(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write bootstrap marker: %w", err)
	}

	// Commit (and push for remote repos)
	commitMsg := "Bootstrap foundational ArgoCD applications"
	if regenApps {
		commitMsg = "Regenerate foundational ArgoCD applications"
	}
	if err := gitClient.CommitAndPush(ctx, commitMsg); err != nil {
		span.RecordError(err)
		if isLocal {
			return fmt.Errorf("failed to commit: %w", err)
		}
		return fmt.Errorf("failed to commit and push: %w", err)
	}

	if isLocal {
		c.logger.Info("Local GitOps directory bootstrapped successfully", "path", localPath)
	} else {
		c.logger.Info("GitOps repository bootstrapped successfully", "url", gitConfig.URL)
	}
	return nil
}

// writeConfigToRepo serialises cfg (with sensitive fields scrubbed) and
// writes the result into the git working directory. Sourcing from the
// parsed config keeps this feature available to library consumers who
// don't construct cfg from a file.
func (c *Client) writeConfigToRepo(cfg *config.NebariConfig, workDir string) error {
	configBytes, err := yaml.Marshal(scrubbedConfig(cfg))
	if err != nil {
		return fmt.Errorf("marshal scrubbed config to YAML: %w", err)
	}

	configDest := filepath.Join(workDir, "nic-config.yaml")
	if err := os.MkdirAll(filepath.Dir(configDest), 0750); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(configDest, configBytes, 0600); err != nil {
		return fmt.Errorf("write config to repository: %w", err)
	}
	c.logger.Info("Wrote NIC config to repository (auth fields scrubbed)", "path", configDest)
	return nil
}

// scrubbedConfig returns a copy of cfg with sensitive fields zeroed (or
// nilled out where the schema supports omitempty). Operating on the typed
// struct means renaming a sensitive field on the source type fails to
// compile here, instead of silently leaking once a deny-list of string
// keys drifts out of sync.
func scrubbedConfig(cfg *config.NebariConfig) *config.NebariConfig {
	out := *cfg
	if cfg.GitRepository != nil {
		gitRepo := *cfg.GitRepository
		gitRepo.Auth = git.AuthConfig{} // value type with no omitempty: zero the env-var names
		gitRepo.ArgoCDAuth = nil        // *AuthConfig with omitempty: nil → omitted
		out.GitRepository = &gitRepo
	}
	return &out
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
