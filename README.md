# Parametron Engine

Parametron Engine is a deterministic execution engine for engineering automation.

It parses and validates Parametron DSL inputs, builds deterministic execution
plans, invokes external CAD/runtime capabilities through Engine-owned contracts,
verifies returned evidence, and produces normalized records and local run
artifacts.

The Engine is designed to remain independent from CAD application internals,
workflow systems, and durable storage. CAD-specific execution is delegated to
external runtimes such as
[`parametron-freecad`](https://github.com/parametron-io/parametron-freecad).

## Features

Parametron Engine currently provides:

- deterministic DSL parsing and validation
- semantic analysis and intermediate representation generation
- deterministic planning and execution identities
- scheduling and local execution
- cache-aware execution
- simulation and plan inspection
- CAD adapter and runtime capability contracts
- deterministic finite declared CAD-resource inventory with project-scoped identity separate from operational paths
- declared-resource staging within isolated execution attempts
- aligned external FreeCAD runtime invocation
- runtime evidence validation and verification
- normalized engineering record contracts
- local record-package generation
- artifact and raw-evidence preservation
- target-action authoring for suppression, visibility, and deletion intent
- target-state observation request and result contracts for suppression, visibility, and existence evidence
- Engine-owned target-state expectation derivation, verification, failure classification, and normalized record mapping
- CLI and HTTP API foundations

Equivalent inputs and execution context are designed to produce stable
Engine-owned identities and normalized outputs.

## Architecture

At a high level:

```text
Parametron DSL
      │
      ▼
Validation / IR
      │
      ▼
Deterministic planning
      │
      ▼
Execution / scheduling
      │
      ├── local Engine operations
      │
      └── external runtime capability
                │
                ▼
          parametron-freecad
                │
                ▼
       runtime evidence/results
                │
                ▼
        verification / records
````

Engine owns the engineering contract and interpretation of runtime results.

External runtimes own application-specific behavior. For FreeCAD, document
loading, mutation, recompute, saving, export, and native observation belong to
`parametron-freecad`, not to Engine.

Current public architecture, authoring, and adapter documentation is
maintained in `parametron-docs`. See the canonical
[Engine documentation landing page](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/README.md),
including the
[system overview](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/architecture/system-overview.md),
[execution model](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/architecture/execution-model.md),
[adapter architecture](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/adapters/README.md),
and [CAD authoring contract](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/authoring/cad-contract.md).

## Command-line interface

The local CLI lives under `cmd/parametron`.

Show available commands:

```bash
go run ./cmd/parametron --help
```

The CLI includes command families for validation, simulation, execution, sweep,
snapshot, diff, and related engineering workflows.

Command-specific documentation is maintained centrally; see the canonical
[Engine CLI documentation](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/cli/command-families.md).

## External CAD runtimes

CAD execution is performed through Engine-owned runtime contracts rather than
embedded CAD implementation code.

FreeCAD execution is provided by the separate
[`parametron-freecad`](https://github.com/parametron-io/parametron-freecad)
runtime.

The Engine-facing executable can be selected with:

```text
PARAMETRON_FREECAD_RUNTIME
```

When not explicitly configured, Engine can resolve the
`parametron-freecad` executable from `PATH`.

This separation keeps Engine independent from FreeCAD APIs and allows CAD
runtime implementations to evolve behind a stable Engine boundary.

## Records and runtime evidence

Engine defines normalized record contracts for:

* execution
* artifacts
* observations
* references
* failures
* verification

Normal runs can materialize a local record package under the run directory:

```text
parametron-record-package/
```

The package separates normalized records from raw runtime evidence.

Normal execution emits applicable execution, artifact, observation, reference,
failure, and verification records through this package. Artifact records are
plural: each artifact has its own normalized record at
`records/artifacts/<identityId>/prm.artifact-record.json`, ordered by
record identity. Other record families remain singular. Duplicate artifact
identities or resolved record paths are rejected.

Typical raw evidence includes runtime results, metadata, verification material,
observations, manifests, and runtime handoff data. Raw evidence is preserved as
evidence and is not treated as equivalent to normalized Engine records.
Observation and verification records come from Engine-owned typed
interpretation; raw bytes remain independently preserved for provenance.

Traversal reference records qualify native endpoints with declared project,
resource kind, and logical resource identity; native object names are scoped to
that resource. Neutral native relationship kind, source property, and reference
mechanism remain distinguishable. Equivalent duplicates canonicalize;
contradictory evidence for one relationship identity fails deterministically.
Native resolution and bounded traversal completeness remain explicit, including
accepted empty traversals and undeclared or unsupported targets. Generic
references, including the historical `component` kind, do not establish assembly
occurrences, quantities, or BOM totals.

Resource-qualified relationship keys and ordering are stable for equivalent
accepted relationships, including declared-resource relocation. The containing
`ReferenceRecord.Identity` remains evidence-sensitive: differing exact raw
traversal bytes can change SHA-256 provenance and record identity. Exact bytes
remain separate at `raw/runtime/prm.reference-traversal.json`. Normal successful
execution adds traversal material only for exactly one eligible verified outcome;
zero or multiple candidates add none.

Adapter-native CAD failures are interpreted by the adapter/runtime boundary as
a generic Engine CAD runtime failure outcome before `MapCADRuntimeFailure`
produces the normalized `FailureRecord`. A valid, uniquely correlated failure
from the terminal failed CAD outcome takes precedence over the report-derived
failure; otherwise the report-derived record remains authoritative. The package
still contains at most one failure record, while execution remains
report-derived and Engine retry context and plan provenance are retained.
Normal package failure records omit `OccurredAt` because their deterministic
report material has operational timestamps stripped. Runtime-native failures
remain distinct from malformed, invalid, missing, or unavailable evidence and
from Engine verification mismatches. Exact native result bytes are preserved
at `raw/runtime/prm.result.json`, and their SHA-256 digest supplies normalized
provenance without making the normalized record a mirror of native vocabulary.

See the canonical
[record contracts](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/reference/record-contracts.md)
and [execution runtime](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/runtime/execution-runtime.md)
documentation in `parametron-docs`.

## Explicit occurrence observation

Generic reference evidence is distinct from explicit occurrence evidence. Engine
owns occurrence requests, resource-qualified validation, normalized identity,
completeness interpretation, duplicate/conflict handling, quantity rollup, and
record/package emission with raw provenance. FreeCAD owns native Assembly
inspection, member discovery, membership/target observations, coverage evidence,
and production evidence generation. Engine does not perform native CAD discovery;
production FreeCAD occurrence emission is not established by the Engine tests.

### Supported native scope

The supported mechanism is
`freecad-1.1.4-assembly-group-individual-link`, with evidence requiring exact
`freecadVersion: "1.1.4"`. It accepts an exact `Assembly::AssemblyObject` root
and direct `Group` members that are individual `App::Link` objects with
`ElementCount == 0`, corroborated parent membership, valid native state, and a
resolved, unambiguously declared-resource-qualified target. Supported target
types are `PartDesign::Body`, `Part::Feature`, and `App::Part`. Each qualifying
link contributes one occurrence of its exact target; `App::Part` contents are
not expanded or counted recursively. This contract makes no FreeCAD 1.1.1
compatibility guarantee.

Evidence `1.1` can classify an exact `Assembly::JointGroup` member as `auxiliary`
only with corroborated parent membership, valid native state, `resolved`
resolution, completed contents inspection, and an explicitly empty child
inventory. Its `elementCount` and `target` must be null and its diagnostic absent
or empty. The following is only the `members[]` entry's `contents` value, not a
complete evidence document:

```json
{"inspectionCompleted": true, "group": []}
```

Such a group contributes no occurrence. Missing contents or child inventory does
not prove emptiness. Nonempty, unverified, unknown, or unclassified members cannot
be silently excluded; every direct member must be accounted for before the
scope can be complete. No general auxiliary-type exclusion is supported.

### Identity, completeness, and quantity

`OccurrenceScope` contains `parent`, ordered `context`, and `mechanism`.
`OccurrenceKey(scope, member)` combines that scope with the exact native member
name and excludes semantic annotations and the target. Each `OccurrenceEntity`
has a declared `resource` tuple `(projectId, kind, logicalId)`, `nativeKind`
(`object`), exact native `name`, and optional `semanticId`. Owning resource and
parent assembly identity remain separate from member identity and referenced
resource/native target identity. Source coordinates and attempt-relative
materialized `document` coordinates qualify evidence against the finite declared
inventory; logical identity and document coordinate must identify the same
binding. The observed parent must match the requested selected source. Absolute
paths, labels, generic relationship keys, and raw digests are not occurrence keys.

Context is an explicit ordered array of `OccurrenceContextFrame` values, each
with `parent` and `member`, bounded to 16 frames. Missing context, malformed
members, repeated parent entities, and cycles to the scope parent are rejected.
Context preserves instance coordinates but does not certify recursive membership:
accepted occurrences and complete quantity require an empty context.

Completeness is `complete`, `incomplete`, `unsupported`, or `unavailable`.
A complete scope requires the exact requested root, completed full direct-member
inspection, explicit classification of every inventory member, corroborated
ownership, supported individual links and resolved resource-qualified targets,
and no unhandled members or scope diagnostics. An inspected empty scope, including
one containing only verified empty auxiliary members, is complete with
`occurrences: []` and `quantities: []`. Incomplete or unsupported evidence can
retain accepted members but has `quantities: null`; unavailable or omitted
evidence also has unavailable quantity, never a fabricated zero.

Engine derives quantity from distinct accepted occurrences only within a complete
scope. Within its owning scope/context, the grouping key is
`OccurrenceEntityKey(target)`: referenced `(projectId, kind, logicalId)` plus
`nativeKind` and exact `name`, excluding `semanticId`. Different native entities
in one resource remain separate; distinct members targeting the same entity
contribute independently. Exact duplicate member evidence canonicalizes;
conflicting evidence for one member identity fails deterministically, including
conflicting semantic annotations. Occurrences sort by member name and quantities
by target entity key, independently of raw member or inventory ordering.

### Execution and package boundary

The Engine API entrypoint is `Executor.ExecutePackageWithOptions`, with
`PackageExecutionOptions.OccurrenceObservation` carrying one explicit
`OccurrenceRequest`. Intent is execution-scoped, defensively copied through
dispatch and each attempt, preserved across retries, and cleared for later default
executions.
`ExecutePackage` uses empty options and preserves ordinary execution. The public
CLI and DSL do not author occurrence requests.

Request and normalized summary schemas are independently `1.0`; evidence accepts
`1.0` and `1.1`. Evidence `1.0` retains its closed legacy member shape and rejects
`contents`, including explicit null. Structured auxiliary contents require `1.1`.
Request/evidence decoding rejects unknown fields, duplicate fields (including
case variants), trailing JSON, invalid versions/types, and invalid contracts;
contradictory coverage or membership evidence is rejected during intake.

Engine materializes `prm.occurrence-request.json` in the prepared attempt working
directory and reads only `prm.occurrence-evidence.json` in its confined output
directory. No additional process argument is implied. A request's `requirement`
is `optional` or `complete`: optional omitted evidence becomes unavailable;
required noncomplete evidence fails with `ErrOccurrenceCompletenessRequired`,
wrapped by `FreeCADRuntimeVerificationError` at `occurrence_validation`.
Malformed evidence fails even when observation is optional. Executor outcomes
retain the request and exact evidence bytes for normalized emission.

The existing exactly-one-eligible-verified-CAD-outcome projection boundary applies;
there is no general multi-outcome aggregation. Emission integrates the summary as
`ObservationRecord.observation.occurrences`, preserving ordinary observation facts
and the separate generic `ReferenceRecord`. Original bytes are preserved at
`raw/runtime/prm.occurrence-evidence.json`, classified as raw runtime evidence;
SHA-256 of those original bytes supplies summary evidence and record provenance
with kind `occurrence-evidence`. Stable normalized semantic keys and rollup do not
imply stable provenance-bearing record identity when raw evidence bytes differ.
The shared record identity contract remains unchanged.

This bounded contract does not support recursive assembly quantity, recursive
occurrence or cyclic assembly expansion, link arrays, `App::LinkElement` counting,
automatic recursive resource discovery, or general auxiliary exclusions.
Quantity is not a generic reference-edge count, semantic declaration, link-array
count, or CAD-reported BOM quantity. It introduces no BOM serialization, general
engineering facts, units, mass, or material calculations. Permanent verification
uses controlled Engine evidence, not real FreeCAD occurrence discovery; see the
[test matrix](docs/test-matrix.md#explicit-occurrence-contract-coverage).

## Target actions

The authoring layer supports target actions for engineering-model intent such
as:

* suppression
* visibility
* deletion

Engine resolves and validates this intent and projects it into external runtime
contracts, alongside Engine-owned target-state observation request and result
contracts for suppression, visibility, and existence evidence. The serialized
schema `1.0` request identifies the targets to observe; Engine retains the
expected final state in memory, derived from canonical mutation intent.

The external CAD runtime remains responsible for applying native document
mutations and gathering native document evidence. Engine compares accepted raw
observations with its expected state and classifies state mismatches, explicit
missing targets, unavailable native evidence, and omitted required evidence.
It can map accepted target-state evidence and verification outcomes into the
existing normalized observation and verification record families while
preserving raw evidence separately.

Normal execution emits these target-state observation and verification records
when the corresponding evidence is requested and available. No separate
target-state record family exists, raw `prm.observed.json` remains separate
evidence, and requested values are never substituted for observations.

Permanent repository tests prove the normal CLI path from target-action intent
through schema `1.0` canonical runtime requests, a controlled external runtime
process, Engine verification, and normalized/raw record-package emission. This
proof does not exercise real FreeCAD-native mutation or observation, and it does
not cover target-mutation submission through the HTTP API.

See the canonical
[Target-action contract](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/reference/target-action-contract.md).

## Repository structure

The primary implementation areas are:

```text
cmd/
  parametron/          Local CLI
  parametron-engine/   Engine API/server entry point

internal/
  authoring/           DSL, IR, validation, and planning
  engine/              execution, adapters, runtime contracts, records,
                       verification, scheduling, and related Engine services

docs/                  Repository-local architecture, development, and historical docs
testdata/              Canonical test fixtures and example inputs
```

For a more detailed map, see
[Repository structure](docs/architecture/repository-structure.md).

## Development

Requirements depend on the area being developed, but the Go toolchain is the
canonical interface for Engine development.

Run the full Go test suite:

```bash
go test ./...
```

Run static analysis:

```bash
go vet ./...
```

Run a specific package:

```bash
go test ./internal/engine/recordmap/...
```

Additional Python-based validation and opt-in real-runtime tests are documented
under [`docs/development/`](docs/development/).

A Nix development environment is also available for contributors who prefer a
reproducible local toolchain, but Nix is not required as the canonical command
interface for the project.

See:

* [Local development](docs/development/local-development.md)
* [Testing strategy](docs/development/testing-strategy.md)
* [Fixture governance](docs/development/fixture-governance.md)

## Documentation

Current public Engine architecture, authoring, CLI, runtime, reference, and
adapter documentation is maintained in `parametron-docs`. Start at the
canonical [Engine documentation landing page](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/README.md).

Repository-local documentation — package layout, contributor workflow, local
testing, fixture governance, and historical implementation records — is
organized under [`docs/`](docs/) in this repository. Start at the
[documentation map](docs/development/docs-map.md) to locate the right
document.

## Contributing

See the organization-wide contribution guidelines for contribution workflow,
coding expectations, and community standards.

Repository-specific development documentation is available under
[`docs/development/`](docs/development/), starting with
[Local development](docs/development/local-development.md) and
[Testing strategy](docs/development/testing-strategy.md).

## License
This project is licensed under the [GNU Affero General Public License v3.0 (AGPL-3.0)](LICENSE) and is part of the Parametron ecosystem.

If you use this software over a network, you must make the source code available under the same license.
