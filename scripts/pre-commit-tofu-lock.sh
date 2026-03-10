#!/usr/bin/env bash
set -euo pipefail

mapfile -t dirs < <(find pkg/provider -type f -path "*/templates/main.tf" -print0 | xargs -0 -n1 dirname)

platforms=(
  "linux_amd64"
  "linux_arm64"
  "darwin_amd64"
  "darwin_arm64"
  "windows_amd64"
)

for d in "${dirs[@]}"; do
  echo "==> $d"
  pushd "$d" >/dev/null

  tofu init -backend=false -input=false

  args=()
  for p in "${platforms[@]}"; do
    args+=("-platform=$p")
  done

  tofu providers lock "${args[@]}"
  rm -rf .terraform

  popd >/dev/null
done

if ! git diff --quiet -- pkg/provider/**/templates/.terraform.lock.hcl; then
  echo "Lockfile drift detected. Commit updated .terraform.lock.hcl"
  git diff -- pkg/provider/**/templates/.terraform.lock.hcl
  exit 1
fi
