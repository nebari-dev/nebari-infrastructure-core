# Security Policy

## Supported Versions

Nebari Infrastructure Core (NIC) is pre-1.0. Security fixes are made on `main` and shipped in the next release. Only the latest minor release line receives fixes; older release lines and release candidates (`-rc.N` tags) are not patched.

| Version | Supported |
| ------- | --------- |
| Latest minor release (see [Releases](https://github.com/nebari-dev/nebari-infrastructure-core/releases/latest)) | Yes |
| Older minor releases | No |
| Release candidates | No |

## Reporting a Vulnerability

Please do not report security vulnerabilities through public GitHub issues, pull requests, or discussions.

Report them privately through GitHub's private vulnerability reporting: [open a new advisory](https://github.com/nebari-dev/nebari-infrastructure-core/security/advisories/new). Only the maintainers can see the report.

Please include as much of the following as you can:

- The NIC version (`nic version`) and the cloud provider in use
- The affected component (CLI, OpenTofu module, or a manifest NIC deploys)
- Steps to reproduce, or a proof of concept
- The impact as you understand it, including what an attacker would need to exploit it

## What to Expect

The maintainers will acknowledge the report, work with you to confirm and understand the issue, and keep you updated on progress toward a fix. Once a fix is released, we will publish a GitHub Security Advisory and credit you unless you ask us not to.

Please give us a reasonable chance to release a fix before disclosing the issue publicly.

## Scope

In scope:

- The `nic` CLI and the Go packages in this repository
- The OpenTofu/Terraform modules under `terraform/`
- The Argo CD applications and Kubernetes manifests that NIC generates and deploys, including their default configuration
- The release pipeline and published release artifacts

Out of scope here (please report upstream instead):

- Vulnerabilities in third-party software NIC deploys, such as Argo CD, Keycloak, Envoy Gateway, or cert-manager, unless the issue is caused by how NIC configures them
- Vulnerabilities in the main [Nebari](https://github.com/nebari-dev/nebari/security/advisories/new) project
- Issues that require an attacker to already hold cluster-admin or cloud-account administrator credentials

## Verifying Releases

Release checksums are signed with [Sigstore cosign](https://docs.sigstore.dev/) using keyless signing from the release workflow, each archive has a GitHub build-provenance attestation, and each archive ships with an SBOM (`*.sbom.json`).

To verify a downloaded release (replace `vX.Y.Z` and the archive name):

```bash
# 1. Verify the signature on checksums.txt
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/nebari-dev/nebari-infrastructure-core/.github/workflows/release.yml@refs/tags/vX.Y.Z" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

# 2. Verify the archive against the signed checksums (on macOS, use `shasum -a 256 -c` in place of `sha256sum -c`)
grep " nebari-infrastructure-core_X.Y.Z_linux_x86_64.tar.gz$" checksums.txt | sha256sum -c

# 3. Verify the build-provenance attestation
gh attestation verify nebari-infrastructure-core_X.Y.Z_linux_x86_64.tar.gz \
  -R nebari-dev/nebari-infrastructure-core
```
