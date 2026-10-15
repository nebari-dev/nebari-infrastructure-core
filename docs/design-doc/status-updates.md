# Status Update Mechanism

The status package (`pkg/status/status.go`) provides a **context-based, non-blocking channel system** for sending progress updates from library code (`pkg/`) to the application layer (`internal/cli`) without introducing logging dependencies in the library code.

## Architecture

```mermaid
flowchart BT
    L["Library code (pkg/nic, pkg/providers/*, pkg/argocd, ...)<br/>status.Send, status.Info/Progress/Success/Warning/Error, status.Phase"]
    A["Application layer (internal/cli)<br/>status.StartHandler: creates the channel, attaches it to the context,<br/>and runs one goroutine that hands each update to the handler"]
    L -->|"updates via the channel"| A
```

The handler the CLI installs depends on `--log-format`: the `internal/output` renderer for `pretty`, `plain` and `github`, or `pkg/nic`'s `SlogHandler` for `json`. For `deploy` and `destroy` it also feeds a second `SlogHandler` that writes the run's JSON log file (see "Output" below).

## Status Levels

- **`LevelInfo`** - Informational messages
- **`LevelProgress`** - Operations in progress (creating, discovering, waiting)
- **`LevelSuccess`** - Successful completion
- **`LevelWarning`** - Non-fatal issues
- **`LevelError`** - Error conditions

## Setup (Application Layer)

`deploy` and `destroy` start the handler through `startCommandOutput` in `internal/cli/output.go`, which picks the renderer for the resolved `--log-format` and opens the log file:

```go
h := deployHeader(commandHeader(ctx, client, cfg, configFile), cfg.ProjectName, deployDryRun)
ctx, out := startCommandOutput(ctx, "deploy", cfg, h, deployLogFile, deployDryRun)
result, err := client.Deploy(ctx, cfg, opts)
out.finish(err, link) // flush, print the result line, close the log file
```

Commands that only need structured logs keep the simpler form, where `SlogHandler` in `pkg/nic/status.go` converts status updates to `slog` calls at the appropriate log level:

```go
ctx, cleanup := nic.StartSlogHandler(ctx, slog.Default())
defer cleanup()
```

## Phases

`pkg/nic` marks each stage of a deploy or destroy with `status.Phase(ctx, name)` (names in `pkg/nic/phases.go`). Every update until the next marker belongs to that phase: the renderer draws one panel per phase, and `SlogHandler` adds a `phase` attribute to each JSON record. Library code below `pkg/nic` doesn't announce phases; its updates land in whichever phase is open.

## Output

