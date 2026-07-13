# Resource sizing for foundational software

Every NIC deploy installs a foundational stack (ArgoCD, cert-manager, Keycloak, PostgreSQL, Envoy Gateway, the OTel collector, the nebari-operator, the landing page, and optionally MetalLB and Longhorn) before any user workload runs. This page documents the default resource requests and limits, why they are what they are, what each component's usage scales with, and how to tune them.

The numbers come from the audit in [#456](https://github.com/nebari-dev/nebari-infrastructure-core/issues/456): a fresh single-node install measured at idle (metrics-server, 15 samples of `kubectl top pods` at 60 second intervals), cross-checked against each project's official sizing guidance and chart defaults. Idle usage is a floor, not a target; the defaults add headroom for login bursts, TLS issuance, and sync spikes.

## The limits policy

- Every pod gets CPU and memory requests. Requests drive scheduling, node sizing, and cluster autoscaler decisions; a pod without requests runs at BestEffort priority and is first to be evicted under node pressure.
- Every pod gets a memory limit. Memory is incompressible: a leaking pod without a limit degrades the whole node instead of being OOM-killed and restarted cleanly. Two components make the memory limit load-bearing rather than optional: Keycloak sizes its JVM heap as 70% of the container memory limit (`MaxRAMPercentage`), and the OTel collector chart derives `GOMEMLIMIT` from the memory limit (`useGOMEMLIMIT`), so removing it silently drops the collector's OOM protection.
- Control-plane components get CPU limits; the Envoy data-plane proxies and Keycloak deliberately do not. CPU is compressible, so a pod exceeding its request is throttled fairly under contention anyway. Hard CPU limits mainly add tail latency, which hurts the data path and login bursts exactly when they matter.

## Default values

| Component | Pod | CPU request | Mem request | CPU limit | Mem limit |
|---|---|---|---|---|---|
| ArgoCD | application-controller | 100m | 256Mi | 500m | 512Mi |
| ArgoCD | repo-server | 25m | 128Mi | 500m | 512Mi |
| ArgoCD | server | 25m | 64Mi | 200m | 128Mi |
| ArgoCD | applicationset-controller | 25m | 64Mi | 200m | 128Mi |
| ArgoCD | redis | 25m | 64Mi | 200m | 128Mi |
| ArgoCD | notifications-controller | 25m | 64Mi | 200m | 128Mi |
| ArgoCD | dex | disabled | | | |
| cert-manager | controller | 25m | 64Mi | 200m | 256Mi |
| cert-manager | webhook | 10m | 32Mi | 100m | 128Mi |
| cert-manager | cainjector | 10m | 64Mi | 200m | 256Mi |
| PostgreSQL | primary | 100m | 256Mi | 500m | 512Mi |
| Keycloak | keycloak | 250m | 1Gi | none | 2Gi |
| Envoy Gateway | controller | 50m | 128Mi | 500m | 512Mi |
| Envoy Gateway | proxy (per Gateway, data plane) | 100m | 128Mi | none | 512Mi |
| MetalLB | controller | 25m | 64Mi | 100m | 128Mi |
| MetalLB | speaker (per node) | 50m | 128Mi | 200m | 256Mi |
| MetalLB | speaker frr sidecars | 25m | 64Mi | 100m | 128Mi |
| OTel collector | agent (per node) | 50m | 128Mi | 250m | 512Mi |
| nebari-operator | manager | 10m | 64Mi | 200m | 128Mi |
| Longhorn | longhorn-manager (per node) | 50m | 128Mi | 500m | 512Mi |
| Longhorn | CSI sidecars (attacher, provisioner, resizer, snapshotter) | 10m | 32Mi | 100m | 128Mi |

The ArgoCD dex pod is disabled entirely: NIC wires ArgoCD's OIDC directly to Keycloak, so dex is never used.

Storage: PostgreSQL provisions a 10Gi PVC. A small install needs far less, but volumes can grow and never shrink, and the cost is negligible, so the default stays.

Replicas: everything runs a single replica by default, which is right for a base install. See the scale-up section below.

## What each component scales with

- **ArgoCD application-controller**: memory grows with the number of managed Kubernetes resources (roughly 500MB base plus per-resource overhead on large installs). The 256Mi request covers the foundational apps; installs managing many additional Applications should raise it.
- **ArgoCD repo-server**: memory spikes during manifest generation (`helm template`). If syncs OOM, raise the memory limit before anything else, or lower repo-server parallelism (`--parallelismlimit`).
- **cert-manager controller and cainjector**: both cache TLS Secrets in memory, so memory scales with the number and size of certificates in the cluster. High certificate churn (short-lived certs) also raises controller CPU.
- **Keycloak**: the official sizing docs state 1250MB base memory per pod including realm caches and 10,000 cached sessions, and about 1 vCPU per 15 password logins per second. The 250m request is a scheduling floor; because there is no CPU limit, login bursts can use idle node capacity. If you expect sustained login load, raise the CPU request accordingly and revisit the PostgreSQL values too (Keycloak's docs budget 0.35 to 0.7 vCPU per 100 login-flow requests per second on the database).
- **PostgreSQL**: sized between the Bitnami nano and small presets. It backs Keycloak and the operator; user workloads do not touch it.
- **Envoy Gateway controller**: xDS state scales with the number of routes and backends. Idle is under 40Mi; hundreds of HTTPRoutes will push it up.
- **Envoy proxy (data plane)**: memory scales with connection count and route table size. The default replaces Envoy Gateway's built-in 512Mi request (idle usage is about 40Mi) with 128Mi requested and a 512Mi ceiling. Sizing lives in the `EnvoyProxy` resource at `pkg/argocd/templates/manifests/networking/envoyproxy.yaml`, referenced from the GatewayClass.
- **OTel collector**: runs as a per-node agent; memory scales with telemetry volume. The memory limit doubles as the `GOMEMLIMIT` anchor.
- **Longhorn**: the dominant cost is not a pod default but the "Guaranteed Instance Manager CPU" setting. By default Longhorn reserves 12% of every node's allocatable CPU per instance-manager pod, per node: on a 4 vCPU node that is roughly 480m gone before any volume exists, and the v2 data engine reserves a flat 1250m. Set `instance_manager_cpu_percent` in the Longhorn storage config to tune it (0 removes the reservation, at the cost of volume performance under CPU pressure). Longhorn's own minimums still apply: 3 nodes, 4 vCPU and 4GiB per node. The `longhorn-csi-plugin` DaemonSet is intentionally left without limits: it is the per-node mount path and throttling it delays volume mounts.

## How to override

- **App templates** (cert-manager, PostgreSQL, Keycloak, Envoy Gateway, MetalLB, OTel collector, operator): values live in the ArgoCD Application templates under `pkg/argocd/templates/apps/` and are baked into the binary at build time. Deployed clusters can be adjusted by editing the synced copies in the GitOps repo; changes here require a rebuild.
- **ArgoCD itself**: values live in `DefaultConfig` in `pkg/argocd/config.go`. Note that Helm values changes only reach existing installs when the chart `Version` is bumped (see the `Config.Version` doc comment).
- **Envoy data plane**: edit the `EnvoyProxy` resource in `pkg/argocd/templates/manifests/networking/envoyproxy.yaml`.
- **Longhorn**: `instance_manager_cpu_percent` under the provider's longhorn config block; manager and CSI defaults are set in `pkg/storage/longhorn/install.go`.

## Scaling up

- **Keycloak**: adding replicas requires Infinispan cache clustering to be configured first; a second replica without it does not share sessions. Scale memory/CPU vertically first.
- **Envoy proxy**: proxy replicas are the first availability lever for the gateway. The controller itself stays at 1 replica happily.
- **ArgoCD**: the HA path is knob-based (controller sharding, repo-server replicas with parallelism limits) rather than blind replica increases.
- **MetalLB speaker** is a per-node DaemonSet by design; the controller stays at 1.
- **PostgreSQL**: raise to the Bitnami small/medium preset shape before considering read replicas; Keycloak is the only real client.
