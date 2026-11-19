#!/usr/bin/env python3
"""Permanent opt-in coverage for the Engine-owned real-FreeCAD occurrence,
fact, and integrity acceptance proof driven by
scripts/cad_runtime_integration_proof.py --mode evidence-real.

This module is skipped by default because it builds the sibling
parametron-freecad launcher and runs real FreeCAD 1.1.4. To run it for real:

    nix develop --command env PARAMETRON_RUN_FREECAD_INTEGRATION=1 \
      python -m unittest scripts.tests.test_cad_runtime_evidence_proof -v

A skip is only acceptable for ordinary default-suite runs; the explicit
command above must not skip.
"""

import functools
import json
import os
import subprocess
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
    "python -m unittest scripts.tests.test_cad_runtime_evidence_proof -v"
)

SCENARIO_ORDER = [
    "executor_facts_and_integrity",
    "executor_required_unavailable_fact",
    "executor_multi_outcome",
    "executor_occurrence_materialization_blocker",
    "fixture_complete_scope",
    "fixture_mixed_scope",
    "fixture_baseline_mismatch",
]


@functools.lru_cache(maxsize=1)
def single_evidence_run():
    workspace = Path(tempfile.mkdtemp(prefix="parametron-evidence-proof-"))
    report_path = workspace / "proof-report.json"
    result = subprocess.run(
        [
            sys.executable,
            str(HARNESS),
            "--mode",
            "evidence-real",
            "--freecad-repo",
            str(FREECAD_REPO),
            "--workspace",
            str(workspace),
            "--report",
            str(report_path),
            "--keep-workspace",
        ],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        timeout=1800,
    )
    report = None
    if report_path.exists():
        report = json.loads(report_path.read_text(encoding="utf-8"))
    return {
        "workspace": workspace,
        "report_path": report_path,
        "report": report,
        "result": result,
    }


def require_passed(test):
    run = single_evidence_run()
    ok = (
        run["result"].returncode == 0
        and run["report"] is not None
        and run["report"].get("status") == "passed"
        and run["report"].get("realEvidence") is not None
    )
    if not ok:
        test.fail(
            "the shared `--mode evidence-real` harness run did not pass; a "
            "skip is not permitted for this explicit real-integration gate.\n"
            f"returncode={run['result'].returncode}\n"
            f"stdout(tail)={run['result'].stdout[-4000:]}\n"
            f"stderr(tail)={run['result'].stderr[-4000:]}"
        )
    return run


def scenario(evidence, name):
    for item in evidence["scenarios"]:
        if item["name"] == name:
            return item
    raise AssertionError(
        f"scenario {name!r} not present: {[s['name'] for s in evidence['scenarios']]}"
    )


