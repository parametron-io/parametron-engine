# Documentation Map

This document maps all public and internal documentation across the Parametron Engine repository.

## Architecture

| Topic | File |
|:------|:-----|
| High-level pipeline, core objectives, and architectural ownership | [docs/architecture/system-overview.md](../architecture/system-overview.md) |
| Execution runtime: jobs, handoff packages, scheduling, and executor flow | [docs/architecture/execution-model.md](../architecture/execution-model.md) |
| Repository layout, package responsibilities, and directory structure | [docs/architecture/repository-structure.md](../architecture/repository-structure.md) |

## Authoring and DSL

| Topic | File |
|:------|:-----|
| DSL structure, version directives, products, parameters, and comments | [docs/authoring/dsl-overview.md](../authoring/dsl-overview.md) |
| Operator syntax, precedence, built-in functions, types, and grammar | [docs/authoring/dsl-grammar.md](../authoring/dsl-grammar.md) |
| Evaluation model, identifier resolution, type rules, and profile settings | [docs/authoring/dsl-semantics.md](../authoring/dsl-semantics.md) |
| IR structure, planning pipeline, and authoring execution plan boundary | [docs/authoring/ir-and-planning.md](../authoring/ir-and-planning.md) |
| Capture-first CAD parameter contracts and validation | [docs/authoring/cad-contract.md](../authoring/cad-contract.md) |
| `parametron.project.json` project format and resource mapping | [docs/authoring/project-mapping.md](../authoring/project-mapping.md) |

## CLI

| Topic | File |
|:------|:-----|
| Command families, harness routing, and CLI quickstart | [docs/cli/command-families.md](../cli/command-families.md) |
| `parametron validate` — Syntax and semantic validation | [docs/cli/validate.md](../cli/validate.md) |
| `parametron simulate` — Plan inspection and simulation | [docs/cli/simulate.md](../cli/simulate.md) |
| `parametron sweep` — Parameter variation and matrix generation | [docs/cli/sweep.md](../cli/sweep.md) |
| `parametron snapshot` — Baseline capture and manifest comparison | [docs/cli/snapshot.md](../cli/snapshot.md) |
| `parametron diff` — Structural and execution diffing | [docs/cli/diff.md](../cli/diff.md) |
| Output directory layout, cache flags, overrides, and CLI runtime behavior | [docs/cli/runtime-behavior.md](../cli/runtime-behavior.md) |

## Engine

| Topic | File |
|:------|:-----|
| HTTP API overview, endpoints, and server configuration | [docs/engine/api-overview.md](../engine/api-overview.md) |
| `POST /job` — Job submission payload, validation, and canonicalization | [docs/engine/job-submission.md](../engine/job-submission.md) |
| Job lifecycle states, transitions, and status inspection | [docs/engine/job-lifecycle.md](../engine/job-lifecycle.md) |
| `GET /job/{id}/artifacts` — Job-scoped artifact listing | [docs/engine/job-artifacts.md](../engine/job-artifacts.md) |
| `GET /artifacts` & `GET /artifacts/{id}` — Artifact listing and file streaming | [docs/engine/artifact-serving.md](../engine/artifact-serving.md) |
| CAD runtime processing pipeline, attempt isolation, verification, and cache | [docs/engine/execution-runtime.md](../engine/execution-runtime.md) |
| `report.json` execution summary schema and determinism | [docs/engine/reporting.md](../engine/reporting.md) |
| JSON table resource format, validation rules, fingerprinting, and lookup | [docs/engine/json-table-resources.md](../engine/json-table-resources.md) |

## Adapters

| Topic | File |
|:------|:-----|
| Generic adapter interface, step payloads, and runtime dispatch | [docs/adapters/adapter-architecture.md](../adapters/adapter-architecture.md) |

## Reference Contracts

| Topic | File |
|:------|:-----|
| Normalized record models, provenance, identity, and package layout | [docs/reference/record-contracts.md](../reference/record-contracts.md) |
| Target-action intent, mutation collections, and suppression contracts | [docs/reference/target-action-contract.md](../reference/target-action-contract.md) |

## Development and Testing

| Topic | File |
|:------|:-----|
| Local build setup, running commands, and smoke tests | [docs/development/local-development.md](local-development.md) |
| Testing philosophy, architectural test layers, and verification commands | [docs/development/testing-strategy.md](testing-strategy.md) |
| Public test matrix across packages and subsystems | [docs/test-matrix.md](../test-matrix.md) |
| Extension guide: adding DSL syntax, step types, and CAD adapters | [docs/development/extension-guide.md](extension-guide.md) |
| DSL fixture governance, categorisation, and contribution rules | [docs/development/fixture-governance.md](fixture-governance.md) |

## Legacy Documentation

| Topic | File |
|:------|:-----|
| Historical FreeCAD manifest and runner design rationale | [docs/legacy/freecad-manifest-and-runner-history.md](../legacy/freecad-manifest-and-runner-history.md) |
