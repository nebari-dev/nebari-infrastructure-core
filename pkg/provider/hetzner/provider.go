package hetzner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const providerName = "hetzner"

// Provider implements the Hetzner Cloud provider using hetzner-k3s.
type Provider struct{}

// NewProvider creates a new Hetzner provider.
func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) Name() string { return providerName }

// parseConfig extracts and validates the Hetzner config from ClusterConfig.
func (p *Provider) parseConfig(ctx context.Context, clusterConfig *config.ClusterConfig) (*Config, error) {
	var hCfg Config
	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg == nil {
		return nil, fmt.Errorf("missing %s configuration block", clusterConfig.ProviderName())
	}
	if err := config.UnmarshalProviderConfig(ctx, rawCfg, &hCfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s config: %w", clusterConfig.ProviderName(), err)
	}
	return &hCfg, nil
}

func (p *Provider) Validate(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", providerName),
		attribute.String("project_name", projectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Validating Hetzner provider configuration").
		WithResource("provider").WithAction("validate"))

	hCfg, err := p.parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if err := hCfg.Validate(); err != nil {
		span.RecordError(err)
		return err
	}

	// Verify HETZNER_TOKEN is set
	if os.Getenv("HETZNER_TOKEN") == "" {
		err := fmt.Errorf("HETZNER_TOKEN environment variable is required")
		span.RecordError(err)
		return err
	}

	// Warn about open network defaults
	for _, warning := range hCfg.NetworkWarnings() {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, warning).
			WithResource("provider").WithAction("validate"))
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Hetzner configuration validated successfully").
		WithResource("provider").WithAction("validate"))

	return nil
}

func (p *Provider) Deploy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts provider.DeployOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", providerName),
		attribute.String("project_name", projectName),
	)

	// Apply deployment timeout if configured
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// Defensive validation in case Deploy is called without Validate
	if os.Getenv("HETZNER_TOKEN") == "" {
		err := fmt.Errorf("HETZNER_TOKEN environment variable is required")
		span.RecordError(err)
		return err
	}

	hCfg, err := p.parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if err := hCfg.Validate(); err != nil {
		span.RecordError(err)
		return err
	}

	cacheDir, err := getHetznerCacheDir()
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Download hetzner-k3s binary
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Ensuring hetzner-k3s binary is available").
		WithResource("provider").WithAction("deploy"))

	downloader := &hetznerK3sDownloader{version: DefaultHetznerK3sVersion, cacheDir: cacheDir}
	binaryPath, err := ensureBinary(ctx, cacheDir, DefaultHetznerK3sVersion, downloader)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get hetzner-k3s binary: %w", err)
	}

	// Ensure SSH keys
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Ensuring SSH keys").
		WithResource("provider").WithAction("deploy"))

	pubKey, privKey, err := ensureSSHKeys(cacheDir, hCfg.SSH)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to ensure SSH keys: %w", err)
	}

	// Resolve k3s version
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Resolving k3s version").
		WithResource("provider").WithAction("deploy"))

	releases, err := getHetznerK3sReleases(ctx, binaryPath)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get k3s releases: %w", err)
	}

	k3sVersion, err := resolveK3sVersion(ctx, hCfg.KubernetesVersion, releases)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to resolve k3s version: %w", err)
	}
	span.SetAttributes(attribute.String("k3s_version", k3sVersion))

	// Set up working directory
	projectDir := filepath.Join(cacheDir, projectName)
	if err := os.MkdirAll(projectDir, 0750); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	kubeconfigPath := filepath.Join(projectDir, "kubeconfig")

	// Generate cluster.yaml
	params := clusterParams{
		ClusterName:    projectName,
		K3sVersion:     k3sVersion,
		SSHPublicKey:   pubKey,
		SSHPrivateKey:  privKey,
		KubeconfigPath: kubeconfigPath,
		Config:         *hCfg,
	}

	clusterYAML, err := generateClusterYAML(params)
	if err != nil {
		span.RecordError(err)
		return err
	}

	clusterYAMLPath, err := writeClusterYAML(projectDir, clusterYAML)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if opts.DryRun {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Dry run: would create Hetzner k3s cluster").
			WithResource("provider").WithAction("deploy").
			WithMetadata("cluster_yaml", clusterYAMLPath))
		return nil
	}

	// Run hetzner-k3s create
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Creating Hetzner k3s cluster").
		WithResource("provider").WithAction("deploy"))

	if err := runHetznerK3s(ctx, binaryPath, "create", clusterYAMLPath); err != nil {
		span.RecordError(err)
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Hetzner k3s cluster created successfully").
		WithResource("provider").WithAction("deploy"))

	// Label volumes for persistence if configured
	if hCfg.PersistData {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Labeling CSI volumes with persist=true").
			WithResource("provider").WithAction("deploy"))

		if err := labelPersistVolumes(ctx); err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Failed to label volumes for persistence: %v", err)).
				WithResource("provider").WithAction("deploy"))
		}
	}

	return nil
}

