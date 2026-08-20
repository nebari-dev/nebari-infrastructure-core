#!/usr/bin/env sh
#
# Install the `nic` (Nebari Infrastructure Core) binary from a GitHub release.
#
#   curl -sfL https://raw.githubusercontent.com/nebari-dev/nebari-infrastructure-core/main/scripts/install.sh | sh
#   curl -sfL .../scripts/install.sh | NIC_VERSION=v0.11.0 sh
#
# Environment variables:
#   NIC_VERSION           version to install: "latest" (default) or a tag like v0.11.0
#   INSTALL_DIR           install location (default: /usr/local/bin; uses sudo if needed)
#   NIC_REPO              source repo (default: nebari-dev/nebari-infrastructure-core)
#   NIC_SKIP_SIGNATURE    set to 1 to skip cosign signature verification (for
#                         air-gapped hosts or networks that block sigstore.dev);
#                         the checksum is still verified
#
# The download is always verified against the release's checksums.txt (integrity).
# When cosign (>= 2.4.2) is installed, checksums.txt is additionally verified
# against the release workflow's signing identity (authenticity); if cosign is
# absent or too old, the GitHub build-provenance attestation is checked instead
# when `gh` is available. Integrity proves the bytes were not corrupted;
# authenticity proves who signed them.
#
# Authenticity is best-effort in one direction only. It degrades, with a plain
# warning, when no tool on this host can verify, or when the release predates
# signing (below v0.10.0). It is never skipped because the signature could not
# be fetched or did not verify for a release that is supposed to have one:
# suppressing the signature is the cheapest attack on a piped installer, so a
# missing or failing signature is fatal rather than a warning.
set -eu

NIC_VERSION="${NIC_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
NIC_REPO="${NIC_REPO:-nebari-dev/nebari-infrastructure-core}"
NIC_SKIP_SIGNATURE="${NIC_SKIP_SIGNATURE:-0}"

# The minimum cosign that can read the sigstore bundle format goreleaser emits.
COSIGN_MIN="2.4.2"
# The first release that publishes checksums.txt.sigstore.json. Below this a
# missing bundle is genuine; at or above it, a missing bundle means something is
# wrong. Keep in step with when `signs:` was added to .goreleaser.yml.
SIGNING_SINCE="0.10.0"
# Absolute doc URL: users who ran the one-liner have no local checkout.
DOCS_URL="https://github.com/${NIC_REPO}/blob/main/docs/operations/verifying-releases.md"

log()  { printf '%s\n' "$*" >&2; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"; }

# curl with a bounded connect time and a few retries, so a flaky or hanging
# network fails fast rather than stalling a piped installer indefinitely.
fetch() { curl --connect-timeout 20 --retry 3 --retry-delay 2 "$@"; }

need curl
need tar
need install
need awk
need mktemp

# Map uname output to the goreleaser archive suffix:
#   nebari-infrastructure-core_<version>_<os>_<arch>.tar.gz
detect_suffix() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$os" in
    linux | darwin) ;;
    *) fail "unsupported OS: $os (Windows users: download the .zip from the releases page)" ;;
  esac
  case "$arch" in
    x86_64 | amd64) arch="x86_64" ;;
    aarch64 | arm64) arch="arm64" ;;
    *) fail "unsupported architecture: $arch" ;;
  esac
  printf '%s_%s' "$os" "$arch"
}

resolve_latest() {
  # Resolve the latest tag from the /releases/latest redirect rather than the
  # GitHub API: no jq, no rate limit (the API is 60/hr per IP unauthenticated),
  # and no token needed. GitHub redirects /releases/latest -> /releases/tag/vX.Y.Z.
  url="$(fetch -fsSL -m 30 -o /dev/null -w '%{url_effective}' "https://github.com/${NIC_REPO}/releases/latest")" || return 1
  case "$url" in
    */releases/tag/*) printf '%s' "${url##*/tag/}" ;;
    *) return 1 ;;
  esac
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail "need sha256sum or shasum to verify the download"
  fi
}

cosign_version() {
  # Print cosign's semantic version (e.g. 2.4.3), or nothing if unparseable.
  cosign version 2>/dev/null |
    sed -n 's/.*GitVersion:[[:space:]]*v\{0,1\}\([0-9][0-9.]*\).*/\1/p' | head -n1
}

version_lt() {
  # True (0) when semver $1 is strictly older than semver $2, comparing
  # component by component so it works for both the cosign floor and the
  # release-signing cutover. An empty or unparseable $1 returns false, so every
  # caller fails safe: an unknown version is treated as new enough to check
  # rather than old enough to skip.
  [ -n "$1" ] || return 1
  awk -v a="$1" -v b="$2" 'BEGIN {
    # Anything that is not a dotted numeric version is "not older", so an
    # unrecognised cosign build still attempts verification and an unrecognised
    # tag is still expected to carry a signature.
    if (a !~ /^[0-9]+(\.[0-9]+)*$/) exit 1;
    na = split(a, x, "."); nb = split(b, y, ".");
    n = (na > nb) ? na : nb;
    for (i = 1; i <= n; i++) {
      ai = x[i] + 0; bi = y[i] + 0;
      if (ai < bi) exit 0;
      if (ai > bi) exit 1;
    }
    exit 1;
  }'
}