| `--log-format` | Used when | What it prints |
|---|---|---|
| `auto` (default) | always | `pretty` on a terminal, `plain` on a terminal with `NO_COLOR` or `TERM=dumb` and on Windows (legacy consoles don't interpret the escape sequences pretty needs; `--log-format pretty` still works in Windows Terminal), `json` otherwise |
| `pretty` | terminals | A panel per phase, drawn as soon as the phase starts and filled in as it runs, with a spinner and coloured results (24-bit colour when `COLORTERM` is `truecolor` or `24bit`, the 16 standard colours otherwise) |
| `plain` | log files, dumb terminals | One line per step, starting with its time of day (`13:00:38`), no colour, nothing redrawn |
| `json` | pipes, CI, scripts | The `slog` JSON records. Compared with earlier releases, each phase adds a marker record (`resource: phase`), every record after it carries a `phase` attribute, and `deploy`/`destroy` log the log file's path |
| `github` | GitHub Actions | `plain`, with each phase in a log group and warnings and errors as annotations (workflow commands carry no time: they must start the line) |

The header names the target (the cluster provider, then its region, location or kubeconfig context, from the `cluster.SummaryKey*` entries of the provider's `Summary`), the domain, the config file and NIC's version, and the log file. A run ends with a result line: on success it carries the platform's URL, with warnings it says so (the exit code is unchanged: that is `pkg/nic`'s contract), and on failure it names the phase that failed and lists where to look next (a command to inspect that phase, the log file, and the command to run again).

Anything else written to stderr while a panel is live goes through the renderer, which prints it above the panel: the CLI's own log records (the interrupt notice) and Kubernetes API warnings, which client-go would otherwise print through klog. Subprocesses never write to the terminal; their output is tool output on the status stream (see AGENTS.md).

After a deploy, every format prints the guidance as numbered **Next steps** on stdout (as plain text in all but `pretty`): the DNS records or `/etc/hosts` line to create when no DNS provider manages them, the services to open, and how to sign in. These replace the framed banners earlier releases printed in every format.

A deploy on a terminal (`pretty`): a local kind deploy as it runs, then an AWS deploy that finished and one that failed. All replay the status stream of a real run, with names anonymised, through `internal/output`; the animation keeps the real durations but shortens the two long waits:

![nic deploy on a local kind cluster, panels filling in as each phase runs](../assets/cli-output/deploy.gif)

![nic deploy after it finishes, with the next steps](../assets/cli-output/deploy-done.svg)

![nic deploy failing in the Argo CD phase, with where to look next](../assets/cli-output/deploy-failed.svg)

`--verbose` (`-v`) also shows informational updates and tool output (OpenTofu events) under their phase. Whatever the format, `deploy` and `destroy` write every update as JSON to `~/.nic/logs/<project>/<UTC time>-<command>.jsonl` (`--log-file` to choose the path, `--no-log-file` to skip it). The file is made readable by its owner only (0600), even when `--log-file` names an existing one; directories NIC creates for it are 0700, while an existing directory keeps its mode.

## Usage in Library Code

### Simple message

```go
status.Info(ctx, "EKS cluster not found")
status.Progress(ctx, "Creating EKS cluster")
status.Success(ctx, "Node group deleted")
status.Warning(ctx, "GetKubeconfig not yet implemented")
status.Error(ctx, "Failed to create node group")
```

### Formatted message

```go
status.Infof(ctx, "Checking node group '%s' for updates", nodeGroupName)
status.Progressf(ctx, "Creating node group '%s' (%d/%d)", name, i, total)
```

### Full fluent API with resource/action/metadata

```go
status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating VPC").
    WithResource("vpc").
    WithAction("creating").
    WithMetadata("cidr", "10.0.0.0/16"))
```

## Real Examples from Codebase

### From `pkg/providers/cluster/aws/eks_delete.go:23-76`

```go
status.Send(ctx, status.NewUpdate(status.LevelProgress, "Checking EKS cluster"))
// ... check if cluster exists ...
status.Send(ctx, status.NewUpdate(status.LevelInfo, "EKS cluster not found"))
// or
status.Send(ctx, status.NewUpdate(status.LevelProgress, "Deleting EKS cluster"))
// ... wait for deletion ...
status.Send(ctx, status.NewUpdate(status.LevelSuccess, "EKS cluster deleted"))
```

### From `pkg/providers/cluster/aws/nodegroups_reconcile.go:67-117`

```go
status.Send(ctx, status.NewUpdate(status.LevelProgress,
    fmt.Sprintf("Creating node group '%s' (%d/%d)", nodeGroupName, i, len(awsCfg.NodeGroups))))

// On error:
status.Send(ctx, status.NewUpdate(status.LevelError,
    fmt.Sprintf("Failed to create node group '%s': %v", nodeGroupName, err)))

// On success:
status.Send(ctx, status.NewUpdate(status.LevelSuccess,
    fmt.Sprintf("Node group '%s' created and active", nodeGroupName)))
```

### From `pkg/providers/cluster/aws/vpc_reconcile.go:101-291`

```go
status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing internet gateway"))
// ... create IGW ...
status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Internet gateway created"))
// or if already exists:
status.Send(ctx, status.NewUpdate(status.LevelInfo, "Internet gateway already exists"))
```

## Key Properties

1. **Non-blocking** - If the channel is full, messages are dropped (see `Send` in `pkg/status/status.go`), so handlers must stay fast
2. **Context-based** - Channel is stored in context, so no global state
3. **Safe to call without handler** - If no channel in context, `Send()` is a no-op
4. **Graceful shutdown** - `cleanup()` function closes channel and waits for handler to drain

## When to Use Each Level

On a terminal (`pretty` and `plain`), `Info` and `Progress` updates only show as the running panel's activity line (`pretty`), or not at all (`plain`): they are listed under their phase with `--verbose`, and in dry runs, whose `Would ...` lines are `Info`. Tool output (updates carrying `MetadataKeyDetail`) at `Info` or `Success` is listed only with `--verbose`; its warnings and errors always show. The `github` format lists everything, since its phases are collapsed groups. Everything reaches the JSON output and the log file. So a message a user must see on every run needs `Success`, `Warning` or `Error`.

| Level | Use Case | Example |
|-------|----------|---------|
| `Info` | State discovery, no changes | "EKS cluster not found", "Internet gateway already exists" |
| `Progress` | Long-running operation started | "Creating EKS cluster", "Deleting node groups" |
| `Success` | Operation completed successfully | "VPC created", "Node group deleted" |
| `Warning` | Non-fatal issues, stubs | "GetKubeconfig not yet implemented" |
| `Error` | Operation failed | "Failed to create node group: ..." |