func (p *Provider) Destroy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts provider.DestroyOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", providerName),
		attribute.String("project_name", projectName),
	)

	// Apply timeout if configured
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	if os.Getenv("HETZNER_TOKEN") == "" {
		err := fmt.Errorf("HETZNER_TOKEN environment variable is required for destroy")
		span.RecordError(err)
		return err
	}

	cacheDir, err := getHetznerCacheDir()
	if err != nil {
		span.RecordError(err)
		return err
	}

	projectDir := filepath.Join(cacheDir, projectName)
	clusterYAMLPath := filepath.Join(projectDir, "cluster.yaml")

	if _, err := os.Stat(clusterYAMLPath); os.IsNotExist(err) {
		return fmt.Errorf("cluster.yaml not found at %s - was this cluster created by NIC?", clusterYAMLPath)
	}

	if opts.DryRun {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Dry run: would destroy Hetzner k3s cluster").
			WithResource("provider").WithAction("destroy").
			WithMetadata("cluster_yaml", clusterYAMLPath))
		return nil
	}

	downloader := &hetznerK3sDownloader{version: DefaultHetznerK3sVersion, cacheDir: cacheDir}
	binaryPath, err := ensureBinary(ctx, cacheDir, DefaultHetznerK3sVersion, downloader)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get hetzner-k3s binary: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Destroying Hetzner k3s cluster").
		WithResource("provider").WithAction("destroy"))

	if err := runHetznerK3s(ctx, binaryPath, "delete", clusterYAMLPath); err != nil {
		span.RecordError(err)
		return err
	}

	// Clean up CSI volumes that aren't marked persist=true
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cleaning up orphaned CSI volumes").
		WithResource("provider").WithAction("destroy"))

	deleted, err := cleanupVolumes(ctx)
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Failed to clean up some volumes: %v", err)).
			WithResource("provider").WithAction("destroy"))
	} else if deleted > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleted %d orphaned CSI volume(s)", deleted)).
			WithResource("provider").WithAction("destroy"))
	}

	// Clean up orphaned CCM load balancers (no targets remaining)
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Cleaning up orphaned load balancers").
		WithResource("provider").WithAction("destroy"))

	deletedLBs, err := cleanupLoadBalancers(ctx)
	if err != nil {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Failed to clean up some load balancers: %v", err)).
			WithResource("provider").WithAction("destroy"))
	} else if deletedLBs > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleted %d orphaned load balancer(s)", deletedLBs)).
			WithResource("provider").WithAction("destroy"))
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Hetzner k3s cluster destroyed successfully").
		WithResource("provider").WithAction("destroy"))

	return nil
}

func (p *Provider) GetKubeconfig(ctx context.Context, projectName string, _ *config.ClusterConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "hetzner.GetKubeconfig")
	defer span.End()

	cacheDir, err := getHetznerCacheDir()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	kubeconfigPath := filepath.Join(cacheDir, projectName, "kubeconfig")
	data, err := os.ReadFile(kubeconfigPath) //nolint:gosec // Path constructed from known cache dir + project name
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to read kubeconfig at %s: %w", kubeconfigPath, err)
	}

	return data, nil
}

func (p *Provider) InfraSettings(clusterConfig *config.ClusterConfig) provider.InfraSettings {
	settings := provider.InfraSettings{
		StorageClass: "hcloud-volumes",
		NeedsMetalLB: false,
	}

	// Derive LB annotations from location.
	// Parse errors are intentionally ignored here: InfraSettings is called after
	// Validate() has already confirmed the config is parseable. If it somehow
	// fails (e.g., nil config in tests), we return valid defaults without annotations.
	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg != nil {
		var hCfg Config
		if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &hCfg); err == nil && hCfg.Location != "" {
			settings.LoadBalancerAnnotations = map[string]string{
				"load-balancer.hetzner.cloud/location": hCfg.Location,
			}
		}
	}

	return settings
}

func (p *Provider) Summary(clusterConfig *config.ClusterConfig) map[string]string {
	result := map[string]string{
		"Provider": "Hetzner Cloud (k3s)",
	}

	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg == nil {
		return result
	}

	var hCfg Config
	if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &hCfg); err != nil {
		return result
	}

	result["Location"] = hCfg.Location
	result["Kubernetes Version"] = hCfg.KubernetesVersion

	masterName, masterGroup := hCfg.MasterGroup()
	if masterName != "" {
		result["Masters"] = fmt.Sprintf("%dx %s", masterGroup.Count, masterGroup.InstanceType)
	}

	for _, w := range hCfg.WorkerGroups() {
		result[fmt.Sprintf("Workers (%s)", w.Name)] = fmt.Sprintf("%dx %s", w.NodeGroup.Count, w.NodeGroup.InstanceType)
	}

	return result
}
