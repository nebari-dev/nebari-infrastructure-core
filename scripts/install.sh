#!/usr/bin/env sh
#
# Install the `nic` (Nebari Infrastructure Core) binary from a GitHub release.
#
#   curl -sfL https://raw.githubusercontent.com/nebari-dev/nebari-infrastructure-core/main/scripts/install.sh | sh
#   curl -sfL .../scripts/install.sh | NIC_VERSION=v0.11.0 sh
#
# Environment variables:
#   NIC_VERSION   version to install: "latest" (default) or a tag like v0.11.0
#   INSTALL_DIR   install location (default: /usr/local/bin; uses sudo if needed)
#   NIC_REPO      source repo (default: nebari-dev/nebari-infrastructure-core)
#
# The download is always verified against the release's checksums.txt
# (integrity). If cosign is installed, checksums.txt is additionally verified
# against the release workflow's signing identity (authenticity); if not, the
# script logs plainly that authenticity was NOT checked. Integrity proves the
# bytes were not corrupted; authenticity proves who signed them. See
# docs/operations/verifying-releases.md.
set -eu

NIC_VERSION="${NIC_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
NIC_REPO="${NIC_REPO:-nebari-dev/nebari-infrastructure-core}"

log()  { printf '%s\n' "$*" >&2; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"; }

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
  url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${NIC_REPO}/releases/latest")" || return 1
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

# Opportunistically verify authenticity: if cosign is present, verify the
# signature over checksums.txt against the release workflow's pinned identity
# (the same regexp documented in docs/operations/verifying-releases.md). A
# present-but-failing signature aborts the install; a missing cosign only
# downgrades to integrity-only, logged plainly.
verify_authenticity() {
  tmp="$1"; tag="$2"; base="$3"
  if ! command -v cosign >/dev/null 2>&1; then
    log "note: authenticity NOT verified — cosign not found, only the checksum was checked."
    log "      For signed-identity verification see docs/operations/verifying-releases.md"
    return 0
  fi
  if ! curl -fsSL "${base}/checksums.txt.sigstore.json" -o "${tmp}/checksums.txt.sigstore.json"; then
    log "warning: cosign is installed but no signature bundle was published for ${tag}; authenticity NOT verified"
    return 0
  fi
  if cosign verify-blob \
    --bundle "${tmp}/checksums.txt.sigstore.json" \
    --certificate-identity-regexp "^https://github.com/${NIC_REPO}/\.github/workflows/release\.yml@refs/tags/.*\$" \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    "${tmp}/checksums.txt" >/dev/null; then
    log "Authenticity verified (cosign, pinned release-workflow identity)"
  else
    fail "cosign signature verification FAILED for checksums.txt — refusing to install (needs cosign v3+; see docs/operations/verifying-releases.md)"
  fi
}

main() {
  tag="$NIC_VERSION"
  if [ "$tag" = "latest" ]; then
    tag="$(resolve_latest)" || fail "could not resolve the latest release tag (set NIC_VERSION=vX.Y.Z)"
  fi
  [ -n "$tag" ] || fail "could not resolve a release tag (set NIC_VERSION=vX.Y.Z)"
  case "$tag" in v*) ;; *) tag="v$tag" ;; esac
  version="${tag#v}"

  suffix="$(detect_suffix)"
  tarball="nebari-infrastructure-core_${version}_${suffix}.tar.gz"
  base="https://github.com/${NIC_REPO}/releases/download/${tag}"

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  log "Downloading ${tarball} (${tag})"
  curl -fsSL "${base}/${tarball}" -o "${tmp}/nic.tar.gz" ||
    fail "download failed: ${base}/${tarball}"
  curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt" ||
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

  [ -d "$INSTALL_DIR" ] || fail "install directory does not exist: ${INSTALL_DIR} (create it or set INSTALL_DIR)"
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
    *) log "warning: ${INSTALL_DIR} is not on your PATH — add it, or run ${INSTALL_DIR}/nic directly" ;;
  esac
}

main
