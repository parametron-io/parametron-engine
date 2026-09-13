# Job-Scoped Artifact Listing

The `GET /job/{id}/artifacts` endpoint lists the artifacts registered by a
specific job, filtered by that job's product identity.

## Endpoint

```http
GET /job/{id}/artifacts
```

## Behavior and Visibility Rules

1. **Job lookup**: Queries `{id}` in the submission store. If unknown, returns
   `404 Not Found`.
2. **Terminal visibility**: Artifacts are registered only at the completion of a
   job and become visible only when the job reaches `succeeded` state.
   - For jobs in `queued`, `running`, `failed`, or `canceled` state, the endpoint
     returns an empty array (`"artifacts": []`).
3. **Product scoping**: Artifacts are filtered to match the job's product key.
4. **Artifact classes**:
   - `execution_output`: Registered execution deliverables and control files
     such as input CSVs, export manifests, and CAD runtime `result.json`.
   - `verified_artifact`: Release-facing deliverables (`step`, `csv`, `pdf`)
     promoted after an explicit Engine-owned verification pass.
5. **Raw evidence exclusion**: Native CAD manifests, observation requests,
   `parametron.observed.json`, and intermediate working copy files are preserved
   as raw runtime evidence and are never registered as artifacts.
6. **Atomic batch registration**: Artifact registration at the completion boundary
   is all-or-nothing. If any artifact in the batch fails validation or filesystem
   checks, the entire batch is rejected and no artifacts become visible.

> [!NOTE]
> `GET /job/{id}/artifacts` returns metadata only. To retrieve the raw file bytes
> of an artifact, use the generic artifact serving endpoint
> `GET /artifacts/{artifactID}`.

## Response Schema

```json
{
  "schemaVersion": "1.0",
  "jobId": "291a134a65b3dd43644f08e49339e31d8717d3d2c88f1dc21b34e55e88863aa9",
  "artifacts": [
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
    },
    {
      "id": "f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2",
      "class": "execution_output",
      "type": "step",
      "path": "jobs/291a134a.../bracket.step",
      "filename": "bracket.step",
      "mimeType": "application/step",
      "sizeBytes": 45056,
      "checksumSHA256": "f2ca1bb6...",
      "jobId": "291a134a65b3dd43644f08e49339e31d8717d3d2c88f1dc21b34e55e88863aa9",
      "productId": "bracket",
      "stepId": "2",
      "createdAt": "2026-09-13T14:00:05Z"
    },
    {
      "id": "a89d71f65bbd698e6b8c9d2e1f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a",
      "class": "verified_artifact",
      "type": "step",
      "path": "jobs/291a134a.../bracket.step",
      "filename": "bracket.step",
      "mimeType": "application/step",
      "sizeBytes": 45056,
      "checksumSHA256": "f2ca1bb6...",
      "jobId": "291a134a65b3dd43644f08e49339e31d8717d3d2c88f1dc21b34e55e88863aa9",
      "productId": "bracket",
      "stepId": "2",
      "createdAt": "2026-09-13T14:00:05Z"
    }
  ]
}
```

### Dual-Class Representation

Eligible release-facing outputs (`step`, `csv`, `pdf`) that pass verification
appear in the artifact inventory under both `execution_output` and
`verified_artifact` classes. Because `class` is an input to deterministic ID
derivation, the two records have distinct, stable SHA-256 artifact IDs.

## Ordering

Artifacts in the response are sorted deterministically by:
1. `jobId`
2. `productId`
3. `stepId`
4. `class`
5. `path`
6. `filename`
7. `type`
8. `checksumSHA256`
9. `id`

## Related Documents

- [Artifact Serving](artifact-serving.md) — Unscoped artifact listing and file byte streaming.
- [Job Lifecycle](job-lifecycle.md) — Job states and status inspection.
- [API Overview](api-overview.md) — HTTP API architecture and configuration.
