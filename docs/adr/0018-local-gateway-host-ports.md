# ADR-0018: Publish the Local Gateway on Host Ports Instead of MetalLB

## Status

Accepted (2026-09-01)

Records the decision implemented by [#640](https://github.com/nebari-dev/nebari-infrastructure-core/pull/640) (closes [#639](https://github.com/nebari-dev/nebari-infrastructure-core/issues/639)). Amends the MetalLB references in [ADR-0006](0006-conditional-foundational-software-helm.md) (which expected MetalLB to migrate to the conditional Helm interface) and [ADR-0014](0014-helm-valuefiles-overlay-seam.md) (which used metallb as a gated-app example). Both carry notes under their Status pointing here.

## Date

2026-09-01

## Context

The local provider runs the platform on a kind cluster, and kind ships no `LoadBalancer` implementation. Until #640, NIC filled that gap by deploying MetalLB and a `metallb-config` app whose `IPAddressPool` was derived at deploy time from the kind node's address on the Docker bridge network. The gateway's Envoy service was `type: LoadBalancer` and received an address such as `172.18.255.100` from that pool. MetalLB was the only `LoadBalancer` consumer in the repo.

This construction had four standing problems:

- **The address pool was a computed value living on `InfraSettings`.** Every other `InfraSettings` field is a static fact a provider can state from config alone. The pool could only be derived after the cluster existed, which created an ordering dependency inside the deploy flow. [#639](https://github.com/nebari-dev/nebari-infrastructure-core/issues/639) was exactly this class of bug: the pool was read before it was derived, and the gateway received an address from the static fallback range that no host route matched.
- **The address was barely reachable.** A Docker bridge address is routable from a Linux host, needs [docker-mac-net-connect](https://github.com/chipmk/docker-mac-net-connect) on macOS, and is reachable by nothing else. Real ACME issuance through the gateway was already impossible on local, so the address served exactly one client, the developer's own machine, while costing platform-specific host tooling.
- **The deploy blocked on a LoadBalancer wait** for an address that was knowable in advance.
- **MetalLB itself was a controller no cloud provider runs**, so the one Kubernetes-level fact it produced (a populated `status.loadBalancer.ingress`) was fidelity theater rather than fidelity.

## Decision Drivers

- The local dev loop must work identically on macOS, Linux, and Windows with no extra host tooling.
- `InfraSettings` must carry static provider facts. Computed-at-deploy values on it produced the #639 ordering class and would keep producing it.
- Cloud fidelity matters in the manifests (Gateway, HTTPRoutes, cert-manager, Keycloak, the ArgoCD app set), not in which controller fills in a service status field. The last hop is the only place local may diverge, and the divergence should be an explicit typed seam.
- Prefer deleting the failing mechanism over patching its ordering.
- The local CI deployment test must keep a real reachability signal. An address appearing in a service status was the signal MetalLB provided, and its replacement must be at least as strong.

## Considered Options

1. Keep MetalLB and fix the pool derivation ordering (the direct #639 fix)
2. Keep MetalLB with a static, operator-configured address pool
3. Publish the gateway on host ports of the loopback interface via kind port mappings pinned to fixed NodePorts, and remove MetalLB

## Decision Outcome

Chosen option: **Option 3**.

The mechanics, and where each value lives:

- kind maps host ports 80 and 443 of `127.0.0.1` (configurable via `cluster.local.http_port` / `https_port`) to the fixed NodePorts `GatewayHTTPNodePort` (30080) and `GatewayHTTPSNodePort` (30443) at cluster creation. The constants live in `pkg/providers/cluster` and are read by both the provider's port mappings and the rendered EnvoyProxy manifest, so the two sides cannot drift.
- The EnvoyProxy resource pins the gateway's Envoy service to those NodePorts through a strategic-merge patch that matches the Gateway's listener ports.
- `InfraSettings.GatewayHostAddress` (non-empty means host-port publishing) carries the address as a static provider fact. Deploy, outputs, and the CLI consume it instead of deriving or asserting an address.
- A `dns:` block is rejected on loopback host-port gateways at validate and deploy time. Public DNS records cannot usefully point at another machine's loopback, and the deploy prints `/etc/hosts` guidance instead.
- The ports are recorded in a `nic-local-cluster` ConfigMap in `kube-system` at creation (the kubeadm-config pattern), and a redeploy fails on mismatch, because kind port mappings cannot change on a live cluster.
- `nic outputs` reports the host address only after substantiating it at read time: the Envoy service must carry the pinned NodePorts, and the gateway must answer a real HTTPS request at the address. A strategic-merge patch that silently stopped matching, or a listener nothing published, surfaces as an unresolved field instead of a wrong URL. This holds at every later read too, since the cluster keeps reconciling after deploy.
- CI consumes that same substantiated read: [deploy-nebari-action](https://github.com/nebari-dev/deploy-nebari-action) runs `nic outputs` after its convergence wait ([deploy-nebari-action#26](https://github.com/nebari-dev/deploy-nebari-action/pull/26)) and reaches this repo with the action's pin bump, so an unreachable local gateway can no longer produce a usable `gateway-address`. Deploy-time gating (a `nic deploy --wait` that ends by probing the gateway) is [#574](https://github.com/nebari-dev/nebari-infrastructure-core/issues/574).

### Consequences

**Good:**

- The #639 failure class is gone structurally: no address derivation, no deploy-time computed `InfraSettings` value, no LoadBalancer wait.
- The dev loop is identical on macOS, Linux, and Windows. Published ports are plain Docker port mappings, and docker-mac-net-connect is not needed.
- Net deletion of code and of one runtime controller. MetalLB, its config app, and the pool derivation are removed.
- The reachability signal in CI gets stronger: a real request through Envoy (the deploy action's post-convergence probe) instead of a status field populated by a controller no production cluster runs.
- The platform binds loopback only, so a development cluster is not exposed to the LAN.

**Bad:**

- Local diverges from cloud at the last hop: a `NodePort` service behind host ports instead of a `LoadBalancer` service. The divergence is explicit (`GatewayHostAddress`) but real, and endpoint discovery (`endpoint.GetLoadBalancerEndpoint`) is no longer exercised by the free local CI job, only by the paid cloud jobs.
- Changing `http_port` / `https_port` requires recreating the cluster. The provisioning marker turns this from silent breakage into a deploy-time error, but the restriction itself is inherent to kind.
- Ports 80 and 443 must be free on the host (or overridden), and only one local cluster can own a given pair at a time.
- Wildcard DNS for subdomains is not solved by `/etc/hosts`. Public wildcard loopback domains (`lvh.me`, `localtest.me`) cover this without any NIC involvement, at the cost of internet-dependent resolution and resolver rebind-protection caveats.

## Options Detail

### Option 1: Fix the pool derivation ordering

Repairs #639 but keeps everything else: the bridge address remains invisible to macOS without docker-mac-net-connect, `InfraSettings` keeps carrying a computed value whose ordering the deploy flow must respect, and the LoadBalancer wait stays.

### Option 2: Static operator-configured pool

Removes the derivation and its ordering, but pushes the burden of picking a routable range onto the operator per machine and per Docker network, which is the configuration that produced "gateway with an IP from the configured pool" failures in the first place. macOS reachability is unchanged.

### Option 3: Host ports on pinned NodePorts (chosen)

See Decision Outcome.

## References

- [#639](https://github.com/nebari-dev/nebari-infrastructure-core/issues/639) MetalLB address pool derivation ordering bug
- [#640](https://github.com/nebari-dev/nebari-infrastructure-core/pull/640) implementation
- [ADR-0006](0006-conditional-foundational-software-helm.md), [ADR-0014](0014-helm-valuefiles-overlay-seam.md) amended by this decision
- [docs/local-kind-development.md](../local-kind-development.md) operator-facing description of the networking model
