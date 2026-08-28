# Configuration Reference

This is the authoritative reference for `nebari-config.yaml`.

## Table of Contents

1. [Top-Level Schema](#1-top-level-schema)
2. [Cluster Providers](#2-cluster-providers)
   1. [`cluster.aws`](#21-clusteraws-amazon-eks)
   2. [`cluster.hetzner`](#22-clusterhetzner-hetzner-cloud-k3s)
   3. [`cluster.local`](#23-clusterlocal-kind-for-development)
   4. [`cluster.existing`](#24-clusterexisting-adopt-a-pre-provisioned-cluster)
   5. [`cluster.azure`](#25-clusterazure-aks)
   6. [`cluster.gcp`](#26-clustergcp-stub)
3. [DNS Providers](#3-dns-providers)
4. [Certificate](#4-certificate)
5. [Trust Bundle](#5-trust-bundle)
6. [Backups](#6-backups)
7. [GitOps Repository](#7-gitops-repository)
8. [Environment Variables](#8-environment-variables)

---

## 1. Top-Level Schema

Defined by `NebariConfig` in `pkg/config/config.go`:

> **Reserved token.** `CHANGEME` is reserved as the "unfilled value" marker.
> `nic validate` and `nic deploy` reject a config where it appears in any scalar
> value or mapping key, matched case-sensitively as a substring. No field below
> may document a default that contains it. See
> [config placeholders](../../operations/config-placeholders.md).

```yaml
project_name: my-nebari        # required, [a-zA-Z0-9][a-zA-Z0-9_-]*
domain: nebari.example.com     # optional, but needed for routable services

cluster:                       # required, exactly one provider
  <provider-name>:
    ...

dns:                           # optional, exactly one provider
  <provider-name>:
    ...

repository:                    # required, exactly one provider
  <provider-name>:
    ...

certificate:                   # optional, defaults to selfsigned
  type: ...

trust_bundle:                  # optional, enterprise CA bundle
  path: ...

backups:                       # optional, off-cluster Longhorn backups
  longhorn:
    ...
```

Anti-pattern: there is no top-level `provider:`, `version:`, `name:`, `kubernetes:`, `node_pools:`, `tls:`, `foundational_software:`, `images:`, or `features:` field. If older documentation shows those, it is out of date.

| Field | Type | Required | Source |
|-------|------|----------|--------|
| `project_name` | string | ✅ | `NebariConfig.ProjectName` |
| `domain` | string | optional | `NebariConfig.Domain` |
| `cluster` | map | ✅ | `NebariConfig.Cluster` (`ClusterConfig`) |
| `dns` | map | optional | `NebariConfig.DNS` (`DNSConfig`) |
| `repository` | map | ✅ | `NebariConfig.Repository` (`RepositoryConfig`) |
| `certificate` | object | optional | `NebariConfig.Certificate` (`CertificateConfig`) |
| `trust_bundle` | object | optional | `NebariConfig.TrustBundle` (`TrustBundleConfig`) |
| `backups` | object | optional | `NebariConfig.Backups` (`BackupsConfig`) |

---

## 2. Cluster Providers

`cluster:` takes exactly one key, the provider name. The shape of the nested object is provider-specific.

Valid provider names (registered in `pkg/nic/registry.go`'s `defaultRegistry`): `aws`, `hetzner`, `local`, `existing`, `gcp`, `azure`.

### 2.1 `cluster.aws` (Amazon EKS)

Defined by `Config` in `pkg/providers/cluster/aws/config.go`.

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

    # Optional: override the auto-derived OpenTofu state bucket name.
    # Default is derived from AWS account ID + region + project name.
    # state_bucket: my-existing-tofu-state-bucket

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

    # Optional: IAM Roles for Service Accounts (EKS OIDC provider).
    # Unset means the upstream module default (true). Set false when the
    # cluster relies exclusively on EKS Pod Identity, or when the VPC cannot
    # resolve oidc.eks.<region>.amazonaws.com.
    # enable_irsa: false

    # Optional: scheme for provisioned load balancers.
    # "internet-facing" (default) | "internal"
    # load_balancer_scheme: internal

    node_groups:                                # map keyed by node-group name
      user:
        instance: m7i.xlarge
        min_nodes: 1
        max_nodes: 5
        # ami_type: AL2023_x86_64_STANDARD     # defaults to AL2023 STANDARD
        # gpu: true                            # AL2023_x86_64_NVIDIA AMI + auto nvidia.com/gpu taint
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
    #   chart_version: "3.4.3"
    #   destroy_timeout: 5m

    # Optional: Kubernetes Cluster Autoscaler (default: enabled)
    # cluster_autoscaler:
    #   enabled: true
    #   chart_version: "9.57.0"
    #   image_tag: v1.34.0                     # default derives from the cluster's k8s minor

    # Optional: EFS shared storage
    efs:
      enabled: true
      performance_mode: generalPurpose          # generalPurpose | maxIO
      throughput_mode: bursting                 # bursting | provisioned | elastic
      encrypted: true
      # provisioned_throughput_mibps: 100        # required if throughput_mode is provisioned
      # kms_key_arn: arn:aws:kms:...
      # storage_class_name: efs-sc

    # Optional: Longhorn distributed storage (default: enabled when the block is omitted)
    # longhorn:
    #   enabled: true
    #   replica_count: 2
    #   dedicated_nodes: false
    #   node_selector: { node.longhorn.io/storage: "true" }
```

**GPU node groups:** setting `gpu: true` selects the `AL2023_x86_64_NVIDIA` AMI and makes NIC automatically apply the taint `nvidia.com/gpu=true:NO_SCHEDULE`, so only pods that tolerate it schedule onto GPU nodes. The NVIDIA GPU Operator does not taint nodes itself; it only tolerates this taint on its own operands. To use a different value or effect, set an explicit `nvidia.com/gpu` taint in the node group's `taints` and NIC leaves it untouched.

**Longhorn default:** on AWS (and Hetzner) an omitted `longhorn:` block means *enabled*. The shared `longhorn.Config` in `pkg/storage/longhorn/config.go` defaults to disabled-when-nil, and the AWS provider inverts that in `Config.LonghornEnabled()`. See [`cluster.existing`](#24-clusterexisting-adopt-a-pre-provisioned-cluster) for the opt-in case.

State backend: S3 with `use_lockfile = true`, bucket auto-created per [§5.2 of State Management](../architecture/05-state-management.md). No DynamoDB.

### 2.2 `cluster.hetzner` (Hetzner Cloud k3s)

Backed by the `hetzner-k3s` binary - **not** OpenTofu. Defined by `Config` in `pkg/providers/cluster/hetzner/config.go`.

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
        # location: fsn1                        # override the top-level location; workers only
        # autoscaling:
        #   enabled: true
        #   min_instances: 2
        #   max_instances: 6

    # Optional: Longhorn distributed storage.
    # Default: ENABLED when the block is omitted. Hetzner's hcloud-volumes CSI is
    # RWO-only, so charts needing RWX (e.g. jupyterhub shared group dirs) depend
    # on Longhorn. Set `enabled: false` to opt out.
    # longhorn:
    #   enabled: true
    #   replica_count: 2
    #   dedicated_nodes: false
    #   node_selector: { node.longhorn.io/storage: "true" }

    # Optional: provide your own SSH keys (else NIC generates ed25519 keys under its cache dir)
    # ssh:
    #   public_key_path:  ~/.ssh/id_ed25519.pub
    #   private_key_path: ~/.ssh/id_ed25519

    # Optional: restrict SSH and API CIDRs (defaults to 0.0.0.0/0; NIC warns at validate time)
    # network:
    #   ssh_allowed_cidrs: [203.0.113.0/24]
    #   api_allowed_cidrs: [203.0.113.0/24]
```

The Hetzner provider requires the **`HETZNER_TOKEN`** environment variable (`pkg/providers/cluster/hetzner/provider.go`). `HCLOUD_TOKEN` is *not* the user-facing name: NIC only exports it into the `hetzner-k3s` subprocess so the token never lands on disk.

### 2.3 `cluster.local` (Kind for development)

Defined by `Config` in `pkg/providers/cluster/local/config.go`. The local provider's `Deploy` creates the Kind cluster (reusing it if one already exists), then runs the bootstrap (ArgoCD + foundational apps) against it; `Destroy` deletes the cluster. Deploy with `nic deploy -f examples/local-config.yaml`.

```yaml
cluster:
  local:
    # Optional: kind cluster tuning. Omit the whole block for defaults.
    # kind:
    #   node_image: kindest/node:v1.35.0        # default: bundled kind's default image
    #   extra_mounts:
    #     - host_path: /absolute/host/path
    #       container_path: /absolute/node/path
    #       read_only: true

    # Optional: HTTPS port for the Gateway listener (default: 443).
    # Override e.g. to 8443 if 443 is in use or requires root.
    # https_port: 8443

    # Optional: MetalLB address pool. MetalLB is ALWAYS enabled on local
    # (kind has no built-in LoadBalancer, so there is no `enabled` field).
    # When unset, NIC derives the pool from the kind Docker network at deploy time.
    # metallb:
    #   address_pool: 172.18.255.100-172.18.255.110

    # Optional: per-node-group selectors used by software packs
    # node_selectors:
    #   general:
    #     kubernetes.io/os: linux
    #   user:
    #     kubernetes.io/os: linux
```

The kube context name is derived from `project_name` (`kindContextName` in `pkg/providers/cluster/local/provider.go`); there is no `kube_context:` field. There is likewise no `storage_class:` field on the local provider. `local.Config` carries an inline `AdditionalFields map[string]any`, so unrecognized keys parse **silently** rather than erroring - do not assume a key works because `nic validate` passes.

The local provider sets `InfraSettings.SupportsLocalGitOps = true`, which is what permits the `repository.local` provider (a GitOps repo in a host directory, auto-created when no explicit path is given). See [§7](#7-gitops-repository) for the path.

### 2.4 `cluster.existing` (adopt a pre-provisioned cluster)

No provisioning happens; NIC just runs the bootstrap against whatever cluster the kubeconfig points at. Defined by `Config` in `pkg/providers/cluster/existing/config.go`.

```yaml
cluster:
  existing:
    # Path to the kubeconfig file. May be absolute or relative; tilde is NOT expanded.
    # When empty: falls back to $KUBECONFIG env var, then $HOME/.kube/config.
    kubeconfig: path/to/kubeconfig

    # Required: context name within that kubeconfig.
    context: "arn:aws:eks:us-west-2:123456789012:cluster/my-nebari"

    # Optional: default StorageClass for foundational PVCs.
    # Default: "standard", or "longhorn" when the longhorn block below is enabled
    # and this field is left unset.
    storage_class: gp2

    # Optional: Longhorn distributed storage.
    # OPT-IN here (unlike aws/hetzner): an omitted block means "do not install".
    # Use on bare-metal / hetzner-k3s clusters with no managed RWX StorageClass.
    # longhorn:
    #   enabled: true
    #   replica_count: 2
    #   dedicated_nodes: false

    # Optional: annotations applied to the Envoy Gateway LoadBalancer Service
    # load_balancer_annotations:
    #   load-balancer.hetzner.cloud/location: ash
```

### 2.5 `cluster.azure` (AKS)

Implemented: provisions AKS via OpenTofu, analogous to the AWS provider. Defined by `Config` in `pkg/providers/cluster/azure/config.go`; see [`examples/azure-config.yaml`](../../../examples/azure-config.yaml) for a deployable config.

```yaml
cluster:
  azure:
    region: eastus                             # required

    # Optional: omit to let NIC create "<project_name>-rg".
    # resource_group_name: my-rg
    # create_resource_group: false             # tri-state; false requires resource_group_name

    kubernetes_version: "1.34"                 # optional; "1.34" or "1.34.0"
    sku_tier: Free                             # optional; passed through to the module (default "Free")
    private_cluster_enabled: false

    # Restrict API server access to specific CIDRs. [] means open.
    # authorized_ip_ranges:
    #   - 203.0.113.0/24

    network:
      vnet_cidr_block: "10.0.0.0/16"
      node_subnet_cidr_block: "10.0.0.0/22"
      pod_cidr: "10.244.0.0/16"
      service_cidr: "10.0.16.0/22"
      dns_service_ip: "10.0.16.10"
      # dataplane: cilium                      # "azure" (default) | "cilium"
      # BYO networking (both required together):
      # existing_vnet_id: /subscriptions/.../virtualNetworks/foo
      # existing_node_subnet_id: /subscriptions/.../subnets/foo

    # Node Auto Provisioning (Karpenter): "Manual" (default) | "Auto".
    # "Auto" requires network.dataplane: cilium.
    # node_provisioning_mode: Auto

    node_groups:                                # required; at most one may set mode: System
      system:
        instance: Standard_D4_v3
        min_nodes: 1
        max_nodes: 3
        mode: System                            # "System" | "User" (default "User")
      user:
        instance: Standard_D8_v3
        min_nodes: 1
        max_nodes: 5
        # os_disk_size_gb: 128
        # labels: { workload: user }
        # taints: ["dedicated=gpu:NoSchedule"]  # "key=value:Effect" strings, not objects
        # zones: ["1", "2"]

    tags:
      Environment: development
```

Requires `AZURE_SUBSCRIPTION_ID`, which NIC maps to `ARM_SUBSCRIPTION_ID` for the child OpenTofu process. Remaining auth is resolved by `azidentity.DefaultAzureCredential` (env vars, workload identity, managed identity, then `az login`).

The provider consumes the upstream `nebari-dev/aks-cluster/azurerm` registry module. It is currently pinned to a git branch ref while the Longhorn backup-container support is unreleased (see the `TODO(#431)` in `pkg/providers/cluster/azure/templates/main.tf`); it reverts to the registry source + pinned version once a release lands. State lives in an `azurerm` backend on a bootstrapped storage account.

### 2.6 `cluster.gcp` (stub)

**GCP is a registered stub**: `Deploy`/`Destroy` emit a "(stub)" status message and return `nil`, and `GetKubeconfig` returns "not yet implemented". The struct fields exist for forward compatibility. See [`examples/gcp-config.yaml`](../../../examples/gcp-config.yaml) (schema only; not deployable today).

`gcp.Config` accepts: `project`, `region`, `kubernetes_version`, `availability_zones`, `release_channel`, `node_groups` (map), `tags`, `networking_mode`, `network`, `subnetwork`, `ip_allocation_policy`, `master_authorized_networks_config`, `private_cluster_config`.

Two shape differences from AWS/Azure worth noting:

- `tags` is a **list of strings** (GCP network tags), not a `map[string]string`.
- Node groups take `instance`, `min_nodes`, `max_nodes`, `labels`, `preemptible`, `guest_accelerators` (`{name, count}`), and `taints` whose `effect` uses the GCP/Kubernetes spelling (`NoSchedule`, `PreferNoSchedule`, `NoExecute`), not the AWS EKS spelling (`NO_SCHEDULE`).

---

## 3. DNS Providers

`dns:` takes exactly one key. The shape is provider-specific.

Valid provider names: `cloudflare` (the only DNS provider implemented today).

### 3.1 `dns.cloudflare`

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

Future DNS providers (Route53, Azure DNS, Google Cloud DNS) will follow the same shape and the same `Provider` interface defined in `pkg/providers/dns/provider.go`.

---

## 4. Certificate

Defined by `CertificateConfig` in `pkg/config/config.go`. Three types: `selfsigned` (default), `letsencrypt`, and `existing`.

```yaml
certificate:
  type: letsencrypt                            # "selfsigned" (default) | "letsencrypt" | "existing"
  acme:                                        # required when type: letsencrypt
    email: admin@example.com
    # server: https://acme-staging-v02.api.letsencrypt.org/directory  # use staging for testing
```

When omitted, NIC behaves as if `type: selfsigned` was set. `selfsigned` is appropriate for local clusters, internal environments, and `existing` clusters where cert lifecycle is handled out-of-band. `letsencrypt` requires a publicly-routable `domain` (and typically a DNS provider).

### 4.1 `type: existing` (bring your own certificate)

Supply the certificate yourself instead of having cert-manager mint one. Exactly **one** of `existing_secret`, `files`, or `env` must be set, and `acme:` may not be combined with it. See [`examples/custom-tls-config.yaml`](../../../examples/custom-tls-config.yaml) and `docs/custom-tls-certificate.md`.

```yaml
certificate:
  type: existing

  # Optional: name of the TLS secret NIC creates for the files/env sources.
  # Default: "nebari-gateway-tls". Cannot be combined with existing_secret
  # (which the gateway references by existing_secret.name directly).
  # secret_name: nebari-gateway-tls

  # 1. Reference a kubernetes.io/tls secret you already created.
  existing_secret:
    name: my-gateway-tls
    # namespace: my-tls-namespace              # default: envoy-gateway-system.
                                               # Cross-namespace renders a Gateway-API ReferenceGrant.

  # 2. OR read PEM material from files on disk. NIC creates the secret directly
  #    in envoy-gateway-system; the cert/key never enter the GitOps repo.
  # files:
  #   cert_file: /path/to/tls.crt
  #   key_file:  /path/to/tls.key

  # 3. OR read raw (non-base64) PEM material from environment variables.
  # env:
  #   cert_env: NEBARI_TLS_CERT
  #   key_env:  NEBARI_TLS_KEY
```

The certificate must cover the apex domain plus the `keycloak.` and `argocd.` subdomains (a wildcard plus the apex also works). NIC warns, but does not fail, on a missing recommended SAN.

---

## 5. Trust Bundle

Defined by `TrustBundleConfig` in `pkg/config/trust_bundle.go`. Required when egress is TLS-inspected by a corporate proxy. The bundle is propagated both to worker-node OS trust stores (via the cluster provider) and into the cluster via trust-manager.

```yaml
trust_bundle:                                  # exactly one of path / inline
  path: /etc/ssl/corp-ca.pem                   # PEM file on the operator's machine
  # inline: |
  #   -----BEGIN CERTIFICATE-----
  #   ...
  #   -----END CERTIFICATE-----
```

Rules:

- `path` and `inline` are mutually exclusive.
- The material must contain at least one `-----BEGIN CERTIFICATE-----` block.
- A `PRIVATE KEY` block is rejected outright. The resolved bundle is written to OpenTofu state and projected cluster-wide via the GitOps repo, so a stray cert+key file must never be distributed.
- `Validate()` never touches disk, so a `path:`-based bundle is only read (and PEM-checked) at deploy/destroy time. Config linting in CI will not catch a missing file.

---

## 6. Backups

Defined by `BackupsConfig` in `pkg/config/backups.go`. Off-cluster backups via Longhorn's native S3 / azblob backup target. Opt-in: an omitted `backups:` block means no backups. See [`examples/longhorn-backups-config.yaml`](../../../examples/longhorn-backups-config.yaml).

```yaml
backups:
  longhorn:
    enabled: true                              # a present block defaults to true
    # allow_recurring_job_while_volume_detached: true   # default: true

    s3:                                        # exactly one of s3 / azure
      bucket: my-nebari-backups                # required
      region: us-east-1                        # required
      prefix: clusterA/                        # optional
      create_bucket: true                      # aws/azure providers only; illegal with endpoint
      retain_on_destroy: true                  # default: true
      # endpoint: https://fsn1.your-objectstorage.com   # S3-compatible target
      # virtual_hosted_style: false
      access_key_id_env: LONGHORN_S3_ACCESS_KEY_ID
      secret_access_key_env: LONGHORN_S3_SECRET_ACCESS_KEY
      # ca_cert:                               # PEM CA bundle for a private endpoint
      #   kind: secret                         # "secret" | "configmap"
      #   name: my-ca
      #   namespace: longhorn-system
      #   key: ca.crt

    # azure:
    #   container: nebari-backups              # required
    #   storage_account: mystorageaccount      # required
    #   prefix: clusterA/
    #   create_container: true                 # azure provider only; illegal with endpoint
    #   retain_on_destroy: true                # default: true
    #   # endpoint: https://...
    #   account_name_env: LONGHORN_AZBLOB_ACCOUNT_NAME
    #   account_key_env:  LONGHORN_AZBLOB_ACCOUNT_KEY

    schedules:                                 # both are required when enabled
      snapshot:
        cron: "0 * * * *"                      # 5-field cron
        retain: 24                             # must be > 0
        concurrency: 2                         # must be > 0
      backup:
        cron: "0 2 * * *"
        retain: 7
        concurrency: 2
```

Validation rules that will reject a config at `nic validate`:

- Exactly one of `s3` / `azure`.
- Both `schedules.snapshot` and `schedules.backup` need a valid 5-field cron with `retain > 0` and `concurrency > 0`.
- **S3 credentials:** set both `access_key_id_env` and `secret_access_key_env`, or neither. Omitting both selects keyless IAM-role auth, which is only valid on the `aws` provider with no custom `endpoint`. In that case NIC omits the AWS keys from the Longhorn credential Secret and provisions an EKS Pod Identity association for Longhorn's service account. Any other combination (non-`aws` provider, or an S3-compatible endpoint) requires static keys.
- `create_bucket` requires a provider with a Terraform module (`aws`, `azure`) and cannot be combined with `endpoint`. `create_container` requires the `azure` provider and likewise rejects `endpoint`.

What NIC provisions: the `longhorn-backup-credentials` Secret in `longhorn-system`, two RecurringJobs (`default-hourly-snapshot`, `default-daily-backup`), the BackupTarget and default, and the `allow-recurring-job-while-volume-detached` cluster Setting. That setting defaults to `true` here rather than Longhorn's stock `false`, because JupyterHub user PVCs detach when servers idle out and would otherwise be skipped silently at the cron tick.

---

## 7. GitOps Repository

The `repository:` block follows the same provider pattern as `cluster:` and `dns:`: exactly one provider, keyed by name (`RepositoryConfig` in `pkg/config/config.go`, backed by `pkg/providers/repository`). Two providers exist: `existing` (a remote repo NIC clones and pushes to) and `local` (a directory on the host, for dev clusters).

```yaml
repository:
  existing:
    url: "git@github.com:my-org/my-gitops-repo.git"  # SSH or HTTPS
    branch: main                                      # default: "main"
    path: "clusters/my-nebari"                        # optional subdirectory

    auth:                                             # NIC's write credentials
      ssh:
        env: GIT_SSH_PRIVATE_KEY                      # name of env var holding the PEM-encoded key
      # OR for HTTPS:
      # token:
      #   env: GIT_TOKEN
      # insecure_skip_host_key_verification: false    # SSH host-key check; leave false outside dev

    # Optional: separate read-only credentials for ArgoCD (falls back to `auth` when unset)
    # argocd_auth:
    #   token:
    #     env: ARGOCD_GIT_TOKEN

# OR, for development clusters:
# repository:
#   local:
#     path: /abs/path/to/gitops   # optional; defaults to ~/.nic/gitops/<project_name>
#     branch: main                # default: "main"
```

Notes:

- The `repository:` block is required on every provider; `nic validate` rejects a config without one.
- The `local` provider is only valid on a cluster provider with `InfraSettings.SupportsLocalGitOps = true` (currently only the local provider); it enables a zero-credential GitOps workflow for development. Deploy fails with an incompatibility error on any other cluster provider.
- When `repository.local.path` is omitted, NIC auto-creates **`~/.nic/gitops/<project_name>`** and points ArgoCD at it (`config.DefaultLocalRepositoryPath`). It falls back to `$TMPDIR/nebari-gitops-<project_name>` only when the home directory cannot be resolved. The home-directory location is deliberate: it is a host path kind and Docker Desktop can mount reliably.
- On the local (kind) provider, NIC auto-mounts that default path into the node container. A **custom** `repository.local.path` needs a matching `cluster.local.kind.extra_mounts` entry with identical `host_path` and `container_path`, or the in-cluster ArgoCD repo-server cannot see it.
- The copy of the config NIC commits into the repo (`nic-config.yaml`) carries only env-var names for credentials, never resolved secrets; a `path:`-based trust bundle is rewritten to its resolved inline form (`committedConfig` in `pkg/nic/deploy.go`).

---

## 8. Environment Variables

Loaded by `godotenv` from `.env` (gitignored) at startup. Used for credentials and runtime options.

| Variable | Used by | Purpose |
|----------|---------|---------|
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_REGION` | AWS provider | Standard AWS SDK credentials |
| `HETZNER_TOKEN` | Hetzner provider | Hetzner Cloud API token (exported to the `hetzner-k3s` subprocess as `HCLOUD_TOKEN`) |
| `AZURE_SUBSCRIPTION_ID` | Azure provider | Required; mapped to `ARM_SUBSCRIPTION_ID` for the child OpenTofu process |
| `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_SECRET` | Azure provider | Optional service-principal auth. Otherwise `DefaultAzureCredential` falls through to workload identity, managed identity, then `az login` |
| `CLOUDFLARE_API_TOKEN` | Cloudflare DNS | Zone:Read + DNS:Edit on the configured zone |
| `GIT_SSH_PRIVATE_KEY` (or whatever you point `repository.existing.auth.ssh.env` at) | `pkg/providers/repository/existing` | SSH private key in PEM form |
| `GIT_TOKEN` (or whatever you point `repository.existing.auth.token.env` at) | `pkg/providers/repository/existing` | Personal access token for HTTPS git URLs |
| whatever you point `backups.longhorn.s3.access_key_id_env` / `secret_access_key_env` at | `pkg/storage/longhorn` | Backup target credentials. Omit both for keyless IAM-role auth on AWS |
| whatever you point `backups.longhorn.azure.account_name_env` / `account_key_env` at | `pkg/storage/longhorn` | azblob backup target credentials |
| whatever you point `certificate.env.cert_env` / `key_env` at | `pkg/argocd` | Raw (non-base64) PEM for `certificate.type: existing` |
| `KUBECONFIG` | `existing` provider, `nic kubeconfig` | Kubeconfig path (used when `cluster.existing.kubeconfig` is empty) |
| `NIC_CONFIG_PATH` | `cmd/nic` | Overrides the config file path when `-f` is not given |
| `NIC_TOFU_PATH` | `pkg/tofu` | Path to a pre-installed OpenTofu binary. Hard error if missing, not executable, or outside the supported version range. When unset, a compatible `tofu` on `PATH` is used, then download of the pinned version. See [Packaging and External Binaries](../../operations/packaging.md) |
| `HELM_DRIVER` | Helm installs | Helm storage driver override |
| `OTEL_EXPORTER` | `pkg/telemetry` | `none` (default), `console`, `otlp`, `both` |
| `OTEL_ENDPOINT` | `pkg/telemetry` | OTLP endpoint (default: `localhost:4317`) |

`.env.example` in the repo root is a starting point, not an exhaustive list: it currently seeds only `CLOUDFLARE_API_TOKEN`. Copy it to `.env` and add whatever the table above says your provider and feature set needs.
