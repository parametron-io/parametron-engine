#!/usr/bin/env python3
"""Permanent Task 14 coverage for the controlled external CAD runtime
protocol (scripts/cad_runtime_proof_runtime.py), invoked as a real
subprocess exactly as the real FreeCAD sibling wrapper is invoked."""

import hashlib
import json
import os
import signal
import subprocess
import sys
import time
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
RUNTIME = REPO_ROOT / "scripts" / "cad_runtime_proof_runtime.py"


def read_json(path):
    return json.loads(Path(path).read_text(encoding="utf-8"))


def read_jsonl(path):
    text = Path(path).read_text(encoding="utf-8")
    return [json.loads(line) for line in text.splitlines() if line]


class Fixture:
    """A single working-copy attempt laid out exactly as Task 7/8 would lay
    one out for the runtime: a prepared source document, a native manifest,
    an Engine verification (observation) request, and a reference traversal
    request, all rooted under the working copy directory."""

    def __init__(self, working, manifest_path, result_path, output_dir, request_path,
                 traversal_request_path, source_path):
        self.working = working
        self.manifest_path = manifest_path
        self.result_path = result_path
        self.output_dir = output_dir
        self.request_path = request_path
        self.traversal_request_path = traversal_request_path
        self.source_path = source_path

    def args(self):
        return [
            "execute",
            "--working-copy", str(self.working),
            "--manifest", str(self.manifest_path),
            "--result", str(self.result_path),
            "--output-dir", str(self.output_dir),
            "--observation-request", str(self.request_path),
            "--reference-traversal-request", str(self.traversal_request_path),
        ]


def build_fixture(root, *, attempt_name="attempt-1", source_bytes=b"prepared-source-bytes", param_value=10):
    working = root / attempt_name
    (working / "source").mkdir(parents=True)
    source_path = working / "source" / "widget.FCStd"
    source_path.write_bytes(source_bytes)

    manifest_path = working / "native_manifest.json"
    manifest_path.write_text(json.dumps({
        "sourceDocument": "source/widget.FCStd",
        "outputs": [{"id": "widget_step", "format": "step", "path": "outputs/widget.step"}],
    }))

    request_path = working / "parametron.verification.json"
    request_path.write_text(json.dumps({
        "expected": {
            "parameters": [{"id": "width", "name": "Width", "value": param_value, "valueKind": "number"}],
            "metadata": [{"id": "working-copy-sha256", "key": "working_copy_sha256", "value": "", "valueKind": "string"}],
            "references": [{"kind": "working_copy_path", "name": str(working)}],
            "components": [],
        },
        "observationContext": {
            "parameters": [{"id": "width", "name": "Width", "groupName": "VarSet"}],
        },
    }))

    traversal_request_path = working / "parametron.reference-traversal-request.json"
    traversal_request_path.write_text(json.dumps({"schemaVersion": "2.0", "externalTargets": []}))

    result_path = working / "result.json"
    output_dir = working / "outputs"
    output_dir.mkdir()

    return Fixture(working, manifest_path, result_path, output_dir, request_path,
                    traversal_request_path, source_path)


def invoke(fixture, *, mode=None, state_dir=None, log_path=None, args=None, extra_env=None, timeout=15):
    env = dict(os.environ)
    if mode is not None:
        env["PARAMETRON_CAD_PROOF_MODE"] = mode
    if state_dir is not None:
        env["PARAMETRON_CAD_PROOF_STATE_DIR"] = str(state_dir)
    if log_path is not None:
        env["PARAMETRON_CAD_PROOF_INVOCATION_LOG"] = str(log_path)
    if extra_env:
        env.update(extra_env)
    cmd = [sys.executable, str(RUNTIME), *(args if args is not None else fixture.args())]
    return subprocess.run(cmd, capture_output=True, text=True, env=env, timeout=timeout)


