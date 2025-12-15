# Terraform-Exec Integration

## 8.1 Overview

NIC uses the `hashicorp/terraform-exec` library to orchestrate OpenTofu execution from Go. The Go CLI doesn't make direct cloud API calls; instead, it manages the OpenTofu lifecycle (init, plan, apply, destroy) and processes outputs.

## 8.2 Execution Flow

```
User → NIC CLI → terraform-exec → OpenTofu Binary → Terraform Provider → Cloud API → Infrastructure
```

1. User runs `nic deploy -f config.yaml`
2. Go CLI parses config and generates Terraform variables
3. terraform-exec invokes OpenTofu init, plan, apply
4. OpenTofu uses provider plugins to call cloud APIs
5. State file updated in remote backend
6. Go CLI retrieves outputs and waits for readiness

## 8.3 Wrapper Package Design

The `pkg/tofu` package wraps terraform-exec with OpenTelemetry instrumentation:

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
func (e *Executor) Show(ctx context.Context) (*tfjson.State, error) {
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

## 8.4 Deploy Command Integration

**cmd/nic/deploy.go:**

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"

    "github.com/spf13/cobra"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"

    "github.com/nebari-dev/nic/pkg/config"
    "github.com/nebari-dev/nic/pkg/tofu"
)

var tracer = otel.Tracer("github.com/nebari-dev/nic")

var deployCmd = &cobra.Command{
    Use:   "deploy",
    Short: "Deploy Nebari infrastructure",
    RunE:  runDeploy,
}

func runDeploy(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()
    ctx, span := tracer.Start(ctx, "deploy")
    defer span.End()

    configFile, _ := cmd.Flags().GetString("config")
    span.SetAttributes(attribute.String("config_file", configFile))

    // Step 1: Parse configuration
    cfg, err := config.ParseFile(configFile)
    if err != nil {
        span.RecordError(err)
        return fmt.Errorf("parsing config: %w", err)
    }

    span.SetAttributes(
        attribute.String("provider", cfg.Provider),
        attribute.String("project_name", cfg.ProjectName),
    )

    // Step 2: Convert config to Terraform variables
    varsFile, err := generateTerraformVars(ctx, cfg)
    if err != nil {
        span.RecordError(err)
        return fmt.Errorf("generating terraform vars: %w", err)
    }
    defer os.Remove(varsFile)

    // Step 3: Locate OpenTofu binary
    tofuPath, err := findOpenTofuBinary()
    if err != nil {
        span.RecordError(err)
        return fmt.Errorf("finding opentofu binary: %w", err)
    }

    span.SetAttributes(attribute.String("tofu_path", tofuPath))

    // Step 4: Create tofu executor
    workingDir := filepath.Join("terraform")
    executor, err := tofu.NewExecutor(workingDir, tofuPath)
    if err != nil {
        span.RecordError(err)
        return fmt.Errorf("creating tofu executor: %w", err)
    }

    // Step 5: Initialize Terraform
    if err := executor.Init(ctx); err != nil {
        span.RecordError(err)
        return fmt.Errorf("terraform init: %w", err)
    }

    // Step 6: Plan infrastructure changes
    hasChanges, err := executor.Plan(ctx, []string{varsFile})
    if err != nil {
        span.RecordError(err)
        return fmt.Errorf("terraform plan: %w", err)
    }

    if !hasChanges {
        fmt.Println("No infrastructure changes needed")
        return nil
    }

    // Step 7: Apply infrastructure changes
    if err := executor.Apply(ctx, []string{varsFile}); err != nil {
        span.RecordError(err)
        return fmt.Errorf("terraform apply: %w", err)
    }

    // Step 8: Retrieve outputs (kubeconfig, URLs, etc.)
    outputs, err := executor.Output(ctx)
    if err != nil {
        span.RecordError(err)
        return fmt.Errorf("terraform output: %w", err)
    }

    // Step 9: Wait for Kubernetes cluster readiness
    kubeconfig := outputs["kubeconfig"].Value.(string)
    if err := waitForClusterReady(ctx, kubeconfig); err != nil {
        span.RecordError(err)
        return fmt.Errorf("waiting for cluster: %w", err)
    }

    // Step 10: Wait for ArgoCD and foundational software
    if err := waitForFoundationalSoftware(ctx, kubeconfig); err != nil {
        span.RecordError(err)
        return fmt.Errorf("waiting for foundational software: %w", err)
    }

    fmt.Println("Nebari deployed successfully")
    fmt.Printf("   Domain: %s\n", cfg.Domain)
    fmt.Printf("   ArgoCD: https://argocd.%s\n", cfg.Domain)
    fmt.Printf("   Grafana: https://grafana.%s\n", cfg.Domain)
    fmt.Printf("   Keycloak: https://keycloak.%s\n", cfg.Domain)

    return nil
}

