# Engine API Overview

The Engine HTTP server (`cmd/parametron-engine`) provides a REST API for job
submission, lifecycle inspection, artifact listing, and artifact file retrieval.
It is a standalone server binary separate from the `parametron` CLI.

## Server Configuration

The server accepts the following command-line flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8080` | TCP network address to listen on. |
| `--artifact-dir` | `""` | Base directory for the artifact store. Enables generic artifact listing and serving endpoints (`GET /artifacts`, `GET /artifacts/{artifactID}`) and job-scoped artifact visibility. |
| `--version` | — | Print server version and exit. |
| `--help` | — | Print command usage and exit. |

The server handles operating system interrupt signals (`SIGINT`, `SIGTERM`)
to execute a graceful shutdown, completing or cancelling in-flight work before
exiting.

## Endpoints

| Method | Path | Status | Description |
|--------|------|--------|-------------|
| `GET` | `/healthz` | `200 OK` | Liveness check. Returns plain text `ok\n`. |
| `POST` | `/job` | `202 Accepted` | Submit a job execution handoff package. Enqueues job and returns deterministic job ID and `queued` state. |
| `GET` | `/job/{id}` | `200 OK` | Inspect the current lifecycle state and failure details of a submitted job. |
| `GET` | `/job/{id}/artifacts` | `200 OK` | List artifacts produced by a specific job. Returns an empty list until the job reaches terminal `succeeded` state. |
| `GET` | `/artifacts` | `200 OK` | List all artifacts across the store. Requires `--artifact-dir` configuration (returns `404` if unconfigured). |
| `GET` | `/artifacts/{artifactID}` | `200 OK` | Stream raw artifact file bytes by artifact SHA-256 ID. Requires `--artifact-dir` configuration (returns `404` if unconfigured). |

Any unregistered route returns `404 Not Found`.

## Execution Architecture

When a job is accepted via `POST /job`:

1. The request payload is validated and canonicalized into a deterministic job
   identity (`job.Job`) and handoff package (`handoff.Package`).
2. The job is registered in the server's submission store in `queued` state and
   placed on the job queue.
3. Background runtime workers dequeue jobs asynchronously and instantiate an
   execution pipeline.
4. The default server executor factory constructs an aligned CAD-runtime
   executor (`executor.NewWithCADRuntimeConsumption`) composed of:
   - the target CAD adapter (`adapter/freecad`)
   - the Engine-owned runtime wrapper (`cadruntime.NewFreeCADRuntimeAdapter`)
   - external capability and executable resolution (`runtimecap.NewExecutableResolver`, `runtimecap.NewExternalCapability`)
   - isolated per-job working directory (`jobs/<jobID>`)
5. Upon job execution completion:
   - For successful runs, completion artifacts (`result.json`, declared output
     files, and verified artifacts) are registered into the artifact store in an
     atomic batch.
   - An execution report (`report.json`) is constructed and written to disk
     before updating the job status. Report write errors do not abort the
     transition.
   - The job status in the submission store transitions to its terminal state
     (`succeeded`, `failed`, or `canceled`).

## Timeout and Failure Semantics

- **Timeout behavior**: When a step exceeds its configured timeout, the job
  transitions to `failed` state. The status record's `error.timeout` flag is set
  to `true`, and the error message details the timeout condition.
- **Cancellation**: When context cancellation occurs (such as during graceful
  server shutdown), in-flight subprocesses are terminated, and the job transitions
  to `canceled` state with `error.canceled = true`.
- **Structured error reporting**: Aligned CAD-runtime failures populate
  structured failure fields in `GET /job/{id}` responses, including
  `classification`, `boundary`, `category`, `code`, `stage`, and `attemptId`.

## Transport Rules and Protections

- `Content-Type: application/json` is required for `POST /job`. Requests with
  unsupported media types receive `415 Unsupported Media Type`.
- Request bodies are capped at 10 MB (`10485760` bytes). Exceeding requests receive
  `413 Request Entity Too Large`.
- Unrecognized fields in `POST /job` payloads are strictly rejected with
  `400 Bad Request`.
- Non-matching HTTP methods return `405 Method Not Allowed` with an `Allow` header.

## Related Documents

- [Job Submission](job-submission.md) — `POST /job` payload structure and validation.
- [Job Lifecycle](job-lifecycle.md) — State transitions, status schema, and failure structures.
- [Job Artifacts](job-artifacts.md) — Job-scoped artifact listing endpoint.
- [Artifact Serving](artifact-serving.md) — Generic artifact listing and file retrieval.
- [Execution Runtime](execution-runtime.md) — Detailed processing pipeline and orchestration.
- [Reporting](reporting.md) — Run-level `report.json` schema and contracts.
