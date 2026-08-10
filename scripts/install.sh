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
# when `gh` is available. Authenticity is best-effort: it is skipped, with a
# plain warning, when neither tool can verify or a release predates signing. It
# is NOT skipped silently on a failed signature fetch for a signed release,
# because that is the exact tampering case verification exists to catch.
# Integrity proves the bytes were not corrupted; authenticity proves who signed
# them.
set -eu

NIC_VERSION="${NIC_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
NIC_REPO="${NIC_REPO:-nebari-dev/nebari-infrastructure-core}"
NIC_SKIP_SIGNATURE="${NIC_SKIP_SIGNATURE:-0}"

# The minimum cosign that can read the sigstore bundle format goreleaser emits.
COSIGN_MIN="2.4.2"
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

cosign_too_old() {
  # True (0) when $1 is a semver older than COSIGN_MIN (2.4.2). An empty/
  # unparseable version returns false so verification is still attempted.
  [ -n "$1" ] || return 1
  awk -v v="$1" 'BEGIN {
    split(v, a, ".");
    maj = a[1] + 0; min = a[2] + 0; pat = a[3] + 0;
    if (maj < 2) exit 0;
    if (maj == 2 && min < 4) exit 0;
    if (maj == 2 && min == 4 && pat < 2) exit 0;
    exit 1;
  }'
}

# cosign verify-blob over checksums.txt with the pinned release-workflow
# identity. A genuine 404 on the bundle (a release from before signing) degrades;
# any other fetch failure on a signed release, or a failed verify, is fatal.
verify_with_cosign() {
  tmp="$1"; tag="$2"; base="$3"

  # Capture the HTTP status so a genuine 404 can degrade while any other failure
  # stays fatal. Fetching without -f/-S keeps curl from leaking its own error.
  sig="${tmp}/checksums.txt.sigstore.json"
  code="$(fetch -sL -m 60 -o "$sig" -w '%{http_code}' "${base}/checksums.txt.sigstore.json")" || code=000
  case "$code" in
    200) ;;
    404)
      log "warning: no signature bundle for ${tag} (HTTP 404); authenticity NOT verified."
      log "         Only releases from before signing was enabled lack one; the checksum is still checked. See ${DOCS_URL}"
      return 0
      ;;
    *)
      fail "could not fetch the signature bundle for ${tag} (HTTP ${code}); refusing to install. If you are offline or on a network that blocks sigstore.dev, verify manually (${DOCS_URL}) or re-run with NIC_SKIP_SIGNATURE=1."
      ;;
  esac

  # Pin the identity to this exact tag (defense in depth over a bare .*).
  if cosign verify-blob \
    --bundle "$sig" \
    --certificate-identity-regexp "^https://github.com/${NIC_REPO}/\.github/workflows/release\.yml@refs/tags/${tag}\$" \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    "${tmp}/checksums.txt" >/dev/null; then
    log "Authenticity verified (cosign, pinned release-workflow identity)"
  else
    fail "cosign could not verify the signature on checksums.txt (see cosign's error above); refusing to install. If cosign cannot reach the Sigstore trust root (air-gapped, or a proxy blocking sigstore.dev), verify manually (${DOCS_URL}) or re-run with NIC_SKIP_SIGNATURE=1."
  fi
}

# Best-effort authenticity check. Prefers cosign over checksums.txt; if cosign is
# absent or too old, falls back to the GitHub build-provenance attestation over
# the tarball for users who have `gh`. The fallback is additive: it can only
# upgrade a would-be-unverified install to verified, never newly abort, since
# without it we would already be degrading to checksum-only.
verify_authenticity() {
  tmp="$1"; tag="$2"; base="$3"

  if [ "$NIC_SKIP_SIGNATURE" != "0" ]; then
    log "note: signature verification skipped (NIC_SKIP_SIGNATURE set); only the checksum is checked."
    return 0
  fi

  if command -v cosign >/dev/null 2>&1; then
    cver="$(cosign_version)"
    if ! cosign_too_old "$cver"; then
      verify_with_cosign "$tmp" "$tag" "$base"
      return 0
    fi
    cosign_note="cosign ${cver} is older than ${COSIGN_MIN} and cannot read this signature format"
  else
    cosign_note="cosign not found"
  fi

  if command -v gh >/dev/null 2>&1; then
    if gh attestation verify "${tmp}/nic.tar.gz" --repo "${NIC_REPO}" >/dev/null 2>&1; then
      log "Authenticity verified (gh attestation, build provenance)"
      return 0
    fi
    log "note: authenticity NOT verified: ${cosign_note}, and gh attestation verify did not succeed (offline, or no attestation for this release)."
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

main
