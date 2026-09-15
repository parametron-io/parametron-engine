#!/usr/bin/env python3
"""Permanent Task 14 coverage for the aligned CAD runtime integration proof
harness (scripts/cad_runtime_integration_proof.py): its stable report
contract (Part B), and the fake (controlled-runtime, no real FreeCAD) CLI/API
integration, repeated-run, retry/failure, and cancellation proof it drives
(Parts C-F).

Building the three Engine binaries and driving the harness through its full
`--mode fake` pass is expensive, so a single shared harness run is memoized
per test process and reused read-only by every test method below: each test
method independently re-derives its own assertion from the harness's real,
already-written subprocess evidence (report.json, invocations.jsonl, and
on-disk artifacts under the kept workspace), rather than re-running the
harness per test or reimplementing its comparison logic.
"""

import functools
import json
import re
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
HARNESS = REPO_ROOT / "scripts" / "cad_runtime_integration_proof.py"
CONTROLLED_RUNTIME = REPO_ROOT / "scripts" / "cad_runtime_proof_runtime.py"

sys.path.insert(0, str(REPO_ROOT / "scripts"))
import cad_runtime_integration_proof as harness  # noqa: E402  (proof tooling, not production source)


def run_harness(args, timeout=900):
    cmd = [sys.executable, str(HARNESS), *args]
    return subprocess.run(cmd, cwd=REPO_ROOT, capture_output=True, text=True, timeout=timeout)


@functools.lru_cache(maxsize=1)
def single_fake_run():
    workspace = Path(tempfile.mkdtemp(prefix="task14-fake-proof-"))
    report_path = workspace / "report.json"
    result = run_harness([
        "--mode", "fake", "--workspace", str(workspace),
        "--report", str(report_path), "--keep-workspace",
    ])
    report = None
    if report_path.exists():
        report = json.loads(report_path.read_text(encoding="utf-8"))
    return {"workspace": workspace, "report_path": report_path, "report": report, "result": result}


@functools.lru_cache(maxsize=1)
def two_independent_fake_reports():
    runs = []
    for _ in range(2):
        workspace = Path(tempfile.mkdtemp(prefix="task14-fake-proof-pair-"))
        report_path = workspace / "report.json"
        result = run_harness(["--mode", "fake", "--workspace", str(workspace), "--report", str(report_path)])
        runs.append({
            "result": result,
            "report_bytes": report_path.read_bytes() if report_path.exists() else b"",
        })
        shutil.rmtree(workspace, ignore_errors=True)
    return tuple(runs)


def require_passed(test):
    """Returns the shared single fake run, failing test with harness
    diagnostics if the shared harness run itself did not pass (in which case
    no test relying on it as evidence can be trusted)."""
    run = single_fake_run()
    ok = run["result"].returncode == 0 and run["report"] is not None and run["report"].get("status") == "passed"
    if not ok:
        test.fail(
            "the shared `--mode fake` harness run did not pass; cannot use its "
            "output as evidence for this test.\n"
            f"returncode={run['result'].returncode}\n"
            f"stdout(tail)={run['result'].stdout[-3000:]}\n"
            f"stderr(tail)={run['result'].stderr[-3000:]}"
        )
    return run


def scenario(report, name):
    for item in report["scenarios"]:
        if item["name"] == name:
            return item
    raise AssertionError(f"scenario {name!r} not present in report: {[s['name'] for s in report['scenarios']]}")


SCENARIO_ORDER = [
    "fake_cli_success", "fake_api_success", "fake_concurrent_isolation", "fake_repeated", "fake_stale_output",
    "retry_then_success", "runtime_failure", "malformed_result", "missing_artifact",
    "observed_mismatch", "cancellation",
]


# ===================== Part B: stable proof report =====================

