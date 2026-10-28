# Deployed image vulnerability scans

The [Deployed image vulnerabilities workflow](../../.github/workflows/deployed-images.yml)
scans the container images NIC deploys every day at 04:23 UTC, on manual dispatch,
and on pull requests changing deployment code, templates, values, dependencies,
or the inventory/scanner. This complements the Go dependency vulnerability check
for the NIC binary. Software packs are outside this inventory.

## Targets and reporting

The release workflow generates `images.json` from the checkout being released,
before GoReleaser runs. It records image manifest digests for Linux AMD64 and
ARM64, source provenance, coverage limits and the OCI digests of rendered charts.
GoReleaser attaches the asset and includes it in `checksums.txt` before the
existing cosign signature is created. Build provenance covers both the checksum
file and `images.json` directly. The intermediate asset lives outside `dist/`
because GoReleaser's `--clean` removes that directory.
Ordinary snapshot and reproducibility builds omit this release-only asset and
need no image-registry access.

Scheduled runs freeze an inventory from `main`, then download the latest stable
release's attached `images.json`, verify its SHA-256 against `checksums.txt`, and
scan the union once per digest. They retain each image's NIC version and source.
They never re-render old release tags or re-resolve the release's image tags:
mutable upstream charts and image tags must not rewrite the release inventory.

Older releases, including `v0.16.0` when this was introduced, lack the asset.
They are explicitly recorded as `asset-not-supported` in the artifact and
coverage notes; until a release includes the asset, the scheduled scan covers
`main` only. A present asset that cannot be downloaded, validated or scanned
fails the run; it does not silently fall back to `main`. Only the latest stable release is covered,
not every historical release. PR runs render their own checkout and do not query
or scan a release. Manual dispatch also includes the latest release; publishing
SARIF is restricted to dispatches on `main`.

Completed runs produce an image list, source provenance, resolved digests, Trivy JSON,
SARIF and a job summary. Vulnerabilities are **report-only**, including on PRs:
an upstream CVE in an unchanged image does not fail a PR. Inventory, registry,
database or scanner failures do fail the job. Incomplete scans cannot publish
replacement SARIF or resolve issues as though missing findings were fixed. A
partial scan can open/reopen issues for positive evidence, retaining earlier
issue evidence and leaving every unresolved issue open.

Successful scheduled/main manual runs upload SARIF to the Security tab using a
stable `nic-deployed-images` category. PRs receive artifacts and summaries without
write permissions. SARIF locations point to NIC deployment sources. Results and
stable fingerprints group by vulnerability/package, listing every affected image
digest and platform. This avoids silently exceeding GitHub's 5,000 displayed
results per run; an oversized report fails explicitly. Raw Trivy JSON retains
the individual package records.

Scheduled runs also reconcile issues labeled **`area: security`** with the
built-in GitHub token (`issues: write` in a separate job). One issue per
**CVE/GHSA plus affected package** lists all affected image digests, platforms,
versions, fixes and NIC targets. This keeps an image
tag update or another affected image from opening a duplicate issue. The first
run can still open many issues for the existing backlog.
Trivy's vendor advisory ID is used when it provides no individual CVE record
(for example SUSE advisories). These findings are retained without guessing CVE
identities or severities from advisory prose. Writes are paced below GitHub's
hourly content creation limit; the initial backlog takes longer than later
runs, where unchanged issues produce no writes.

The reporter updates changed findings, leaves unchanged issues alone, reopens a
recurrence, and closes only after a complete successful scan of all previously
covered targets. Resolution reasons remain distinct:

- **Fixed:** the package is still shipped by an affected image repository, but
  this HIGH/CRITICAL finding is absent from the complete scan.
- **Suppressed:** the finding remains in the raw report and has a documented,
  unexpired exception. The issue records the reason and expiry.
- **No longer shipped:** the affected image/package is absent from the inventory.

Fixing `main` does not close an issue still affecting the scanned release. Loss
of release coverage preserves earlier issue evidence. Human-created issues are
untouched; managed issues are identified by their bot author, label and hidden
`nic-image-scan:v2` marker. Keep these markers and the label intact. Critical
findings without an upstream fix have a separate summary section and an explicit
issue warning. Issue reconciliation runs
only on the schedule, not PRs or manual dispatches.

## How the inventory is built

`scripts/scan-images.py inventory` runs opt-in Go tests in the installer packages,
using `internal/imageinventory` to collect separate inventory shards. Ordinary
unit tests do not download charts or images. Generation needs network access,
Go, Git and the pinned `crane` executable; it needs no cluster, cloud credentials,
Docker daemon or installed Helm CLI.

