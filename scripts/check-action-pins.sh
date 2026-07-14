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
  if [[ ! -f "$wf" ]]; then
    echo "MISSING: $wf (expected workflow file not found)"
    status=1
    continue
  fi
  # Match only remote refs shaped owner/repo(/subpath)?@ref. The leading alnum
  # class means local refs (./.github/actions/...) never match: they carry no
  # @ref, and GitHub does not support @-pinning local actions anyway.
  while IFS= read -r ref; do
    ref="${ref%[\"\']}"   # strip an optional trailing quote
    sha="${ref#*@}"
    if [[ ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
      echo "UNPINNED: $wf -> $ref"
      status=1
    fi
  done < <(grep -oE "uses:[[:space:]]*['\"]?[A-Za-z0-9][A-Za-z0-9._-]*/[^@'\"[:space:]]+@[^'\"[:space:]]+" "$wf" | sed -E "s/uses:[[:space:]]*['\"]?//")
done

# GoReleaser binary version must not float. Scoped to release.yml (the release
# path); a blanket scan of ci.yml would false-flag other actions' legitimate
# `version: latest` inputs (e.g. golangci-lint).
if grep -qE "version:[[:space:]]*['\"]?(latest|nightly)\b" .github/workflows/release.yml; then
  echo "UNPINNED: GoReleaser version is latest/nightly in release.yml"
  status=1
fi

if [[ "$status" -eq 0 ]]; then
  echo "All actions and reusable workflows are pinned to commit SHAs."
fi
exit "$status"
