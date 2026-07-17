package local

import (
	"context"
	"fmt"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// ProviderName is the identifier for the local provider.
	ProviderName = "local"
	// defaultStorageClass is the StorageClass kind installs by default
	defaultStorageClass = "standard"
	// defaultMetalLBAddressPool is the fallback MetalLB pool used only when
	// the kind node network cannot be inspected (e.g. dry-run, or no
	// container runtime). The normal path derives the pool from the node IP.
	defaultMetalLBAddressPool = "192.168.1.100-192.168.1.110"
)

// Provider implements the local kind provider
type Provider struct {
	// metalLBPool caches the MetalLB address pool derived from the kind node
	// network during Deploy, for InfraSettings to read. InfraSettings has no
	// projectName or runtime access of its own, and deriving from the live
	// environment there would make unit tests depend on whatever kind
	// clusters happen to be running. Keying is unnecessary: every kind
	// cluster on a host shares the one "kind" Docker network, so the pool is
	// identical for all local clusters. Empty until Deploy runs, in which
	// case InfraSettings falls back to defaultMetalLBAddressPool.
	//
	// No locking: Deploy (writer) and InfraSettings (reader) run sequentially
	// on the same goroutine in the deploy flow, and the local provider is not
	// used to deploy multiple clusters concurrently.
	metalLBPool string
}

// NewProvider creates a new local provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return ProviderName
}

// parseConfig unmarshals the local provider config block, returning a zero
// Config when no block is present (all fields then take their defaults).
func parseConfig(ctx context.Context, clusterConfig *config.ClusterConfig) (Config, error) {
	var localCfg Config
	if rawCfg := clusterConfig.ProviderConfig(); rawCfg != nil {
		if err := config.UnmarshalProviderConfig(ctx, rawCfg, &localCfg); err != nil {
			return Config{}, fmt.Errorf("failed to unmarshal local config: %w", err)
		}
	}
	return localCfg, nil
}

// Validate validates the local configuration
func (p *Provider) Validate(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Validating local provider configuration").
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", projectName))

	localCfg, err := parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if localCfg.Kind != nil {
		for _, m := range localCfg.Kind.ExtraMounts {
			if !filepath.IsAbs(m.HostPath) || !filepath.IsAbs(m.ContainerPath) {
				err := fmt.Errorf("kind extra_mounts paths must be absolute: %s -> %s", m.HostPath, m.ContainerPath)
				span.RecordError(err)
				return err
			}
		}
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Successfully validated local provider configuration").
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", projectName))

	return nil
}

// Deploy creates the NIC-managed kind cluster (named after the project) if it
// does not already exist, then derives the MetalLB address pool from the
// cluster's network for InfraSettings to consume.
func (p *Provider) Deploy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts cluster.DeployOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
	)

	localCfg, err := parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}
	kindCfg := localCfg.Kind
	if kindCfg == nil {
		kindCfg = &KindConfig{}
	}

	if opts.DryRun {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Would create kind cluster %s (dry-run)", projectName)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("cluster_name", projectName))
		return nil
	}

	kp, err := newKindProvider()
	if err != nil {
		span.RecordError(err)
		return err
	}

	exists, err := kindClusterExists(ctx, kp, projectName)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if exists {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Kind cluster %s already exists, reusing it (changes to kind settings such as node_image, extra_mounts, or the default local GitOps mount path only take effect on a recreate)", projectName)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("cluster_name", projectName))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Creating kind cluster %s", projectName)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("cluster_name", projectName).
			WithMetadata("gitops_path", git.DefaultLocalPath(projectName)))

		if err := createKindCluster(ctx, kp, projectName, kindCfg); err != nil {
			span.RecordError(err)
			return err
		}

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Kind cluster %s created", projectName)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("cluster_name", projectName).
			WithMetadata("kube_context", kindContextName(projectName)))
	}

	// Derive the MetalLB pool from the cluster's network for InfraSettings.
	// On failure we keep the static default and say so, rather
	// than failing the deploy over a load-balancer address range.
	if pool, err := kindNodeAddressPool(ctx, kp, projectName); err == nil {
		p.metalLBPool = pool
		span.SetAttributes(attribute.String("metallb_address_pool", pool))
	} else {
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Could not derive MetalLB address pool from the kind network, using default %s", defaultMetalLBAddressPool)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("cluster_name", projectName).
			WithMetadata("error", err.Error()))
	}

	return nil
}

