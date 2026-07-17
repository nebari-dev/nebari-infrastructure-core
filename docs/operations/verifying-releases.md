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
   reviewers. This activates the approval gate on the release job.

`ADD_TO_PROJECT_PAT` is already a fine-grained token with least-privilege scope
(organization Projects: read and write; repository Issues, Pull requests, and
Metadata: read-only), verified 2026-07-14, so it needs no change. The only
token-side hardening in this change is pinning the reusable workflow that
consumes it.