# cosign verify-blob over checksums.txt with the pinned release-workflow
# identity. A 404 on the bundle degrades only for releases that predate signing;
# on a release that is supposed to have one, a 404 is treated exactly like any
# other fetch failure, because "the signature is simply missing" is the cheapest
# way to suppress the check.
verify_with_cosign() {
  tmp="$1"; tag="$2"; base="$3"
  ver="${tag#v}"

  # Capture the HTTP status so a pre-signing 404 can degrade while every other
  # outcome stays fatal. Fetching without -f/-S keeps curl from leaking its own
  # error. 4xx is not retried by --retry, so the code here is the final one.
  sig="${tmp}/checksums.txt.sigstore.json"
  code="$(fetch -sL -m 60 -o "$sig" -w '%{http_code}' "${base}/checksums.txt.sigstore.json")" || code=000
  case "$code" in
    200) ;;
    404)
      if version_lt "$ver" "$SIGNING_SINCE"; then
        log "warning: ${tag} predates release signing (first signed release: v${SIGNING_SINCE}); authenticity NOT verified."
        log "         The checksum is still checked. See ${DOCS_URL}"
        return 0
      fi
      fail "no signature bundle for ${tag} (HTTP 404), but every release from v${SIGNING_SINCE} on publishes one; refusing to install. A missing signature on a signed release is indistinguishable from one that was removed to suppress this check. Verify manually (${DOCS_URL}), or re-run with NIC_SKIP_SIGNATURE=1 if you accept that risk."
      ;;
    *)
      fail "could not fetch the signature bundle for ${tag} (HTTP ${code}); refusing to install. If you are offline or on a network that blocks sigstore.dev, verify manually (${DOCS_URL}) or re-run with NIC_SKIP_SIGNATURE=1."
      ;;
  esac

  # Pin the identity to this exact tag (defense in depth over a bare .*), with
  # the tag's dots escaped so v0.13.0 cannot also match v0x13y0.
  tag_re="$(printf '%s' "$tag" | sed 's/\./\\./g')"
  if cosign verify-blob \
    --bundle "$sig" \
    --certificate-identity-regexp "^https://github.com/${NIC_REPO}/\.github/workflows/release\.yml@refs/tags/${tag_re}\$" \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    "${tmp}/checksums.txt" >/dev/null; then
    log "Authenticity verified (cosign, pinned release-workflow identity)"
    return 0
  fi

  # verify-blob exits 1 both when the signature is invalid and when cosign
  # cannot reach the Sigstore trust root, and its stderr wording is not stable
  # enough to match on. `cosign initialize` separates them structurally: it
  # performs the same TUF refresh and nothing else, so if it succeeds the trust
  # root is reachable and the only remaining explanation is that the signature
  # does not verify. That distinction decides whether offering
  # NIC_SKIP_SIGNATURE is sound advice or an invitation to install a tampered
  # binary, so the two cases get two messages.
  if cosign initialize >/dev/null 2>&1; then
    fail "the signature on checksums.txt for ${tag} did NOT verify (see cosign's error above); refusing to install. The Sigstore trust root is reachable, so this is not a network problem: the release assets do not match a signature from ${NIC_REPO}'s release workflow. Do not bypass this check. Please report it at https://github.com/${NIC_REPO}/issues and see ${DOCS_URL}."
  fi
  fail "cosign could not reach the Sigstore trust root to verify checksums.txt for ${tag} (see cosign's error above); refusing to install. If you are air-gapped or on a network that blocks sigstore.dev, verify manually (${DOCS_URL}) or re-run with NIC_SKIP_SIGNATURE=1."
}

