#!/usr/bin/env bash
# Build conda packages for one published nic release, one per conda subdir.
#
# Nothing is compiled here. Each package repackages the release archive
# GoReleaser already published, with the sha256 taken from that release's
# checksums.txt - the file the release pipeline signs with cosign. If a
# checksum is missing the build fails rather than producing something
# unverified.
#
# Usage:
#   packaging/conda/build-packages.sh v0.14.0 [output-dir]
#
# Requires rattler-build, and gh (or a public release, for the curl fallback).
set -euo pipefail

REPO="nebari-dev/nebari-infrastructure-core"
TAG="${1:-}"
OUTPUT_DIR="${2:-dist/conda}"
RECIPE_TMPL="$(dirname "$0")/recipe.yaml.tmpl"

if [[ -z "$TAG" ]]; then
  echo "usage: $0 <tag> [output-dir]   (e.g. $0 v0.14.0)" >&2
  exit 1
fi

# Accept v0.14.0 or 0.14.0; the tag carries the v, the asset names do not.
VERSION="${TAG#v}"

# conda-subdir : release-asset-suffix : archive-extension : binary name
#
# The suffixes are GoReleaser's, from .goreleaser.yml's archives.name_template
# (goos plus a remapped goarch); the subdirs are conda's names for the same
# platforms. Windows archives are zip, not tar.gz, and conda expects binaries
# under Library/bin there rather than bin.
PLATFORMS=(
  "linux-64:linux_x86_64:tar.gz:nic:bin"
  "linux-aarch64:linux_arm64:tar.gz:nic:bin"
  "osx-64:darwin_x86_64:tar.gz:nic:bin"
  "osx-arm64:darwin_arm64:tar.gz:nic:bin"
  "win-64:windows_x86_64:zip:nic.exe:Library/bin"
  "win-arm64:windows_arm64:zip:nic.exe:Library/bin"
)

# conda forbids '-' in a version string: it is the separator between name,
# version and build string in a package filename, so a prerelease tag like
# v0.14.0-rc.1 produces an artifact whose own version cannot be resolved at
# test time. 0.14.0rc1 is the conda spelling, and conda also sorts it before
# 0.14.0, which is the ordering a prerelease wants.
CONDA_VERSION="$(printf '%s' "$VERSION" | tr -d '-' | sed 's/rc\./rc/; s/beta\./beta/; s/alpha\./alpha/')"
[[ "$CONDA_VERSION" == "$VERSION" ]] || echo "==> conda version: ${VERSION} -> ${CONDA_VERSION}"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

echo "==> fetching checksums.txt for ${TAG}"
if command -v gh >/dev/null 2>&1; then
  gh release download "$TAG" --repo "$REPO" --pattern checksums.txt --dir "$workdir"
else
  curl -fsSL -o "${workdir}/checksums.txt" \
    "https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"
fi

mkdir -p "$OUTPUT_DIR"

for entry in "${PLATFORMS[@]}"; do
  IFS=: read -r subdir suffix ext binary bindir <<<"$entry"
  asset="nebari-infrastructure-core_${VERSION}_${suffix}.${ext}"

  sha256="$(awk -v f="$asset" '$2 == f {print $1}' "${workdir}/checksums.txt")"
  if [[ -z "$sha256" ]]; then
    echo "ERROR: no checksum for ${asset} in ${TAG}'s checksums.txt" >&2
    exit 1
  fi

  recipe="${workdir}/recipe-${subdir}.yaml"
  sed -e "s|__VERSION__|${VERSION}|g" \
      -e "s|__CONDA_VERSION__|${CONDA_VERSION}|g" \
      -e "s|__ASSET__|${asset}|g" \
      -e "s|__SHA256__|${sha256}|g" \
      -e "s|__BIN_SRC__|${binary}|g" \
      -e "s|__BIN_DIR__|${bindir}|g" \
      "$RECIPE_TMPL" > "$recipe"

  echo "==> ${subdir}  <-  ${asset}  (${sha256:0:12})"
  # --test native: the smoke test executes nic, so it can only run when the
  # target platform is the build platform. rattler-build skips it elsewhere
  # rather than failing, which is what lets one Linux runner emit all six.
  rattler-build build \
    --recipe "$recipe" \
    --target-platform "$subdir" \
    --test native \
    --output-dir "$OUTPUT_DIR"
done

echo "==> built:"
find "$OUTPUT_DIR" -name '*.conda' -print
