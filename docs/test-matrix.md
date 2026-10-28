# Public Test Matrix

This matrix summarizes the automated test coverage for Parametron Engine across
its core architectural layers.

## Core Feature Matrix

| Area | Coverage | Primary Validation Command | Notes |
|:-----|:---------|:---------------------------|:------|
| **Authoring / DSL** | Syntax parsing, Pratt parselet model, operator precedence, string interpolation, escape sequences, diagnostics | `go test ./internal/authoring/dsl/...` | Exercises `testdata/dsl/smoke/` and break tests |
| **Semantic Validation** | Strict type system, identifier resolution, enum literals, profile settings, mutation validation | `go test ./internal/authoring/dsl/...` | Rejects ambiguous and untyped constructs |
| **IR & Planning** | AST-to-IR conversion, IR-to-plan evaluation, param and let binding override resolution, inferred number/string/boolean/enum let type validation, mixed binding dependency propagation, topological ordering, roundtrip codegen, target-mutation planning | `go test ./internal/authoring/ir/... ./internal/authoring/planner/...` | Covers inferred enum-domain rejection, unqualified cross-product overrides, deterministic override-name diagnostics, and target-action selection from overridden lets; repeated equivalent overridden plans preserve serialization and hashes, changed resolved intent changes identity, and unused internal overrides preserve identity; CSV values, planner manifest exported values, and CAD parameter assignments remain param-only |
| **Capture-backed Semantic Targets** | Feature and Component native-reference retention through build, clone, and canonicalization; internal `NativeRef` exclusion from semantic-model JSON; exact case-sensitive semantic lookup | `go test ./internal/engine/semantic/...` | Distinct semantic Name, stable ID, DisplayName, and native selector; native references, IDs, display labels, and case variants are not semantic lookup aliases; absent or empty capture mappings acquire no fallback |
| **Native Target Resolution & Projection** | Shared native resolution in both target-operation projection paths; Feature, part Component, and assembly Component routing; suppression, unsuppression, hide, unhide, and deletion | `go test ./internal/engine/semanticmap/...` | Preserves Component linkage and independent Part/Assembly destinations; repeated failures for missing, empty, whitespace-only, surrounding-whitespace, and NUL-containing mappings; accepted selectors preserved exactly; no semantic Name, DisplayName, or semantic-ID fallback; same-name fixtures supply explicit mappings |
| **Native Target Plan Identity** | Semantic target lowering, native runtime objects in planner/export mutations, projected-object ordering, and resolved-intent identity | `go test ./internal/authoring/planner/...` | Equivalent mappings preserve plan JSON and hash; used native mapping changes alter runtime Object, serialized intent, and hash; DisplayName-only and unused mapping changes preserve identity; suppression, visibility, and deletion order by projected objects; `keep` equals omission without a native mapping; Part/Assembly destinations remain independent |
| **DSL Regression Gate** | Full language feature gate combining arithmetic, math built-ins, conditionals, and exports | `go run ./cmd/parametron --file testdata/dsl/stress.dsl --print-plan` | Validates AST, IR, and plan generation without external dependencies |
| **Project & Resource Mapping** | Existing `prm.project.json` model/table mapping, resource capture, fingerprint derivation, duplicate JSON declaration rejection, and deterministic validation ordering | `go test ./internal/shared/projectmap/... ./internal/engine/projectinput/...` | Canonicalizes project-source coordinates; validates all declared model sources, including unused resources; rejects missing/nonregular sources and symlink escape, accepts contained source symlinks; source failures retain logical ID and coordinate under `resourcebinding.ErrInvalid` |
| **Declared Resource Identity** | Project-scoped, case-sensitive model logical IDs; deterministic finite inventory; explicit selection; aliases; standalone, unused, and zero-reference resources; defensive copies | `go test ./internal/engine/resourcebinding/... ./internal/engine/projectinput/...` | Reuses project declarations without a second registry. Relocated project roots preserve logical bindings and canonical project-source coordinates separately from operational absolute paths. Only model bindings are accepted for CAD inventory; unsupported kinds, duplicate/conflicting identities, invalid coordinates, destination collisions/ancestor conflicts, and ambiguous coordinate lookup are rejected deterministically |
| **Table Resources** | Strict JSON table parsing, type validation, canonical serialization, SHA-256 fingerprinting, row lookup | `go test ./internal/engine/table/... ./internal/engine/tableloader/...` | Validates exact numeric lexeme preservation and caching |
| **Job & Handoff Packaging** | Multi-product plan splitting, deterministic job IDs, handoff package validation and reconstruction | `go test ./internal/engine/job/... ./internal/engine/handoff/...` | Preserves step order, product keys, and canonical schema 1.0 mutation payloads with stable job IDs |
| **Scheduler & Worker Pool** | Concurrency control, worker pool management, product-order result collection, error propagation | `go test ./internal/engine/scheduler/...` | Guarantees deterministic result ordering under concurrency |
| **Executor & Lifecycle** | Step dispatch, retry state machine, timeout enforcement, context cancellation, error wrapping | `go test ./internal/engine/executor/...` | Tests isolated attempt execution and retry limits |
| **Layered Cache** | Geometry skip, content-addressed artifact reuse, metadata tracking, table fingerprint integration | `go test ./internal/engine/cache/...` | Concurrency tested with `-race` |
| **CAD Runtime Attempt Management** | Attempt ID derivation, isolated workspace creation (`_working/<attemptID>/`), atomic source staging, and verification-boundary result propagation | `go test ./internal/engine/cadruntime/...` | Verifies complete filesystem isolation across retries and, with a fake external capability, preserves target-state verification failures as Engine verification failures rather than runtime failures; no real FreeCAD target-state execution is exercised |
| **Declared Resource Materialization** | Bounded multi-model staging; selected primary at `source/<basename>`; distinct additional-resource coordinates; same-basename and alias separation; canonical relative bindings across relocation and retries | `go test ./internal/engine/adapter/freecad/... ./internal/engine/cadruntime/...` | Preserves original sources and confines staging to isolated attempt workspaces. Preflight rejects missing/nonregular/escaping sources, destination symlinks and unsafe ancestors, source hardlinks, destination type conflicts, and reserved-file/output conflicts before primary staging or runtime invocation in tested cases. Tests assert `source_preparation` attempt failures and `request_materialization`/`output_preparation` orchestration classification; mismatched selected source fails request validation. Atomic staging is per file, not an inventory-wide transaction; no protection against concurrent adversarial filesystem mutation is claimed |
| **Manifest & Request Materialization** | Canonical schema 1.0 `prm.export-manifest.json`, `prm.verification.json`, and `prm.reference-traversal-request.json`; optional target mutations and mapped external traversal targets; Engine-side FreeCAD projection of param-only assignments after internal let overrides | `go test ./internal/engine/cadruntime/... ./internal/engine/verification/... ./internal/engine/adapter/freecad/...` | Deterministic request JSON; exact target identities, canonical relative paths, conflict rejection, and stable traversal-target ordering; traversal mappings remain available under schema 1.0; overridden lets create no synthetic runtime parameter assignments; capture-backed native objects propagate into the runtime manifest, observation request, expected verification state, and controlled returned evidence with exact `(destination, object)` correlation; projection tests do not execute native FreeCAD |
| **External Runtime Invocation** | Executable resolution, capability execution, argument construction, process stdout/stderr capture | `go test ./internal/engine/runtimecap/...` | Direct process invocation without shell wrapping |
| **Raw Result & Evidence Intake** | `prm.result.json` decoding, declared artifact containment and format verification, `prm.observed.json` working copy validation and raw target-state evidence intake (`suppression`, `visibility`, `existence`), and `prm.reference-traversal.json` intake | `go test ./internal/engine/cadruntime/... ./internal/engine/observed/...` | Enforces structural validity, attempt correlation, and deterministic canonical ordering; preserves explicit boolean evidence (`observed true/false`, `target_missing`, `unavailable`) and existence evidence (`exists`, `absent`, `unavailable`) distinct from mutation intent; absent when unrequested |
| **Engine-Owned Verification** | In-memory verification comparison across parameters, metadata, references, components, and target state (`suppression`, `visibility`, `existence`) | `go test ./internal/engine/verification/...` | Derives expected target state from canonical mutation intent and compares actual evidence by exact `(destination, object)` identity; covers matches, `target_state_mismatch`, `target_missing`, `native_evidence_unavailable`, `required_observation_missing`, invalid evidence before semantic comparison, deterministic first-failure ordering, and independence of requested mutation values from observations |
| **Outcome Consumption** | Attempt correlation, verification outcome mapping, terminal failed CAD outcome handling, and generic semantic classification of adapter-native failures | `go test ./internal/engine/executor/...` | Bridges verified runs to executor state; keeps runtime-native failure distinct from malformed or missing evidence and from Engine verification failure, and proves terminal attempt authority across retries |
| **Artifact Store & Serving** | In-memory and filesystem artifact registry, deterministic SHA-256 artifact IDs, batch acceptance, HTTP serving | `go test ./internal/engine/artifact/...` | Dual-class (`execution_output`, `verified_artifact`) handling |
| **Execution Reporting** | `prm.report.json` construction, step timing, structured error serialization, deterministic formatting | `go test ./internal/engine/report/...` | Atomic report writing and skipped step visibility |
| **Run Metadata** | `prm.metadata.json` generation, toolchain info, product summaries, table input provenance | `go test ./internal/engine/metadata/...` | Deterministic key sorting and fingerprint recording |
| **Normalized Record Contracts** | Record schemas for execution, artifact, observation, reference, failure, and verification families | `go test ./internal/engine/recordcontract/...` | Covers schema versioning and provenance models, including the `target_state` observation kind, `target_state` verification category, and target-state verification failure vocabulary within the existing record families. Reference contract tests cover declared resource/native endpoint identity, canonical relationship keys, nullable property/mechanism discrimination, strict qualification/resolution/completeness validation, copy safety, and the unchanged shared record identity algorithm |
| **Operational Record Mapping** | Deterministic mapping of reports, metadata, artifacts, typed observations, traversals, Engine verification results, and generic CAD runtime failure outcomes to normalized records | `go test ./internal/engine/recordmap/...` | Covers target-state material within the existing observation and verification families and adapter-neutral `MapCADRuntimeFailure` mapping after runtime-specific interpretation; verifies stable identity, semantic class/stage mapping, Engine operational context, and exact-evidence provenance without treating raw bytes as semantic truth. Reference traversal tests separately prove cross-resource joins, selected-source correlation and aliases, same-name native entities, independent property-only/mechanism-only distinctions, null versus observed values, exact duplicate canonicalization, deterministic conflict rejection before projection, all five native resolution states, unsupported/undeclared targets, and scope-bearing empty traversals |
| **Record Package Emission** | Normal-run emission, local package writing and manifest indexing, plural identity-addressed artifact records, and preservation of exact attempt evidence at `raw/runtime/prm.result.json`, `raw/verification/prm.verification.json`, `raw/observed/prm.observed.json`, and `raw/runtime/prm.reference-traversal.json` | `go test ./internal/engine/recordpackage/... ./internal/engine/recordemit/... ./cmd/parametron/...` | Proves zero/one/many artifact records at `records/artifacts/<identityId>/prm.artifact-record.json`, identity ordering and duplicate rejection; observation, verification, and target-state emission from typed Engine material; runtime-native failure precedence with report-derived fallback and one failure record; deterministic repeated output; source-byte preservation and exact-byte SHA-256 provenance; and deterministic fallback when terminal failed-outcome correlation is absent or ambiguous. Traversal emission tests validate complete zero-edge records, exact raw traversal bytes and SHA-256 provenance, strict JSON/discriminator rejection, and coexistence with artifact, observation, and verification records; normal CLI candidate tests preserve zero/one/many eligibility behavior |
| **HTTP API & Endpoints** | `POST /job`, `GET /job/{id}`, `GET /job/{id}/artifacts`, `GET /artifacts`, `GET /artifacts/{id}`, `GET /healthz` | `go test ./internal/engine/api/...` | Validates transport limits, idempotency, and status |
| **CLI Commands** | Harness commands: `validate`, `simulate`, `sweep`, `snapshot`, `diff`, `sync`, output directory strategies, repeated `--set` collection | `go test ./cmd/parametron/...` | Validates CLI routing, exit codes, and canonical schema 1.0 manifest payloads in `--json-plan`; repeated `--set` values for the same binding use last-value-wins collection |

