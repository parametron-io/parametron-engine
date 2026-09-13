# Artifact Serving

The Engine HTTP API provides two unscoped endpoints for interacting directly with
the artifact store:

- `GET /artifacts` — Lists all artifacts registered across the store.
- `GET /artifacts/{artifactID}` — Streams the raw file bytes of a specific artifact.

These routes are enabled when the server is started with the `--artifact-dir`
flag. Without an configured artifact store, both routes return `404 Not Found`.

## Endpoints

### GET /artifacts

Returns all artifacts currently registered in the artifact store, sorted in stable,
deterministic order by artifact ID.

```http
GET /artifacts
```

#### Response

```json
[
  {
    "id": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "class": "execution_output",
    "type": "csv",
    "path": "jobs/291a134a.../params.csv",
    "filename": "params.csv",
    "mimeType": "text/csv",
    "sizeBytes": 1024,
    "checksumSHA256": "e3b0c442...",
    "jobId": "291a134a65b3dd43644f08e49339e31d8717d3d2c88f1dc21b34e55e88863aa9",
    "productId": "bracket",
    "stepId": "0",
    "createdAt": "2026-09-13T14:00:02Z"
  }
]
```

### GET /artifacts/{artifactID}

Streams the exact file bytes of the artifact identified by `{artifactID}`.

```http
GET /artifacts/e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

#### Behavior and Errors

- **Artifact ID format**: A 64-character lowercase hexadecimal SHA-256 digest
  computed from the canonical identity string:
  `job=<j>|product=<p>|step=<s>|type=<t>|value=<v>` (where `value` is the SHA-256
  checksum if available, or filename).
- **Streaming**: Uses standard HTTP file serving with automatic `Content-Type`,
  `Content-Length`, and byte-range support.
- **Error responses**:
  - `404 Not Found` if `{artifactID}` is unknown to the store or if the backing
    file is absent from disk.
  - `405 Method Not Allowed` for non-GET requests.

## Security Properties

- **ID-based resolution**: Access is strictly by deterministic SHA-256 artifact ID.
  The server never accepts caller-specified filesystem paths.
- **Path confinement**: Artifact file paths are verified to reside strictly within
  the configured store base directory. Path traversal attempts are rejected.
- **No internal path disclosure**: Internal server filesystem paths are never
  leaked in API responses; paths returned in listings are store-relative.

## Artifact Classes and Visibility

Artifact records carry an explicit `class` attribute:

- `execution_output`: Intermediate and final execution deliverables, including
  generated CSVs, export manifests, and CAD runtime `result.json`.
- `verified_artifact`: Release-facing deliverables (`step`, `csv`, `pdf`) that
  have passed an Engine-owned verification check.

### Visibility Rules

1. **Terminal completion**: Artifacts are registered only after job completion.
   Jobs that fail, time out, or cancel register no artifacts.
2. **Atomic batch acceptance**: Completion records are accepted as a complete batch.
   If any artifact fails validation or store checks, no artifacts from that run are
   made visible.
3. **No directory scanning**: Registration is driven entirely by explicit executor
   declarations and verified outcomes. Arbitrary files in working directories are
   never registered or served.
4. **Raw evidence preservation**: Native CAD manifests, observation requests, and
   observed CAD state files (`parametron.observed.json`) remain raw evidence and
   are not exposed as registered artifacts.

## Separation from Job-Scoped Listing

| Feature | `GET /job/{id}/artifacts` | `GET /artifacts` | `GET /artifacts/{id}` |
|---------|---------------------------|------------------|-----------------------|
| Scope | Single job ID | All artifacts in store | Single artifact |
| Filtering | Scoped to job product | Unscoped | Exact ID lookup |
| Content | JSON metadata | JSON array metadata | Raw file bytes |
| Store requirement | Optional (empty if unconfigured) | Requires `--artifact-dir` | Requires `--artifact-dir` |

## Related Documents

- [Job Artifacts](job-artifacts.md) — Job-scoped artifact listing.
- [API Overview](api-overview.md) — Server configuration and endpoints.
- [Execution Runtime](execution-runtime.md) — Execution pipeline, cache, and verification.