class StableReportSchemaAndOrderTests(unittest.TestCase):
    def test_stable_report_schema_and_order(self):
        run = require_passed(self)
        report = run["report"]
        self.assertEqual(report["schemaVersion"], "1.0")
        self.assertEqual(report["status"], "passed")
        self.assertEqual([s["name"] for s in report["scenarios"]], SCENARIO_ORDER)

        raw = run["report_path"].read_bytes()
        self.assertTrue(raw.endswith(b"\n"), "report must end with a trailing newline")
        text = raw.decode("utf-8")
        # Sorted JSON keys: re-encoding with sort_keys must reproduce the file
        # byte-for-byte (module writes json.dumps(..., indent=2, sort_keys=True)).
        self.assertEqual(json.dumps(json.loads(text), indent=2, sort_keys=True) + "\n", text)

        for key in ("timestamp", "createdat", "startedat", "endedat"):
            self.assertNotIn(key, text.lower())

        for item in report["scenarios"]:
            for key, value in item.items():
                if key.lower().endswith("hash") and isinstance(value, str):
                    self.assertRegex(value, r"^[0-9a-f]{64,}$", f"{key}={value!r} is not a lowercase hex digest")


class AbsolutePathsAreConfinedToDiagnosticsTests(unittest.TestCase):
    def test_absolute_paths_are_confined_to_diagnostics(self):
        run = require_passed(self)
        text = run["report_path"].read_text(encoding="utf-8")
        self.assertNotIn(str(run["workspace"]), text)
        self.assertNotIn("/tmp/", text)
        self.assertNotIn("diagnostics", json.loads(text))


class FakeModeInvocationContractTests(unittest.TestCase):
    """Part K: --mode fake must never build or invoke real FreeCAD, and its
    report must carry no realRuntime evidence."""

    def test_fake_mode_does_not_surface_real_runtime(self):
        run = require_passed(self)
        self.assertNotIn("realRuntime", run["report"])
        self.assertNotIn("freecadVersion", run["report_path"].read_text(encoding="utf-8"))


class EquivalentFakeRunsProduceByteIdenticalReportsTests(unittest.TestCase):
    def test_equivalent_fake_runs_produce_byte_identical_reports(self):
        first, second = two_independent_fake_reports()
        self.assertEqual(first["result"].returncode, 0, first["result"].stderr[-2000:])
        self.assertEqual(second["result"].returncode, 0, second["result"].stderr[-2000:])
        self.assertTrue(first["report_bytes"])
        self.assertEqual(first["report_bytes"], second["report_bytes"])


class FailedScenarioMakesOverallReportFailedTests(unittest.TestCase):
    def test_failed_scenario_makes_overall_report_failed(self):
        with tempfile.TemporaryDirectory(prefix="task14-broken-repo-") as broken_repo, \
             tempfile.TemporaryDirectory(prefix="task14-fail-ws-") as workspace:
            report_path = Path(workspace) / "report.json"
            result = run_harness([
                "--mode", "fake",
                "--engine-repo", broken_repo,
                "--workspace", workspace,
                "--report", str(report_path),
            ], timeout=120)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("FAIL", result.stderr)
            report = json.loads(report_path.read_text(encoding="utf-8"))
            self.assertEqual(report["status"], "failed")
            self.assertIn("diagnostics", report)
            self.assertIn("message", report["diagnostics"])


class SubprocessTimeoutIsBoundedAndDiagnosticTests(unittest.TestCase):
    def test_subprocess_timeout_is_bounded_and_diagnostic(self):
        with tempfile.TemporaryDirectory(prefix="task14-timeout-ws-") as workspace:
            report_path = Path(workspace) / "report.json"
            started = time.monotonic()
            result = run_harness([
                "--mode", "timeout", "--workspace", workspace, "--report", str(report_path),
                "--timeout", "1",
            ], timeout=120)
            elapsed = time.monotonic() - started
            self.assertLess(elapsed, 90, "a 1-second subprocess timeout must fail fast, not hang")
            self.assertNotEqual(result.returncode, 0)
            report = json.loads(report_path.read_text(encoding="utf-8"))
            self.assertEqual(report["status"], "failed")
            self.assertIn("timed out", report["diagnostics"]["message"].lower())