// generateTerraformVars converts NebariConfig to Terraform variables JSON
func generateTerraformVars(ctx context.Context, cfg *config.NebariConfig) (string, error) {
    ctx, span := tracer.Start(ctx, "generateTerraformVars")
    defer span.End()

    // Convert Go config struct to Terraform variables map
    vars := map[string]any{
        "provider":           cfg.Provider,
        "cluster_name":       cfg.ProjectName,
        "domain":             cfg.Domain,
        "region":             getRegion(cfg),
        "kubernetes_version": cfg.KubernetesVersion,
        "node_pools":         convertNodePools(cfg),
        "tags":               getTags(cfg),
    }

    // Add provider-specific variables
    switch cfg.Provider {
    case "aws":
        vars["aws_vpc_cidr"] = cfg.AmazonWebServices.VPC.CIDR
        vars["aws_availability_zones"] = cfg.AmazonWebServices.AvailabilityZones
    case "gcp":
        vars["gcp_project_id"] = cfg.GoogleCloudPlatform.ProjectID
    case "azure":
        vars["azure_resource_group"] = cfg.Azure.ResourceGroup
    }

    // Write to temporary file
    tmpFile, err := os.CreateTemp("", "nic-vars-*.json")
    if err != nil {
        return "", fmt.Errorf("creating temp file: %w", err)
    }
    defer tmpFile.Close()

    encoder := json.NewEncoder(tmpFile)
    encoder.SetIndent("", "  ")
    if err := encoder.Encode(vars); err != nil {
        return "", fmt.Errorf("encoding vars: %w", err)
    }

    return tmpFile.Name(), nil
}

// findOpenTofuBinary locates the tofu or terraform binary
func findOpenTofuBinary() (string, error) {
    // Try tofu first (OpenTofu)
    if path, err := exec.LookPath("tofu"); err == nil {
        return path, nil
    }

    // Fall back to terraform
    if path, err := exec.LookPath("terraform"); err == nil {
        return path, nil
    }

    return "", fmt.Errorf("neither tofu nor terraform binary found in PATH")
}
```

## 8.5 Error Handling

**Terraform errors are wrapped with context:**

```go
// Direct error from terraform-exec
err := executor.Apply(ctx, varFiles)
// Error: terraform apply: creating EKS Cluster: ValidationException: Invalid instance type

// The Go CLI can provide additional context
if err != nil {
    slog.ErrorContext(ctx, "deployment failed",
        "error", err,
        "provider", cfg.Provider,
        "cluster", cfg.ProjectName,
    )
    return fmt.Errorf("deploying %s: %w", cfg.ProjectName, err)
}
```

**Error types and handling:**

```go
// Check for specific Terraform error types
import "github.com/hashicorp/terraform-exec/tfexec"

if exitErr, ok := err.(*tfexec.ErrTerraformNotFound); ok {
    return fmt.Errorf("OpenTofu/Terraform not installed: %w", exitErr)
}

if lockErr, ok := err.(*tfexec.ErrStateLocked); ok {
    return fmt.Errorf("state locked by another process: %w", lockErr)
}
```

## 8.6 Working Directory Management

The Go CLI manages the Terraform working directory:

```go
// pkg/tofu/workspace.go
package tofu

import (
    "embed"
    "io/fs"
    "os"
    "path/filepath"
)

//go:embed modules/*
var embeddedModules embed.FS

type Workspace struct {
    baseDir string
}

// NewWorkspace creates or opens a workspace
func NewWorkspace(baseDir string) (*Workspace, error) {
    ws := &Workspace{baseDir: baseDir}

    // Create .nic/terraform directory
    tfDir := filepath.Join(baseDir, ".nic", "terraform")
    if err := os.MkdirAll(tfDir, 0755); err != nil {
        return nil, fmt.Errorf("creating workspace: %w", err)
    }

    // Extract embedded modules if not present
    if err := ws.extractModules(tfDir); err != nil {
        return nil, fmt.Errorf("extracting modules: %w", err)
    }

    return ws, nil
}

