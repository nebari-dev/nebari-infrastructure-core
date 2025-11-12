# Terraform-Exec Integration

### 6.1 Wrapper Package Design

**pkg/tofu/executor.go:**
```go
package tofu

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    "github.com/hashicorp/terraform-exec/tfexec"
    "log/slog"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("github.com/nebari-dev/nic/pkg/tofu")

type Executor struct {
    workingDir string
    tofuPath   string
    tf         *tfexec.Terraform
}

// NewExecutor creates a new OpenTofu executor
func NewExecutor(workingDir string, tofuPath string) (*Executor, error) {
    tf, err := tfexec.NewTerraform(workingDir, tofuPath)
    if err != nil {
        return nil, fmt.Errorf("creating terraform executor: %w", err)
    }

    return &Executor{
        workingDir: workingDir,
        tofuPath:   tofuPath,
        tf:         tf,
    }, nil
}

// Init initializes the Terraform working directory
func (e *Executor) Init(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "Executor.Init")
    defer span.End()

    span.SetAttributes(
        attribute.String("working_dir", e.workingDir),
    )

    slog.InfoContext(ctx, "initializing OpenTofu", "working_dir", e.workingDir)

    if err := e.tf.Init(ctx, tfexec.Upgrade(true)); err != nil {
        span.RecordError(err)
        return fmt.Errorf("terraform init: %w", err)
    }

    slog.InfoContext(ctx, "OpenTofu initialized successfully")
    return nil
}

// Plan generates an execution plan
func (e *Executor) Plan(ctx context.Context, varFiles []string) (bool, error) {
    ctx, span := tracer.Start(ctx, "Executor.Plan")
    defer span.End()

    span.SetAttributes(
        attribute.StringSlice("var_files", varFiles),
    )

    slog.InfoContext(ctx, "planning infrastructure changes")

    var opts []tfexec.PlanOption
    for _, vf := range varFiles {
        opts = append(opts, tfexec.VarFile(vf))
    }

    hasChanges, err := e.tf.Plan(ctx, opts...)
    if err != nil {
        span.RecordError(err)
        return false, fmt.Errorf("terraform plan: %w", err)
    }

    span.SetAttributes(
        attribute.Bool("has_changes", hasChanges),
    )

    if hasChanges {
        slog.InfoContext(ctx, "infrastructure changes detected")
    } else {
        slog.InfoContext(ctx, "no infrastructure changes needed")
    }

    return hasChanges, nil
}

// Apply applies the Terraform configuration
func (e *Executor) Apply(ctx context.Context, varFiles []string) error {
    ctx, span := tracer.Start(ctx, "Executor.Apply")
    defer span.End()

    span.SetAttributes(
        attribute.StringSlice("var_files", varFiles),
    )

    slog.InfoContext(ctx, "applying infrastructure changes")

    var opts []tfexec.ApplyOption
    for _, vf := range varFiles {
        opts = append(opts, tfexec.VarFile(vf))
    }

    if err := e.tf.Apply(ctx, opts...); err != nil {
        span.RecordError(err)
        return fmt.Errorf("terraform apply: %w", err)
    }

    slog.InfoContext(ctx, "infrastructure applied successfully")
    return nil
}

// Destroy destroys the Terraform-managed infrastructure
func (e *Executor) Destroy(ctx context.Context, varFiles []string) error {
    ctx, span := tracer.Start(ctx, "Executor.Destroy")
    defer span.End()

    slog.InfoContext(ctx, "destroying infrastructure")

    var opts []tfexec.DestroyOption
    for _, vf := range varFiles {
        opts = append(opts, tfexec.VarFile(vf))
    }

    if err := e.tf.Destroy(ctx, opts...); err != nil {
        span.RecordError(err)
        return fmt.Errorf("terraform destroy: %w", err)
    }

    slog.InfoContext(ctx, "infrastructure destroyed successfully")
    return nil
}

// Output retrieves Terraform outputs
func (e *Executor) Output(ctx context.Context) (map[string]tfexec.OutputMeta, error) {
    ctx, span := tracer.Start(ctx, "Executor.Output")
    defer span.End()

    slog.InfoContext(ctx, "retrieving Terraform outputs")

    outputs, err := e.tf.Output(ctx)
    if err != nil {
        span.RecordError(err)
        return nil, fmt.Errorf("terraform output: %w", err)
    }

    span.SetAttributes(
        attribute.Int("output_count", len(outputs)),
    )

    return outputs, nil
}

// Show retrieves the current state
func (e *Executor) Show(ctx context.Context) (*tfexec.State, error) {
    ctx, span := tracer.Start(ctx, "Executor.Show")
    defer span.End()

    slog.InfoContext(ctx, "retrieving Terraform state")

    state, err := e.tf.Show(ctx)
    if err != nil {
        span.RecordError(err)
        return nil, fmt.Errorf("terraform show: %w", err)
    }

    return state, nil
}
```

