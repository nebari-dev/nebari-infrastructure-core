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
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
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
		status.Info(ctx, "Starting deployment (dry-run)")
	} else {
		status.Info(ctx, "Starting deployment")
	}

	reg := c.registry

	// Handle context cancellation (from signal interrupt)
	defer func() {
		if ctx.Err() == context.Canceled {
			status.Warning(ctx, "Deployment interrupted by user")
		}
	}()

	// Validate configuration with registered providers
	if err := cfg.Validate(validateOptions(ctx, reg)); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Configuration parsed successfully").
		WithResource("config").
		WithAction("validated").
		WithMetadata("provider", cfg.Cluster.ProviderName()).
		WithMetadata("project_name", cfg.ProjectName))

	// For user-supplied certificates sourced from files/env, validate the
	// material is readable and a valid keypair before provisioning anything.
	// This turns a local config error into a fast failure instead of a silently
	// broken gateway discovered after the cluster is up.
	if err := argocd.PreflightGatewayTLS(cfg); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("gateway TLS certificate: %w", err)
	}

	if opts.Timeout > 0 {
		span.SetAttributes(attribute.String("timeout", opts.Timeout.String()))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Using custom timeout").
			WithMetadata("timeout", opts.Timeout.String()))
	}

	// Get the appropriate provider
	clusterProvider, err := reg.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, "Failed to get provider").
			WithMetadata("provider", cfg.Cluster.ProviderName()).
			WithMetadata("error", err.Error()))
		return nil, fmt.Errorf("get cluster provider %q: %w", cfg.Cluster.ProviderName(), err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Provider selected").
		WithMetadata("provider", clusterProvider.Name()))

	// Resolve the top-level trust bundle once, here at the orchestration layer.
	// The raw PEM feeds trust-manager via the GitOps repo (threaded into
	// bootstrapGitOps) and its base64 form feeds the cluster provider's OS trust
	// store. Resolving once avoids a second disk read and the TOCTOU window
	// between two reads.
	trustPEM, err := cfg.TrustBundle.ResolvePEM()
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("resolve trust_bundle: %w", err)
	}
	var caBundle string
	if trustPEM != "" {
		caBundle = base64.StdEncoding.EncodeToString([]byte(trustPEM))
	}

	// Deploy infrastructure
	if err := clusterProvider.Deploy(ctx, cfg.ProjectName, cfg.Cluster, cluster.DeployOptions{DryRun: opts.DryRun, Timeout: opts.Timeout, TrustBundle: caBundle}); err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, "Deployment failed").
			WithMetadata("provider", clusterProvider.Name()).
			WithMetadata("error", err.Error()))
		return nil, fmt.Errorf("deploy infrastructure: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Infrastructure deployment completed").
		WithMetadata("provider", clusterProvider.Name()))

	// Get provider infrastructure settings for GitOps and foundational services
	infraSettings := clusterProvider.InfraSettings(cfg.Cluster)

	// Resolve the effective GitOps configuration. This may auto-create a
	// local directory for providers that support it, or fall back to the
	// caller's cfg.GitRepository. We never mutate cfg — the resolved value
	// is threaded explicitly into every downstream call that needs it.
	gitConfig, err := c.getOrCreateGitConfig(ctx, cfg, infraSettings.SupportsLocalGitOps)
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelError, "GitOps configuration failed").
			WithMetadata("error", err.Error()))
		return nil, fmt.Errorf("resolve gitops configuration: %w", err)
	}
	if gitConfig != nil && !opts.DryRun {
		if err := c.bootstrapGitOps(ctx, cfg, gitConfig, opts.RegenApps, infraSettings, trustPEM); err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelError, "GitOps bootstrap failed").
				WithMetadata("error", err.Error()))
			return nil, fmt.Errorf("bootstrap gitops: %w", err)
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Deployment completed successfully").
		WithMetadata("provider", clusterProvider.Name()))

	result := &DeployResult{}

	// Install Argo CD (skip in dry-run mode)
	if !opts.DryRun {
		status.Progress(ctx, "Installing Argo CD on cluster")

		// Generate OIDC client secret upfront - needed by both ArgoCD Helm values
		// and the Keycloak realm-setup job
		argoCDClientSecret, err := generateSecurePassword(rand.Reader)
		if err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelError, "Failed to generate ArgoCD client secret").
				WithMetadata("error", err.Error()))
			return nil, fmt.Errorf("generate ArgoCD client secret: %w", err)
		}

		// Build ArgoCD config with Keycloak OIDC SSO
		argoCDConfig := argocd.ConfigWithOIDC(cfg.Domain, infraSettings.KeycloakBasePath, argoCDClientSecret)

		if err := argocd.Install(ctx, cfg, clusterProvider, gitConfig, trustPEM, argoCDConfig); err != nil {
			// Log error but don't fail deployment
			status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install Argo CD").
				WithMetadata("error", err.Error()))
			status.Warning(ctx, "You can install Argo CD manually with: helm install argocd argo/argo-cd --namespace argocd --create-namespace")
		} else {
			status.Success(ctx, "Argo CD installed successfully")
			result.ArgoCDInstalled = true

			// Install foundational services via Argo CD
			status.Progress(ctx, "Installing foundational services")

			secrets, err := generateFoundationalSecrets(rand.Reader)
			if err != nil {
				span.RecordError(err)
				status.Send(ctx, status.NewUpdate(status.LevelError, "Failed to generate foundational secrets").
					WithMetadata("error", err.Error()))
				return nil, fmt.Errorf("generate foundational secrets: %w", err)
			}

			// Generate a Longhorn OIDC client secret only when the provider installs
			// Longhorn. When Longhorn is disabled, longhornClientSecret stays "" and
			// InstallFoundationalServices no-ops on the empty string.
			var longhornClientSecret string
			if infraSettings.LonghornEnabled {
				longhornClientSecret, err = generateSecurePassword(rand.Reader)
				if err != nil {
					span.RecordError(err)
					status.Send(ctx, status.NewUpdate(status.LevelError, "Failed to generate Longhorn client secret").
						WithMetadata("error", err.Error()))
					return nil, fmt.Errorf("generate Longhorn client secret: %w", err)
				}
			}

			foundationalCfg := argocd.FoundationalConfig{
				Keycloak: argocd.KeycloakConfig{
					Enabled:               true,
					AdminUsername:         "admin",
					AdminPassword:         secrets.KeycloakAdmin,
					DBPassword:            secrets.KeycloakDB,
					PostgresAdminPassword: secrets.PostgresAdmin,
					PostgresUserPassword:  secrets.PostgresUser,
					RealmAdminUsername:    "admin",
					RealmAdminPassword:    secrets.RealmAdmin,
					Hostname:              "", // Will be auto-generated from domain
				},
				ArgoCD: argocd.ArgoCDSSOConfig{
					ClientSecret: argoCDClientSecret,
				},
				Longhorn: argocd.LonghornSSOConfig{
					ClientSecret: longhornClientSecret,
				},
				LandingPage: argocd.LandingPageConfig{
					RedisPassword: secrets.Redis,
				},
				// Enable MetalLB only for providers that need it
				MetalLB: argocd.MetalLBConfig{
					Enabled:     infraSettings.NeedsMetalLB,
					AddressPool: infraSettings.MetalLBAddressPool,
				},
			}

			if err := argocd.InstallFoundationalServices(ctx, cfg, clusterProvider, gitConfig, foundationalCfg); err != nil {
				// Log warning but don't fail deployment
				status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to install foundational services").
					WithMetadata("error", err.Error()))
			} else {
				status.Success(ctx, "Foundational services installed successfully")
				result.KeycloakInstalled = true
			}
		}
	} else {
		status.Info(ctx, "Would install Argo CD and foundational services (dry-run mode)")
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
	return &git.Config{
		URL:    fmt.Sprintf("file://%s", git.DefaultLocalPath(projectName)),
		Branch: git.DefaultBranch,
		Path:   "",
		Auth:   git.AuthConfig{},
	}
}

