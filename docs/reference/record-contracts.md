# Engine-produced record contracts

Engine builds normalized records and local record packages independently of an
external storage service. Raw operational outputs are evidence and mapping inputs;
they are not interchangeable with normalized records. Engine does not own durable
PDM persistence or write directly to a PDM store.

## Families, version, identity, and provenance

`internal/engine/recordcontract` owns the payload contracts, builders, normalizers,
validators, family registry, identity derivation, and shared provenance. The
registered version is `1.0`. Version validation rejects missing, malformed, and
unsupported versions, including unsupported versions within the same major.
Concrete JSON schema files and golden examples are not the current authority.

Registry order and filenames are:

| Family | Filename under `records/` | Material |
| --- | --- | --- |
| execution | `parametron.execution-record.json` | Execution outcome, jobs, steps, timing and related record links |
| artifact | `parametron.artifact-record.json` | Artifact identity, inventory and content metadata |
| observation | `parametron.observation-record.json` | Normalized observed facts and evidence |
| reference | `parametron.reference-record.json` | Normalized reference edges, endpoints, resolution and evidence |
| failure | `parametron.failure-record.json` | Class, message, stage, severity, code, location, retry/timeout/cancellation and evidence |
| verification | `parametron.verification-record.json` | Engine outcome, category results, failure classes, linkage and evidence |

Each family has `Build…Record`, `Normalize…Record`, and `Validate…Record`
functions. `Definitions()` returns an independent registry snapshot;
`FileNameForFamily` supplies the package filename. Registration does not imply
that every normal run emits every family.

`DeriveIdentity` uses `sha256-canonical-json-v1` over normalized family, version,
required caller record key, provenance, and deterministic identity parts. Identity
parts sort by key and value. Family builders own payload-specific identity
material; callers must not invent a second record hash or substitute runtime paths
for logical identity.

Shared provenance contains:

- Source revision: revision ID, asset ID, optional SHA-256 digest.
- Inputs: kind, identity, optional digest for inputs such as DSL, model, tables,
  and profile.
- Plan: plan ID/hash.
- Linkage: product key, job ID, step reference.
- Evidence: kind, reference, optional digest.
- Runtime: tool ID, runtime ID, adapter.

Normalization trims applicable strings, sorts collections deterministically, and
copies caller-owned data. Present source-revision provenance requires a revision
or asset ID; inputs require kind and identity; evidence requires kind and ref;
present plan provenance requires plan ID or hash. Supplied digests must be valid
lowercase SHA-256. Normalization does not fabricate source or revision identities.
Failure and verification records validate their outcome consistency and normalize
evidence, linkage, and category ordering.

## Package layout and writer

`internal/engine/recordpackage` owns layout, path classification, and filesystem
materialization. `internal/engine/recordemit` integrates normal-run emission.

```text
parametron-record-package/
  parametron.record-package.json
  records/
    parametron.execution-record.json
    parametron.artifact-record.json
    parametron.observation-record.json
    parametron.reference-record.json
    parametron.failure-record.json
    parametron.verification-record.json
  artifacts/files/
  raw/
    report.json
    metadata.json
    artifact-store/manifest.json
    handoff/
    observed/parametron.observed.json
    verification/parametron.verification.json
    runtime/result.json
    runtime/parametron.reference-traversal.json
```

This is the allowed layout, not a promise that every listed file exists per run.
There is at most one record per family. `PackageInput` requires `PackageRoot`,
`PackageKey`, and one or more validated records; artifact and raw evidence files
are optional. By default it writes directly to the resolved root;
`UseCanonicalDirectoryName` selects the canonical child directory.

`parametron.record-package.json` indexes records, artifacts and raw evidence and
includes `schemaVersion`, `packageKey`, `layoutVersion`, and ownership metadata.
Records follow registry family order, artifacts sort by contract path, and raw
evidence follows layout order. Safe `raw/handoff/...` files sort lexically at the
handoff layout slot. Writer-owned JSON uses two-space indentation and a trailing
newline. Input payload buffers are copied.

