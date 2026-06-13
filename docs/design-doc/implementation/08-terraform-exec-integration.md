# terraform-exec Integration

## 8.1 Scope

NIC uses HashiCorp's `terraform-exec` library to orchestrate OpenTofu execution **from the AWS provider**. Other cluster providers (Hetzner, local, existing) do not use terraform-exec. The wrapper for this integration lives in `pkg/tofu/`.

This document describes the wrapper, the Setup helper, and how AWS-provider code uses it. The AWS-side code that calls into `pkg/tofu` is in `pkg/provider/aws/tofu.go`.

## 8.2 Package Layout

```
pkg/tofu/
├── tofu.go               # TerraformExecutor type, Setup, Init/Plan/Apply/Destroy/Output, downloader
├── log.go                # JSON line mapper for status streaming
├── version.go            # Pinned OpenTofu version
├── context_default.go    # Non-Linux signal handling
└── context_linux.go      # Linux-specific signal handling (PR_SET_PDEATHSIG)
```

There is no `executor.go`, `workspace.go`, or `outputs.go`. The entire wrapper is in `tofu.go`.

## 8.3 The Wrapper Type

```go
// pkg/tofu/tofu.go
type TerraformExecutor struct {
    *tfexec.Terraform
    workingDir string
    appFs      afero.Fs
}
```

`TerraformExecutor` embeds `*tfexec.Terraform` so callers get the full upstream API for free. The wrapper adds:

- The temp working directory it created
- An `afero.Fs` for testable filesystem access
- A `Cleanup()` method that removes the working dir

The exported methods that NIC actually calls are wrapped to stream JSON output through the status channel attached to `ctx`:

```go
func (te *TerraformExecutor) Init(ctx context.Context, opts ...tfexec.InitOption) error {
    ctx = signalSafeContext(ctx)
    return te.streamThroughStatus(ctx, func(w io.Writer) error {
        return te.InitJSON(ctx, w, opts...)
    })
}
```

`Plan`, `Apply`, and `Destroy` follow the same pattern, calling `PlanJSON`, `ApplyJSON`, and `DestroyJSON` respectively. `Output` does not stream because its caller wants the parsed `map[string]tfexec.OutputMeta` directly.

`streamThroughStatus` creates a stdout writer that maps each JSON line to a `status.Update` (`jsonLineMapper`) and a stderr writer that maps each raw line to an error-level `Update`. Both writers are flushed after the operation completes to drain any partial trailing line.

### Why JSON streaming?

OpenTofu's `-json` mode emits one structured event per line, with `@level`, `@message`, plus structured fields per event type (apply progress, plan summary, diagnostics, etc.). Streaming those through the status channel lets the CLI render live progress without parsing OpenTofu's human-readable output. The full event payload is attached to each `status.Update` via `Update.Metadata[status.MetadataKeyPayload]` so downstream handlers can pick out any field they want.

### Logging policy

`pkg/tofu` does not call `slog`. That's intentional and required: per [`CLAUDE.md`](../../../CLAUDE.md), library code never logs. Translation into log records happens in `cmd/nic/status_handler.go`.

## 8.4 Setup

`Setup` is the entry point that providers actually call:

```go
func Setup(ctx context.Context, templates fs.FS, tfvars any) (*TerraformExecutor, error)
```

It does the following:

1. Allocates a fresh temp working directory via `afero.TempDir`.
2. Walks the `templates` filesystem (an `embed.FS` from the calling provider) and copies each file into the working dir.
3. Ensures `~/.cache/nic/tofu/` exists and uses it as the OpenTofu download cache.
4. Downloads the OpenTofu binary (version pinned in `pkg/tofu/version.go`) via the `tofudl` library, with `MirrorConfig` that caches both API responses and artifacts indefinitely. Writes the executable into the working dir to avoid version-mismatch races between concurrent NIC invocations.
5. Sets `TF_PLUGIN_CACHE_DIR` to `~/.cache/nic/tofu/plugins` so provider plugins are reused across runs.
6. Marshals `tfvars` to `terraform.tfvars.json` in the working dir.
7. Constructs `tfexec.NewTerraform(workingDir, execPath)` and returns the wrapped `TerraformExecutor`.

If any step fails, the temp dir and the (empty) cache directories are cleaned up. The caller is responsible for `defer executor.Cleanup()` once Setup succeeds.