// getOrCreateGitConfig returns the git configuration, creating a default local one if none is configured.
// For providers that support local gitops without explicit git_repository config, this auto-creates
// ~/.nic/gitops/{project_name}. For other providers, explicit git_repository config is required.
// The supportsLocalGitOps parameter comes from cluster.InfraSettings().SupportsLocalGitOps.
func (c *Client) getOrCreateGitConfig(ctx context.Context, cfg *config.NebariConfig, supportsLocalGitOps bool) (*git.Config, error) {
	if cfg.GitRepository != nil {
		return cfg.GitRepository, nil
	}

	// Only auto-create local gitops for providers that support it (e.g., local, kind, k3s)
	// Cloud providers without explicit git_repository config skip GitOps bootstrapping
	if !supportsLocalGitOps {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "No git_repository configured and provider does not support local gitops, skipping GitOps bootstrap").
			WithMetadata("provider", cfg.Cluster.ProviderName()))
		return nil, nil
	}

	gitCfg := defaultGitConfig(cfg.ProjectName)
	localPath, err := gitCfg.GetLocalPath()
	if err != nil {
		return nil, fmt.Errorf("invalid local path in auto-generated git config: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "No git_repository configured, using auto-generated local directory").
		WithMetadata("path", localPath))

	if err := git.EnsureLocalGitOpsDir(ctx, localPath); err != nil {
		return nil, err
	}

	return gitCfg, nil
}

