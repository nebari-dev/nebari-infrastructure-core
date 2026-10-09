#!/usr/bin/env bash
# Offline tests for scripts/install.sh.
#
# install.sh is a published entry point: the README and every release's notes
# point users at it, and a merge to main reaches them with no release gate in
# between. Its security-relevant behaviour is a decision table -- which signature
# outcomes install and which abort -- and that table is exactly the kind of thing
# that regresses silently under a well-meaning edit. This asserts it.
#
# The installer is sourced with NIC_INSTALL_SH_SOURCE_ONLY=1 so the real
# functions are under test rather than a copy, then `fetch`, `cosign`, `gh` and
# `uname` are replaced with stubs. Nothing here touches the network, so it runs
# in the same checkout-only CI job as the other repo-hygiene checks.
#
# Deliberately NOT covered:
#   - A real end-to-end install. That needs the network and a published release.
#   - Real cosign behaviour. The stubs assert how install.sh REACTS to cosign's
#     exit codes, not that cosign itself is correct.
#   - Bashisms. The `-n` checks below are parse-only, and a bashism like
#     `[[ x ]]` parses fine under dash because `[[` is a valid command NAME --
#     it only fails when executed. Catching those needs shellcheck, tracked
#     in #569; do not read a green run here as proof of POSIX compliance.
#
# Run: ./scripts/test-installer.sh
#
# The stubs below (fetch, cosign, gh, uname, ...) and the variables they set
# (NIC_REQUIRE_SIGNATURE, INSTALL_DIR, ...) are consumed by the sourced
# installer, which shellcheck cannot follow, so it reports them as unused.
# shellcheck disable=SC2034,SC2329
set -euo pipefail

cd "$(dirname "$0")/.."
installer="scripts/install.sh"

pass=0
status=0
ok()   { printf '  ok    %s\n' "$1"; pass=$((pass + 1)); }
bad()  { printf 'INSTALLER-TEST: %s\n' "$1"; status=1; }

# --- 1. the script parses under every shell it claims to support -------------
# install.sh is POSIX sh because it is piped into whatever shell the user has.
for shell in sh dash bash "busybox ash"; do
  cmd=${shell%% *}
  command -v "$cmd" >/dev/null 2>&1 || { printf '  skip  %s not installed\n' "$cmd"; continue; }
  if $shell -n "$installer" 2>/dev/null; then
    ok "$shell parses $installer"
  else
    bad "$installer is not valid $shell; it is piped into arbitrary shells, so this breaks real installs"
  fi
done

# --- 2. version_lt and the cosign floor --------------------------------------
# version_lt drives the release-signing cutover; cosign_capable applies the
# per-major cosign floor. An unparseable version must count as "not older" and
# "capable", so an unknown build still verifies and an unknown tag is still
# expected to be signed.
# shellcheck source=scripts/install.sh
NIC_INSTALL_SH_SOURCE_ONLY=1 . "./$installer"

check_version_lt() { # <a> <b> <expected: yes|no>
  if version_lt "$1" "$2"; then got=yes; else got=no; fi
  if [[ $got == "$3" ]]; then
    ok "version_lt('${1:-<empty>}', '$2') = $got"
  else
    bad "version_lt('${1:-<empty>}', '$2') = $got, expected $3"
  fi
}

check_version_lt 0.9.0       "$SIGNING_SINCE" yes   # predates signing
check_version_lt 0.9.99      "$SIGNING_SINCE" yes
check_version_lt 0.10.0      "$SIGNING_SINCE" no    # first signed release
check_version_lt 0.14.0      "$SIGNING_SINCE" no
check_version_lt 1.0.0       "$SIGNING_SINCE" no    # a major bump is still signed
check_version_lt 0.14.0-rc.1 "$SIGNING_SINCE" no    # fail safe: expected to be signed
check_version_lt ""          "$SIGNING_SINCE" no    # fail safe

# GHSA-fx35-mq7g-6g98 is fixed in 2.6.5 and 3.1.3. 3.0.0 through 3.1.2 sort
# above 2.6.5 and are still affected, which is why one floor cannot work.
check_cosign() { # <version> <expected: yes|no>
  if cosign_capable "$1"; then got=yes; else got=no; fi
  if [[ $got == "$2" ]]; then
    ok "cosign_capable('${1:-<empty>}') = $got"
  else
    bad "cosign_capable('${1:-<empty>}') = $got, expected $2 (GHSA-fx35-mq7g-6g98: 2.x needs >= $COSIGN_MIN_V2, 3.x needs >= $COSIGN_MIN_V3)"
  fi
}

