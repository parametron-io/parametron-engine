# Adapter Architecture

Engine adapters translate planned engineering intent into runtime-facing
contracts. Engine owns input preparation, invocation policy, validation,
verification, and interpretation of evidence. The external `parametron-freecad`
runtime owns FreeCAD API interaction, document mutation, recompute, native save,
export, and native observation.

## Package ownership

| Package | Engine responsibility |
| --- | --- |
| `internal/engine/adapter` | Generic CAD-neutral `Adapter`, `CADRuntimeOrchestrator`, orchestration requests, and generic CSV/manifest helpers. |
| `internal/engine/adapter/freecad` | FreeCAD-specific manifest projection, attempt identity, working-copy preparation, runtime request adaptation, and result contracts. |
| `internal/engine/cadruntime` | Composition of FreeCAD preparation, observation requests, external invocation, evidence validation, and Engine verification. |
| `internal/engine/runtimecap` | External capability interface, executable selection, command construction, and context-aware subprocess invocation. |
| `internal/engine/verification` | Expected-versus-observed verification contracts and decisions. |

The import direction is `adapter/freecad -> adapter`. Generic contracts remain
independent of their FreeCAD-specific implementation. `cadruntime` composes
these packages; `adapter/freecad` contains Engine contract adaptation, not
FreeCAD-native execution.

`Adapter.Run(context.Context, planner.Step)` handles ordinary step dispatch.
The base `FreeCADAdapter` writes CSV and manifest files. The
`cadruntime.FreeCADRuntimeAdapter` wraps that adapter, implements
`CADRuntimeOrchestrator`, and exposes the verified-run snapshot consumed by the
executor. This wrapper performs one orchestration call per attempt.

## RunCADRuntime contract

The current planner step types are `WriteCSV`, `WriteExportManifest`, and
`RunCADRuntime`. `RunCADRuntimePayload` contains four logical fields:

| Field | Meaning |
| --- | --- |
| `ProductKey` | Product execution identity. |
| `Adapter` | Logical runtime adapter identity, `freecad` for FreeCAD. |
| `ManifestFilename` | Planned manifest filename. |
| `ResultFilename` | Required raw runtime result filename. |

Executable paths, workspace locations, environment, and verification policy are
not fields of this payload. A validated handoff binds it to the product CSV and
manifest. The executor dispatches it only through package-scoped execution.
See the [execution model](../architecture/execution-model.md) for handoff
validation, preflight, retries, and scheduling.

`CADRuntimeOrchestrationRequest` adds job ID, product key and directory, step ID,
one-based attempt number, CSV/manifest/runtime payloads, and operational
executable selection. It can also carry Engine-authoritative external target
identities for reference traversal. The logical plan and handoff do not store
the selected executable.

`runtimecap.ExecutableResolver` selects a configured external command or the
`parametron-freecad` wrapper from `PATH`. This selects the Engine-facing runtime
entry point. Discovery and use of the FreeCAD application belong to the external
runtime. `runtimecap.Capability.Invoke` receives explicit working-copy, manifest,
result, and output paths, plus optional observation and reference-traversal
request paths. The external command uses the file-based `execute` interface;
process results retain the command, stdout, and stderr.

## Attempt identity

`ComputeFreeCADRuntimeAttempt` validates the orchestration request and derives
an identity and layout without filesystem or process side effects.
`PrepareFreeCADRuntimeAttempt` uses that authoritative layout to prepare the
source and output directory.

The general `ComputeFreeCADRuntimeWorkingCopyLayout` helper also exposes layout
computation from product key, plan hash, and source path, normalizing relative
host paths to absolute paths. Shared preparation helpers copy the source,
prepare output directories, and validate the prepared document. Executor-driven
runtime execution uses the attempt-aware helpers above to isolate retries.

