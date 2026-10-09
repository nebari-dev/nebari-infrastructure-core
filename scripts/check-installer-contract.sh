#!/usr/bin/env bash
# Fails if scripts/install.sh has drifted from the facts it hand-reimplements
# from .goreleaser.yml and the release workflow. install.sh cannot import the
# GoReleaser config, so it duplicates a handful of things that `goreleaser check`
# does NOT cross-check: the project name, the release archive name template and
# format, the amd64 -> x86_64 arch rename, the checksum filename, the signature
# bundle suffix, and the release workflow filename baked into the cosign
# identity. A change to either side that is not mirrored in the other would leave
# CI green while breaking the installer for every user at once. It also checks
# the facts install.sh shares with the docs (the cosign floor) and with the repo
# (the pinned trust root file).
#
# Deliberately NOT covered, so the gaps are visible rather than assumed:
#   - This greps source text, so it cannot tell a live argument from a commented
#     one. It catches renames and reorderings, not a deletion that leaves the
#     string behind in a comment.
#   - It does not parse or execute install.sh. `sh -n` and a real install are a
#     separate concern (see #569 for shell linting).
#   - The per-major cosign floor in install.sh is also stated in
#     docs/operations/verifying-releases.md and README.md; those are checked
#     below, but the cosign-release pin in release.yml (the signer) is not,
#     because a signer bump does not move the verifier floor.
#   - The pinned trust root is checked for existence and digest only. Whether a
#     new release still verifies against it after a Sigstore key rotation needs
#     the release itself and the network; the check-installer job in release.yml
#     covers that by installing each release with main's installer.
#
# It runs in the merge-blocking `Test` job, next to test-installer.sh: since a
# missing bundle on a signed release is fatal, a drift here breaks every cosign
# user's install rather than degrading it.
set -euo pipefail

# Every path below is repo-relative, so anchor to the repo root rather than
# emitting five bogus "no longer pins ..." failures when run from elsewhere.
cd "$(dirname "$0")/.."

installer="scripts/install.sh"
goreleaser=".goreleaser.yml"
release_wf=".github/workflows/release.yml"
verifying_doc="docs/operations/verifying-releases.md"
readme="README.md"

status=0
fail() {
  echo "INSTALLER-CONTRACT: $1"
  status=1
}

# 1. The cosign identity in install.sh pins the release workflow by filename.
#    If that workflow is renamed, every signed-install verification fails with a
#    confusing identity mismatch.
if ! grep -q 'workflows/release\.yml@refs/tags/' "$installer"; then
  fail "$installer no longer pins .github/workflows/release.yml@refs/tags/<tag> as its cosign identity; did the identity change?"
fi
if [[ ! -f "$release_wf" ]]; then
  fail "$installer pins '$release_wf' but that workflow does not exist; a rename breaks signed installs."
fi
# The same identity pin is documented for humans; keep the two in step.
if ! grep -q 'workflows/release\.yml@refs/tags/' "$verifying_doc"; then
  fail "$verifying_doc no longer documents the .github/workflows/release.yml identity pin that $installer enforces."
fi

# 2. install.sh builds the archive name as
#    nebari-infrastructure-core_<version>_<os>_<arch>.tar.gz. Assert GoReleaser
#    still emits that ProjectName_Version_Os_Arch order.
#
#    The name_template spans several lines, so flatten the file first. Using
#    `tr` + `grep -E` rather than `grep -Pzo` keeps this working on the BSD grep
#    a macOS contributor has locally; -P is a GNU extension.
if ! tr -d '\n' <"$goreleaser" |
  grep -Eq '\{\{ \.ProjectName \}\}_ *\{\{-? ?\.Version \}\}_ *\{\{-? ?\.Os \}\}_'; then
  fail "$goreleaser archive name_template is no longer ProjectName_Version_Os_Arch; $installer builds that name by hand and will 404."
fi
# shellcheck disable=SC2016  # matching the literal ${version}_${suffix} text in install.sh, not expanding
if ! grep -q 'nebari-infrastructure-core_${version}_${suffix}' "$installer"; then
  fail "$installer no longer builds the nebari-infrastructure-core_<version>_<os>_<arch> archive name; keep it in sync with $goreleaser."
fi
#    .ProjectName has no `project_name:` key today, so it resolves from
#    release.github.name. Setting project_name (or renaming the release repo)
#    silently renames every asset while install.sh keeps asking for the long
#    name, and the release-notes template self-updates so nothing else notices.
if grep -q '^project_name:' "$goreleaser"; then
  if ! grep -q '^project_name: nebari-infrastructure-core[[:space:]]*$' "$goreleaser"; then
    fail "$goreleaser sets project_name to something other than nebari-infrastructure-core; $installer hardcodes the long name in its archive URL and will 404."
  fi
elif ! grep -q '^[[:space:]]*name: nebari-infrastructure-core[[:space:]]*$' "$goreleaser"; then
  fail "$goreleaser no longer resolves .ProjectName to nebari-infrastructure-core (no project_name key and release.github.name changed); $installer hardcodes that name and will 404."
fi