check_cosign 1.13.1    no
check_cosign 2.4.2     no    # reads the bundle format, but affected
check_cosign 2.6.4     no    # last affected 2.x
check_cosign 2.6.5     yes   # first fixed 2.x
check_cosign 2.7.0     yes
check_cosign 3.0.0     no    # newer than 2.6.5, still affected
check_cosign 3.1.2     no    # last affected 3.x
check_cosign 3.1.3     yes   # first fixed 3.x
check_cosign 3.2.0     yes
check_cosign 4.0.0     yes   # a later major postdates both fixes
check_cosign ""        yes   # fail safe: attempt verification
check_cosign garbage   yes   # fail safe

# --- 3. detect_suffix ------------------------------------------------------
# Mirrors the arch rename in .goreleaser.yml; check-installer-contract.sh
# asserts the two stay in step, this asserts the mapping itself.
# Run detect_suffix in a subshell with uname stubbed.
suffix_of() { # <os> <arch>
  ( uname() { case "$1" in -s) printf '%s' "$STUB_OS" ;; -m) printf '%s' "$STUB_ARCH" ;; esac; }
    STUB_OS="$1" STUB_ARCH="$2"
    detect_suffix )
}
expect_suffix() { # <os> <arch> <expected|ABORT>
  local out rc
  out="$(suffix_of "$1" "$2" 2>&1)" && rc=0 || rc=$?
  if [[ $3 == ABORT ]]; then
    if [[ ${rc:-0} -ne 0 ]]; then ok "detect_suffix($1,$2) aborts"
    else bad "detect_suffix($1,$2) returned '$out', expected an abort"; fi
  elif [[ ${rc:-0} -eq 0 && $out == "$3" ]]; then
    ok "detect_suffix($1,$2) = $3"
  else
    bad "detect_suffix($1,$2) = '$out' (rc=${rc:-0}), expected '$3'"
  fi
}

expect_suffix Linux  x86_64  linux_x86_64
expect_suffix Linux  amd64   linux_x86_64
expect_suffix Linux  aarch64 linux_arm64
expect_suffix Darwin arm64   darwin_arm64
expect_suffix Darwin x86_64  darwin_x86_64
expect_suffix Linux  riscv64 ABORT
expect_suffix MINGW64_NT x86_64 ABORT   # Windows users get the .zip

# --- 4. inputs ------------------------------------------------------------------
# NIC_REQUIRE_SIGNATURE: "anything but 0 means on" would read =false as on, so
# both spellings are explicit and anything else aborts. NIC_EXPECTED_SHA256: a
# mistyped pin must abort rather than read as "no pin".
bool_result() { # <value> -> on|off|abort
  local out rc
  out="$(bool_env NIC_REQUIRE_SIGNATURE "$1" 2>&1)" && rc=0 || rc=$?
  if [[ $out == *error:* ]]; then echo abort
  elif [[ $rc -eq 0 ]]; then echo on
  else echo off; fi
}
expect_bool() { # <value> <expected>
  local got; got="$(bool_result "$1")"
  if [[ $got == "$2" ]]; then ok "NIC_REQUIRE_SIGNATURE='$1' -> $got"
  else bad "NIC_REQUIRE_SIGNATURE='$1' -> $got, expected $2 (a security control must not flip by accident)"; fi
}
expect_bool 1     on
expect_bool true  on
expect_bool yes   on
expect_bool 0     off
expect_bool false off
expect_bool no    off
expect_bool ""    off
expect_bool maybe abort

pin_result() { # <value> -> the normalised digest, <none>, or abort
  local out
  if out="$(NIC_EXPECTED_SHA256="$1"; expected_sha256 2>&1)"; then printf '%s' "${out:-<none>}"
  else printf 'abort'; fi
}
expect_pin() { # <desc> <value> <expected>
  local got; got="$(pin_result "$2")"
  if [[ $got == "$3" ]]; then ok "NIC_EXPECTED_SHA256 $1 -> ${got:0:16}"
  else bad "NIC_EXPECTED_SHA256 $1 -> $got, expected $3"; fi
}
digest="$(printf 'ab%.0s' {1..32})"
expect_pin "unset"                ""                          "<none>"
expect_pin "lower-case digest"    "$digest"                   "$digest"
expect_pin "upper-case digest"    "${digest^^}"               "$digest"
expect_pin "63 characters"        "${digest:1}"               abort
expect_pin "non-hex character"    "${digest:1}g"              abort
expect_pin "with a filename"      "$digest  nic.tar.gz"       abort