class ExecuteRequiresCompleteAlignedArgumentsTests(unittest.TestCase):
    """A1: exact CLI protocol."""

    def setUp(self):
        import tempfile
        tmp = tempfile.TemporaryDirectory(prefix="task14-proof-runtime-")
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)

    def test_execute_requires_complete_aligned_arguments(self):
        fixture = build_fixture(self.root)

        with self.subTest("missing execute subcommand entirely"):
            result = invoke(fixture, args=[])
            self.assertNotEqual(result.returncode, 0)

        with self.subTest("unknown subcommand"):
            args = ["bogus-subcommand"] + fixture.args()[1:]
            result = invoke(fixture, args=args)
            self.assertEqual(result.returncode, 2)
            self.assertIn("expected execute subcommand", result.stderr)

        required_flags = ("--working-copy", "--manifest", "--result", "--output-dir",
                          "--observation-request", "--reference-traversal-request")
        for flag in required_flags:
            with self.subTest(f"missing {flag}"):
                args = fixture.args()
                idx = args.index(flag)
                trimmed = args[:idx] + args[idx + 2:]
                result = invoke(fixture, args=trimmed)
                self.assertEqual(result.returncode, 2)

        for flag in required_flags:
            with self.subTest(f"relative {flag}"):
                args = fixture.args()
                idx = args.index(flag)
                relative = os.path.relpath(args[idx + 1], start=str(self.root))
                args = args[:idx + 1] + [relative] + args[idx + 2:]
                result = invoke(fixture, args=args)
                self.assertEqual(result.returncode, 2)
                self.assertIn("absolute", result.stderr)


class ExecuteRejectsPathsOutsideWorkingCopyTests(unittest.TestCase):
    """A2: path containment."""

    def test_execute_rejects_paths_outside_working_copy(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="task14-containment-") as tmp:
            root = Path(tmp)
            fixture = build_fixture(root)
            outside = root / "outside"
            outside.mkdir()

            for flag, replacement in (
                ("--manifest", outside / "native_manifest.json"),
                ("--result", outside / "result.json"),
                ("--output-dir", outside / "outputs"),
                ("--observation-request", outside / "parametron.verification.json"),
                ("--reference-traversal-request", outside / "parametron.reference-traversal-request.json"),
            ):
                with self.subTest(flag=flag):
                    args = fixture.args()
                    idx = args.index(flag)
                    args = args[:idx + 1] + [str(replacement)] + args[idx + 2:]
                    result = invoke(fixture, args=args)
                    self.assertEqual(result.returncode, 2)
                    self.assertIn("escapes working copy", result.stderr)


class SuccessWritesCanonicalRuntimeOutputsTests(unittest.TestCase):
    """A3: success output."""

    def test_success_writes_canonical_runtime_outputs(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="task14-success-") as tmp:
            root = Path(tmp)
            fixture = build_fixture(root)
            state_dir = root / "state"
            log_path = state_dir / "invocations.jsonl"

            result = invoke(fixture, mode="success", state_dir=state_dir, log_path=log_path)
            self.assertEqual(result.returncode, 0, result.stderr)

            invocations = read_jsonl(log_path)
            self.assertEqual(len(invocations), 1)
            inv = invocations[0]
            for key in ("sequence", "attemptID", "workingCopy", "manifest", "result", "outputDirectory", "mode", "pid"):
                self.assertIn(key, inv)
            self.assertEqual(inv["workingCopy"], str(fixture.working))
            self.assertEqual(inv["mode"], "success")

            result_doc = read_json(fixture.result_path)
            self.assertEqual(result_doc["schemaVersion"], "1.0")
            self.assertEqual(result_doc["status"], "succeeded")
            self.assertEqual(
                [a["id"] for a in result_doc["artifacts"]],
                ["widget_step"],
            )

            observed_path = fixture.output_dir / "prm.observed.json"
            observed = read_json(observed_path)
            self.assertEqual(observed["schemaVersion"], "1.0")
            for key in ("path", "sha256"):
                self.assertIn(key, observed["workingCopy"])
            for key in ("parameters", "metadata", "references", "components"):
                self.assertIn(key, observed["observation"])

            artifact_path = fixture.working / "outputs" / "widget.step"
            self.assertTrue(artifact_path.is_file())

            digest = hashlib.sha256(fixture.source_path.read_bytes()).hexdigest()
            self.assertEqual(observed["workingCopy"]["sha256"], digest)
            self.assertEqual(observed["workingCopy"]["path"], str(fixture.working))
            metadata_entry = observed["observation"]["metadata"][0]
            self.assertEqual(metadata_entry["value"], digest)