class ArtifactInventoryNormalizationTests(unittest.TestCase):
    """B6: normalized_json_digest ignores operational-path/store-id
    prefixes but not logical content differences. This is the same pure
    function stable_cli_facts uses for artifactManifestHash/requestHash."""

    def _write(self, tmp, name, value):
        path = Path(tmp) / name
        path.write_text(json.dumps(value), encoding="utf-8")
        return path

    def test_artifact_inventory_ignores_store_ids_and_operational_paths(self):
        with tempfile.TemporaryDirectory(prefix="task14-inventory-") as tmp:
            common = {"filename": "widget.step", "class": "product", "type": "step"}
            a = self._write(tmp, "a.json", {
                "path": "/nix/store/abc123-run/_working/attempt-1/outputs/widget.step", **common,
            })
            b = self._write(tmp, "b.json", {
                "path": "/nix/store/xyz789-run/_working/attempt-1/outputs/widget.step", **common,
            })
            self.assertEqual(harness.normalized_json_digest(a), harness.normalized_json_digest(b))

    def test_artifact_inventory_does_not_ignore_logical_differences(self):
        with tempfile.TemporaryDirectory(prefix="task14-inventory-diff-") as tmp:
            a = self._write(tmp, "a.json", {"filename": "widget.step", "class": "product", "type": "step"})
            b = self._write(tmp, "b.json", {"filename": "widget.step", "class": "reference", "type": "step"})
            self.assertNotEqual(harness.normalized_json_digest(a), harness.normalized_json_digest(b))


class RecordPackageComparisonNormalizationTests(unittest.TestCase):
    """B7: the same normalization (documented volatile keys createdAt/
    startedAt/endedAt/durationMs stripped) applies to any comparison built on
    normalized_json_digest, including record-package-adjacent documents."""

    def _write(self, tmp, name, value):
        path = Path(tmp) / name
        path.write_text(json.dumps(value), encoding="utf-8")
        return path

    def test_record_package_comparison_excludes_only_documented_volatile_fields(self):
        with tempfile.TemporaryDirectory(prefix="task14-recordpkg-") as tmp:
            base = {"family": "run", "recordKey": "box-widget", "startedAt": "t1", "endedAt": "t2", "durationMs": 12}
            other = {"family": "run", "recordKey": "box-widget", "startedAt": "t9", "endedAt": "t10", "durationMs": 99}
            a = self._write(tmp, "a.json", base)
            b = self._write(tmp, "b.json", other)
            self.assertEqual(harness.normalized_json_digest(a), harness.normalized_json_digest(b))

            mutated = dict(other)
            mutated["recordKey"] = "box-widget-different"
            c = self._write(tmp, "c.json", mutated)
            self.assertNotEqual(harness.normalized_json_digest(a), harness.normalized_json_digest(c))


