# Testing Strategy

Parametron Engine uses a layered, contract-driven testing strategy to guarantee
deterministic behavior across authoring, planning, execution, verification, and
record packaging.

## Core Testing Philosophy

- **Deterministic identity**: Equivalent inputs must yield byte-identical plans,
  hashes, canonical JSON, and normalized records.
- **Contract boundary isolation**: Package boundaries (such as verification,
  record mapping, and runtime capability) are tested with unit suites using typed
  models rather than live subprocesses.
- **Fail-closed verification**: Inconsistent states, contradictory evidence, or
  malformed outputs fail deterministically with typed error classifications.
- **Controlled simulation**: Complex process lifecycles (retries, timeouts,
  process failures, and signal handling) are validated using deterministic fake
  runtimes without external CAD dependencies.
- **Opt-in live integration**: Real FreeCAD tests are opt-in and gated behind
  environment flags to keep default testing fast and dependency-free.

## Architectural Test Layers

```text
+-------------------------------------------------------------+
| CLI & HTTP API Integration                                  |
| (Harness routing, HTTP lifecycle, concurrency, -race)       |
+-------------------------------------------------------------+
| Engine Execution & Orchestration                            |
| (Job splitting, handoff packages, scheduler, executor)      |
+-------------------------------------------------------------+
| Verification & Normalized Records                           |
| (Contract derivation, observed comparison, record mapping)  |
+-------------------------------------------------------------+
| CAD Runtime & Adapter Contracts                             |
| (Attempt isolation, manifest projection, evidence intake)   |
+-------------------------------------------------------------+
| Authoring & Planning Core                                   |
| (Lexer, Pratt parser, AST validation, IR, plan generation)  |
+-------------------------------------------------------------+
```

### 1. Authoring and Planning

- **DSL Lexer & Parser (`internal/authoring/dsl`)**: Validates syntax, Pratt
  parselet dispatch, operator precedence, string interpolation, and diagnostic
  reporting.
- **Semantic Validation (`internal/authoring/dsl`)**: Enforces strict type
  checking, identifier resolution, enum literal constraints, and profile setting
  rules.
- **Intermediate Representation & Planning (`internal/authoring/ir`, `internal/authoring/planner`)**:
  Validates AST-to-IR conversion, IR-to-plan generation, dependency resolution,
  topological ordering, and roundtrip codegen.
- **DSL Regression Gate (`testdata/dsl/stress.dsl`)**: Comprehensive regression
  suite exercising language features, math functions, conditionals, and manifest
  generation.

### 2. CAD Runtime and Adapter Contracts

- **Attempt Management (`internal/engine/cadruntime`)**: Validates attempt
  identity derivation, isolated layout creation (`_working/<attemptID>/`),
  atomic source model preparation, and pre-invocation output cleaning.
- **Manifest & Request Materialization (`internal/engine/cadruntime`, `internal/engine/verification`, `internal/engine/adapter/freecad`)**:
  Ensures pure, deterministic projection of execution manifests, verification
  requests (`prm.verification.json`) including optional request-scoped target-state
  observation contracts (`suppression`, `visibility`, `existence`) derived from
  canonical mutation intent, and reference traversal requests
  (`prm.reference-traversal-request.json`). Validates deterministic canonical
  serialization (`destination` then `object`) and schema 1.0 preservation.
- **Evidence Intake Validation (`internal/engine/cadruntime`, `internal/engine/observed`)**:
  Validates raw `prm.result.json` loading, strict artifact containment checks,
  `prm.observed.json` working copy fingerprint correlation, strict target-state
  evidence parsing and canonical serialization preserving distinct boolean evidence states (`observed`, `target_missing`, `unavailable`) and existence evidence states (`exists`, `absent`, `unavailable`) independently from mutation intent, and `prm.reference-traversal.json` intake.

### 3. Verification and Normalized Records

- **Engine Verification (`internal/engine/verification`)**: Tests in-memory
  comparison of verification contracts against observed CAD state across
  parameters, metadata, references, and components. Validates failure class
  taxonomy. Raw target-state contract and derivation tests are separated from
  later expected-versus-observed semantic verification.
- **Record Contracts (`internal/engine/recordcontract`)**: Validates schema
  versioning, provenance structures, and identity derivation across execution,
  artifact, observation, reference, failure, and verification record families.
- **Record Mapping (`internal/engine/recordmap`)**: Tests mapping from operational
  files (`prm.report.json`, `prm.metadata.json`, legacy `result.json` payloads,
  observed state, traversals) to normalized record structures. This legacy
  result-mapper coverage is distinct from the active runtime transport filename.
- **Record Package Writer & Emission (`internal/engine/recordpackage`, `internal/engine/recordemit`)**:
  Verifies deterministic package generation, manifest indexing, and raw evidence
  preservation under `raw/`. CAD evidence tests read authoritative attempt
  paths, preserve source bytes, reject run-root guessing, and avoid arbitrary
  selection when multiple outcomes are eligible.

### 4. Scheduler, Executor, and Cache

- **Job & Handoff Packaging (`internal/engine/job`, `internal/engine/handoff`)**:
  Tests multi-product plan splitting, deterministic job IDs, and handoff package
  reconstruction.
- **Scheduler & Executor (`internal/engine/scheduler`, `internal/engine/executor`)**:
  Validates worker pool concurrency, step execution dispatch, retry state
  machines, timeout enforcement, and context cancellation.
- **Layered Cache (`internal/engine/cache`)**: Tests geometry, artifact, and
  metadata cache key derivation, table fingerprint sensitivity, and cache hit/skip
  logic.

### 5. HTTP API and CLI Harness

- **HTTP API (`internal/engine/api`, `cmd/parametron-engine`)**: Tests `POST /job`
  canonicalization, idempotency, submission store persistence, job lifecycle
  polling (`GET /job/{id}`), and artifact endpoints.
- **CLI Commands (`cmd/parametron`)**: Validates `validate`, `simulate`, `sweep`,
  `snapshot`, and `diff` subcommands using mock and fake execution layers.

### 6. Controlled Simulation and Integration Proof

- **Fake Runtime Proof (`scripts/cad_runtime_integration_proof.py --mode fake`)**:
  Executes end-to-end multi-attempt retry sequences, timeout handling, and
  verification failure scenarios using a deterministic mock CAD runtime.
- **Opt-in Real FreeCAD Integration (`scripts/cad_runtime_integration_proof.py --mode real`)**:
  Executes live CAD mutation, recompute, export, and verification against
  `parametron-freecad` and FreeCAD binaries.

## Canonical Verification Commands

```bash
# Run all unit and package tests
go test ./...

# Run static analysis
go vet ./...

# Run targeted package tests
go test ./internal/authoring/dsl/...
go test ./internal/engine/verification/...
go test ./internal/engine/observed/...
go test ./internal/engine/recordmap/...

# Run with race detector
go test -race ./internal/engine/cache/... ./internal/engine/api/...
```

## Related Documents

- [Local Development](local-development.md) — Build setup, environment configuration, and proof scripts.
- [Test Matrix](../test-matrix.md) — Public test coverage matrix by package and feature area.
- [Extension Guide](extension-guide.md) — Testing guidance when adding features or adapters.
