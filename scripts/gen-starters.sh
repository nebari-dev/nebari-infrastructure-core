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
#   "<line prefix, up to and including the colon and space>###<value CI fills in>"
# Both the CHANGEME substitution and the fill-for-CI script are derived from
# this one list, so they cannot drift apart. Everything else in the example
# stays a working default. Applied as line edits, so the inline comments that
# make the examples useful survive into the starter.
placeholder_fields() {
  case "$1" in
    local)
      printf '%s\n' \
        'project_name: ###nebari-local-ci'
      ;;
    aws)
      printf '%s\n' \
        'project_name: ###nebari-aws-ci' \
        'domain: ###nebari.example.com' \
        '    email: ###admin@example.com' \
        '    url: ###git@github.com:example-org/example-gitops.git' \
        '    path: ###clusters/nebari-aws-ci'
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
    aws)   printf 'opentofu = ">=1.11,<2"' ;;
  esac
}

mkdir -p "$OUT_DIR"
for provider in "${PROVIDERS[@]}"; do
  src="examples/${provider}-config.yaml"
  [ -f "$src" ] || { echo "missing $src" >&2; exit 1; }
  dest="${OUT_DIR}/${provider}"
  mkdir -p "$dest"

  cp "$src" "$dest/config.yaml"
  fill_script="${dest}/.fill-for-ci.sed"
  : > "$fill_script"

  matched=0
  while IFS= read -r field; do
    [ -n "$field" ] || continue
    prefix="${field%%###*}"
    value="${field##*###}"

    before="$(cat "$dest/config.yaml")"
    sed "s|^${prefix}.*|${prefix}CHANGEME|" "$dest/config.yaml" > "$dest/config.yaml.tmp"
    mv "$dest/config.yaml.tmp" "$dest/config.yaml"
    # Every field must match something. examples/ is upstream of this
    # generator, so a restructure there (a key moving or being renamed) would
    # otherwise silently ship a starter with a real value left in it.
    if [ "$before" = "$(cat "$dest/config.yaml")" ]; then
      echo "placeholder field matched nothing for ${provider}: '${prefix}'" >&2
      echo "examples/${provider}-config.yaml has probably been restructured" >&2
      exit 1
    fi

    # The inverse edit, so CI can prove a filled starter validates without
    # keeping its own copy of the field list.
    printf 's|^%sCHANGEME$|%s%s|\n' "$prefix" "$prefix" "$value" >> "$fill_script"
    matched=$((matched + 1))
  done < <(placeholder_fields "$provider")

  # A provider added to PROVIDERS without a placeholder_fields arm would
  # otherwise ship a starter with every real value intact.
  if [ "$matched" -eq 0 ]; then
    echo "no placeholder fields declared for ${provider}" >&2
    exit 1
  fi

  sed -e "s|__PROVIDER__|${provider}|g" \
      -e "s|__NIC_VERSION__|${NIC_VERSION}|g" \
      -e "s|__PROVIDER_DEPS__|$(provider_deps "$provider")|g" \
      "$TEMPLATES/pixi.toml.tmpl" > "$dest/pixi.toml"

  cp "$TEMPLATES/README.${provider}.md" "$dest/README.md"

  echo "generated $dest (nic ${NIC_VERSION})"
done