# Best-effort authenticity check. Prefers cosign over checksums.txt; if cosign is
# absent or too old, falls back to the GitHub build-provenance attestation over
# the tarball for users who have `gh`. The fallback is additive: it can only
# upgrade a would-be-unverified install to verified, never newly abort, since
# without it we would already be degrading to checksum-only.
verify_authenticity() {
  tmp="$1"; tag="$2"; base="$3"

  # Match truthy values explicitly rather than "anything but 0". Treating every
  # non-zero string as "skip" would turn the check off for NIC_SKIP_SIGNATURE=false
  # and =no, which are the natural spellings of leaving it on.
  case "$NIC_SKIP_SIGNATURE" in
    0 | false | no | off | '') ;;
    1 | true | yes | on)
      log "note: signature verification skipped (NIC_SKIP_SIGNATURE=${NIC_SKIP_SIGNATURE}); only the checksum is checked."
      return 0
      ;;
    *) fail "NIC_SKIP_SIGNATURE must be one of 0/1, true/false, yes/no, on/off (got '${NIC_SKIP_SIGNATURE}'); refusing to guess whether you meant to disable signature verification." ;;
  esac

  if command -v cosign >/dev/null 2>&1; then
    cver="$(cosign_version)"
    if ! version_lt "$cver" "$COSIGN_MIN"; then
      verify_with_cosign "$tmp" "$tag" "$base"
      return 0
    fi
    cosign_note="cosign ${cver} is older than ${COSIGN_MIN} and cannot read this signature format"
  else
    cosign_note="cosign not found"
  fi

  if command -v gh >/dev/null 2>&1; then
    # --signer-workflow pins the attestation to the same release workflow the
    # cosign branch pins its certificate identity to. Without it this accepts a
    # provenance statement from any workflow in the repo, which is a weaker
    # claim than the one the success message advertises.
    if gh attestation verify "${tmp}/nic.tar.gz" --repo "${NIC_REPO}" \
      --signer-workflow "${NIC_REPO}/.github/workflows/release.yml" >/dev/null 2>&1; then
      log "Authenticity verified (gh attestation, build provenance from the release workflow)"
      return 0
    fi
    log "note: authenticity NOT verified: ${cosign_note}, and gh attestation verify did not succeed (not logged in to gh, offline, or no attestation for this release)."
  else
    log "note: authenticity NOT verified: ${cosign_note}; only the checksum is checked."
  fi
  log "      Install cosign (>= ${COSIGN_MIN}) to verify the signature, or see ${DOCS_URL}"
  return 0
}

main() {
  tag="$NIC_VERSION"
  if [ "$tag" = "latest" ]; then
    tag="$(resolve_latest)" || fail "could not resolve the latest release tag (set NIC_VERSION=vX.Y.Z)"
  fi
  case "$tag" in v*) ;; *) tag="v$tag" ;; esac
  version="${tag#v}"

  suffix="$(detect_suffix)"
  tarball="nebari-infrastructure-core_${version}_${suffix}.tar.gz"
  base="https://github.com/${NIC_REPO}/releases/download/${tag}"

  # Fail fast on a bad INSTALL_DIR before the (large) download, and create the
  # directory when we can, since the README suggests paths like ~/.local/bin
  # that often do not exist yet.
  if [ ! -d "$INSTALL_DIR" ]; then
    mkdir -p "$INSTALL_DIR" 2>/dev/null ||
      fail "install directory does not exist and could not be created: ${INSTALL_DIR} (create it or set INSTALL_DIR)"
  fi

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  log "Downloading ${tarball} (${tag})"
  # --speed-limit/--speed-time abort a stalled transfer without capping the total
  # time, which matters for a large archive on a slow link.
  fetch -fsSL --speed-limit 1024 --speed-time 30 "${base}/${tarball}" -o "${tmp}/nic.tar.gz" ||
    fail "download failed: ${base}/${tarball}"
  fetch -fsSL -m 60 "${base}/checksums.txt" -o "${tmp}/checksums.txt" ||
    fail "could not fetch checksums.txt for ${tag}"

  # Authenticity first (does the checksum list come from the release workflow?),
  # then integrity (does the tarball match the list?).
  verify_authenticity "$tmp" "$tag" "$base"

  expected="$(awk -v n="$tarball" '$2 == n {print $1}' "${tmp}/checksums.txt")"
  [ -n "$expected" ] || fail "no checksum entry for ${tarball} in checksums.txt"
  actual="$(sha256_of "${tmp}/nic.tar.gz")"
  [ "$expected" = "$actual" ] || fail "checksum mismatch (expected ${expected}, got ${actual})"
  log "Checksum verified"

  tar -xzf "${tmp}/nic.tar.gz" -C "${tmp}"
  # Goreleaser places the binary at the archive root. Match the exact name
  # `nic` at depth 1 (not a prefix, not deeper) so a doc file shipped under
  # docs/ in the archive can never be mistaken for the binary.
  bin="$(find "${tmp}" -maxdepth 1 -type f -name nic | head -n1)"
  [ -n "$bin" ] || fail "no 'nic' binary found in ${tarball}"

  if [ -w "$INSTALL_DIR" ]; then
    install -m 0755 "$bin" "${INSTALL_DIR}/nic"
  else
    command -v sudo >/dev/null 2>&1 ||
      fail "cannot write to ${INSTALL_DIR} and sudo is not available (set INSTALL_DIR to a writable path)"
    log "Installing to ${INSTALL_DIR} (requires sudo)"
    sudo install -m 0755 "$bin" "${INSTALL_DIR}/nic"
  fi

  log "Installed nic ${version} to ${INSTALL_DIR}/nic"
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *) log "warning: ${INSTALL_DIR} is not on your PATH; add it, or run ${INSTALL_DIR}/nic directly" ;;
  esac
}

# main is defined above and called here, on the last line, so a truncated
# `curl | sh` download cannot execute a partial script.
#
# NIC_INSTALL_SH_SOURCE_ONLY lets scripts/test-installer.sh source this file to
# exercise the functions above without performing an install. Nothing else sets
# it, and the only thing setting it can do is make the installer a no-op, so it
# is not a bypass of anything. Do not add other early exits here.
[ "${NIC_INSTALL_SH_SOURCE_ONLY:-0}" = "1" ] || main