Attempt identity incorporates a versioned identity contract, job ID, product
key, step ID, adapter ID, plan hash, canonical host source path, manifest/result
filenames, and attempt number. Its directory name combines a sanitized product
prefix, a hash, and a zero-padded attempt suffix. Equivalent same-attempt inputs
reproduce the identity and layout; retries use different identities and isolated
workspaces. The product directory and selected executable are not attempt-hash
inputs.

This is operational identity. It is not added to plan, job, handoff, cache, or
normalized record identity contracts. Changing the operational product root
relocates the workspace without changing the logical plan or job identity.

## Prepared working copy

Normal FreeCAD planning uses the following attempt layout:

```text
<product-dir>/_working/<attempt-id>/
├── source/
│   └── <source-basename>
├── outputs/
│   ├── parametron.observed.json
│   └── <declared-output-files>
├── export_manifest_v1.json
├── parametron.verification.json
└── result.json
```

The tree shows path roles, not files guaranteed to exist after preparation.
Manifest and result paths use the validated handoff filenames; normal planner
filenames are `export_manifest_v1.json` and `result.json`. The manifest filename
remains the same for supported runtime manifest schema versions.

| Path or field | Meaning and materialization |
| --- | --- |
| `Manifest.Inputs.SourceModel` | Authoritative canonical absolute host source path selected by Engine inputs/project mapping. |
| `source/<source-basename>` | Runtime-facing relative source document; preparation copies the host source here. |
| `Layout.SourceDocumentPath` | Absolute path of the prepared source file. |
| `outputs/` | Prepared output directory; planned export paths are runtime-relative paths below it. |
| `export_manifest_v1.json` | Reserved attempt-root manifest path, written by Engine manifest materialization. |
| `parametron.verification.json` | Attempt-root observation request written by Engine CAD orchestration after deriving the verification contract from the prepared source. |
| `result.json` | Reserved attempt-root raw result path, written by the external runtime. |
| `outputs/parametron.observed.json` | Raw runtime observation evidence read by Engine. |

Pure layout computation creates nothing. Source preparation creates a regular
source copy and the output directory; it does not create the reserved manifest
or result files or invoke CAD. The original host source is preserved. Copying
uses a temporary file and atomic replacement, with temporary-file cleanup on
failure. Native manifest materialization sets `sourceDocument` to the relative
`source/<source-basename>` path. Equivalent same-attempt manifest and observation
request writes are deterministic and atomically replace regular destination
files while preserving unrelated output files.

The observation request lives at the working-copy root, not in `outputs/`.
`working_copy_path` identifies the attempt root passed to the runtime;
`working_copy_sha256` is derived from the exact prepared source file's bytes.
The root, prepared-source path, and source checksum are distinct evidence.
The request is not a verification-result file: Engine comparison produces a
separate in-memory verification result.

## Path confinement and freshness

Attempt preparation requires canonical absolute product and host source paths.
The attempt ID is a single path segment below `_working/`; source, manifest,
result, and output paths are confined to the attempt root. Manifest/result
filenames are validated logical filenames, and runtime-facing source/output
paths are canonical relative paths.

Before process invocation, Engine rejects unsafe owned output objects and
symlinked working-copy roots or intermediate components. Existing regular files
at contract-owned result, observed, and declared artifact paths are removed so
that an attempt cannot succeed using stale evidence. Unrelated files and
prepared inputs are preserved. After invocation begins, partial raw evidence
is retained on failure.

Outcome consumption correlates canonical absolute working-copy, output, and
result paths against the authoritative attempt layout. Validated artifacts must
remain confined to the working copy, and each absolute artifact path must equal
the attempt root joined with its declared canonical relative path. These checks
establish path and attempt correlation; Engine verification separately decides
whether returned observations satisfy expected engineering state.

The [execution runtime](../engine/execution-runtime.md) owns the detailed
invoke, validate, verify, and consume lifecycle. The
[record contracts](../reference/record-contracts.md) describe normalization and
preservation of raw evidence. A prepared source document remains runtime
workspace state rather than a declared export in the handoff artifact inventory.
