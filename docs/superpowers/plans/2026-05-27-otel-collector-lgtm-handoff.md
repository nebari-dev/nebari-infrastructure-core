# OTel Collector ConfigMap Handoff to LGTM Pack — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a user installs the `nebari-lgtm-pack` Helm chart on a NIC-deployed cluster, the OpenTelemetry Collector is automatically rewired to ship logs/traces/metrics to Loki/Tempo/Mimir, and subsequent `nic deploy` runs do not revert the wiring.

**Architecture:** NIC's OTel collector ArgoCD `Application` adds `ignoreDifferences` on the rendered ConfigMap's `data.relay` field plus `RespectIgnoreDifferences=true` sync option. The LGTM pack chart ships a Helm `post-install,post-upgrade` hook Job (with least-privilege RBAC) that reads the current ConfigMap, deep-merges LGTM exporters+pipelines via `yq`, writes the merged config back, and rolls the collector DaemonSet.

**Tech Stack:** Go (NIC test), Helm v3 templating, kubectl, yq (mikefarah), jq, ArgoCD sync options.

**Spec:** [`/Users/tylerman/gh/nebari-infrastructure-core/docs/superpowers/specs/2026-05-27-otel-collector-lgtm-handoff-design.md`](../specs/2026-05-27-otel-collector-lgtm-handoff-design.md)

**Repos touched:**
- `/Users/tylerman/gh/nebari-infrastructure-core` (branch: `otel-lgtm-configmap-handoff`)
- `/Users/tylerman/gh/nebari-lgtm-pack` (new branch: `otel-collector-auto-wire`)

**PR strategy:** Two PRs (one per repo). NIC PR is a no-op for any cluster without a software pack claiming `data.relay`, so it can land first independently.

---

## File Structure

### NIC repo (`/Users/tylerman/gh/nebari-infrastructure-core`)
- **Modify** `pkg/argocd/templates/apps/opentelemetry-collector.yaml` — add `ignoreDifferences` and `RespectIgnoreDifferences=true`.
- **Modify** `pkg/argocd/writer_test.go` — new test `TestWriteApplication_OtelCollector_IgnoreDifferences` asserting the rendered YAML contains the right fields.

### LGTM pack repo (`/Users/tylerman/gh/nebari-lgtm-pack`)
- **Create** `templates/otel-collector-config-patch.yaml` — SA + Role + RoleBinding + override ConfigMap + Job, all gated on `.Values.otelCollectorOverrides.enabled`.
- **Modify** `values.yaml` — append `otelCollectorOverrides` block (toggle, names, image).
- **Modify** `examples/opentelemetry-collector-overrides.yaml` — reframe header comment to "reference of what the chart now auto-applies."
- **Modify** `README.md` — new section "OTel collector wiring."
- **Modify** `.github/workflows/test.yaml` — add a steps block that pre-creates a stub OTel collector ConfigMap + DaemonSet, installs the chart with `otelCollectorOverrides.enabled=true`, asserts the ConfigMap gets merged.

---

## Part A — NIC changes

### Task A1: Failing unit test for ignoreDifferences

**Working directory:** `/Users/tylerman/gh/nebari-infrastructure-core`
**Branch:** `otel-lgtm-configmap-handoff` (already created, currently has the spec commits)

**Files:**
- Modify: `pkg/argocd/writer_test.go` (append new test function at the end)

- [ ] **Step 1: Append the test function**

Open `pkg/argocd/writer_test.go` and append (preserving existing content):

```go
func TestWriteApplication_OtelCollector_IgnoreDifferences(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	if err := WriteApplication(ctx, &buf, "opentelemetry-collector"); err != nil {
		t.Fatalf("WriteApplication(opentelemetry-collector) error: %v", err)
	}

	content := buf.String()

	// ignoreDifferences must be set so LGTM (or any pack) can claim data.relay
	// without ArgoCD reverting it on the next sync.
	requiredFragments := []string{
		"ignoreDifferences:",
		"kind: ConfigMap",
		"name: opentelemetry-collector-opentelemetry-collector-agent",
		"namespace: monitoring",
		"jsonPointers:",
		"- /data/relay",
		"RespectIgnoreDifferences=true",
	}

	for _, frag := range requiredFragments {
		if !strings.Contains(content, frag) {
			t.Errorf("rendered opentelemetry-collector.yaml is missing fragment %q\n--- rendered:\n%s", frag, content)
		}
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
cd /Users/tylerman/gh/nebari-infrastructure-core
go test -run TestWriteApplication_OtelCollector_IgnoreDifferences ./pkg/argocd/ -v
```