### 6.2 Provider Implementation with terraform-exec

**pkg/provider/tofu_provider.go:**
```go
package provider

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"

    "github.com/nebari-dev/nic/pkg/config"
    "github.com/nebari-dev/nic/pkg/tofu"
    "log/slog"
    "go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("github.com/nebari-dev/nic/pkg/provider")

type TofuProvider struct {
    config     *config.Config
    workingDir string
    executor   *tofu.Executor
}

func NewTofuProvider(cfg *config.Config, workingDir string) (*TofuProvider, error) {
    // Find OpenTofu binary
    tofuPath, err := findTofuBinary()
    if err != nil {
        return nil, fmt.Errorf("finding tofu binary: %w", err)
    }

    // Create executor
    executor, err := tofu.NewExecutor(workingDir, tofuPath)
    if err != nil {
        return nil, fmt.Errorf("creating tofu executor: %w", err)
    }

    return &TofuProvider{
        config:     cfg,
        workingDir: workingDir,
        executor:   executor,
    }, nil
}

func (p *TofuProvider) Deploy(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "TofuProvider.Deploy")
    defer span.End()

    slog.InfoContext(ctx, "starting deployment", "provider", p.config.Provider.Type)

    // 1. Generate backend configuration
    if err := p.generateBackendConfig(ctx); err != nil {
        return fmt.Errorf("generating backend config: %w", err)
    }

    // 2. Generate tfvars from config.yaml
    if err := p.generateTfvars(ctx); err != nil {
        return fmt.Errorf("generating tfvars: %w", err)
    }

    // 3. Initialize Terraform
    if err := p.executor.Init(ctx); err != nil {
        return fmt.Errorf("initializing terraform: %w", err)
    }

    // 4. Plan
    hasChanges, err := p.executor.Plan(ctx, []string{"terraform.tfvars"})
    if err != nil {
        return fmt.Errorf("planning infrastructure: %w", err)
    }

    if !hasChanges {
        slog.InfoContext(ctx, "no infrastructure changes needed")
        return nil
    }

    // 5. Apply
    if err := p.executor.Apply(ctx, []string{"terraform.tfvars"}); err != nil {
        return fmt.Errorf("applying infrastructure: %w", err)
    }

    // 6. Wait for foundational software readiness
    if err := p.waitForFoundationalSoftware(ctx); err != nil {
        return fmt.Errorf("waiting for foundational software: %w", err)
    }

    slog.InfoContext(ctx, "deployment completed successfully")
    return nil
}

func (p *TofuProvider) Destroy(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "TofuProvider.Destroy")
    defer span.End()

    slog.InfoContext(ctx, "destroying infrastructure")

    if err := p.executor.Destroy(ctx, []string{"terraform.tfvars"}); err != nil {
        return fmt.Errorf("destroying infrastructure: %w", err)
    }

    slog.InfoContext(ctx, "infrastructure destroyed successfully")
    return nil
}

func (p *TofuProvider) generateBackendConfig(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "TofuProvider.generateBackendConfig")
    defer span.End()

    slog.InfoContext(ctx, "generating backend configuration")

    backendTmpl := `
