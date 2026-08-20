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
set `NIC_TOFU_PATH` in an activation script. NIC itself is distributed via the
prefix.dev `github-releases` channel (see
[https://github.com/nebari-dev/nebari-infrastructure-core/issues/552](https://github.com/nebari-dev/nebari-infrastructure-core/issues/552)),
and conda-forge ships `opentofu` for all platforms NIC supports, so a pixi
workspace that pins both never has to phone home on first run.

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
