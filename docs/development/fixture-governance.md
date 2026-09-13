# DSL Fixture Governance

This document defines the governance model for DSL fixtures in the `testdata/dsl/` directory.
All DSL files under `testdata/dsl/` are part of the enforced test surface and are subject to the rules described here.

---

## Overview

The `testdata/dsl/` directory contains DSL fixtures that are actively exercised by automated tests.
Each fixture belongs to exactly one category based on its role in the test pipeline.
Fixtures not covered by any test are classified as orphans and must not remain in active fixture directories.

---

## Fixture Categories

### `contract_fixture`

DSL files enforced by automated tests as part of the language contract.

- Tests cover the full parse → validate → plan pipeline.
- Includes both Smoke fixtures (`testdata/dsl/smoke/`) and Break fixtures (`testdata/dsl/break/`).
- Smoke fixtures must parse, validate, and plan without error.
- Break fixtures must fail at a specific pipeline stage with a specific error classification.

### `rehearsal_fixture`

DSL files used to validate project-mode execution and Project Resource Mapping.

- Tests cover project entrypoint loading, model mapping, table mapping, path safety enforcement, and error propagation.
- Includes all fixtures under `testdata/projects/freecad/` (both smoke and break scenarios).
- Exercised via CLI harness tests, not the DSL corpus runner.

### `stress_fixture`

DSL files used as broad regression coverage across multiple DSL features in a single execution.

- Must parse, validate, and plan cleanly on every run.
- Includes `testdata/dsl/stress.dsl`.
- Explicitly wired into the regression gate.

### `orphan` / `delete_candidate`

DSL files present on disk but not tracked or exercised by any test.

- Orphan files must not reside in active fixture directories (`testdata/dsl/smoke/`, `testdata/dsl/break/`, `testdata/projects/freecad/`, or as `testdata/dsl/stress.dsl`).
- Historical or experimental DSL files placed in these directories without test coverage are immediate deletion candidates.

---

## Enforcement Model

### Smoke Fixtures

- `testdata/dsl/smoke/` is globbed by `TestStabilizationAudit_SmokeAndStressParseValidatePlan`.
- Every `.dsl` file in the directory is executed through the full parse → validate → plan pipeline.
- All files must succeed without error.
- Adding a file to `testdata/dsl/smoke/` automatically subjects it to this test.

### Break Fixtures

- `testdata/dsl/break/` is driven by `TestBreakMatrix` in `break_matrix_test.go`.
- Each Break fixture must have a corresponding entry in `testdata/dsl/break/_expectations.json`.
- The entry declares the expected `stage` (`parse`, `validate`, `plan`) and `class` for the failure.
- Files with `"expect": "pass"` in `_expectations.json` are robustness probes — they must succeed, not fail.
- Stage and error classification must match the declared expectations exactly.

### Project Fixtures

- `testdata/projects/freecad/` fixtures are exercised by `TestValidateCommand_ProjectFixtures` and related tests in `cmd/parametron/harness_cmd_test.go`.
- Tests cover project entrypoint loading in both directory form (`--file <dir>`) and direct-file form (`--file <dir>/parametron.project.json`).
- Smoke scenarios must result in a successful `validate` execution.
- Break scenarios must fail at the declared boundary (project mapping validation, resource resolution, CLI validate).
- Project fixtures are not registered in `_expectations.json`; they are not part of the DSL break matrix.

### Stress Fixture

- `testdata/dsl/stress.dsl` is executed as part of `TestStabilizationAudit_SmokeAndStressParseValidatePlan`.
- It must parse, validate, and plan cleanly.
- Coverage spans constants, enums, arithmetic, boolean logic, ternary expressions, string operations, profiles, and planner output.

---

## Tracking Sources

### `docs/test-matrix.md`

Human-readable mapping of fixtures to features and coverage gaps.

