#!/usr/bin/env sh
#
# Install the `nic` (Nebari Infrastructure Core) binary from a GitHub release.
#
#   curl -sfL https://raw.githubusercontent.com/nebari-dev/nebari-infrastructure-core/main/scripts/install.sh | sh
#   curl -sfL .../scripts/install.sh | NIC_VERSION=v0.11.0 sh
#
# Environment variables:
#   NIC_VERSION            version to install: "latest" (default) or a tag like v0.14.0
#   INSTALL_DIR            install location (default: /usr/local/bin; uses sudo if needed)
#   NIC_REPO               source repo (default: nebari-dev/nebari-infrastructure-core)
#   NIC_EXPECTED_SHA256    the archive's SHA-256, obtained from a source you trust.
#                          When set, the archive is compared against it directly and
#                          the release's checksums.txt and signature are not consulted
#   NIC_REQUIRE_SIGNATURE  set to 1 to refuse a checksum-only install when nothing on
#                          this host can verify the release
#
# The download is always verified against the release's checksums.txt (integrity).
# Authenticity is checked with the first capable verifier on the host: cosign
# (>= 2.6.5 on 2.x, >= 3.1.3 on 3.x) over checksums.txt, pinned to the release
# workflow's identity at this exact tag and verified offline against a pinned
# Sigstore trust root; otherwise a logged-in `gh`, over the archive's
# build-provenance attestation. Integrity proves the bytes were not corrupted;
# authenticity proves who produced them.
#
# Authenticity degrades in exactly one case: no capable verifier on the host. The
# install then continues on the checksum alone with a warning, or stops if
# NIC_REQUIRE_SIGNATURE is set. Once a capable verifier is present nothing
# degrades, for any tag: a missing, unfetchable or failing signature is fatal,
# because suppressing the signature is the cheapest attack on a piped installer.
set -eu

NIC_VERSION="${NIC_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
NIC_REPO="${NIC_REPO:-nebari-dev/nebari-infrastructure-core}"
NIC_EXPECTED_SHA256="${NIC_EXPECTED_SHA256:-}"
NIC_REQUIRE_SIGNATURE="${NIC_REQUIRE_SIGNATURE:-0}"

# The minimum cosign trusted per major version. Older builds are inside
# GHSA-fx35-mq7g-6g98, where a legacy bundle carrying an attacker's own public key
# passes an identity-pinned verify-blob. One floor cannot express this: 3.0.0 is
# newer than 2.6.5 and still affected.
COSIGN_MIN_V2="2.6.5"
COSIGN_MIN_V3="3.1.3"
# The first release that publishes checksums.txt.sigstore.json and a
# build-provenance attestation. It only shapes the error message: a release below
# it can still be installed, but only against a digest the user pins, because
# "this release predates signing" is exactly what a forged release would claim.
SIGNING_SINCE="0.10.0"
# The Sigstore trust root cosign verifies against, offline. Content-addressed:
# scripts/trusted-roots/<sha256>.json is never edited or deleted, only added to,
# so an installer saved before a root rotation keeps working.
TRUSTED_ROOT_SHA256="6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66"
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

cosign_capable() {
  # True (0) when cosign version $1 is at or above the floor for its major
  # version. An empty or unparseable version, or a major newer than 3, counts as
  # capable, so an unrecognised build still attempts verification rather than
  # silently downgrading to checksum-only.
  major="${1%%.*}"
  case "$major" in
    '' | *[!0-9]*) return 0 ;;
    0 | 1) return 1 ;;
    2) floor="$COSIGN_MIN_V2" ;;
    3) floor="$COSIGN_MIN_V3" ;;
    *) return 0 ;;
  esac
  if version_lt "$1" "$floor"; then return 1; fi
  return 0
}

bool_env() { # <name> <value>: 0 for a truthy value, 1 for a falsy one, abort otherwise
  # Match both spellings explicitly rather than "anything but 0", which would read
  # NIC_REQUIRE_SIGNATURE=false as true and is the kind of surprise a security
  # control should not have.
  case "$2" in
    1 | true | yes | on) return 0 ;;
    0 | false | no | off | '') return 1 ;;
    *) fail "$1 must be one of 0/1, true/false, yes/no, on/off (got '$2')" ;;
  esac
}

# Write the pinned Sigstore trust root to $1. A copy next to this script (a
# checkout) is preferred, otherwise it comes from the repository's main branch.
# Either way the digest is checked, so the source is a convenience, not a trust
# decision.
fetch_trusted_root() {
  dest="$1"
  case "$0" in
    */install.sh) local_root="${0%/*}/trusted-roots/${TRUSTED_ROOT_SHA256}.json" ;;
    *) local_root="" ;;
  esac
  if [ -n "$local_root" ] && [ -f "$local_root" ]; then
    cp "$local_root" "$dest"
  else
    fetch -fsSL -m 60 -o "$dest" \
      "https://raw.githubusercontent.com/${NIC_REPO}/main/scripts/trusted-roots/${TRUSTED_ROOT_SHA256}.json" ||
      fail "could not fetch the pinned Sigstore trust root from raw.githubusercontent.com; refusing to install. If your network blocks that host, verify the release on a machine that can reach it (${DOCS_URL}) and re-run with NIC_EXPECTED_SHA256 set to the archive's digest."
  fi
  [ "$(sha256_of "$dest")" = "$TRUSTED_ROOT_SHA256" ] ||
    fail "the Sigstore trust root does not match the digest this installer pins; refusing to install. Please report it at https://github.com/${NIC_REPO}/issues."
}

