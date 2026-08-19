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
NIC_VERSION="${NIC_VERSION:-$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')}"
: "${NIC_VERSION:?could not determine NIC_VERSION and none was supplied}"

# Providers in scope. Extend deliberately: each one needs its placeholder rules
# and any provider-specific dependencies below.
PROVIDERS=("local" "aws")

# Which fields become CHANGEME, per provider. These are the identity-bearing
# values a user must supply; everything else stays a working default. Applied
# as line edits so the examples' inline comments survive intact.
placeholder_rules() {
  case "$1" in
    local)
      printf '%s\n' \
        's|^project_name: .*|project_name: CHANGEME|'
      ;;
    aws)
      printf '%s\n' \
        's|^project_name: .*|project_name: CHANGEME|' \
        's|^domain: .*|domain: CHANGEME|' \
        's|^    email: .*|    email: CHANGEME|' \
        's|^  url: .*|  url: CHANGEME|'
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

provider_title() {
  case "$1" in
    local) printf 'local (kind)' ;;
    aws)   printf 'AWS' ;;
  esac
}

provider_notes() {
  case "$1" in
    local)
      cat <<'EOF'
## What you must edit

Only `project_name`. The local provider runs everything in a kind cluster on
your machine, so it needs no cloud credentials, the certificate is self-signed
and the GitOps repository is created for you.
EOF
      ;;
    aws)
      cat <<'EOF'
## What you must edit

`project_name`, `domain`, `certificate.acme.email` and `git_repository.url`.
The infrastructure defaults (region, availability zones, instance types,
Longhorn, EFS) are working values, not placeholders. **If you change `region`,
change `availability_zones` to match**: `nic validate` does not cross-check
them and a mismatch fails mid-deploy.

`nic validate` runs offline and needs no AWS credentials. It will not catch a
bad region, a nonexistent instance type or an availability zone that does not
exist in your region; those surface at deploy time.

## Cost note

This starter inherits the production-recommended shape, including dedicated
Longhorn storage nodes with large gp3 volumes and EFS. That is a real monthly
bill; trim the node groups for experiments.
EOF
      ;;
  esac
}

mkdir -p "$OUT_DIR"
for provider in "${PROVIDERS[@]}"; do
  src="examples/${provider}-config.yaml"
  [ -f "$src" ] || { echo "missing $src" >&2; exit 1; }
  dest="${OUT_DIR}/${provider}"
  mkdir -p "$dest"

  # config.yaml: the example with identity-bearing values replaced.
  cp "$src" "$dest/config.yaml"
  while IFS= read -r rule; do
    [ -n "$rule" ] && sed -i "$rule" "$dest/config.yaml"
  done < <(placeholder_rules "$provider")

  # Fail loudly rather than publishing a starter that would validate as-is.
  grep -q CHANGEME "$dest/config.yaml" || {
    echo "no placeholders were substituted for $provider" >&2; exit 1; }

  sed -e "s|__PROVIDER__|${provider}|g" \
      -e "s|__NIC_VERSION__|${NIC_VERSION}|g" \
      -e "s|__PROVIDER_DEPS__|$(provider_deps "$provider")|g" \
      "$TEMPLATES/pixi.toml.tmpl" > "$dest/pixi.toml"

  notes="$(provider_notes "$provider")"
  awk -v notes="$notes" \
      -v provider="$provider" \
      -v title="$(provider_title "$provider")" \
      '{gsub(/__PROVIDER_NOTES__/, notes); gsub(/__PROVIDER_TITLE__/, title); gsub(/__PROVIDER__/, provider); print}' \
      "$TEMPLATES/README.md.tmpl" > "$dest/README.md"

  echo "generated $dest (nic ${NIC_VERSION})"
done