# 3. The amd64 -> x86_64 rename. GoReleaser renames the arch in the template;
#    install.sh mirrors it in detect_suffix. If GoReleaser stops renaming,
#    install.sh would ask for _x86_64 while the asset is _amd64.
if ! grep -q 'if eq .Arch "amd64" }}x86_64' "$goreleaser"; then
  fail "$goreleaser no longer renames amd64 -> x86_64; $installer's detect_suffix still does, so the names will diverge."
fi
#    Match the assignment, not just the string: the bare word also appears in the
#    case pattern, so grepping for it alone passes even if the rename is dropped.
if ! grep -q 'arch="x86_64"' "$installer"; then
  fail "$installer's detect_suffix no longer assigns x86_64; keep it in sync with $goreleaser."
fi

# 4. The archive format and the checksum filename. install.sh hardcodes
#    ".tar.gz" and "checksums.txt" in the URLs it builds; either change 404s.
if ! grep -Eq '^[[:space:]]*formats:[[:space:]]*\[[[:space:]]*tar\.gz[[:space:]]*\]' "$goreleaser"; then
  fail "$goreleaser no longer produces tar.gz archives; $installer hardcodes the .tar.gz suffix and will 404."
fi
#    The default format above only holds where no override replaces it, so the
#    overrides must name windows and nothing else: an override flipped to
#    `goos: linux` would ship Linux as zip while the line above still passes.
override_goos="$(awk '
  /^[[:space:]]*format_overrides:/ { f = 1; ind = match($0, /[^ ]/); next }
  f && NF { if (match($0, /[^ ]/) <= ind) exit; if (match($0, /goos:[[:space:]]*[^[:space:]]+/)) print substr($0, RSTART + 5) }
' "$goreleaser" | tr -d ' "' | sort -u | tr '\n' ' ')"
if [[ $override_goos != "windows " ]]; then
  fail "$goreleaser format_overrides applies to '${override_goos% }', not only windows; $installer expects tar.gz on linux and darwin and will 404."
fi
if ! grep -Eq "^[[:space:]]*name_template:[[:space:]]*'?checksums\.txt'?[[:space:]]*$" "$goreleaser"; then
  fail "$goreleaser no longer names the checksum file checksums.txt; $installer fetches that exact name and will fail."
fi

# 5. The signature bundle suffix, and that the checksum file is what gets signed.
#    install.sh derives the bundle URL as checksums.txt.sigstore.json, and a 404
#    on that URL is fatal for every user with a capable cosign. Renaming the
#    signature would make every such install fail with a message that reads as
#    tampering, while users without cosign install unverified as before.
# shellcheck disable=SC2016  # matching GoReleaser's literal ${artifact} template, not expanding
if ! grep -q 'signature: "${artifact}.sigstore.json"' "$goreleaser"; then
  fail "$goreleaser no longer emits \${artifact}.sigstore.json; $installer derives the bundle URL from that suffix, so every install with cosign present would fail as if the signature had been removed."
fi
if ! grep -Eq '^[[:space:]]*artifacts:[[:space:]]*checksum[[:space:]]*$' "$goreleaser"; then
  fail "$goreleaser no longer signs the checksum artifact; $installer verifies the signature over checksums.txt, not over the archive."
fi

# 6. The cosign floor, per major version, is the same fact in install.sh, the
#    operator docs and the README. Moving one without the others tells users to
#    trust a cosign that GHSA-fx35-mq7g-6g98 affects, or refuses one that is fixed.
for major in 2 3; do
  floor="$(sed -n "s/^COSIGN_MIN_V${major}=\"\([0-9.]*\)\".*/\1/p" "$installer" | head -n1)"
  if [[ -z $floor ]]; then
    fail "$installer no longer defines COSIGN_MIN_V${major}; the documented cosign floor can no longer be cross-checked."
    continue
  fi
  for doc in "$verifying_doc" "$readme"; do
    if ! grep -q "v${floor}+" "$doc"; then
      fail "$doc does not state the cosign ${major}.x floor v${floor}+ that $installer enforces via COSIGN_MIN_V${major}."
    fi
  done
done

# 7. The pinned trust root. install.sh fetches scripts/trusted-roots/<digest>.json
#    from main and refuses anything that does not hash to <digest>, so the file
#    must exist and must not have been edited in place: a root is replaced by
#    adding a new file, never by changing one an older installer pins.
root_sha="$(sed -n 's/^TRUSTED_ROOT_SHA256="\([0-9a-f]*\)".*/\1/p' "$installer" | head -n1)"
if [[ -z $root_sha ]]; then
  fail "$installer no longer defines TRUSTED_ROOT_SHA256."
elif [[ ! -f scripts/trusted-roots/${root_sha}.json ]]; then
  fail "$installer pins trust root ${root_sha} but scripts/trusted-roots/${root_sha}.json does not exist; every cosign install would fail."
fi
for root in scripts/trusted-roots/*.json; do
  [[ -e $root ]] || continue
  name="$(basename "$root" .json)"
  actual="$(sha256sum "$root" 2>/dev/null || shasum -a 256 "$root")"
  if [[ ${actual%% *} != "$name" ]]; then
    fail "$root does not hash to its name; trust roots are content-addressed and must never be edited, add a new file instead."
  fi
done

if [[ $status -eq 0 ]]; then
  echo "installer contract OK: project name, archive naming and format, arch rename, checksum and signature filenames, cosign floor, trust root, and release workflow filename are in sync."
fi
exit $status
