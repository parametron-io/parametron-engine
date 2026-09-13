# Execution Runtime

This document describes the Engine-owned CAD runtime processing pipeline: attempt
isolation, external runtime invocation, evidence validation, verification,
artifact registration, run output generation, normalized record package emission,
and caching.

For scheduler and executor concurrency, job splitting, and handoff mechanics,
see [Execution Model](../architecture/execution-model.md). For normalized record
schema specifications, see [Record Contracts](../reference/record-contracts.md).

## CAD Runtime Processing Pipeline

```text
Execution Plan (StepRunCADRuntime)
              |
              v
     handoff.Package -> Executor (prepareCADRuntimeDispatch)
                                |
                                v
               Deterministic Attempt Layout & Identity
                                |
                                v
              Request Materialization (Manifest, Verification, Traversal)
                                |
                                v
                Pre-Invocation Freshness Checks
                                |
                                v
             External Runtime Invocation (runtimecap)
                                |
                                v
           Raw Result & Evidence Validation (cadruntime)
                                |
                                v
              Engine Verification (verification.Verify)
                                |
                                v
             Outcome Consumption & Retry Decision
                                |
        +-----------------------+-----------------------+
        | Passed                                        | Failed / Non-retryable
        v                                               v
Atomic Batch Registration                    Structured Error Reporting
(result.json, execution & verified)          (no aligned artifacts)
        |                                               |
        +-----------------------+-----------------------+
                                |
                                v
                Run Outputs (report.json, metadata.json)
                                |
                                v
           Local Record Package Emission (recordemit)
```

## Architectural Composition

CAD-runtime execution is composed of four decoupled layers:

1. **CAD Adapter (`internal/engine/adapter/freecad`)**:
   Provides target-specific path templates and native manifest projections.
2. **Engine Runtime Wrapper (`internal/engine/cadruntime`)**:
   Owns attempt directory management, request materialization, external invocation
   orchestration, raw evidence intake validation, and verification execution.
3. **Capability Boundary (`internal/engine/runtimecap`)**:
   Provides executable selection (`ExecutableResolver`) and operating system
   process execution (`Capability`).
4. **Executor (`internal/engine/executor`)**:
   Manages step lifecycle, invocation retries, outcome consumption, and completion
   artifact registration.

## Attempt Identity and Layout

Every CAD runtime invocation executes inside an isolated attempt workspace:

- **Attempt Identity**: Derived deterministically from contract version, `JobID`,
  `ProductKey`, `StepID`, adapter name, `PlanHash`, source model path, declared
  output filenames, and attempt index (1-based: `1`, `2`, `3`).
- **Workspace Isolation**: Located at `<productDir>/_working/<attemptID>/`.
  - `source/`: Contains an atomic copy of the source CAD document.
  - `outputs/`: Dedicated directory for external runtime outputs.
  - `export_manifest_v1.json`: Materialized native manifest.
  - `parametron.verification.json`: Materialized verification request contract.
  - `parametron.reference-traversal-request.json`: Reference traversal configuration.

Attempts are completely isolated: retries use distinct attempt IDs and directories,
preventing cross-attempt contamination.

## Request Materialization and Freshness

Before invoking the external CAD runtime:

1. **Native Manifest**: The execution manifest is projected into the format
   required by the runtime and written to `export_manifest_v1.json`.
2. **Verification Contract**: Engine derives the expected metadata, parameter,
   and reference constraints from the manifest intent and prepared source model,
   writing `parametron.verification.json`.
3. **Reference Traversal Request**: If reference traversal is configured,
   `parametron.reference-traversal-request.json` is materialized.
4. **Pre-Invocation Freshness**: Stale contract-owned outputs (`result.json`,
   `parametron.observed.json`, `parametron.reference-traversal.json`, and all
   declared artifact paths) are checked. Regular files from prior runs are
   removed; symlinks or non-regular files cause preflight rejection.

## External Runtime Invocation

The external CAD runtime executable (e.g. `parametron-freecad`) is invoked
directly via `runtimecap.Capability`:

```bash
<runtime-binary> execute \
  --working-copy <WorkingCopyDir> \
  --manifest <ManifestPath> \
  --result <ResultPath> \
  --output-dir <OutputDir> \
  --observation-request <ObservationRequestPath> \
  --reference-traversal-request <ReferenceTraversalRequestPath>
```

- Invoked directly without an intermediate shell.
- Process environment is filtered; timeouts and context cancellations propagate
  directly to the subprocess.
- Process exit code, stdout, and stderr are captured.

## Raw Result and Evidence Intake Validation

Upon subprocess completion, Engine validates raw runtime evidence:

1. **`result.json` Validation**:
   - For exit code 0: `result.json` must exist with `status: "succeeded"` and
     valid artifact declarations.
   - For non-zero exit: `result.json` is inspected for structured failure details.
2. **Artifact Declaration Correlation**:
   - Result artifact declarations must match manifest output declarations exactly
     in count, declaration order, ID, format (`step`, `csv`, `pdf`), and path.
   - Each artifact path must be a regular file strictly contained within
     `OutputDir`.
3. **Observed CAD State Intake**:
   - `<OutputDir>/parametron.observed.json` is loaded and structurally validated.
   - Observed working copy path and SHA-256 are verified against the prepared
     source model.
