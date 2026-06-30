package openshift

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-exec/tfexec"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
)

// Deploy prepares the OpenShift cluster for Nebari's foundational stack.
//
// Crucially, the SCC bootstrap runs BEFORE NIC installs ArgoCD and the
// foundational services, so the foundational pods (argocd-redis, keycloak,
// envoy-gateway, ...) can schedule on their first attempt — the manual
// `oc adm policy add-scc-to-group` step is no longer needed.
//
//   - existing mode: apply SCC bindings (+ optional Longhorn).
//   - provision mode: stand up a ROSA HCP cluster via OpenTofu, then apply SCC
//     bindings against the freshly provisioned cluster.
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

	switch cfg.Mode() {
	case ModeExisting:
		return p.deployExisting(ctx, projectName, clusterConfig, cfg, opts)
	case ModeProvision:
		return p.deployProvision(ctx, projectName, clusterConfig, cfg, opts)
	default:
		err := fmt.Errorf("invalid openshift mode %q", cfg.Mode())
		span.RecordError(err)
		return err
	}
}

// deployExisting applies the OpenShift-specific prerequisites to an existing
// cluster: SCC bindings for the foundational namespaces, then optional Longhorn.
func (p *Provider) deployExisting(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, cfg *Config, opts cluster.DeployOptions) error {
	if opts.DryRun {
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Dry run: would prepare existing OpenShift cluster (scc=%s)", cfg.SCCName())).
			WithResource("provider").WithAction("deploy"))
		return nil
	}

	if err := p.applySCCIfManaged(ctx, projectName, clusterConfig, cfg); err != nil {
		return err
	}

	if cfg.LonghornEnabled() {
		return fmt.Errorf("openshift: longhorn install is not yet supported on this provider; use a CSI storage_class (default gp3-csi)")
	}
	return nil
}

// deployProvision stands up a ROSA HCP cluster via OpenTofu (with S3 remote
// state, mirroring the aws provider) and then applies the SCC bindings against
// the new cluster.
func (p *Provider) deployProvision(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, cfg *Config, opts cluster.DeployOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "openshift.deployProvision")
	defer span.End()

	if cfg.Region == "" {
		return fmt.Errorf("region is required for openshift provision mode")
	}

	bucketName, s3Client, bucketExists, err := p.resolveStateBucket(ctx, cfg, projectName)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Create the state bucket for real (non-dry-run) operations only.
	if !opts.DryRun {
		if err := ensureStateBucket(ctx, s3Client, cfg.Region, bucketName); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to ensure state bucket: %w", err)
		}
	}

	tfVars, err := cfg.toTFVars(projectName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to resolve terraform variables: %w", err)
	}
	tf, err := tofu.Setup(ctx, tofuTemplates, tfVars)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer func() {
		if cerr := tf.Cleanup(); cerr != nil {
			span.RecordError(cerr)
		}
	}()

	if err := initTofu(ctx, tf, cfg.Region, bucketName, projectName, opts.DryRun, bucketExists); err != nil {
		span.RecordError(err)
		return err
	}

	if opts.DryRun {
		if _, err := tf.Plan(ctx); err != nil {
			span.RecordError(err)
			return err
		}
		return nil
	}

	if err := tf.Apply(ctx); err != nil {
		span.RecordError(err)
		return err
	}

	if err := p.applySCCIfManaged(ctx, projectName, clusterConfig, cfg); err != nil {
		return err
	}

	if cfg.LonghornEnabled() {
		return fmt.Errorf("openshift: longhorn install is not yet supported on this provider; use a CSI storage_class (default gp3-csi)")
	}
	return nil
}

// applySCCIfManaged grants the configured SCC to the foundational (and any extra
// pack) namespaces, unless SCC management is disabled. Shared by both modes.
func (p *Provider) applySCCIfManaged(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, cfg *Config) error {
	if !cfg.SCCManageEnabled() {
		return nil
	}
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
	return nil
}

// Destroy tears down the cluster. In existing mode there is nothing NIC
// provisioned to remove. In provision mode it runs `tofu destroy` against the
// remote state and then deletes the state bucket.
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

	if cfg.Mode() != ModeProvision {
		// existing mode: nothing to tear down.
		return nil
	}
	return p.destroyProvision(ctx, projectName, cfg, opts)
}

