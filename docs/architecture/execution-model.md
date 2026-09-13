# Execution Model

Engine executes an ordered `planner.ExecutionPlan` through product jobs. The
scheduler owns concurrent dispatch and result ordering; the executor owns
sequential steps, attempts, and execution state for each job.

```text
ExecutionPlan -> job.SplitPlan -> product jobs -> scheduler workers
                                                    |
                           CAD-runtime job: handoff.FromJob -> ExecutePackage
                           other job:                          Execute
                                                    |
                                                    v
                                          ordered executor steps
```

The current step types are `WriteCSV`, `WriteExportManifest`, and
`RunCADRuntime`. FreeCAD execution plans use that sequence. Planning and semantic
validation precede scheduling; the executor consumes resolved payloads.

## Product jobs and identity

`job.SplitPlan` groups steps by product key, preserves their order within each
job, and orders jobs by first encounter in the plan. Steps without a derivable
product key become standalone jobs with positional `product_<index>` keys.
CSV steps use an explicit product key or infer it from the CSV filename;
manifest steps use an explicit key or the manifest product ID. CAD-runtime
steps use their explicit product key without filename inference.

`job.New` rejects empty jobs and inconsistent product identity. A job ID is the
SHA-256 digest of canonical JSON containing the product key and ordered step
identities and payloads. Payload changes, step presence, and step order affect
identity; map insertion order does not. Job identity is separate from artifact
identity and operational runtime-attempt identity.

## Handoff packages

`handoff.FromJob` constructs a validated, single-product `handoff.Package` with
job ID, product key, CSV and manifest payloads, an optional CAD-runtime payload,
optional table input metadata, and ordered step snapshots. `Package.Plan()`
reconstructs the step sequence. Table metadata is defensively copied and sorted
by logical ID, name, and fingerprint; duplicate logical IDs are rejected.

A package requires exactly one `WriteCSV`, exactly one `WriteExportManifest`,
and at most one `RunCADRuntime`. Validation checks product consistency, manifest
filename and schema, declared outputs, and CAD-runtime adapter/filename
agreement. The result filename must be a single logical filename and must not
conflict with other package filenames. A CAD-runtime package may declare zero
export outputs for native-only execution; a package without a CAD-runtime step
requires nonempty manifest outputs.

`ExpectedArtifacts()` lists CSV, manifest, optional runtime result, and declared
outputs in deterministic order. This is a logical inventory, not evidence of
successful execution or artifact acceptance. The native working document is
workspace state and is not added to that inventory.

The scheduler builds handoff packages for jobs containing `RunCADRuntime` and
calls `ExecutePackage`. Jobs without that step use `Execute` directly.
`ExecutePackage` takes a defensive snapshot of the supplied package. Direct
`Execute` rejects CAD-runtime plans because runtime dispatch requires package
context.

## Scheduling

The worker pool is bounded by the smaller of the CPU count and product job
count. `NewWithProductDirs` provisions an executor for each product directory;
the shared-executor constructor copies executor state for each dispatched job.
Adapter dependencies in a shared executor still require appropriate concurrency
handling.

`ExecuteWithResult` returns per-job step snapshots, errors, and CAD-runtime
outcomes in product order. Workers can finish in any order. A job failure does
not cancel other jobs: the scheduler drains dispatched work and selects the
returned error by product order. Caller cancellation stops further dispatch
through the scheduling context, and dispatched jobs receive the same caller
context. The scheduler waits for those workers to finish.

## Executor and runtime dispatch

The executor runs steps sequentially and stops the job on terminal failure.
Step snapshots carry product and step IDs, status, attempts, and operational
start/end timestamps. Step IDs are zero-based decimal positions in the job.

Before executing any step of a CAD-runtime package, preflight checks package
and plan agreement, the product directory, executable resolver, orchestration
capability, and request validity. Outcome-consuming executors also require a
verified-run provider. A preflight failure starts no steps or runtime attempts.
The executable is selected once for the package and reused across attempts.

CSV and manifest steps dispatch through `Adapter.Run`. `RunCADRuntime`
dispatches through `CADRuntimeOrchestrator` with package payloads, job and
product identity, step ID, product directory, executable selection, and a
one-based attempt number. Normal CLI and default API construction use the
Engine-owned `cadruntime.FreeCADRuntimeAdapter` and outcome-consuming executors.
See [adapter architecture](../adapters/adapter-architecture.md) for those
contracts and the attempt workspace.

## Attempts, timeout, and cancellation

Each step has at most three attempts: the initial attempt and two retries.
There is no retry backoff. Each attempt receives a child context with a
30-second timeout, bounded by the caller's deadline. Context values and
cancellation propagate to the adapter and external capability. The executor
checks the caller context before starting each step.

Timeout and cancellation are terminal. Other step errors can retry until the
attempt limit. On the normal CAD-runtime consumption path, process-invocation
and runtime-reported failures can retry; preparation, contract validation,
evidence correlation, and verification failures are terminal. The orchestration
wrapper executes once per attempt; the executor owns retries. Earlier successful
steps are not rerun when a later step retries.

Across CAD-runtime retries, logical payloads and executable selection remain
fixed while the attempt number changes. Each attempt uses a separate workspace.
`ExecutionError` carries product/step identity, the cause, and timeout/canceled
flags. Its `RetryCount` field records attempts made on step failure, including
the first attempt; a failure before starting records zero.

## Execution outcomes

The executor retains step state and the latest correlated CAD-runtime outcome,
including verification status and structured failure details. Runtime success
alone does not establish artifact acceptance: normal CAD artifact registration
requires an explicit Engine verification pass.

The CLI configures step-time artifact registration, with the CAD-runtime step
required to be final. The API owns completion-time registration and writes the
report before publishing terminal job status. API job lifecycle and transport
state are separate from the deterministic `job.Job` execution unit.

The [execution runtime](../engine/execution-runtime.md) owns the detailed
CAD-runtime invocation, evidence validation, verification, and outcome-consumption
pipeline, together with runtime output surfaces. See also
[job lifecycle](../engine/job-lifecycle.md) for API state transitions and
[record contracts](../reference/record-contracts.md) for normalized records.