| Deployment source | Inventory method |
| --- | --- |
| Argo CD applications, values and raw manifests | Call NIC's `WriteAllToGit`, discover rendered application sources, apply ordered value files, and render pinned charts through the Helm SDK. Git-hosted charts build dependencies from `Chart.lock`. |
| Direct Argo CD, Longhorn and AWS chart installs | Call the same version constants, default config and Helm values builders as the installers. Include prerequisites, install hooks, init containers and controller image flags/configuration. |
| CloudNativePG Postgres | Honor the Cluster's `imageName`. Otherwise resolve `POSTGRES_IMAGE_NAME` from the rendered operator config, or `DefaultImageName` from the operator source at the effective chart/operator version. Unknown image catalogs or indirect environment values require explicit inventory support. |
| Envoy Gateway data plane | Resolve `DefaultEnvoyProxyImage` from the pinned gateway source, applying NIC's EnvoyProxy image override if present. Chart-rendered controller, shutdown and ratelimit images are included too. |
| NVIDIA GPU operator | Read enabled ClusterPolicy repository/image/version combinations, using NIC's driver/toolkit settings. This is a conservative union of declared hardware-dependent components. |
| Hetzner | Follow the pinned hetzner-k3s source to its pinned cloud controller, CSI, autoscaler and system-upgrade-controller manifests. |
| Local Kind | Read the node default from NIC's exact Kind Go dependency, plus its HA load-balancer default. Export both AMD64 and ARM64 node filesystems without executing containers and read their containerd metadata to discover preloaded Kubernetes, networking and storage images. |
| AWS/Azure OpenTofu | Audit the pinned modules and record their managed add-ons as unresolved where cloud services choose the image/version. Module-source changes require a new coverage review. |

Rendering uses Kubernetes 1.34 capabilities and enables optional Longhorn backup,
trust-manager, autoscaler and GPU operator paths. The AWS autoscaler covers both
the chart default and NIC's representative Kubernetes 1.34 override. A deployment
with another Kubernetes minor or custom chart/image configuration can differ.
Helm test-only hooks and CRD schema examples are excluded. Unsupported application
sources, Helm precedence layers, Kustomizations and unresolved image references
fail explicitly instead of scanning a guessed image.

`internal/imageinventory/sources.json` records reviewed deployment sources. The
ordinary `TestDeploymentSourceCoverage` test discovers Go/OpenTofu files capable
of adding images, charts, embedded manifests, modules or external installers and
checks their hashes, plus the Kind dependency version. It catches a new source
and a new chart added to an existing installer. Rendered Argo CD chart changes
are discovered automatically. This is a maintenance guard, not proof that an
arbitrary future external controller has no additional image defaults.

When this guard fails, inspect the changed installer/module and extend its
inventory test or resolver as needed, run generation, review the resulting list,
then update only its reviewed `sources.json` entry using the digest printed by
the test. Do not regenerate the lock wholesale to make the check pass.