@unittest.skipUnless(
    os.environ.get(REQUIRED_ENV) == "1",
    f"real FreeCAD integration is opt-in; run: {REQUIRED_COMMAND}",
)
class RealFreeCADEvidenceProofTests(unittest.TestCase):
    def test_real_freecad_114_launcher_smoke(self):
        run = require_passed(self)
        evidence = run["report"]["realEvidence"]
        self.assertEqual(evidence["launcher"], "parametron-freecad")
        self.assertEqual(evidence["host"], "freecadcmd")
        self.assertEqual(evidence["freecadVersion"][:3], ["1", "1", "4"])
        self.assertEqual(evidence["goTestExit"], 0)

    def test_all_required_scenarios_are_present_and_passed(self):
        run = require_passed(self)
        evidence = run["report"]["realEvidence"]
        self.assertEqual(
            [item["name"] for item in evidence["scenarios"]], SCENARIO_ORDER
        )
        for item in evidence["scenarios"]:
            self.assertEqual(item["status"], "passed")

    def test_complete_occurrence_scope_rolls_up_to_ten(self):
        run = require_passed(self)
        item = scenario(run["report"]["realEvidence"], "fixture_complete_scope")
        self.assertEqual(item["facts"]["occurrences"], 10)
        self.assertEqual(item["facts"]["quantity"], 10)
        self.assertEqual(item["facts"]["quantityTarget"], "component_a/Body")
        self.assertEqual(item["facts"]["missingStatus"], "missing")
        self.assertIn("observation", item["facts"]["normalizedFamilies"])

    def test_incomplete_and_unsupported_occurrence_scope_is_explicit(self):
        run = require_passed(self)
        item = scenario(run["report"]["realEvidence"], "fixture_mixed_scope")
        self.assertEqual(item["facts"]["completeness"], "incomplete")
        self.assertIsNone(item["facts"]["quantities"])
        self.assertEqual(item["facts"]["acceptedOccurrences"], 4)
        self.assertEqual(
            item["facts"]["unsupported"], ["MarkerLink", "NestedLink", "ArrayLink"]
        )
        self.assertEqual(item["facts"]["unresolved"], ["MissingTarget"])
        self.assertEqual(item["facts"]["auxiliary"], ["Joints"])

    def test_executor_occurrence_materialization_blocker_is_deterministic(self):
        run = require_passed(self)
        item = scenario(
            run["report"]["realEvidence"], "executor_occurrence_materialization_blocker"
        )
        self.assertEqual(item["facts"]["completeness"], "incomplete")
        self.assertEqual(item["facts"]["acceptedOccurrences"], 0)
        self.assertEqual(item["facts"]["unresolvedMembers"], 10)
        blocker = run["report"]["realEvidence"]["crossRepositoryBlocker"]
        self.assertEqual(blocker["boundary"], "freecad")
        self.assertIn("relative path", blocker["summary"])

    def test_required_unavailable_fact_fails_at_fact_evidence(self):
        run = require_passed(self)
        item = scenario(
            run["report"]["realEvidence"], "executor_required_unavailable_fact"
        )
        self.assertEqual(item["facts"]["stage"], "fact_evidence")
        self.assertEqual(item["facts"]["boundary"], "execution_entrypoint")
        self.assertEqual(item["facts"]["code"], "runtime_failure")
        self.assertIn("failure", item["facts"]["recordFamilies"])
        self.assertNotIn("observation", item["facts"]["recordFamilies"])

    def test_multi_outcome_keeps_independent_evidence(self):
        run = require_passed(self)
        item = scenario(run["report"]["realEvidence"], "executor_multi_outcome")
        self.assertEqual(item["facts"]["outcomes"], 2)
        self.assertTrue(item["facts"]["distinctRawEvidence"])
        self.assertEqual(item["facts"]["outcomeScopedRaw"], 2)
        self.assertEqual(item["facts"]["observationRecords"], 2)

    def test_baseline_mismatch_is_rejected_before_mutation(self):
        run = require_passed(self)
        item = scenario(run["report"]["realEvidence"], "fixture_baseline_mismatch")
        self.assertEqual(item["facts"]["stage"], "integrity_baseline")
        self.assertTrue(item["facts"]["sourceUnchanged"])
        self.assertTrue(item["facts"]["evidenceBlocked"])

    def test_fact_evidence_preserves_all_scalar_and_explicit_states(self):
        run = require_passed(self)
        item = scenario(run["report"]["realEvidence"], "executor_facts_and_integrity")
        self.assertEqual(item["facts"]["factCount"], 10)
        self.assertEqual(item["facts"]["observedCount"], 7)
        self.assertEqual(item["facts"]["unavailableCount"], 1)
        self.assertEqual(item["facts"]["unsupportedCount"], 2)
        self.assertEqual(item["facts"]["integrityResources"], 2)
        self.assertRegex(item["facts"]["persistedResultHash"], r"^[0-9a-f]{64}$")
        self.assertIn(
            "MeasuredLength:observed:number:12.5:mm",
            item["facts"]["normalizedFactOrder"],
        )
        self.assertIn(
            "SecondaryLength:observed:number:20:mm",
            item["facts"]["normalizedFactOrder"],
        )
        self.assertIn(
            "SpecialText:unsupported:::", item["facts"]["normalizedFactOrder"]
        )
        self.assertIn(
            "MissingProperty:unavailable:::", item["facts"]["normalizedFactOrder"]
        )


if __name__ == "__main__":
    unittest.main()