Expected output: `FAIL` with errors like `rendered opentelemetry-collector.yaml is missing fragment "ignoreDifferences:"`.

- [ ] **Step 3: Commit the failing test**

```bash
git add pkg/argocd/writer_test.go
git commit -m "test(argocd): assert OTel collector Application has ignoreDifferences for data.relay"
```

---

### Task A2: Add ignoreDifferences + RespectIgnoreDifferences to the OTel collector template

**Files:**
- Modify: `pkg/argocd/templates/apps/opentelemetry-collector.yaml`

- [ ] **Step 1: Apply the edit**

Open `pkg/argocd/templates/apps/opentelemetry-collector.yaml`. Two specific changes:

**Change 1 — insert `ignoreDifferences` block** immediately after `spec:` and before `source:` (line 14 area). The result should look like:

```yaml
spec:
  project: foundational

  # Allow software packs (e.g. nebari-lgtm-pack) to overwrite the collector
  # config without ArgoCD reverting it. The data.relay field holds the entire
  # collector config; everything else (labels, annotations, existence) still
  # reconciles normally. Requires RespectIgnoreDifferences=true in syncOptions
  # below to be effective during sync (not just drift detection).
  ignoreDifferences:
    - group: ""
      kind: ConfigMap
      name: opentelemetry-collector-opentelemetry-collector-agent
      namespace: monitoring
      jsonPointers:
        - /data/relay

  source:
    chart: opentelemetry-collector
    # ... unchanged
```

**Change 2 — add `RespectIgnoreDifferences=true`** to the existing `syncOptions` list. The result should look like:

```yaml
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
      allowEmpty: false
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
      - RespectIgnoreDifferences=true
    retry:
      limit: 5
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 3m
```

- [ ] **Step 2: Run the test to confirm it passes**

```bash
cd /Users/tylerman/gh/nebari-infrastructure-core
go test -run TestWriteApplication_OtelCollector_IgnoreDifferences ./pkg/argocd/ -v
```

Expected output: `PASS`.

- [ ] **Step 3: Run the full argocd package tests to confirm no regressions**

```bash
go test ./pkg/argocd/ -v
```

Expected: all tests pass.

- [ ] **Step 4: Run golangci-lint per the CLAUDE.md rule**

```bash
golangci-lint run ./pkg/argocd/...
```

Expected: no warnings.

- [ ] **Step 5: Commit**

```bash
git add pkg/argocd/templates/apps/opentelemetry-collector.yaml
git commit -m "feat(argocd): allow software packs to claim OTel collector data.relay

Add ignoreDifferences on the rendered ConfigMap data.relay field and
RespectIgnoreDifferences=true sync option so a software pack (e.g.
nebari-lgtm-pack) can overwrite the collector config without ArgoCD
reverting it on the next sync.

Refs: nebari-dev/nebari-lgtm-pack#8"
```

---

### Task A3: Open NIC PR

- [ ] **Step 1: Push the branch**

```bash
cd /Users/tylerman/gh/nebari-infrastructure-core
git push -u origin otel-lgtm-configmap-handoff
```

- [ ] **Step 2: Open PR**

```bash
gh pr create --title "feat(argocd): allow software packs to claim OTel collector data.relay" --body "$(cat <<'EOF'
## Summary

- Adds `ignoreDifferences` on the OTel collector ConfigMap's `data.relay` field in NIC's ArgoCD Application template.
- Adds `RespectIgnoreDifferences=true` sync option so the rule applies during sync, not just drift detection.
- New unit test in `pkg/argocd/writer_test.go` locks in both fields.

This is a generic mechanism: any software pack (starting with nebari-lgtm-pack) can now overwrite the collector config without ArgoCD reverting it. Defaults are unchanged — a fresh cluster with no software pack still ships the debug-exporter config.

Spec: `docs/superpowers/specs/2026-05-27-otel-collector-lgtm-handoff-design.md`

## Test plan

- [x] `go test ./pkg/argocd/...` passes
- [x] `golangci-lint run ./pkg/argocd/...` clean
- [ ] Reviewer confirms the chart's ConfigMap name (`opentelemetry-collector-opentelemetry-collector-agent`) still matches in the targeted chart version (0.143.0)

Refs: https://github.com/nebari-dev/nebari-lgtm-pack/issues/8
EOF
)"
```

---

## Part B — LGTM pack changes

