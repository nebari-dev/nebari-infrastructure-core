#!/usr/bin/env python3
"""Freeze NIC's deployed images and scan main/release inventories with Trivy.

Standard library only. See docs/operations/image-scanning.md for the asset
schema, coverage limits, suppression policy and release/issue lifecycle.
"""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from urllib.error import HTTPError
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
PLATFORMS = ("linux/amd64", "linux/arm64")
DIGEST = re.compile(r"sha256:[a-f0-9]{64}\Z")


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def repository_name(image):
    repository = image.split("@", 1)[0]
    if ":" in repository.rsplit("/", 1)[-1]:
        repository = repository.rsplit(":", 1)[0]
    return repository


def github_json(repository, endpoint):
    headers = {"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
    if os.environ.get("GH_TOKEN"):
        headers["Authorization"] = "Bearer " + os.environ["GH_TOKEN"]
    request = Request(f"https://api.github.com/repos/{repository}/{endpoint}", headers=headers)
    with urlopen(request, timeout=60) as response:
        return json.load(response)


def download(url):
    with urlopen(url, timeout=120) as response:
        data = response.read(32 * 1024 * 1024 + 1)
    if len(data) > 32 * 1024 * 1024:
        raise ValueError("release inventory/checksums exceed size limit")
    return data


def merge_resolved(destination, digest, item):
    merged = destination.setdefault(digest, {**item, "images": [], "sources": [], "platforms": [], "targets": []})
    for key in ("images", "sources", "platforms", "targets"):
        merged[key] = sorted(set(merged[key] + item[key]))


def freeze_images(inv, crane):
    """Resolve a tag once, then enumerate its immutable platform manifests.

    crane digest --platform alone is insufficient: a single-platform manifest
    can return successfully for the wrong architecture. Inspect its config too.
    """
    resolved, matrix, gaps, errors = {}, {}, [], []
    for image, sources in sorted(inv["images"].items()):
        try:
            digest = subprocess.check_output([crane, "digest", image], text=True).strip()
            if not DIGEST.fullmatch(digest):
                raise ValueError("invalid registry digest: " + digest)
            reference = repository_name(image) + "@" + digest
            manifest = json.loads(subprocess.check_output([crane, "manifest", reference], text=True))
            available = {}
            if "manifests" in manifest:
                for child in manifest["manifests"]:
                    platform = child.get("platform", {})
                    name = platform.get("os", "") + "/" + platform.get("architecture", "")
                    if name in PLATFORMS:
                        if name in available:
                            raise ValueError("ambiguous platform variants: " + name)
                        available[name] = child["digest"]
            else:
                config = json.loads(subprocess.check_output([crane, "config", reference], text=True))
                available[config["os"] + "/" + config["architecture"]] = digest
            if not any(platform in available for platform in PLATFORMS):
                raise ValueError("no supported Linux image platform")
            matrix[image] = {}
            for platform in PLATFORMS:
                if platform not in available:
                    matrix[image][platform] = {"status": "not-published"}
                    gaps.append(f"{image}: upstream publishes no {platform} image; this variant cannot be scanned or deployed.")
                    continue
                child_digest = available[platform]
                if not DIGEST.fullmatch(child_digest):
                    raise ValueError("invalid platform manifest digest")
                matrix[image][platform] = {"status": "resolved", "digest": child_digest}
                targets = [name for name, target in inv["targets"].items() if image in target["images"]]
                merge_resolved(resolved, child_digest, {
                    "reference": repository_name(image) + "@" + child_digest, "images": [image],
                    "sources": sources, "platforms": [platform], "targets": targets,
                })
        except (subprocess.CalledProcessError, ValueError, KeyError) as exc:
            errors.append(f"{image}: {exc}")
    return resolved, matrix, gaps, errors


def chart_changes(charts, baseline, label):
    return [{"chart": ref, "before": baseline[ref], "after": digest, "baseline": label}
            for ref, digest in sorted(charts.items()) if ref in baseline and baseline[ref] != digest]


def validate_inventory(inv):
    if inv.get("schema_version") != 1 or not inv.get("images") or not inv.get("resolved"):
        raise ValueError("invalid or empty frozen image inventory")
    if set(inv.get("platforms", [])) != set(PLATFORMS) or inv.get("errors"):
        raise ValueError("incomplete inventory platform coverage or resolution errors")
    expected_images = {image for target in inv["targets"].values() for image in target["images"]}
    if set(inv["images"]) != expected_images:
        raise ValueError("images missing target provenance")
    for digest, item in inv["resolved"].items():
        if not DIGEST.fullmatch(digest) or not item["reference"].endswith("@" + digest):
            raise ValueError("inventory reference must use its frozen manifest digest")
        if not item["platforms"] or not set(item["platforms"]).issubset(PLATFORMS):
            raise ValueError("invalid image platforms")
        if not item["images"] or not item["sources"] or not item["targets"]:
            raise ValueError("missing image provenance")
        if not set(item["images"]).issubset(inv["images"]) or not set(item["targets"]).issubset(inv["targets"]):
            raise ValueError("unknown image or target")
    for name, target in inv["targets"].items():
        if target["status"] != "scanned":
            continue
        for image in target["images"]:
            matrix = target["image_platforms"][image]
            if set(matrix) != set(PLATFORMS):
                raise ValueError("missing architecture entry for " + image)
            if not any(entry["status"] == "resolved" for entry in matrix.values()):
                raise ValueError("no resolved architecture for " + image)
            for platform, entry in matrix.items():
                if entry["status"] == "not-published":
                    if not inv.get("platform_gaps"):
                        raise ValueError("missing prominent architecture coverage warning")
                    continue
                item = inv["resolved"].get(entry.get("digest"), {})
                if entry["status"] != "resolved" or image not in item.get("images", []) or name not in item.get("targets", []) or platform not in item.get("platforms", []):
                    raise ValueError("unresolved inventory entry for " + image)


def add_latest_release(result, repository):
    try:
        release = github_json(repository, "releases/latest")
    except HTTPError as exc:
        if exc.code != 404:
            raise
        result["notes"].append("No published stable release; scanning checkout only.")
        return
    tag = release["tag_name"]
    assets = {asset["name"]: asset for asset in release["assets"]}
    if "images.json" not in assets:
        result["targets"]["release"] = {"revision": tag, "status": "asset-not-supported", "images": []}
        result["notes"].append(f"Latest release {tag} has no images.json asset; scanning checkout only until a release includes it.")
        return
    # Never re-render an old tag: mutable chart/image tags would change history.
    data = download(assets["images.json"]["browser_download_url"])
    sums = download(assets["checksums.txt"]["browser_download_url"]).decode()
    expected = [line.split()[0] for line in sums.splitlines()
                if len(line.split()) == 2 and line.split()[1].lstrip("*") == "images.json"]
    if expected != [hashlib.sha256(data).hexdigest()]:
        raise ValueError("release images.json does not match checksums.txt")
    released = json.loads(data)
    validate_inventory(released)
    if set(released["targets"]) != {"release"}:
        raise ValueError("release asset must contain only the release target")
    result["targets"]["release"] = {**released["targets"]["release"], "revision": tag,
                                    "asset_sha256": expected[0]}
    for image, sources in released["images"].items():
        result["images"][image] = sorted(set(result["images"].get(image, []) + sources))
    for digest, item in released["resolved"].items():
        merge_resolved(result["resolved"], digest, {**item, "targets": ["release"]})
    result["release_charts"] = released["charts"]
    result["chart_drift"] += chart_changes(result["charts"], released["charts"], tag + " release asset")
    result["platform_gaps"] += [f"Release {tag}: {gap}" for gap in released.get("platform_gaps", [])]
    result["notes"] += [f"Release {tag}: {note}" for note in released.get("notes", [])]


def inventory(output, target="checkout", include_latest=False, repository="nebari-dev/nebari-infrastructure-core", crane="crane"):
    with tempfile.TemporaryDirectory(prefix="nic-images-") as tmp:
        subprocess.run(["go", "test", "-count=1", "-timeout=30m", "-run",
                        "^(TestDeployedImageInventory|TestDeploymentSourceCoverage)$", "./..."],
                       cwd=ROOT, env={**os.environ, "NIC_IMAGE_INVENTORY_DIR": tmp}, check=True)
        shards = list(Path(tmp).glob("*.json"))
        if not {"argocd", "aws", "hetzner", "local", "longhorn"}.issubset({p.stem for p in shards}):
            raise ValueError("incomplete inventory: missing installer shards")
        images, charts, notes = {}, {}, set()
        for shard in shards:
            data = json.loads(shard.read_text())
            notes.update(data["notes"] or [])
            charts.update(data["charts"] or {})
            for image, sources in data["images"].items():
                images.setdefault(image, set()).update(sources)
        if not images:
            raise ValueError("empty image inventory")
        revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
        result = {"schema_version": 1, "platforms": list(PLATFORMS),
                  "images": {k: sorted(v) for k, v in sorted(images.items())}, "charts": charts, "notes": sorted(notes),
                  "targets": {target: {"revision": revision, "status": "scanned", "images": sorted(images)}}}
        resolved, matrix, gaps, errors = freeze_images(result, crane)
        result.update(resolved=resolved, platform_gaps=gaps, errors=errors)
        result["targets"][target]["image_platforms"] = matrix
        baseline = json.loads((ROOT / "internal/imageinventory/chart-digests.json").read_text())
        for ref in sorted(set(charts) - set(baseline)):
            errors.append(f"Review new OCI chart {ref} and record {charts[ref]} in chart-digests.json before release/scanning")
        result["chart_drift"] = chart_changes(charts, baseline, "reviewed chart-digests.json")
        if include_latest and not errors:
            add_latest_release(result, repository)
        write_json(output, result)
        output.with_suffix(".txt").write_text("\n".join(sorted(result["images"])) + "\n")
        print("\n".join(sorted(result["images"])))
        notices = result["platform_gaps"] + [f"PINNED CHART CHANGED: {d['chart']}: {d['before']} -> {d['after']} ({d['baseline']})" for d in result["chart_drift"]]
        for notice in notices:
            print("::warning::" + notice)
        output.with_suffix(".md").write_text("# Image inventory coverage\n\n" + "\n".join("- " + note for note in errors + notices + result["notes"]) + "\n")
        if errors:
            raise ValueError("image resolution failed: " + "; ".join(errors))
        validate_inventory(result)
        return result


def validate_ignorefile(filename, today=None):
    """Return active suppressions; every entry needs a reason and valid expiry."""
    today = today or datetime.now(timezone.utc).date()
    reason, active, seen = [], {}, set()
    for number, raw in enumerate(filename.read_text().splitlines(), 1):
        line = raw.strip()
        if line.startswith("#"):
            comment = line[1:].strip()
            if comment.lower().startswith("reason:"):
                reason.append(comment[len("reason:"):].strip())
        elif not line:
            reason = []
        else:
            match = re.fullmatch(r"([A-Z0-9][A-Za-z0-9:._-]{1,199}) exp:([0-9]{4}-[0-9]{2}-[0-9]{2})", line)
            if not any(reason) or not match:
                raise ValueError(f"{filename}:{number}: each entry needs its own '# reason: ...' and exp:YYYY-MM-DD")
            expiry = datetime.strptime(match[2], "%Y-%m-%d").date()
            if match[1] in seen:
                raise ValueError("duplicate suppression: " + match[1])
            seen.add(match[1])
            if expiry > today:
                active[match[1]] = {"reason": " ".join(reason), "expires": match[2]}
            reason = []
    return active


def vulnerabilities(report):
    for result in report.get("Results", []):
        for vuln in result.get("Vulnerabilities") or []:
            if vuln["Severity"] in {"HIGH", "CRITICAL"}:
                yield result, vuln


def sarif_report(reports, resolved, ignored):
    rules, results = {}, {}
    # GitHub retains only 5,000 results per run. Roll up the same CVE/package
    # across images/architectures, retaining every affected digest in its message
    # and raw JSON, instead of silently losing thousands of individual records.
    grouped = issue_findings(reports, {"resolved": resolved, "targets": {}}, ignored, True)["findings"]
    for key, finding in grouped.items():
        if finding["suppression"]:
            continue
        cve = finding["cve"]
        rules.setdefault(cve, {"id": cve, "shortDescription": {"text": cve},
                               "help": {"text": "Inspect the affected image digests and package versions below. " + finding["url"]},
                               "properties": {"tags": ["security"], "security-severity": "7.0"}})
        if finding["severity"] == "CRITICAL":
            rules[cve]["properties"]["security-severity"] = "9.0"
        lines = [f"{cve} in {finding['package']} ({finding['severity']})"]
        locations = set()
        for affected in finding["affected"]:
            fix = "fixed in " + affected["fixed"] if affected["fixed"] else "NO UPSTREAM FIX (" + affected["status"] + ")"
            lines.append(f"{', '.join(affected['images'])} @ {affected['digest']} ({', '.join(affected['platforms'])}; {', '.join(affected['targets'])}): {affected['installed']}; {fix}")
            locations.update(s.split(" -> ", 1)[0] for s in affected["sources"])
        message = "\n".join(lines)
        if len(message.encode()) > 32768:
            raise ValueError("SARIF finding too large; split reporting before upload: " + cve)
        results[key] = {
            "ruleId": cve, "level": "error" if finding["severity"] == "CRITICAL" else "warning",
            "message": {"text": message},
            "locations": [{"physicalLocation": {"artifactLocation": {"uri": sorted(locations)[0]}, "region": {"startLine": 1}}}],
            "partialFingerprints": {"nicImageVulnerability/v2": key},
        }
    if len(results) > 5000:
        raise ValueError("SARIF exceeds GitHub's 5,000-result display limit; split reporting before upload")
    return {"$schema": "https://json.schemastore.org/sarif-2.1.0.json", "version": "2.1.0",
            "runs": [{"tool": {"driver": {"name": "Trivy deployed images", "informationUri": "https://trivy.dev",
                                           "rules": [rules[k] for k in sorted(rules)]}},
                      "automationDetails": {"id": "nic-deployed-images/"},
                      "results": [results[k] for k in sorted(results)]}]}


def finding_key(cve, package):
    return hashlib.sha256((cve + "\n" + package).encode()).hexdigest()


def issue_findings(reports, inv, ignored, complete):
    findings, shipped = {}, {}
    for digest, report in reports.items():
        item = inv["resolved"][digest]
        packages = {p["Name"] for result in report.get("Results", []) for p in (result.get("Packages") or [])}
        for image in item["images"]:
            shipped.setdefault(repository_name(image), set()).update(packages)
        for _, vuln in vulnerabilities(report):
            cve, package = vuln["VulnerabilityID"], vuln["PkgName"]
            key = finding_key(cve, package)
            url = vuln.get("PrimaryURL") or next((ref for ref in vuln.get("References", [])
                if cve.lower().replace(":", "") in ref.lower().replace(":", "")), "")
            finding = findings.setdefault(key, {"cve": cve, "package": package, "severity": "HIGH", "affected": [],
                                                "critical_unfixed": False, "url": url,
                                                "suppression": ignored.get(cve)})
            if vuln["Severity"] == "CRITICAL":
                finding["severity"] = "CRITICAL"
                finding["critical_unfixed"] |= not bool(vuln.get("FixedVersion"))
            affected = {**item, "digest": digest, "installed": vuln["InstalledVersion"],
                        "fixed": vuln.get("FixedVersion", ""), "status": vuln.get("Status", "unknown")}
            if affected not in finding["affected"]:
                finding["affected"].append(affected)
    for finding in findings.values():
        finding["affected"].sort(key=lambda a: (a["digest"], a["installed"], a["fixed"]))
    return {"schema_version": 2, "complete": complete, "targets": inv["targets"], "findings": findings,
            "covered_targets": sorted(name for name, target in inv["targets"].items() if target["status"] == "scanned"),
            "shipped_packages": {repo: sorted(names) for repo, names in sorted(shipped.items())}}


def scan(input_path, output, trivy, ignorefile=ROOT / ".trivyignore", fail_on_fixable=False):
    inv = json.loads(input_path.read_text())
    output.mkdir(parents=True, exist_ok=True)
    sarif = output / "results.sarif"
    sarif.unlink(missing_ok=True)
    (output / "findings.json").unlink(missing_ok=True)
    validate_inventory(inv)
    ignored = validate_ignorefile(ignorefile)
    reports, failures, summary = {}, [], []
    for digest, item in sorted(inv["resolved"].items()):
        report_path = output / (digest.removeprefix("sha256:") + ".json")
        log_path = report_path.with_suffix(".log")
        print(f"Scanning {item['reference']} ({', '.join(item['platforms'])})", flush=True)
        with log_path.open("w") as log:
            process = subprocess.run(
                [trivy, "image", "--image-src", "remote", "--platform", item["platforms"][0],
                 "--scanners", "vuln", "--severity", "HIGH,CRITICAL", "--list-all-pkgs",
                 "--exit-code", "0", "--timeout", "15m", "--quiet", "--format", "json",
                 # Preserve raw evidence to distinguish suppressed/fixed/removed.
                 # Apply validated native ignore IDs and expiry after scanning.
                 "--ignorefile", os.devnull, "--output", str(report_path), item["reference"]],
                stdout=log, stderr=log, check=False)
        try:
            if process.returncode:
                raise ValueError(log_path.read_text())
            report = json.loads(report_path.read_text())
            for result in report.get("Results", []):
                if result.get("Vulnerabilities") and not result.get("Packages"):
                    raise ValueError("missing installed package evidence for issue resolution")
            reports[digest] = report
        except (ValueError, OSError) as exc:
            failures.append(item["reference"])
            summary.append({"images": item["images"], "platforms": item["platforms"], "error": str(exc)})
            print(f"::error::Scan failed: {item['reference']}: {exc}", file=sys.stderr)
            continue
        vulns = [v for _, v in vulnerabilities(report) if v["VulnerabilityID"] not in ignored]
        summary.append({"images": item["images"], "platforms": item["platforms"], "digest": digest,
                        "HIGH": sum(v["Severity"] == "HIGH" for v in vulns),
                        "CRITICAL": sum(v["Severity"] == "CRITICAL" for v in vulns),
                        "critical_unfixed": sum(v["Severity"] == "CRITICAL" and not v.get("FixedVersion") for v in vulns),
                        "fixable": sum(bool(v.get("FixedVersion")) for v in vulns),
                        "suppressed": sum(v["VulnerabilityID"] in ignored for _, v in vulnerabilities(report))})
    write_json(output / "summary.json", summary)
    findings = issue_findings(reports, inv, ignored, not failures)
    write_json(output / "findings.json", findings)
    lines = ["# Deployed image vulnerabilities", "", "All HIGH/CRITICAL findings, including unfixed, will-not-fix and end-of-life; report-only.", "",
             "| Images | Platforms | Critical | High | Critical without upstream fix |",
             "| --- | --- | ---: | ---: | ---: |"]
    lines += [f"| {', '.join('`' + a + '`' for a in r['images'])} | {', '.join(r['platforms'])} | {r.get('CRITICAL', 'FAILED')} | {r.get('HIGH', 'FAILED')} | {r.get('critical_unfixed', 'FAILED')} |" for r in summary]
    lines += ["", "## Critical findings without an upstream fix", ""]
    lines += [f"- **{f['cve']} — {f['package']}**: no upstream fix for at least one affected image." for f in findings["findings"].values() if f["critical_unfixed"] and not f["suppression"]]
    lines += ["", "## Coverage warnings", ""] + [f"- **{gap}**" for gap in inv["platform_gaps"]]
    lines += [f"- **PINNED CHART CHANGED:** `{d['chart']}`: `{d['before']}` → `{d['after']}` ({d['baseline']})." for d in inv["chart_drift"]]
    lines += [f"- {note}" for note in inv["notes"]]
    if failures:
        lines += ["", "**INCOMPLETE SCAN: existing issues must remain open; SARIF withheld.**"]
    (output / "summary.md").write_text("\n".join(lines) + "\n")
    if failures:
        raise RuntimeError(f"{len(failures)} scans failed; no resolution or replacement SARIF allowed")
    write_json(sarif, sarif_report(reports, inv["resolved"], ignored))
    if fail_on_fixable and any(row["fixable"] for row in summary):
        raise RuntimeError("fixable HIGH/CRITICAL findings (--fail-on-fixable)")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    build = sub.add_parser("inventory")
    build.add_argument("--output", type=Path, required=True)
    build.add_argument("--target", choices=["checkout", "main", "pull-request", "release"], default="checkout")
    build.add_argument("--include-latest-release", action="store_true")
    build.add_argument("--repository", default="nebari-dev/nebari-infrastructure-core")
    build.add_argument("--crane", default="crane")
    check = sub.add_parser("scan")
    check.add_argument("--inventory", type=Path, required=True)
    check.add_argument("--output", type=Path, required=True)
    check.add_argument("--trivy", default="trivy")
    check.add_argument("--ignorefile", type=Path, default=ROOT / ".trivyignore")
    check.add_argument("--fail-on-fixable", action="store_true")
    args = parser.parse_args()
    if args.command == "inventory":
        inventory(args.output.resolve(), args.target, args.include_latest_release, args.repository, args.crane)
    else:
        scan(args.inventory.resolve(), args.output.resolve(), args.trivy, args.ignorefile.resolve(), args.fail_on_fixable)


if __name__ == "__main__":
    try:
        main()
    except (subprocess.CalledProcessError, ValueError, RuntimeError, OSError, KeyError) as exc:
        sys.exit(str(exc))