- Organizes coverage by feature area (Parser, Core DSL Semantics, String System, File Pattern System, Numeric Stdlib, Profile Semantics, IR/Planner, Table Lookup, Adapter-Related Planning).
- Records which Smoke files, Break files, and `stress.dsl` cover each behavior.
- Identifies known gaps and manual-only validation areas.
- Must be updated when a new fixture is added or a new feature is introduced.

### `testdata/dsl/break/_expectations.json`

Machine-enforced registry for Break fixtures.

- Authoritative source for expected failure `stage` and `class` for each Break file.
- Read directly by `TestBreakMatrix` at test execution time.
- Adding a Break fixture without a corresponding entry in this file causes a test failure.

### Test Files

| File | Role |
|:-----|:-----|
| `internal/authoring/dsl/stabilization_audit_test.go` | Enforces Smoke and stress fixture correctness |
| `cmd/parametron/break_matrix_test.go` | Enforces Break fixture stage and classification |
| `cmd/parametron/harness_cmd_test.go` | Enforces project fixture CLI behavior |

---

## Rules for Adding Fixtures

- A new DSL file **must** belong to exactly one fixture category before it is placed in `testdata/dsl/`.
- Placing a file under `testdata/dsl/smoke/` automatically subjects it to Smoke enforcement; no additional wiring is required.
- Placing a file under `testdata/dsl/break/` **requires** a corresponding entry in `testdata/dsl/break/_expectations.json` declaring `stage` and `class`.
- Adding a project fixture under `testdata/projects/freecad/smoke/` or `testdata/projects/freecad/break/` **requires** a corresponding test case in `cmd/parametron/harness_cmd_test.go`.
- Modifying or replacing `testdata/dsl/stress.dsl` **requires** verifying that the file remains explicitly wired into `TestStabilizationAudit_SmokeAndStressParseValidatePlan`.
- Files not covered by any test **must not** be placed in any active fixture directory.
- `docs/test-matrix.md` **must** be updated to reflect any new fixture and its feature coverage.

---

## Directory Structure Semantics

| Path | Semantics |
|:-----|:----------|
| `testdata/dsl/smoke/` | Valid DSL fixtures. Every file is executed through parse → validate → plan. All must succeed. |
| `testdata/dsl/break/` | Invalid or adversarial DSL fixtures. Every file must fail at the stage and with the class declared in `_expectations.json`. Files with `"expect": "pass"` are robustness probes. |
| `testdata/projects/freecad/` | Project-mode fixtures. Subdirectories `smoke/` and `break/` contain self-contained project directories, each with a `parametron.project.json` and associated resources. Exercised by CLI harness tests. |
| `testdata/dsl/stress.dsl` | Single broad regression fixture. Covers multiple DSL feature areas in one valid file. Must pass the full pipeline on every run. |

---

## Determinism Guarantees

- All `contract_fixture` DSL files are part of deterministic test coverage.
- Identical inputs must produce identical outcomes across runs: either success or a classified, reproducible failure.
- Break fixtures enforce stable error stage and classification. Any deviation from the declared `stage` or `class` in `_expectations.json` is a test failure.
- Project fixtures enforce deterministic project resolution behavior: the same project directory and DSL inputs must always produce the same success or failure boundary.
- Robustness probes (Break files with `"expect": "pass"`) must succeed deterministically without parser crash, stack overflow, or planner timeout.

---

## Non-Goals

- `testdata/dsl/` is not a loose collection of illustrative DSL examples.
- Every DSL file placed in `testdata/dsl/` is part of the enforced test surface and is subject to automated test coverage requirements.
- Historical DSL files, experimental syntax explorations, and demonstration files must not be placed in `testdata/dsl/smoke/`, `testdata/dsl/break/`, `testdata/projects/freecad/`, or used as `testdata/dsl/stress.dsl`.
- Fixtures from one category must not be mixed into another category's directory.
- The DSL break matrix (`_expectations.json` and `TestBreakMatrix`) covers DSL-level failure classification only. It does not cover project file loading, CLI entrypoint behavior, or resource resolution — those are the responsibility of the project fixture corpus.