// destroyProvision runs `tofu destroy` against the ROSA HCP state and removes the
// state bucket.
func (p *Provider) destroyProvision(ctx context.Context, projectName string, cfg *Config, opts cluster.DestroyOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "openshift.destroyProvision")
	defer span.End()

	if cfg.Region == "" {
		return fmt.Errorf("region is required for openshift provision mode")
	}

	// Drop any cached kubeconfig so a follow-up deploy in the same process does
	// not reuse credentials for a torn-down cluster.
	if !opts.DryRun {
		defer p.invalidateKubeconfigCache("provision:" + projectName)
	}

	bucketName, s3Client, bucketExists, err := p.resolveStateBucket(ctx, cfg, projectName)
	if err != nil {
		span.RecordError(err)
		return err
	}

	tfVars, err := cfg.toTFVars(projectName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to resolve terraform variables: %w", err)
	}
	tf, err := tofu.Setup(ctx, tofuTemplates, tfVars)
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer func() {
		if cerr := tf.Cleanup(); cerr != nil {
			span.RecordError(cerr)
		}
	}()

	if err := initTofu(ctx, tf, cfg.Region, bucketName, projectName, opts.DryRun, bucketExists); err != nil {
		span.RecordError(err)
		return err
	}

	if opts.DryRun {
		if _, err := tf.Plan(ctx, tfexec.Destroy(true)); err != nil {
			span.RecordError(err)
			return err
		}
		return nil
	}

	// Capture the VPC id from state BEFORE destroying, so that if `tofu destroy`
	// trips on a VPC DependencyViolation we can sweep the ROSA/k8s-created
	// orphans (PrivateLink SG, Gateway NLB + ENIs) that are not in our state and
	// retry. See cleanup.go for why these block VPC deletion.
	vpcID := vpcIDFromOutputs(ctx, tf)

	var derr error
	for attempt := 1; attempt <= destroyAttempts; attempt++ {
		derr = tf.Destroy(ctx)
		if derr == nil {
			break
		}
		if vpcID == "" || attempt == destroyAttempts {
			break
		}
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			fmt.Sprintf("destroy attempt %d failed (%v); sweeping orphaned VPC resources and retrying", attempt, derr)).
			WithResource("vpc").WithAction("cleanup"))
		if serr := sweepVPCOrphans(ctx, cfg.Region, vpcID); serr != nil {
			status.Send(ctx, status.NewUpdate(status.LevelWarning,
				fmt.Sprintf("VPC orphan sweep incomplete: %v", serr)).
				WithResource("vpc").WithAction("cleanup"))
		}
	}
	if derr != nil {
		span.RecordError(derr)
		return derr
	}

	if err := destroyStateBucket(ctx, s3Client, cfg.Region, bucketName); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to destroy state bucket: %w", err)
	}
	return nil
}

// destroyAttempts bounds how many times destroyProvision runs `tofu destroy`,
// sweeping orphaned VPC resources between tries.
const destroyAttempts = 3

// vpcIDFromOutputs reads the vpc_id output from current state, or "" if
// unavailable (e.g. state already empty). Best-effort — a missing id just
// disables the orphan sweep.
func vpcIDFromOutputs(ctx context.Context, tf *tofu.TerraformExecutor) string {
	outs, err := tf.Output(ctx)
	if err != nil {
		return ""
	}
	o, ok := outs["vpc_id"]
	if !ok {
		return ""
	}
	var id string
	if err := json.Unmarshal(o.Value, &id); err != nil {
		return ""
	}
	return id
}

// resolveStateBucket determines the state bucket name (config override or derived
// from the AWS account ID), constructs an S3 client, and reports whether the
// bucket already exists.
func (p *Provider) resolveStateBucket(ctx context.Context, cfg *Config, projectName string) (bucketName string, s3Client S3Client, exists bool, err error) {
	bucketName = cfg.StateBucket
	if bucketName == "" {
		stsClient, serr := newSTSClient(ctx, cfg.Region)
		if serr != nil {
			return "", nil, false, fmt.Errorf("failed to create STS client: %w", serr)
		}
		bucketName, err = getStateBucketName(ctx, stsClient, cfg.Region, projectName)
		if err != nil {
			return "", nil, false, fmt.Errorf("failed to get state bucket name: %w", err)
		}
	}

	s3Client, err = newS3Client(ctx, cfg.Region)
	if err != nil {
		return "", nil, false, fmt.Errorf("failed to create S3 client: %w", err)
	}
	exists, err = stateBucketExists(ctx, s3Client, bucketName)
	if err != nil {
		return "", nil, false, err
	}
	return bucketName, s3Client, exists, nil
}

// initTofu runs `tofu init`. On a first-time dry run (state bucket absent) it
// overrides the S3 backend with a local backend so no cloud state is created;
// otherwise it configures the S3 backend dynamically (bucket/key/region).
func initTofu(ctx context.Context, tf *tofu.TerraformExecutor, region, bucketName, projectName string, dryRun, bucketExists bool) error {
	if dryRun && !bucketExists {
		if err := tf.WriteBackendOverride(); err != nil {
			return err
		}
		return tf.Init(ctx)
	}
	return tf.Init(ctx,
		tfexec.BackendConfig(fmt.Sprintf("bucket=%s", bucketName)),
		tfexec.BackendConfig(fmt.Sprintf("key=%s", stateKey(projectName))),
		tfexec.BackendConfig(fmt.Sprintf("region=%s", region)),
	)
}

// invalidateKubeconfigCache removes a cached kubeconfig entry by key.
func (p *Provider) invalidateKubeconfigCache(cacheKey string) {
	p.kubeconfigMu.Lock()
	delete(p.kubeconfigCache, cacheKey)
	p.kubeconfigMu.Unlock()
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