OCI charts are pulled once through Helm's registry client, and their exact
manifest digest is recorded alongside the bytes rendered. Compare these digests
with `internal/imageinventory/chart-digests.json` and the latest release asset.
A changed digest under the same version produces a **PINNED CHART CHANGED**
workflow annotation and a prominent summary entry. Review upstream contents
before accepting a new baseline. This detects the Nebari catalog republication
problem. A new OCI chart version must have its reviewed digest added to the
baseline before generation succeeds. The upstream republication is tracked in
[helm-repository#119](https://github.com/nebari-dev/helm-repository/issues/119);
changing NIC deployment pins is separately tracked by
[#468](https://github.com/nebari-dev/nebari-infrastructure-core/issues/468).

## Noise, digests and database downloads

Inventory generation resolves each tag to an index digest once, then records
its **linux/amd64 and linux/arm64** child manifests. For single-manifest images
it checks the config architecture rather than assuming that a successful pull
means both platforms exist. Aliases with the same digest are combined across
targets, and the immutable digest is scanned once.

Reports include **every HIGH and CRITICAL vulnerability**, including unfixed,
will-not-fix and end-of-life findings. `--ignore-unfixed` is deliberately absent.
The default policy is report-only; local callers can opt into
`--fail-on-fixable`, which changes the exit status only for unsuppressed findings
with a fixed version. It never filters the report. Summary counts are affected
package records; issue counts are distinct CVE/package pairs. Deployment pins
remain unchanged.

The root `.trivyignore` is validated and applied after collecting Trivy evidence.
Raw Trivy JSON retains all findings and installed packages so issue resolution
can distinguish a suppression from a fix or removal. Every CVE/GHSA entry must
have its own preceding `# reason: ...` comment explaining the exception, ideally
linking evidence or a tracking issue, and a mandatory expiration date:

```text
# reason: Example only: link the evidence for an unreachable affected feature.
CVE-2026-1234 exp:2026-12-01
```

A header comment, missing/invalid expiry, duplicate ID or a reason reused across
multiple entries is rejected. Vendor advisory IDs reported by Trivy are accepted
too. An exception expires at 00:00 UTC on its date;
expired findings reappear and reopen their issues. There are no suppressions
initially. Native `.trivyignore` entries apply across images;
consider that scope before suppressing a shared dependency.

CI caches Trivy's vulnerability and Java databases between runs and tries the
Google registry mirror before GHCR. Trivy still checks database freshness. The
cache key includes the pinned Trivy version and a run ID, with a shared restore
prefix. Image layers are not persisted in the Actions cache. The Trivy binary is
version/checksum pinned and all workflow actions use full commit SHAs.

## Coverage limits

The inventory and job summary retain these limits for triage:

- AWS's managed CoreDNS, kube-proxy, VPC CNI, pod identity and EBS/EFS CSI images
  are selected by EKS rather than pinned by NIC's module. AKS similarly selects
  system, CSI/CNI and optional managed Karpenter images. Resolve these against an
  actual cluster; chart rendering cannot establish their versions.
- Hetzner's k3s version is supplied/resolved at deployment time. Its Kubernetes,
  CoreDNS, metrics-server, local-path storage, optional embedded registry and
  upgrade images have no single NIC-pinned version. The fixed add-ons above are
  covered. NIC disables Traefik/ServiceLB and uses flannel by default.
- Existing clusters, custom node images, arbitrary user chart/value overrides,
  and software packs need their own inventories. The GCP provider is a stub.
- Each image has an entry for both supported Linux architectures. A registry
  manifest proving an architecture is not published is recorded as
  `not-published`, with a workflow warning and prominent coverage summary.
  For example, NVIDIA's optional sandbox device plugin v0.0.3 has no ARM64
  manifest. This is an unavailable deployment variant, not a failed pull that
  can be treated as clean. All published variants must resolve and scan
  successfully before issues can close; any registry/scan error prevents closure.
- Mutable upstream tags such as `latest` are frozen at release/scan time. A
  cluster deployed later can receive different content until #468 pins inputs;
  the release asset records what was published, not an attestation of a live
  cluster. Optional GPU components may not all launch on a single machine.

Current CloudNativePG and Envoy defaults are resolved, not counted as gaps.
No live cluster or cloud deployment is needed or exercised by this workflow.

## Run locally

Install Go from `go.mod`, Git, `crane` v0.22.1 and Trivy v0.76.1 (the workflow
contains the verified Linux binary checksum). From the repository root:

```bash
go install github.com/google/go-containerregistry/cmd/crane@v0.22.1
python3 scripts/scan-images.py inventory --target main --include-latest-release \
  --output /tmp/nic-images/inventory.json
python3 scripts/scan-images.py scan --inventory /tmp/nic-images/inventory.json \
  --output /tmp/nic-images/results
go test ./internal/imageinventory
python3 -m unittest discover -s scripts -p 'test_*image*.py'
```

Omit `--include-latest-release` to inspect only the working checkout. Use
`--target release --output .release-assets/images.json` to exercise the release
asset generator. An optional
read-only `GH_TOKEN` raises the public GitHub API rate limit for release lookup.
The inventory JSON and adjacent text list include uncommitted local overrides,
although the recorded revision is the checkout's HEAD commit.

`report-image-issues.py --report .../findings.json --repository OWNER/REPO`
reads existing issues and prints proposed operation counts without writing.
It requires `GH_TOKEN`; only `--apply` enables writes. The local regression tests
exercise issue creation, stable updates, reopening, release retention and safe
resolution without GitHub writes.

Users can download `images.json` and `checksums.txt` for their release, follow
[the release verification guide](verifying-releases.md) to check the signature
and provenance, then pass that `images.json` directly to the `scan` command.
No old checkout, chart download or image-tag lookup is needed. The asset's
`resolved` map is keyed by digest, with pull references and platform/target
provenance; `targets.release.image_platforms` maps every original image and
architecture to its digest or explicit `not-published` status. `charts` maps
OCI chart version references to the manifest digests actually rendered.