The default writer rejects a non-empty destination or an existing manifest.
`OverwriteExisting` replaces layout-owned files idempotently and preserves
unrelated files. Writes are confined to the package root. Artifact payloads must
be under `artifacts/files/`; raw files must pass `IsRawEvidenceFileContractPath`.
Sentinels include `ErrInvalidPackageInput`, `ErrInvalidRecord`,
`ErrPackageDestinationExists`, `ErrPackageManifestExists`, and `ErrInvalidLayoutPath`.

`Entries`, `RecordEntries`, `RawEvidenceEntries`, `ValidateContractPath`,
`JoinContractPath`, and the write-free `Layout` resolver define paths without
performing record mapping. `EntryForContractPath` classifies static layout entries.
`IsNormalizedRecordContractPath` recognizes only registered record paths.

Raw runtime evidence is an explicit allowlist: `raw/runtime/result.json` and
`raw/runtime/parametron.reference-traversal.json`. Arbitrary descendants under
`raw/runtime/` are not accepted automatically. Safe descendants of `raw/handoff/`
are dynamic handoff evidence files, not normalized records or raw runtime-result
evidence. `raw/handoff` itself is a directory, not an evidence file. Unsafe and
noncanonical paths are rejected.

## Operational mapping and emission

`internal/engine/recordmap` performs deterministic, validated, copy-safe mapping;
it does not write package files. Each mapping family exposes a typed invalid-input
error (`ErrInvalidReportMapping`, `ErrInvalidMetadataMapping`,
`ErrInvalidArtifactMapping`, `ErrInvalidObservedMapping`,
`ErrInvalidVerificationMapping`, `ErrInvalidRuntimeResultMapping`, or
`ErrInvalidReferenceTraversalMapping`).

| Input | Mapping | Normal-run use |
| --- | --- | --- |
| `report.json` | `MapReport`: execution and optional failure record; status, timing, plan, jobs, steps, errors, retry/timeout/cancellation, deterministic linkage and outcome precedence | Execution/failure records and raw report |
| `metadata.json` | `MapMetadata`: provenance and input identities, plan and conservative runtime/toolchain enrichment | Provenance enrichment and available raw metadata |
| Artifact store records / `manifest.json` | `MapArtifactStoreRecords` / `MapArtifactStoreManifest`: artifact records | Available raw inventory; normalized artifact emission is not integrated |
| `parametron.observed.json` | `MapObserved`: observation and optional reference records | Available raw evidence; no observation-derived normalized records |
| `parametron.verification.json` result | `MapVerification`: summary, categories, failure classes and evidence | Available raw evidence; no normalized verification emission |
| Runtime `result.json` | `MapRuntimeResult` handles the legacy result shape only; success has no failure record | Available raw evidence; no direct aligned-result failure mapping |
| `parametron.reference-traversal.json` | `MapReferenceTraversal`: optional reference record | Bounded verified-candidate integration described below |
| Handoff package | Raw runtime/provenance evidence classification | No normalized handoff record mapping or automatic handoff collection |
| Job status | Operational lifecycle state | Not a normalized record or durable storage contract |

The retained runtime-result mapper is separate from the aligned decoder in
`internal/engine/adapter/freecad/runtime_result.go`. It does not reinterpret
aligned `succeeded`/`failed` payloads as the old result shape. Its failure mapping
preserves known classifications, uses a runtime fallback for unknown ones, and
uses caller-supplied linkage. Verification and runtime-result mappers validate
optional evidence digests, collapse matching evidence references, and reject
conflicting digests. Verification category order is `components`, `metadata`,
`parameters`, `references` under record normalization.

Normal non-cached runs emit under `<runRoot>/parametron-record-package/`, with
package key `engine-run:<planHash>`. Execution records are always mapped when
report construction succeeds; report failure material adds a validated failure
record, including on failed runs. Raw report bytes remain unchanged. Available
metadata, artifact inventory, observed, verification, and runtime-result evidence
are included from their configured run-root locations.

