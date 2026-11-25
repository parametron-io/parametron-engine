# Public Test Matrix

This matrix summarizes the automated test coverage for Parametron Engine across
its core architectural layers.

## Core Feature Matrix

| Area | Coverage | Primary Validation Command | Notes |
|:-----|:---------|:---------------------------|:------|
| **Authoring / DSL** | Syntax parsing, Pratt parselet model, operator precedence, string interpolation, escape sequences, target-action and target-metadata declarations, optional metadata settings-block normalization, diagnostics | `go test ./internal/authoring/dsl/...` | Exercises `testdata/dsl/smoke/` and break tests. Target declarations accept one optional action plus distinct metadata keys; omitted and empty settings blocks normalize identically, while non-empty metadata settings fail deterministically |
| **Semantic Validation** | Strict type system, identifier resolution, enum literals, profile settings, mutation validation, scalar target-metadata expression validation | `go test ./internal/authoring/dsl/...` | Rejects ambiguous and untyped constructs. Target metadata expressions must resolve to number, string, or boolean without a captured model; captured-type equality remains a planning responsibility |
| **IR & Planning** | AST-to-IR conversion, IR-to-plan evaluation, param and let binding override resolution, inferred number/string/boolean/enum let type validation, mixed binding dependency propagation, topological ordering, roundtrip codegen, target-mutation planning | `go test ./internal/authoring/ir/... ./internal/authoring/planner/...` | Covers inferred enum-domain rejection, unqualified cross-product overrides, deterministic override-name diagnostics, and target-action selection from overridden lets; repeated equivalent overridden plans preserve serialization and hashes, changed resolved intent changes identity, and unused internal overrides preserve identity; CSV values, planner manifest exported values, and CAD parameter assignments remain param-only |
| **Capture-backed Semantic Targets** | Feature and Component native-reference retention through build, clone, and canonicalization; internal `NativeRef` exclusion from semantic-model JSON; exact case-sensitive semantic lookup | `go test ./internal/engine/semantic/...` | Distinct semantic Name, stable ID, DisplayName, and native selector; native references, IDs, display labels, and case variants are not semantic lookup aliases; absent or empty capture mappings acquire no fallback |
| **CAD Capture Contract** | `prm.cad.json` schema-1.0 entity registries including the required `entities.drawings` registry; drawing-level `componentIds` and `resource`; ordered native-bound `sheets`; global ID uniqueness across all seven registries and nested sheet IDs; embedded/separate resource topology and project-relative path rules; deterministic canonical ordering | `go test ./internal/engine/cad/...` | Schema remains `1.0`; zero drawings serialize as `"drawings":[]`; drawing/sheet names, orders, component references, and sheet `native_ref` bindings are validated; drawings sort by `id`, `componentIds` lexicographically, and `sheets` by `order` then `id`. `internal/engine/cad/testdata/smoke/with-drawings` plus inline smoke and break tests cover missing/null drawings, duplicate IDs/names/orders, unresolved components, invalid native linkage, and resource path rejection. Drawings are capture relationships only, not DSL targets, planner inputs, or reference-record edges |
| **Target-Scoped Metadata Resolution** | Exact semantic target plus exact captured metadata ownership; writable and observable capability validation; captured `valueType` plus native-type-to-value-kind mapping (`string`, `boolean`, `number`, `integer`); native owner/property resolution; resolved-value integrality | `go test ./internal/engine/semantic/... ./internal/engine/semanticmap/...` | No native-name, display-label, case, or whitespace fallback. `nativeValueKinds` is additive under semantic-map schema 1.0 and is required when resolving metadata through a metadata-bearing mapping. Relationship-owned metadata remains out of scope. No metadata creation. |
| **Target-Scoped Metadata Planning** | DSL target metadata lowered into native runtime metadata mutations after expression evaluation and resolution; component/Feature routing to Part or Assembly buckets; `let`/`param`/constant/`table_cell`/ternary values; type, kind, writability, observability, and native-identity rejection; deterministic ordering and plan identity | `go test ./internal/authoring/planner/...` | Mutations carry only `object`, `property`, `value`, and `valueKind`; semantic IDs and display labels never enter the runtime manifest. Equivalent authoring order preserves plan JSON and hash; value, key, and target changes alter identity while display-label changes do not. The retired implicit `param` → `set_property` path emits no metadata mutation, and numeric parameter assignments and target actions remain valid alongside metadata. |
| **FreeCAD Runtime Metadata Manifest** | Projection of resolved native metadata mutations into the schema-1.0 `assemblyMutations.metadata` / `partMutations.metadata` families; exact `object`, `property`, `value`, `valueKind` entries; pre-write validation | `go test ./internal/engine/adapter/freecad/...` | Semantic target names, metadata keys, display labels, and policy never reach the runtime manifest. Metadata-only sections are emitted and empty families omitted; the adapter preserves planner order and rejects empty, whitespace-surrounded, or NUL selectors, unsupported or mismatched value kinds, non-finite or non-integral numbers, and duplicate native object/property pairs before write. No real FreeCAD execution is exercised. |
| **Target-Scoped Metadata Verification** | Authored metadata expectations in `prm.verification.json`; typed value and value-kind correlation; observed metadata comparison by semantic ID plus native owner/property identity; deterministic conflict rejection | `go test ./internal/authoring/planner/... ./internal/engine/verification/... ./internal/engine/cadruntime/...` | Planner intent carries the semantic metadata ID separately from the exact native owner/property identity. Verification covers pass, missing observation, value mismatch, value-kind mismatch, owner/key mismatch, duplicate semantic and native expectations, and repeated deterministic failures. The working-copy digest expectation and existing verification behavior remain compatible under schema `1.0`. |
| **Normalized Observed Metadata Records** | `observed.Metadata` normalization into observation facts; semantic metadata ID versus native owner/property identity; typed value preservation; canonical ordering; observation record and record-package emission | `go test ./internal/engine/recordcontract/... ./internal/engine/recordmap/... ./internal/engine/recordemit/... ./internal/engine/recordpackage/...` | Metadata facts keep `subject.id` (semantic metadata ID), `subject.ownerId` (native owner), `subject.name`/`key` (native property), and typed `value`/`kind`. Owner-tied ordering and input-order permutations remain deterministic. Raw `prm.observed.json` stays byte-exact separate evidence; the normalized record is not a second metadata store. Record schema remains `1.0`. |
| **Target-Scoped Metadata End-to-End Rehearsal** | Captured metadata from DSL authoring through planning, the native metadata runtime manifest, controlled runtime observed evidence, Engine expected-versus-observed verification, and normalized record-package emission; compatibility with parameters and target actions; authoring-order determinism | `go test ./cmd/parametron/ -run 'TestContractPackage_.*Metadata'` | Proves semantic target names (`Bracket`, `Assembly`, `Chamfer`) differ from native owners (`Body01`, `Assembly01`, `Chamfer01`); all four value kinds; omitted and empty metadata settings blocks; suppress/hide/delete coexistence; numeric parameter assignment verification; and schema `1.0` across the manifest, verification request, observed evidence, and records. No real FreeCAD native execution. |
| **Native Target Resolution & Projection** | Shared native resolution in both target-operation projection paths; Feature, part Component, and assembly Component routing; suppression, unsuppression, hide, unhide, and deletion | `go test ./internal/engine/semanticmap/...` | Preserves Component linkage and independent Part/Assembly destinations; repeated failures for missing, empty, whitespace-only, surrounding-whitespace, and NUL-containing mappings; accepted selectors preserved exactly; no semantic Name, DisplayName, or semantic-ID fallback; same-name fixtures supply explicit mappings |
| **Native Target Plan Identity** | Semantic target lowering, native runtime objects in planner/export mutations, projected-object ordering, and resolved-intent identity | `go test ./internal/authoring/planner/...` | Equivalent mappings preserve plan JSON and hash; used native mapping changes alter runtime Object, serialized intent, and hash; DisplayName-only and unused mapping changes preserve identity; suppression, visibility, and deletion order by projected objects; `keep` equals omission without a native mapping; Part/Assembly destinations remain independent |
| **DSL Regression Gate** | Full language feature gate combining arithmetic, math built-ins, conditionals, and exports | `go run ./cmd/parametron --file testdata/dsl/stress.dsl --print-plan` | Validates AST, IR, and plan generation without external dependencies |
| **Project & Resource Mapping** | Existing `prm.project.json` model/table mapping, resource capture, fingerprint derivation, duplicate JSON declaration rejection, and deterministic validation ordering | `go test ./internal/shared/projectmap/... ./internal/engine/projectinput/...` | Canonicalizes project-source coordinates; validates all declared model sources, including unused resources; rejects missing/nonregular sources and symlink escape, accepts contained source symlinks; source failures retain logical ID and coordinate under `resourcebinding.ErrInvalid`. Captured model signatures supply the path-free per-resource source identity consumed by project-lock enforcement, and lock digests are validated as canonical SHA-256; see the [source lock and configured-result provenance coverage](#source-lock-and-configured-result-provenance-contract-coverage) |
| **Declared Resource Identity** | Project-scoped, case-sensitive model logical IDs; deterministic finite inventory; explicit selection; aliases; standalone, unused, and zero-reference resources; defensive copies | `go test ./internal/engine/resourcebinding/... ./internal/engine/projectinput/...` | Reuses project declarations without a second registry. Relocated project roots preserve logical bindings and canonical project-source coordinates separately from operational absolute paths. Only model bindings are accepted for CAD inventory; unsupported kinds, duplicate/conflicting identities, invalid coordinates, destination collisions/ancestor conflicts, and ambiguous coordinate lookup are rejected deterministically. Bindings carry optional path-free source digests, validated as canonical SHA-256, that feed materialized baseline verification and lock enforcement |
| **Table Resources** | Strict JSON table parsing, type validation, canonical serialization, SHA-256 fingerprinting, row lookup | `go test ./internal/engine/table/... ./internal/engine/tableloader/...` | Validates exact numeric lexeme preservation and caching |
| **Job & Handoff Packaging** | Multi-product plan splitting, deterministic job IDs, handoff package validation and reconstruction | `go test ./internal/engine/job/... ./internal/engine/handoff/...` | Preserves step order, product keys, and canonical schema 1.0 mutation payloads with stable job IDs |
| **Scheduler & Worker Pool** | Concurrency control, worker pool management, product-order result collection, error propagation | `go test ./internal/engine/scheduler/...` | Guarantees deterministic result ordering under concurrency |
| **Executor & Lifecycle** | Step dispatch, retry state machine, timeout enforcement, context cancellation, error wrapping | `go test ./internal/engine/executor/...` | Tests isolated attempt execution and retry limits |
| **Layered Cache** | Geometry skip, content-addressed artifact reuse, metadata tracking, table fingerprint integration | `go test ./internal/engine/cache/...` | Concurrency tested with `-race` |
| **CAD Runtime Attempt Management** | Attempt ID derivation, isolated workspace creation (`_working/<attemptID>/`), atomic source staging, and verification-boundary result propagation | `go test ./internal/engine/cadruntime/...` | Verifies complete filesystem isolation across retries and, with a fake external capability, preserves target-state verification failures as Engine verification failures rather than runtime failures; no real FreeCAD target-state execution is exercised. Attempts publish the Engine-authored integrity request at `<runRoot>/integrity/<attemptID>/` when the orchestrator supplies an integrity root; see the [source lock and configured-result provenance coverage](#source-lock-and-configured-result-provenance-contract-coverage) |
| **Declared Resource Materialization** | Bounded multi-model staging; selected primary at `source/<basename>`; distinct additional-resource coordinates; same-basename and alias separation; canonical relative bindings across relocation and retries | `go test ./internal/engine/adapter/freecad/... ./internal/engine/cadruntime/...` | Preserves original sources and confines staging to isolated attempt workspaces. Preflight rejects missing/nonregular/escaping sources, destination symlinks and unsafe ancestors, source hardlinks, destination type conflicts, and reserved-file/output conflicts before primary staging or runtime invocation in tested cases. Tests assert `source_preparation` attempt failures and `request_materialization`/`output_preparation` orchestration classification; mismatched selected source fails request validation. Atomic staging is per file, not an inventory-wide transaction; no protection against concurrent adversarial filesystem mutation is claimed. Materialized baselines are independently re-hashed and must byte-match the accepted source identity before runtime invocation; see the [source lock and configured-result provenance coverage](#source-lock-and-configured-result-provenance-contract-coverage) |
| **Source Lock & Configured-Result Provenance** | Project source/input lock enforcement at the `source_lock` dispatch stage, path-free per-resource source identity, byte-preserving materialized baseline verification, runtime baseline evidence validation, and independent persisted-result validation | `go test ./internal/engine/integrity/...` plus the [dedicated coverage](#source-lock-and-configured-result-provenance-contract-coverage) below | Locked digests are validated as canonical SHA-256; unlocked workflows record `not_performed` without a verification claim; integrity failures carry class/stage `integrity` with stable codes; result digests never update the source lock; no real FreeCAD integrity execution is exercised |
| **Manifest & Request Materialization** | Canonical schema 1.0 `prm.export-manifest.json`, `prm.verification.json`, and `prm.reference-traversal-request.json`; optional target mutations and mapped external traversal targets; Engine-side FreeCAD projection of param-only assignments after internal let overrides | `go test ./internal/engine/cadruntime/... ./internal/engine/verification/... ./internal/engine/adapter/freecad/...` | Deterministic request JSON; exact target identities, canonical relative paths, conflict rejection, and stable traversal-target ordering; traversal mappings remain available under schema 1.0; overridden lets create no synthetic runtime parameter assignments; capture-backed native objects propagate into the runtime manifest, observation request, expected verification state, and controlled returned evidence with exact `(destination, object)` correlation; projection tests do not execute native FreeCAD |
| **External Runtime Invocation** | Executable resolution, capability execution, argument construction, process stdout/stderr capture | `go test ./internal/engine/runtimecap/...` | Direct process invocation without shell wrapping. The integrity request location is transported only through the Engine-owned `PARAMETRON_RUNTIME_INTEGRITY_REQUEST` environment variable; an inherited value is stripped when Engine publishes no request, and argv is unchanged |
| **Raw Result & Evidence Intake** | `prm.result.json` decoding, declared artifact containment and format verification, `prm.observed.json` working copy validation and raw target-state evidence intake (`suppression`, `visibility`, `existence`), `prm.reference-traversal.json` intake, and `prm.integrity-evidence.json` runtime baseline and persisted-result evidence intake | `go test ./internal/engine/cadruntime/... ./internal/engine/observed/...` | Enforces structural validity, attempt correlation, and deterministic canonical ordering; preserves explicit boolean evidence (`observed true/false`, `target_missing`, `unavailable`) and existence evidence (`exists`, `absent`, `unavailable`) distinct from mutation intent; absent when unrequested. Integrity evidence is validated strictly against the Engine-authored integrity request: malformed digests, duplicate or unordered resources, unrequested coordinates, and non-observed primary resources are rejected |
| **Engine-Owned Verification** | In-memory verification comparison across parameters, metadata, references, components, and target state (`suppression`, `visibility`, `existence`) | `go test ./internal/engine/verification/...` | Derives expected target state from canonical mutation intent and compares actual evidence by exact `(destination, object)` identity; covers matches, `target_state_mismatch`, `target_missing`, `native_evidence_unavailable`, `required_observation_missing`, invalid evidence before semantic comparison, deterministic first-failure ordering, and independence of requested mutation values from observations |
| **Outcome Consumption** | Attempt correlation, verification outcome mapping, terminal failed CAD outcome handling, and generic semantic classification of adapter-native failures | `go test ./internal/engine/executor/...` | Bridges verified runs to executor state; keeps runtime-native failure distinct from malformed or missing evidence and from Engine verification failure, and proves terminal attempt authority across retries |
| **Artifact Store & Serving** | In-memory and filesystem artifact registry, deterministic SHA-256 artifact IDs, batch acceptance, HTTP serving | `go test ./internal/engine/artifact/...` | Dual-class (`execution_output`, `verified_artifact`) handling |
| **Execution Reporting** | `prm.report.json` construction, step timing, structured error serialization, deterministic formatting | `go test ./internal/engine/report/...` | Atomic report writing and skipped step visibility |
| **Run Metadata** | `prm.metadata.json` generation, toolchain info, product summaries, table input provenance | `go test ./internal/engine/metadata/...` | Deterministic key sorting and fingerprint recording |
| **Normalized Record Contracts** | Record schemas for execution, artifact, observation, reference, failure, and verification families | `go test ./internal/engine/recordcontract/...` | Covers schema versioning and provenance models, including the `target_state` observation kind, the bounded `fact` observation kind contract (see [fact coverage](#bounded-engineering-fact-observation-contract-coverage)), `target_state` verification category, and target-state verification failure vocabulary within the existing record families. Reference contract tests cover declared resource/native endpoint identity, canonical relationship keys, nullable property/mechanism discrimination, strict qualification/resolution/completeness validation, copy safety, and the unchanged shared record identity algorithm. Fact and occurrence evidence references accept the canonical path or a safe outcome-scoped raw path without weakening validation. Integrity provenance tests prove separate source, baseline, runtime-baseline, and persisted-result identity fields, ordered and joined by declared logical resource identity (see [source lock and configured-result provenance coverage](#source-lock-and-configured-result-provenance-contract-coverage)) |
| **Operational Record Mapping** | Deterministic mapping of reports, metadata, artifacts, typed observations, traversals, Engine verification results, and generic CAD runtime failure outcomes to normalized records | `go test ./internal/engine/recordmap/...` | Covers target-state material within the existing observation and verification families and adapter-neutral `MapCADRuntimeFailure` mapping after runtime-specific interpretation; verifies stable identity, semantic class/stage mapping, Engine operational context, and exact-evidence provenance without treating raw bytes as semantic truth. Reference traversal tests separately prove cross-resource joins, selected-source correlation and aliases, same-name native entities, independent property-only/mechanism-only distinctions, null versus observed values, exact duplicate canonicalization, deterministic conflict rejection before projection, all five native resolution states, unsupported/undeclared targets, and scope-bearing empty traversals. Mapping inputs accept optional raw-evidence path overrides so one outcome's normalized evidence retains its outcome-scoped package path while single-outcome defaults stay canonical. Integrity tests prove deterministic per-resource provenance mapping with separate source, baseline, runtime-baseline, and result identities, and integrity failures classified distinctly as failure class/stage `integrity` with their specific codes preserved |
| **Record Package Emission** | Normal-run emission, local package writing and manifest indexing, plural identity-addressed artifact records, outcome-scoped engineering-observation records, and preservation of exact attempt evidence at canonical paths or `raw/outcomes/<outcomeKey>/` when multiple eligible outcomes exist | `go test ./internal/engine/recordpackage/... ./internal/engine/recordemit/... ./cmd/parametron/...` | Proves zero/one/many artifact records at `records/artifacts/<identityId>/prm.artifact-record.json`, identity ordering and duplicate rejection; observation, verification, and target-state emission from typed Engine material; runtime-native failure precedence with report-derived fallback and one failure record; deterministic repeated output; source-byte preservation and exact-byte SHA-256 provenance; and deterministic fallback when terminal failed-outcome correlation is absent or ambiguous. Traversal emission tests validate complete zero-edge records, exact raw traversal bytes and SHA-256 provenance, strict JSON/discriminator rejection, and coexistence with artifact, observation, and verification records. Multi-outcome tests prove plural observation, reference, and verification records at `records/outcomes/<outcomeKey>/`, plural raw evidence under `raw/outcomes/<outcomeKey>/`, no arbitrary outcome selection, per-outcome configured-result provenance, stable ordering across outcome input order, and rejection of ineligible or duplicate outcome identities. Integrity emission tests preserve raw runtime integrity evidence verbatim with SHA-256 provenance, keep normalized provenance unchanged for runs without integrity material, and emit byte-deterministic records for integrity-bearing runs |
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
normalized reference records. Existing project-lock addressing is retained, and
project-lock digest-integrity enforcement is covered by the dedicated
[source lock and configured-result provenance coverage](#source-lock-and-configured-result-provenance-contract-coverage)
below.

Reference guarantees are anchored in these permanent tests:

- `internal/engine/recordcontract/reference_record_test.go` retains generic record
  normalization, identity, validation, and copy-safety coverage.
  `TestReferenceRecordIdentityExcludesOperationalEndpointMaterials` proves that
  generic endpoint paths and runtime-reported digests do not participate in
  record identity while remaining present in the normalized record.
  `reference_structure_test.go` adds
  `TestNativeReferenceRelationshipKeyContract`,
  `TestNativeReferenceRecordRejectsInvalidContract`,
  `TestNativeReferenceRecordIdentityRetainsContractRelativeCoordinate`, and
  `TestNativeReferenceRecordSerializationCopyAndSharedIdentity` for native keys,
  null/observed discriminators, qualification, summary validation, retained
  contract-relative identity coordinates, and shared evidence-sensitive record
  identity.
- `internal/engine/recordmap/observed_test.go` proves generic observed reference
  semantics through
  `TestMapObservedReferenceIdentityIgnoresOperationalWorkingCopyMaterial`,
  `TestMapObservedReferenceIdentityIncludesReferenceMaterial`, and
  `TestMapObservedReferenceRecordRejectsDuplicateReferences`: working-copy
  relocation and digest changes preserve reference identity, endpoint material is
  retained, semantic reference changes still alter identity, and duplicate
  references remain rejected.
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
  with identical evidence and all eligible verified outcomes retained as distinct
  groups.
  `cmd/parametron/reference_structure_test.go` supplies
  `TestContractPackage_ResourceQualifiedReferencesOptionA` and
  `TestContractPackage_EmptyReferenceCompleteness`: controlled normal CLI proof
  of cross-resource normalization and all three empty-scope completeness states.
  Raw bytes and SHA-256 provenance are preserved; equivalent relationship keys
  and ordering do not require identical containing record identities or bytes.

Generic observed reference identity is independent of operational working-copy
locations and runtime-reported working-copy digests. Those materials remain in
the normalized record and raw evidence. Native traversal identity keeps accepted
contract-relative fallback coordinates, while relationship keys and ordering are
unchanged.

## Technical drawing capture coverage

Technical drawings are first-class, CAD-neutral capture entities in
`prm.cad.json` (`entities.drawings[]`) and in `semantic.Model` (`drawings[]`).
They are logical drawing relationships with ordered, native-bound sheets, not
DSL targets, planner inputs, export semantics, or reference-record edges.
Capture may declare zero, one, or multiple drawings per part/assembly; each
drawing carries drawing-level `componentIds` and `resource`, and each sheet
carries an explicit positive `order` and required
`identitySource.kind: "native_ref"`.

| Permanent test file | Verification responsibility |
|:--------------------|:----------------------------|
| `internal/engine/cad/drawings_test.go` | Closed-field drawing/resource/sheet parsing, required non-null `entities.drawings`, global ID uniqueness, component resolution, sheet ordering, embedded/separate resource rules, and canonical byte stability |
| `internal/engine/semantic/drawings_test.go` | Projection into `semantic.Model`, internal `NativeRef` retention and JSON exclusion, canonical drawing/component/sheet ordering, `DrawingsForComponent` query readiness, and validation failure determinism |
| `internal/engine/semantic/drawings_real_capture_test.go` | Committed real FreeCAD drawing captures from parametron-freecad #93/#97 validate and project deterministically for zero, single-sheet, multi-sheet, and multiple-drawing cases; native selectors stay out of semantic JSON |
| `cmd/parametron/drawings_real_capture_project_test.go` | The same real capture projects pass `validate --project` repeatedly through both project entrypoints without structural repair |
| `cmd/parametron/drawings_project_boundary_test.go` | CLI project ingestion accepts zero/one/multiple drawings and both topologies, and rejects an unresolved drawing component |
| `internal/engine/semanticmap/drawings_boundary_test.go` | Strict semantic-map parsing has no `captureToSemantic.drawings`; identity linkage ignores semantic drawings and adds no drawing operation capability |
| `internal/engine/recordmap/semantic_drawings_boundary_test.go` | Native traversal mapping never emits `ReferenceKindDrawing` and semantic drawings never enter reference records |
| `internal/engine/cad/fixture_alignment_test.go` | Every parseable repository `prm.cad.json` declares a non-null `entities.drawings` array |

Real FreeCAD capture fixtures are committed under
`testdata/projects/freecad/drawings/` as verbatim snapshots of the
`parametron-freecad` #93/#97 capture outputs (single-sheet, multi-sheet,
multiple-drawings) together with their source `.FCStd` fixtures. The
zero-drawing case uses the existing rehearsal cube fixture. Engine validates
and projects these captured contracts deterministically; no test performs live
FreeCAD execution.

`embedded` and `separate` are CAD-neutral resource topologies. FreeCAD v1.0
capture emits `embedded` drawings because it promotes active-document pages
only; `separate` is exercised by Engine fixtures. `resource.path` is
project-relative and does not enter declared-resource inventory, reference
records, or integrity records in v1.0. Sheet `NativeRef` values are retained
internally and are never exposed in semantic-model JSON.

Canonical ordering is drawings by `id`, `componentIds` lexicographically, and
sheets by `order` then `id`. Drawing layout automation and export semantics
remain deferred to `parametron-strategy#7` and `#8`; v1.0 adds no drawing DSL
target, no `captureToSemantic.drawings` policy, and no `ReferenceKindDrawing`
traversal mapping.

## Multi-outcome engineering observation coverage

Permanent tests prove the removal of the single-eligible-outcome evidence-loss
boundary while preserving family semantics:

| Permanent test file | Test identifiers | Verification responsibility |
|:--------------------|:-----------------|:----------------------------|
| `internal/engine/recordemit/multi_outcome_test.go` | `TestEmitRunPackage_MultiOutcome_EmitsEveryOutcomeScopedFamily`; `TestEmitRunPackage_MultiOutcome_DistinctOutcomesKeepDistinctIdentity`; `TestEmitRunPackage_MultiOutcome_DeterministicAcrossInputOrder`; `TestEmitRunPackage_MultiOutcome_RejectsIneligibleAndAmbiguousOutcomes` | Two eligible outcomes each emit observation, reference, verification, occurrence, quantity, fact, integrity, and raw evidence; quantity stays scoped per outcome; same fact subject/kind with different values remains distinct; configured-result identity stays outcome-specific while source identity is shared; identical traversal bytes remain independently addressable; outcome input order does not change package paths, bytes, or manifest ordering; runtime-failed and duplicate-identity outcomes are rejected deterministically |
| `internal/engine/recordpackage/outcome_scope_test.go` | `TestOutcomeScopedRecordsUseDeterministicPathsAndAllowPluralFamilies`; `TestOutcomeScopedRecordsRejectDuplicateScopeAndUnsafeKeys`; `TestOutcomeScopedRawEvidenceIsClassifiedAndOrdered` | Deterministic `records/outcomes/<outcomeKey>/` record paths, plural families across scopes, duplicate family rejection within one scope, unsafe outcome-key rejection, and classification/ordering of `raw/outcomes/<outcomeKey>/` evidence |
| `cmd/parametron/multi_outcome_emission_test.go` | `TestCLIProjectRun_MultiProductEmitsEveryOutcomeScopedEngineeringFamily` | Controlled normal CLI execution of two products sharing a source resource emits both outcome-scoped observation, reference, verification, and raw evidence groups without singular-path fallback |
| `cmd/parametron/cli_test.go` | `TestRunOutcomeEvidences_ZeroEligibleOutcomes`; `TestRunOutcomeEvidences_ProjectsEveryEligibleOutcome`; `TestRunOutcomeEvidences_MultipleEligibleOutcomesRemainDistinct`; `TestRunOutcomeEvidences_CapturesExactBytesPerOutcome`; `TestRunOutcomeEvidences_PropagatesPathSafetyErrors` | Eligibility filtering for failed/unverified outcomes, projection of every eligible verified outcome, distinct outcome groups, exact evidence bytes, and guarded evidence reads |
| `internal/engine/recordcontract/evidence_paths_test.go` | `TestOutcomeScopedEvidenceRefAcceptsCanonicalAndSafeScopedForms` | Fact and occurrence evidence references accept canonical or safe outcome-scoped raw paths and reject escaping, malformed, or unknown forms |

Across distinct outcomes, equivalent evidence is not globally deduplicated:
each outcome keeps its own record identity, raw path, and provenance. Existing
family-level duplicate and ordering rules remain unchanged within one outcome.
Artifacts and failure projection remain under their existing semantics; the
selected runtime failure keeps canonical raw failure paths.

These Engine tests use controlled traversal evidence, not real FreeCAD execution.
The finite declared inventory need not have a native edge for each member.
Completeness covers supported traversal and resource joins, not recursive
closure or assembly occurrence coverage. Automatic external mapping population,
general multi-outcome aggregation, occurrence identity, and quantity/BOM inference
remain outside this proof.

## Coordinated engineering-observation closure coverage

`cmd/parametron/coordinated_closure_rehearsal_test.go` is the permanent #45
closure rehearsal. It drives planning, handoff, per-product attempt preparation,
the controlled external `runtimecap` process boundary, occurrence/fact sidecars,
reference traversal, integrity request/evidence, Engine verification, and
normal package emission without real FreeCAD.

| Test identifier | Verification responsibility |
|:----------------|:----------------------------|
| `TestCoordinatedClosure_MultiResourceMultiOutcomeRehearsal` | Two products over one primary source plus an aliased resource and a zero-reference standalone resource; cross-resource generic references; declared, undeclared, and unsupported targets; explicit occurrences with complete quantity rollup and cross-resource targets; bounded material/property, unit-bearing numeric, context-scoped, and unavailable facts; locked source/baseline/result provenance; outcome-scoped raw and normalized package evidence; no cross-outcome overwrite or aggregation |
| `TestCoordinatedClosure_RepeatRunDeterministicSurfaces` | Repeated fresh controlled executions preserve deterministic Engine-owned normalized record paths, manifest bytes, and normalized record bytes for identical controlled evidence |
| `TestCoordinatedClosure_UnsupportedVersionContractsAreRejected` | Observed, occurrence request, fact request, integrity request/evidence, reference traversal, and normalized record version surfaces reject unsupported schema versions |
| `TestCoordinatedClosure_NegativeSurfaceIsolation` | Incomplete occurrence scope retains explicit incomplete state with no quantity while otherwise valid facts remain observable, proving one surface does not silently corrupt another |

The closure matrix below maps the parent #45 requirements to current evidence.
Canonical cross-repository descriptions remain owned by `parametron-docs`.

| Parent requirement | Closure evidence |
|:-------------------|:-----------------|
| Versioned Engine-owned request/evidence contracts with strict version behavior | `internal/engine/observed`, `internal/engine/cadruntime` integrity/occurrence/fact tests, `internal/engine/recordmap` traversal tests, and `TestCoordinatedClosure_UnsupportedVersionContractsAreRejected` |
| Explicit resource/source/materialization joins | `internal/shared/projectmap`, `internal/engine/resourcebinding`, `internal/engine/projectinput`, `internal/engine/adapter/freecad` materialization tests, and the coordinated rehearsal |
| Explicit occurrence identity/cardinality and complete-only quantity | `internal/engine/recordcontract/occurrence_test.go`, `internal/engine/cadruntime/occurrence_evidence_test.go`, `internal/engine/recordemit/occurrence_test.go`, and the coordinated rehearsal |
| Generic reference distinctions without occurrence fabrication | `internal/engine/recordmap/reference_structure_test.go`, `internal/engine/recordcontract/reference_structure_test.go`, `cmd/parametron/reference_structure_test.go`, and the coordinated rehearsal |
| Bounded facts, explicit availability, and unit representation | `internal/engine/cadruntime/fact_evidence_test.go`, `internal/engine/recordemit/fact_test.go`, `docs/test-matrix.md` fact coverage, and the coordinated rehearsal |
| Source lock, materialized baseline, runtime baseline, and persisted-result separation | `internal/engine/integrity`, `internal/engine/cadruntime` integrity tests, `cmd/parametron/integrity_run_test.go`, and the coordinated rehearsal |
| Engine integrity enforcement and independent persisted-result validation | Integrity failure tests plus `TestIntegrityNormalRunLockedVerifiesSourceAndPersistedResult` and persisted-result mismatch coverage |
| Zero-reference/standalone resources | Declared-resource tests, integrity coverage, and the coordinated rehearsal |
| Multi-outcome evidence retention without aggregation | Multi-outcome coverage above and the coordinated rehearsal |
| Raw evidence versus normalized records and adapter-native vocabulary boundary | Raw-evidence classification tests, record-package emission tests, and the coordinated rehearsal |
| FreeCAD adapter handoff sufficiency | README FreeCAD handoff section plus the canonical `parametron-docs` contracts |

## Explicit occurrence contract coverage

The [local contract summary](../README.md#explicit-occurrence-observation)
describes the bounded FreeCAD 1.1.4 direct-member contract. These permanent tests
use controlled Engine evidence; none proves real FreeCAD-native occurrence
discovery or production emission. Generic reference proof above remains separate
from explicit occurrence/quantity proof below.

| Permanent test file | Test identifiers | Verification responsibility |
|:--------------------|:-----------------|:----------------------------|
| `internal/engine/recordcontract/occurrence_test.go` | `TestOccurrenceIdentitySeparatesMemberParentResourceAndContext`; `TestOccurrenceContextBoundsOrderAndCycles`; `TestOccurrenceSummaryRejectsFabricatedQuantitiesAndIndependentVersion` | Member/parent/resource/context identity, optional semantic annotation, ordered 16-frame bound and cycle rejection, independent summary version, and rejection of duplicate members or fabricated/noncomplete quantities |
| `internal/engine/cadruntime/occurrence_evidence_test.go` | `TestOccurrenceSupportedTargetsAndDistinctQuantity`; `TestOccurrenceRejectsUnprovenNativeEvidence`; `TestOccurrenceQuantityResourceAndEntitySeparation` | Supported Body/Feature/Part targets, distinct same-target members, complete empty scope, exact native eligibility, parent/resource/materialized-coordinate joins, and separate native entities/resources in rollup |
| Same file | `TestOccurrenceAuxiliaryRequiresVerifiedEmptyContents`; `TestOccurrenceAuxiliaryJSONRequiresExplicitInspectionInventory` | Exact empty joint-group exclusion; explicit completed empty inventory; rejection of missing, null, nonempty, contradictory, or unclassified auxiliary evidence and concealed coverage gaps |
| Same file | `TestOccurrenceStrictJSONAndVersionIsolation`; `TestOccurrenceRequestRequiredFieldsAndTypes` | Request schema 1.0, evidence 1.0/1.1 acceptance, closed legacy member shape, unknown/duplicate/case-duplicate/trailing JSON rejection, and required fields/types |
| Same file | `TestOccurrenceAvailabilityAndRequiredCompleteness`; `TestOccurrenceNoncompleteMembersCannotProduceQuantity`; `TestOccurrenceScopeContextCannotAuthorizeRecursiveQuantity` | Complete zero versus incomplete/unsupported/unavailable/omitted quantity; optional/required completeness; accepted partial members without numeric quantity; nested context cannot authorize recursive quantity |
| Same file | `TestOccurrenceDuplicateConflictAndSemanticDeterminism` | Exact duplicates canonicalize; conflicts fail under reordered evidence; member/inventory ordering leaves normalized occurrences and rollup stable while exact-byte digest changes; semantic annotations do not split groups |
| `internal/engine/cadruntime/occurrence_request_test.go` | `TestOccurrenceRuntimeSidecarAndVerificationBoundary` | Sidecar materialization, canonical confined intake, no-request behavior, exact bytes, typed required-completeness and invalid-evidence failures at `occurrence_validation`, alternate-path and escaping-symlink rejection |
| `internal/engine/executor/occurrence_test.go` | `TestCADRuntimeAttemptRequestPreservesOccurrenceIntent`; `TestExecutePackageWithOptionsOwnsOccurrenceIntentAcrossRetriesAndExecutions`; `TestOccurrenceOutcomeRetentionAndRequiredFailure` | Explicit optional/complete intent through dispatch and attempts, defensive copying, retry and later-execution isolation, outcome retention, default execution, and typed required/malformed failures |
| `internal/engine/recordemit/occurrence_test.go` | `TestEmitOccurrencePreservesRawProvenanceAndIndependentReferences`; `TestEmitOccurrenceOptionalOmissionAndRequiredFailure` | Observation integration preserving ordinary facts and independent references; exact raw bytes and SHA-256 provenance; stable semantic rollup with evidence-sensitive containing identity; omission versus required failure |
| `cmd/parametron/occurrence_test.go` | `TestCADRuntimeRunEvidenceOccurrenceCardinalityAndCopy` | Zero/one/multiple eligible outcome projection retaining every eligible outcome, exact transport, and defensive copying |
| `cmd/parametron/occurrence_execution_test.go` | `TestOccurrenceExecutionCompletePackageAndProvenance`; `TestOccurrenceExecutionEvidenceStatesAndFailures` | Continuous Engine API execution through attempt sidecar, runtime invocation, intake, verification, retained outcome, normal projection, and package emission; three distinct occurrences form two native quantity groups; reordered evidence preserves semantics and changes provenance-bearing identity; independent generic references; optional/complete failures and no-intent preservation |

The continuous proof calls `ExecutePackageWithOptions` on a planned handoff and
uses the existing controlled external runtime participant for ordinary CAD output;
a controlled capability supplies occurrence evidence. It is not occurrence
authoring through public CLI flags or DSL, nor native FreeCAD discovery.
Its package checks include raw-evidence classification and exact bytes at
`raw/runtime/prm.occurrence-evidence.json` or its safe outcome-scoped form.

Direct occurrence coverage preserves the declared-resource joins and
provenance-sensitive identity contracts. Existing capture-backed native-target
tests, declared-resource tests, and resource-qualified reference tests documented
above remain the separate regression coverage for #42, #48, and #49. Their
relocation and native-target guarantees must not be recast as recursive occurrence
or real FreeCAD quantity proof.

Focused verification uses the existing package test interface:

```bash
go test ./internal/engine/recordcontract/... ./internal/engine/cadruntime/... ./internal/engine/executor/... ./internal/engine/recordemit/... ./cmd/parametron/... -run 'Test.*Occurrence'
```

## Bounded engineering fact observation contract coverage

The [bounded fact observation summary](../README.md#bounded-engineering-fact-observation)
describes the bounded `bound_property` fact contract. These permanent tests use
controlled Engine evidence; none proves real FreeCAD-native fact observation,
because no native fact producer exists yet. Occurrence proof above stays
separate from fact proof below.

| Permanent test file | Test identifiers | Verification responsibility |
|:--------------------|:-----------------|:----------------------------|
| `internal/engine/recordcontract/fact_test.go` | `TestCanonicalFactScalarAcceptsFiniteTypedValues`; `TestCanonicalFactScalarCanonicalizesEquivalentNumbersToOneIdentity`; `TestCanonicalFactScalarRejectsNonFiniteAndMalformedValues`; `TestFactVocabularyIsClosed` | Finite typed scalar acceptance, canonical numeric identity collapsing `42`, `42.0`, and `4.2e1`, non-finite and malformed value rejection, and closed kind/availability/requirement/value-kind vocabularies |
| Same file | `TestValidateFactBindingAcceptsBoundedContract`; `TestValidateFactBindingRejectsInvalidContract`; `TestValidateFactRequestRejectsInvalidContract`; `TestFactBindingKeyExcludesNonIdentityMaterial` | Bounded binding and request acceptance and rejection, and fact slot keying that excludes semantic annotations and observed value material |
| Same file | `TestBuildObservationRecordFactKindPreservesQualifiedSubject`; `TestBuildObservationRecordUnavailableFactIsExplicitAndValid`; `TestValidateObservationRecordRejectsInvalidFacts`; `TestBuildObservationRecordFactConflictSemantics` | Resource-qualified `fact`-kind subject preservation, explicit unavailable facts without empty-record fabrication, record-level invalid and conflicting fact rejection |
| Same file | `TestBuildObservationRecordFactIdentityAndOrderingAreDeterministic`; `TestParameterObservationRecordIdentityMatchesPreChangeGolden` | Deterministic fact identity and ordering, and pre-change `parameter` observation record identity preservation against `internal/engine/recordcontract/testdata/parameter-observation-record.golden.json` |
| `internal/engine/cadruntime/fact_request_test.go` | `TestDecodeFactRequestAcceptsVersionedContract`; `TestDecodeFactRequestRejectsInvalidContent`; `TestCloneFactRequestIsIndependent`; `TestWriteFactRequestMaterializesExactVersionedIntent` | Strict versioned request sidecar decoding, invalid-content rejection, request cloning independence, and exact request materialization in the prepared attempt working directory |
| `internal/engine/cadruntime/fact_evidence_test.go` | `TestMapFactEvidenceObservedValues`; `TestMapFactEvidenceNumericCanonicalizationCollapsesEquivalentEvidence`; `TestMapFactEvidenceUnits` | Observed typed values, numeric canonicalization collapsing equivalent duplicate evidence, and explicit unit agreement with requested bindings |
| Same file | `TestMapFactEvidenceSubjectQualification`; `TestMapFactEvidenceAvailabilityStates` | Declared-resource and materialized-coordinate subject joins, and observed/unavailable/unsupported presence strictness per binding |
| Same file | `TestMapFactEvidenceDuplicateAndConflictSemantics`; `TestMapFactEvidenceDeterminismUnderShuffledEvidence` | Equivalent duplicate canonicalization, deterministic conflict rejection, and order-independent normalized acceptance and output |
| Same file | `TestMapFactEvidenceRejectsMalformedAndUnsolicitedEvidence`; `TestMapFactEvidenceRuntimeVersionIsProvenanceOnly`; `TestFactEvidenceProvenanceCoversUnobservedFactsFromPresentEvidence` | Malformed and unsolicited evidence rejection, provenance-only native runtime version tokens, and shared evidence provenance covering unobserved facts from present evidence |
| `internal/engine/cadruntime/fact_run_test.go` | `TestLoadFactRunAbsentEvidenceIsExplicitUnavailability`; `TestLoadFactRunPresentEvidencePreservesExactBytes`; `TestLoadFactRunRejectsUnconfinedOrEmptyEvidence` | Omitted evidence file as explicit unavailability, exact confined evidence byte retention, and unconfined or empty evidence rejection |
| `internal/engine/recordemit/fact_test.go` | `TestEmitRunPackageFactMergesIntoExistingObservationFamily`; `TestEmitRunPackageFactDoesNotAlterExistingFamilies`; `TestEmitRunPackageFactAvailabilityAndFailures`; `TestEmitRunPackageFactIsDeterministicAndRelocationIndependent` | Fact merge into the existing observation record family, preservation of other record families, optional omission versus required failure, and deterministic repeated emission with relocation-independent normalized facts |
| `internal/engine/recordpackage/fact_evidence_classification_test.go` | `TestRawFactEvidenceContractPathAndClassification`; `TestRawFactEvidenceRejectsUnknownAndUnsafePaths` | `raw/fact/prm.fact-evidence.json` contract path and raw-runtime-evidence classification; rejection of unknown and unsafe raw-evidence paths |
| `cmd/parametron/fact_evidence_test.go` | `TestCADRuntimeRunEvidenceFactCardinalityAndCopy` | Zero/one/multiple eligible outcome projection retaining every eligible outcome, exact evidence transport, defensive copying, and unsolicited evidence transported unchanged for Engine-owned rejection |

Fact observations normalize into the existing observation record family as
`observation.facts[]` material of the `fact` kind; no separate record family
exists. Raw fact evidence is preserved at `raw/fact/prm.fact-evidence.json` or
its safe outcome-scoped form, classified as raw runtime evidence, and the
request sidecar is not packaged.
Invalid contracts fail with `ErrInvalidFact`; required unavailable or
unsupported bindings fail with `ErrRequiredFactUnavailable`; record-level
conflicting facts fail with `ErrInvalidObservationRecord`. Intake failures
surface at the `outcome_validation` consumption stage. Fact observation is
separate from target-state mutation verification, and quantity remains
#50 occurrence-derived semantics, not a CAD fact.

As with occurrence proof, fact observation is not authored through public CLI
flags or DSL: opt-in intent is `ExecutePackageWithOptions` with
`PackageExecutionOptions.FactObservation` only. No native FreeCAD fact
observation, unit conversion, material inference, or physical mass is claimed
by these tests.

Focused verification uses the existing package test interface:

```bash
go test ./internal/engine/recordcontract/... ./internal/engine/cadruntime/... ./internal/engine/recordemit/... ./internal/engine/recordpackage/... ./cmd/parametron/... -run 'Fact|TestParameterObservationRecordIdentityMatchesPreChangeGolden'
```

## Source lock and configured-result provenance contract coverage

The [source lock and configured-result provenance summary](../README.md#source-lock-and-configured-result-provenance)
describes the enforced project source/input lock, the separate source, baseline,
and result content identities, and the runtime integrity request/evidence
contracts. These permanent tests use controlled runtimes and stub capabilities;
none proves real FreeCAD integrity execution, because no native FreeCAD
integrity producer exists yet. Reference, occurrence, and fact proof above stays
separate from integrity proof below.

| Permanent test file | Test identifiers | Verification responsibility |
|:--------------------|:-----------------|:----------------------------|
| `internal/engine/integrity/digest_test.go` | `TestParseDigestAcceptsCanonicalSHA256`; `TestParseDigestRejectsNonCanonicalEncodings`; `TestNewDigestIsContentAddressed`; `TestHashFileIsPathFreeAndContentAddressed`; `TestHashFileRejectsMissingFile`; `TestDigestIsZero` | Canonical SHA-256 digest acceptance, rejection of non-canonical encodings, content-addressed identity, path-free file hashing, and missing-file failure |
| `internal/engine/integrity/failure_test.go` | `TestFailureErrorRendersOnlyStableFields`; `TestFailureErrorOmitsUnsetFields`; `TestAsFailureExtractsWrappedFailure`; `TestIntegrityFailureCodesAreDistinctAndStable` | Path-free failure rendering from stable fields only, unset-field omission, wrapped failure extraction, and distinct, stable integrity failure codes |
| `internal/engine/integrity/integrity_test.go` | `TestEvaluateLockStates`; `TestEvaluateLockSelectsFailureDeterministically`; `TestEvaluateLockIncludesZeroEdgeDeclarations`; `TestEvaluateLockDoesNotTreatEmptyDigestAsVerified` | Explicit lock states (verified, mismatch, missing, malformed, unavailable, not performed), deterministic failure selection across multiple invalid resources, zero-edge declarations, and empty digests never counting as verified |
| Same file | `TestProjectIntegrityCopyIsDefensive`; `TestOperationIntegrityCopyIsDefensive`; `TestMergeRunIntegrityKeepsContentIdentitiesSeparate`; `TestMergeRunIntegrityDefaultsUndatedStagesToNotPerformed`; `TestMergeRunIntegrityRejectsContradictoryEvidence`; `TestNormalizeRunIntegrityIsDeterministic` | Defensive copies, separate source/baseline/runtime-baseline/result identities, explicit not-performed stages, rejection of duplicate, contradictory, or undeclared provenance evidence, and deterministic normalization |
| `internal/engine/projectinput/source_identity_test.go` | `TestAttachSourceIdentitiesCoversEveryDeclaredResource`; `TestAttachSourceIdentitiesIsRelocationInvariant`; `TestAttachSourceIdentitiesValidatesBidirectionalCoverage`; `TestAttachSourceIdentitiesWithoutCapturedResourcesFailsClosed`; `TestAttachSourceIdentitiesWithoutProjectIsEmpty`; `TestAttachSourceIdentitiesDoesNotMutateCapturedResources` | Path-free source identity for every declared resource including unused, zero-reference, and standalone declarations; relocation-invariant content identity; bidirectional declared/captured coverage; fail-closed capture joins; empty projects; captured-resource immutability |
| `internal/engine/adapter/freecad/materialized_baseline_test.go` | `TestVerifyFreeCADRuntimeMaterializedBaselinesAcceptsBytePreservingEquality`; `TestVerifyFreeCADRuntimeMaterializedBaselinesRejectsTamperedMaterialization`; `TestVerifyFreeCADRuntimeMaterializedBaselinesRejectsMissingMaterializedResource`; `TestVerifyFreeCADRuntimeMaterializedBaselinesReportsUnmaterializedResource`; `TestVerifyFreeCADRuntimeMaterializedBaselinesIsRelocationInvariant`; `TestVerifyFreeCADRuntimeMaterializedBaselinesReportsFirstFailureDeterministically`; `TestVerifyFreeCADRuntimeMaterializedBaselinesWithoutSourceIdentityStillHashes` | Byte-preserving source-to-materialized equality, tampered and missing materialization rejection, explicit unmaterialized reporting, relocation-invariant digests, deterministic first-failure selection across resources, and hashing without captured source identity |
| `internal/engine/cadruntime/integrity_request_test.go` | `TestComposeFreeCADIntegrityRequestComposition`; `TestComposeFreeCADIntegrityRequestIsByteDeterministic`; `TestComposeFreeCADIntegrityRequestRejectsInvalidBaselines`; `TestParseFreeCADIntegrityRequestStrictDecoding`; `TestFreeCADIntegrityRequestPathHandling`; `TestWriteFreeCADIntegrityRequestPublishesExactBytes` | Request composition, byte determinism, invalid-baseline rejection, strict schema 1.0 decoding, attempt-scoped path handling, and exact published bytes under the integrity root |
| `internal/engine/cadruntime/integrity_evidence_test.go` | `TestEvaluateFreeCADRuntimeIntegrityAcceptsMatchingEvidence`; `TestEvaluateFreeCADRuntimeIntegrityMissingBaselineEvidence`; `TestEvaluateFreeCADRuntimeIntegrityMalformedBaselineDigest`; `TestEvaluateFreeCADRuntimeIntegrityBaselineMismatchIsDistinct` | Matching runtime baseline acceptance, missing and malformed baseline digests, and runtime baseline mismatch distinct from materialization mismatch |
| Same file | `TestEvaluateFreeCADRuntimeIntegrityRejectsUnrequestedCoordinate`; `TestEvaluateFreeCADRuntimeIntegrityNonObservedSecondaryStatuses`; `TestEvaluateFreeCADRuntimeIntegrityNonObservedPrimaryIsRejected` | Unrequested-coordinate rejection, accepted `unavailable`/`missing`/`unsupported` secondary-resource statuses, and rejection of non-observed primary resources |
| Same file | `TestEvaluateFreeCADRuntimeIntegrityPersistedResultValidation`; `TestEvaluateFreeCADRuntimeIntegrityReportsDistinctEvidenceAndProvenanceConflict` | Persisted-result agreement between runtime-reported and Engine-computed digests, distinct status handling for missing, malformed, inaccessible, and mismatched results, and distinguishable evidence and provenance conflicts |
| Same file | `TestParseFreeCADIntegrityEvidenceStrictDecoding`; `TestParseFreeCADIntegrityEvidenceOrdersResourcesDeterministically`; `TestEvaluateFreeCADRuntimeIntegrityIsOrderAndRelocationInvariant`; `TestEvaluateFreeCADRuntimeIntegrityMultipleResourcesDoNotCollide`; `TestLoadFreeCADIntegrityEvidence` | Strict schema 1.0 decoding with duplicate, unordered, and unknown-field rejection; deterministic resource ordering; order and relocation invariance; same-filename resources not colliding; confined evidence loading |
| `internal/engine/cadruntime/freecad_runtime_execution_integrity_test.go` | `TestInvokeAndValidateFreeCADRuntimeRejectsMaterializationMismatchBeforeInvocation`; `TestInvokeAndValidateFreeCADRuntimePublishesIntegrityRequestAndValidatesEvidence`; `TestInvokeAndValidateFreeCADRuntimeWithoutIntegrityRootOmitsTransport`; `TestInvokeAndValidateFreeCADRuntimeClassifiesInvalidEvidence`; `TestInvokeAndValidateFreeCADRuntimeReportsMalformedEvidenceLoadStage`; `TestIntegrityRequestEvidenceFixtureUsesDeclaredResources` | Materialization mismatch rejected before runtime invocation, integrity request publication and evidence validation across the invocation boundary, omitted transport without an integrity root, invalid-evidence classification, malformed-evidence load stage, and declared-resource fixtures |
| `internal/engine/executor/cad_runtime_source_lock_test.go` | `TestExecutorSourceLockRejectsInvalidSourceBeforeOrchestration`; `TestExecutorSourceLockVerifiedSourceProceeds`; `TestExecutorUnlockedWorkflowProceedsWithoutVerificationClaim`; `TestExecutorSourceLockFailureIdentityIsPathFree` | `source_lock` dispatch enforcement before any orchestration or runtime invocation, verified sources proceeding, unlocked workflows without false verification claims, and path-free failure identity |
| `internal/engine/runtimecap/integrity_request_test.go` | `TestIntegrityRequestEnvAppliesEngineValue`; `TestIntegrityRequestEnvStripsInheritedValueWithoutEnginePath`; `TestNormalizeRequestIntegrityRequestPath`; `TestBuildCommandKeepsArgvIndependentOfIntegrityRequestPath`; `TestExternalCapabilityTransportsIntegrityRequestThroughEnvironment`; `TestExternalCapabilityOmitsIntegrityRequestEnvironmentWhenAbsent` | Engine-owned `PARAMETRON_RUNTIME_INTEGRITY_REQUEST` transport: applied values, stripped inherited values without a published request, normalized request paths, argv independence, process-level transport, and omission when absent |
| `internal/engine/recordcontract/provenance_integrity_test.go` | `TestNormalizeProvenanceIntegrityOrdersAndSeparatesIdentities`; `TestValidateProvenanceIntegrityRejectsInvalidStates`; `TestDeriveIdentityTreatsIntegrityAsSeparateProvenanceMaterial` | Ordered, separate source, baseline, runtime-baseline, and result provenance identities, invalid-state rejection, and integrity remaining separate provenance material under the shared record identity contract, preserving existing record bytes for runs without integrity material |
| `internal/engine/recordmap/integrity_test.go` | `TestMapIntegrityProvenanceSeparatesAndOrdersIdentities`; `TestMapIntegrityProvenanceIsDeterministic`; `TestMapIntegrityProvenanceUnlockedWorkflow`; `TestMapIntegrityProvenanceNilAndInvalid` | Deterministic provenance mapping with separate content identities, stable ordering, unlocked `not_performed` representation, and nil/invalid handling |
| `internal/engine/recordmap/report_integrity_test.go` | `TestMapReportClassifiesIntegrityFailuresDistinctly`; `TestMapReportIntegrityClassificationIsDeterministicAcrossCodes`; `TestMapReportNonIntegrityFailureRemainsExecutionClass` | Distinct integrity failure classification with specific codes preserved, deterministic classification across codes, and non-integrity failures keeping their existing classes |
| `internal/engine/recordemit/integrity_test.go` | `TestEmitRunPackagePreservesRawIntegrityEvidenceVerbatim`; `TestEmitRunPackageIntegrityRecordsAreByteDeterministic`; `TestEmitRunPackageWithoutIntegrityLeavesProvenanceUnchanged` | Verbatim raw evidence preservation at `raw/runtime/prm.integrity-evidence.json` with SHA-256 provenance, byte-deterministic integrity-bearing records, and unchanged provenance for runs without integrity material |
| `cmd/parametron/integrity_run_test.go` | `TestIntegrityNormalRunLockedVerifiesSourceAndPersistedResult`; `TestIntegrityNormalRunUnlockedRecordsNotPerformed` | Continuous normal CLI proof: a locked run verifies source and persisted-result identity end to end, and an unlocked run records `not_performed` without a verification claim |
| Same file | `TestIntegrityRunRejectsChangedSourceAfterLock`; `TestIntegrityRunRejectsMalformedLockDigest`; `TestIntegrityRunRejectsDeclaredResourceMissingFromLock` | Source bytes changed after lock creation, malformed locked digests, and declared resources missing from the required lock scope fail deterministically with class/stage `integrity` |
| Same file | `TestIntegrityRunRejectsRuntimeBaselineMismatch`; `TestIntegrityRunRejectsPersistedResultMismatch`; `TestIntegrityRunRejectsUnrequestedEvidence` | Runtime baseline mismatch, persisted-result content mismatch, and unrequested evidence coordinates remain distinct deterministic end-to-end failures |
| `internal/engine/resourcebinding/resourcebinding_test.go` | `TestInventoryValidatesSourceDigest` | Declared-resource bindings reject non-canonical path-free source digests |
| `internal/shared/projectlock/projectlock_test.go` | `TestValidate_InvalidForms` (extended) | `prm.project-lock.json` rejects short, overlong, and non-lowercase-hex digests as invalid lock forms |

Integrity semantics are coordinated with but separate from reference (#49),
occurrence (#50), and fact (#51) proof: these tests never establish reference
resolution, occurrence completeness, or fact availability from digest
validity. Resource qualification goes through the #48 declared-resource
identity model, and zero-reference, unused, and standalone declared resources
participate in lock enforcement. Configured-result digests are never written
into `prm.project-lock.json`, and no recursive dependency discovery or metadata
mutation is covered. The contracts are intended to be sufficient for a later
FreeCAD-native integrity implementation without redesigning Engine
semantics; that adapter phase is not covered here.

Focused verification uses the existing package test interface:

```bash
go test ./internal/engine/integrity/... ./internal/engine/projectinput/... ./internal/engine/adapter/freecad/... ./internal/engine/cadruntime/... ./internal/engine/executor/... ./internal/engine/runtimecap/... ./internal/engine/recordcontract/... ./internal/engine/recordmap/... ./internal/engine/recordemit/... ./internal/shared/projectlock/... ./cmd/parametron/... -run 'Test.*(Integrity|Digest|HashFile|Failure|Lock|SourceIdentity|MaterializedBaseline|Validate_InvalidForms)'
```

## Known pre-existing test-order limitation

`go test -shuffle=on ./cmd/parametron/...` can intermittently fail the
`TestLoadPlannedRun_ProjectMode*` tests in `cmd/parametron/harness_cmd_test.go`
with table I/O errors resolving a fixture inside another test's temporary
working directory, indicating a pre-existing order coupling through shared
process working-directory state that relative project entrypoints depend on.
This failure is pre-existing: it was identified while adding #51 fact coverage
and reproduced on the parent commit before fact changes; it does not involve
any fact observation test. Plain `go test ./...` has not been observed to
trigger it in fresh Nix runs. It is recorded here so it is not mistaken for a
#51 regression; no fix is claimed or covered by any current test.

## Integration and End-to-End Proof

| Boundary | Coverage | Validation Command | Notes |
|:---------|:---------|:-------------------|:------|
| **Controlled Simulation Proof** | End-to-end multi-attempt retry, timeout handling, verification failure, cancellation, and API execution | `python scripts/cad_runtime_integration_proof.py --mode fake` | Uses deterministic mock CAD runtime; no external FreeCAD required |
| **Declared Resource Execution Propagation** | Project input → CLI → scheduler → executor → runtime orchestration → attempt → outcome; product-specific selections, complete inventory, context snapshots/defensive copies, multiple products, and retries | `go test ./cmd/parametron/... -run 'TestContractPackage_.*Declared'; go test ./internal/engine/scheduler/... -run TestSchedulerResourceContextsSnapshotAndProductIsolation` | `cmd/parametron/declared_resources_test.go` uses the controlled external contract-package participant; `internal/engine/scheduler/resource_context_test.go` checks scheduler/executor propagation independently. Planned/computed inventories remain unmaterialized; prepared outcomes carry completed coordinates, including on runtime failure after preparation. Existing serialized handoff/runtime request semantics and single-source manifests are preserved. Declared-resource staging alone proves no native opening, recursive discovery, link rebinding, or resource-qualified reference normalization. Separate reference mapping and controlled CLI tests prove Engine resource-qualified normalization |
| **Engine-owned FreeCAD Runtime Contract Package** | Permanent normal CLI proof from project and DSL target actions through planning, scheduling, executor handoff, isolated attempt preparation, the actual external `runtimecap` process boundary, evidence intake, Engine verification, outcome consumption, and normal record-package emission | `go test ./cmd/parametron/... -run 'TestContractPackage_'` | Uses a repository-controlled external runtime participant, not real FreeCAD. The separated native-target proof covers `suppress` and `hide`, carrying `Body01` through runtime requests, returned controlled evidence, and normalized package facts. Covers ordinary execution; `suppress`, `unsuppress`, `hide`, `unhide`, and `delete`; Part/Assembly destinations; combined mutation families; schema `1.0` content across the manifest, verification request, traversal request, result, observed output, and returned traversal evidence; deterministic ordering and request materialization; staged-source and attempt-path containment; observation independence from requested intent; suppression, visibility, and deletion mismatches; omitted required evidence; unavailable native evidence; missing targets; malformed evidence rejection before semantic verification; and runtime-native failure separation. Eligible traversal evidence produces a normalized reference record with exact raw-byte provenance. Successful runs emit applicable execution, artifact, observation, and verification records while preserving raw result, verification-request, and observed evidence separately. Equivalent runs with identical complete contract inputs, including reference evidence digests, prove stable plan, job, and attempt identities, canonical runtime manifests, equivalent-location observation requests, semantic verification results, normalized records, and package entry ordering and identities; runtime-produced operational evidence is not claimed byte-identical. This proof covers the normal CLI path and does not cover HTTP API target-mutation submission or real FreeCAD-native mutation and observation. The companion `reference_structure_test.go` proves complete inventory forwarding and selected-source correlation, cross-resource same-name objects, independent property/mechanism distinctions, stable relationship keys/order under relocation and reordered raw evidence, evidence-sensitive record identity (Option A), exact bytes/digests, and complete/incomplete/unavailable zero-edge records on an overall successful run. Failed traversal aggregate status does not itself imply failed Engine execution. |
| **Opt-in FreeCAD Integration** | Live CAD document mutation, recompute, STEP export, Engine verification, and repeated execution using `parametron-freecad` | `PARAMETRON_RUN_FREECAD_INTEGRATION=1 python scripts/cad_runtime_integration_proof.py --mode real --freecad-repo ../parametron-freecad` | Requires the sibling FreeCAD runtime to accept the canonical schema 1.0 traversal request and emit rich schema 1.0 traversal evidence. The completed real-native rehearsal for `parametron-freecad#9` additionally proved suppress/unsuppress, hide/unhide, safe deletion, unsafe-delete rejection, representative post-mutation native validity failure, target-state observation, schema-1 reference traversal, Engine verification and normal record/package return, raw-evidence provenance, and repeatability against FreeCAD 1.1.1. |
| **Opt-in Real FreeCAD Evidence Acceptance** | Engine-authored occurrence, fact, and integrity requests through the production `parametron-freecad` launcher and real FreeCAD 1.1.4, followed by Engine strict intake, normalization, quantity rollup, provenance, package emission, and independent persisted-result hashing | `PARAMETRON_FREECAD_EVIDENCE_PROOF=1 PARAMETRON_FREECAD_RUNTIME=<wrapper> PARAMETRON_FREECAD_REPO=../parametron-freecad go test ./cmd/parametron -run '^TestRealFreeCADEvidenceProof$' -count=1 -v` or `python scripts/cad_runtime_integration_proof.py --mode evidence-real --freecad-repo ../parametron-freecad` | Covers ten-occurrence complete quantity, ordinary references without quantity, shared targets, duplicate native names in separate declared resources, explicit incomplete/unsupported/unresolved/auxiliary states, scalar and unit-bearing facts, unavailable/unsupported facts, deterministic required-fact failure at `fact_evidence`, baseline mismatch rejected before mutation, relocation, repeated normalized structure, and two outcomes retaining independent outcome-scoped evidence. Records the FreeCAD-side external-link layout blocker instead of substituting a fake runtime. Requires Git LFS fixtures and the FreeCAD 1.1.4 Nix environment; persisted `.FCStd` bytes are not claimed identical across runs. |
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
