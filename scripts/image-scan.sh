#!/usr/bin/env bash
# Scans every image in an imagepin inventory for known vulnerabilities, once
# per platform NIC can deploy on (the inventory lists them per image).
#
# For each image and platform: Trivy builds a CycloneDX SBOM from that
# platform's own image digest, then scans that SBOM, so the findings describe
# exactly the inventory kept. Outputs (under OUT, default dist/image-scan):
#   sbom/<image>.cdx.json      SBOM per image and platform
#   findings/<image>.json      Trivy findings per image and platform
#   findings.jsonl             every finding, one line each, tagged with its image and platform
#   platform-gaps.txt          platforms a vendor doesn't publish an image for
#   summary.md                 counts per image and platform (CI appends it to the job summary)
#   attestation/subjects.txt   every scanned digest, index and per-platform (sha256sum format)
#   attestation/predicate.json the report manifest CI signs: hashes of every
#                              report file, counts, gaps and run identity
#
# Running this locally produces the same attestation inputs CI would sign, so
# a local run shows exactly what would be attested.
#
# Findings never fail this script: it reports, it doesn't gate. Neither does a
# platform a vendor doesn't publish: that is recorded as a limitation for
# deployments on that platform. It exits 1 only on functional failures: an
# image the inventory couldn't resolve, or one that couldn't be pulled or scanned.
#
# Usage: scripts/image-scan.sh [inventory.json] [out-dir]
#   Produce the inventory first: go run ./cmd/imagepin inventory -o dist/image-scan/inventory.json
set -euo pipefail

inventory="${1:-dist/image-scan/inventory.json}"
out="${2:-dist/image-scan}"
scanned="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

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
# An inventory from before per-platform support has no platform data; scanning
# it would scan nothing and could look clean.
if ! jq -e '[.images[] | select(.scanRef) | has("platforms") or has("missingPlatforms")] | any' "$inventory" >/dev/null; then
  echo "inventory has no platform data; regenerate it with the current imagepin" >&2
  exit 1
fi

mkdir -p "$out/sbom" "$out/findings"
failures="$out/failures.txt"
gaps="$out/platform-gaps.txt"
: >"$failures"
: >"$gaps"

# Images the inventory couldn't resolve are functional failures; they can't
# be scanned because there is no digest to scan.
jq -r '.images[] | select(.error) | "unresolved  \(.ref): \(.error)"' "$inventory" >>"$failures"

# Platforms a vendor doesn't publish: a limitation for deployments there.
jq -r '.images[] | select(.scanRef) | .ref as $r | (.missingPlatforms // [])[] | "\($r)  not published for \(.)"' \
  "$inventory" >>"$gaps"

# One scan target per image and platform: the image's name with that
# platform's own digest, plus what the summary and findings need about it.
targets() {
  jq -c '.images[] | select(.scanRef) | . as $i | ($i.platforms // [])[] | {
    image: $i.ref, status: $i.status, sources: $i.sources, indexDigest: $i.digest,
    platform: .platform, digest: .digest,
    scanRef: (($i.scanRef | sub("@sha256:[a-f0-9]+$"; "")) + "@" + .digest)
  }' "$inventory"
}

# file name for an image reference: registry/repo@sha256:... -> registry_repo_sha256_...
fname() { printf '%s' "$1" | tr '/:@' '___'; }

while IFS= read -r t; do
  ref="$(jq -r .scanRef <<<"$t")"
  platform="$(jq -r .platform <<<"$t")"
  f="$(fname "$ref")"
  if trivy -q image --image-src remote \
      --format cyclonedx --output "$out/sbom/$f.cdx.json" "$ref" &&
    trivy -q sbom --format json --output "$out/findings/$f.json" "$out/sbom/$f.cdx.json"; then
    echo "ok    $platform  $ref"
  else
    echo "FAIL  $platform  $ref"
    echo "scan        $ref ($platform)" >>"$failures"
  fi
done < <(targets)