// Destroy deletes the NIC-managed kind cluster. The shared "kind" Docker
// network is intentionally left in place: it is shared across all kind
// clusters, so a single-cluster Destroy must not remove it (kind reaps it
// itself once no clusters remain).
func (p *Provider) Destroy(ctx context.Context, projectName string, _ *config.ClusterConfig, opts cluster.DestroyOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
	)

	if opts.DryRun {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Would delete kind cluster %s (dry-run)", projectName)).
			WithResource("provider").
			WithAction("destroy").
			WithMetadata("cluster_name", projectName))
		return nil
	}

	kp, err := newKindProvider()
	if err != nil {
		span.RecordError(err)
		return err
	}

	exists, err := kindClusterExists(ctx, kp, projectName)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if !exists {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Kind cluster %s not found, nothing to destroy", projectName)).
			WithResource("provider").
			WithAction("destroy").
			WithMetadata("cluster_name", projectName))
		return nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Deleting kind cluster %s", projectName)).
		WithResource("provider").
		WithAction("destroy").
		WithMetadata("cluster_name", projectName))

	if err := kp.Delete(projectName, ""); err != nil {
		span.RecordError(err)
		return fmt.Errorf("delete kind cluster %s: %w", projectName, err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Kind cluster %s deleted", projectName)).
		WithResource("provider").
		WithAction("destroy").
		WithMetadata("cluster_name", projectName))
	return nil
}

// GetKubeconfig retrieves the kubeconfig for the NIC-managed kind cluster
// straight from kind (internal=false yields host-reachable API server URLs,
// which is what nic running on the host needs).
func (p *Provider) GetKubeconfig(ctx context.Context, projectName string, _ *config.ClusterConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("cluster_name", projectName),
	)

	kp, err := newKindProvider()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	kubeconfigStr, err := kp.KubeConfig(projectName, false)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get kubeconfig for kind cluster %s: %w", projectName, err)
	}

	span.SetAttributes(attribute.Int("kubeconfig_size_bytes", len(kubeconfigStr)))
	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Retrieved kubeconfig for kind cluster %s", projectName)).
		WithResource("provider").
		WithAction("get-kubeconfig").
		WithMetadata("cluster_name", projectName).
		WithMetadata("kube_context", kindContextName(projectName)))
	return []byte(kubeconfigStr), nil
}

// Summary returns key configuration details for display purposes
func (p *Provider) Summary(clusterConfig *config.ClusterConfig) map[string]string {
	result := map[string]string{
		"Kind Cluster": "managed by NIC (created on deploy, deleted on destroy)",
	}

	localCfg, err := parseConfig(context.Background(), clusterConfig)
	if err != nil {
		return result
	}
	if localCfg.Kind != nil && localCfg.Kind.NodeImage != "" {
		result["Kind Node Image"] = localCfg.Kind.NodeImage
	}
	return result
}

// InfraSettings returns local provider Kubernetes infrastructure settings.
// Values are read from the local provider config block, falling back to defaults.
// Parse errors are intentionally ignored: InfraSettings is called after Validate()
// has confirmed the config is parseable. If it somehow fails (e.g., nil config in
// tests), we return valid defaults.
// LonghornEnabled is false: Longhorn is not yet wired for the local provider.
func (p *Provider) InfraSettings(cfg *config.ClusterConfig) cluster.InfraSettings {
	settings := cluster.InfraSettings{
		StorageClass:        defaultStorageClass,
		NeedsMetalLB:        true,
		MetalLBAddressPool:  defaultMetalLBAddressPool,
		SupportsLocalGitOps: true,
		LonghornEnabled:     false,
	}

	localCfg, err := parseConfig(context.Background(), cfg)
	if err != nil {
		return settings
	}

	if localCfg.HTTPSPort != 0 {
		settings.HTTPSPort = localCfg.HTTPSPort
	}

	explicitPool := localCfg.MetalLB != nil && localCfg.MetalLB.AddressPool != ""
	if explicitPool {
		settings.MetalLBAddressPool = localCfg.MetalLB.AddressPool
	}

	// Explicit pool takes precedence. When no explicit pool was configured, use
	// the pool derived from the live cluster network during Deploy, so MetalLB IPs
	// are routable on whatever subnet Docker picked. If Deploy produced none
	// (dry-run, tests), defaultMetalLBAddressPool is kept.
	// This relies on Deploy having populated p.metalLBPool on this same Provider
	// instance before InfraSettings is called.
	if !explicitPool && p.metalLBPool != "" {
		settings.MetalLBAddressPool = p.metalLBPool
	}

	return settings
}