# The error for a release below SIGNING_SINCE when a verifier is present. It
# names NIC_EXPECTED_SHA256 because there the premise is true: nothing about the
# release can prove it, so the proof has to come from the user.
fail_unsigned_release() { # <tag> <tarball> <what is missing>
  fail "$1 predates release signing (first signed release: v${SIGNING_SINCE}), so it has no $3, and this installer will not fall back to checksums.txt while a verifier is installed: 'this release predates signing' is what a forged release would claim too. To install $1 anyway, verify $2 yourself and re-run with NIC_EXPECTED_SHA256 set to its digest, taken from a source you trust and NOT from this release's checksums.txt. See ${DOCS_URL}"
}

# cosign verify-blob over checksums.txt, offline against the pinned trust root,
# with the identity pinned to the release workflow at this exact tag. Every
# outcome other than a clean verify is fatal.
verify_with_cosign() {
  tmp="$1"; tag="$2"; base="$3"; tarball="$4"

  # Capture the HTTP status so each failure gets an accurate message. Fetching
  # without -f/-S keeps curl from leaking its own error. 4xx is not retried by
  # --retry, so the code here is the final one.
  sig="${tmp}/checksums.txt.sigstore.json"
  code="$(fetch -sL -m 60 -o "$sig" -w '%{http_code}' "${base}/checksums.txt.sigstore.json")" || code=000
  case "$code" in
    200) ;;
    404)
      if version_lt "${tag#v}" "$SIGNING_SINCE"; then
        fail_unsigned_release "$tag" "$tarball" "signature bundle"
      fi
      fail "no signature bundle for ${tag} (HTTP 404), but every release from v${SIGNING_SINCE} on publishes one; refusing to install. The server answered, so this is not a network problem: a signature is absent from a release that should carry one, which is what removing it to suppress this check looks like. Do not work around it: falling back to checksums.txt would trust the same origin the signature is missing from. Please report it at https://github.com/${NIC_REPO}/issues and see ${DOCS_URL}."
      ;;
    *)
      fail "could not fetch the signature bundle for ${tag} (HTTP ${code}); refusing to install. If your network blocks this download, verify the release on a machine that can reach it (${DOCS_URL}) and re-run with NIC_EXPECTED_SHA256 set to the archive's digest."
      ;;
  esac

  fetch_trusted_root "${tmp}/trusted_root.json"

  # A bundle plus --trusted-root needs no network and writes nothing to
  # ~/.sigstore, so a failure below has one meaning: the signature does not
  # verify. cosign 2.x needs that spelled out: --offline keeps it from the
  # transparency-log lookup, and --new-bundle-format makes it refuse a
  # legacy-format bundle outright. 3.x does both already and warns that the
  # flags are deprecated, so they are passed to 2.x only. The identity is exact,
  # not a regexp, so nothing in the tag or NIC_REPO can act as a metacharacter.
  set --
  case "$(cosign_version)" in
    2.*) set -- --offline --new-bundle-format ;;
  esac
  if cosign verify-blob "$@" \
    --trusted-root "${tmp}/trusted_root.json" \
    --bundle "$sig" \
    --certificate-identity "https://github.com/${NIC_REPO}/.github/workflows/release.yml@refs/tags/${tag}" \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    "${tmp}/checksums.txt" >/dev/null; then
    log "Authenticity verified (cosign, pinned release-workflow identity)"
    return 0
  fi
  fail "the signature on checksums.txt for ${tag} did NOT verify (see cosign's error above); refusing to install. Verification ran offline against a pinned trust root, so this is not a network problem: the release assets do not match a signature from ${NIC_REPO}'s release workflow at ${tag}. Do not bypass this check. Please report it at https://github.com/${NIC_REPO}/issues and see ${DOCS_URL}."
}

# gh attestation verify over the archive. Only reached when gh is logged in, at
# which point gh is a capable verifier and its failure is fatal. gh answers a
# substituted archive and a never-attested release the same way (no attestation
# for that digest), so the two cannot be told apart and neither may degrade.
verify_with_gh() {
  tmp="$1"; tag="$2"; tarball="$3"
  # --signer-workflow pins the attestation to the same release workflow the
  # cosign branch pins its certificate identity to. Without it this accepts a
  # provenance statement from any workflow in the repo.
  if gh attestation verify "${tmp}/nic.tar.gz" --repo "${NIC_REPO}" \
    --signer-workflow "${NIC_REPO}/.github/workflows/release.yml" >/dev/null; then
    log "Authenticity verified (gh attestation, build provenance from the release workflow)"
    return 0
  fi
  if version_lt "${tag#v}" "$SIGNING_SINCE"; then
    fail_unsigned_release "$tag" "$tarball" "build-provenance attestation"
  fi
  fail "gh attestation verify found no build provenance from ${NIC_REPO}'s release workflow for ${tarball} (see gh's error above); refusing to install. gh reports a substituted archive the same way it reports a missing attestation, so this cannot be told apart from tampering. Do not bypass this check. If gh cannot reach api.github.com, install cosign (>= ${COSIGN_MIN_V2} on 2.x, >= ${COSIGN_MIN_V3} on 3.x), which verifies offline. Please report it at https://github.com/${NIC_REPO}/issues and see ${DOCS_URL}."
}