# One line per finding, tagged with the image it came from: the reference as
# NIC renders it, the platform and its exact digest scanned, the index digest
# NIC deploys, and where in NIC the image is defined. This is the file later
# reporting, notification and tracking work builds on.
targets | while IFS= read -r t; do
  f="$out/findings/$(fname "$(jq -r .scanRef <<<"$t")").json"
  [[ -f "$f" ]] || continue # scan failed; already recorded
  jq -c --argjson t "$t" --arg scanned "$scanned" '
    .Results[]? as $r | $r.Vulnerabilities[]? | {
      image: $t.image,
      short: ($t.image | sub("@sha256:(?<d>[a-f0-9]{12})[a-f0-9]+$"; "@\(.d)")),
      platform: $t.platform, scanRef: $t.scanRef,
      digest: $t.digest, indexDigest: $t.indexDigest,
      scanned: $scanned, sources: $t.sources,
      id: .VulnerabilityID, severity: .Severity,
      pkg: .PkgName, purl: (.PkgIdentifier.PURL // ""), path: (.PkgPath // ""),
      installed: .InstalledVersion, fixed: (.FixedVersion // ""),
      status: (.Status // "unknown"),
      where: $r.Target, title: (.Title // ""), url: (.PrimaryURL // "")
    }' "$f"
done >"$out/findings.jsonl"

# Counts per image and platform; every scan target listed (including ones
# with no findings), then the platform gaps and any functional failures.
targets | jq -rs --slurpfile f "$out/findings.jsonl" --rawfile fail "$failures" --rawfile gaps "$gaps" '
  def count($xs; $s): $xs | map(select(. == $s)) | length;
  . as $targets
  | ($f | map(.severity)) as $all
  | ($targets | map(. as $t | ($f | map(select(.image == $t.image and .platform == $t.platform)) | map(.severity)) as $s | {
      name: ($t.image | sub("@sha256:(?<d>[a-f0-9]{12})[a-f0-9]+$"; "@\(.d)")),
      platform: $t.platform,
      pinned: (if $t.status == "digest" then "yes" else "no" end),
      failed: ($fail | contains("scan        \($t.scanRef) ")),
      c: count($s; "CRITICAL"), h: count($s; "HIGH"), m: count($s; "MEDIUM"), l: count($s; "LOW")
    }) | sort_by((.failed | not), -.c, -.h, -.m, .name, .platform)) as $rows
  | "## Image vulnerability scan",
    "",
    "\($rows | map(select(.failed | not)) | length) of \($rows | length) image/platform scans completed. Findings: **\(count($all; "CRITICAL")) critical**, \(count($all; "HIGH")) high, \(count($all; "MEDIUM")) medium, \(count($all; "LOW")) low.",
    "Details for every finding are in the `findings.jsonl` artifact.",
    "",
    "_These are unassessed scanner matches: a finding means an installed package version matches a published advisory, not that NIC is confirmed affected._",
    "",
    "| Image | Platform | Pinned by digest | Critical | High | Medium | Low |",
    "|---|---|---|--:|--:|--:|--:|",
    ($rows[] | if .failed
      then "| `\(.name)` | \(.platform) | \(.pinned) | scan failed | | | |"
      else "| `\(.name)` | \(.platform) | \(.pinned) | \(.c) | \(.h) | \(.m) | \(.l) |" end),
    (if ($gaps | length) > 0 then
      "", "### Platform gaps", "",
      "Images the vendor doesn'\''t publish for a platform NIC supports. Deployments on that platform can'\''t run them.",
      "", "```", ($gaps | rtrimstr("\n")), "```"
     else empty end),
    (if ($fail | length) > 0 then
      "", "### Functional failures", "", "```", ($fail | rtrimstr("\n")), "```"
     else empty end)
' >"$out/summary.md"

# Attestation inputs. Subjects: every digest scanned, both the index NIC
# deploys and each platform's image, so a lookup by either finds this run.
# Predicate: a manifest of the run, binding every report file by its hash.
# Only completed scans are subjects: a signed statement must not name an
# image that wasn't scanned.
mkdir -p "$out/attestation"
targets | while IFS= read -r t; do
  [[ -f "$out/findings/$(fname "$(jq -r .scanRef <<<"$t")").json" ]] || continue
  jq -r '(.scanRef | sub("@sha256:[a-f0-9]+$"; "")) as $name
    | "\(.indexDigest)  \($name)", "\(.digest)  \($name)"' <<<"$t"
done | sed 's/^sha256://' | sort -u >"$out/attestation/subjects.txt"
if (($(wc -l <"$out/attestation/subjects.txt") > 1024)); then
  echo "more than 1024 attestation subjects; split the attestation" >&2
  exit 1
fi

sha() { sha256sum "$1" | cut -d' ' -f1; }
reports="$(for f in inventory.json findings.jsonl summary.md platform-gaps.txt; do
  [[ -f "$out/$f" ]] && jq -n --arg f "$f" --arg h "$(sha "$out/$f")" '{($f): ("sha256:" + $h)}'
done | jq -s 'add')"
images="$(targets | while IFS= read -r t; do
  f="$(fname "$(jq -r .scanRef <<<"$t")")"
  [[ -f "$out/findings/$f.json" ]] || continue
  jq -c --argjson t "$t" --arg sbom "$(sha "$out/sbom/$f.cdx.json")" --arg fnd "$(sha "$out/findings/$f.json")" '
    [.Results[]?.Vulnerabilities[]?.Severity] as $s
    | def count($l): $s | map(select(. == $l)) | length;
    {image: $t.image, platform: $t.platform, digest: $t.digest, indexDigest: $t.indexDigest,
     sbom: ("sha256:" + $sbom), findings: ("sha256:" + $fnd),
     counts: {critical: count("CRITICAL"), high: count("HIGH"), medium: count("MEDIUM"), low: count("LOW")}}
  ' "$out/findings/$f.json"
done | jq -s .)"
jq -n --argjson reports "$reports" --argjson images "$images" \
  --argjson trivy "$(trivy version --format json)" --arg scanned "$scanned" \
  --rawfile gaps "$gaps" --rawfile fail "$failures" '{
    assessment: "unassessed: scanner matches of installed package versions to published advisories; not confirmed impact on NIC",
    scanned: $scanned,
    scanner: {name: "trivy", version: $trivy.Version, vulnerabilityDB: ($trivy.VulnerabilityDB // null)},
    source: {
      repository: (env.GITHUB_REPOSITORY // "local"),
      commit: (env.GITHUB_SHA // null), ref: (env.GITHUB_REF // null),
      run: (if env.GITHUB_RUN_ID then "\(env.GITHUB_SERVER_URL)/\(env.GITHUB_REPOSITORY)/actions/runs/\(env.GITHUB_RUN_ID)" else null end)
    },
    complete: (($fail | length) == 0),
    reports: $reports,
    images: $images,
    platformGaps: ($gaps | split("\n") | map(select(length > 0)))
  }' >"$out/attestation/predicate.json"

cat "$out/summary.md"

if [[ -s "$failures" ]]; then
  echo "" >&2
  echo "FAIL: $(wc -l <"$failures") image scan(s) could not be resolved or completed; see $failures" >&2
  exit 1
fi
