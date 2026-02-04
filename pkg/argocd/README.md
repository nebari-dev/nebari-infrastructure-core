# ArgoCD Manifest Writer

Generates ArgoCD Application manifests for Nebari's foundational software stack.

## Adding a New Application

1. Create a YAML file in `templates/`:
   ```
   pkg/argocd/templates/cert-manager.yaml
   ```

2. Done. The file is automatically picked up.

The filename (without `.yaml`) becomes the application name. See `_example.yaml` for a template.

## Usage

```go
import "github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"

// List available applications
apps, err := argocd.Applications()

// Write a single application to any io.Writer
var buf bytes.Buffer
err := argocd.WriteApplication(ctx, &buf, "cert-manager")

// Write all applications (orchestrator pattern)
err := argocd.WriteAll(ctx, func(appName string) (io.WriteCloser, error) {
    return os.Create(filepath.Join(dir, appName+".yaml"))
})
```

## Integration with GitOps Bootstrap

```go
gitClient.Init(ctx)

if !gitClient.IsBootstrapped(ctx) {
    err := argocd.WriteAll(ctx, func(appName string) (io.WriteCloser, error) {
        return os.Create(filepath.Join(gitClient.WorkDir(), appName+".yaml"))
    })
    if err != nil {
        return err
    }

    gitClient.WriteBootstrapMarker(ctx)
    gitClient.CommitAndPush(ctx, "Bootstrap foundational apps")
}
```

## File Naming

- `*.yaml` - Application manifests (automatically included)
- `_*.yaml` - Examples/documentation (ignored by `Applications()`)
