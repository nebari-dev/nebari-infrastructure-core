#!/usr/bin/env bash
# Fails if any workflow references a GitHub Action or reusable workflow by a
# mutable ref (a tag/branch instead of a 40-char commit SHA), or if the
# GoReleaser action version floats (latest/nightly).
set -euo pipefail

workflows=(
  .github/workflows/release.yml
  .github/workflows/ci.yml
  .github/workflows/add-to-project.yaml
  .github/workflows/opentofu-lockfile-pr.yml
)

status=0

for wf in "${workflows[@]}"; do
  # Every `uses:` value must be owner/repo(/path)?@<40-hex>. Local (./) refs are allowed.
  while IFS= read -r line; do
    ref="${line#*@}"
    if [[ ! "$ref" =~ ^[0-9a-f]{40}([[:space:]].*)?$ ]]; then
      echo "UNPINNED: $wf -> $line"
      status=1
    fi
  done < <(grep -oE 'uses:[[:space:]]*[^./][^@[:space:]]+@[^[:space:]]+' "$wf" | sed 's/uses:[[:space:]]*//' || true)
done

# GoReleaser binary version must not float.
if grep -qE "version:[[:space:]]*['\"]?(latest|nightly)" .github/workflows/release.yml; then
  echo "UNPINNED: GoReleaser version is latest/nightly in release.yml"
  status=1
fi

if [[ "$status" -eq 0 ]]; then
  echo "All actions and reusable workflows are pinned to commit SHAs."
fi
exit "$status"
