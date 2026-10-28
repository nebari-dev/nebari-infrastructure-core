"""Issue lifecycle tests: no GitHub writes."""

import copy
import importlib.util
from pathlib import Path
import unittest

from test_scan_images import SCAN, DIGEST, OTHER, inventory_fixture, report_fixture

SPEC = importlib.util.spec_from_file_location("report_images", Path(__file__).with_name("report-image-issues.py"))
REPORT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(REPORT)


def evidence():
    return SCAN.issue_findings({DIGEST: report_fixture()}, inventory_fixture(), {}, True)


def managed_issue(report):
    return {**REPORT.plan(report, [])[0]["data"], "number": 745, "state": "open", "user": {"login": "github-actions[bot]"}}


class IssueTests(unittest.TestCase):
    def test_vendor_advisories_are_not_dropped(self):
        report = report_fixture()
        report["Results"][0]["Vulnerabilities"][0]["VulnerabilityID"] = "SUSE-SU-2026:2925-1"
        url = "https://www.suse.com/support/update/announcement/2026/suse-su-20262925-1/"
        report["Results"][0]["Vulnerabilities"][0]["References"] = [url]
        ev = SCAN.issue_findings({DIGEST: report}, inventory_fixture(), {}, True)
        self.assertEqual(len(REPORT.plan(ev, [])), 1)
        self.assertIn(url, REPORT.plan(ev, [])[0]["data"]["body"])

    def test_lifecycle_and_digest_change(self):
        report = evidence()
        issue = managed_issue(report)
        self.assertEqual(REPORT.plan(report, [issue]), [])
        report["targets"]["main"]["revision"] = "unrelated-commit"
        self.assertEqual(REPORT.plan(report, [issue]), [])
        issue["state"] = "closed"
        self.assertEqual(REPORT.plan(report, [issue])[0]["data"]["state"], "open")
        issue["state"] = "open"
        next(iter(report["findings"].values()))["affected"][0]["digest"] = OTHER
        operations = REPORT.plan(report, [issue])
        self.assertEqual(len(operations), 1)
        self.assertEqual(operations[0]["action"], "update")

    def test_cve_and_package_is_identity(self):
        report = report_fixture()
        other = copy.deepcopy(report["Results"][0]["Vulnerabilities"][0])
        other["PkgName"] = "different-package"
        report["Results"][0]["Vulnerabilities"].append(other)
        ev = SCAN.issue_findings({DIGEST: report}, inventory_fixture(), {}, True)
        self.assertEqual(len(REPORT.plan(ev, [])), 2)
        for finding in ev["findings"].values():
            finding["affected"].append({**finding["affected"][0], "digest": OTHER})
        self.assertEqual(len(REPORT.plan(ev, [])), 2)

    def test_resolution_reasons_are_distinct(self):
        for reason in ("fixed", "suppressed", "no longer shipped"):
            with self.subTest(reason=reason):
                report = evidence()
                issue = managed_issue(report)
                if reason == "suppressed":
                    next(iter(report["findings"].values()))["suppression"] = {"reason": "reviewed exception", "expires": "2026-12-01"}
                else:
                    report["findings"] = {}
                    if reason == "no longer shipped":
                        report["shipped_packages"] = {}
                operation = REPORT.plan(report, [issue])[0]
                self.assertEqual(operation["action"], "close")
                self.assertIn("**Resolution:** " + reason, operation["data"]["body"])

    def test_partial_scan_never_closes_or_erases_prior_evidence(self):
        report = evidence()
        issue = managed_issue(report)
        report["complete"] = False
        next(iter(report["findings"].values()))["affected"][0]["digest"] = OTHER
        self.assertEqual(REPORT.plan(report, [issue]), [])
        issue["state"] = "closed"
        self.assertEqual(REPORT.plan(report, [issue])[0]["data"]["state"], "open")
        issue["state"] = "open"
        report["findings"] = {}
        self.assertEqual(REPORT.plan(report, [issue]), [])

    def test_main_fix_retains_release_and_missing_release_preserves_evidence(self):
        report = evidence()
        report["covered_targets"].append("release")
        report["targets"]["release"] = {"revision": "v1", "status": "scanned"}
        affected = next(iter(report["findings"].values()))["affected"][0]
        affected["targets"].append("release")
        issue = managed_issue(report)
        affected["targets"] = ["release"]
        operation = REPORT.plan(report, [issue])[0]
        self.assertEqual(operation["data"]["state"], "open")
        report["covered_targets"] = ["main"]
        affected["targets"] = ["main"]
        self.assertEqual(REPORT.plan(report, [issue]), [])
        report["findings"] = {}
        self.assertEqual(REPORT.plan(report, [issue]), [])

    def test_human_issues_untouched_and_new_suppressions_do_not_open(self):
        report = evidence()
        issue = managed_issue(report)
        issue["user"]["login"] = "human"
        self.assertEqual(REPORT.plan(report, [issue])[0]["action"], "create")
        next(iter(report["findings"].values()))["suppression"] = {"reason": "reviewed", "expires": "2026-12-01"}
        self.assertEqual(REPORT.plan(report, []), [])
