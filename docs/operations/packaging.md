# Packaging NIC from source

Downstream packagers (Linux distros, Nix, Homebrew, and anyone building
outside GoReleaser) can produce a binary whose `nic version` reports correct
metadata instead of the placeholder `dev` / `none` / `unknown` defaults.

## The ldflags contract

Version metadata is injected at link time into three package-level string
variables in `cmd/nic`:

| Variable       | `-X` target      | Meaning                          |
| -------------- | ---------------- | -------------------------------- |
| `version`      | `main.version`   | Release version (e.g. `v1.2.3`)  |
| `commit`       | `main.commit`    | Short commit SHA                 |
| `date`         | `main.date`      | Build timestamp (RFC 3339, UTC)  |

The linker overrides them with:

```
-X main.version=<version> -X main.commit=<commit> -X main.date=<date>
```

These variables MUST remain `var` (not `const`). The Go linker's `-X` flag can
only override package-level string *variables*; declaring them `const` silently
discards the injected values and the binary reports the defaults regardless of
how it was built. See the comment in
[`cmd/nic/version.go`](../../cmd/nic/version.go).

## Supported entry point: `make build`

`make build` threads all three values through as overridable variables, so the
standard, supported way to build with injected metadata is:

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

Any of the three may be omitted; the Makefile falls back to deriving it from
git and the current time. Packagers who do not build via `make` can invoke
`go build` directly with the same `-ldflags` string.

## Release-archive naming

Official releases are built by GoReleaser (`.goreleaser.yml`). Archives follow:

```
nebari-infrastructure-core_<version>_<os>_<arch>.tar.gz
```

- `<os>` is `linux`, `darwin`, or `windows`.
- `<arch>` is `x86_64` (amd64), `arm64`, or `i386` (386).
- Windows archives ship as `.zip` instead of `.tar.gz`.

For example: `nebari-infrastructure-core_v1.2.3_linux_x86_64.tar.gz`.

See [Verifying a NIC release](verifying-releases.md) for checksum, signature,
provenance, and SBOM verification of these archives.
