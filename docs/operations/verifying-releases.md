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

Requires [cosign](https://docs.sigstore.dev/) v2.6.5+ on 2.x, or v3.1.3+ on
3.x. Earlier builds are affected by
[GHSA-fx35-mq7g-6g98](https://github.com/sigstore/cosign/security/advisories/GHSA-fx35-mq7g-6g98),
where a legacy-format bundle carrying an attacker's own key passes an
identity-pinned verify. 3.0.0 through 3.1.2 are affected even though they are
newer than 2.6.5, so check the floor for your major version.

Identity pinning is mandatory: a bundle-only verify checks the math, not who
signed it. Pin the exact tag you downloaded (replace `<tag>`, e.g. `v0.14.0`),
not a pattern: a pattern that accepts any tag also accepts an older signed
release served in place of the one you asked for.

On cosign 2.x, also pass `--new-bundle-format`. It forces 2.x onto the new
bundle format, so a legacy-format bundle is refused outright. 3.x has no
equivalent: it still falls back to the legacy format when a bundle does not
parse as the new one, which is why 3.0.0 through 3.1.2 are affected, and
upgrading is the only fix. On 3.x the flag is accepted but changes nothing, and
from 3.1.0 on it also prints a deprecation warning.

```bash
# On cosign 2.x, add --new-bundle-format (see above).
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity 'https://github.com/nebari-dev/nebari-infrastructure-core/.github/workflows/release.yml@refs/tags/<tag>' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  checksums.txt
```

Expected: `Verified OK`.

On a host that cannot reach Sigstore's TUF repository, add `--trusted-root`
pointing at the root file in
[`scripts/trusted-roots/`](../../scripts/trusted-roots/) whose name matches
`TRUSTED_ROOT_SHA256` in `scripts/install.sh` (plus `--offline` on cosign 2.x).
That is the root the install script verifies against. On 3.x, `--trusted-root`
also keeps a legacy-format bundle from being verified at all, because cosign
refuses that flag on the legacy path. The version floor above still applies.

## 3. Verify build provenance

Requires the GitHub CLI, v2.47.0 or later. `<version>` is the tag without its
leading `v`; the identity takes the tag itself:

```bash
gh attestation verify nebari-infrastructure-core_<version>_linux_x86_64.tar.gz \
  --repo nebari-dev/nebari-infrastructure-core \
  --cert-identity 'https://github.com/nebari-dev/nebari-infrastructure-core/.github/workflows/release.yml@refs/tags/<tag>'
```

Expected: a line confirming the attestation was issued by the release workflow.

Use `--cert-identity`, not `--signer-workflow`. `--signer-workflow` checks the
workflow path but not the ref it ran at, so it accepts an older release's
archive presented under a newer version's name.

## 4. Inspect the SBOM

```bash
jq '.spdxVersion, (.packages | length)' nebari-infrastructure-core_<version>_linux_x86_64.tar.gz.sbom.json
```

## Installing with a pinned digest (`NIC_EXPECTED_SHA256`)

`scripts/install.sh` points here whenever it cannot verify a release itself: on
an air-gapped host, behind a network that blocks the signature or the trust
root, or for a release older than v0.10.0, which predates signing. In those
cases it installs only against a SHA-256 you supply:

```bash
NIC_VERSION=<tag> NIC_EXPECTED_SHA256=<digest> sh install.sh
```

The archive is compared against that digest and nothing else, so where the
digest comes from is the whole guarantee:

- **v0.10.0 and later.** Verify `checksums.txt` with cosign as in step 2, on any
  machine that can, then take the first field of the archive's line in that
  verified file, e.g. `grep linux_x86_64.tar.gz checksums.txt`.
- **Before v0.10.0.** Nothing in the release can vouch for it, so the digest has
  to come from outside the release: one you recorded when you first installed
  that version, or a mirror you already trust. Never copy it from that release's
  own `checksums.txt`. It comes from the same place as the archive, so if one
  was replaced the other can be too.

## Maintainer prerequisites (one-time repo-admin setup)

1. **Create the `release` environment** (Settings -> Environments) with required
   reviewers. The `Release` job in `release.yml` uses it, so a release asks for
   approval once, before it is cut. Publishing the conda package needs nothing
   from this repository: octoconda picks up the published release on its own
   (see [packaging.md](packaging.md#the-conda-channel)).

2. **Create the `quay-publish` environment before merging the workflow that
   uses it**, with required reviewers, and move `QUAY_OCI_STARTERS_USERNAME` and
   `QUAY_OCI_STARTERS_TOKEN` into it. They are repository-scoped today, so the
   starter publish has no approval gate and any job in the repository can read
   them, and a job that names a missing environment creates it with no
   protection at all. Restrict its deployment branches to `main`: both the hourly
   cron and the dispatch from `release.yml` run on `main`, and a policy that also
   admitted `v*` tags would let anyone who can push a tag run an edited copy of
   the workflow against the Quay token. Only the `publish` job uses the
   environment, so an approval is requested only when a starter actually needs
   pushing, not on every cron tick.

   Related hardening worth doing: the "Protect release tags" ruleset only blocks
   deletion and force-pushes, so anyone with write access can create a `v*` tag.
   A `creation` rule limited to maintainers closes that, and matters because
   the publish job builds tag code while holding the Quay token.

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
