package azure

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-exec/tfexec"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
)

const (
	subscriptionIDEnv    = "AZURE_SUBSCRIPTION_ID"
	armSubscriptionIDEnv = "ARM_SUBSCRIPTION_ID"
)

// Provider implements the Azure cloud provider for NIC.
type Provider struct{}

// NewProvider returns a fresh Azure provider. Registered in cmd/nic/main.go.
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name used in cluster.azure: dispatch.
func (p *Provider) Name() string { return "azure" }

func (p *Provider) parseConfig(ctx context.Context, clusterConfig *config.ClusterConfig) (*Config, error) {
	raw := clusterConfig.ProviderConfig()
	if raw == nil {
		return nil, fmt.Errorf("cluster.azure block is missing")
	}
	var cfg Config
	if err := config.UnmarshalProviderConfig(ctx, raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse azure config: %w", err)
	}
	return &cfg, nil
}

// Validate checks config integrity and probes Azure auth via env vars.
func (p *Provider) Validate(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.Validate")
	defer span.End()
	span.SetAttributes(
		attribute.String("provider", "azure"),
		attribute.String("project_name", projectName),
	)

	cfg, err := p.parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if err := cfg.Validate(); err != nil {
		span.RecordError(err)
		return err
	}

	if os.Getenv(subscriptionIDEnv) == "" {
		err := fmt.Errorf("%s environment variable is required", subscriptionIDEnv)
		span.RecordError(err)
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Azure configuration validated").
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", projectName))
	return nil
}

// Deploy provisions the Azure AKS cluster by invoking the embedded OpenTofu
// shim. It writes terraform.tfvars.json from the parsed Config, runs
// `tofu init` and `tofu apply`, and streams output through the status channel.
func (p *Provider) Deploy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, _ provider.DeployOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.Deploy")
	defer span.End()
	span.SetAttributes(
		attribute.String("provider", "azure"),
		attribute.String("project_name", projectName),
	)

	cfg, err := p.parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if err := cfg.Validate(); err != nil {
		span.RecordError(err)
		return err
	}

	subID := os.Getenv(subscriptionIDEnv)
	if subID == "" {
		err := fmt.Errorf("%s environment variable is required", subscriptionIDEnv)
		span.RecordError(err)
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Ensuring Terraform state backend resources").
		WithResource("state-backend").
		WithAction("bootstrap").
		WithMetadata("cluster_name", projectName))
	backend, err := ensureStateBackend(ctx, subID, cfg.Region, projectName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("bootstrap state backend: %w", err)
	}

	tf, err := tofu.Setup(ctx, tofuTemplates, cfg.toTFVars(projectName))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu setup: %w", err)
	}
	defer func() {
		if cleanupErr := tf.Cleanup(); cleanupErr != nil {
			span.RecordError(cleanupErr)
		}
	}()

	// The azurerm provider reads ARM_SUBSCRIPTION_ID; map it from the
	// user-facing AZURE_SUBSCRIPTION_ID and scope it to the child tofu
	// process so the parent process env is left untouched.
	if err := tf.SetExtraEnv(map[string]string{armSubscriptionIDEnv: subID}); err != nil {
		span.RecordError(err)
		return fmt.Errorf("set tofu env: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Initializing OpenTofu working directory").
		WithResource("tofu").
		WithAction("init").
		WithMetadata("cluster_name", projectName))
	if err := tf.Init(ctx,
		tfexec.BackendConfig(fmt.Sprintf("resource_group_name=%s", backend.RGName)),
		tfexec.BackendConfig(fmt.Sprintf("storage_account_name=%s", backend.SAName)),
		tfexec.BackendConfig(fmt.Sprintf("container_name=%s", backend.Container)),
		tfexec.BackendConfig(fmt.Sprintf("key=%s", backend.Key)),
	); err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu init: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Applying Terraform plan").
		WithResource("tofu").
		WithAction("apply").
		WithMetadata("cluster_name", projectName))
	if err := tf.Apply(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu apply: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Azure cluster deployed").
		WithResource("cluster").
		WithAction("deploy").
		WithMetadata("cluster_name", projectName))
	return nil
}

