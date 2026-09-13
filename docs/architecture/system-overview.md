# System Overview

Parametron Engine owns deterministic engineering authoring and execution. It
parses and validates engineering intent, builds execution plans, schedules work,
invokes external runtime capabilities, and interprets returned evidence through
Engine-owned verification and normalized records.

## Authoring and execution

```text
DSL -> semantic validation -> Engine-owned IR -> deterministic planning
                                                   |
                                                   v
                          plan / handoff package -> scheduler / executor
                                                   |
                                                   v
                                 Engine adapter / runtime contracts
                                                   |
                                                   v
                                      external runtime capability
                                                   |
                                                   v
                                      returned runtime evidence
                                                   |
                                                   v
                                Engine verification / normalization
```

The IR provides a serializable authoring representation and planning entry point.
The normal CLI also plans directly from the validated AST; it does not require an
IR serialization round trip. Both authoring representations and the execution
plan belong to Engine.

The CLI and HTTP API expose local execution. Engine owns execution contracts,
retry decisions, evidence validation, artifact acceptance, and local record
packages. It does not own durable workflow/storage policy.

## Runtime ownership

External runtimes perform application-native work behind Engine contracts.
For FreeCAD, `parametron-freecad` owns FreeCAD API interaction, CAD document
mutation, recompute, native save, export, and native observation. Engine prepares
execution inputs and working copies, invokes that external capability, and
verifies returned evidence. FreeCAD-specific contract adaptation in Engine is
separate from FreeCAD-native implementation.

## Deterministic behavior

Engine defines canonical ordering and serialization for its identity surfaces.
Equivalent planning inputs produce stable plans and hashes; product jobs retain
planned step order, and scheduler results are collected in product order even
when execution finishes concurrently. Runtime attempts have explicit identities
and isolated workspaces.

Determinism applies to the defined Engine-owned representations. Operational
timestamps, absolute workspace paths, and raw runtime evidence can vary between
runs. Preserved raw evidence and normalized records serve different purposes;
raw evidence is not replaced by normalization.

## Further reading

- [Execution model](execution-model.md): jobs, handoff, scheduling, and execution.
- [Repository structure](repository-structure.md): architectural navigation.
- [Adapter architecture](../adapters/adapter-architecture.md): runtime contracts
  and attempt workspaces.
- [IR and planning](../authoring/ir-and-planning.md): authoring representations.
- [Execution runtime](../engine/execution-runtime.md): CAD-runtime processing
  and runtime output surfaces.
- [Record contracts](../reference/record-contracts.md): normalized records and
  raw-evidence packaging.