# Authenticity, from the first capable verifier: cosign at the floor, then a
# logged-in gh. With neither, the install continues on the checksum alone with a
# warning, unless NIC_REQUIRE_SIGNATURE asks for it to stop.
verify_authenticity() {
  tmp="$1"; tag="$2"; base="$3"; tarball="$4"

  if command -v cosign >/dev/null 2>&1; then
    cver="$(cosign_version)"
    if cosign_capable "$cver"; then
      verify_with_cosign "$tmp" "$tag" "$base" "$tarball"
      return 0
    fi
    cosign_note="cosign ${cver} is below the minimum this installer trusts (>= ${COSIGN_MIN_V2} on 2.x, >= ${COSIGN_MIN_V3} on 3.x; older builds are affected by GHSA-fx35-mq7g-6g98)"
  else
    cosign_note="cosign not found"
  fi

  if command -v gh >/dev/null 2>&1; then
    if gh auth status --hostname github.com >/dev/null 2>&1; then
      verify_with_gh "$tmp" "$tag" "$tarball"
      return 0
    fi
    gh_note="gh is not logged in"
  else
    gh_note="gh not found"
  fi

  if bool_env NIC_REQUIRE_SIGNATURE "$NIC_REQUIRE_SIGNATURE"; then
    fail "NIC_REQUIRE_SIGNATURE is set, but nothing on this host can verify ${tag} (${cosign_note}; ${gh_note}); refusing to install on the checksum alone. Install cosign (>= ${COSIGN_MIN_V2} on 2.x, >= ${COSIGN_MIN_V3} on 3.x) or log in to gh, or set NIC_EXPECTED_SHA256 to a digest from a source you trust. See ${DOCS_URL}"
  fi
  log "note: authenticity NOT verified (${cosign_note}; ${gh_note}); only the checksum is checked."
  log "      Install cosign (>= ${COSIGN_MIN_V2} on 2.x, >= ${COSIGN_MIN_V3} on 3.x) to verify the signature, or see ${DOCS_URL}"
  return 0
}

# The digest from NIC_EXPECTED_SHA256, lower-cased, or nothing when it is unset.
# Anything that is not 64 hex characters aborts: a mistyped pin must not be
# mistaken for no pin.
expected_sha256() {
  [ -n "$NIC_EXPECTED_SHA256" ] || return 0
  want="$(printf '%s' "$NIC_EXPECTED_SHA256" | tr 'ABCDEF' 'abcdef')"
  case "$want" in
    *[!0-9a-f]*) fail "NIC_EXPECTED_SHA256 must be a SHA-256 digest of 64 hex characters (got '${NIC_EXPECTED_SHA256}')" ;;
  esac
  [ "${#want}" -eq 64 ] ||
    fail "NIC_EXPECTED_SHA256 must be a SHA-256 digest of 64 hex characters (got ${#want} characters)"
  printf '%s' "$want"
}

main() {
  # Check both inputs before any download, so a typo aborts in a second rather
  # than after a large archive.
  pinned="$(expected_sha256)"
  bool_env NIC_REQUIRE_SIGNATURE "$NIC_REQUIRE_SIGNATURE" || :

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

  if [ -n "$pinned" ]; then
    # The user's digest is the verifier. Nothing else from the release is
    # consulted, so it works offline and for releases that predate signing.
    actual="$(sha256_of "${tmp}/nic.tar.gz")"
    [ "$pinned" = "$actual" ] ||
      fail "${tarball} does not match NIC_EXPECTED_SHA256 (expected ${pinned}, got ${actual}); refusing to install."
    log "Verified against NIC_EXPECTED_SHA256 (release checksums and signature not consulted)"
  else
    fetch -fsSL -m 60 "${base}/checksums.txt" -o "${tmp}/checksums.txt" ||
      fail "could not fetch checksums.txt for ${tag}"

    # Authenticity first (does the checksum list come from the release
    # workflow?), then integrity (does the tarball match the list?).
    verify_authenticity "$tmp" "$tag" "$base" "$tarball"

    expected="$(awk -v n="$tarball" '$2 == n {print $1}' "${tmp}/checksums.txt")"
    [ -n "$expected" ] || fail "no checksum entry for ${tarball} in checksums.txt"
    actual="$(sha256_of "${tmp}/nic.tar.gz")"
    [ "$expected" = "$actual" ] || fail "checksum mismatch (expected ${expected}, got ${actual})"
    log "Checksum verified"
  fi

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
