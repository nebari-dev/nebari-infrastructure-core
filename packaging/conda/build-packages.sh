#!/usr/bin/env bash
# Build conda packages for one published nic release, one per conda subdir.
#
# Nothing is compiled here. Each package repackages the release archive
# GoReleaser already published, with the sha256 taken from that release's
# checksums.txt.
#
# checksums.txt is cosign-verified against the release workflow's identity
# before any digest is read from it. Without that step the digests would only
# prove an archive matches a manifest fetched from the same place - matching
# bytes, not authentic ones - so the verification is what makes the provenance
# claim in recipe.yaml.tmpl true rather than aspirational. A missing checksum
# entry fails the build for the same reason.
#
# Usage:
#   packaging/conda/build-packages.sh v0.14.0 [output-dir]
#   BUILD_NUMBER=1 packaging/conda/build-packages.sh v0.14.0   # republish
#
# Requires rattler-build, cosign v3+, and gh (or a public release, for curl).
set -euo pipefail

REPO="nebari-dev/nebari-infrastructure-core"
TAG="${1:-}"
OUTPUT_DIR="${2:-dist/conda}"
# Bump when republishing a corrected recipe for a version already on the
# channel: the recipe body does not reach the variant hash, so without this the
# rebuilt package reuses the published filename.
BUILD_NUMBER="${BUILD_NUMBER:-0}"
RECIPE_TMPL="$(dirname "$0")/recipe.yaml.tmpl"

if [[ -z "$TAG" ]]; then
  echo "usage: $0 <tag> [output-dir]   (e.g. $0 v0.14.0)" >&2
  exit 1
fi

for tool in rattler-build cosign; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "ERROR: $tool not found on PATH." >&2
    echo "  rattler-build: https://rattler.build/latest/installation/" >&2
    echo "  cosign (v3+):  https://docs.sigstore.dev/system_config/installation/" >&2
    exit 1; }
done

# TAG is the git tag and keeps its v - it is what the release API is queried
# with. VERSION drops it, because the asset names do not carry one.
VERSION="${TAG#v}"

# conda-subdir : release-asset-suffix : archive-extension : binary : bin dir
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

echo "==> fetching checksums.txt and its signature for ${TAG}"
if command -v gh >/dev/null 2>&1; then
  gh release download "$TAG" --repo "$REPO" --dir "$workdir" \
    --pattern checksums.txt --pattern checksums.txt.sigstore.json
else
  for f in checksums.txt checksums.txt.sigstore.json; do
    curl -fsSL -o "${workdir}/${f}" \
      "https://github.com/${REPO}/releases/download/${TAG}/${f}"
  done
fi

# Identity-pinned: a bundle-only verify checks the math, not who signed it.
# Same invocation documented for end users in docs/operations/verifying-releases.md.
echo "==> verifying checksums.txt against the release workflow's identity"
cosign verify-blob \
  --bundle "${workdir}/checksums.txt.sigstore.json" \
  --certificate-identity-regexp \
    "^https://github.com/${REPO}/\\.github/workflows/release\\.yml@refs/tags/.*\$" \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  "${workdir}/checksums.txt"

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
      -e "s|__BUILD_NUMBER__|${BUILD_NUMBER}|g" \
      "$RECIPE_TMPL" > "$recipe"

  # Same guard starters.yml applies to rendered starters: a new slot in the
  # template without a matching sed above would otherwise ship as a literal
  # __TOKEN__, showing up as a bad URL or a directory named __BIN_DIR__.
  # Comments are stripped first: the template's own header explains the
  # __PLACEHOLDER__ convention in prose, and that is not a rendering failure.
  if grep -v '^[[:space:]]*#' "$recipe" | grep -q '__[A-Z_]*__'; then
    echo "ERROR: unsubstituted token in ${recipe}:" >&2
    grep -vn '^[[:space:]]*#' "$recipe" | grep '__[A-Z_]*__' >&2
    exit 1
  fi

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
