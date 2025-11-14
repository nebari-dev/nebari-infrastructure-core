# Terraform-Exec Integration

**Note**: This document describes how Go code orchestrates OpenTofu execution in the alternative OpenTofu-based design. See [../README.md](../README.md) for comparison with the native SDK design.

## Overview

In the OpenTofu alternative design, the Go CLI doesn't make direct cloud API calls. Instead, it orchestrates OpenTofu execution using the `hashicorp/terraform-exec` library. This library provides a programmatic interface to run OpenTofu commands (init, plan, apply, destroy) from Go code.

## Wrapper Package Design

The `pkg/tofu` package wraps terraform-exec with OpenTelemetry instrumentation and structured logging:

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

## Deploy Command Integration

How the deploy command uses the tofu executor:

**cmd/nic/deploy.go:**

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
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
        fmt.Println("✅ No infrastructure changes needed")
        return nil
    }

    // Step 7: Apply infrastructure changes
    if err := executor.Apply(ctx, []string{varsFile})if err != nil {
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

    fmt.Println("✅ Nebari deployed successfully")
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
        "provider":      cfg.Provider,
        "cluster_name":  cfg.ProjectName,
        "domain":        cfg.Domain,
        "region":        getRegion(cfg),
        "node_pools":    convertNodePools(cfg),
        "tags":          getTags(cfg),
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
    // Try tofu first
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

## Key Differences from Native SDK Design

### Execution Flow

**Native SDK Design**:
```
User → NIC CLI → Cloud SDK → Cloud API → Infrastructure
```

**OpenTofu Design**:
```
User → NIC CLI → terraform-exec → OpenTofu Binary → Terraform Provider → Cloud API → Infrastructure
```

### Error Handling

**Native SDK**:
```go
// Direct error from AWS SDK
err := eksClient.CreateCluster(ctx, &eks.CreateClusterInput{...})
// Error: ValidationException: Invalid instance type
```

**OpenTofu**:
```go
// Terraform error wrapping cloud API error
err := executor.Apply(ctx, varFiles)
// Error: terraform apply: creating EKS Cluster: ValidationException: Invalid instance type
```

### State Management

**Native SDK**: Stateless - queries cloud APIs for actual state

**OpenTofu**: Terraform state file tracks expected state
- Requires backend configuration (S3, GCS, Azure Blob)
- State locking to prevent concurrent modifications
- State drift can occur if resources modified outside Terraform

## Benefits vs. Trade-offs

### Benefits

- ✅ Leverage existing Terraform modules (no need to write cloud SDK code)
- ✅ Terraform ecosystem tools work (terraform-docs, tfsec, etc.)
- ✅ Standard state format understood by teams
- ✅ Less Go code to write and maintain

### Trade-offs

- ⚠️ External dependency (requires OpenTofu/Terraform binary installed)
- ⚠️ Additional execution layer (slower than direct SDK calls)
- ⚠️ State management complexity (state files, locking, drift)
- ⚠️ Debugging requires understanding Go → Terraform → Cloud API flow
- ⚠️ Error messages less direct than native SDK errors

## Working Directory Management

OpenTofu requires a working directory with Terraform modules:

```
.nic/
├── terraform/          # Working directory
│   ├── .terraform/    # Terraform plugins and modules
│   ├── terraform.tfstate  # State file (if using local backend)
│   ├── vars.json      # Generated from config.yaml
│   └── backend.tf     # Generated backend configuration
```

The Go code manages this working directory lifecycle:
1. Create working directory if not exists
2. Copy Terraform modules from embedded FS or git clone
3. Generate `vars.json` from `config.yaml`
4. Generate `backend.tf` from config
5. Run `tofu init`, `tofu plan`, `tofu apply`
6. Cleanup temporary files

## Summary

The terraform-exec integration provides programmatic control over OpenTofu while delegating infrastructure provisioning to proven Terraform modules. This trades some performance and adds state management complexity in exchange for module reuse and faster development.

See [../README.md](../README.md) for full comparison and [07-state-management.md](07-state-management.md) for state handling details.

---
