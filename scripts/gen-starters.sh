#!/usr/bin/env bash
# Render Nebi starter workspaces from examples/<provider>-config.yaml.
#
# Starters are published as OCI bundles to a registry; they are deliberately
# NOT committed to this repository. Only the templates and this generator live
# in the tree, so examples/ stays the single source of truth for config content
# and there is nothing to drift.
#
# Usage: scripts/gen-starters.sh [output-dir]   (default: dist/starters)
set -euo pipefail

OUT_DIR="${1:-dist/starters}"
TEMPLATES="starters/templates"
# Version the starter pins nic to. Defaults to the latest tag, minus the "v".
NIC_VERSION="${NIC_VERSION:-$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || true)}"
: "${NIC_VERSION:?could not determine NIC_VERSION and none was supplied}"

# Providers in scope. Extend deliberately: each one needs its placeholder
# fields and any provider-specific dependencies below.
PROVIDERS=("local" "aws")

# The identity-bearing fields a user must supply, declared once per provider as
# the line prefix up to and including the colon and space. Everything else in
# the example stays a working default.
#
# Applied as line edits rather than a YAML round trip, so the inline comments
# that make the examples useful survive into the starter - including the comment
# on a replaced line, which is preserved and reattached (those five lines are
# exactly the ones whose hint the reader needs most).
placeholder_fields() {
  case "$1" in
    local)
      printf '%s\n' \
        'project_name: '
      ;;
    aws)
      printf '%s\n' \
        'project_name: ' \
        'domain: ' \
        '    email: ' \
        '    url: ' \
        '    path: '
      ;;
  esac
}

# Extra conda dependencies a provider needs beyond nic itself.
provider_deps() {
  case "$1" in
    # The local provider drives kind through a Go library, so no OpenTofu.
    local) printf '' ;;
    # AWS runs OpenTofu. Pinning it here is the point of a pinned toolchain:
    # without it nic falls back to downloading an unpinned tofu at deploy time.
    # The floor must stay >= pkg/tofu.MinVersion (1.11.3): below that,
    # compatibleVersion rejects the PATH binary and nic downloads one anyway,
    # silently, which defeats the pin.
    aws)   printf 'opentofu = ">=1.11.3,<2"' ;;
  esac
}

mkdir -p "$OUT_DIR"

# Report-and-block, accumulating: a restructure of examples/ usually moves more
# than one key, and exiting on the first would make the author re-run once per
# field to discover them. Held as a newline-delimited string rather than an
# array so an empty accumulator is safe under `set -u` on bash 3.2 (macOS).
errors=""
note_error() { errors="${errors}${1}"$'\n'; }

for provider in "${PROVIDERS[@]}"; do
  src="examples/${provider}-config.yaml"
  if [ ! -f "$src" ]; then
    note_error "${provider}: missing ${src}"
    continue
  fi
  dest="${OUT_DIR}/${provider}"
  mkdir -p "$dest"

  cp "$src" "$dest/config.yaml"

  matched=0
  while IFS= read -r prefix; do
    [ -n "$prefix" ] || continue

    # Every field must match EXACTLY ONE line. examples/ is upstream of this
    # generator, so a restructure there can break this two ways, and counting
    # is what tells them apart: zero matches means a key was renamed or moved
    # and a real value would ship untouched; more than one means a same-named
    # key appeared at the same indent, and blanking both would bury a real
    # value under a placeholder that looks correct.
    hits="$(grep -c "^${prefix}" "$dest/config.yaml" || true)"
    if [ "$hits" -ne 1 ]; then
      note_error "${provider}: '${prefix}' matched ${hits} lines in examples/${provider}-config.yaml, want exactly 1"
      continue
    fi

    # Replace the value but reattach any trailing comment, so the hint on a
    # line the reader must edit survives (examples/aws-config.yaml's path: key
    # documents itself as optional, and that is worth keeping in the starter).
    sed "s|^\(${prefix}\)[^#]*\(#.*\)\{0,1\}$|\1CHANGEME  \2|" \
      "$dest/config.yaml" > "$dest/config.yaml.tmp"
    mv "$dest/config.yaml.tmp" "$dest/config.yaml"
    matched=$((matched + 1))
  done < <(placeholder_fields "$provider")

  # A provider added to PROVIDERS without a placeholder_fields arm would
  # otherwise ship a starter with every real value intact.
  if [ "$matched" -eq 0 ] && [ -z "$errors" ]; then
    note_error "${provider}: no placeholder fields declared; add an arm to placeholder_fields()"
  fi

  # Trailing whitespace from a replaced line that carried no comment.
  sed 's|[[:space:]]*$||' "$dest/config.yaml" > "$dest/config.yaml.tmp"
  mv "$dest/config.yaml.tmp" "$dest/config.yaml"

  sed -e "s|__PROVIDER__|${provider}|g" \
      -e "s|__NIC_VERSION__|${NIC_VERSION}|g" \
      -e "s|__PROVIDER_DEPS__|$(provider_deps "$provider")|g" \
      "$TEMPLATES/pixi.toml.tmpl" > "$dest/pixi.toml"

  cp "$TEMPLATES/README.${provider}.md" "$dest/README.md"

  echo "generated $dest (nic ${NIC_VERSION})"
done

if [ -n "$errors" ]; then
  echo "gen-starters failed:" >&2
  printf '%s' "$errors" | sed 's|^|  - |' >&2
  exit 1
fi
