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
`records/artifacts/<identityId>/parametron.artifact-record.json`, ordered by
record identity. Other record families remain singular. Duplicate artifact
identities or resolved record paths are rejected.

Typical raw evidence includes runtime results, metadata, verification material,
observations, manifests, and runtime handoff data. Raw evidence is preserved as
evidence and is not treated as equivalent to normalized Engine records.
Observation and verification records come from Engine-owned typed
interpretation; raw bytes remain independently preserved for provenance.

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