### Task B0: Create branch in lgtm-pack repo

**Working directory:** `/Users/tylerman/gh/nebari-lgtm-pack`

- [ ] **Step 1: Sync main and create branch**

```bash
cd /Users/tylerman/gh/nebari-lgtm-pack
git fetch origin main
git checkout main
git pull origin main
git checkout -b otel-collector-auto-wire
```

---

### Task B1: Add `otelCollectorOverrides` block to `values.yaml`

**Files:**
- Modify: `values.yaml` (append a new top-level block at the end of the file)

- [ ] **Step 1: Append the values block**

Append to `values.yaml`:

```yaml

# =============================================================================
# OpenTelemetry Collector overrides
# =============================================================================
# When enabled (default), a post-install/post-upgrade Helm hook patches the
# OTel collector ConfigMap deployed by nebari-infrastructure-core (NIC) so that
# logs/traces/metrics flow to this chart's Loki/Tempo/Mimir backends. The hook
# reads the current ConfigMap, deep-merges the override config, and rolls the
# collector DaemonSet. NIC's ArgoCD Application is configured with
# RespectIgnoreDifferences=true so the override survives subsequent syncs.
#
# Set enabled=false in environments where NIC is not deploying the collector
# (e.g. standalone LGTM installs against a user-managed collector).
otelCollectorOverrides:
  enabled: true
  # Namespace, ConfigMap, and DaemonSet names targeted by the hook. Defaults
  # match what NIC's OTel collector ArgoCD Application produces with chart
  # release name "opentelemetry-collector" in daemonset mode.
  namespace: monitoring
  configMapName: opentelemetry-collector-opentelemetry-collector-agent
  daemonSetName: opentelemetry-collector-opentelemetry-collector-agent
  # Image bundling kubectl + yq + jq for the patch Job.
  image: alpine/k8s:1.30.4
  # Optional pull policy override.
  imagePullPolicy: IfNotPresent
```

- [ ] **Step 2: Verify with helm template**

```bash
cd /Users/tylerman/gh/nebari-lgtm-pack
helm dependency update >/dev/null 2>&1
helm template test . --set nebariapp.enabled=false > /dev/null
```

Expected: no errors (the new block is referenced by no templates yet, just sanity-checking YAML validity).

- [ ] **Step 3: Commit**

```bash
git add values.yaml
git commit -m "feat: add otelCollectorOverrides values block

Toggle, target names, and image for the upcoming post-install hook that
rewires NIC's OTel collector to ship to this chart's Loki/Tempo/Mimir."
```

---

### Task B2: Create the patch template skeleton (SA + RBAC, no Job yet)

**Files:**
- Create: `templates/otel-collector-config-patch.yaml`

- [ ] **Step 1: Write the SA + RBAC objects**

Create `templates/otel-collector-config-patch.yaml`:

```yaml
{{- if .Values.otelCollectorOverrides.enabled }}
# Post-install/upgrade hook that overwrites NIC's OTel collector ConfigMap with
# this chart's exporter wiring (Loki/Tempo/Mimir) and rolls the DaemonSet so
# the collector picks up the new config. NIC's ArgoCD Application has
# RespectIgnoreDifferences=true on data.relay, so the override persists.
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "nebari-lgtm-pack.fullname" . }}-otel-patcher
  namespace: {{ .Values.otelCollectorOverrides.namespace }}
  labels:
    {{- include "nebari-lgtm-pack.labels" . | nindent 4 }}
  annotations:
    helm.sh/hook: post-install,post-upgrade
    helm.sh/hook-weight: "0"
    helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ include "nebari-lgtm-pack.fullname" . }}-otel-patcher
  namespace: {{ .Values.otelCollectorOverrides.namespace }}
  labels:
    {{- include "nebari-lgtm-pack.labels" . | nindent 4 }}
  annotations:
    helm.sh/hook: post-install,post-upgrade
    helm.sh/hook-weight: "0"
    helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded
rules:
  - apiGroups: [""]
    resources: ["configmaps"]
    resourceNames: ["{{ .Values.otelCollectorOverrides.configMapName }}"]
    verbs: ["get", "patch"]
  - apiGroups: ["apps"]
    resources: ["daemonsets"]
    resourceNames: ["{{ .Values.otelCollectorOverrides.daemonSetName }}"]
    verbs: ["get", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ include "nebari-lgtm-pack.fullname" . }}-otel-patcher
  namespace: {{ .Values.otelCollectorOverrides.namespace }}
  labels:
    {{- include "nebari-lgtm-pack.labels" . | nindent 4 }}
  annotations:
    helm.sh/hook: post-install,post-upgrade
    helm.sh/hook-weight: "0"
    helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ include "nebari-lgtm-pack.fullname" . }}-otel-patcher
subjects:
  - kind: ServiceAccount
    name: {{ include "nebari-lgtm-pack.fullname" . }}-otel-patcher
    namespace: {{ .Values.otelCollectorOverrides.namespace }}
{{- end }}
```

