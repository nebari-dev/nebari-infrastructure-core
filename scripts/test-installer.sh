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

# --- 2. version_lt --------------------------------------------------------
# Drives both the cosign floor and the release-signing cutover. An unparseable
# version must compare as "not older", so an unknown build still verifies and an
# unknown tag is still expected to be signed.
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

check_version_lt 2.4.1     "$COSIGN_MIN"    yes   # below the cosign floor
check_version_lt 2.4.2     "$COSIGN_MIN"    no    # exactly the floor
check_version_lt 2.4.3     "$COSIGN_MIN"    no
check_version_lt 3.1.1     "$COSIGN_MIN"    no    # cosign 3.x parses correctly
check_version_lt 1.13.1    "$COSIGN_MIN"    yes
check_version_lt ""        "$COSIGN_MIN"    no    # fail safe: attempt verification
check_version_lt garbage   "$COSIGN_MIN"    no    # fail safe
check_version_lt 2.4.2-rc1 "$COSIGN_MIN"    no    # fail safe
check_version_lt 0.9.0     "$SIGNING_SINCE" yes   # predates signing
check_version_lt 0.10.0    "$SIGNING_SINCE" no    # first signed release
check_version_lt 0.13.0    "$SIGNING_SINCE" no
check_version_lt 1.0.0     "$SIGNING_SINCE" no    # a major bump is still signed

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

# --- 4. NIC_SKIP_SIGNATURE --------------------------------------------------
# "Anything but 0 means skip" would disable the check for the natural spellings
# of leaving it on, so the accepted values are explicit and anything else aborts.
skip_result() { # <value> -> prints skipped|verified|abort
  (
    NIC_SKIP_SIGNATURE="$1"
    verify_with_cosign() { printf 'verified\n'; }
    command() { case "$2" in cosign) return 0 ;; *) return 1 ;; esac; }
    cosign_version() { printf '3.1.1'; }
    out="$(verify_authenticity /tmp v0.13.0 https://example 2>&1)" || { printf 'abort\n'; exit 0; }
    case "$out" in
      *"skipped"*)  printf 'skipped\n' ;;
      *verified*)   printf 'verified\n' ;;
      *)            printf 'other:%s\n' "$out" ;;
    esac
  )
}
expect_skip() { # <value> <expected>
  local got; got="$(skip_result "$1")"
  if [[ $got == "$2" ]]; then ok "NIC_SKIP_SIGNATURE='$1' -> $got"
  else bad "NIC_SKIP_SIGNATURE='$1' -> $got, expected $2 (a security control must not turn off by accident)"; fi
}

expect_skip 1     skipped
expect_skip true  skipped
expect_skip yes   skipped
expect_skip 0     verified
expect_skip false verified
expect_skip no    verified
expect_skip ""    verified
expect_skip maybe abort

# --- 5. the signature decision table ----------------------------------------
# The core of the installer's threat model: which outcomes install and which
# abort. `fetch` returns the stubbed HTTP status, `cosign verify-blob` and
# `cosign initialize` return stubbed exit codes.
verify_case() { # <http_code> <verify_rc> <initialize_rc> <tag> -> prints install|abort, then the messages
  (
    STUB_CODE="$1" STUB_VERIFY="$2" STUB_INIT="$3"
    fetch() { printf '%s' "$STUB_CODE"; }
    cosign() {
      case "$1" in
        verify-blob) return "$STUB_VERIFY" ;;
        initialize)  return "$STUB_INIT" ;;
        *)           return 1 ;;
      esac
    }
    tmp="$(mktemp -d)"; : >"$tmp/checksums.txt"
    trap 'rm -rf "$tmp"' EXIT
    if out="$(verify_with_cosign "$tmp" "$4" https://example/base 2>&1)"; then
      printf 'install\n%s' "$out"
    else
      printf 'abort\n%s' "$out"
    fi
  )
}
expect_verify() { # <desc> <expected> <code> <vrc> <irc> <tag>
  local res got
  res="$(verify_case "$3" "$4" "$5" "$6")"
  got="${res%%$'\n'*}"
  if [[ $got == "$2" ]]; then ok "$1 -> $got"
  else bad "$1 -> $got, expected $2"; fi
  LAST_MSG="${res#*$'\n'}"
}

expect_verify "signed release, valid signature"        install 200 0 0 v0.13.0
expect_verify "pre-signing tag, bundle 404"            install 404 0 0 v0.9.0
expect_verify "signed tag, bundle 404 (suppression)"   abort   404 0 0 v0.13.0
expect_verify "first signed tag, bundle 404"           abort   404 0 0 v0.10.0
expect_verify "bundle fetch 500"                       abort   500 0 0 v0.13.0
expect_verify "bundle fetch transport failure"         abort   000 0 0 v0.13.0

expect_verify "invalid signature, trust root reachable" abort 200 1 0 v0.13.0
tampered_msg="$LAST_MSG"
expect_verify "verify fails, trust root unreachable"    abort 200 1 1 v0.13.0
airgap_msg="$LAST_MSG"

# The two failures above are the same cosign exit code and must not produce the
# same advice: telling a user to re-run with NIC_SKIP_SIGNATURE=1 is correct when
# the trust root is unreachable and is an invitation to install a tampered binary
# when it is not.
if [[ $tampered_msg == "$airgap_msg" ]]; then
  bad "a tampered signature and an unreachable trust root print an identical message; the user cannot tell them apart"
else
  ok "tampered and unreachable-trust-root failures print different messages"
fi
if [[ $tampered_msg == *NIC_SKIP_SIGNATURE* ]]; then
  bad "the invalid-signature message offers NIC_SKIP_SIGNATURE=1; following that advice installs the attacker's binary"
else
  ok "the invalid-signature message does not offer NIC_SKIP_SIGNATURE"
fi
if [[ $airgap_msg == *NIC_SKIP_SIGNATURE* ]]; then
  ok "the unreachable-trust-root message keeps the NIC_SKIP_SIGNATURE escape hatch"
else
  bad "the unreachable-trust-root message lost its escape hatch; air-gapped users have no documented way through"
fi

# --- report ------------------------------------------------------------------
if [[ $status -eq 0 ]]; then
  echo "installer tests OK: $pass assertions passed."
fi
exit $status
