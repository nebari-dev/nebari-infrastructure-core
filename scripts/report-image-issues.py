#!/usr/bin/env python3
"""Reconcile scheduled image findings with bot-owned GitHub security issues.

One issue per CVE/GHSA and package rolls up affected images and NIC versions.
Without --apply this only prints the proposed operations; it never writes.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import time
from urllib.error import HTTPError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

LABEL = "area: security"
MARKER = re.compile(r"\A<!-- nic-image-scan:v2:([a-f0-9]{64}) -->")
STATE = re.compile(r"<!-- nic-image-state:(.+) -->")


def repository_name(image):
    repository = image.split("@", 1)[0]
    if ":" in repository.rsplit("/", 1)[-1]:
        repository = repository.rsplit(":", 1)[0]
    return repository


def issue_body(key, finding, report):
    targets = sorted({target for affected in finding["affected"] for target in affected["targets"]})
    repositories = sorted({repository_name(image) for affected in finding["affected"] for image in affected["images"]})
    state = {"cve": finding["cve"], "package": finding["package"], "targets": targets, "repositories": repositories}
    # An unrelated main commit should not rewrite every open security issue.
    # Exact checkout revisions remain in the immutable scan artifact.
    versions = [f"`{target}` (`{report['targets'][target]['revision']}`)" if target == "release" else f"`{target}`" for target in targets]
    lines = [f"<!-- nic-image-scan:v2:{key} -->", "<!-- nic-image-state:" + json.dumps(state, sort_keys=True) + " -->",
             f"**{finding['cve']}** affects package **`{finding['package']}`** in container images NIC deploys.", "",
             "**Status:** Present in the scanned targets. HIGH/CRITICAL findings include those without fixes.", "",
             "Affected NIC versions: " + ", ".join(versions), "",
             "| Image references | Platform / digest | Installed → fixed |",
             "| --- | --- | --- |"]
    for affected in finding["affected"]:
        images = "<br>".join(f"`{image}`" for image in affected["images"])
        fix = affected["fixed"] or "NO UPSTREAM FIX (" + affected["status"] + ")"
        lines.append(f"| {images} | {', '.join(affected['platforms'])}<br>`{affected['digest']}` | `{affected['installed']}` → `{fix}` |")
    if finding["critical_unfixed"]:
        lines += ["", "**CRITICAL WITHOUT AN UPSTREAM FIX:** at least one affected image has no fixed package version. Evaluate mitigation or replacement; this finding is not hidden."]
    if not report["complete"]:
        lines += ["", "**PARTIAL SCAN:** some images failed. This evidence cannot resolve existing findings."]
    lines += ["", "Advisory: " + finding.get("url", ""), "",
              "The Deployed image vulnerabilities workflow maintains this issue. Only a complete successful scan can resolve it, with a distinct fixed, suppressed or no longer shipped reason. A recurrence reopens the same issue.",
              "Raw Trivy JSON, SARIF, digests and coverage notes are attached to the workflow run. Suppressions in `.trivyignore` require a reason and expiry date."]
    for target, info in report["targets"].items():
        if info["status"] != "scanned":
            lines += ["", f"Coverage limit: `{target}` (`{info['revision']}`) is `{info['status']}`."]
    body = "\n".join(lines) + "\n"
    if len(body.encode()) > 60000:
        raise ValueError(f"{key}: issue body exceeds safe GitHub size; findings remain in the scan artifact")
    return body


def plan(report, existing):
    if report.get("schema_version") != 2 or not isinstance(report.get("complete"), bool) or "main" not in report.get("covered_targets", []):
        raise ValueError("refusing issue reconciliation without scheduled main scan evidence")
    for key, finding in report["findings"].items():
        # Trivy also reports vendor advisory IDs (e.g. SUSE-SU-2026:2925-1)
        # when the vendor does not provide per-CVE package records. Keep that
        # evidence instead of dropping it or guessing CVEs from prose.
        if not re.fullmatch(r"[A-Z0-9][A-Za-z0-9:._-]{1,199}", finding["cve"]):
            raise ValueError("unexpected vulnerability identifier")
        if key != hashlib.sha256((finding["cve"] + "\n" + finding["package"]).encode()).hexdigest():
            raise ValueError("invalid CVE/package identity")
    managed = {}
    for issue in existing:
        match = MARKER.match(issue.get("body") or "")
        if match and issue.get("user", {}).get("login") == "github-actions[bot]" and "pull_request" not in issue:
            if match[1] in managed:
                raise ValueError(f"duplicate managed issues for {match[1]}; reconcile before creating more")
            managed[match[1]] = issue
    operations = []
    for key, finding in sorted(report["findings"].items()):
        if finding["suppression"]:
            continue
        cve = finding["cve"]
        desired = {"title": f"[image scan] {finding['severity']}: {cve} in {finding['package']}"[:250],
                   "body": issue_body(key, finding, report),
                   "state": "open"}
        issue = managed.get(key)
        if issue is not None:
            old = STATE.search(issue["body"])
            if not report["complete"] or not old or not set(json.loads(old[1])["targets"]).issubset(report["covered_targets"]):
                # Retain earlier evidence on partial scans. Positive evidence
                # can reopen an issue, but cannot erase earlier affected images.
                if issue["state"] == "closed":
                    body = re.sub(r"(?m)^\*\*Status:\*\*.*$", "**Status:** Present again in a partial scan; earlier evidence is retained.", issue["body"].split("\n**Resolution:**", 1)[0], count=1)
                    operations.append({"action": "update", "cve": cve, "number": issue["number"],
                                       "data": {"state": "open", "body": body}})
                continue
        if issue is None:
            operations.append({"action": "create", "cve": cve, "data": {"title": desired["title"], "body": desired["body"], "labels": [LABEL]}})
        elif any(issue.get(k) != v for k, v in desired.items()):
            operations.append({"action": "update", "cve": cve, "number": issue["number"], "data": desired})
    for key, issue in managed.items():
        finding = report["findings"].get(key)
        if not report["complete"] or issue["state"] == "closed" or (finding and not finding["suppression"]):
            continue
        metadata = STATE.search(issue["body"])
        if not metadata:
            continue
        old = json.loads(metadata[1])
        if not set(old["targets"]).issubset(report["covered_targets"]):
            continue
        if finding:
            suppression = finding["suppression"]
            resolution = f"suppressed until {suppression['expires']} — {suppression['reason']}"
        elif any(old["package"] in report["shipped_packages"].get(repo, []) for repo in old["repositories"]):
            resolution = "fixed — the package is still shipped, but a complete scan no longer reports this HIGH/CRITICAL finding"
        else:
            resolution = "no longer shipped — the affected package/image is absent from the complete inventory"
        body = re.sub(r"(?m)^\*\*Status:\*\*.*$", "**Status:** Resolved. Earlier affected images are retained below for reference.", issue["body"].split("\n**Resolution:**", 1)[0], count=1)
        body += "\n**Resolution:** " + resolution + ".\n"
        operations.append({"action": "close", "cve": old["cve"], "number": issue["number"],
                           "data": {"state": "closed", "state_reason": "completed", "body": body}})
    # Resolve/update existing issues before creating new ones. Critical findings
    # are created first, including the motivating Keycloak regression.
    return sorted(operations, key=lambda op: (op["action"] == "create", "CRITICAL" not in op["data"].get("title", ""), op["cve"]))


class GitHub:
    def __init__(self, repository):
        self.base = f"https://api.github.com/repos/{repository}/"
        self.token = os.environ["GH_TOKEN"]

    def request(self, method, endpoint, data=None):
        request = Request(self.base + endpoint, method=method,
                          data=json.dumps(data).encode() if data is not None else None,
                          headers={"Authorization": "Bearer " + self.token,
                                   "Accept": "application/vnd.github+json",
                                   "X-GitHub-Api-Version": "2022-11-28"})
        # Do not retry an ambiguous POST: a later run
        # discovers any issue that GitHub created before the connection failed.
        with urlopen(request, timeout=60) as response:
            return json.load(response)

    def issues(self):
        result, page = [], 1
        while True:
            batch = self.request("GET", "issues?" + urlencode({"labels": LABEL, "state": "all", "per_page": 100, "page": page}))
            result.extend(batch)
            if len(batch) < 100:
                return result
            page += 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    report = json.loads(args.report.read_text())
    api = GitHub(args.repository)
    operations = plan(report, api.issues())
    counts = {action: sum(op["action"] == action for op in operations) for action in ("create", "update", "close")}
    print(json.dumps({"apply": args.apply, **counts}))
    if not args.apply:
        return
    try:
        api.request("GET", "labels/" + quote(LABEL, safe=""))
    except HTTPError as exc:
        if exc.code != 404:
            raise
        api.request("POST", "labels", {"name": LABEL, "color": "b60205", "description": "Security findings"})
    for operation in operations:
        if operation["action"] == "create":
            api.request("POST", "issues", operation["data"])
        else:
            api.request("PATCH", f"issues/{operation['number']}", operation["data"])
        print(f"{operation['action']}: {operation['cve']}", flush=True)
        # Stay below GitHub's 500 content-generating requests/hour secondary
        # limit, including the initial backlog. Unchanged issues cost no writes.
        time.sleep(7.5)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, KeyError) as exc:
        sys.exit(str(exc))