// Destroy tears down the Azure AKS cluster by running `tofu destroy` against
// the same state backend used by Deploy. After tofu completes, cleanupOrphans
// reports any tagged resources tofu missed (e.g., AKS-managed MC_* siblings)
// as a non-fatal warning so users can clean them up manually.
func (p *Provider) Destroy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, _ provider.DestroyOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.Destroy")
	defer span.End()
	span.SetAttributes(
		attribute.String("provider", "azure"),
		attribute.String("project_name", projectName),
	)

	cfg, err := p.parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if err := cfg.Validate(); err != nil {
		span.RecordError(err)
		return err
	}

	subID := os.Getenv(subscriptionIDEnv)
	if subID == "" {
		err := fmt.Errorf("%s environment variable is required", subscriptionIDEnv)
		span.RecordError(err)
		return err
	}

	// ensureStateBackend is idempotent; on Destroy it's a no-op when the
	// RG/SA/container already exist (the common case) and reconstructs them
	// only if a partial-deploy left them missing — which would also mean
	// there's nothing to destroy, but tofu init still needs the backend
	// resources reachable to read state.
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Verifying Terraform state backend resources").
		WithResource("state-backend").
		WithAction("verify").
		WithMetadata("cluster_name", projectName))
	backend, err := ensureStateBackend(ctx, subID, cfg.Region, projectName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("ensure state backend: %w", err)
	}

	tf, err := tofu.Setup(ctx, tofuTemplates, cfg.toTFVars(projectName))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu setup: %w", err)
	}
	defer func() {
		if cleanupErr := tf.Cleanup(); cleanupErr != nil {
			span.RecordError(cleanupErr)
		}
	}()

	// The azurerm provider reads ARM_SUBSCRIPTION_ID; map it from the
	// user-facing AZURE_SUBSCRIPTION_ID and scope it to the child tofu
	// process so the parent process env is left untouched.
	if err := tf.SetExtraEnv(map[string]string{armSubscriptionIDEnv: subID}); err != nil {
		span.RecordError(err)
		return fmt.Errorf("set tofu env: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Initializing OpenTofu working directory").
		WithResource("tofu").
		WithAction("init").
		WithMetadata("cluster_name", projectName))
	if err := tf.Init(ctx,
		tfexec.BackendConfig(fmt.Sprintf("resource_group_name=%s", backend.RGName)),
		tfexec.BackendConfig(fmt.Sprintf("storage_account_name=%s", backend.SAName)),
		tfexec.BackendConfig(fmt.Sprintf("container_name=%s", backend.Container)),
		tfexec.BackendConfig(fmt.Sprintf("key=%s", backend.Key)),
	); err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu init: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Destroying Terraform-managed resources").
		WithResource("tofu").
		WithAction("destroy").
		WithMetadata("cluster_name", projectName))
	if err := tf.Destroy(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu destroy: %w", err)
	}

	// Best-effort orphan check (non-fatal — user can rerun nic destroy or az resource delete).
	if err := cleanupOrphans(ctx, subID, projectName); err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Orphan cleanup encountered issues").
			WithResource("cleanup").
			WithAction("destroy").
			WithMetadata("error", err.Error()))
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Azure cluster destroyed").
		WithResource("cluster").
		WithAction("destroy").
		WithMetadata("cluster_name", projectName))
	return nil
}

// GetKubeconfig is implemented in Task 17.
func (p *Provider) GetKubeconfig(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) ([]byte, error) {
	return nil, fmt.Errorf("azure.GetKubeconfig: not implemented in this commit")
}

// Summary returns display-only metadata about the cluster from config.
func (p *Provider) Summary(clusterConfig *config.ClusterConfig) map[string]string {
	out := make(map[string]string)
	cfg, err := p.parseConfig(context.Background(), clusterConfig)
	if err != nil {
		return out
	}
	out["Region"] = cfg.Region
	if cfg.ResourceGroupName != "" {
		out["ResourceGroup"] = cfg.ResourceGroupName
	}
	out["NodeGroupCount"] = strconv.Itoa(len(cfg.NodeGroups))
	return out
}

// InfraSettings returns Azure-specific Kubernetes infra settings.
func (p *Provider) InfraSettings(_ *config.ClusterConfig) provider.InfraSettings {
	return provider.InfraSettings{
		StorageClass: "managed-csi",
		NeedsMetalLB: false,
	}
}
