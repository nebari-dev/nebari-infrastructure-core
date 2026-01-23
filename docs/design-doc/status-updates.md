# Status Update Mechanism

The status package (`pkg/status/status.go`) provides a **context-based, non-blocking channel system** for sending progress updates from library code (`pkg/`) to the application layer (`cmd/nic/`) without introducing logging dependencies in the library code.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Application Layer (cmd/nic/)                                   │
│                                                                 │
│  1. StartHandler() → creates channel, attaches to context       │
│  2. Handler function receives updates and logs via slog         │
└─────────────────────────────────────────────────────────────────┘
                              ↑
                              │ Updates via channel
                              │
┌─────────────────────────────────────────────────────────────────┐
│  Library Code (pkg/provider/*, pkg/dnsprovider/*)               │
│                                                                 │
│  status.Send(ctx, update) → sends to channel in context         │
│  status.Info(ctx, "message")                                    │
│  status.Progress(ctx, "message")                                │
│  status.Success(ctx, "message")                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Status Levels

- **`LevelInfo`** - Informational messages
- **`LevelProgress`** - Operations in progress (creating, discovering, waiting)
- **`LevelSuccess`** - Successful completion
- **`LevelWarning`** - Non-fatal issues
- **`LevelError`** - Error conditions

## Setup (Application Layer)

In `cmd/nic/deploy.go:63-64`:

```go
// Setup status handler for progress updates
ctx, cleanupStatus := status.StartHandler(ctx, statusLogHandler())
defer cleanupStatus()
```

The `statusLogHandler()` in `cmd/nic/status_handler.go:12-48` converts status updates to `slog` calls at the appropriate log level.

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

### From `pkg/provider/aws/eks_delete.go:23-76`

```go
status.Send(ctx, status.NewUpdate(status.LevelProgress, "Checking EKS cluster"))
// ... check if cluster exists ...
status.Send(ctx, status.NewUpdate(status.LevelInfo, "EKS cluster not found"))
// or
status.Send(ctx, status.NewUpdate(status.LevelProgress, "Deleting EKS cluster"))
// ... wait for deletion ...
status.Send(ctx, status.NewUpdate(status.LevelSuccess, "EKS cluster deleted"))
```

### From `pkg/provider/aws/nodegroups_reconcile.go:67-117`

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

### From `pkg/provider/aws/vpc_reconcile.go:101-291`

```go
status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing internet gateway"))
// ... create IGW ...
status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Internet gateway created"))
// or if already exists:
status.Send(ctx, status.NewUpdate(status.LevelInfo, "Internet gateway already exists"))
```

## Key Properties

1. **Non-blocking** - If the channel is full, messages are dropped (line 103-110)
2. **Context-based** - Channel is stored in context, so no global state
3. **Safe to call without handler** - If no channel in context, `Send()` is a no-op
4. **Graceful shutdown** - `cleanup()` function closes channel and waits for handler to drain

## When to Use Each Level

| Level | Use Case | Example |
|-------|----------|---------|
| `Info` | State discovery, no changes | "EKS cluster not found", "Internet gateway already exists" |
| `Progress` | Long-running operation started | "Creating EKS cluster", "Deleting node groups" |
| `Success` | Operation completed successfully | "VPC created", "Node group deleted" |
| `Warning` | Non-fatal issues, stubs | "GetKubeconfig not yet implemented" |
| `Error` | Operation failed | "Failed to create node group: ..." |
