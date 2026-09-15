# Parametron Engine Agent Guide

## Parametron

Parametron is an early-stage engineering automation project.

The current public engineering system consists of:

- `parametron-engine` — deterministic engineering authoring, planning,
  execution contracts, verification, and normalized engineering records.
- `parametron-freecad` — FreeCAD-native execution and raw CAD evidence behind
  Engine-owned contracts.

Engine decides what engineering work should happen and how returned evidence is
interpreted. FreeCAD performs the CAD-native operations required by those
contracts.

## This repository

`parametron-engine` is the deterministic engineering execution engine for
Parametron.

Its implemented capabilities include:

- Parametron DSL parsing, validation, and semantic analysis
- Engine-owned intermediate representation and deterministic planning
- deterministic plan and execution identity
- scheduling, caching, and local execution
- simulation and plan inspection
- adapter-neutral CAD runtime contracts
- aligned external FreeCAD runtime invocation
- runtime evidence validation and verification
- normalized execution, artifact, observation, reference, failure, and
  verification record contracts
- local record-package generation
- raw runtime-evidence preservation
- target-action authoring for suppression, visibility, and deletion intent
- CLI and HTTP API foundations

Engine owns engineering intent, deterministic planning, runtime orchestration
contracts, verification decisions, normalized records, and interpretation of
returned runtime evidence.

It does not own CAD-native document mutation, recompute, save, export,
application-specific observation, or other FreeCAD internals. Those
responsibilities belong to `parametron-freecad`.

Before changing behavior, inspect the relevant source, tests, and permanent
documentation. Do not describe unsupported behavior as implemented.

## Repository boundaries

Keep Engine independent from CAD implementation details.

The intended boundary is:

```text
Engine
  adapter/runtime contract
        |
        v
external runtime
        |
        v
parametron-freecad
````

Important ownership rules:

* `internal/engine/adapter` contains generic Engine adapter contracts.
* `internal/engine/adapter/freecad` contains FreeCAD-specific Engine contract
  adaptation only.
* `internal/engine/cadruntime` owns Engine-side CAD runtime orchestration.
* `internal/engine/runtimecap` owns the external runtime capability boundary.
* `parametron-freecad` owns actual FreeCAD API interaction and native CAD
  execution.
* Engine owns verification and normalized interpretation of returned evidence.

See the canonical
[system overview](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/architecture/system-overview.md)
and
[adapter architecture](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/adapters/README.md)
documentation in `parametron-docs` for the detailed, current description of
this boundary.

Do not introduce FreeCAD Python implementation, document mutation, recompute,
save, export, FreeCAD executable discovery, or native observation logic into
Engine.

Do not delete architectural foundations merely because they currently have few
or no production callers. In particular, Engine-owned authoring and IR
boundaries may intentionally exist ahead of broader integration.

Respect package direction. Generic packages must not depend on their
runtime-specific implementations merely for convenience.

## Workflow

Work is issue-driven and phase-oriented.

Keep every change bounded to the requested objective. Respect repository
ownership: work belonging to another repository is an external dependency, not
an excuse to implement that responsibility here.

Planning and actionable work live in GitHub Issues. Do not recreate
repository-local roadmap, checklist, progress-journal, or status-tracking files.

Prefer focused implementation, focused verification, documentation alignment,
and explicit closure over broad opportunistic cleanup.

Do not silently turn a documentation task into source refactoring, or a focused
source task into architecture redesign.

## Verification standard

Implementation alone is not completion.

A change is complete only when the relevant behavior is implemented,
appropriate tests pass, deterministic expectations are demonstrated where
applicable, and affected documentation is synchronized.

Use focused tests first and broader validation when the scope requires it.

Canonical Go validation commands are:

```bash
go test ./...
```

```bash
go vet ./...
```

Run a specific package when appropriate:

```bash
go test ./internal/engine/recordmap/...
```

Prefer the narrowest relevant package test while iterating, then run the broader
suite before closure when the change can affect shared behavior.

The repository may provide optional development environments or wrappers, but
project-native commands are the canonical verification interface.

Real external-runtime integration tests may be opt-in. Do not make real FreeCAD
availability a requirement for the default unit-test suite unless a task
explicitly changes that policy.

See [testing strategy](docs/development/testing-strategy.md) for architectural
test layers and the full verification command set.

Failure behavior is part of the contract: invalid inputs should fail
deterministically, at the correct ownership boundary, with stable
classification where the repository defines one.

Never claim determinism without repeatable evidence.

## Determinism

Determinism is a core Engine property.

When changing planning, identities, manifests, records, packages, cache keys, or
normalization behavior:

* inspect canonical ordering rules
* preserve deterministic serialization where defined
* consider repeated equivalent runs
* verify stable identity surfaces
* avoid dependence on map iteration order, filesystem discovery order, or other
  incidental runtime ordering

Raw runtime evidence may intentionally preserve operational timestamps, paths,
or other run-specific material. Do not confuse raw-evidence byte equality with
Engine-owned normalized determinism.

## Phases

A phase is a repository-owned, bounded development objective.

A valid phase has:

* a clear goal
* explicit scope
* known dependencies
* verifiable exit criteria

Phases must be bounded, deterministic, independently understandable, and
objectively closable.

Phase identifiers belong to their owning repository. Do not redefine another
repository's phase or use phase numbers as global ordering.

Do not preserve private development chronology in user-facing errors,
documentation, or public API names unless that chronology is itself part of a
stable contract.

Phase titles describe outcomes rather than vague activity.

## Naming

Use explicit, descriptive names and established Parametron terminology.

Prefer clarity over brevity. Do not introduce synonyms when a canonical term
already exists.

In this repository:

* `authoring` means DSL, semantic analysis, IR, and planning inputs
* `execution` means Engine-controlled execution and orchestration
* `runtime` means an external capability invoked behind Engine contracts
* `observation` means runtime state returned as evidence
* `verification` means Engine-owned expected-versus-observed decisions
* `validation` means syntax, semantic, schema, or contract checking
* `adapter` means the Engine boundary adapting generic execution intent to a
  runtime-specific contract
* `raw evidence` means preserved runtime or operational evidence that has not
  become an Engine-normalized record
* `record` means an Engine-owned normalized representation
* `reference traversal` means the FreeCAD-owned runtime process or capability
  that discovers CAD references and emits traversal evidence
* `raw traversal evidence` means serialized raw evidence produced by reference
  traversal, currently `parametron.reference-traversal.json`
* `reference record` means Engine-owned normalized reference information
  derived from accepted evidence

Do not use raw runtime evidence and normalized records interchangeably.

Use lowercase hyphenated names for multi-word documentation files.

Versioned JSON contracts should follow established repository naming and schema
conventions; do not invent parallel formats casually.

## Records and evidence

Engine owns normalized record contracts and package interpretation.

When changing record emission:

* preserve record-family ownership
* preserve deterministic identity and normalization
* preserve raw evidence rather than silently replacing it with normalized data
* keep raw and normalized package paths distinct
* do not move runtime-native semantics into normalized records without an
  explicit contract decision

Existing mapper support does not automatically imply that the same record family
is emitted by every normal-run path. Inspect actual emission wiring before
claiming support.

See the canonical
[record contracts](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/reference/record-contracts.md)
documentation in `parametron-docs` for record family definitions and package
layout.

## Compatibility and legacy surfaces

Do not preserve or expand compatibility behavior merely because tests exist for
it.

First determine whether the behavior is part of the current public contract.

Likewise, do not remove an architectural boundary merely because it has few
callers.

When encountering apparently legacy code:

1. identify current callers
2. inspect tests
3. inspect current documentation
4. determine whether the behavior is contractual, transitional, or dead
5. report ambiguity before deleting or redesigning it

Do not reintroduce retired execution models, bridges, shims, or embedded
runtime implementations without an explicit task requiring them.

## Documentation

Public documentation describes the current implemented Engine.

It must not be used as a private development journal.

Do not add repository-local roadmap, task-list, status-history, or private
planning documents.

Avoid private phase/task chronology in public prose unless it is necessary to
explain a stable compatibility contract.

Canonical documentation commands should be project-native. Optional Nix or
other environment tooling may be documented as contributor convenience, but
must not replace the underlying Go or Python command as the public interface.

When source, tests, and documentation disagree, investigate the discrepancy
instead of choosing whichever version is convenient.

Start from [the documentation map](docs/development/docs-map.md) to locate the
canonical document for a given subsystem before writing new prose.

## Commits

Use structured Parametron commit messages:

`<type>(<scope>): <specific summary>`

Common types are `feat`, `fix`, `docs`, `test`, `refactor`, and `chore`.

Use one clear scope whenever possible. The subject should describe the concrete
effect of the change, not vague activity such as "update tests" or "fix stuff".

When a commit belongs to an issue, reference that issue. Use a closing keyword
only when the work is actually complete and its verification and documentation
requirements are satisfied.

Do not create commits unless explicitly requested.

Do not push unless explicitly requested.

## Agent behavior

Keep changes narrow. Do not modify unrelated files or silently expand scope.

Treat source and tests as authoritative evidence for implemented behavior, and
permanent documentation as the public contract surface. If they disagree,
investigate and report the inconsistency instead of guessing.

Do not infer that unused code is obsolete solely from caller count.

Preserve backward compatibility and versioned contracts when they are current
public contracts unless the task explicitly requires a breaking change.

Do not invent future architecture, product behavior, or roadmap commitments to
fill gaps in the current implementation.

Before finishing:

* review the complete diff
* confirm changed paths match the requested scope
* run relevant focused validation
* run broader validation when warranted
* report what changed
* report what was verified
* disclose remaining limitations, skipped validation, or blockers
* state whether any commit or push was performed