terraform {
  backend "%s" {
%s
  }
}
`

    var backendConfig string
    switch p.config.Provider.Type {
    case "aws":
        backendConfig = fmt.Sprintf(`    bucket         = "%s"
    key            = "%s"
    region         = "%s"
    encrypt        = true
    dynamodb_table = "%s"`,
            p.config.StateBackend.AWS.Bucket,
            p.config.StateBackend.AWS.Key,
            p.config.StateBackend.AWS.Region,
            p.config.StateBackend.AWS.DynamoDBTable,
        )

    case "gcp":
        backendConfig = fmt.Sprintf(`    bucket = "%s"
    prefix = "%s"`,
            p.config.StateBackend.GCP.Bucket,
            p.config.StateBackend.GCP.Prefix,
        )

    case "azure":
        backendConfig = fmt.Sprintf(`    storage_account_name = "%s"
    container_name       = "%s"
    key                  = "%s"`,
            p.config.StateBackend.Azure.StorageAccount,
            p.config.StateBackend.Azure.Container,
            p.config.StateBackend.Azure.Key,
        )

    default:
        backendConfig = `    path = "terraform.tfstate"`
    }

    backendType := map[string]string{
        "aws":   "s3",
        "gcp":   "gcs",
        "azure": "azurerm",
        "local": "local",
    }[p.config.Provider.Type]

    content := fmt.Sprintf(backendTmpl, backendType, backendConfig)

    backendFile := filepath.Join(p.workingDir, "backend.tf")
    if err := os.WriteFile(backendFile, []byte(content), 0644); err != nil {
        return fmt.Errorf("writing backend.tf: %w", err)
    }

    return nil
}

func (p *TofuProvider) generateTfvars(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "TofuProvider.generateTfvars")
    defer span.End()

    slog.InfoContext(ctx, "generating terraform variables")

    // Convert config.yaml to Terraform variables
    tfvars := map[string]interface{}{
        "provider":                       p.config.Provider.Type,
        "cluster_name":                   p.config.Name,
        "region":                         p.config.Provider.Region,
        "kubernetes_version":             p.config.Kubernetes.Version,
        "domain":                         p.config.Domain,
        "letsencrypt_email":              p.config.TLS.LetsEncrypt.Email,
        "foundational_software_repo_url": p.config.FoundationalSoftware.ArgoCD.RepoURL,
        "argocd_version":                 p.config.FoundationalSoftware.ArgoCD.Version,
        "cert_manager_version":           p.config.FoundationalSoftware.CertManager.Version,
        "envoy_gateway_version":          p.config.FoundationalSoftware.EnvoyGateway.Version,
        "keycloak_version":               p.config.FoundationalSoftware.Keycloak.Version,
        "grafana_version":                p.config.FoundationalSoftware.Observability.Grafana.Version,
        "loki_version":                   p.config.FoundationalSoftware.Observability.Loki.Version,
        "mimir_version":                  p.config.FoundationalSoftware.Observability.Mimir.Version,
        "tempo_version":                  p.config.FoundationalSoftware.Observability.Tempo.Version,
        "otel_collector_version":         p.config.FoundationalSoftware.Observability.OpenTelemetry.Version,
    }

    // Add provider-specific variables
    switch p.config.Provider.Type {
    case "aws":
        tfvars["aws_vpc_cidr"] = p.config.Provider.AWS.VPC.CIDR
        tfvars["aws_availability_zones"] = p.config.Provider.AWS.VPC.AvailabilityZones

    case "gcp":
        tfvars["gcp_project_id"] = p.config.Provider.GCP.ProjectID

    case "azure":
        tfvars["azure_resource_group"] = p.config.Provider.Azure.ResourceGroup
        tfvars["azure_vnet_address_space"] = p.config.Provider.Azure.VNet.AddressSpace
    }

    // Add node pools
    nodePools := make([]map[string]interface{}, len(p.config.Kubernetes.NodePools))
    for i, np := range p.config.Kubernetes.NodePools {
        nodePools[i] = map[string]interface{}{
            "name":          np.Name,
            "instance_type": np.InstanceType,
            "min_size":      np.MinSize,
            "max_size":      np.MaxSize,
            "labels":        np.Labels,
            "taints":        np.Taints,
        }
    }
    tfvars["node_pools"] = nodePools

    // Write tfvars file
    tfvarsJSON, err := json.MarshalIndent(tfvars, "", "  ")
    if err != nil {
        return fmt.Errorf("marshaling tfvars: %w", err)
    }

    tfvarsFile := filepath.Join(p.workingDir, "terraform.tfvars.json")
    if err := os.WriteFile(tfvarsFile, tfvarsJSON, 0644); err != nil {
        return fmt.Errorf("writing terraform.tfvars.json: %w", err)
    }

    return nil
}

func (p *TofuProvider) waitForFoundationalSoftware(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "TofuProvider.waitForFoundationalSoftware")
    defer span.End()

    slog.InfoContext(ctx, "waiting for foundational software to be ready")

    // Get kubeconfig from Terraform outputs
    outputs, err := p.executor.Output(ctx)
    if err != nil {
        return fmt.Errorf("getting terraform outputs: %w", err)
    }

    kubeconfigOutput, ok := outputs["kubeconfig"]
    if !ok {
        return fmt.Errorf("kubeconfig not found in terraform outputs")
    }

    var kubeconfigContent string
    if err := json.Unmarshal(kubeconfigOutput.Value, &kubeconfigContent); err != nil {
        return fmt.Errorf("unmarshaling kubeconfig: %w", err)
    }

    // Use Kubernetes client-go to wait for ArgoCD Applications
    // (Implementation similar to Native SDK edition)

    slog.InfoContext(ctx, "foundational software is ready")
    return nil
}

func findTofuBinary() (string, error) {
    // Check for OpenTofu first
    if path, err := exec.LookPath("tofu"); err == nil {
        return path, nil
    }

    // Fall back to Terraform
    if path, err := exec.LookPath("terraform"); err == nil {
        return path, nil
    }

    return "", fmt.Errorf("neither 'tofu' nor 'terraform' binary found in PATH")
}
```

