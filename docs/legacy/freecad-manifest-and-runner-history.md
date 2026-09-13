Historical design note — not current implementation documentation.

# FreeCAD manifest and runner history

Engine previously contained a FreeCAD runner and a separate observation bridge.
A per-product manifest connected planning to that runner: it separated parameter
assignment intent, assembly and part mutations, and declared exports. The runner
validated intent before applying CAD operations to a working copy, leaving the
source document intact. Raw execution results and observed facts were separate
from Engine verification decisions.

That separation explains vocabulary still present in Engine contracts:

- `parameterAssignments` distinguishes executable scalar assignments from
  resolved values and exported artifacts.
- `assemblyMutations` and `partMutations` distinguish mutation destinations.
  Parameter, property, and suppression families informed the internal model;
  the current runtime projection has its own closed family contract.
- Working-copy isolation, explicit output declarations, and raw result evidence
  let Engine correlate execution with its planned intent before accepting outputs.
- Observation supplies facts; Engine compares those facts and decides verification.

CAD-native execution and observation were externalized, and the in-Engine
runner/bridge architecture was retired as the runtime integration path. Current
code must not use that historical architecture. Its invocation protocol, schema
details, stage ordering, and setup recipes are not current implementation guidance.

The current source of truth is `internal/engine/adapter/freecad`, composed through
`internal/engine/cadruntime` and the Engine-owned `internal/engine/runtimecap`
external-runtime contract. See [adapter architecture](../adapters/adapter-architecture.md),
[execution runtime](../engine/execution-runtime.md), and the
[target-action contract](../reference/target-action-contract.md) for current boundaries.
