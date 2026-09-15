# Public Test Matrix

This matrix summarizes the automated test coverage for Parametron Engine across
its core architectural layers.

## Core Feature Matrix

| Area | Coverage | Primary Validation Command | Notes |
|:-----|:---------|:---------------------------|:------|
| **Authoring / DSL** | Syntax parsing, Pratt parselet model, operator precedence, string interpolation, escape sequences, diagnostics | `go test ./internal/authoring/dsl/...` | Exercises `testdata/dsl/smoke/` and break tests |
| **Semantic Validation** | Strict type system, identifier resolution, enum literals, profile settings, mutation validation | `go test ./internal/authoring/dsl/...` | Rejects ambiguous and untyped constructs |
| **IR & Planning** | AST-to-IR conversion, IR-to-plan evaluation, parameter resolution, topological ordering, roundtrip codegen | `go test ./internal/authoring/ir/... ./internal/authoring/planner/...` | Validates execution plan determinism |
| **DSL Regression Gate** | Full language feature gate combining arithmetic, math built-ins, conditionals, and exports | `go run ./cmd/parametron --file testdata/dsl/stress.dsl --print-plan` | Validates AST, IR, and plan generation without external dependencies |
| **Project & Resource Mapping** | `parametron.project.json` mapping, model/table resource discovery, fingerprint derivation | `go test ./internal/shared/projectmap/... ./internal/engine/projectinput/...` | Verifies project-mode resource binding |
| **Table Resources** | Strict JSON table parsing, type validation, canonical serialization, SHA-256 fingerprinting, row lookup | `go test ./internal/engine/table/... ./internal/engine/tableloader/...` | Validates exact numeric lexeme preservation and caching |
| **Job & Handoff Packaging** | Multi-product plan splitting, deterministic job IDs, handoff package validation and reconstruction | `go test ./internal/engine/job/... ./internal/engine/handoff/...` | Preserves step order and product keys |
| **Scheduler & Worker Pool** | Concurrency control, worker pool management, product-order result collection, error propagation | `go test ./internal/engine/scheduler/...` | Guarantees deterministic result ordering under concurrency |
| **Executor & Lifecycle** | Step dispatch, retry state machine, timeout enforcement, context cancellation, error wrapping | `go test ./internal/engine/executor/...` | Tests isolated attempt execution and retry limits |
| **Layered Cache** | Geometry skip, content-addressed artifact reuse, metadata tracking, table fingerprint integration | `go test ./internal/engine/cache/...` | Concurrency tested with `-race` |
| **CAD Runtime Attempt Management** | Attempt ID derivation, isolated workspace creation (`_working/<attemptID>/`), atomic source staging | `go test ./internal/engine/cadruntime/...` | Verifies complete filesystem isolation across retries |
| **Manifest & Request Materialization** | Native manifest projection, verification request derivation (`parametron.verification.json`), reference traversal request | `go test ./internal/engine/cadruntime/... ./internal/engine/adapter/freecad/...` | Deterministic request JSON materialization |
| **External Runtime Invocation** | Executable resolution, capability execution, argument construction, process stdout/stderr capture | `go test ./internal/engine/runtimecap/...` | Direct process invocation without shell wrapping |
| **Raw Result & Evidence Intake** | `result.json` decoding, declared artifact containment and format verification, observed working copy validation | `go test ./internal/engine/cadruntime/...` | Enforces structural validity and attempt correlation |
| **Engine-Owned Verification** | In-memory verification comparison across parameters, metadata, references, and components | `go test ./internal/engine/verification/...` | Evaluates deterministic failure taxonomy |
| **Outcome Consumption** | Attempt correlation, verification outcome mapping, structured CAD runtime consumption failure | `go test ./internal/engine/executor/...` | Bridges verified runs to executor state |
| **Artifact Store & Serving** | In-memory and filesystem artifact registry, deterministic SHA-256 artifact IDs, batch acceptance, HTTP serving | `go test ./internal/engine/artifact/...` | Dual-class (`execution_output`, `verified_artifact`) handling |
| **Execution Reporting** | `prm.report.json` construction, step timing, structured error serialization, deterministic formatting | `go test ./internal/engine/report/...` | Atomic report writing and skipped step visibility |
| **Run Metadata** | `prm.metadata.json` generation, toolchain info, product summaries, table input provenance | `go test ./internal/engine/metadata/...` | Deterministic key sorting and fingerprint recording |
| **Normalized Record Contracts** | Record schemas for execution, artifact, observation, reference, failure, and verification families | `go test ./internal/engine/recordcontract/...` | Schema versioning and provenance models |
| **Operational Record Mapping** | Mapping operational files (`prm.report.json`, `prm.metadata.json`, `result.json`, observed state, traversals) to normalized records | `go test ./internal/engine/recordmap/...` | Pure, deterministic mapping functions |
| **Record Package Emission** | Local record package writer, package manifest indexing, raw evidence file preservation under `raw/` | `go test ./internal/engine/recordpackage/... ./internal/engine/recordemit/...` | Stable package key `engine-run:<planHash>` |
| **HTTP API & Endpoints** | `POST /job`, `GET /job/{id}`, `GET /job/{id}/artifacts`, `GET /artifacts`, `GET /artifacts/{id}`, `GET /healthz` | `go test ./internal/engine/api/...` | Validates transport limits, idempotency, and status |
| **CLI Commands** | Harness commands: `validate`, `simulate`, `sweep`, `snapshot`, `diff`, `sync`, output directory strategies | `go test ./cmd/parametron/...` | Validates CLI routing and exit codes |

## Integration and End-to-End Proof

| Boundary | Coverage | Validation Command | Notes |
|:---------|:---------|:-------------------|:------|
| **Controlled Simulation Proof** | End-to-end multi-attempt retry, timeout handling, verification failure, cancellation, and API execution | `python scripts/cad_runtime_integration_proof.py --mode fake` | Uses deterministic mock CAD runtime; no external FreeCAD required |
| **Opt-in FreeCAD Integration** | Live CAD document mutation, recompute, STEP export, and Engine verification using `parametron-freecad` | `PARAMETRON_RUN_FREECAD_INTEGRATION=1 python scripts/cad_runtime_integration_proof.py --mode real --freecad-repo ../parametron-freecad` | Opt-in; requires external FreeCAD and wrapper binary |
| **Opt-in FreeCAD Adapter Unit Tests** | FreeCAD adapter smoke tests against local `freecadcmd` binary | `PARAMETRON_RUN_FREECAD_INTEGRATION=1 PARAMETRON_FREECAD_BIN=/path/to/freecadcmd go test ./internal/engine/adapter/freecad/...` | Opt-in smoke validation |

## Comprehensive Test Execution

```bash
# Run all unit and package tests
go test ./...

# Run static analysis
go vet ./...

# Run race detector across concurrent subsystems
go test -race ./internal/engine/cache/... ./internal/engine/api/... ./internal/engine/scheduler/...
```