- [ ] **Step 2: Verify it renders**

```bash
helm template test . --set nebariapp.enabled=false 2>&1 | yq 'select(.kind == "ServiceAccount" and .metadata.name == "test-nebari-lgtm-pack-otel-patcher")' -
```

Expected: a `ServiceAccount` block with the name `test-nebari-lgtm-pack-otel-patcher`.

- [ ] **Step 3: Verify gating works**

```bash
helm template test . --set nebariapp.enabled=false --set otelCollectorOverrides.enabled=false 2>&1 | yq 'select(.metadata.name == "test-nebari-lgtm-pack-otel-patcher")' -
```

Expected: empty output (template is gated off).

- [ ] **Step 4: Commit**

```bash
git add templates/otel-collector-config-patch.yaml
git commit -m "feat: add ServiceAccount and RBAC for OTel collector patch hook"
```

---

### Task B3: Add override ConfigMap to the patch template

**Files:**
- Modify: `templates/otel-collector-config-patch.yaml` (append before `{{- end }}`)

- [ ] **Step 1: Append the override ConfigMap**

Insert immediately before the final `{{- end }}` line:

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "nebari-lgtm-pack.fullname" . }}-otel-overrides
  namespace: {{ .Values.otelCollectorOverrides.namespace }}
  labels:
    {{- include "nebari-lgtm-pack.labels" . | nindent 4 }}
  annotations:
    helm.sh/hook: post-install,post-upgrade
    helm.sh/hook-weight: "1"
    helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded
data:
  # Deep-merged into the existing collector config's data.relay via yq.
  # Endpoints reference {{ .Release.Name }} so the chart survives renames.
  overrides.yaml: |
    exporters:
      otlphttp/loki:
        endpoint: http://{{ .Release.Name }}-loki:3100/otlp
        tls:
          insecure: true
      otlp/tempo:
        endpoint: http://{{ .Release.Name }}-tempo:4317
        tls:
          insecure: true
      otlphttp/mimir:
        endpoint: http://{{ .Release.Name }}-mimir-gateway/otlp
        tls:
          insecure: true
    service:
      pipelines:
        logs:
          receivers: [otlp]
          processors: [memory_limiter, batch]
          exporters: [otlphttp/loki]
        traces:
          receivers: [otlp]
          processors: [memory_limiter, batch]
          exporters: [otlp/tempo]
        metrics:
          receivers: [otlp, prometheus]
          processors: [memory_limiter, batch]
          exporters: [otlphttp/mimir]
```

- [ ] **Step 2: Verify rendering with the default release name**

```bash
helm template lgtm-pack . --set nebariapp.enabled=false 2>&1 | yq 'select(.metadata.name == "lgtm-pack-nebari-lgtm-pack-otel-overrides") | .data["overrides.yaml"]' -
```

Expected output contains `http://lgtm-pack-loki:3100/otlp`, `http://lgtm-pack-tempo:4317`, `http://lgtm-pack-mimir-gateway/otlp`.

- [ ] **Step 3: Verify rendering with a custom release name**

```bash
helm template foo . --set nebariapp.enabled=false 2>&1 | yq 'select(.metadata.name == "foo-nebari-lgtm-pack-otel-overrides") | .data["overrides.yaml"]' -
```

Expected output contains `http://foo-loki:3100/otlp`, `http://foo-tempo:4317`, `http://foo-mimir-gateway/otlp`.

- [ ] **Step 4: Commit**

```bash
git add templates/otel-collector-config-patch.yaml
git commit -m "feat: add override ConfigMap templated on release name"
```

---

### Task B4: Add the patch Job to the template

**Files:**
- Modify: `templates/otel-collector-config-patch.yaml` (append before `{{- end }}`)

- [ ] **Step 1: Append the Job manifest**

Insert immediately before the final `{{- end }}` line:

