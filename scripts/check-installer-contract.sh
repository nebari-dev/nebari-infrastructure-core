#!/usr/bin/env bash
# Fails if scripts/install.sh has drifted from the facts it hand-reimplements
# from .goreleaser.yml and the release workflow. install.sh cannot import the
# GoReleaser config, so it duplicates a handful of things that `goreleaser check`
# does NOT cross-check: the project name, the release archive name template and
# format, the amd64 -> x86_64 arch rename, the checksum filename, the signature
# bundle suffix, and the release workflow filename baked into the cosign identity
# regexp. A change to either side that is not mirrored in the other would leave
# CI green while breaking the installer for every user at once.
#
# Deliberately NOT covered, so the gaps are visible rather than assumed:
#   - This greps source text, so it cannot tell a live argument from a commented
#     one. It catches renames and reorderings, not a deletion that leaves the
#     string behind in a comment.
#   - It does not parse or execute install.sh. `sh -n` and a real install are a
#     separate concern (see #569 for shell linting).
#   - It runs in the `workflow-pins` job, which is NOT merge-blocking. It reports
#     drift; it does not by itself stop a merge.
#   - COSIGN_MIN in install.sh is also duplicated in
#     docs/operations/verifying-releases.md; that pair is checked below, but the
#     cosign-release pin in release.yml (the signer whose bundle format the floor
#     describes) is not, because a signer bump does not always move the floor.
set -euo pipefail

# Every path below is repo-relative, so anchor to the repo root rather than
# emitting five bogus "no longer pins ..." failures when run from elsewhere.
cd "$(dirname "$0")/.."

installer="scripts/install.sh"
goreleaser=".goreleaser.yml"
release_wf=".github/workflows/release.yml"
verifying_doc="docs/operations/verifying-releases.md"

status=0
fail() {
  echo "INSTALLER-CONTRACT: $1"
  status=1
}

# 1. The cosign identity regexp in install.sh pins the release workflow by
#    filename. If that workflow is renamed, every signed-install verification
#    fails with a confusing identity mismatch.
if ! grep -q 'workflows/release\\\.yml' "$installer"; then
  fail "$installer no longer pins .github/workflows/release.yml in its cosign identity regexp; did the regexp change?"
fi
if [[ ! -f "$release_wf" ]]; then
  fail "$installer pins '$release_wf' but that workflow does not exist; a rename breaks signed installs."
fi
# The same identity pin is documented for humans; keep the two in step.
if ! grep -q 'workflows/release\\\.yml' "$verifying_doc"; then
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
if ! grep -Eq "^[[:space:]]*name_template:[[:space:]]*'?checksums\.txt'?[[:space:]]*$" "$goreleaser"; then
  fail "$goreleaser no longer names the checksum file checksums.txt; $installer fetches that exact name and will fail."
fi

# 5. The signature bundle suffix, and that the checksum file is what gets signed.
#    This one matters most: install.sh derives the bundle URL as
#    checksums.txt.sigstore.json, and a 404 on that URL is the one drift that
#    DEGRADES to checksum-only instead of failing loudly. Renaming the signature
#    would quietly turn authenticity verification off for everyone.
if ! grep -q 'signature: "${artifact}.sigstore.json"' "$goreleaser"; then
  fail "$goreleaser no longer emits \${artifact}.sigstore.json; $installer derives the bundle URL from that suffix, and a missing bundle silently downgrades it to checksum-only."
fi
if ! grep -Eq '^[[:space:]]*artifacts:[[:space:]]*checksum[[:space:]]*$' "$goreleaser"; then
  fail "$goreleaser no longer signs the checksum artifact; $installer verifies the signature over checksums.txt, not over the archive."
fi

# 6. The cosign version floor is the same fact in install.sh and in the operator
#    docs. Moving one without the other tells users to install a cosign that
#    cannot read the bundle, or refuses one that can.
cosign_min="$(sed -n 's/^COSIGN_MIN="\([0-9.]*\)".*/\1/p' "$installer" | head -n1)"
if [[ -z $cosign_min ]]; then
  fail "$installer no longer defines COSIGN_MIN; the documented cosign floor can no longer be cross-checked."
elif ! grep -q "v${cosign_min}+" "$verifying_doc"; then
  fail "$verifying_doc does not state the cosign floor v${cosign_min}+ that $installer enforces via COSIGN_MIN."
fi

if [[ $status -eq 0 ]]; then
  echo "installer contract OK: project name, archive naming and format, arch rename, checksum and signature filenames, cosign floor, and release workflow filename are in sync."
fi
exit $status