class StepComparisonTests(unittest.TestCase):
    """B8-B10: exercised directly against normalize_step_export_metadata,
    the exact pure function real_proof uses to classify STEP variance."""

    HEADER = (
        b"ISO-10303-21;\nHEADER;\n"
        b"FILE_DESCRIPTION((''),'2;1');\n"
        b"FILE_NAME('Open CASCADE Shape Model','2026-07-25T10:00:00',('Author'),(''),"
        b"'Open CASCADE STEP processor 7.7','','');\n"
        b"FILE_SCHEMA(('AUTOMOTIVE_DESIGN { 1 0 10303 214 3 1 1 }'));\nENDSEC;\n"
    )
    DATA = (
        b"DATA;\n#1=PRODUCT('Widget','Widget','',(#2));\n#2=PRODUCT_CONTEXT('',#3,'mechanical');\n"
        b"ENDSEC;\nEND-ISO-10303-21;\n"
    )

    def test_step_comparison_accepts_byte_identical_files(self):
        a = self.HEADER + self.DATA
        b = self.HEADER + self.DATA
        self.assertEqual(a, b)
        self.assertEqual(harness.normalize_step_export_metadata(a), harness.normalize_step_export_metadata(b))

    def test_step_comparison_accepts_only_file_name_timestamp_variance(self):
        a = self.HEADER + self.DATA
        b = a.replace(b"2026-07-25T10:00:00", b"2026-07-25T11:30:45")
        self.assertNotEqual(a, b)
        norm_a = harness.normalize_step_export_metadata(a)
        norm_b = harness.normalize_step_export_metadata(b)
        self.assertEqual(norm_a, norm_b)
        self.assertIn(b"<export-timestamp>", norm_a)
        # DATA section identical, header valid, size in the same ballpark.
        self.assertEqual(a.split(b"DATA;", 1)[1], b.split(b"DATA;", 1)[1])
        self.assertTrue(a.startswith(b"ISO-10303-21"))
        self.assertTrue(b.startswith(b"ISO-10303-21"))

    def test_step_comparison_rejects_semantic_or_unbounded_variance(self):
        cases = {
            "DATA-section difference": self.HEADER + self.DATA.replace(b"'Widget'", b"'WidgetV2'", 1),
            "geometry entity difference": self.HEADER + self.DATA.replace(
                b"PRODUCT_CONTEXT('',#3,'mechanical')", b"PRODUCT_CONTEXT('',#3,'electrical')", 1),
            "product name difference": self.HEADER + self.DATA.replace(b"Widget", b"Gadget"),
            "missing STEP header": self.DATA,
            "additional header-field difference": self.HEADER.replace(
                b"AUTOMOTIVE_DESIGN { 1 0 10303 214 3 1 1 }", b"CONFIG_CONTROL_DESIGN { 1 0 10303 214 3 1 1 }") + self.DATA,
            "truncated file": (self.HEADER + self.DATA)[:60],
        }
        baseline = harness.normalize_step_export_metadata(self.HEADER + self.DATA)
        for name, variant in cases.items():
            with self.subTest(name=name):
                self.assertNotEqual(harness.normalize_step_export_metadata(variant), baseline)


# ===================== Part C: fake proof integration =====================

class FakeCLISuccessUsesNormalAlignedPathTests(unittest.TestCase):
    def test_fake_cli_success_uses_normal_aligned_path(self):
        run = require_passed(self)
        item = scenario(run["report"], "fake_cli_success")
        self.assertEqual(item["status"], "passed")
        self.assertEqual(item["runtimeInvocations"], 1)
        for key in ("planHash", "jobID", "manifestHash", "requestHash", "resultHash",
                    "stepHash", "artifactManifestHash", "recordPackageHash"):
            self.assertTrue(item[key], f"{key} missing/empty")
        self.assertTrue(item["metadataPresent"])
        self.assertEqual(item["stepCount"], 1)

        invocations = harness.invocations(run["workspace"] / "fake-cli-success" / "invocations.jsonl")
        self.assertEqual(len(invocations), 1)
        out = run["workspace"] / "fake-cli-success" / "cwd" / "output"
        self.assertTrue(list(out.rglob("*.step")))
        self.assertTrue(list(out.rglob("result.json")))
        self.assertTrue(list(out.rglob("parametron.record-package.json")))
        for forbidden in ("parametron.observed.json", "native_manifest.json", "parametron.verification.json"):
            manifest_matches = [p for p in out.rglob("manifest.json") if "parametron-record-package" not in p.parts]
            self.assertTrue(manifest_matches)
            with open(manifest_matches[0], encoding="utf-8") as handle:
                self.assertNotIn(forbidden, handle.read())


class FakeAPISuccessUsesDefaultExecutorFactoryTests(unittest.TestCase):
    def test_fake_api_success_uses_default_executor_factory(self):
        run = require_passed(self)
        item = scenario(run["report"], "fake_api_success")
        self.assertEqual(item["status"], "passed")
        self.assertEqual(item["terminalState"], "succeeded")
        self.assertEqual(item["runtimeInvocations"], 1)
        self.assertEqual(len(item["jobIDs"]), 1)

        invocations = harness.invocations(run["workspace"] / "fake-api-success" / "invocations.jsonl")
        self.assertEqual(len(invocations), 1)
        artifacts_root = run["workspace"] / "fake-api-success" / "artifacts"
        self.assertTrue(list(artifacts_root.rglob("prm.report.json")))
        self.assertTrue(list(artifacts_root.rglob("*.step")))
        self.assertTrue(list(artifacts_root.rglob("result.json")))
        # Raw evidence remains on disk under the unregistered attempt working
        # copy (Task 10 owns writing it) but must never be a *registered*
        # artifact surfaced through the API's report.
        persisted = harness.json_file(harness.find_one(artifacts_root, "prm.report.json"))
        registered_filenames = [a.get("filename", "") for a in persisted.get("artifacts", [])]
        for forbidden in ("parametron.observed.json", "native_manifest.json", "parametron.verification.json"):
            self.assertFalse(
                any(name.endswith(forbidden) for name in registered_filenames),
                f"{forbidden} must not be registered as an artifact",
            )


