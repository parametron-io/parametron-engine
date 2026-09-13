# parametron snapshot

`parametron snapshot` executes a single input case and captures a deterministic snapshot package in the output directory.

## Usage

```bash
# Standalone DSL entrypoint
go run ./cmd/parametron snapshot --file <dsl-path> --inputs <inputs.json> --out <dir> [flags]

# Project-based entrypoint
go run ./cmd/parametron snapshot --project <project-dir | parametron.project.json> --inputs <inputs.json> --out <dir> [flags]
```

`--file` and `--project` are mutually exclusive; exactly one must be provided.

## Flags

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--file` | `-f` | One of | Path to a DSL file (standalone DSL entrypoint); mutually exclusive with `--project` |
| `--project` | | One of | Path to a project directory or `parametron.project.json` file (project-based entrypoint); mutually exclusive with `--file` |
| `--inputs` | | Yes | Flat JSON object of parameter overrides |
| `--out` | `-o` | No | Output directory; defaults to `./output` (must be empty or non-existent) |
| `--debug` | `-d` | No | Enable debug logging |

## Inputs Format

`--inputs` is a path to a JSON file containing a flat object:

```json
{
  "width": 100,
  "material": "Steel"
}
```

## Output Directory Requirement

The output directory must be empty or non-existent. Snapshot rejects a non-empty directory.

## Output Files

Snapshot uses `--out` as the run root without adding a plan-hash directory.
Descriptor and operational files are at the root; product artifacts use
subdirectories:

| File | Description |
|------|-------------|
| `snapshot.json` | Snapshot metadata |
| `inputs.json` | Input parameter values used |
| `plan.json` | Resolved execution plan |
| `report.json` | Execution report |
| `metadata.json` | Run-level metadata |
| `manifest.json` | Artifact inventory |
| Generated artifacts | CSV files under `products/<product-key>/`; CAD outputs in attempt workspaces below that product directory |

Execution also uses the normal [record-package emission](../reference/record-contracts.md).
Execution failures may leave partial outputs. If execution returns an error,
snapshot writes its descriptor before returning that error when file enumeration
and descriptor writing succeed.

## snapshot.json Schema

```json
{
  "schemaVersion": "1.0",
  "dslFile": "<execution entrypoint path: DSL file path in standalone mode, project directory or parametron.project.json path in project mode>",
  "dslHash": "<hash>",
  "planHash": "<hash>",
  "inputs": { "width": 100, "material": "Steel" },
  "runRoot": "<out dir>",
  "generatedFiles": ["manifest.json", "products/widget/export_manifest_v1.json"]
}
```

- `profile` field is omitted when no active profile is present.
- `generatedFiles` is a sorted list of paths relative to `runRoot`, collected
  before `snapshot.json` is written, so it excludes the descriptor itself.
- The `products/widget/export_manifest_v1.json` example reflects active planner
  naming. FreeCAD plans use `RunCADRuntime` and the external runtime capability.
- `snapshot.json` has stable content for equivalent inputs and the same output
  path; operational timestamps and raw runtime evidence are not promised to be
  byte-identical across runs.

## Cache Behavior

The cache is not used by `snapshot`. Execution always runs regardless of cached results.

## Success Output

On success, the CLI prints: `"Snapshot created at <dir>"`

## Related Documents

- [Diff](diff.md) — comparing two snapshot directories
- [Command families](command-families.md) — command routing and shared infrastructure
- [Reporting](../engine/reporting.md) — `report.json` schema
