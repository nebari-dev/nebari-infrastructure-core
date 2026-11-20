#!/usr/bin/env bash
# Scans every image in an imagepin inventory for known vulnerabilities.
#
# For each image: Trivy builds a CycloneDX SBOM from the image digest, then
# scans that SBOM, so the findings describe exactly the inventory kept.
# Outputs (under OUT, default dist/image-scan):
#   sbom/<image>.cdx.json      SBOM per image
#   findings/<image>.json      Trivy findings per image
#   findings.jsonl             every finding, one line each, tagged with its image
#   summary.md                 counts per image (CI appends it to the job summary)
#
# Findings never fail this script: it reports, it doesn't gate. It exits 1
# only on functional failures: an image the inventory couldn't resolve, or
# one that couldn't be pulled or scanned.
#
# Usage: scripts/image-scan.sh [inventory.json] [out-dir]
#   Produce the inventory first: go run ./cmd/imagepin inventory -o dist/image-scan/inventory.json
set -euo pipefail

inventory="${1:-dist/image-scan/inventory.json}"
out="${2:-dist/image-scan}"

for tool in trivy jq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "$tool not found in PATH" >&2
    exit 1
  fi
done
if [[ ! -f "$inventory" ]]; then
  echo "inventory not found: $inventory" >&2
  exit 1
fi

mkdir -p "$out/sbom" "$out/findings"
failures="$out/failures.txt"
: >"$failures"

# Images the inventory couldn't resolve are functional failures; they can't
# be scanned because there is no digest to scan.
jq -r '.images[] | select(.error) | "unresolved  \(.ref): \(.error)"' "$inventory" >>"$failures"

# file name for an image reference: registry/repo@sha256:... -> registry_repo_sha256_...
fname() { printf '%s' "$1" | tr '/:@' '___'; }

while IFS= read -r ref; do
  f="$(fname "$ref")"
  if trivy -q image --image-src remote --platform linux/amd64 \
      --format cyclonedx --output "$out/sbom/$f.cdx.json" "$ref" &&
    trivy -q sbom --format json --output "$out/findings/$f.json" "$out/sbom/$f.cdx.json"; then
    echo "ok    $ref"
  else
    echo "FAIL  $ref"
    echo "scan        $ref" >>"$failures"
  fi
done < <(jq -r '.images[] | select(.scanRef) | .scanRef' "$inventory")

# One line per finding, tagged with the image as rendered (not the file name).
# This is the file later reporting, notification and tracking work builds on.
jq -r '.images[] | select(.scanRef) | [.ref, .scanRef] | @tsv' "$inventory" |
  while IFS=$'\t' read -r image ref; do
    f="$out/findings/$(fname "$ref").json"
    [[ -f "$f" ]] || continue # scan failed; already recorded
    jq -c --arg image "$image" '
      .Results[]? as $r | $r.Vulnerabilities[]? | {
        image: $image,
        short: ($image | sub("@sha256:(?<d>[a-f0-9]{12})[a-f0-9]+$"; "@\(.d)")),
        id: .VulnerabilityID, severity: .Severity,
        pkg: .PkgName, path: (.PkgPath // ""), installed: .InstalledVersion,
        fixed: (.FixedVersion // ""), status: (.Status // "unknown"),
        where: $r.Target, title: (.Title // ""), url: (.PrimaryURL // "")
      }' "$f"
  done >"$out/findings.jsonl"

# Counts per image, every scanned image listed (including ones with none).
jq -rn --slurpfile inv "$inventory" --slurpfile f "$out/findings.jsonl" --rawfile fail "$failures" '
  def count($xs; $s): $xs | map(select(. == $s)) | length;
  ($inv[0].images | map(select(.scanRef))) as $imgs
  | ($f | map(.severity)) as $all
  | ($imgs | map(. as $i | ($f | map(select(.image == $i.ref)) | map(.severity)) as $s | {
      name: ($i.ref | sub("@sha256:(?<d>[a-f0-9]{12})[a-f0-9]+$"; "@\(.d)")),
      pinned: (if $i.status == "digest" then "yes" else "no" end),
      failed: ($fail | contains("scan        \($i.scanRef)")),
      c: count($s; "CRITICAL"), h: count($s; "HIGH"), m: count($s; "MEDIUM"), l: count($s; "LOW")
    }) | sort_by((.failed | not), -.c, -.h, -.m, .name)) as $rows
  | "## Image vulnerability scan",
    "",
    "\($rows | map(select(.failed | not)) | length) of \($imgs | length) images scanned. Findings: **\(count($all; "CRITICAL")) critical**, \(count($all; "HIGH")) high, \(count($all; "MEDIUM")) medium, \(count($all; "LOW")) low.",
    "Details for every finding are in the `findings.jsonl` artifact.",
    "",
    "| Image | Pinned by digest | Critical | High | Medium | Low |",
    "|---|---|--:|--:|--:|--:|",
    ($rows[] | if .failed
      then "| `\(.name)` | \(.pinned) | scan failed | | | |"
      else "| `\(.name)` | \(.pinned) | \(.c) | \(.h) | \(.m) | \(.l) |" end),
    (if ($fail | length) > 0 then
      "", "### Functional failures", "", "```", ($fail | rtrimstr("\n")), "```"
     else empty end)
' >"$out/summary.md"

cat "$out/summary.md"

if [[ -s "$failures" ]]; then
  echo "" >&2
  echo "FAIL: $(wc -l <"$failures") image(s) could not be resolved or scanned; see $failures" >&2
  exit 1
fi