Normalized report mapping removes step/runtime timing for normal-run records.
Equivalent normal runs have stable package keys, normalized record and manifest
bytes, file sets, and indexes. Raw evidence preserves timestamps and operational
paths; full package-tree byte equality is not guaranteed. Successful non-cached
runs complete cache only after package emission succeeds. Failed execution retains
its original execution error and does not complete cache. Cached runs neither
create missing packages nor modify existing packages.

## Traversal normalization and bounded reference emission

The pure mapper accepts supplied traversal schema `2.0` evidence and a stable
caller `RecordKey`. It performs no file discovery, I/O, digest computation, or
package writing. `recordcontract.BuildReferenceRecord` owns normalized identity
and edge order; `NormalizeProvenance` owns provenance order.

| Raw value | Normalized value |
| --- | --- |
| Resolution `resolved` | `resolved` |
| Resolution `missing`, `unresolved`, `skipped`, `failed` | `unresolved` |
| Kind `document_internal_reference` | `component` |
| Kind `external_document_reference`, `external_file_reference` | `external` |

Object endpoints use document path and object name; document/external-document/
external-file endpoints use document path with an empty name. Raw node IDs only
support edge lookup; they do not become endpoint IDs, asset/revision IDs, or
storage identity. Labels are not identity. `ReferenceEdge.Role` stays empty.
Property/mechanism/type details, labels, diagnostics, raw sequence, aggregate
status, and raw node IDs stay in raw evidence. Exact normalized duplicate edges
collapse deterministically; distinct normalized edges remain distinct without
invented roles or path suffixes.

Mapper linkage and provenance linkage reconcile independently for `JobID`,
`ProductKey`, and `StepRef`, after trimming: empty plus a value adopts that value,
equal values converge, and conflicting non-empty values fail. One resolved
linkage populates both edge and provenance linkage. Canonical evidence is
`kind = reference-traversal`, `ref = raw/runtime/parametron.reference-traversal.json`.
Caller and provenance digests reconcile the same way; duplicate canonical evidence
collapses to one entry. Unrelated provenance is preserved. Context validation
occurs even for zero edges; valid zero-edge input returns no record and no error.

Identity can include caller record key, normalized provenance and linkage,
normalized kind/endpoints/resolution, and supplied canonical evidence digest.
The mapper introduces no additional identity based on raw ordering, labels,
diagnostics, timestamps, process IDs or attempt-local placement.

For normal-run emission, only an overall successful run is eligible. Each candidate
must be a terminal correlated outcome with an explicit verification pass, traversal
bytes, and canonical Engine job/product/step linkage:

| Eligible candidates | Emission |
| --- | --- |
| Zero | No traversal additions |
| Exactly one | Exact raw evidence and optional normalized reference record |
| More than one | Omit traversal additions deterministically |

`recordemit` strictly decodes JSON, rejecting unknown fields and trailing values;
computes lowercase SHA-256 over the exact bytes; supplies
`engine-run:<planHash>:reference` as record key and metadata-derived provenance;
and calls the pure mapper. The exact digest input is emitted unchanged at the
canonical raw path. Non-zero mapped edges produce the registry-backed reference
file through the writer. Zero-edge evidence is indexed without a reference record.
Invalid JSON, mapping, linkage, provenance, or evidence fails package emission
closed. No first/last candidate selection or concatenated traversal JSON occurs.

Normal-planner production coverage is internal/component references. Non-empty
external target integration, multi-product aggregation, multiple records per family,
richer reference fields, and failed-run traversal normalization are not part of
this bounded emission path. Package archiving and remote publishing are not
implemented. Runtime collection is owned by
[execution runtime](../engine/execution-runtime.md); traversal request structure
is owned by [adapter architecture](../adapters/adapter-architecture.md).
