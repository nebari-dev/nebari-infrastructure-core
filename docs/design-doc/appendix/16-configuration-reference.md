# Configuration Reference

This is the authoritative reference for `nebari-config.yaml`. Field-level source of truth is the Go code; this document is updated as code changes. Ground-truth file references are inline.

## Table of Contents

1. [Top-Level Schema](#1-top-level-schema)
2. [Cluster Providers](#2-cluster-providers)
   1. [`cluster.aws`](#21-clusteraws-amazon-eks)
   2. [`cluster.hetzner`](#22-clusterhetzner-hetzner-cloud-k3s)
   3. [`cluster.local`](#23-clusterlocal-kind-for-development)
   4. [`cluster.existing`](#24-clusterexisting-adopt-a-pre-provisioned-cluster)
   5. [`cluster.gcp` / `cluster.azure`](#25-clustergcp--clusterazure-stubs)
3. [DNS Providers](#3-dns-providers)
4. [Certificate](#4-certificate)
5. [Git Repository](#5-git-repository)
6. [Environment Variables](#6-environment-variables)

---

## 1. Top-Level Schema

Defined by `NebariConfig` in `pkg/config/config.go`:

```yaml
project_name: my-nebari        # required, [a-zA-Z0-9][a-zA-Z0-9_-]*
domain: nebari.example.com     # optional, but needed for routable services

cluster:                       # required, exactly one provider
  <provider-name>:
    ...

dns:                           # optional, exactly one provider
  <provider-name>:
    ...

git_repository:                # required on cloud providers; optional on local
  url: ...
  ...

certificate:                   # optional, defaults to selfsigned
  type: ...
```

Anti-pattern: there is no top-level `provider:`, `version:`, `name:`, `kubernetes:`, `node_pools:`, `tls:`, `foundational_software:`, `images:`, or `features:` field. If older documentation shows those, it is out of date.

| Field | Type | Required | Source |
|-------|------|----------|--------|
| `project_name` | string | ✅ | `NebariConfig.ProjectName` |
| `domain` | string | optional | `NebariConfig.Domain` |
| `cluster` | map | ✅ | `NebariConfig.Cluster` (`ClusterConfig`) |
| `dns` | map | optional | `NebariConfig.DNS` (`DNSConfig`) |
| `git_repository` | object | conditional | `NebariConfig.GitRepository` (`git.Config`) |
| `certificate` | object | optional | `NebariConfig.Certificate` (`CertificateConfig`) |

---

## 2. Cluster Providers

`cluster:` takes exactly one key, the provider name. The shape of the nested object is provider-specific.

Valid provider names (registered in `cmd/nic/main.go`): `aws`, `hetzner`, `local`, `existing`, `gcp`, `azure`.

### 2.1 `cluster.aws` (Amazon EKS)

Source: `pkg/provider/aws/config.go`. Status: **implemented**.

```yaml
cluster:
  aws:
    region: us-west-2                          # required
    kubernetes_version: "1.34"                 # required (string)
    availability_zones:                        # optional (defaults to []; module picks)
      - us-west-2a
      - us-west-2b
    vpc_cidr_block: "10.10.0.0/16"             # optional, default: "10.0.0.0/16"
    endpoint_private_access: true
    endpoint_public_access: true

    # Optional: adopt existing VPC infrastructure
    # existing_vpc_id: vpc-...
    # existing_private_subnet_ids: [subnet-..., subnet-...]
    # existing_security_group_id: sg-...

    # Optional: pin to existing IAM roles
    # existing_cluster_role_arn: arn:aws:iam::...
    # existing_node_role_arn:    arn:aws:iam::...
    # permissions_boundary:      arn:aws:iam::...:policy/...

    # Optional: EKS KMS key + log types
    # eks_kms_arn: arn:aws:kms:...
    enabled_log_types: ["api", "audit"]

    node_groups:                                # map keyed by node-group name
      user:
        instance: m7i.xlarge
        min_nodes: 1
        max_nodes: 5
        # ami_type: AL2023_x86_64_STANDARD     # defaults to AL2023 STANDARD
        # gpu: true                            # uses AL2023_x86_64_NVIDIA AMI
        # spot: true
        # disk_size: 100
        # labels:
        #   workload: user
        # taints:
        #   - key: nebari.example/dedicated
        #     value: user
        #     effect: NO_SCHEDULE              # NO_SCHEDULE, NO_EXECUTE, PREFER_NO_SCHEDULE

    tags:                                       # optional map[string]string
      Environment: development

    # Optional: AWS Load Balancer Controller (default: enabled)
    # aws_load_balancer_controller:
    #   enabled: true
    #   chart_version: "3.2.1"
    #   destroy_timeout: 5m

    # Optional: EFS shared storage
    efs:
      enabled: true
      performance_mode: generalPurpose          # generalPurpose | maxIO
      throughput_mode: bursting                 # bursting | provisioned | elastic
      encrypted: true
      # provisioned_throughput_mibps: 100        # required if throughput_mode is provisioned
      # kms_key_arn: arn:aws:kms:...
      # storage_class_name: efs-sc

    # Optional: Longhorn distributed storage (default: enabled when nil)
    # longhorn:
    #   enabled: true
    #   replica_count: 2
    #   dedicated_nodes: false
    #   node_selector: { workload: storage }
```

Fields not in `aws.NodeGroup`: `single_subnet`, per-node-group `permissions_boundary`. If you see them in older docs, they are not real.

State backend: S3 with `use_lockfile = true`, bucket auto-created per [§5.2 of State Management](../architecture/05-state-management.md). No DynamoDB.

### 2.2 `cluster.hetzner` (Hetzner Cloud k3s)

Source: `pkg/provider/hetzner/config.go`. Status: **implemented**. Backed by the `hetzner-k3s` binary - **not** OpenTofu.

```yaml
cluster:
  hetzner:
    location: ash                              # required: Hetzner location (ash, fsn1, nbg1, ...)
    kubernetes_version: "1.32"                 # required: "1.32", "1.32.0", or "v1.32.0+k3s1"

    # Optional: prevent application pods on control-plane nodes.
    # Default: true (single-node clusters and small instances work better).
    # Set to false for production with dedicated masters.
    # schedule_workloads_on_masters: false

    # Optional: preserve CSI volumes through destroy.
    # When true, deploy labels volumes persist=true and destroy skips them.
    # persist_data: false

    node_groups:                                # map keyed by node-group name; exactly one must have master: true
      master:
        instance_type: cpx31
        count: 1                                # for k3s HA, count should be 1, 3, or 5 (odd)
        master: true
      workers:
        instance_type: cpx31
        count: 2
        # autoscaling:
        #   enabled: true
        #   min_instances: 2
        #   max_instances: 6

    # Optional: provide your own SSH keys (else NIC generates ed25519 keys in ~/.cache/nic/hetzner-k3s/ssh/)
    # ssh:
    #   public_key_path:  ~/.ssh/id_ed25519.pub
    #   private_key_path: ~/.ssh/id_ed25519

    # Optional: restrict SSH and API CIDRs (defaults to 0.0.0.0/0; NIC warns at validate time)
    # network:
    #   ssh_allowed_cidrs: [203.0.113.0/24]
    #   api_allowed_cidrs: [203.0.113.0/24]
```

The Hetzner provider requires the `HCLOUD_TOKEN` environment variable.

### 2.3 `cluster.local` (Kind for development)

Source: `pkg/provider/local/config.go`. Status: **implemented as a stub**. The local provider does not create the cluster itself; `make localkind-up` does. The provider is a thin adapter that runs the bootstrap (ArgoCD + foundational apps) against the Kind cluster.

```yaml
cluster:
  local:
    kube_context: "kind-nebari-local"          # context name from kubeconfig
    # storage_class: standard                   # default: "standard"; use "local-path" for k3s
    # https_port: 443                           # override e.g. 8443 if 443 is in use

    # MetalLB defaults to enabled with pool 192.168.1.100-192.168.1.110
    # metallb:
    #   enabled: false                          # disable for k3s (ships with ServiceLB)
    #   address_pool: 172.18.255.100-172.18.255.110

    # Optional: per-node-group selectors used by software packs
    # node_selectors:
    #   general:
    #     kubernetes.io/os: linux
    #   user:
    #     kubernetes.io/os: linux
```

The local provider sets `InfraSettings.SupportsLocalGitOps = true`, which lets NIC auto-create `/tmp/nebari-gitops-<project_name>` when `git_repository:` is not specified.

### 2.4 `cluster.existing` (adopt a pre-provisioned cluster)

Source: `pkg/provider/existing/config.go`. Status: **implemented**. No provisioning happens; NIC just runs the bootstrap against whatever cluster the kubeconfig points at.

```yaml
cluster:
  existing:
    # Path to the kubeconfig file. May be absolute or relative; tilde is NOT expanded.
    # When empty: falls back to $KUBECONFIG env var, then $HOME/.kube/config.
    kubeconfig: path/to/kubeconfig

    # Required: context name within that kubeconfig.
    context: "arn:aws:eks:us-west-2:123456789012:cluster/my-nebari"

    # Optional: default StorageClass for foundational PVCs (default: "standard")
    storage_class: gp2

    # Optional: annotations applied to the Envoy Gateway LoadBalancer Service
    # load_balancer_annotations:
    #   load-balancer.hetzner.cloud/location: ash
```

### 2.5 `cluster.gcp` / `cluster.azure` (stubs)

Sources: `pkg/provider/gcp/config.go`, `pkg/provider/azure/config.go`. Status: **registered but not implemented**. The struct fields exist for forward compatibility; calling `Validate`, `Deploy`, `Destroy`, or `GetKubeconfig` on these providers returns "not yet implemented" today.

The GCP struct accepts: `project`, `region`, `kubernetes_version`, `availability_zones`, `release_channel`, `node_groups` (map), `tags`, `networking_mode`, `network`, `subnetwork`, `ip_allocation_policy`, `master_authorized_networks_config`, `private_cluster_config`.

The Azure struct accepts: `region`, `kubernetes_version`, `storage_account_postfix`, `authorized_ip_ranges`, `resource_group_name`, `node_resource_group_name`, `node_groups` (map), `vnet_subnet_id`, `private_cluster_enabled`, `tags`, `network_profile`, `max_pods`, `workload_identity_enabled`, `azure_policy_enabled`.

See [`examples/gcp-config.yaml`](../../../examples/gcp-config.yaml) and [`examples/azure-config.yaml`](../../../examples/azure-config.yaml) for schemas. Don't try to deploy with them.

---

## 3. DNS Providers

`dns:` takes exactly one key. The shape is provider-specific.

Valid provider names: `cloudflare` (the only DNS provider implemented today).

### 3.1 `dns.cloudflare`

Source: `pkg/dnsprovider/cloudflare/config.go`.

```yaml
dns:
  cloudflare:
    zone_name: example.com                     # the Cloudflare zone hosting `domain`
```

Behavior:

- On deploy, NIC waits for the Envoy Gateway LB to receive a hostname or IP and then creates a root record and a wildcard record (`*.<domain>`) in the zone. Record type is A for IPs, CNAME for hostnames.
- On destroy, both records are removed. Idempotent.
- Failures are non-blocking: deploy/destroy continue with a warning.

Credential: `CLOUDFLARE_API_TOKEN` env var, with Zone:Read and DNS:Edit permissions on the zone. Domain must be a suffix of `zone_name` (suffix check with a dot separator).

Future DNS providers (Route53, Azure DNS, Google Cloud DNS) will follow the same shape and the same `DNSProvider` interface defined in `pkg/dnsprovider/provider.go`.

---

## 4. Certificate

Source: `pkg/config/config.go` (`CertificateConfig`, `ACMEConfig`).

```yaml
certificate:
  type: letsencrypt                            # "selfsigned" (default) | "letsencrypt"
  acme:                                        # required when type: letsencrypt
    email: admin@example.com
    # server: https://acme-staging-v02.api.letsencrypt.org/directory  # use staging for testing
```

When omitted, NIC behaves as if `type: selfsigned` was set. `selfsigned` is appropriate for local clusters, internal environments, and `existing` clusters where cert lifecycle is handled out-of-band. `letsencrypt` requires a publicly-routable `domain` (and typically a DNS provider).

---

## 5. Git Repository

Source: `pkg/git/config.go` (`Config`, `AuthConfig`).

```yaml
git_repository:
  url: "git@github.com:my-org/my-gitops-repo.git"   # SSH, HTTPS, or file:// path
  branch: main                                       # default: "main"
  path: "clusters/my-nebari"                         # optional subdirectory

  auth:                                              # NIC's write credentials
    ssh_key_env: GIT_SSH_PRIVATE_KEY                 # name of env var holding the PEM-encoded key
    # OR for HTTPS:
    # token_env: GIT_TOKEN

  # Optional: separate read-only credentials for ArgoCD (falls back to `auth` when unset)
  # argocd_auth:
  #   ssh_key_env: ARGOCD_SSH_KEY
```

Notes:

- `file://` URLs are valid. Combined with `InfraSettings.SupportsLocalGitOps = true` (currently only the local provider), this enables a zero-credential GitOps workflow for development.
- When `git_repository:` is omitted on a provider that supports local GitOps, NIC auto-creates `/tmp/nebari-gitops-<project_name>` and points ArgoCD at it.
- When `git_repository:` is omitted on a provider that does **not** support local GitOps (e.g., AWS), the deploy continues but the GitOps bootstrap is skipped.
- The CLI scrubs the `auth:` and `argocd_auth:` blocks from any copy of the config it writes into the repo (`scrubSensitiveFields` in `cmd/nic/deploy.go`).

---

## 6. Environment Variables

Loaded by `godotenv` from `.env` (gitignored) at startup. Used for credentials and runtime options.

| Variable | Used by | Purpose |
|----------|---------|---------|
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_REGION` | AWS provider | Standard AWS SDK credentials |
| `HCLOUD_TOKEN` | Hetzner provider | Hetzner Cloud API token |
| `CLOUDFLARE_API_TOKEN` | Cloudflare DNS | Zone:Read + DNS:Edit on the configured zone |
| `GIT_SSH_PRIVATE_KEY` (or whatever you point `git_repository.auth.ssh_key_env` at) | `pkg/git` | SSH private key in PEM form |
| `GIT_TOKEN` (or whatever you point `git_repository.auth.token_env` at) | `pkg/git` | Personal access token for HTTPS git URLs |
| `KUBECONFIG` | `existing` provider, `nic kubeconfig` | Kubeconfig path (used when `cluster.existing.kubeconfig` is empty) |
| `OTEL_EXPORTER` | `pkg/telemetry` | `console` (default), `otlp`, `both`, `none` |
| `OTEL_ENDPOINT` | `pkg/telemetry` | OTLP endpoint (default: `localhost:4317`) |

`.env.example` in the repo root lists the variables NIC looks at; copy to `.env` and fill in the values you need.