class SuccessIsDeterministicForEquivalentLogicalInputsTests(unittest.TestCase):
    """A4: repeated success across different absolute attempt roots."""

    def test_success_is_deterministic_for_equivalent_logical_inputs(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="task14-det-a-") as tmp_a, \
             tempfile.TemporaryDirectory(prefix="task14-det-b-") as tmp_b:
            # Same attempt basename (matching the real Task 7 attempt-number
            # naming, which is stable across independent fresh runs), but
            # under two entirely different absolute tmp-root prefixes.
            fixture_a = build_fixture(Path(tmp_a), attempt_name="attempt-1")
            fixture_b = build_fixture(Path(tmp_b), attempt_name="attempt-1")
            self.assertNotEqual(str(fixture_a.working), str(fixture_b.working))

            result_a = invoke(fixture_a, mode="success", state_dir=Path(tmp_a) / "state")
            result_b = invoke(fixture_b, mode="success", state_dir=Path(tmp_b) / "state")
            self.assertEqual(result_a.returncode, 0, result_a.stderr)
            self.assertEqual(result_b.returncode, 0, result_b.stderr)

            doc_a = read_json(fixture_a.result_path)
            doc_b = read_json(fixture_b.result_path)
            self.assertEqual(doc_a, doc_b)

            bytes_a = (fixture_a.working / "outputs" / "widget.step").read_bytes()
            bytes_b = (fixture_b.working / "outputs" / "widget.step").read_bytes()
            self.assertEqual(bytes_a, bytes_b)

            observed_a = read_json(fixture_a.output_dir / "prm.observed.json")
            observed_b = read_json(fixture_b.output_dir / "prm.observed.json")
            observed_a["workingCopy"]["path"] = "<attempt>"
            observed_b["workingCopy"]["path"] = "<attempt>"
            for obs in (observed_a, observed_b):
                for ref in obs["observation"]["references"]:
                    if ref.get("kind") == "working_copy_path":
                        ref["name"] = "<attempt>"
            self.assertEqual(observed_a, observed_b)


class RetryThenSuccessUsesOneProcessPerAttemptTests(unittest.TestCase):
    """A5: retry sequence, driven by three separate subprocess invocations."""

    def test_retry_then_success_uses_one_process_per_attempt(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="task14-retry-") as tmp:
            root = Path(tmp)
            fixture = build_fixture(root)
            state_dir = root / "state"

            first = invoke(fixture, mode="retry_then_success", state_dir=state_dir)
            self.assertEqual(first.returncode, 17)
            self.assertEqual(read_json(fixture.result_path)["failure"]["code"], "controlled_retry_1")

            second = invoke(fixture, mode="retry_then_success", state_dir=state_dir)
            self.assertEqual(second.returncode, 17)
            self.assertEqual(read_json(fixture.result_path)["failure"]["code"], "controlled_retry_2")

            third = invoke(fixture, mode="retry_then_success", state_dir=state_dir)
            self.assertEqual(third.returncode, 0, third.stderr)
            self.assertEqual(read_json(fixture.result_path)["status"], "succeeded")

            invocations = read_jsonl(state_dir / "invocations.jsonl")
            self.assertEqual([i["sequence"] for i in invocations], [1, 2, 3])


class RuntimeFailureWritesStructuredFailedResultTests(unittest.TestCase):
    """A6: runtime-native failure."""

    def test_runtime_failure_writes_structured_failed_result(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="task14-runtime-failure-") as tmp:
            fixture = build_fixture(Path(tmp))
            result = invoke(fixture, mode="runtime_failure", state_dir=Path(tmp) / "state")
            self.assertEqual(result.returncode, 17)

            doc = read_json(fixture.result_path)
            self.assertEqual(doc["status"], "failed")
            failure = doc["failure"]
            self.assertEqual(failure["boundary"], "freecad")
            self.assertEqual(failure["category"], "runtime")
            self.assertEqual(failure["code"], "controlled_runtime_failure")
            self.assertEqual(failure["message"], "intentional controlled runtime failure")
            self.assertEqual(failure["stage"], "execute")