There is **no** `findOpenTofuBinary()` in `PATH`. The binary is always the version NIC pinned and downloaded.

## 8.5 AWS Provider Usage

The AWS provider's `Deploy` and `Destroy` methods are the primary callers. The shape (simplified, with telemetry, dry-run/backend-override handling, and bucket-existence branching omitted - see `pkg/provider/aws/provider.go` for the authoritative version):

```go
// pkg/provider/aws/provider.go (illustrative)
func (p *Provider) Deploy(ctx context.Context, projectName string, cluster *config.ClusterConfig, opts provider.DeployOptions) error {
    awsCfg, err := decodeConfig(cluster)
    if err != nil { return err }

    if err := ensureStateBucket(ctx, s3Client, awsCfg.Region, bucketName); err != nil {
        return err
    }

    tfvars := buildTfvars(projectName, awsCfg)
    te, err := tofu.Setup(ctx, templatesFS, tfvars)
    if err != nil { return err }
    defer te.Cleanup()

    if err := te.Init(ctx,
        tfexec.BackendConfig(fmt.Sprintf("bucket=%s", bucketName)),
        tfexec.BackendConfig(fmt.Sprintf("key=%s", stateKey(projectName))),
        tfexec.BackendConfig(fmt.Sprintf("region=%s", awsCfg.Region)),
    ); err != nil { return err }

    if opts.DryRun {
        _, err := te.Plan(ctx)
        return err
    }
    return te.Apply(ctx)
}
```

Key points the previous version of this doc got wrong:

- The CLI does **not** call a function like `generateTerraformVars(cfg)` itself; each provider owns its own tfvars construction.
- There is no `cfg.Provider` or `cfg.ProviderConfig` field on `NebariConfig`. The provider name is `cfg.Cluster.ProviderName()`; the typed config comes from decoding `cfg.Cluster.ProviderConfig()` inside the provider package.
- There is no `findOpenTofuBinary()`; see Setup above.

## 8.6 Backend Override (Dry-Run)

For `--dry-run` runs against a fresh AWS account where the state bucket might not yet exist, `pkg/tofu` exposes:

```go
func (te *TerraformExecutor) WriteBackendOverride() error
```

This writes `backend_override.tf.json` into the working dir with a `terraform.backend.local` block, which OpenTofu uses to override the configured S3 backend for this single run. The AWS provider only triggers this in dry-run mode.

## 8.7 Signal Handling

Long-running tofu operations need to survive Ctrl-C in a controlled way. `signalSafeContext(ctx)` returns a derived context whose cancellation is propagated to the tofu child process via SIGTERM, then SIGKILL after a grace period. On Linux, `pkg/tofu/context_linux.go` also sets `PR_SET_PDEATHSIG` so a crashed NIC process doesn't orphan its tofu child.

There is a known cleanup gap during destroy ([#63](https://github.com/nebari-dev/nebari-infrastructure-core/issues/63)): Ctrl-C while `tofu destroy` is mid-flight can leave the S3 state lockfile in place.

## 8.8 OpenTelemetry Instrumentation Status

`TerraformExecutor`'s operation-granularity methods (`Init`, `Plan`, `Apply`, `Destroy`, `Output`) are **not yet** wrapped in their own spans. This is acknowledged as outstanding work in `CLAUDE.md`. When that lands, each method will look like:

```go
func (te *TerraformExecutor) Apply(ctx context.Context, opts ...tfexec.ApplyOption) error {
    tracer := otel.Tracer("nebari-infrastructure-core")
    ctx, span := tracer.Start(ctx, "tofu.Apply")
    defer span.End()
    // ... existing body ...
}
```

The byte/line helpers (`streamThroughStatus`, `jsonLineMapper`, `mapStatusLevel`) and the `pkg/status` writers themselves are intentionally exempt: spans at that granularity would dwarf the operations they describe.

## 8.9 Not Implemented

There is no `nic state` subcommand, no `nic plan` subcommand, no `nic unlock`, no `nic init-backend`, and no `nic status` subcommand. Several of those have open issues ([#64](https://github.com/nebari-dev/nebari-infrastructure-core/issues/64) for unlock). Users who need direct state manipulation today must invoke the tofu binary themselves; the bundled cache makes the same version available at `~/.cache/nic/tofu/`.