# --- 5. the authenticity decision table ----------------------------------------
# Which outcomes install and which abort. `command -v` reports which of cosign/gh
# exist, cosign_version the cosign build, `fetch` the bundle's HTTP status, and
# `cosign verify-blob` / `gh` their exit codes. The rule under test: with no
# capable verifier the install degrades to checksum-only (or stops under
# NIC_REQUIRE_SIGNATURE); with one, nothing degrades, for any tag.
auth_case() { # <cosign: absent|VERSION> <gh: absent|loggedout|pass|fail> <bundle http code> <verify rc> <require> <tag>
  (
    STUB_COSIGN="$1" STUB_GH="$2" STUB_CODE="$3" STUB_VERIFY="$4"
    NIC_REQUIRE_SIGNATURE="$5"
    command() {
      if [[ $1 == -v ]]; then
        case "$2" in
          cosign) [[ $STUB_COSIGN != absent ]]; return ;;
          gh)     [[ $STUB_GH != absent ]]; return ;;
        esac
      fi
      builtin command "$@"
    }
    cosign_version() { printf '%s' "$STUB_COSIGN"; }
    fetch() { printf '%s' "$STUB_CODE"; }
    fetch_trusted_root() { :; }
    cosign() {
      [[ $1 == verify-blob ]] && printf 'COSIGN-ARGS: %s\n' "$*" >&2
      return "$STUB_VERIFY"
    }
    gh() {
      case "$1" in
        auth)        [[ $STUB_GH == pass || $STUB_GH == fail ]] ;;
        attestation) [[ $STUB_GH == pass ]] ;;
        *)           return 1 ;;
      esac
    }
    tmp="$(mktemp -d)"; : >"$tmp/checksums.txt"
    trap 'rm -rf "$tmp"' EXIT
    if out="$(verify_authenticity "$tmp" "$6" https://example/base nic_test.tar.gz 2>&1)"; then
      printf 'install\n%s' "$out"
    else
      printf 'abort\n%s' "$out"
    fi
  )
}
expect_auth() { # <desc> <expected> <auth_case args...>
  local res got
  res="$(auth_case "${@:3}")"
  got="${res%%$'\n'*}"
  if [[ $got == "$2" ]]; then ok "$1 -> $got"
  else bad "$1 -> $got, expected $2"; fi
  LAST_MSG="${res#*$'\n'}"
}
#                                                                  cosign  gh        code verify require tag
expect_auth "cosign, signed release, valid signature"     install 3.1.3  absent    200  0      0       v0.14.0
valid_msg="$LAST_MSG"
expect_auth "cosign 2.x at the floor, valid signature"    install 2.6.5  absent    200  0      0       v0.14.0
valid_v2_msg="$LAST_MSG"
expect_auth "cosign, signature does not verify"           abort   3.1.3  absent    200  1      0       v0.14.0
tampered_msg="$LAST_MSG"
expect_auth "cosign rejects; a passing gh cannot override" abort  3.1.3  pass      200  1      0       v0.14.0
expect_auth "cosign, signed tag, bundle 404 (suppression)" abort  3.1.3  absent    404  0      0       v0.14.0
suppressed_msg="$LAST_MSG"
expect_auth "cosign, first signed tag, bundle 404"        abort   3.1.3  absent    404  0      0       v0.10.0
expect_auth "cosign, pre-signing tag, bundle 404"         abort   3.1.3  absent    404  0      0       v0.9.0
unsigned_msg="$LAST_MSG"
# A release named below the cutover and marked latest is the default
# `curl | sh` path; contents: write is enough to publish one.
expect_auth "cosign, forged pre-signing tag, bundle 404"  abort   3.1.3  absent    404  0      0       v0.9.99
expect_auth "cosign, bundle fetch 500"                    abort   3.1.3  absent    500  0      0       v0.14.0
fetch_failed_msg="$LAST_MSG"
expect_auth "cosign, bundle fetch transport failure"      abort   3.1.3  absent    000  0      0       v0.14.0

