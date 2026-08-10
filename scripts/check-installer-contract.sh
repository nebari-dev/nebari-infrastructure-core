#!/usr/bin/env bash
# Fails if scripts/install.sh has drifted from the facts it hand-reimplements
# from .goreleaser.yml and the release workflow. install.sh cannot import the
# GoReleaser config, so it duplicates three things that `goreleaser check` does
# NOT cross-check: the release archive name template, the amd64 -> x86_64 arch
# rename, and the release workflow filename baked into the cosign identity
# regexp. A change to either side that is not mirrored in the other would leave
# CI green while breaking the installer for every user at once. This is the
# tripwire that fails in the PR that breaks it.
set -euo pipefail

installer="scripts/install.sh"
goreleaser=".goreleaser.yml"
release_wf=".github/workflows/release.yml"

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

# 2. install.sh builds the archive name as
#    nebari-infrastructure-core_<version>_<os>_<arch>.tar.gz. Assert GoReleaser
#    still emits that ProjectName_Version_Os_Arch order.
if ! grep -Pzoq '(?s)name_template:.*\{\{ \.ProjectName \}\}_.*\{\{-? ?\.Version \}\}_.*\{\{-? ?\.Os \}\}_' "$goreleaser"; then
  fail "$goreleaser archive name_template is no longer ProjectName_Version_Os_Arch; $installer builds that name by hand and will 404."
fi
# shellcheck disable=SC2016  # matching the literal ${version}_${suffix} text in install.sh, not expanding
if ! grep -q 'nebari-infrastructure-core_${version}_${suffix}' "$installer"; then
  fail "$installer no longer builds the nebari-infrastructure-core_<version>_<os>_<arch> archive name; keep it in sync with $goreleaser."
fi

# 3. The amd64 -> x86_64 rename. GoReleaser renames the arch in the template;
#    install.sh mirrors it in detect_suffix. If GoReleaser stops renaming,
#    install.sh would ask for _x86_64 while the asset is _amd64.
if ! grep -q 'if eq .Arch "amd64" }}x86_64' "$goreleaser"; then
  fail "$goreleaser no longer renames amd64 -> x86_64; $installer's detect_suffix still does, so the names will diverge."
fi
if ! grep -q 'x86_64' "$installer"; then
  fail "$installer no longer maps to x86_64; keep detect_suffix in sync with $goreleaser."
fi

if [[ $status -eq 0 ]]; then
  echo "installer contract OK: archive naming, arch rename, and release workflow filename are in sync."
fi
exit $status