4. **Reference Traversal Intake**:
   - `<OutputDir>/parametron.reference-traversal.json` is captured if present.

## Engine-Owned Verification

After raw output validation succeeds, Engine performs an in-memory verification
comparison (`verification.Verify`):

- **Contract vs Observed**: Compares expected values against observed values
  across four categories: `parameters`, `metadata`, `references`, and `components`.
- **Decision Authority**: The verification decision belongs entirely to Engine.
  The external runtime cannot approve or verify its own output.
- **Deterministic Classification**: Mismatches produce typed failure classes:
  - `metadata_mismatch`
  - `reference_mismatch`
  - `parameter_mismatch`
  - `required_observation_missing`
  - `internal_verification_error`

## Outcome Consumption and Retries

The Executor correlates the attempt outcome against the active job request:

- **Success**: Succeeded runtime status + passed Engine verification.
- **Verification Failure**: Succeeded runtime status + failed verification.
  Treated as a terminal failure (not retried).
- **Runtime Failure**: Subprocess failure or runtime-reported error.
  Eligible for retry if within max retry limit.
- **Retry Ownership**: The Executor is the sole retry authority. Retries
  advance the attempt counter and execute in a fresh attempt workspace.

## Completion Artifacts and Atomic Registration

When execution succeeds with a passed verification outcome:

1. `result.json` is registered under class `execution_output` (`type: json`).
2. Declared output files (`step`, `csv`, `pdf`) are registered under class
   `execution_output`.
3. Those same declared output files are additionally promoted to class
   `verified_artifact`.
4. Batch registration is atomic: all records are accepted together or none become
   visible.

Raw runtime evidence (native manifests, verification requests, observed JSON,
prepared source files) is excluded from registered artifact listings.

## Run-Level Outputs

A completed execution writes up to four run-level output files:

| File | Purpose |
|------|---------|
| `report.json` | Run outcome, step timing, artifact summary, and structured errors. |
| `metadata.json` | Toolchain versions, profile settings, product summaries, and table fingerprints. |
| `manifest.json` | Deterministic artifact inventory from the artifact store. |
| `export_manifest_v1.json` | Active per-product adapter manifest. |

## Normalized Record Package Emission

At the conclusion of a normal run, `recordemit.EmitRunPackage` builds a local
record package (`<runRoot>/parametron-record-package/`):

### Record Emission vs Mapper Availability

| Record Family | Contract Defined | Mapper Implemented | Emitted on Normal Run |
|---------------|:----------------:|:------------------:|:---------------------:|
| Execution | Yes | Yes (`recordmap.MapReport`) | Yes |
| Failure | Yes | Yes (`recordmap.MapReport`) | Yes (on failed runs) |
| Reference | Yes | Yes (`recordmap.MapReferenceTraversal`) | Yes (when traversal evidence exists) |
| Artifact | Yes | Yes (`recordmap.MapArtifact`) | Available via mapper |
| Observation | Yes | Yes (`recordmap.MapObserved`) | Available via mapper |
| Verification | Yes | Yes (`recordmap.MapVerification`) | Available via mapper |

### Raw Evidence Preservation

The emitted record package preserves raw evidence files under `raw/`:
- `raw/engine/report.json`
- `raw/engine/metadata.json`
- `raw/engine/manifest.json`
- `raw/engine/parametron.observed.json`
- `raw/engine/parametron.verification.json`
- `raw/engine/result.json`
- `raw/runtime/reference-traversal.json` (if present)

Raw evidence preserves operational paths and timestamps, while normalized records
under `records/` maintain deterministic byte stability.

## Layered Cache

The cache consists of three independent layers stored in `.cache/`:

1. **Geometry Layer (`.cache/geometry/<key>.done`)**:
   Skips CAD execution when the plan hash, model hash, and table fingerprints match.
2. **Artifact Layer (`.cache/artifact/<key>.done`)**:
   Enables content-addressed artifact reuse via SHA-256 checksums.
3. **Metadata Layer (`.cache/metadata/<key>.done`)**:
   Tracks run metadata recording.

Cache keys incorporate `PlanHash`, model resource hashes, profile overrides, and
semantic table fingerprints.

## Error Taxonomy

| Error Type | Package | Description |
|------------|---------|-------------|
| `CADRuntimeDispatchError` | `executor` | Preflight, resolver, or orchestration failure before process start. |
| `FreeCADRuntimeRunError` | `cadruntime` | Subprocess invocation, output preparation, or raw result intake failure. |
| `FreeCADRuntimeVerificationError` | `cadruntime` | In-memory verification mismatch or result inconsistency. |
| `CADRuntimeConsumptionError` | `executor` | Outcome validation or correlation error during Executor intake. |
| `ExecutionError` | `executor` | Step-level execution failure wrapper containing retry and timeout state. |

## Related Documents

- [Adapter Architecture](../adapters/adapter-architecture.md) — Adapter contracts and working copy boundaries.
- [Execution Model](../architecture/execution-model.md) — Scheduler, executor, and worker pool.
- [Reporting](reporting.md) — `report.json` schema and contract.
- [Record Contracts](../reference/record-contracts.md) — Normalized record models and packaging rules.