The reusable capture fixture
[`internal/engine/semantic/testdata/native-target/prm.cad.json`](../internal/engine/semantic/testdata/native-target/prm.cad.json)
deliberately separates Feature Name `mounting_bracket`, stable ID
`fea.mountingBracket`, NativeRef `Body01`, and DisplayName `Mounting Bracket`.
Permanent tests in `internal/engine/semantic/native_target_test.go`,
`internal/engine/semanticmap/target_native_resolution_test.go`,
`internal/authoring/planner/planner_native_target_test.go`,
`internal/engine/cadruntime/freecad_native_target_test.go`, and
`cmd/parametron/native_target_contract_test.go` reuse it to prove semantic
selection, native routing, and downstream correlation. These are Engine-owned
proofs with controlled evidence; they do not validate native FreeCAD execution.

Declared-resource coverage is anchored in
`internal/shared/projectmap/declared_resources_test.go`,
`internal/engine/resourcebinding/resourcebinding_test.go`,
`internal/engine/projectinput/declared_resources_test.go`,
`internal/engine/adapter/freecad/resource_materialization_test.go`, and
`internal/engine/cadruntime/declared_resources_test.go`.
Relocation independence applies to the declared-resource join, not globally to
planner, job, cache, attempt, or normalized record identities. Project-source
coordinates and attempt-relative materialized coordinates remain separate;
an empty materialized coordinate records no completed staging. Inventory
membership requires no native reference edge and does not automatically emit
normalized reference records. Existing project-lock addressing is retained;
these tests do not establish digest-integrity enforcement.

