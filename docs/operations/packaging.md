# Packaging and External Binaries

NIC shells out to [OpenTofu](https://opentofu.org/) for the providers that provision
infrastructure declaratively (AWS, Azure). By default it downloads its own pinned
OpenTofu binary on first use. This page covers how to make NIC use a pre-installed
binary instead, which matters for OS/conda packaging, CI, and air-gapped environments.

The Hetzner provider additionally uses the
[`hetzner-k3s`](https://github.com/vitobotta/hetzner-k3s) binary, resolved the same
way — see [hetzner-k3s](#hetzner-k3s-hetzner-provider) below.

## OpenTofu resolution order

When a command needs OpenTofu, NIC resolves the binary in this order:

1. **`NIC_TOFU_PATH`** — an explicit path to an OpenTofu binary (on Windows, the
   full path to `tofu.exe`). If set, it must point to an executable, compatible
   binary; anything else is a hard error. NIC never silently falls back to
   another binary when the override is set.
2. **`tofu` on `PATH`** — used if its version is compatible. If the `PATH` binary
   is too old (or its version cannot be determined), NIC emits a warning and falls
   back to downloading, so a stale system tofu never breaks a deploy.
3. **Download** — NIC downloads its pinned version (`pkg/tofu.Version`) and caches
   the archive under the [NIC cache directory](#the-nic-cache-directory). This is
   the default when no external binary is present and matches the behavior of
   earlier releases.

A binary is *compatible* when its version is at or above the minimum supported
version (`pkg/tofu.MinVersion`, currently the same as the pinned version) and below
`2.0.0`. When the resolved version differs from the pinned version NIC is tested
against, NIC notes the version in use in its status output.

## The NIC cache directory

NIC caches OpenTofu downloads under `<user-cache-dir>/nic/tofu/`, where
`<user-cache-dir>` is Go's [`os.UserCacheDir()`](https://pkg.go.dev/os#UserCacheDir):

| Platform | Default cache path |
|----------|--------------------|
| Linux    | `~/.cache/nic/tofu/` (or `$XDG_CACHE_HOME/nic/tofu/` if `XDG_CACHE_HOME` is set) |
| macOS    | `~/Library/Caches/nic/tofu/` |
| Windows  | `%LocalAppData%\nic\tofu\` |

`nic version` reports which binary would be used, its version, and its source
(`NIC_TOFU_PATH`, `PATH`, or downloaded), so include its output in support requests:

```console
$ nic version
...
OpenTofu version: 1.12.5 (from PATH: /usr/local/bin/tofu)
```

## Air-gapped and high-security environments

NIC never needs network access to fetch OpenTofu if you provide the binary:

```bash
export NIC_TOFU_PATH=/opt/opentofu/tofu
nic deploy -f config.yaml
```

Alternatively, pre-seed the [NIC cache directory](#the-nic-cache-directory)
(e.g. `~/.cache/nic/tofu/` on Linux, `~/Library/Caches/nic/tofu/` on macOS) from
a machine with network access; NIC serves downloads from that cache indefinitely
once populated.

Note that `tofu init` still needs access to provider plugin registries (or a
pre-populated `TF_PLUGIN_CACHE_DIR`, which NIC sets to the `plugins/`
subdirectory of the cache directory, e.g. `~/.cache/nic/tofu/plugins` on Linux);
`NIC_TOFU_PATH` only covers the OpenTofu binary itself.

**External binaries are trusted, not verified.** When NIC downloads its own
OpenTofu, the download is integrity-checked (signed artifacts via `tofudl`). A
binary supplied through `NIC_TOFU_PATH` or found on `PATH` gets no such check:
NIC executes it to probe its version, so the version floor is a correctness
gate, not a trust boundary. Vet the provenance of pre-installed binaries
through your own supply-chain controls; `NIC_TOFU_PATH` is a packaging
feature, not a hardening one.

## Packaging guidance (pixi/prefix.dev, distro packages)

Declare `opentofu` as a runtime dependency and either rely on `PATH` discovery or
set `NIC_TOFU_PATH` in an activation script, so a workspace that pins both NIC and
OpenTofu never has to phone home on first run.

Declare it in the *workspace*, not in NIC's own package. NIC is provider-agnostic
at the binary level - `local` and `existing` never invoke OpenTofu - and
conda-forge publishes `opentofu` for `linux-64`, `linux-aarch64`, `linux-ppc64le`,
`osx-64`, `osx-arm64` and `win-64`, but **not `win-arm64`**, a platform NIC does
publish a package for. An unconditional run dependency would therefore trade a
working install for an unsolvable one there. The starter workspaces scope the
constraint to the provider that needs it and derive it from
`pkg/tofu.MinVersion`/`MaxVersionExclusive`, which is both tighter and correct
per-platform.

### The conda channel

NIC is published to the `nebari-dev/nebari` channel on prefix.dev:

```bash
pixi add --channel https://prefix.dev/nebari-dev/nebari nebari-infrastructure-core
```

Packages are repackaged from the GitHub release archives rather than rebuilt, so
the installed binary is byte-identical to the one in the release and reports the
release version from `nic version`. Before any digest is read, the release's
`checksums.txt` is cosign-verified against the release workflow's identity, so
the digests prove authenticity and not merely that two files fetched from the
same place agree. `packaging/conda/` holds the recipe and the build script;
The `publish-prefix-dev` job in `.github/workflows/release.yml` runs after the release
itself and uploads over OIDC trusted publishing. It lives in that workflow rather
than one of its own so that it runs on the tag, after the assets exist: a release
created as published fires the release event about twenty minutes before
GoReleaser finishes attaching them.

`publish-starters` then follows it. The starters pin `nic` as a conda dependency,
so their `pixi lock` cannot resolve until the package is on the channel; that
ordering is a `needs:` edge rather than an event relationship.

Only stable releases are published, and this is enforced rather than assumed: the
tag is checked against the release API and a prerelease is refused. That applies
to manual runs too, which is where it matters - a `release: published` filter
alone would still let a hand-dispatched release candidate through. (conda also
forbids `-` in a version string, so a prerelease tag could not be packaged
unrenamed even if the policy changed.)

**When a release does not appear on the channel**, check in this order:

1. Did `Publish to prefix.dev` run for the tag, and was its `release`
   environment approval granted? It is a separate workflow from `Release`, so a
   green release does not imply a published package.
2. Is the prefix.dev trusted publisher still registered for this repository,
   workflow and environment? It is configured outside the repository, so nothing
   here fails a review when it is missing - the upload step is where it surfaces.
3. Does the release carry `checksums.txt`, its `.sigstore.json` bundle, and all
   six archives? The build verifies the bundle, reads the sha256 from
   `checksums.txt`, and fails loudly when either is missing.
4. Re-run the failed job on the tag's own `Release` run. Uploads skip filenames
   the channel already has, so a re-run after a partial upload resumes rather
   than failing on the first one. There is no dispatch path: publishing happens
   only as part of a release, so a corrected recipe ships with a new patch
   release rather than by republishing an existing tag.

**Package on the channel but no starter on quay** means the second job did not
run or did not pass. Check its `quay-publish` approval first, then whether
`pixi lock` resolved - a starter cannot lock until the package it pins is
actually on the channel. This is also how a release
   published before the workflow existed gets backfilled.

This channel is a bridge. The intended home is prefix.dev's shared
[`github-releases`](https://prefix.dev/channels/github-releases) channel, which is
populated by [octoconda](https://redirect.github.com/hunger/octoconda) from our
published releases and needs no recipe on our side. Onboarding is a one-line
entry in its `config.toml`, proposed upstream and not yet merged. When it lands,
`packaging/conda/` and its workflow should be removed and the starter templates
repointed, rather than kept as a second source of packages.

## CI

Runners that already provision OpenTofu (e.g. via `opentofu/setup-opentofu`) are
picked up automatically through `PATH` discovery. Otherwise, persist the
[NIC cache directory](#the-nic-cache-directory) (`~/.cache/nic/tofu/` on typical
Linux runners) across runs to avoid a re-download per fresh runner.

## hetzner-k3s (Hetzner provider)

The Hetzner provider drives cluster creation through
[`hetzner-k3s`](https://github.com/vitobotta/hetzner-k3s) rather than OpenTofu. NIC
resolves it in the same order:

1. **`NIC_HETZNER_K3S_PATH`** — an explicit path to a `hetzner-k3s` binary. If set it
   must point to an executable; anything else is a hard error.
2. **`hetzner-k3s` on `PATH`** — used when present.
3. **Download** — NIC downloads its pinned version and caches it under
   `<user-cache-dir>/nic/hetzner-k3s/`, verifying it against a SHA256 table of known
   digests before use.

Two differences from OpenTofu, both deliberate:

- **No version gate on external binaries.** `hetzner-k3s` ships as a single pinned
  release with no supported range and no stable self-version probe, so a binary from
  `NIC_HETZNER_K3S_PATH` or `PATH` is used as-is — NIC does not check its version.
- **External binaries are neither integrity- nor version-checked.** Only the download
  path is SHA256-verified, and that table covers the pinned version alone. Supplying
  your own `hetzner-k3s` trades that verification for air-gapped/pre-provisioned
  installs; vet its provenance through your own supply-chain controls.

Unlike OpenTofu, `hetzner-k3s` is **not** packaged on conda-forge/prefix.dev, so there
is no dependency to declare. Network-restricted operators can pre-provide the binary
via `NIC_HETZNER_K3S_PATH`, `PATH`, or a pre-seeded `<user-cache-dir>/nic/hetzner-k3s/`
cache directory. Note this covers the binary only: `nic deploy` still runs
`hetzner-k3s releases`, which fetches k3s release tags from GitHub on a cold cache (and
cluster provisioning pulls k3s onto the servers), so fully air-gapped Hetzner deploys
are not currently supported.

## Related

- A shared resolution seam for both binaries (extracting the common override → `PATH` →
  download logic behind one interface) is planned as a follow-up once
  [ADR-0016](https://github.com/nebari-dev/nebari-infrastructure-core/pull/584) lands.
