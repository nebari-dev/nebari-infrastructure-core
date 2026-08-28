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
   reviewers. Two jobs in `release.yml` use it - `Release` and `Publish to
   prefix.dev` - so a release asks for approval twice, once before cutting and
   once before publishing to the channel.

2. **Register the prefix.dev trusted publisher** for the conda channel, under
   the channel's settings: this repository, workflow file `release.yml`, and
   the `release` environment. If a registration against an older workflow
   filename exists, this is a cutover rather than a one-time setup: it has to
   happen between merging the workflow and cutting the next tag, or that
   release fails at upload. Publishing uses OIDC, so there is no token to
   store, but there is also nothing in the repository that fails when the
   registration is missing or wrong. It surfaces only as a failed upload at the
   end of the `Publish to prefix.dev` job. See
   [packaging.md](packaging.md#the-conda-channel).

3. **Create the `quay-publish` environment** with required reviewers, and move
   `QUAY_OCI_STARTERS_USERNAME` and `QUAY_OCI_STARTERS_TOKEN` into it. They are
   repository-scoped today, so the starter publish has no approval gate and any
   job in the repository can read them.

`ADD_TO_PROJECT_PAT` is already a fine-grained token with least-privilege scope
(organization Projects: read and write; repository Issues, Pull requests, and
Metadata: read-only), verified 2026-07-14, so it needs no change. The only
token-side hardening in this change is pinning the reusable workflow that
consumes it.