// extractModules copies embedded Terraform modules to workspace
func (ws *Workspace) extractModules(destDir string) error {
    return fs.WalkDir(embeddedModules, "modules", func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }

        destPath := filepath.Join(destDir, path)

        if d.IsDir() {
            return os.MkdirAll(destPath, 0755)
        }

        content, err := embeddedModules.ReadFile(path)
        if err != nil {
            return err
        }

        return os.WriteFile(destPath, content, 0644)
    })
}

// GenerateBackendConfig creates backend.tf from config
func (ws *Workspace) GenerateBackendConfig(cfg *config.NebariConfig) error {
    tmpl := `
terraform {
  backend "{{.Type}}" {
    {{- if eq .Type "s3" }}
    bucket         = "{{.Bucket}}"
    key            = "{{.Key}}"
    region         = "{{.Region}}"
    encrypt        = true
    dynamodb_table = "{{.DynamoDBTable}}"
    {{- end }}
    {{- if eq .Type "gcs" }}
    bucket = "{{.Bucket}}"
    prefix = "{{.Prefix}}"
    {{- end }}
    {{- if eq .Type "azurerm" }}
    storage_account_name = "{{.StorageAccount}}"
    container_name       = "{{.Container}}"
    key                  = "{{.Key}}"
    {{- end }}
  }
}
`
    // ... template execution
    return nil
}
```

## 8.7 Output Processing

**Retrieving and using Terraform outputs:**

```go
// pkg/tofu/outputs.go
package tofu

import (
    "encoding/json"
    "fmt"
)

type DeploymentOutputs struct {
    Kubeconfig      string `json:"kubeconfig"`
    ClusterEndpoint string `json:"cluster_endpoint"`
    ArgocdURL       string `json:"argocd_url"`
    GrafanaURL      string `json:"grafana_url"`
    KeycloakURL     string `json:"keycloak_url"`
}

func (e *Executor) GetDeploymentOutputs(ctx context.Context) (*DeploymentOutputs, error) {
    ctx, span := tracer.Start(ctx, "GetDeploymentOutputs")
    defer span.End()

    rawOutputs, err := e.Output(ctx)
    if err != nil {
        return nil, fmt.Errorf("getting outputs: %w", err)
    }

    outputs := &DeploymentOutputs{}

    if kc, ok := rawOutputs["kubeconfig"]; ok {
        outputs.Kubeconfig = string(kc.Value)
    }

    if ep, ok := rawOutputs["cluster_endpoint"]; ok {
        outputs.ClusterEndpoint = string(ep.Value)
    }

    if argocd, ok := rawOutputs["argocd_url"]; ok {
        outputs.ArgocdURL = string(argocd.Value)
    }

    if grafana, ok := rawOutputs["grafana_url"]; ok {
        outputs.GrafanaURL = string(grafana.Value)
    }

    if keycloak, ok := rawOutputs["keycloak_url"]; ok {
        outputs.KeycloakURL = string(keycloak.Value)
    }

    return outputs, nil
}
```

## 8.8 State Operations

**Exposing state commands via CLI:**

```go
// cmd/nic/state.go
var stateCmd = &cobra.Command{
    Use:   "state",
    Short: "Terraform state operations",
}

var stateListCmd = &cobra.Command{
    Use:   "list",
    Short: "List resources in state",
    RunE: func(cmd *cobra.Command, args []string) error {
        executor, err := getExecutor(cmd)
        if err != nil {
            return err
        }

        state, err := executor.Show(cmd.Context())
        if err != nil {
            return err
        }

        for _, resource := range state.Values.RootModule.Resources {
            fmt.Printf("%s.%s\n", resource.Type, resource.Name)
        }

        return nil
    },
}

var stateShowCmd = &cobra.Command{
    Use:   "show [address]",
    Short: "Show a resource in state",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        executor, err := getExecutor(cmd)
        if err != nil {
            return err
        }

        // Use terraform state show command
        output, err := executor.StateShow(cmd.Context(), args[0])
        if err != nil {
            return err
        }

        fmt.Println(output)
        return nil
    },
}
```

---

## Summary

The terraform-exec integration provides:

- **Programmatic control**: Go CLI orchestrates OpenTofu without shell scripts
- **OpenTelemetry instrumentation**: Full tracing of Terraform operations
- **Error handling**: Structured error types for better debugging
- **Output processing**: Type-safe access to Terraform outputs
- **State management**: CLI commands for state operations

See [State Management](../architecture/05-state-management.md) for backend configuration and [OpenTofu Module Architecture](06-opentofu-module-architecture.md) for module design.