```yaml
---
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "nebari-lgtm-pack.fullname" . }}-otel-patch
  namespace: {{ .Values.otelCollectorOverrides.namespace }}
  labels:
    {{- include "nebari-lgtm-pack.labels" . | nindent 4 }}
  annotations:
    helm.sh/hook: post-install,post-upgrade
    helm.sh/hook-weight: "2"
    helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded
spec:
  backoffLimit: 5
  ttlSecondsAfterFinished: 600
  template:
    metadata:
      labels:
        {{- include "nebari-lgtm-pack.selectorLabels" . | nindent 8 }}
        app.kubernetes.io/component: otel-patcher
    spec:
      serviceAccountName: {{ include "nebari-lgtm-pack.fullname" . }}-otel-patcher
      restartPolicy: OnFailure
      containers:
        - name: patch
          image: {{ .Values.otelCollectorOverrides.image | quote }}
          imagePullPolicy: {{ .Values.otelCollectorOverrides.imagePullPolicy | default "IfNotPresent" }}
          command: ["/bin/sh", "-c"]
          args:
            - |
              set -eu
              NS={{ .Values.otelCollectorOverrides.namespace | quote }}
              CM={{ .Values.otelCollectorOverrides.configMapName | quote }}
              DS={{ .Values.otelCollectorOverrides.daemonSetName | quote }}

              echo "Waiting up to 5m for ConfigMap $CM in $NS..."
              for i in $(seq 1 60); do
                if kubectl -n "$NS" get cm "$CM" >/dev/null 2>&1; then
                  echo "ConfigMap found."
                  break
                fi
                sleep 5
              done
              kubectl -n "$NS" get cm "$CM" >/dev/null

              echo "Reading current data.relay..."
              CURRENT=$(kubectl -n "$NS" get cm "$CM" -o jsonpath='{.data.relay}')
              if [ -z "$CURRENT" ]; then
                echo "ERROR: data.relay is empty; refusing to overwrite blindly." >&2
                exit 1
              fi

              echo "Deep-merging overrides..."
              MERGED=$(printf '%s\n' "$CURRENT" | yq -P '. *= load("/overrides/overrides.yaml")' -)

              echo "Patching ConfigMap..."
              PATCH=$(jq -n --arg relay "$MERGED" \
                '{metadata:{annotations:{"nic.nebari.dev/managed-by":"lgtm-pack"}},data:{relay:$relay}}')
              kubectl -n "$NS" patch cm "$CM" --type merge --patch "$PATCH"

              echo "Rolling DaemonSet $DS..."
              kubectl -n "$NS" rollout restart daemonset/"$DS"
              kubectl -n "$NS" rollout status daemonset/"$DS" --timeout=3m
              echo "Done."
          volumeMounts:
            - name: overrides
              mountPath: /overrides
              readOnly: true
      volumes:
        - name: overrides
          configMap:
            name: {{ include "nebari-lgtm-pack.fullname" . }}-otel-overrides
```

- [ ] **Step 2: Verify the Job renders**

```bash
helm template lgtm-pack . --set nebariapp.enabled=false 2>&1 | yq 'select(.kind == "Job" and .metadata.name == "lgtm-pack-nebari-lgtm-pack-otel-patch")' -
```

Expected: a `Job` block with `serviceAccountName: lgtm-pack-nebari-lgtm-pack-otel-patcher` and the script body containing `kubectl rollout restart daemonset/`.

- [ ] **Step 3: Helm lint passes**

```bash
helm lint . --set nebariapp.enabled=false
```

Expected: `1 chart(s) linted, 0 chart(s) failed`.

- [ ] **Step 4: Commit**

```bash
git add templates/otel-collector-config-patch.yaml
git commit -m "feat: add OTel collector patch Job with yq merge and rollout"
```

---

### Task B5: Reframe the existing example file

**Files:**
- Modify: `examples/opentelemetry-collector-overrides.yaml`

- [ ] **Step 1: Replace the header comment**

Open `examples/opentelemetry-collector-overrides.yaml`. Replace the existing header (lines 1–8) with:

```yaml
# OpenTelemetry Collector overrides — REFERENCE ONLY.
#
# The nebari-lgtm-pack chart now auto-applies the override below to the OTel
# collector ConfigMap deployed by NIC, via a post-install/post-upgrade Helm
# hook. You do NOT need to copy this file into your GitOps repo.
#
# This file is kept as a reference of what gets merged into the collector
# config. If you want to customize it, fork the chart or open an issue.
# Endpoints in the auto-applied version use {{ .Release.Name }}-{loki,tempo,
# mimir} rather than the hard-coded "lgtm-pack-" prefix shown here.
```

