package local

import (
	"context"
	"fmt"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// ProviderName is the identifier for the local provider.
	ProviderName = "local"
	// defaultStorageClass is the StorageClass kind installs by default
	defaultStorageClass = "standard"
)

// Provider implements the local kind provider
type Provider struct{}

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

	for _, p := range []struct {
		name string
		port int
	}{
		{"http_port", localCfg.HTTPPort},
		{"https_port", localCfg.HTTPSPort},
	} {
		if p.port < 0 || p.port > 65535 {
			err := fmt.Errorf("%s must be between 1 and 65535 or omitted, got %d", p.name, p.port)
			span.RecordError(err)
			return err
		}
	}

	// Compare the ports kind will actually map (hostPort applies the 80/443
	// defaults), so a collision with a defaulted port is also caught. kind
	// rejects duplicate port mappings too, but only after provisioning has
	// started.
	if hostPort(localCfg.HTTPPort, defaultHTTPPort) == hostPort(localCfg.HTTPSPort, defaultHTTPSPort) {
		err := fmt.Errorf("http_port and https_port must differ, both are %d", hostPort(localCfg.HTTPPort, defaultHTTPPort))
		span.RecordError(err)
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Successfully validated local provider configuration").
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", projectName))

	return nil
}

// Deploy creates the NIC-managed kind cluster (named after the project) if it
// does not already exist. The gateway's NodePorts are published on host ports
// at creation time (see gatewayPortMappings), so the platform is reachable at
// 127.0.0.1 without any load-balancer or DNS setup.
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

		// Fail before anything is rendered when the configured ports differ
		// from the ones the cluster was created with: the mappings cannot
		// change on a live cluster, so continuing would move the gateway
		// listeners and printed URLs to ports the host does not publish.
		client, err := clusterClient(kp, projectName)
		if err != nil {
			span.RecordError(err)
			return err
		}
		if err := verifyClusterPorts(ctx, client, projectName,
			hostPort(localCfg.HTTPPort, defaultHTTPPort), hostPort(localCfg.HTTPSPort, defaultHTTPSPort)); err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelError, "Configured host ports do not match the existing kind cluster").
				WithResource("provider").
				WithAction("deploy").
				WithMetadata("cluster_name", projectName).
				WithMetadata("error", err.Error()))
			return err
		}
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Creating kind cluster %s", projectName)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("cluster_name", projectName).
			WithMetadata("gitops_path", config.DefaultLocalRepositoryPath(projectName)))

		if err := createKindCluster(ctx, kp, projectName, kindCfg, localCfg.HTTPPort, localCfg.HTTPSPort); err != nil {
			span.RecordError(err)
			return err
		}

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Kind cluster %s created", projectName)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("cluster_name", projectName).
			WithMetadata("kube_context", kindContextName(projectName)))

		// Record the ports the cluster was just created with, so the next
		// deploy can verify them. A failed write costs only that check (a
		// later deploy adopts and records the values it finds configured),
		// so it does not fail a deploy that is otherwise healthy.
		client, err := clusterClient(kp, projectName)
		if err == nil {
			err = recordClusterPorts(ctx, client,
				hostPort(localCfg.HTTPPort, defaultHTTPPort), hostPort(localCfg.HTTPSPort, defaultHTTPSPort))
		}
		if err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelWarning, "Could not record the cluster's host ports, so the next deploy cannot verify them against the config").
				WithResource("provider").
				WithAction("deploy").
				WithMetadata("cluster_name", projectName).
				WithMetadata("error", err.Error()))
		}
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
		GatewayHostAddress:  gatewayHostAddress,
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

	return settings
}
