# conda packaging

Repackages published `nic` release archives as conda packages for the
[`nebari-dev/nebari`](https://prefix.dev/nebari-dev/nebari) channel on
prefix.dev, so `nic` installs with pixi and a Nebari workspace can pin the
whole toolchain in one lockfile.

```bash
pixi add --channel https://prefix.dev/nebari-dev/nebari nebari-infrastructure-core
```

## What this is not

Nothing here builds `nic` from source. Each package unpacks the archive
GoReleaser already published and copies the binary into the prefix, so the
bytes a conda user runs are the bytes the release signed. The sha256 in every
rendered recipe comes from that release's `checksums.txt`, which the release
pipeline signs with cosign; a missing checksum fails the build rather than
producing an unverified package.

## Layout

| Path | Role |
| --- | --- |
| `recipe.yaml.tmpl` | The recipe, with `__PLACEHOLDER__` slots. Not a standalone recipe |
| `build-packages.sh` | Renders and builds it once per conda subdir |

`.github/workflows/publish-conda.yml` runs the script on every published
release and uploads the results.

## Building locally

```bash
packaging/conda/build-packages.sh v0.14.0 dist/conda
```

Six packages land in `dist/conda/<subdir>/`. One Linux runner emits all six:
the recipe copies files rather than compiling, so only the smoke test
(`nic version`) is platform-bound, and `--test native` runs it for the build
platform and skips it elsewhere.

To check one end to end before uploading:

```bash
pixi init --channel "file://$PWD/dist/conda" --channel conda-forge /tmp/check
cd /tmp/check && pixi add nebari-infrastructure-core && pixi run nic version
```

## Two things that will bite

**conda forbids `-` in a version string.** It separates name, version and build
string in a package filename, so a `v0.14.0-rc.1` tag builds an artifact whose
own version cannot be resolved, failing at test time as an unhelpful "failed to
setup test environment". `build-packages.sh` normalises to `0.14.0rc1`, which
conda also sorts before `0.14.0` - the ordering a prerelease wants.

**Windows differs twice.** The archives are `.zip` rather than `.tar.gz`, and
conda expects binaries under `Library/bin` rather than `bin`. Both are handled
by the platform table in the script.

## Relationship to the github-releases channel

This channel is a bridge. The intended long-term home is prefix.dev's shared
[`github-releases`](https://prefix.dev/channels/github-releases) channel, which
is populated by [octoconda](https://github.com/hunger/octoconda) and needs no
recipe at all - only a one-line entry in its `config.toml`. That entry is
proposed upstream and not yet merged, and until it is, the starter workspaces
cannot resolve `nic` from anywhere.

When it merges, this directory and its workflow should be deleted and the
starter templates repointed, rather than kept as a second place packages come
from.
