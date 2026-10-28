"""Offline regressions for frozen releases, architectures and scan policy."""

import copy
from datetime import date
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("scan_images", Path(__file__).with_name("scan-images.py"))
SCAN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SCAN)
DIGEST = "sha256:" + "a" * 64
OTHER = "sha256:" + "b" * 64


def inventory_fixture(target="main", digest=DIGEST):
    image = "example/image:v1"
    return {"schema_version": 1, "platforms": list(SCAN.PLATFORMS), "images": {image: ["source.go"]},
            "charts": {"example/chart:1": DIGEST}, "chart_drift": [], "platform_gaps": [], "notes": [], "errors": [],
            "resolved": {digest: {"reference": "example/image@" + digest, "images": [image], "sources": ["source.go"],
                                  "platforms": list(SCAN.PLATFORMS), "targets": [target]}},
            "targets": {target: {"revision": "abc", "status": "scanned", "images": [image],
                                 "image_platforms": {image: {p: {"status": "resolved", "digest": digest} for p in SCAN.PLATFORMS}}}}}


def report_fixture(fixed="26.7.2", status="fixed"):
    return {"Results": [{"Target": "Java", "Packages": [{"Name": "org.keycloak:keycloak-services"}],
                         "Vulnerabilities": [{"VulnerabilityID": "CVE-2026-18963", "Severity": "CRITICAL",
                                              "PkgName": "org.keycloak:keycloak-services", "InstalledVersion": "26.5.0",
                                              "FixedVersion": fixed, "Status": status}]}]}