Reference guarantees are anchored in these permanent tests:

- `internal/engine/recordcontract/reference_record_test.go` retains generic record
  normalization, identity, validation, and copy-safety coverage.
  `reference_structure_test.go` adds
  `TestNativeReferenceRelationshipKeyContract`,
  `TestNativeReferenceRecordRejectsInvalidContract`, and
  `TestNativeReferenceRecordSerializationCopyAndSharedIdentity` for native keys,
  null/observed discriminators, qualification, summary validation, and shared
  evidence-sensitive record identity.
- `internal/engine/recordmap/reference_traversal_test.go` covers native kind and
  five-state resolution matrices, linkage/provenance reconciliation, discriminator
  preservation, conflicting states, and complete zero-edge records.
  `reference_structure_test.go` adds `TestReferenceStructureResourceJoinsAndRelocation`,
  `TestReferenceStructureDiscriminatorsIndependently`,
  `TestReferenceStructureOptionAIdentity`,
  `TestReferenceStructureConflictsBeforeProjection`,
  `TestReferenceStructureInvalidEvidence`,
  `TestReferenceStructureEmptyScopeAndUnsupportedTargets`,
  `TestReferenceStructureUnsupportedScopeRemainsExplicit`, and
  `TestReferenceStructureRejectsAmbiguousSourceCoordinate`. These prove resource
  joins and source selection/aliases, same-name entities in different resources,
  order-independent duplicates/conflicts, explicit fallback coordinates without
  fabricated resource IDs, and bounded completeness. Serialization checks reject
  fabricated occurrence, quantity, cardinality, and BOM fields or semantic roles.
