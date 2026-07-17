#!/usr/bin/env bash
# Fails when govulncheck reports a reachable ("called") vulnerability that has a
# fixed version available. Advisories with no fix (Fixed in: N/A) are printed for
# visibility but do not fail the build, so the gate stays green on unavoidable
# findings (e.g. golang.org/x/crypto openpgp, containerd CRI daemon-only paths)
# while still forcing action the moment an upstream fix ships. Mirrors the
# report-and-block style of scripts/check-action-pins.sh.
set -euo pipefail

if ! command -v govulncheck >/dev/null 2>&1; then
  echo "govulncheck not found in PATH; install with: go install golang.org/x/vuln/cmd/govulncheck@v1.4.0" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq not found in PATH (preinstalled on ubuntu-latest runners)" >&2
  exit 1
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

# -format json exits 0 regardless of findings (JSON output is for tooling), so
# the gate decision is made entirely from the parsed findings below.
govulncheck -format json ./... >"$tmp"

# A finding is "called"/reachable when its most specific trace frame (trace[0])
# has a function. Group findings by OSV id; a group fails the gate when it is
# both called and carries a non-empty fixed_version.
group='
  [ .[] | select(.finding) | .finding ]
  | group_by(.osv)
  | map({
      osv:    .[0].osv,
      called: (any(.[]; .trace[0].function != null)),
      fixed:  (map(.fixed_version // empty) | first)
    })
'

echo "govulncheck findings (grouped by advisory):"
jq -s -r "$group | .[] | \"  \(.osv)  called=\(.called)  fixed=\(.fixed // \"N/A\")\"" "$tmp" || true

fixable="$(jq -s "$group | map(select(.called and .fixed))" "$tmp")"
count="$(echo "$fixable" | jq 'length')"

if [[ "$count" -gt 0 ]]; then
  echo ""
  echo "FAIL: ${count} reachable vulnerability(ies) have an available fix; bump the dependency:" >&2
  echo "$fixable" | jq -r '.[] | "  \(.osv) -> fixed in \(.fixed)"' >&2
  exit 1
fi

echo ""
echo "OK: no reachable vulnerability with an available fix."
