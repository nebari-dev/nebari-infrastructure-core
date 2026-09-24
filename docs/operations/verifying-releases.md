# Verifying a NIC release

Each release publishes, alongside the binaries:

- `checksums.txt` - SHA-256 of every archive
- `checksums.txt.sigstore.json` - a keyless cosign signature bundle over `checksums.txt`
- `<archive>.sbom.json` - an SPDX SBOM per archive
- a build-provenance attestation (stored in GitHub, queried with `gh`)

## 1. Verify integrity

```bash
sha256sum -c checksums.txt   # macOS: shasum -a 256 -c checksums.txt
```

## 2. Verify the signature (authenticity)

Requires [cosign](https://docs.sigstore.dev/) v3+. Identity pinning is mandatory:
a bundle-only verify checks the math, not who signed it.

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/nebari-dev/nebari-infrastructure-core/\.github/workflows/release\.yml@refs/tags/.*$' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  checksums.txt
```

Expected: `Verified OK`.

## 3. Verify build provenance

Requires the GitHub CLI:

```bash
gh attestation verify nebari-infrastructure-core_<version>_linux_x86_64.tar.gz \
  --repo nebari-dev/nebari-infrastructure-core
```

Expected: a line confirming the attestation was issued by the release workflow.

## 4. Inspect the SBOM

```bash
jq '.spdxVersion, (.packages | length)' nebari-infrastructure-core_<version>_linux_x86_64.tar.gz.sbom.json
```

## Maintainer prerequisites (one-time repo-admin setup)

1. **Create the `release` environment** (Settings -> Environments) with required
   reviewers. The `Release` job in `release.yml` uses it, so a release asks for
   approval once, before it is cut. Publishing the conda package needs nothing
   from this repository: octoconda picks up the published release on its own
   (see [packaging.md](packaging.md#the-conda-channel)).

2. **Create the `quay-publish` environment** with required reviewers, and move
   `QUAY_OCI_STARTERS_USERNAME` and `QUAY_OCI_STARTERS_TOKEN` into it. They are
   repository-scoped today, so the starter publish has no approval gate and any
   job in the repository can read them. If the environment gets a
   deployment-branch policy, it must admit both `main` (the hourly cron runs
   there) and `v*` tags (the dispatch from `release.yml` runs on the tag). Only
   the `publish` job uses the environment, so an approval is requested only when
   a starter actually needs pushing, not on every cron tick.

3. **Revoke the old prefix.dev trusted publisher** on the `nebari-dev/nebari`
   channel (a one-time cleanup). It was registered for this repository,
   `release.yml` and the `release` environment while NIC published its own conda
   package. Nothing uses it any more, but until it is removed it still accepts an
   upload from any job that matches that triple. Keep the channel itself: the
   v0.14.0 starters on quay resolve `nic` from it.

`ADD_TO_PROJECT_PAT` is already a fine-grained token with least-privilege scope
(organization Projects: read and write; repository Issues, Pull requests, and
Metadata: read-only), verified 2026-07-14, so it needs no change. The only
token-side hardening in this change is pinning the reusable workflow that
consumes it.