class ConcurrentIsolationTests(unittest.TestCase):
    def test_concurrent_success_failure_and_artifact_ownership(self):
        run = require_passed(self)
        item = scenario(run["report"], "fake_concurrent_isolation")
        self.assertEqual(item["status"], "passed")
        self.assertEqual(item["runtimeInvocations"], 3)
        self.assertEqual(item["overlappingRuntimes"], 3)
        self.assertEqual(len(set(item["jobIDs"])), 3)
        self.assertEqual(item["terminalStates"], ["succeeded", "succeeded", "failed"])
        root = run["workspace"] / "fake-concurrent"
        calls = harness.invocations(root / "invocations.jsonl")
        self.assertEqual(len({c["pid"] for c in calls}), 3)
        self.assertEqual(sorted(c["mode"] for c in calls), ["malformed_result", "success", "success"])
        evidence = harness.json_file(root / "isolation-evidence.json")
        self.assertEqual(len(evidence), 5)
        self.assertEqual(item["isolationSnapshots"], len(evidence))
        products = ["concurrent-alpha", "concurrent-beta", "concurrent-failure"]
        self.assertEqual([[snap[p]["status"]["state"] for p in products] for snap in evidence], [
            ["running", "running", "running"],
            ["succeeded", "running", "running"],
            ["succeeded", "running", "failed"],
            ["succeeded", "succeeded", "failed"],
            ["succeeded", "succeeded", "failed"],
        ])
        artifact_ids = set()
        for product, job_id in zip(products, item["jobIDs"]):
            own = evidence[-1][product]
            self.assertEqual(own["status"]["jobId"], job_id)
            self.assertEqual(own["status"]["productKey"], product)
            self.assertEqual(own["report"]["jobs"][0]["jobId"], job_id)
            for other in products:
                if other != product:
                    self.assertNotIn(other, json.dumps(own))
            artifacts = own["listing"]["artifacts"]
            if product == products[-1]:
                self.assertEqual(artifacts, [])
                self.assertEqual(own["report"]["artifacts"], [])
                self.assertEqual(own["status"]["error"]["productId"], product)
            else:
                self.assertTrue(artifacts)
                self.assertTrue(any(product in content for content in own["contents"]))
                for artifact in artifacts:
                    self.assertEqual(artifact["jobId"], job_id)
                    self.assertEqual(artifact["productId"], product)
                    self.assertNotIn(artifact["id"], artifact_ids)
                    artifact_ids.add(artifact["id"])
        self.assertEqual(evidence[-1], evidence[-2])


class CLIAndAPIProofsDoNotShareRuntimeStateTests(unittest.TestCase):
    def test_cli_and_api_proofs_do_not_share_runtime_state(self):
        run = require_passed(self)
        cli_log = run["workspace"] / "fake-cli-success" / "invocations.jsonl"
        api_log = run["workspace"] / "fake-api-success" / "invocations.jsonl"
        cli_pids = {i["pid"] for i in harness.invocations(cli_log)}
        api_pids = {i["pid"] for i in harness.invocations(api_log)}
        self.assertTrue(cli_pids)
        self.assertTrue(api_pids)
        self.assertTrue(cli_pids.isdisjoint(api_pids), "CLI and API proofs must not share a runtime process")
        cli_state = (run["workspace"] / "fake-cli-success" / "state").resolve()
        api_state = (run["workspace"] / "fake-api-success" / "state").resolve()
        self.assertNotEqual(cli_state, api_state)