expect_auth "affected cosign 2.6.4 alone: checksum-only"  install 2.6.4  absent    200  0      0       v0.14.0
affected_msg="$LAST_MSG"
expect_auth "affected cosign 3.1.2 alone: checksum-only"  install 3.1.2  absent    200  0      0       v0.14.0
expect_auth "affected cosign, NIC_REQUIRE_SIGNATURE=1"    abort   3.1.2  absent    200  0      1       v0.14.0
expect_auth "affected cosign, gh logged in and passing"   install 2.6.4  pass      200  0      0       v0.14.0

expect_auth "no verifier: checksum-only"                  install absent absent    200  0      0       v0.14.0
expect_auth "no verifier, pre-signing tag: checksum-only" install absent absent    404  0      0       v0.9.0
expect_auth "no verifier, NIC_REQUIRE_SIGNATURE=1"        abort   absent absent    200  0      1       v0.14.0
require_msg="$LAST_MSG"
expect_auth "gh logged out: checksum-only"                install absent loggedout 200  0      0       v0.14.0
expect_auth "gh logged out, NIC_REQUIRE_SIGNATURE=1"      abort   absent loggedout 200  0      1       v0.14.0
expect_auth "gh logged in, attestation verifies"          install absent pass      200  0      0       v0.14.0
expect_auth "gh logged in, attestation fails"             abort   absent fail      200  0      0       v0.14.0
gh_failed_msg="$LAST_MSG"
expect_auth "gh logged in, pre-signing tag"               abort   absent fail      200  0      0       v0.9.0
gh_unsigned_msg="$LAST_MSG"

# What each message tells the user to do matters as much as the exit code: a
# pinned digest is offered only where the premise holds (the release cannot
# prove itself, or the network failed), never where the server answered and the
# signature is absent or wrong, since a user copying the digest from the same
# release's checksums.txt would install the attacker's binary.
expect_msg() { # <desc> <message> <needle> <yes|no>
  if [[ $2 == *"$3"* ]]; then got=yes; else got=no; fi
  if [[ $got == "$4" ]]; then ok "$1"
  else bad "$1 (looked for '$3' in: ${2:0:200})"; fi
}
expect_msg "the invalid-signature message does not offer NIC_EXPECTED_SHA256"   "$tampered_msg"     NIC_EXPECTED_SHA256 no
expect_msg "the suppressed-signature message does not offer NIC_EXPECTED_SHA256" "$suppressed_msg"  NIC_EXPECTED_SHA256 no
expect_msg "the gh-attestation failure does not offer NIC_EXPECTED_SHA256"      "$gh_failed_msg"    NIC_EXPECTED_SHA256 no
expect_msg "the pre-signing message names NIC_EXPECTED_SHA256"                  "$unsigned_msg"     NIC_EXPECTED_SHA256 yes
expect_msg "the pre-signing message warns off this release's checksums.txt"     "$unsigned_msg"     "NOT from this release's checksums.txt" yes
expect_msg "the gh pre-signing message names NIC_EXPECTED_SHA256"               "$gh_unsigned_msg"  NIC_EXPECTED_SHA256 yes
expect_msg "the bundle-fetch-failure message names NIC_EXPECTED_SHA256"         "$fetch_failed_msg" NIC_EXPECTED_SHA256 yes
expect_msg "the NIC_REQUIRE_SIGNATURE message names NIC_EXPECTED_SHA256"        "$require_msg"      NIC_EXPECTED_SHA256 yes
expect_msg "an affected cosign is named with its advisory"                      "$affected_msg"     GHSA-fx35-mq7g-6g98 yes
expect_msg "an affected cosign is never asked to verify"                        "$affected_msg"     COSIGN-ARGS no

# The verify-blob invocation itself: offline against the pinned root, with the
# exact identity at this tag rather than a regexp. cosign 2.x needs --offline and
# --new-bundle-format spelled out (the latter is what closes the advisory there);
# 3.x behaves that way by default and warns that both flags are deprecated.
identity="--certificate-identity https://github.com/${NIC_REPO}/.github/workflows/release.yml@refs/tags/v0.14.0 "
expect_msg "cosign 2.x verify-blob runs --offline"            "$valid_v2_msg" "--offline"           yes
expect_msg "cosign 2.x verify-blob requires --new-bundle-format" "$valid_v2_msg" "--new-bundle-format" yes
expect_msg "cosign 2.x verify-blob uses the pinned --trusted-root" "$valid_v2_msg" "--trusted-root" yes
expect_msg "cosign 3.x verify-blob omits the deprecated --offline"  "$valid_msg" "--offline"           no
expect_msg "cosign 3.x verify-blob omits the deprecated --new-bundle-format" "$valid_msg" "--new-bundle-format" no
expect_msg "verify-blob uses the pinned --trusted-root"     "$valid_msg" "--trusted-root"        yes
expect_msg "verify-blob pins the exact identity at the tag" "$valid_msg" "$identity"             yes
expect_msg "verify-blob does not use an identity regexp"    "$valid_msg" "identity-regexp"       no