class MalformedResultModeWritesInvalidResultOnlyTests(unittest.TestCase):
    """A7: malformed result mode."""

    def test_malformed_result_mode_writes_invalid_result_only(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="task14-malformed-") as tmp:
            fixture = build_fixture(Path(tmp))
            result = invoke(fixture, mode="malformed_result", state_dir=Path(tmp) / "state")
            self.assertEqual(result.returncode, 0)

            with self.assertRaises(json.JSONDecodeError):
                read_json(fixture.result_path)

            self.assertFalse((fixture.output_dir / "prm.observed.json").exists())
            self.assertFalse((fixture.working / "outputs" / "widget.step").exists())


class MissingArtifactModeDeclaresButOmitsArtifactTests(unittest.TestCase):
    """A8: missing artifact mode."""

    def test_missing_artifact_mode_declares_but_omits_artifact(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="task14-missing-artifact-") as tmp:
            fixture = build_fixture(Path(tmp))
            result = invoke(fixture, mode="missing_artifact", state_dir=Path(tmp) / "state")
            self.assertEqual(result.returncode, 0, result.stderr)

            doc = read_json(fixture.result_path)
            self.assertEqual(doc["status"], "succeeded")
            self.assertEqual([a["id"] for a in doc["artifacts"]], ["widget_step"])
            self.assertFalse((fixture.working / "outputs" / "widget.step").exists())


class ObservedMismatchIsSchemaValidTests(unittest.TestCase):
    """A9: observed mismatch mode."""

    def test_observed_mismatch_is_schema_valid(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="task14-observed-mismatch-") as tmp:
            fixture = build_fixture(Path(tmp), param_value=10)
            result = invoke(fixture, mode="observed_mismatch", state_dir=Path(tmp) / "state")
            self.assertEqual(result.returncode, 0, result.stderr)

            observed = read_json(fixture.output_dir / "prm.observed.json")
            self.assertEqual(observed["schemaVersion"], "1.0")
            for key in ("path", "sha256"):
                self.assertIn(key, observed["workingCopy"])
            for key in ("parameters", "metadata", "references", "components"):
                self.assertIn(key, observed["observation"])
            self.assertEqual(len(observed["observation"]["parameters"]), 1)
            self.assertNotEqual(observed["observation"]["parameters"][0]["value"], 10)


class BlockModeExitsWhenParentTerminatesProcessTests(unittest.TestCase):
    """A10: blocking and termination."""

    def test_block_mode_exits_when_parent_terminates_process(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="task14-block-") as tmp:
            root = Path(tmp)
            fixture = build_fixture(root)
            state_dir = root / "state"
            log_path = state_dir / "invocations.jsonl"

            env = dict(os.environ)
            env["PARAMETRON_CAD_PROOF_MODE"] = "block"
            env["PARAMETRON_CAD_PROOF_STATE_DIR"] = str(state_dir)
            env["PARAMETRON_CAD_PROOF_INVOCATION_LOG"] = str(log_path)
            proc = subprocess.Popen(
                [sys.executable, str(RUNTIME), *fixture.args()],
                env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            )
            try:
                deadline = time.monotonic() + 10
                while time.monotonic() < deadline and not log_path.exists():
                    time.sleep(0.05)
                self.assertTrue(log_path.exists(), "expected the blocked process to record its invocation")
                invocations = read_jsonl(log_path)
                self.assertEqual(len(invocations), 1)
                self.assertEqual(invocations[0]["pid"], proc.pid)
                self.assertIsNone(proc.poll(), "process exited before it was terminated")

                proc.send_signal(signal.SIGTERM)
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=5)
                    self.fail("blocked controlled runtime did not terminate on SIGTERM")

                self.assertIsNotNone(proc.poll())
                self.assertFalse(fixture.result_path.exists())
                self.assertFalse((fixture.output_dir / "prm.observed.json").exists())
            finally:
                if proc.poll() is None:
                    proc.kill()
                    proc.wait(timeout=5)
                proc.stdout.close()
                proc.stderr.close()


if __name__ == "__main__":
    unittest.main()