Leave the rest of the file (the `config:` block) untouched.

- [ ] **Step 2: Commit**

```bash
git add examples/opentelemetry-collector-overrides.yaml
git commit -m "docs(examples): reframe overrides file as reference, not manual step"
```

---

### Task B6: Update README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Find the right insertion point**

```bash
grep -n "^## " README.md
```

Pick an insertion point after the "Installation" or "Configuration" section (whichever exists). If neither exists, append at the end.

- [ ] **Step 2: Append the new section**

Append (or insert at the chosen point):

```markdown
## OpenTelemetry collector wiring

When this chart is installed on a cluster deployed by [nebari-infrastructure-core](https://github.com/nebari-dev/nebari-infrastructure-core) (NIC), a post-install Helm hook automatically rewires the OTel collector ConfigMap to ship logs/traces/metrics to this chart's Loki/Tempo/Mimir backends. No manual edits to the GitOps repo are required.

**How it works**

1. NIC ships an ArgoCD `Application` that deploys the upstream OTel collector with a default debug exporter. The Application has `ignoreDifferences` on the ConfigMap's `data.relay` field and `RespectIgnoreDifferences=true` in its sync options — meaning ArgoCD will not revert third-party changes to that field.
2. This chart's `post-install,post-upgrade` hook runs a Job that:
   - Reads the current `data.relay` from `opentelemetry-collector-opentelemetry-collector-agent` in `monitoring`.
   - Deep-merges the LGTM exporter and pipeline overrides via `yq`.
   - Patches the ConfigMap and stamps `nic.nebari.dev/managed-by=lgtm-pack`.
   - Rolls the collector DaemonSet so the new config is loaded.

**Disabling**

Set `otelCollectorOverrides.enabled=false` if NIC is not managing the collector (e.g. standalone LGTM against a user-managed collector).

**Uninstall behavior**

`helm uninstall` does **not** revert the ConfigMap. The collector will keep its LGTM-wired endpoints, which will start failing once the LGTM services are gone. To reset to NIC defaults, delete the ConfigMap and let ArgoCD recreate it from Helm:

```bash
kubectl -n monitoring delete configmap opentelemetry-collector-opentelemetry-collector-agent
```

ArgoCD's next sync will render a fresh debug-exporter ConfigMap from Helm.
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document OTel collector auto-wire behavior"
```

---

### Task B7: Extend CI test workflow to cover the auto-wire path

**Files:**
- Modify: `.github/workflows/test.yaml`

This step pre-creates a stub OTel collector ConfigMap + DaemonSet (mimicking what NIC would produce), installs the chart with the auto-wire enabled, and verifies the merge.

- [ ] **Step 1: Find the existing "Deploy chart" step in `.github/workflows/test.yaml`**

```bash
grep -n "Deploy chart" .github/workflows/test.yaml
```

- [ ] **Step 2: Insert new steps before "Deploy chart"**

Insert immediately before the "Deploy chart" step:

```yaml
      - name: Stage stub OTel collector (mimics NIC)
        run: |
          set -e
          kubectl create namespace monitoring
          # Stub ConfigMap with default debug-exporter relay (same shape as
          # the OTel chart renders). This is what NIC would put in place.
          cat <<'EOF' | kubectl apply -f -
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: opentelemetry-collector-opentelemetry-collector-agent
            namespace: monitoring
          data:
            relay: |
              exporters:
                debug: {}
              receivers:
                otlp:
                  protocols:
                    grpc:
                      endpoint: 0.0.0.0:4317
                prometheus: {}
              processors:
                batch: {}
                memory_limiter:
                  check_interval: 5s
                  limit_percentage: 80
              service:
                pipelines:
                  logs:    { receivers: [otlp],             processors: [memory_limiter, batch], exporters: [debug] }
                  traces:  { receivers: [otlp],             processors: [memory_limiter, batch], exporters: [debug] }
                  metrics: { receivers: [otlp, prometheus], processors: [memory_limiter, batch], exporters: [debug] }
          EOF
          # Stub DaemonSet so the Job's `rollout restart` and `rollout status`
          # have something to act on. Uses pause image (tiny, always available).
          cat <<'EOF' | kubectl apply -f -
          apiVersion: apps/v1
          kind: DaemonSet
          metadata:
            name: opentelemetry-collector-opentelemetry-collector-agent
            namespace: monitoring
          spec:
            selector:
              matchLabels:
                app: stub-otel
            template:
              metadata:
                labels:
                  app: stub-otel
              spec:
                containers:
                  - name: pause
                    image: registry.k8s.io/pause:3.9
          EOF
          kubectl -n monitoring rollout status daemonset/opentelemetry-collector-opentelemetry-collector-agent --timeout=2m
```

