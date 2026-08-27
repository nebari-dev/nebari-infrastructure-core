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
GoReleaser already published and copies the binary into the prefix.

The sha256 in every rendered recipe comes from that release's `checksums.txt`,
and the build cosign-verifies that file against the release workflow's identity
before reading a digest out of it. The order is the point: without the signature
check the digests would only show that an archive matches a manifest fetched
from the same place, which is consistency, not authenticity. A missing checksum
entry, or a bundle that does not verify, fails the build.

## Layout

| Path | Role |
| --- | --- |
| `recipe.yaml.tmpl` | The recipe, with `__PLACEHOLDER__` slots. Not a standalone recipe |
| `build-packages.sh` | Renders and builds it once per conda subdir |

`.github/workflows/publish-conda.yml` runs the script after a successful
`Release` run, for stable releases only, uploads the results, and then publishes
the starter workspaces in a second job gated on that upload.

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

## Three things that will bite

**conda forbids `-` in a version string.** It separates name, version and build
string in a package filename, so a `v0.14.0-rc.1` tag builds an artifact whose
own version cannot be resolved, failing at test time as an unhelpful "failed to
setup test environment". `build-packages.sh` normalises to `0.14.0rc1`, which
conda also sorts before `0.14.0`.

The workflow refuses prereleases outright, so that normalisation is currently
unreachable from CI. It is kept because the policy is a decision, not a
constraint: if release candidates are ever published, this is the part that
would otherwise fail in a way whose error message points nowhere near the cause.

**A recipe fix needs a new build number.** The package filename is
`name-version-<varianthash>_<number>`, and the variant hash covers the variant
configuration rather than the recipe body: adding a requirements block or editing
`build.script` leaves the name byte-identical. Since uploads skip filenames the
channel already has, republishing a corrected recipe for an already-published
version would be skipped silently. Pass `BUILD_NUMBER=1`, or the workflow's
`build_number` input, when republishing.

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
