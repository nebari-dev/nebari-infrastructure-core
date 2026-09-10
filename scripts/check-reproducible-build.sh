#!/usr/bin/env bash
#
# Proves the release build is reproducible: two builds of the same commit must
# produce byte-identical release artifacts.
#
# This is the property that lets the release pipeline build once, run the
# deployment tests against that build, and publish a rebuild while still
# honestly claiming the tested artifact and the published artifact are the same
# file. Without it, "we release what we test" is an assumption. With it, it is
# a checked fact.
#
# It runs on every pull request rather than at release time because
# reproducibility rots silently. An ldflag that carries a build timestamp, an
# archive that picks up checkout mtimes, a dependency that embeds build info:
# none of those fail anything until someone compares two builds. Checking here
# turns that into a red check on the pull request that introduced it.
#
# Run locally with goreleaser, syft and jq on PATH. Note that it rewrites the
# mtimes of tracked files in the working tree (contents are untouched) and that
# goreleaser --clean empties dist/.

set -euo pipefail

for cmd in goreleaser syft jq; do
  command -v "$cmd" >/dev/null || { echo "::error::$cmd is not on PATH"; exit 1; }
done

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

# --snapshot so this works on a pull request with no tag; the snapshot version
# derives from the previous tag and is stable across both runs.
# --skip=sign because keyless signatures are non-deterministic by construction
# and are not part of the artifact being compared.
build() {
  local into="$1"
  goreleaser release --clean --snapshot --skip=sign >"$workdir/$into.log" 2>&1 || {
    echo "::error::goreleaser failed on build '$into'"
    tail -30 "$workdir/$into.log"
    exit 1
  }
  mkdir -p "$workdir/$into"
  cp dist/checksums.txt "$workdir/$into/"
  # Tolerated here so the explicit "no SBOMs" error below is what fires, rather
  # than an unexplained cp failure under set -e.
  cp dist/*.sbom.json "$workdir/$into/" 2>/dev/null || true
}

echo "==> build 1 of 2"
build first

# The release pipeline builds in one job and rebuilds in another, and each job
# runs its own actions/checkout, which stamps every tracked file with a fresh
# mtime. Both builds here share a single checkout, so without re-stamping, an
# archive that embeds source-file mtimes would look stable and this check would
# pass while the real two-job pipeline produced differing archives.
echo "==> re-stamping tracked file mtimes to simulate the publish job's checkout"
git ls-files -z | xargs -0 touch

echo "==> build 2 of 2"
build second

failed=0

echo
echo "==> comparing checksums.txt (archives and source)"
# SBOM lines are compared separately below, with the two fields syft cannot
# make deterministic normalized away.
grep -v '\.sbom\.json$' "$workdir/first/checksums.txt"  | sort >"$workdir/first.sums"
grep -v '\.sbom\.json$' "$workdir/second/checksums.txt" | sort >"$workdir/second.sums"

if [[ ! -s "$workdir/first.sums" ]]; then
  echo "::error::checksums.txt listed no non-SBOM artifacts, so this check would pass having compared nothing"
  exit 1
fi

if diff -u "$workdir/first.sums" "$workdir/second.sums"; then
  echo "OK: $(wc -l <"$workdir/first.sums") artifacts are byte-identical across builds"
else
  echo "::error::release artifacts are not reproducible; two builds of this commit differ"
  echo "Usual causes: an ldflag using {{ .Date }} rather than {{ .CommitDate }}, an unset"
  echo "builds.mod_timestamp, or an archive files: entry with no info.mtime. See .goreleaser.yml."
  failed=1
fi

echo
echo "==> comparing SBOMs"
# syft stamps each document with the time it ran and a random documentNamespace
# UUID, and honors no SOURCE_DATE_EPOCH, so two SBOMs of a byte-identical
# archive always differ in those two places. Both are dropped here; every
# package, version, license, checksum and relationship is still compared.
normalize_sbom() {
  jq -S 'del(.creationInfo.created, .documentNamespace)' "$1"
}

shopt -s nullglob
sboms=("$workdir/first"/*.sbom.json)
if (( ${#sboms[@]} == 0 )); then
  echo "::error::no SBOMs were produced; expected one per archive"
  exit 1
fi

for a in "${sboms[@]}"; do
  name="$(basename "$a")"
  b="$workdir/second/$name"
  if [[ ! -f "$b" ]]; then
    echo "::error::$name was produced by the first build but not the second"
    failed=1
    continue
  fi
  if diff -q <(normalize_sbom "$a") <(normalize_sbom "$b") >/dev/null; then
    echo "OK: $name"
  else
    echo "::error::$name differs beyond its generation timestamp and namespace"
    # A whole SBOM is far too much for a job log; the first differing lines are
    # enough to identify what became non-deterministic.
    diff <(normalize_sbom "$a") <(normalize_sbom "$b") | head -30
    failed=1
  fi
done

echo
if (( failed )); then
  echo "FAIL: the release build is not reproducible"
  exit 1
fi
echo "PASS: two builds of this commit produced identical release artifacts"
