# Packaging and External Binaries

NIC shells out to [OpenTofu](https://opentofu.org/) for the providers that provision
infrastructure declaratively (AWS, Azure). By default it downloads its own pinned
OpenTofu binary on first use. This page covers how to make NIC use a pre-installed
binary instead, which matters for OS/conda packaging, CI, and air-gapped environments.

## OpenTofu resolution order

When a command needs OpenTofu, NIC resolves the binary in this order:

1. **`NIC_TOFU_PATH`** — an explicit path to an OpenTofu binary. If set, it must
   point to an executable, compatible binary; anything else is a hard error. NIC
   never silently falls back to another binary when the override is set.
2. **`tofu` on `PATH`** — used if its version is compatible. If the `PATH` binary
   is too old (or its version cannot be determined), NIC emits a warning and falls
   back to downloading, so a stale system tofu never breaks a deploy.
3. **Download** — NIC downloads its pinned version (`pkg/tofu.Version`) and caches
   the archive under `~/.cache/nic/tofu/`. This is the default when no external
   binary is present and matches the behavior of earlier releases.

A binary is *compatible* when its version is at or above the minimum supported
version (`pkg/tofu.MinVersion`, currently the same as the pinned version) and below
`2.0.0`. When the resolved version differs from the pinned version NIC is tested
against, NIC logs a warning naming the version in use.

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

Alternatively, pre-seed `~/.cache/nic/tofu/` from a machine with network access;
NIC serves downloads from that cache indefinitely once populated.

Note that `tofu init` still needs access to provider plugin registries (or a
pre-populated `TF_PLUGIN_CACHE_DIR`, which NIC sets to `~/.cache/nic/tofu/plugins`);
`NIC_TOFU_PATH` only covers the OpenTofu binary itself.

## Packaging guidance (conda-forge, distro packages)

Declare `opentofu` as a runtime dependency and either rely on `PATH` discovery or
set `NIC_TOFU_PATH` in an activation script. conda-forge ships `opentofu` for all
platforms NIC supports, so a packaged NIC never has to phone home on first run.

## CI

Runners that already provision OpenTofu (e.g. via `opentofu/setup-opentofu`) are
picked up automatically through `PATH` discovery. Otherwise, persist
`~/.cache/nic/tofu/` across runs to avoid a re-download per fresh runner.

## Related

- The Hetzner provider uses the `hetzner-k3s` binary rather than OpenTofu; the same
  external-binary treatment for it is tracked separately.
