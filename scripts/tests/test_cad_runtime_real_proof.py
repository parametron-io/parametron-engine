#!/usr/bin/env python3
"""Permanent Task 14 coverage for the real FreeCAD integration proof driven
by scripts/cad_runtime_integration_proof.py --mode real.

This entire module is opt-in: by default (ordinary `go test ./...` /
`python -m unittest discover` runs) every test here is skipped, because
building the real sibling FreeCAD launcher and running real FreeCAD is slow
and requires the parametron-freecad sibling checkout. To run it for real:

    nix develop --command env PARAMETRON_RUN_FREECAD_INTEGRATION=1 \\
      python -m unittest scripts.tests.test_cad_runtime_real_proof -v

A skip is only acceptable for ordinary default-suite runs; the explicit
command above must not skip.
"""

import functools
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
HARNESS = REPO_ROOT / "scripts" / "cad_runtime_integration_proof.py"
FREECAD_REPO = REPO_ROOT.parent / "parametron-freecad"
REQUIRED_ENV = "PARAMETRON_RUN_FREECAD_INTEGRATION"
REQUIRED_COMMAND = (
    "nix develop --command env PARAMETRON_RUN_FREECAD_INTEGRATION=1 "
    "python -m unittest scripts.tests.test_cad_runtime_real_proof -v"
)

sys.path.insert(0, str(REPO_ROOT / "scripts"))
import cad_runtime_integration_proof as harness  # noqa: E402  (proof tooling, not production source)


def run_harness(args, timeout=1800):
    import subprocess
    cmd = [sys.executable, str(HARNESS), *args]
    return subprocess.run(cmd, cwd=REPO_ROOT, capture_output=True, text=True, timeout=timeout)


@functools.lru_cache(maxsize=1)
def single_real_run():
    workspace = Path(tempfile.mkdtemp(prefix="task14-real-proof-"))
    report_path = workspace / "report.json"
    result = run_harness([
        "--mode", "real",
        "--freecad-repo", str(FREECAD_REPO),
        "--workspace", str(workspace),
        "--report", str(report_path),
        "--keep-workspace",
    ])
    report = None
    if report_path.exists():
        report = json.loads(report_path.read_text(encoding="utf-8"))
    return {"workspace": workspace, "report_path": report_path, "report": report, "result": result}


def find_run_report(out):
    """The run's own report.json, excluding the raw evidence copy the record
    package embeds under parametron-record-package/raw/ -- the same
    exclusion pattern stable_cli_facts uses for manifest.json/metadata.json."""
    matches = [p for p in out.rglob("report.json") if "parametron-record-package" not in p.parts]
    assert len(matches) == 1, f"expected one run report.json beneath {out}, found {len(matches)}"
    return matches[0]


def require_real_passed(test):
    run = single_real_run()
    ok = (
        run["result"].returncode == 0
        and run["report"] is not None
        and run["report"].get("status") in ("passed",)
        and run["report"].get("realRuntime") is not None
    )
    if not ok:
        test.fail(
            "the shared `--mode real` harness run did not pass; a skip is not "
            "permitted for this explicit real-integration gate.\n"
            f"returncode={run['result'].returncode}\n"
            f"stdout(tail)={run['result'].stdout[-4000:]}\n"
            f"stderr(tail)={run['result'].stderr[-4000:]}"
        )
    return run