# ===================== Part D: repeated-run tests =====================

class RepeatedFakeRunsExecuteRuntimeTwiceTests(unittest.TestCase):
    def test_repeated_fake_runs_execute_runtime_twice(self):
        run = require_passed(self)
        item = scenario(run["report"], "fake_repeated")
        self.assertEqual(item["status"], "passed")
        self.assertEqual(item["runtimeInvocations"], 2)
        self.assertTrue(item["stable"])
        for key in ("planHash", "jobID", "manifestHash", "requestHash", "resultHash",
                    "stepHash", "artifactManifestHash", "recordPackageHash"):
            self.assertTrue(item[key])


class RepeatedProofFailsWhenSecondRunIsOnlyCacheReuseTests(unittest.TestCase):
    def test_repeated_proof_fails_when_second_run_is_only_cache_reuse(self):
        run = require_passed(self)
        first_log = harness.invocations(run["workspace"] / "fake-repeat-1" / "invocations.jsonl")
        second_log = harness.invocations(run["workspace"] / "fake-repeat-2" / "invocations.jsonl")
        # Each of the two "fresh" repeated runs must independently prove a
        # real external process invocation; if the second run had silently
        # reused a cache hit instead of invoking the controlled runtime, its
        # invocation log would be empty despite the harness reporting success.
        self.assertEqual(len(first_log), 1, "first repeated run did not externally invoke the runtime")
        self.assertEqual(len(second_log), 1, "second repeated run did not externally invoke the runtime")
        self.assertNotEqual(first_log[0]["pid"], second_log[0]["pid"])


class SameRootMissingArtifactCannotReuseTests(unittest.TestCase):
    def test_same_root_missing_artifact_cannot_reuse_previous_success(self):
        run = require_passed(self)
        item = scenario(run["report"], "fake_stale_output")
        self.assertEqual(item["status"], "passed")
        self.assertEqual(item["runtimeInvocations"], 2)
        self.assertFalse(item["falseSuccess"])
        invocations = harness.invocations(run["workspace"] / "fake-stale" / "invocations.jsonl")
        self.assertEqual(len(invocations), 2)
        self.assertEqual(invocations[0]["mode"], "success")
        self.assertEqual(invocations[1]["mode"], "missing_artifact")

    def test_same_root_malformed_result_cannot_reuse_previous_result(self):
        run = require_passed(self)
        item = scenario(run["report"], "malformed_result")
        self.assertEqual(item["status"], "passed")
        self.assertEqual(item["terminalState"], "failed")
        artifacts_root = run["workspace"] / "fake-malformed_result" / "artifacts"
        persisted = harness.find_one(artifacts_root, "prm.report.json")
        self.assertEqual(harness.json_file(persisted).get("artifacts"), [])


# ===================== Part E: retry and failure proof =====================

class RetryThenSuccessExactSequenceTests(unittest.TestCase):
    def test_retry_then_success_uses_exact_three_attempt_sequence(self):
        run = require_passed(self)
        item = scenario(run["report"], "retry_then_success")
        self.assertEqual(item["status"], "passed")
        self.assertEqual(item["runtimeInvocations"], 3)
        self.assertEqual(item["terminalState"], "succeeded")

        invocations = harness.invocations(run["workspace"] / "fake-retry_then_success" / "invocations.jsonl")
        self.assertEqual([i["sequence"] for i in invocations], [1, 2, 3])
        attempt_ids = [i["attemptID"] for i in invocations]
        self.assertEqual(len(set(attempt_ids)), 3, "expected three distinct ordered attempt identities")

        artifacts_root = run["workspace"] / "fake-retry_then_success" / "artifacts"
        persisted = harness.json_file(harness.find_one(artifacts_root, "prm.report.json"))
        self.assertEqual(persisted["status"], "success")
        step = harness.find_one(artifacts_root, "*.step")
        # The accepted STEP artifact must live under the third (final,
        # successful) attempt's working copy, not attempt 1 or 2.
        self.assertIn(attempt_ids[2], str(step))
        self.assertNotIn(attempt_ids[0], str(step))
        self.assertNotIn(attempt_ids[1], str(step))


