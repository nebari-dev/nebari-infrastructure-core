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
# The download is always verified against the release's checksums.txt.
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

# Map uname output to the goreleaser archive suffix:
#   nebari-infrastructure-core_<version>_<os>_<arch>.tar.gz
detect_suffix() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in
    x86_64 | amd64) arch="x86_64" ;;
    aarch64 | arm64) arch="arm64" ;;
    *) fail "unsupported architecture: $arch" ;;
  esac
  printf '%s_%s' "$os" "$arch"
}

resolve_latest() {
  # Parse the latest release tag without requiring jq.
  curl -fsSL "https://api.github.com/repos/${NIC_REPO}/releases/latest" |
    sed -n 's/.*"tag_name" *: *"\([^"]*\)".*/\1/p' | head -n1
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

main() {
  tag="$NIC_VERSION"
  [ "$tag" = "latest" ] && tag="$(resolve_latest)"
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

  expected="$(awk -v n="$tarball" '$2 == n {print $1}' "${tmp}/checksums.txt")"
  [ -n "$expected" ] || fail "no checksum entry for ${tarball} in checksums.txt"
  actual="$(sha256_of "${tmp}/nic.tar.gz")"
  [ "$expected" = "$actual" ] || fail "checksum mismatch (expected ${expected}, got ${actual})"
  log "Checksum verified"

  tar -xzf "${tmp}/nic.tar.gz" -C "${tmp}"
  bin="$(find "${tmp}" -maxdepth 3 -type f -name nic | head -n1)"
  [ -n "$bin" ] || fail "no 'nic' binary found in ${tarball}"

  if [ -w "$INSTALL_DIR" ]; then
    install -m 0755 "$bin" "${INSTALL_DIR}/nic"
  else
    log "Installing to ${INSTALL_DIR} (requires sudo)"
    sudo install -m 0755 "$bin" "${INSTALL_DIR}/nic"
  fi

  log "Installed: $("${INSTALL_DIR}/nic" version 2>/dev/null || echo "${INSTALL_DIR}/nic")"
}

main
