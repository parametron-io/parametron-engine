# Job Submission

The `POST /job` endpoint accepts an execution handoff package, validates and
canonicalizes its engineering intent, registers the job in the submission store,
enqueues it for asynchronous execution, and returns a deterministic job identity.

## Endpoint

```http
POST /job
Content-Type: application/json
```

## Submission Lifecycle

```text
HTTP Client                      API Server                        Job Queue
    |                                |                                 |
    |-- POST /job (JSON payload) --->|                                 |
    |                                |-- Validate transport & schema   |
    |                                |-- Canonicalize job identity     |
    |                                |-- Register in submission store  |
    |                                |-- Enqueue job ----------------->|
    |<-- 202 Accepted (jobId) -------|                                 |
```

1. **Transport validation**: Checks `Content-Type: application/json` and enforces
   the 10 MB payload size limit.
2. **Schema validation**: Decodes the JSON body with strict unknown field
   rejection (`DisallowUnknownFields`).
3. **Table validation**: If `tables` metadata is present, validates non-empty
   identifiers and SHA-256 fingerprints.
4. **Canonicalization & identity**: Derives the canonical job identity (`job.Job`)
   and verifies that any caller-supplied `jobID` exactly matches the computed
   canonical ID.
5. **Registration & idempotency**: Registers the job in `queued` state in the
   submission store. Re-submitting an existing job ID returns the existing status
   without duplicate enqueueing.
6. **Enqueue & dispatch**: Enqueues the canonical package onto the internal queue
   for background runtime execution.
7. **Response**: Returns `202 Accepted` with the canonical `jobId`, `productKey`,
   initial `queued` state, and a `Location: /job/{jobId}` header.

## Request Payload

The request body is a JSON object containing a single `handoff` root object.

```json
{
  "handoff": {
    "jobID": "291a134a65b3dd43644f08e49339e31d8717d3d2c88f1dc21b34e55e88863aa9",
    "productKey": "bracket",
    "csv": {
      "productKey": "bracket",
      "filename": "params.csv",
      "headers": ["width", "height"],
      "values": [100, 50]
    },
    "manifest": {
      "manifestFilename": "export_manifest_v1.json",
      "schemaVersion": "1.0",
      "planHash": "a1b2c3d4e5f67890...",
      "adapter": "freecad",
      "product": {
        "key": "bracket",
        "name": "Mounting Bracket"
      },
      "sourceDocument": "models/bracket.FCStd",
      "inputs": {
        "sourceModel": "models/bracket.FCStd",
        "parameters": [
          { "name": "width", "type": "number", "unit": "mm" }
        ]
      },
      "values": {
        "width": 100
      },
      "parameterAssignments": [
        { "name": "width", "value": 100 }
      ],
      "outputs": [
        { "format": "step", "filename": "bracket.step" }
      ]
    },
    "cadRuntime": {
      "productKey": "bracket",
      "adapter": "freecad",
      "manifestFilename": "export_manifest_v1.json",
      "resultFilename": "result.json"
    },
    "tables": [
      {
        "logicalId": "fasteners",
        "name": "fastener_catalog",
        "fingerprint": "f865b53623b121fd34ee5426c792e5c33af8c227"
      }
    ],
    "steps": [
      {
        "type": "WriteCSV",
        "csv": {
          "productKey": "bracket",
          "filename": "params.csv",
          "headers": ["width", "height"],
          "values": [100, 50]
        }
      },
      {
        "type": "WriteExportManifest",
        "manifest": {
          "manifestFilename": "export_manifest_v1.json",
          "schemaVersion": "1.0",
          "planHash": "a1b2c3d4e5f67890...",
          "adapter": "freecad",
          "product": { "key": "bracket", "name": "Mounting Bracket" },
          "sourceDocument": "models/bracket.FCStd",
          "inputs": { "sourceModel": "models/bracket.FCStd" },
          "values": { "width": 100 },
          "parameterAssignments": [{ "name": "width", "value": 100 }],
          "outputs": [{ "format": "step", "filename": "bracket.step" }]
        }
      },
      {
        "type": "RunCADRuntime",
        "cadRuntime": {
          "productKey": "bracket",
          "adapter": "freecad",
          "manifestFilename": "export_manifest_v1.json",
          "resultFilename": "result.json"
        }
      }
    ]
  }
}
```

### Top-Level Handoff Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `jobID` | string | Optional | Expected canonical job ID. If provided, must match the computed canonical identity. |
| `productKey` | string | Yes | Unique identifier for the product being executed. |
| `csv` | object | Yes | Payload for `WriteCSV` step. |
| `manifest` | object | Yes | Payload for `WriteExportManifest` step. |
| `cadRuntime` | object | Optional | Logical CAD runtime execution payload. |
| `tables` | array | Optional | Table input metadata references. |
| `steps` | array | Yes | Ordered sequence of step snapshots to execute. |

### Logical CAD-Runtime Payload

The `cadRuntime` object specifies authoring intent for external runtime invocation:

| Field | Type | Description |
|-------|------|-------------|
| `productKey` | string | Target product execution key. |
| `adapter` | string | Adapter identifier (e.g. `"freecad"`). |
| `manifestFilename` | string | Expected manifest filename (e.g. `"export_manifest_v1.json"`). |
| `resultFilename` | string | Expected runtime result filename (e.g. `"result.json"`). |

> [!IMPORTANT]
> The `cadRuntime` payload represents **logical authoring intent only**. It must
> not include host-specific executable paths, working copy paths, attempt layouts,
> or raw evidence data. Operational resolution is performed on the server side.

### Table Input Metadata

When tables participate in the job, `tables` carries metadata entries:

| Field | Type | Description |
|-------|------|-------------|
| `logicalId` | string | Logical table identifier used in DSL expressions. |
| `name` | string | Embedded table name from the table resource JSON. |
| `fingerprint` | string | SHA-256 semantic fingerprint of the table content. |

Table metadata is validated and sorted deterministically. It does not alter the
step-based canonical job ID calculation.

## Response

Upon successful validation and registration, the server responds with
`202 Accepted`:

```http
HTTP/1.1 202 Accepted
Content-Type: application/json
Location: /job/291a134a65b3dd43644f08e49339e31d8717d3d2c88f1dc21b34e55e88863aa9

{
  "schemaVersion": "1.0",
  "jobId": "291a134a65b3dd43644f08e49339e31d8717d3d2c88f1dc21b34e55e88863aa9",
  "productKey": "bracket",
  "state": "queued"
}
```

## Error Responses

| Status | Reason |
|--------|--------|
| `400 Bad Request` | Malformed JSON, unknown fields present, invalid table metadata, or mismatched `jobID`. |
| `405 Method Not Allowed` | Method other than `POST` used. `Allow: POST` header returned. |
| `413 Request Entity Too Large` | Payload body exceeds 10 MB limit. |
| `415 Unsupported Media Type` | `Content-Type` is not `application/json`. |
| `500 Internal Server Error` | Registration or queueing failed in the submission store. |

## Related Documents

- [API Overview](api-overview.md) — HTTP API server endpoints and architecture.
- [Job Lifecycle](job-lifecycle.md) — Job lifecycle states, transitions, and status inspection.
- [Execution Runtime](execution-runtime.md) — CAD runtime execution, verification, and records.
- [Execution Model](../architecture/execution-model.md) — Handoff packages, scheduling, and executor flow.
