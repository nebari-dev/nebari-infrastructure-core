package openshift

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// Deploy prepares the OpenShift cluster for Nebari's foundational stack.
//
// Crucially, this runs BEFORE NIC installs ArgoCD and the foundational services,
// so applying the SecurityContextConstraints bindings here means the foundational
// pods (argocd-redis, keycloak, envoy-gateway, ...) can schedule on their first
// attempt — the manual `oc adm policy add-scc-to-group` step is no longer needed.
//
// existing mode: apply SCC bindings (+ optional Longhorn). provision mode is not
// yet wired end-to-end (its kubeconfig retrieval is pending) and returns a clear
// error directing the operator to provision then use existing mode.
func (p *Provider) Deploy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts cluster.DeployOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "openshift.Deploy")
	defer span.End()

	cfg, err := extractConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
		attribute.String("mode", cfg.Mode()),
		attribute.Bool("dry_run", opts.DryRun),
	)

	if opts.DryRun {
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Dry run: would prepare OpenShift cluster (mode=%s, scc=%s)", cfg.Mode(), cfg.SCCName())).
			WithResource("provider").WithAction("deploy"))
		return nil
	}

	switch cfg.Mode() {
	case ModeExisting:
		return p.deployExisting(ctx, projectName, clusterConfig, cfg)
	case ModeProvision:
		err := fmt.Errorf("openshift provision-mode deploy is not yet wired end-to-end; provision the ROSA cluster (templates under pkg/providers/cluster/openshift/templates) then deploy with mode: existing")
		span.RecordError(err)
		return err
	default:
		err := fmt.Errorf("invalid openshift mode %q", cfg.Mode())
		span.RecordError(err)
		return err
	}
}

// deployExisting applies the OpenShift-specific prerequisites to an existing
// cluster: SCC bindings for the foundational namespaces, then optional Longhorn.
func (p *Provider) deployExisting(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, cfg *Config) error {
	if cfg.SCCManageEnabled() {
		kubeconfigBytes, err := p.GetKubeconfig(ctx, projectName, clusterConfig)
		if err != nil {
			return fmt.Errorf("failed to get kubeconfig for SCC bootstrap: %w", err)
		}
		namespaces := cfg.sccNamespaces()
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Granting SCC %q to %d namespaces (foundational + %d pack)", cfg.SCCName(), len(namespaces), len(cfg.SCC.ExtraNamespaces))).
			WithResource("scc").WithAction("granting"))
		if err := applySCCBindings(ctx, kubeconfigBytes, namespaces, cfg.SCCName()); err != nil {
			return fmt.Errorf("failed to apply SCC bindings: %w", err)
		}
	}

	if cfg.LonghornEnabled() {
		return fmt.Errorf("openshift: longhorn install is not yet supported on this provider; use a CSI storage_class (default gp3-csi)")
	}

	return nil
}

// Destroy is a no-op for existing clusters (NIC did not provision them).
// provision-mode teardown is pending alongside provision-mode deploy.
func (p *Provider) Destroy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts cluster.DestroyOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "openshift.Destroy")
	defer span.End()

	cfg, err := extractConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
		attribute.String("mode", cfg.Mode()),
		attribute.Bool("dry_run", opts.DryRun),
	)

	if cfg.Mode() == ModeProvision {
		err := fmt.Errorf("openshift provision-mode destroy is not yet wired end-to-end")
		span.RecordError(err)
		return err
	}
	// existing mode: nothing to tear down.
	return nil
}

// Summary returns key configuration details for display purposes.
func (p *Provider) Summary(clusterConfig *config.ClusterConfig) map[string]string {
	result := make(map[string]string)
	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg == nil {
		return result
	}
	var cfg Config
	if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &cfg); err != nil {
		return result
	}

	result["Provider"] = "OpenShift"
	result["Mode"] = cfg.Mode()
	result["Storage Class"] = cfg.StorageClassOrDefault()
	if cfg.SCCManageEnabled() {
		result["SCC"] = cfg.SCCName()
	}
	switch cfg.Mode() {
	case ModeProvision:
		if cfg.Region != "" {
			result["Region"] = cfg.Region
		}
	case ModeExisting:
		if cfg.Context != "" {
			result["Context"] = cfg.Context
		}
	}
	return result
}