// lookupEndpointAndProvisionDNS gets the load balancer endpoint from the cluster
// and provisions DNS records if a DNS provider is configured. Returns the LB
// endpoint for use in manual DNS guidance (may be nil if lookup failed).
func (c *Client) lookupEndpointAndProvisionDNS(ctx context.Context, cfg *config.NebariConfig, clusterProvider cluster.Provider, reg *registry.Registry) *endpoint.LoadBalancerEndpoint {
	kubeconfigBytes, err := clusterProvider.GetKubeconfig(ctx, cfg.ProjectName, cfg.Cluster)
	if err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Could not get kubeconfig for endpoint lookup").
			WithMetadata("error", err.Error()))
		return nil
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Could not parse kubeconfig for endpoint lookup").
			WithMetadata("error", err.Error()))
		return nil
	}

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Could not create k8s client for endpoint lookup").
			WithMetadata("error", err.Error()))
		return nil
	}

	status.Progress(ctx, "Waiting for load balancer endpoint...")
	lbEndpoint, err := endpoint.GetLoadBalancerEndpoint(ctx, k8sClient)
	if err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Could not retrieve load balancer endpoint").
			WithMetadata("error", err.Error()))
		return nil
	}

	// Provision DNS records if a provider is configured
	if cfg.DNS == nil {
		return lbEndpoint
	}

	if lbEndpoint == nil {
		status.Warning(ctx, "Skipping DNS provisioning: load balancer endpoint not available")
		return nil
	}

	lbEndpointStr := lbEndpoint.Hostname
	if lbEndpointStr == "" {
		lbEndpointStr = lbEndpoint.IP
	}
	if lbEndpointStr == "" {
		status.Warning(ctx, "Load balancer endpoint has no hostname or IP, skipping DNS provisioning")
		return lbEndpoint
	}

	dnsProvider, err := reg.DNSProviders.Get(ctx, cfg.DNS.ProviderName())
	if err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "DNS provider not found, skipping DNS provisioning").
			WithMetadata("provider", cfg.DNS.ProviderName()).
			WithMetadata("error", err.Error()))
		return lbEndpoint
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Provisioning DNS records").
		WithResource("dns").
		WithAction("provisioning").
		WithMetadata("provider", cfg.DNS.ProviderName()).
		WithMetadata("domain", cfg.Domain))
	if err := dnsProvider.ProvisionRecords(ctx, cfg.Domain, cfg.DNS.ProviderConfig(), lbEndpointStr); err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to provision DNS records").
			WithMetadata("error", err.Error()))
		status.Warning(ctx, "You can configure DNS manually - see instructions below")
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "DNS records provisioned successfully").
			WithMetadata("domain", cfg.Domain))
	}

	return lbEndpoint
}

// bootstrapGitOps initializes the GitOps repository with ArgoCD application manifests.
// This is the orchestrator function that handles all I/O operations.
// gitConfig must be non-nil and represents the effective GitOps configuration
// (either cfg.GitRepository or an auto-generated local config).
func (c *Client) bootstrapGitOps(ctx context.Context, cfg *config.NebariConfig, gitConfig *git.Config, regenApps bool, settings cluster.InfraSettings, trustBundlePEM string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.bootstrapGitOps")
	defer span.End()

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
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Initializing local GitOps directory").
			WithMetadata("path", localPath))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Initializing GitOps repository").
			WithMetadata("url", gitConfig.URL))
	}

	// Create git client
	gitClient, err := git.NewClient(gitConfig)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create git client: %w", err)
	}
	defer func() {
		if err := gitClient.Cleanup(); err != nil {
			status.Send(ctx, status.NewUpdate(status.LevelWarning, "Failed to clean up git client temp directory").
				WithMetadata("error", err.Error()))
		}
	}()

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
		status.Info(ctx, "GitOps repository already bootstrapped, skipping manifest generation")
		span.SetAttributes(attribute.Bool("skipped", true))
		return nil
	}

	if regenApps {
		status.Progress(ctx, "Regenerating ArgoCD application manifests (--regen-apps)")
	} else {
		status.Progress(ctx, "Bootstrapping GitOps repository with ArgoCD application manifests")
	}

	if err := c.writeConfigToRepo(ctx, cfg, gitConfig, gitClient.WorkDir(), trustBundlePEM); err != nil {
		span.RecordError(err)
		return err
	}

	// Write all ArgoCD application manifests and raw K8s manifests to git
	status.Progress(ctx, "Writing ArgoCD application manifests to git repository")
	if err := argocd.WriteAllToGit(ctx, gitClient, cfg, gitConfig, settings, trustBundlePEM); err != nil {
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
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Local GitOps directory bootstrapped successfully").
			WithMetadata("path", localPath))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "GitOps repository bootstrapped successfully").
			WithMetadata("url", gitConfig.URL))
	}
	return nil
}