@unittest.skipUnless(
    os.environ.get(REQUIRED_ENV) == "1",
    f"real FreeCAD integration is opt-in; run: {REQUIRED_COMMAND}",
)
class RealFreeCADIntegrationTests(unittest.TestCase):
    """J1-J5: real wrapper build/smoke, real normal execution, repeated real
    execution, and STEP comparison, all read from a single shared `--mode
    real` harness run's report and kept workspace."""

    def test_real_wrapper_smoke(self):
        run = require_real_passed(self)
        rt = run["report"]["realRuntime"]
        self.assertEqual(rt["launcher"], "parametron-freecad")
        self.assertTrue(rt["smokePassed"])
        self.assertEqual(rt["host"], "freecadcmd")
        self.assertTrue(rt.get("freecadVersion"))

    def test_real_normal_engine_execution(self):
        run = require_real_passed(self)
        rt = run["report"]["realRuntime"]
        self.assertTrue(rt["normalRunPassed"])
        self.assertEqual(rt["stepHeader"], "ISO-10303-21")
        self.assertTrue(rt["preparedSourceSHA256"])
        self.assertTrue(rt["planHash"])
        self.assertTrue(rt["jobID"])

        # cache_isolated_cli (used internally by real_proof) reads results
        # from <root>/cwd/output, not <root>/out: the smoke fixture project's
        # active profile pins its own output_dir, so the --out CLI flag
        # value under <root>/out is not where the run actually lands.
        real1 = run["workspace"] / "real-1"
        out = real1 / "cwd" / "output"
        steps = list(out.rglob("*.step"))
        self.assertTrue(steps, "expected a real accepted STEP artifact")
        self.assertGreater(steps[0].stat().st_size, 0)
        with open(steps[0], "rb") as handle:
            self.assertTrue(handle.read(4096).startswith(b"ISO-10303-21"))

        report_doc = harness.json_file(find_run_report(out))
        self.assertEqual(report_doc["status"], "success")
        artifact_types = [a.get("type") for a in report_doc.get("artifacts", [])]
        self.assertIn("step", artifact_types)
        self.assertIn("json", artifact_types)
        for forbidden in ("parametron.observed", "parametron.verification", "source/"):
            self.assertFalse(
                any(forbidden in str(a.get("filename", "")) for a in report_doc.get("artifacts", [])),
                f"raw real runtime evidence {forbidden!r} was promoted to a registered artifact",
            )
        self.assertTrue(list(out.rglob("metadata.json")))
        self.assertTrue(list(out.rglob("parametron.record-package.json")))
        self.assertTrue(list(out.rglob("parametron.verification.json")))

    def test_repeated_real_execution_preserves_engine_identity(self):
        run = require_real_passed(self)
        rt = run["report"]["realRuntime"]
        self.assertTrue(rt["repeatedRunPassed"])
        self.assertTrue(rt["recordPackageStable"])

        real1 = run["workspace"] / "real-1"
        out = real1 / "cwd" / "output"
        report1 = harness.json_file(find_run_report(out))
        self.assertEqual(report1["planHash"], rt["planHash"])
        self.assertEqual(report1["jobs"][0]["jobId"], rt["jobID"])

    def test_repeated_real_step_is_identical_or_timestamp_only_variant(self):
        run = require_real_passed(self)
        rt = run["report"]["realRuntime"]
        if rt["stepBytesIdentical"]:
            self.assertEqual(rt["stepSHA256"][0], rt["stepSHA256"][1])
        else:
            self.assertTrue(
                rt["stepExporterMetadataOnlyVariance"],
                "repeated real STEP bytes differ beyond the bounded FreeCAD "
                "FILE_NAME exporter timestamp",
            )
            self.assertNotEqual(rt["stepSHA256"][0], rt["stepSHA256"][1])

    def test_repeated_real_runs_are_fresh_external_executions(self):
        run = require_real_passed(self)
        real1, real2 = run["workspace"] / "real-1", run["workspace"] / "real-2"
        step1 = harness.find_one(real1 / "cwd" / "output", "*.step")
        step2 = harness.find_one(real2 / "cwd" / "output", "*.step")
        self.assertNotEqual(str(step1), str(step2))
        self.assertGreater(step1.stat().st_size, 0)
        self.assertGreater(step2.stat().st_size, 0)
        # Each attempt is a completely independent cache-isolated root (its
        # own cwd/.cache), so a fresh FreeCAD execution is structurally
        # required to produce either STEP file at all.
        cache1 = list((real1 / "cwd" / ".cache").glob("**/*")) if (real1 / "cwd" / ".cache").exists() else []
        cache2 = list((real2 / "cwd" / ".cache").glob("**/*")) if (real2 / "cwd" / ".cache").exists() else []
        self.assertTrue(cache1)
        self.assertTrue(cache2)


@unittest.skipUnless(
    os.environ.get(REQUIRED_ENV) == "1",
    f"real FreeCAD integration is opt-in; run: {REQUIRED_COMMAND}",
)
class RealProofInvocationContractTests(unittest.TestCase):
    """Part K: --mode real must not silently substitute a fake runtime, and
    a missing/broken FreeCAD environment must be reported as a deterministic
    failure, never as success."""

    def test_missing_freecad_repo_is_reported_as_failure_not_success(self):
        with tempfile.TemporaryDirectory(prefix="task14-missing-freecad-repo-") as bogus_repo, \
             tempfile.TemporaryDirectory(prefix="task14-missing-freecad-ws-") as workspace:
            report_path = Path(workspace) / "report.json"
            import subprocess
            result = subprocess.run(
                [sys.executable, str(HARNESS), "--mode", "real",
                 "--freecad-repo", bogus_repo,
                 "--workspace", workspace, "--report", str(report_path)],
                cwd=REPO_ROOT, capture_output=True, text=True, timeout=300,
            )
            self.assertNotEqual(result.returncode, 0)
            report = json.loads(report_path.read_text(encoding="utf-8"))
            self.assertEqual(report["status"], "failed")
            self.assertNotIn("realRuntime", report)

    def test_real_mode_does_not_substitute_a_fake_runtime(self):
        run = require_real_passed(self)
        rt = run["report"]["realRuntime"]
        # A fake substitution would report our own controlled-runtime host
        # marker; the real launcher always reports the actual FreeCAD host.
        self.assertEqual(rt["host"], "freecadcmd")
        self.assertNotIn("fake", rt["host"])
        self.assertNotEqual(rt.get("freecadVersion"), [])


if __name__ == "__main__":
    unittest.main()
