# Documentation Map

This document maps documentation across the Parametron Engine repository and
clarifies where each kind of Engine documentation is owned.

Documentation follows one ownership rule:

> one fact -> one canonical documentation owner -> references elsewhere

## Canonical current/public Engine documentation (`parametron-docs`)

Current, public-facing explanatory documentation for Engine architecture,
authoring, CLI, runtime, reference contracts, and adapters is maintained
centrally in `parametron-docs`, alongside documentation for other Parametron
repositories. `parametron-engine` remains the source of truth for the
underlying implementation, schemas, and tests.

| Topic | Canonical location |
|:------|:--------------------|
| Engine documentation landing page | [docs/engine/README.md](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/README.md) |
| Architecture: system overview, execution model | [docs/engine/architecture/](https://github.com/parametron-io/parametron-docs/tree/main/docs/engine/architecture) |
| Authoring: DSL overview, grammar, semantics, IR and planning, project mapping, CAD contract | [docs/engine/authoring/](https://github.com/parametron-io/parametron-docs/tree/main/docs/engine/authoring) |
| CLI: command families, validate, simulate, sweep, snapshot, diff, runtime behavior | [docs/engine/cli/](https://github.com/parametron-io/parametron-docs/tree/main/docs/engine/cli) |
| Runtime: API overview, job submission/lifecycle/artifacts, execution runtime, reporting | [docs/engine/runtime/](https://github.com/parametron-io/parametron-docs/tree/main/docs/engine/runtime) |
| Reference: record contracts, target-action contract, JSON table resources | [docs/engine/reference/](https://github.com/parametron-io/parametron-docs/tree/main/docs/engine/reference) |
| Adapters: generic adapter domain and FreeCAD adapter architecture/runtime/contracts | [docs/engine/adapters/](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/adapters/README.md) |

## Repository-local Engine documentation (`parametron-engine`)

Documentation that is genuinely specific to this repository — package layout,
contributor workflow, local testing procedures, fixture governance, and
historical implementation records — remains here.

| Topic | File |
|:------|:-----|
| Repository layout, package responsibilities, and directory structure | [docs/architecture/repository-structure.md](../architecture/repository-structure.md) |
| Local build setup, running commands, and smoke tests | [docs/development/local-development.md](local-development.md) |
| Testing philosophy, architectural test layers, and verification commands | [docs/development/testing-strategy.md](testing-strategy.md) |
| Extension guide: adding DSL syntax, step types, and CAD adapters | [docs/development/extension-guide.md](extension-guide.md) |
| DSL fixture governance, categorisation, and contribution rules | [docs/development/fixture-governance.md](fixture-governance.md) |
| Public test matrix across packages and subsystems | [docs/test-matrix.md](../test-matrix.md) |
| Historical FreeCAD manifest and runner design rationale | [docs/legacy/freecad-manifest-and-runner-history.md](../legacy/freecad-manifest-and-runner-history.md) |

Centralizing current/public documentation does not move implementation
ownership. Go implementation, CLI implementation, executable schemas,
validators, record contract implementation, planner/runtime implementation,
tests, and fixtures remain owned by `parametron-engine`.