// writeConfigToRepo serialises cfg (with sensitive fields scrubbed and the
// effective gitConfig substituted in) and writes the result into the git
// working directory. Sourcing from the parsed config keeps this feature
// available to library consumers who don't construct cfg from a file.
func (c *Client) writeConfigToRepo(ctx context.Context, cfg *config.NebariConfig, gitConfig *git.Config, workDir string, trustBundlePEM string) error {
	configBytes, err := yaml.Marshal(scrubbedConfig(cfg, gitConfig, trustBundlePEM))
	if err != nil {
		return fmt.Errorf("marshal scrubbed config to YAML: %w", err)
	}

	configDest := filepath.Join(workDir, "nic-config.yaml")
	if err := os.MkdirAll(filepath.Dir(configDest), git.GitOpsDirMode); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(configDest, configBytes, git.GitOpsFileMode); err != nil {
		return fmt.Errorf("write config to repository: %w", err)
	}
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Wrote NIC config to repository (auth fields scrubbed)").
		WithMetadata("path", configDest))
	return nil
}

// scrubbedConfig returns a copy of cfg with sensitive fields zeroed (or
// nilled out where the schema supports omitempty). The gitConfig argument
// supplies the effective GitOps configuration to persist (may differ from
// cfg.GitRepository when the local default was auto-generated); when nil,
// the resulting GitRepository field is also nil. Operating on the typed
// struct means renaming a sensitive field on the source type fails to
// compile here, instead of silently leaking once a deny-list of string
// keys drifts out of sync.
//
// trustBundlePEM is the already-resolved trust bundle (empty when unset). A
// path:-based trust_bundle references a file on the operator's machine, which
// is meaningless (and leaks a local path) in the committed record, so any
// configured bundle is rewritten to its resolved inline form; this also keeps
// the committed config self-contained and reflecting the deployed value.
func scrubbedConfig(cfg *config.NebariConfig, gitConfig *git.Config, trustBundlePEM string) *config.NebariConfig {
	out := *cfg
	out.GitRepository = nil
	if gitConfig != nil {
		gitRepo := *gitConfig
		gitRepo.Auth = git.AuthConfig{} // value type with no omitempty: zero the env-var names
		gitRepo.ArgoCDAuth = nil        // *AuthConfig with omitempty: nil → omitted
		out.GitRepository = &gitRepo
	}
	out.TrustBundle = nil
	if cfg.TrustBundle != nil && trustBundlePEM != "" {
		out.TrustBundle = &config.TrustBundleConfig{Inline: trustBundlePEM}
	}
	return &out
}

// generateSecurePassword generates a cryptographically secure random password.
// It accepts an io.Reader to allow for deterministic testing with known bytes.
// Callers must propagate the error rather than substituting a weaker fallback:
// these strings end up as Keycloak admin / Postgres / Redis credentials on the
// installed cluster.
func generateSecurePassword(r io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	// Encode to base64 and take first 43 characters (removes padding)
	return base64.URLEncoding.EncodeToString(b)[:43], nil
}

// foundationalSecrets bundles the random secrets required to install the
// foundational services (Keycloak, Postgres, Redis).
type foundationalSecrets struct {
	KeycloakAdmin string
	KeycloakDB    string
	PostgresAdmin string
	PostgresUser  string
	RealmAdmin    string
	Redis         string
}

// generateFoundationalSecrets generates the secrets for the foundational
// services in one pass, failing fast on RNG error
func generateFoundationalSecrets(r io.Reader) (foundationalSecrets, error) {
	var s foundationalSecrets
	for _, dst := range []*string{
		&s.KeycloakAdmin,
		&s.KeycloakDB,
		&s.PostgresAdmin,
		&s.PostgresUser,
		&s.RealmAdmin,
		&s.Redis,
	} {
		p, err := generateSecurePassword(r)
		if err != nil {
			return foundationalSecrets{}, fmt.Errorf("generate foundational secret: %w", err)
		}
		*dst = p
	}
	return s, nil
}
