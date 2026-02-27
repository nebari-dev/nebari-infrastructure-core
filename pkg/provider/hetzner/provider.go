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

const (
	providerName = "hetzner"
	configKey    = "hetzner_cloud"
)

// Provider implements the Hetzner Cloud provider using hetzner-k3s.
type Provider struct{}

// NewProvider creates a new Hetzner provider.
func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) Name() string      { return providerName }
func (p *Provider) ConfigKey() string { return configKey }

// parseConfig extracts and validates the Hetzner config from NebariConfig.
func (p *Provider) parseConfig(ctx context.Context, cfg *config.NebariConfig) (*Config, error) {
	var hCfg Config
	rawCfg := cfg.ProviderConfig[configKey]
	if rawCfg == nil {
		return nil, fmt.Errorf("missing %s configuration block", configKey)
	}
	if err := config.UnmarshalProviderConfig(ctx, rawCfg, &hCfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s config: %w", configKey, err)
	}
	return &hCfg, nil
}

func (p *Provider) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "hetzner.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", providerName),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Validating Hetzner provider configuration").
		WithResource("provider").WithAction("validate"))

	hCfg, err := p.parseConfig(ctx, cfg)
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

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Hetzner configuration validated successfully").
		WithResource("provider").WithAction("validate"))

	return nil
}

func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", providerName),
		attribute.String("project_name", cfg.ProjectName),
	)

	hCfg, err := p.parseConfig(ctx, cfg)
	if err != nil {
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
	binaryPath, err := ensureBinary(ctx, cacheDir, downloader)
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

	k3sVersion, err := resolveK3sVersion(ctx, hCfg.KubernetesVersion, "")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to resolve k3s version: %w", err)
	}
	span.SetAttributes(attribute.String("k3s_version", k3sVersion))

	// Set up working directory
	projectDir := filepath.Join(cacheDir, cfg.ProjectName)
	if err := os.MkdirAll(projectDir, 0755); err != nil { //nolint:gosec // Cache directory, not sensitive
		span.RecordError(err)
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	kubeconfigPath := filepath.Join(projectDir, "kubeconfig")

	// Generate cluster.yaml
	params := clusterParams{
		ClusterName:    cfg.ProjectName,
		K3sVersion:     k3sVersion,
		HetznerToken:   os.Getenv("HETZNER_TOKEN"),
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

	if cfg.DryRun {
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

	return nil
}

func (p *Provider) Destroy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "hetzner.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", providerName),
		attribute.String("project_name", cfg.ProjectName),
	)

	cacheDir, err := getHetznerCacheDir()
	if err != nil {
		span.RecordError(err)
		return err
	}

	projectDir := filepath.Join(cacheDir, cfg.ProjectName)
	clusterYAMLPath := filepath.Join(projectDir, "cluster.yaml")

	if _, err := os.Stat(clusterYAMLPath); os.IsNotExist(err) {
		return fmt.Errorf("cluster.yaml not found at %s - was this cluster created by NIC?", clusterYAMLPath)
	}

	downloader := &hetznerK3sDownloader{version: DefaultHetznerK3sVersion, cacheDir: cacheDir}
	binaryPath, err := ensureBinary(ctx, cacheDir, downloader)
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

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Hetzner k3s cluster destroyed successfully").
		WithResource("provider").WithAction("destroy"))

	return nil
}

func (p *Provider) GetKubeconfig(ctx context.Context, cfg *config.NebariConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "hetzner.GetKubeconfig")
	defer span.End()

	cacheDir, err := getHetznerCacheDir()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	kubeconfigPath := filepath.Join(cacheDir, cfg.ProjectName, "kubeconfig")
	data, err := os.ReadFile(kubeconfigPath) //nolint:gosec // Path constructed from known cache dir + project name
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to read kubeconfig at %s: %w", kubeconfigPath, err)
	}

	return data, nil
}

func (p *Provider) InfraSettings(cfg *config.NebariConfig) provider.InfraSettings {
	settings := provider.InfraSettings{
		StorageClass:     "hcloud-volumes",
		NeedsMetalLB:     false,
		KeycloakBasePath: "/auth",
	}

	// Derive LB annotations from location
	rawCfg := cfg.ProviderConfig[configKey]
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

func (p *Provider) Summary(cfg *config.NebariConfig) map[string]string {
	result := map[string]string{
		"Provider": "Hetzner Cloud (k3s)",
	}

	rawCfg := cfg.ProviderConfig[configKey]
	if rawCfg == nil {
		return result
	}

	var hCfg Config
	if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &hCfg); err != nil {
		return result
	}

	result["Location"] = hCfg.Location
	result["Kubernetes Version"] = hCfg.KubernetesVersion
	result["Masters"] = fmt.Sprintf("%dx %s", hCfg.MastersPool.InstanceCount, hCfg.MastersPool.InstanceType)

	for _, pool := range hCfg.WorkerNodePools {
		result[fmt.Sprintf("Workers (%s)", pool.Name)] = fmt.Sprintf("%dx %s", pool.InstanceCount, pool.InstanceType)
	}

	return result
}