- [ ] **Step 3: Update the "Deploy chart" step to enable auto-wire**

Find the existing step:

```yaml
      - name: Deploy chart
        run: |
          helm install lgtm-pack . --namespace default \
            --set nebariapp.enabled=false \
            --set grafana.envFromConfigMaps=null \
            --set grafana.envValueFrom=null \
            --timeout 10m
```

Change it to:

```yaml
      - name: Deploy chart
        run: |
          helm install lgtm-pack . --namespace default \
            --set nebariapp.enabled=false \
            --set grafana.envFromConfigMaps=null \
            --set grafana.envValueFrom=null \
            --set otelCollectorOverrides.enabled=true \
            --timeout 10m
```

- [ ] **Step 4: Add a verification step after "Deploy chart"**

Insert immediately after the "Deploy chart" step:

```yaml
      - name: Verify OTel collector ConfigMap was patched
        run: |
          set -e
          echo "=== ConfigMap annotations ==="
          kubectl -n monitoring get cm opentelemetry-collector-opentelemetry-collector-agent \
            -o jsonpath='{.metadata.annotations}' | tee /tmp/annotations.json
          echo
          grep -q '"nic.nebari.dev/managed-by":"lgtm-pack"' /tmp/annotations.json

          echo "=== Merged data.relay ==="
          kubectl -n monitoring get cm opentelemetry-collector-opentelemetry-collector-agent \
            -o jsonpath='{.data.relay}' | tee /tmp/relay.yaml
          echo

          # All three exporter endpoints present, derived from release name "lgtm-pack"
          grep -q 'http://lgtm-pack-loki:3100/otlp' /tmp/relay.yaml
          grep -q 'http://lgtm-pack-tempo:4317' /tmp/relay.yaml
          grep -q 'http://lgtm-pack-mimir-gateway/otlp' /tmp/relay.yaml

          # NIC's receivers / processors preserved (merge, not replace)
          grep -q 'memory_limiter:' /tmp/relay.yaml
          grep -q 'prometheus:' /tmp/relay.yaml

      - name: Dump patch Job logs on failure
        if: failure()
        run: |
          echo "=== Patch Job pods ==="
          kubectl -n monitoring get pods -l app.kubernetes.io/component=otel-patcher
          echo "=== Patch Job logs ==="
          kubectl -n monitoring logs -l app.kubernetes.io/component=otel-patcher --tail=200 || true
```

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/test.yaml
git commit -m "ci: verify OTel collector auto-wire patches the ConfigMap"
```

---

### Task B8: Local smoke test before opening PR

This step runs the chart in a local k3d cluster to catch issues before CI.

- [ ] **Step 1: Ensure k3d / kind is installed and create a cluster**

If you don't have k3d:

```bash
curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
```

Create cluster:

```bash
k3d cluster create lgtm-test --wait
```

- [ ] **Step 2: Pre-stage stub OTel collector**

```bash
cd /Users/tylerman/gh/nebari-lgtm-pack
kubectl create namespace monitoring
kubectl apply -n monitoring -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: opentelemetry-collector-opentelemetry-collector-agent
data:
  relay: |
    exporters:
      debug: {}
    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: 0.0.0.0:4317
      prometheus: {}
    processors:
      batch: {}
      memory_limiter:
        check_interval: 5s
        limit_percentage: 80
    service:
      pipelines:
        logs:    { receivers: [otlp], processors: [memory_limiter, batch], exporters: [debug] }
        traces:  { receivers: [otlp], processors: [memory_limiter, batch], exporters: [debug] }
        metrics: { receivers: [otlp, prometheus], processors: [memory_limiter, batch], exporters: [debug] }
EOF
kubectl apply -n monitoring -f - <<'EOF'
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: opentelemetry-collector-opentelemetry-collector-agent
spec:
  selector: { matchLabels: { app: stub-otel } }
  template:
    metadata: { labels: { app: stub-otel } }
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
EOF
kubectl -n monitoring rollout status daemonset/opentelemetry-collector-opentelemetry-collector-agent --timeout=2m
```

- [ ] **Step 3: Install the chart**

```bash
helm dependency update
helm install lgtm-pack . --namespace default \
  --set nebariapp.enabled=false \
  --set grafana.envFromConfigMaps=null \
  --set grafana.envValueFrom=null \
  --timeout 10m