class ScanTests(unittest.TestCase):
    def test_platforms_match_release_architectures(self):
        config = (SCAN.ROOT / ".goreleaser.yml").read_text()
        block = re.search(r"    goarch:\n((?:      - [^\n]+\n)+)", config)
        self.assertIsNotNone(block, "review image platforms when the release architecture list changes")
        self.assertEqual({"linux/" + line.split("- ")[1] for line in block[1].splitlines()}, set(SCAN.PLATFORMS))

    def test_sarif_rollup_preserves_all_digests_with_stable_identity(self):
        inv = inventory_fixture()
        inv["resolved"][OTHER] = {**inv["resolved"][DIGEST], "reference": "example/image@" + OTHER}
        reports = {DIGEST: report_fixture(), OTHER: report_fixture()}
        results = SCAN.sarif_report(reports, inv["resolved"], {})["runs"][0]["results"]
        self.assertEqual(len(results), 1)
        self.assertIn(DIGEST, results[0]["message"]["text"])
        self.assertIn(OTHER, results[0]["message"]["text"])
        self.assertEqual(results[0]["partialFingerprints"], SCAN.sarif_report({OTHER: report_fixture()}, inv["resolved"], {})["runs"][0]["results"][0]["partialFingerprints"])

    def test_all_high_critical_including_unfixed_are_reported(self):
        for status in ("affected", "will_not_fix", "end_of_life"):
            with self.subTest(status=status):
                inv = inventory_fixture()
                reports = {DIGEST: report_fixture("", status)}
                sarif = SCAN.sarif_report(reports, inv["resolved"], {})
                self.assertEqual(len(sarif["runs"][0]["results"]), 1)
                self.assertIn("NO UPSTREAM FIX", sarif["runs"][0]["results"][0]["message"]["text"])
                finding = next(iter(SCAN.issue_findings(reports, inv, {}, True)["findings"].values()))
                self.assertTrue(finding["critical_unfixed"])
                self.assertEqual(finding["affected"][0]["status"], status)

    def test_digest_aliases_and_architectures(self):
        inv = inventory_fixture()
        inv["images"]["example/image:latest"] = ["other.go"]
        inv["targets"]["main"]["images"].append("example/image:latest")
        index = {"manifests": [{"digest": d, "platform": {"os": "linux", "architecture": arch}}
                               for d, arch in [(DIGEST, "amd64"), (OTHER, "arm64")]]}
        def crane(args, **kwargs):
            return DIGEST if args[1] == "digest" else json.dumps(index)
        with patch.object(SCAN.subprocess, "check_output", side_effect=crane):
            resolved, matrix, gaps, errors = SCAN.freeze_images(inv, "crane")
        self.assertEqual(len(resolved), 2)
        self.assertEqual(resolved[DIGEST]["images"], ["example/image:latest", "example/image:v1"])
        self.assertEqual(matrix["example/image:v1"]["linux/arm64"]["digest"], OTHER)
        self.assertEqual(gaps + errors, [])

    def test_single_architecture_is_not_mistaken_for_multiarch(self):
        def crane(args, **kwargs):
            return {"digest": DIGEST, "manifest": "{}", "config": '{"os":"linux","architecture":"amd64"}'}[args[1]]
        with patch.object(SCAN.subprocess, "check_output", side_effect=crane):
            resolved, matrix, gaps, errors = SCAN.freeze_images(inventory_fixture(), "crane")
        self.assertEqual(resolved[DIGEST]["platforms"], ["linux/amd64"])
        self.assertEqual(matrix["example/image:v1"]["linux/arm64"], {"status": "not-published"})
        self.assertIn("cannot be scanned or deployed", gaps[0])
        self.assertEqual(errors, [])

    def test_registry_errors_are_not_platform_gaps(self):
        with patch.object(SCAN.subprocess, "check_output", side_effect=subprocess.CalledProcessError(1, "crane")):
            resolved, _, gaps, errors = SCAN.freeze_images(inventory_fixture(), "crane")
        self.assertFalse(resolved)
        self.assertFalse(gaps)
        self.assertEqual(len(errors), 1)

    def test_scan_error_withholds_sarif_and_retains_partial_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            inv = inventory_fixture()
            inv["resolved"][OTHER] = {**inv["resolved"][DIGEST], "reference": "example/image@" + OTHER}
            SCAN.write_json(root / "images.json", inv)
            (root / "results.sarif").write_text("stale")
            def fake_scan(args, **kwargs):
                self.assertNotIn("--ignore-unfixed", args)
                self.assertIn("--list-all-pkgs", args)
                Path(args[args.index("--output") + 1]).write_text(json.dumps(report_fixture()))
                return subprocess.CompletedProcess(args, int(args[-1].endswith(OTHER)))
            with patch.object(SCAN.subprocess, "run", side_effect=fake_scan) as run:
                with self.assertRaises(RuntimeError):
                    SCAN.scan(root / "images.json", root, "trivy")
            self.assertEqual(run.call_count, 2)
            self.assertFalse((root / "results.sarif").exists())
            partial = json.loads((root / "findings.json").read_text())
            self.assertFalse(partial["complete"])
            self.assertEqual(len(partial["findings"]), 1)

    def test_optional_failure_gate_only_counts_fixable_findings(self):
        for fixed in ("", "26.7.2"):
            with self.subTest(fixed=fixed), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                SCAN.write_json(root / "images.json", inventory_fixture())
                def fake_scan(args, **kwargs):
                    Path(args[args.index("--output") + 1]).write_text(json.dumps(report_fixture(fixed)))
                    return subprocess.CompletedProcess(args, 0)
                with patch.object(SCAN.subprocess, "run", side_effect=fake_scan):
                    if fixed:
                        with self.assertRaisesRegex(RuntimeError, "fixable HIGH"):
                            SCAN.scan(root / "images.json", root, "trivy", fail_on_fixable=True)
                    else:
                        SCAN.scan(root / "images.json", root, "trivy", fail_on_fixable=True)
                self.assertTrue((root / "results.sarif").exists())

    def test_ignore_requires_reason_and_expiry(self):
        for body, valid in [
            ("# policy header\n", True),
            ("# reason: tracking issue and evidence\nCVE-2026-1234 exp:2026-12-01\n", True),
            ("CVE-2026-1234 exp:2026-12-01\n", False),
            ("# reason: evidence\nCVE-2026-1234\n", False),
            ("# reason: evidence\nCVE-2026-1234 exp:2026-02-30\n", False),
            ("# reason: evidence\nCVE-2026-1234 exp:2026-12-01\nCVE-2026-5678 exp:2026-12-01\n", False),
        ]:
            with self.subTest(body=body), tempfile.TemporaryDirectory() as tmp:
                filename = Path(tmp) / ".trivyignore"
                filename.write_text(body)
                if valid:
                    SCAN.validate_ignorefile(filename, date(2026, 10, 28))
                else:
                    with self.assertRaises(ValueError):
                        SCAN.validate_ignorefile(filename, date(2026, 10, 28))

    def test_expired_suppression_restores_finding_and_keeps_raw_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            filename = Path(tmp) / ".trivyignore"
            filename.write_text("# reason: temporary exception\nCVE-2026-18963 exp:2026-10-28\n")
            before = SCAN.validate_ignorefile(filename, date(2026, 10, 27))
            after = SCAN.validate_ignorefile(filename, date(2026, 10, 28))
            inv, reports = inventory_fixture(), {DIGEST: report_fixture()}
            self.assertFalse(SCAN.sarif_report(reports, inv["resolved"], before)["runs"][0]["results"])
            self.assertTrue(SCAN.sarif_report(reports, inv["resolved"], after)["runs"][0]["results"])
            self.assertTrue(next(iter(SCAN.issue_findings(reports, inv, before, True)["findings"].values()))["suppression"])

    def test_release_asset_is_frozen_even_when_main_tag_changes(self):
        main, released = inventory_fixture(digest=OTHER), inventory_fixture("release")
        data = json.dumps(released).encode()
        release = {"tag_name": "v1.0.0", "assets": [{"name": n, "browser_download_url": n} for n in ["images.json", "checksums.txt"]]}
        downloads = {"images.json": data, "checksums.txt": (hashlib.sha256(data).hexdigest() + "  images.json\n").encode()}
        with patch.object(SCAN, "github_json", return_value=release), patch.object(SCAN, "download", side_effect=downloads.get), patch.object(SCAN.subprocess, "run") as run, patch.object(SCAN.subprocess, "check_output") as check:
            SCAN.add_latest_release(main, "owner/repo")
            run.assert_not_called()
            check.assert_not_called()
        SCAN.validate_inventory(main)
        self.assertEqual(main["resolved"][DIGEST]["targets"], ["release"])
        self.assertEqual(main["resolved"][OTHER]["targets"], ["main"])

    def test_release_without_asset_and_invalid_checksum(self):
        main = inventory_fixture()
        with patch.object(SCAN, "github_json", return_value={"tag_name": "v0", "assets": []}):
            SCAN.add_latest_release(main, "owner/repo")
        self.assertEqual(main["targets"]["release"]["status"], "asset-not-supported")
        assets = [{"name": n, "browser_download_url": n} for n in ["images.json", "checksums.txt"]]
        with patch.object(SCAN, "github_json", return_value={"tag_name": "v1", "assets": assets}), patch.object(SCAN, "download", return_value=b"invalid"):
            with self.assertRaisesRegex(ValueError, "checksums"):
                SCAN.add_latest_release(inventory_fixture(), "owner/repo")

    def test_chart_republication_is_flagged(self):
        self.assertEqual(SCAN.chart_changes({"quay.io/nebari/charts/nebari-operator:0.2.0": OTHER},
                                           {"quay.io/nebari/charts/nebari-operator:0.2.0": DIGEST}, "release"),
                         [{"chart": "quay.io/nebari/charts/nebari-operator:0.2.0", "before": DIGEST, "after": OTHER, "baseline": "release"}])

    def test_missing_platform_entry_is_rejected(self):
        inv = inventory_fixture()
        del inv["targets"]["main"]["image_platforms"]["example/image:v1"]["linux/arm64"]
        with self.assertRaisesRegex(ValueError, "architecture"):
            SCAN.validate_inventory(inv)


if __name__ == "__main__":
    unittest.main()