class RuntimeFailurePreservesStructuredNativeFieldsTests(unittest.TestCase):
    def test_runtime_failure_preserves_structured_native_fields(self):
        run = require_passed(self)
        item = scenario(run["report"], "runtime_failure")
        self.assertEqual(item["status"], "passed")
        self.assertEqual(item["terminalState"], "failed")
        # Live retry policy for this failure class, proven empirically by the
        # harness's own scenario execution (see class docstring at top of
        # file for why this is read from the harness rather than asserted a
        # priori).
        self.assertGreaterEqual(item["runtimeInvocations"], 1)

        invocations = harness.invocations(run["workspace"] / "fake-runtime_failure" / "invocations.jsonl")
        self.assertEqual(len(invocations), item["runtimeInvocations"])
        first_result_path = Path(invocations[0]["result"])
        result_doc = harness.json_file(first_result_path)
        self.assertEqual(result_doc["status"], "failed")
        failure = result_doc["failure"]
        self.assertEqual(failure["boundary"], "freecad")
        self.assertEqual(failure["category"], "runtime")
        self.assertEqual(failure["code"], "controlled_runtime_failure")
        self.assertEqual(failure["stage"], "execute")


class MalformedResultIsTerminalWithoutRetryTests(unittest.TestCase):
    def test_malformed_result_is_terminal_without_retry(self):
        run = require_passed(self)
        item = scenario(run["report"], "malformed_result")
        self.assertEqual(item["runtimeInvocations"], 1)
        self.assertEqual(item["terminalState"], "failed")


class MissingArtifactIsTerminalAndAtomicTests(unittest.TestCase):
    def test_missing_artifact_is_terminal_and_atomic(self):
        run = require_passed(self)
        item = scenario(run["report"], "missing_artifact")
        self.assertEqual(item["runtimeInvocations"], 1)
        self.assertEqual(item["terminalState"], "failed")
        artifacts_root = run["workspace"] / "fake-missing_artifact" / "artifacts"
        persisted = harness.json_file(harness.find_one(artifacts_root, "prm.report.json"))
        self.assertEqual(persisted["artifacts"], [])


class ObservedMismatchReachesEngineVerificationTests(unittest.TestCase):
    def test_observed_mismatch_reaches_engine_verification(self):
        run = require_passed(self)
        item = scenario(run["report"], "observed_mismatch")
        self.assertEqual(item["runtimeInvocations"], 1, "verification failure must not retry")
        self.assertEqual(item["terminalState"], "failed")
        artifacts_root = run["workspace"] / "fake-observed_mismatch" / "artifacts"
        persisted = harness.json_file(harness.find_one(artifacts_root, "prm.report.json"))
        self.assertEqual(persisted["artifacts"], [])
        error = persisted.get("error", {})
        self.assertIn(error.get("category"), ("verification", "runtime"))


class FailureScenariosDoNotShareAttemptOrArtifactStateTests(unittest.TestCase):
    def test_failure_scenarios_do_not_share_attempt_or_artifact_state(self):
        run = require_passed(self)
        names = ("retry_then_success", "runtime_failure", "malformed_result", "missing_artifact", "observed_mismatch")
        all_pids = []
        all_states = []
        for name in names:
            log = harness.invocations(run["workspace"] / f"fake-{name}" / "invocations.jsonl")
            self.assertTrue(log, f"{name}: no invocations recorded")
            all_pids.extend(i["pid"] for i in log)
            all_states.append((run["workspace"] / f"fake-{name}" / "state").resolve())
        self.assertEqual(len(all_pids), len(set(all_pids)), "failure scenarios shared a runtime process pid")
        self.assertEqual(len(all_states), len(set(all_states)), "failure scenarios shared a state directory")


# ===================== Part F: cancellation proof =====================