```

- [ ] **Step 4: Verify the patch**

```bash
kubectl -n monitoring get cm opentelemetry-collector-opentelemetry-collector-agent -o jsonpath='{.data.relay}'
```

Expected: contains `lgtm-pack-loki`, `lgtm-pack-tempo`, `lgtm-pack-mimir-gateway` AND preserves `memory_limiter` + `prometheus` receivers.

- [ ] **Step 5: Tear down**

```bash
k3d cluster delete lgtm-test
```

---

### Task B9: Open LGTM pack PR

- [ ] **Step 1: Push the branch**

```bash
cd /Users/tylerman/gh/nebari-lgtm-pack
git push -u origin otel-collector-auto-wire
```

- [ ] **Step 2: Open PR**

```bash
gh pr create --title "feat: auto-wire OTel collector to LGTM backends on install" --body "$(cat <<'EOF'
## Summary

- Adds a `post-install,post-upgrade` Helm hook (`templates/otel-collector-config-patch.yaml`) that reads the OTel collector ConfigMap deployed by [nebari-infrastructure-core](https://github.com/nebari-dev/nebari-infrastructure-core), deep-merges this chart's exporter+pipeline overrides into `data.relay` via `yq`, stamps `nic.nebari.dev/managed-by=lgtm-pack`, and rolls the collector DaemonSet.
- New `otelCollectorOverrides` values block (toggle, target names, image) — defaults match NIC's collector Application output.
- Override ConfigMap endpoints are templated with `{{ .Release.Name }}` so the chart survives custom release names.
- Existing `examples/opentelemetry-collector-overrides.yaml` reframed as reference (no longer a manual copy step).
- CI workflow now pre-stages a stub OTel collector ConfigMap + DaemonSet and asserts the patch landed.

Depends on (but does not require pre-merge of): https://github.com/nebari-dev/nebari-infrastructure-core/pull/XXX (NIC `ignoreDifferences` change). Without that change, ArgoCD will revert this chart's patch on its next sync; with it, the patch persists.

Closes https://github.com/nebari-dev/nebari-lgtm-pack/issues/8

## Test plan

- [x] `helm lint .` clean
- [x] `helm template . | yq` renders the SA, RBAC, override CM, and Job
- [x] Custom release name (`helm template foo .`) produces `foo-loki` / `foo-tempo` / `foo-mimir-gateway` endpoints
- [x] Local k3d smoke test (Task B8) — stub collector pre-staged, install patches CM correctly, receivers/processors preserved by merge
- [ ] CI test workflow passes (covers same steps as local smoke test)
EOF
)"
```

- [ ] **Step 3: After both PRs are open, update the NIC PR body** to link to this PR (so reviewers see the full picture).

---

## Self-review checklist

- [x] **Spec coverage:** Every section of the spec is covered by tasks:
  - NIC changes (spec §"NIC changes") → Tasks A1–A3
  - LGTM template (spec §"New file") → Tasks B2–B4
  - values.yaml additions (spec §"values.yaml additions") → Task B1
  - Examples reframe (spec §"examples/opentelemetry-collector-overrides.yaml") → Task B5
  - Documentation (spec §"Documentation") → Task B6
  - Testing (spec §"Testing strategy") → Tasks A1 (unit), B7 (CI), B8 (local smoke)
  - Edge cases (spec §"Edge cases") — covered by the Job's wait loop and `rollout status --timeout`, exercised by CI
- [x] **Placeholder scan:** No "TBD", "TODO", "implement later". Every step has the exact code/command. Exception: PR body references `nebari-infrastructure-core/pull/XXX` — this is a real placeholder for the actual PR number, which is determined after Task A3 runs. The Task B9 step 3 explicitly handles the linkback.
- [x] **Type/name consistency:**
  - `opentelemetry-collector-opentelemetry-collector-agent` used for ConfigMap and DaemonSet across all tasks (matches `helm template` verification).
  - `{{ include "nebari-lgtm-pack.fullname" . }}-otel-patcher` used consistently for SA / Role / RoleBinding / `serviceAccountName`.
  - `{{ include "nebari-lgtm-pack.fullname" . }}-otel-overrides` used consistently for the override ConfigMap and the Job's volume reference.
  - `{{ include "nebari-lgtm-pack.fullname" . }}-otel-patch` for the Job name.
  - `nic.nebari.dev/managed-by=lgtm-pack` annotation matches between Job script and CI verification.