### 6.3 Working Directory Management

**Directory Structure During Deployment:**
```
/tmp/nic-<cluster-name>/
├── backend.tf              # Generated from config.yaml
├── terraform.tfvars.json   # Generated from config.yaml
├── main.tf                 # Symlink to terraform/main.tf
├── variables.tf            # Symlink to terraform/variables.tf
├── outputs.tf              # Symlink to terraform/outputs.tf
├── providers.tf            # Symlink to terraform/providers.tf
├── modules/                # Symlink to terraform/modules/
├── .terraform/             # Terraform working directory
└── terraform.tfstate       # State (if using local backend)
```

**Setup Code:**
```go
func (p *TofuProvider) setupWorkingDirectory(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "TofuProvider.setupWorkingDirectory")
    defer span.End()

    // Create working directory
    if err := os.MkdirAll(p.workingDir, 0755); err != nil {
        return fmt.Errorf("creating working directory: %w", err)
    }

    // Symlink Terraform files from repo to working directory
    repoRoot, err := findRepoRoot()
    if err != nil {
        return fmt.Errorf("finding repo root: %w", err)
    }

    terraformDir := filepath.Join(repoRoot, "terraform")

    filesToLink := []string{
        "main.tf",
        "variables.tf",
        "outputs.tf",
        "providers.tf",
        "modules",
    }

    for _, file := range filesToLink {
        src := filepath.Join(terraformDir, file)
        dst := filepath.Join(p.workingDir, file)

        if err := os.Symlink(src, dst); err != nil {
            return fmt.Errorf("symlinking %s: %w", file, err)
        }
    }

    return nil
}
```

---