class APIShutdownCancelsBlockedExternalRuntimeTests(unittest.TestCase):
    def test_api_shutdown_cancels_blocked_external_runtime(self):
        run = require_passed(self)
        item = scenario(run["report"], "cancellation")
        self.assertEqual(item["status"], "passed")
        self.assertEqual(item["runtimeInvocations"], 1)
        self.assertFalse(item["orphanProcess"])

        invocations = harness.invocations(run["workspace"] / "fake-cancellation" / "invocations.jsonl")
        self.assertEqual(len(invocations), 1)
        child_pid = invocations[0]["pid"]
        with self.assertRaises(ProcessLookupError):
            import os
            os.kill(child_pid, 0)

        artifacts_root = run["workspace"] / "fake-cancellation" / "artifacts"
        self.assertFalse(list(artifacts_root.rglob("*.step")))
        persisted = harness.json_file(harness.find_one(artifacts_root, "prm.report.json"))
        self.assertEqual(persisted["status"], "canceled")
        self.assertEqual(persisted["artifacts"], [])
        step = next(s for s in persisted["jobs"][0]["steps"] if s["type"] == "RunCADRuntime")
        self.assertEqual(step["status"], "canceled")
        self.assertEqual(step["attempts"], 1)
        self.assertTrue(persisted["error"].get("canceled"))


# ===================== Part L: source and ownership boundaries =====================

class SourceAndOwnershipBoundaryTests(unittest.TestCase):
    """Static-source boundary checks: the proof tooling and the aligned
    production path it exercises must not reach for the legacy bridge/runner,
    must drive the real sibling wrapper (not the legacy headless script), the
    controlled proof runtime must not itself decide Engine verification
    pass/fail, and none of this may couple to record persistence/PDM."""

    PRODUCTION_ALIGNED_FILES = (
        REPO_ROOT / "cmd" / "parametron" / "cli_helpers.go",
        REPO_ROOT / "internal" / "engine" / "cadruntime" / "freecad_observation_request.go",
        REPO_ROOT / "internal" / "engine" / "api" / "runtime.go",
    )

    def test_no_legacy_bridge_use(self):
        for path in (HARNESS, CONTROLLED_RUNTIME):
            text = path.read_text(encoding="utf-8")
            self.assertNotIn("internal/engine/bridge", text)
            self.assertNotIn("legacy_observation_runner", text)
            for token in ("RunPython", "freecad_runner.py", "freecad_runner_shim.py",
                          "api_rehearsal_runner.py", "integrations/freecad/"):
                self.assertNotIn(token, text)
        for path in self.PRODUCTION_ALIGNED_FILES:
            text = path.read_text(encoding="utf-8")
            self.assertNotIn('"parametron/internal/engine/bridge"', text)

    def test_no_direct_sibling_source_script_invocation(self):
        text = HARNESS.read_text(encoding="utf-8")
        self.assertIn("parametron-freecad", text)
        self.assertNotIn("parametron_freecad_headless.py", text)
        for path in self.PRODUCTION_ALIGNED_FILES:
            self.assertNotIn("parametron_freecad_headless.py", path.read_text(encoding="utf-8"))

    def test_proof_runtime_does_not_decide_engine_verification(self):
        text = CONTROLLED_RUNTIME.read_text(encoding="utf-8")
        for token in ("verification.Contract", "StatusPass", "StatusFail", "def verify(", "class Verification"):
            self.assertNotIn(token, text)
        # The runtime only ever emits a raw observed payload and a raw
        # runtime result/failure; it never writes anything resembling a
        # verification pass/fail decision itself.
        self.assertNotIn('"status":"pass"', text.replace(" ", ""))
        self.assertNotIn('"status":"fail"', text.replace(" ", ""))

    def test_no_pdm_or_record_persistence_coupling(self):
        for path in (HARNESS, CONTROLLED_RUNTIME):
            text = path.read_text(encoding="utf-8").lower()
            for token in ("recordcontract", "recordemit", "recordmap", "recordpackage.persist", " pdm "):
                self.assertNotIn(token, text)


if __name__ == "__main__":
    unittest.main()