# --- 6. the pinned trust root -------------------------------------------------
# The trust root is fetched by digest and checked against it, so a wrong or
# missing file aborts rather than verifying against something else.
root_case() { # <good|bad|fail> -> ok|abort, then the messages
  (
    STUB_ROOT="$1"
    fetch() {
      local out=""
      while [[ $# -gt 0 ]]; do [[ $1 == -o ]] && { out="$2"; shift; }; shift; done
      case "$STUB_ROOT" in
        good) cp "scripts/trusted-roots/${TRUSTED_ROOT_SHA256}.json" "$out" ;;
        bad)  printf '{}\n' >"$out" ;;
        *)    return 22 ;;
      esac
    }
    d="$(mktemp -d)"; trap 'rm -rf "$d"' EXIT
    if out="$(fetch_trusted_root "$d/root.json" 2>&1)"; then printf 'ok\n%s' "$out"
    else printf 'abort\n%s' "$out"; fi
  )
}
expect_root() { # <stub> <expected>
  local res got; res="$(root_case "$1")"; got="${res%%$'\n'*}"
  if [[ $got == "$2" ]]; then ok "trust root fetch '$1' -> $got"
  else bad "trust root fetch '$1' -> $got, expected $2"; fi
  LAST_MSG="${res#*$'\n'}"
}
expect_root good ok
expect_root bad  abort
expect_root fail abort
expect_msg "the trust-root fetch failure names NIC_EXPECTED_SHA256" "$LAST_MSG" NIC_EXPECTED_SHA256 yes

# --- 7. main() with a pinned digest --------------------------------------------
# A pinned digest is a verifier of its own: the archive is compared against it
# and nothing else from the release is fetched, which is what lets a pre-signing
# tag install while cosign is present. A stand-in archive is served by `fetch`.
pin_case() { # <right|wrong> -> install|abort, fetched=<non-archive fetches>, installed=yes|no
  (
    work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
    mkdir "$work/pkg" "$work/bin"
    printf '#!/bin/sh\necho stub\n' >"$work/pkg/nic"; chmod +x "$work/pkg/nic"
    tar -czf "$work/archive.tar.gz" -C "$work/pkg" nic
    : >"$work/fetched"
    if [[ $1 == right ]]; then NIC_EXPECTED_SHA256="$(sha256_of "$work/archive.tar.gz")"
    else NIC_EXPECTED_SHA256="$(printf '0%.0s' {1..64})"; fi
    NIC_VERSION=v0.9.0
    INSTALL_DIR="$work/bin"
    uname() { case "$1" in -s) printf Linux ;; -m) printf x86_64 ;; esac; }
    fetch() {
      local out="" url=""
      while [[ $# -gt 0 ]]; do
        case "$1" in -o) out="$2"; shift ;; https://*) url="$1" ;; esac
        shift
      done
      printf '%s\n' "$url" >>"$work/fetched"
      [[ $url == *.tar.gz ]] && cp "$work/archive.tar.gz" "$out"
    }
    if out="$(main 2>&1)"; then r=install; else r=abort; fi
    others="$(grep -cv '\.tar\.gz$' "$work/fetched" || true)"
    if [[ -x $work/bin/nic ]]; then installed=yes; else installed=no; fi
    printf '%s fetched=%s installed=%s' "$r" "$others" "$installed"
  )
}
expect_pin_install() { # <right|wrong> <expected summary>
  local got; got="$(pin_case "$1")"
  if [[ $got == "$2" ]]; then ok "pinned digest ($1) -> $got"
  else bad "pinned digest ($1) -> $got, expected $2"; fi
}
expect_pin_install right "install fetched=0 installed=yes"
expect_pin_install wrong "abort fetched=0 installed=no"

# --- report ------------------------------------------------------------------
if [[ $status -eq 0 ]]; then
  echo "installer tests OK: $pass assertions passed."
fi
exit $status
