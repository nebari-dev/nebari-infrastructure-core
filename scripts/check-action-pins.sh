#!/usr/bin/env bash
# Fails if any workflow references a GitHub Action or reusable workflow by a
# mutable ref (a tag/branch instead of a 40-char commit SHA), or if the
# GoReleaser action version floats (latest/nightly). Globs every workflow file
# so newly added workflows are covered automatically.
set -euo pipefail

shopt -s nullglob
workflows=(.github/workflows/*.yml .github/workflows/*.yaml)
shopt -u nullglob

if [[ ${#workflows[@]} -eq 0 ]]; then
  echo "no workflow files found under .github/workflows/"
  exit 1
fi

status=0

for wf in "${workflows[@]}"; do
  # Match only remote refs shaped owner/repo(/subpath)?@ref. The leading alnum
  # class means local refs (./.github/actions/...) never match: they carry no
  # @ref, and GitHub does not support @-pinning local actions anyway. The ref
  # char class stops before any closing quote, so quoted refs are handled too.
  while IFS= read -r ref; do
    sha="${ref#*@}"
    if [[ ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
      echo "UNPINNED: $wf -> $ref"
      status=1
    fi
  done < <(grep -oE "uses:[[:space:]]*['\"]?[A-Za-z0-9][A-Za-z0-9._-]*/[^@'\"[:space:]]+@[^'\"[:space:]]+" "$wf" | sed -E "s/uses:[[:space:]]*['\"]?//")

  # The two rules below cover the specific fetch vectors used inside run:
  # blocks (curl of raw.githubusercontent.com scripts, container images passed
  # via --image). They are not general coverage of run: blocks. Arbitrary
  # shell cannot be audited by grep, but they stop the pins we do have from
  # silently regressing.

  # raw.githubusercontent.com fetches: the path segment after owner/repo is
  # the git ref; require an immutable commit SHA there.
  while IFS= read -r url; do
    ref="$(sed -E 's#https://raw\.githubusercontent\.com/[^/]+/[^/]+/([^/]+)/.*#\1#' <<<"$url")"
    if [[ ! "$ref" =~ ^[0-9a-f]{40}$ ]]; then
      echo "UNPINNED: $wf -> $url (raw.githubusercontent.com ref must be a commit SHA)"
      status=1
    fi
  done < <(grep -oE "https://raw\.githubusercontent\.com/[^\"'[:space:]]+" "$wf")

  # github.com/<owner>/<repo>/raw/<ref>/<path> serves the same bytes. Apply
  # the same rule to the segment after /raw/.
  while IFS= read -r url; do
    ref="$(sed -E 's#https://github\.com/[^/]+/[^/]+/raw/([^/]+)/.*#\1#' <<<"$url")"
    if [[ ! "$ref" =~ ^[0-9a-f]{40}$ ]]; then
      echo "UNPINNED: $wf -> $url (github.com /raw/ ref must be a commit SHA)"
      status=1
    fi
  done < <(grep -oE "https://github\.com/[^/\"'[:space:]]+/[^/\"'[:space:]]+/raw/[^\"'[:space:]]+" "$wf")

  # Container images passed with --image must be digest-pinned.
  while IFS= read -r img; do
    if [[ "$img" != *"@sha256:"* ]]; then
      echo "UNPINNED: $wf -> $img (image must carry a sha256 digest)"
      status=1
    fi
  done < <(grep -oE -- "--image[= ][^\"'[:space:]]+" "$wf")
done

# GoReleaser binary version must not float. Scoped to release.yml (the release
# path); a blanket scan would false-flag other actions' legitimate
# `version: latest` inputs (e.g. golangci-lint in ci.yml).
if [[ -f .github/workflows/release.yml ]] && grep -qE "version:[[:space:]]*['\"]?(latest|nightly)\b" .github/workflows/release.yml; then
  echo "UNPINNED: GoReleaser version is latest/nightly in release.yml"
  status=1
fi

if [[ "$status" -eq 0 ]]; then
  echo "All actions and reusable workflows are pinned to commit SHAs."
fi
exit "$status"
