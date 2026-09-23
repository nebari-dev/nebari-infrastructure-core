# Packaging and External Binaries

NIC shells out to [OpenTofu](https://opentofu.org/) for the providers that provision
infrastructure declaratively (AWS, Azure). By default it downloads its own pinned
OpenTofu binary on first use. This page covers how to make NIC use a pre-installed
binary instead, and how to inject version metadata when building NIC from source --
both of which matter for OS/conda packaging, CI, and air-gapped environments.

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

NIC is published to prefix.dev's shared
[`github-releases`](https://prefix.dev/channels/github-releases) channel:

```bash
pixi add --channel https://prefix.dev/github-releases nebari-infrastructure-core
```

The package is `nebari-infrastructure-core`, after the repository; the binary it
installs is `nic`.

That channel is generated by [octoconda](https://redirect.github.com/hunger/octoconda),
which repackages published GitHub release binaries untouched. There is no recipe,
no feedstock and no build step in this repository: onboarding was a one-line entry
in octoconda's `config.toml`, and every release after it is picked up automatically.
The installed binary is byte-identical to the one in the release and reports the
release version from `nic version`.

**Publication is asynchronous.** octoconda runs on a two-hourly cron, but its runs
start anywhere up to about 50 minutes after the scheduled slot and take between 5
and 20 minutes. A freshly cut release therefore becomes resolvable one to two hours
after the tag in the typical case and close to three hours at worst. Nothing in
this repository can make that faster, and no job here waits for it.

Only stable releases appear, **as long as prerelease tags are `-rc.N`**. octoconda
does not read GitHub's prerelease flag: it skips a tag whose name contains `rc`,
`alpha`, `beta` or `prerelease`, and otherwise takes everything before the first
`-` as the version. A tag like `v0.15.0-pre.1` would be packaged as `0.15.0`, and
since octoconda never rebuilds a version it already carries, the real `v0.15.0`
would then never reach the channel. `release.yml` therefore rejects any tag that
is not `vX.Y.Z` or `vX.Y.Z-rc.N` before the release starts.

**When a release does not appear on the channel**, check in this order:

1. Is the release actually published, not a draft, and does it carry all six
   archives? octoconda reads published release assets; a draft is invisible to it.
2. Has an octoconda run covered us since the release? Its schedule and per-shard
   logs are at
   [hunger/octoconda actions](https://github.com/hunger/octoconda/actions);
   `nebari-infrastructure-core` falls in the `mk-nt` shard.
3. Is the repository still listed in octoconda's `config.toml`? Removal upstream
   would be silent from here.

Escalation is an issue on `hunger/octoconda`, not a change in this repository.
Previously NIC published its own package to a `nebari-dev/nebari` channel as a
bridge while the upstream onboarding PR was open. That PR merged, and octoconda
imported the ten most recent releases (0.4.0 onward; there was never a 0.7.0
release), so the bridge - `packaging/conda/` and the `publish-prefix-dev` job -
has been deleted rather than kept as a second source of packages.

The `nebari-dev/nebari` channel itself stays up. It only ever held v0.14.0,
uploaded by hand, but the v0.14.0 starters on quay were locked against it, so
deleting it would break `pixi install` on those starters.

### Starter workspaces

The starters pin `nic` as a conda dependency, so their `pixi lock` cannot resolve
until the package is on the channel. Because that is now asynchronous, starter
publishing is no longer a job in the release run; it lives in
`.github/workflows/publish-starters.yml`, which `release.yml` fires once and an
hourly cron re-fires until the channel catches up. Allow up to about four hours
from tag to starters: the channel's own delay plus up to one cron interval.

The workflow is two jobs. `check` holds no secrets and needs no approval: for each
recent stable release from the first one that had starters, it asks quay which
providers are missing that tag, then asks the channel whether it can serve the
version yet. `publish` runs only when there is something to push, behind the
`quay-publish` approval, and builds each release's starters from that release's
own tag. So:

- a provider that failed to publish is retried on the next tick, without touching
  the ones that succeeded;
- a release missed while a newer one was cut is still picked up;
- nothing already on quay is republished.

Two cases are reported instead of healed:

- **A release still not on the channel six hours after it was published** fails
  `check`. That is no longer octoconda's delay; it means octoconda has stopped
  covering us, or the channel is wrong. Work through the list above.
- **A missing starter older than one already on quay** is a warning, not a
  publish. `nebi publish` also moves `:latest`, so backfilling it automatically
  would point `:latest` at an older release. Publish it by hand and re-tag
  `:latest`.

A starter that published but then failed the round-trip check stays on quay, and
later runs see it as present. The red run is the signal: delete that tag on quay
and dispatch the workflow again.

**Package on the channel but no starter on quay** means that workflow did not run
or did not pass. Check its `quay-publish` approval first, then its most recent run:
each skip is reported as a notice naming the version and the reason.

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

## Building from source: version metadata (ldflags)

Downstream packagers (Linux distros, Nix, Homebrew, and anyone building outside
GoReleaser) can produce a binary whose `nic version` reports correct metadata
instead of the placeholder `dev` / `none` / `unknown` defaults.

Version metadata is injected at link time into three package-level string
variables in package `internal/cli`:

| Variable  | `-X` target           | Meaning                         |
| --------- | --------------------- | ------------------------------- |
| `version` | `internal/cli.version` | Release version (e.g. `v1.2.3`) |
| `commit`  | `internal/cli.commit`  | Short commit SHA                |
| `date`    | `internal/cli.date`    | Build timestamp (RFC 3339, UTC) |

The `-X` target is the full import path
(`github.com/nebari-dev/nebari-infrastructure-core/internal/cli.version`, etc.),
not `main`: the linker matches `-X` against the fully-qualified package path, so
pointing it at `main.version` injects nothing.

These variables MUST remain `var` (not `const`). The Go linker's `-X` flag can
only override package-level string *variables*; declaring them `const` silently
discards the injected values and the binary reports the defaults regardless of
how it was built. See the comment in
[`internal/cli/version.go`](../../internal/cli/version.go).

`make build` threads all three values through as overridable variables, so a
build with injected metadata looks like:

```bash
make build \
  VERSION=v1.2.3 \
  COMMIT=$(git rev-parse --short HEAD) \
  DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

./nic version
# Nebari Infrastructure Core (NIC)
# Version: v1.2.3
# Commit: <short-sha>
# Built: <timestamp>
# OpenTofu version: ...
```

The `VERSION`, `COMMIT` and `DATE` Make variables are declared with `?=`, so they
can also be supplied through the **environment** -- which is what most distro and
Nix build phases do rather than passing them on the command line:

```bash
VERSION=v1.2.3 \
COMMIT=$(git rev-parse --short HEAD) \
DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ") \
  make build
```

Any of the three may be omitted; the Makefile falls back to deriving it from git
and the current time. Note that an ambient `VERSION`, `COMMIT` or `DATE` already
exported in your shell will therefore feed into the build -- unset them for a
clean git-derived build. NIC stamps the literal `DATE` it is given and does not
currently read `SOURCE_DATE_EPOCH`; a reproducible-build phase should pass a
fixed `DATE` explicitly. Packagers who do not build via `make` can invoke
`go build` directly with the same `-ldflags` string.

Official releases are built by GoReleaser (`.goreleaser.yml`), whose archive
`name_template` and build matrix are the source of truth for release-asset names
and the OS/arch combinations that ship. See
[Verifying a NIC release](verifying-releases.md) for the archive naming used in
`tar`/URL examples, plus checksum, signature, provenance, and SBOM verification.

## Related

- A shared resolution seam for both binaries (extracting the common override → `PATH` →
  download logic behind one interface) is planned as a follow-up once
  [ADR-0016](https://github.com/nebari-dev/nebari-infrastructure-core/pull/584) lands.
