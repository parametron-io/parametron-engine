# Repository Structure

The repository separates authoring, Engine execution, and shared input support.
FreeCAD-native execution lives in the external `parametron-freecad` repository.

## Entry points and supporting areas

| Path | Responsibility |
| --- | --- |
| `cmd/parametron/` | Local CLI execution, validation, plan inspection, simulation, and supporting command families. |
| `cmd/parametron-engine/` | HTTP server entry point for the Engine API. |
| `cmd/parametron-engine-smoke/` | HTTP API smoke-test client. |
| `internal/authoring/` | DSL, validation, IR, expression evaluation, and planning. |
| `internal/engine/` | Execution contracts, orchestration, verification, artifacts, and records. |
| `internal/shared/` | Configuration, project mapping/locking, and security helpers. |
| `internal/testsupport/` | Shared Go test fixtures and helpers. |
| `testdata/` | DSL and project fixtures, including FreeCAD project inputs. |
| `scripts/` | External-runtime integration proof tooling and its Python tests. |
| `docs/` | Repository-local architecture, development, and historical documentation. |

Go dependencies are declared in `go.mod` and `go.sum`. The Nix flake and `.envrc`
provide optional development-environment provisioning.

## Authoring packages

Within `internal/authoring/`, `dsl` owns parsing and validation; `ir` owns the
serializable intermediate representation, AST conversion, DSL generation, and
IR planning. `planner` owns execution-plan construction, canonical payload
ordering, plan identity, and product output naming. `runtime` evaluates DSL
expressions and functions; it is unrelated to the external CAD process boundary.

## Execution and CAD boundaries

Within `internal/engine/`:

| Package area | Responsibility |
| --- | --- |
| `job`, `handoff` | Product execution units and validated package contracts. |
| `scheduler`, `executor` | Concurrent product dispatch, ordered step execution, attempts, and outcome consumption. |
| `adapter` | Generic CAD-neutral adapter and orchestration contracts, with generic file materialization helpers. |
| `adapter/freecad` | FreeCAD-specific Engine manifest/result adaptation and working-copy preparation. |
| `cadruntime` | Engine-side composition of preparation, external invocation, evidence validation, and verification. |
| `runtimecap` | External runtime executable selection and process invocation. |
| `verification`, `observed` | Engine verification and returned observation contracts/loading. |
| `cad`, `semantic`, `semanticmap` | CAD and semantic models, identity linkage, and semantic-to-CAD intent mapping. |
| `projectinput`, `table`, `tableloader` | Project resource preparation and table inputs. |
| `api`, `jobstatus` | HTTP execution services and job lifecycle state. |

`adapter/freecad` depends on `adapter`; the generic adapter package does not
depend on its FreeCAD-specific implementation. `cadruntime` composes these
Engine boundaries with `runtimecap` and verification. Native document mutation,
recompute, save, export, and observation remain external-runtime responsibilities.

## Outputs and records

`artifact`, `cache`, `metadata`, and `report` support execution output handling,
reuse, and operational reporting. `recordcontract` defines normalized record
contracts; `recordmap` interprets accepted execution evidence into records;
`recordemit` connects emission to execution outputs; `recordpackage` writes local
packages. Raw runtime evidence and Engine-normalized records remain distinct.

## Further reading

Current public Engine architecture, authoring, CLI, runtime, reference, and
adapter documentation is maintained in `parametron-docs`. See the canonical
[Engine documentation landing page](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/README.md)
for system overview, execution model, adapter architecture, and record
contracts.

Repository-local development, testing, and maintenance documentation remains
under [`docs/development/`](../development/) in this repository.