- `internal/engine/recordemit/emit_test.go` proves validated reference emission,
  complete empty records, exact raw evidence and digest provenance, and malformed
  discriminator rejection. `normalized_runtime_test.go` proves reference emission
  alongside the existing artifact, observation, and verification families.
- `cmd/parametron/cli_test.go` retains repeated-equivalent-run reference proof
  with identical evidence and zero/exactly-one/multiple eligible candidate tests.
  `cmd/parametron/reference_structure_test.go` supplies
  `TestContractPackage_ResourceQualifiedReferencesOptionA` and
  `TestContractPackage_EmptyReferenceCompleteness`: controlled normal CLI proof
  of cross-resource normalization and all three empty-scope completeness states.
  Raw bytes and SHA-256 provenance are preserved; equivalent relationship keys
  and ordering do not require identical containing record identities or bytes.

These Engine tests use controlled traversal evidence, not real FreeCAD execution.
The finite declared inventory need not have a native edge for each member.
Completeness covers supported traversal and resource joins, not recursive
closure or assembly occurrence coverage. Automatic external mapping population,
general multi-outcome aggregation, occurrence identity, and quantity/BOM inference
remain outside this proof.

## Integration and End-to-End Proof

| Boundary | Coverage | Validation Command | Notes |
|:---------|:---------|:-------------------|:------|
| **Controlled Simulation Proof** | End-to-end multi-attempt retry, timeout handling, verification failure, cancellation, and API execution | `python scripts/cad_runtime_integration_proof.py --mode fake` | Uses deterministic mock CAD runtime; no external FreeCAD required |
| **Declared Resource Execution Propagation** | Project input → CLI → scheduler → executor → runtime orchestration → attempt → outcome; product-specific selections, complete inventory, context snapshots/defensive copies, multiple products, and retries | `go test ./cmd/parametron/... -run 'TestContractPackage_.*Declared'; go test ./internal/engine/scheduler/... -run TestSchedulerResourceContextsSnapshotAndProductIsolation` | `cmd/parametron/declared_resources_test.go` uses the controlled external contract-package participant; `internal/engine/scheduler/resource_context_test.go` checks scheduler/executor propagation independently. Planned/computed inventories remain unmaterialized; prepared outcomes carry completed coordinates, including on runtime failure after preparation. Existing serialized handoff/runtime request semantics and single-source manifests are preserved. Declared-resource staging alone proves no native opening, recursive discovery, link rebinding, or resource-qualified reference normalization. Separate reference mapping and controlled CLI tests prove Engine resource-qualified normalization |
| **Engine-owned FreeCAD Runtime Contract Package** | Permanent normal CLI proof from project and DSL target actions through planning, scheduling, executor handoff, isolated attempt preparation, the actual external `runtimecap` process boundary, evidence intake, Engine verification, outcome consumption, and normal record-package emission | `go test ./cmd/parametron/... -run 'TestContractPackage_'` | Uses a repository-controlled external runtime participant, not real FreeCAD. The separated native-target proof covers `suppress` and `hide`, carrying `Body01` through runtime requests, returned controlled evidence, and normalized package facts. Covers ordinary execution; `suppress`, `unsuppress`, `hide`, `unhide`, and `delete`; Part/Assembly destinations; combined mutation families; schema `1.0` content across the manifest, verification request, traversal request, result, observed output, and returned traversal evidence; deterministic ordering and request materialization; staged-source and attempt-path containment; observation independence from requested intent; suppression, visibility, and deletion mismatches; omitted required evidence; unavailable native evidence; missing targets; malformed evidence rejection before semantic verification; and runtime-native failure separation. Eligible traversal evidence produces a normalized reference record with exact raw-byte provenance. Successful runs emit applicable execution, artifact, observation, and verification records while preserving raw result, verification-request, and observed evidence separately. Equivalent runs with identical complete contract inputs, including reference evidence digests, prove stable plan, job, and attempt identities, canonical runtime manifests, equivalent-location observation requests, semantic verification results, normalized records, and package entry ordering and identities; runtime-produced operational evidence is not claimed byte-identical. This proof covers the normal CLI path and does not cover HTTP API target-mutation submission or real FreeCAD-native mutation and observation. The companion `reference_structure_test.go` proves complete inventory forwarding and selected-source correlation, cross-resource same-name objects, independent property/mechanism distinctions, stable relationship keys/order under relocation and reordered raw evidence, evidence-sensitive record identity (Option A), exact bytes/digests, and complete/incomplete/unavailable zero-edge records on an overall successful run. Failed traversal aggregate status does not itself imply failed Engine execution. |
| **Opt-in FreeCAD Integration** | Live CAD document mutation, recompute, STEP export, Engine verification, and repeated execution using `parametron-freecad` | `PARAMETRON_RUN_FREECAD_INTEGRATION=1 python scripts/cad_runtime_integration_proof.py --mode real --freecad-repo ../parametron-freecad` | Requires the sibling FreeCAD runtime to accept the canonical schema 1.0 traversal request and emit rich schema 1.0 traversal evidence. The completed real-native rehearsal for `parametron-freecad#9` additionally proved suppress/unsuppress, hide/unhide, safe deletion, unsafe-delete rejection, representative post-mutation native validity failure, target-state observation, schema-1 reference traversal, Engine verification and normal record/package return, raw-evidence provenance, and repeatability against FreeCAD 1.1.1. |
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
