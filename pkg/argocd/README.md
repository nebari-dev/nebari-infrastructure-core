# ArgoCD Manifest Writer

Generates ArgoCD Application manifests for Nebari's foundational software stack.

## Adding a New Application

1. Create a YAML file in `templates/apps/`:
   ```
   pkg/argocd/templates/apps/cert-manager.yaml
   ```

2. Done. The file is automatically picked up.

The filename (without `.yaml`) becomes the application name.

### Adding a Helm-based application

A Helm-based app needs two files, not one, because Helm values are sourced from the gitops repo rather than inlined in the Application (see [ADR-0012](../../docs/adr/0012-helm-valuefiles-overlay-seam.md) and [docs/helm-value-overlays.md](../../docs/helm-value-overlays.md)):

1. `templates/apps/<name>.yaml` - the Application manifest. It must be multi-source:
   - The chart source comes first and carries `helm.valueFiles` using the `GitPath` idiom, plus `ignoreMissingValueFiles: true`:
     ```yaml
     helm:
       releaseName: <name>
       valueFiles:
         - $values/{{ if .GitPath }}{{ .GitPath }}/{{ end }}values/<name>/base.yaml
         - $values/{{ if .GitPath }}{{ .GitPath }}/{{ end }}values/<name>/overlays/*.yaml
       ignoreMissingValueFiles: true
     ```
   - A second source points at the gitops repo with `ref: values`. See `templates/apps/envoy-gateway.yaml` for the full worked example, including how `keycloak.yaml` and `nebari-landingpage.yaml` reuse or extend an existing second source instead of adding a third.

2. `templates/values/<name>/base.yaml` - the chart's default values, rendered from the same `TemplateData` as the Application.

Do not use inline `helm.values` or `helm.valuesObject` on any source. ArgoCD's Helm precedence is `parameters` > `valuesObject` > `values` > `valueFiles`, so an inline block outranks every `valueFiles` entry and silently defeats overlays. `TestHelmApps_SeamInvariants` (`pkg/argocd/writer_test.go`) fails the build if a Helm app template contains `values:`/`valuesObject:`, is missing `valueFiles:`, or is not enrolled in the `helmValueFilesApps` table in `writer_test.go` (see next section).

You must also:

- Add a row for `<name>` to the `helmValueFilesApps` table in `pkg/argocd/writer_test.go`, with a signature string that appears in the rendered `base.yaml` (a distinctive substring is enough; it just proves the right template rendered). `TestHelmApps_SeamInvariants` enforces that every Helm app template and every `templates/values/<name>` directory is enrolled here.
- If the app is gated (like `metallb` and `trust-manager`), add `values/<name>/base.yaml` as a **file** path to the corresponding gate predicate in `writer.go` (`isMetalLBPath`, `isTrustBundlePath`, or a new one), matching the file, never the `values/<name>` directory. Matching the directory routes it through `removeStaleTemplate`'s `os.RemoveAll` branch when the gate is off, which would delete any user overlays sitting next to `base.yaml`. See the warning comment above `removeStaleTemplate` in `writer.go`.

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
