# parametron simulate

`parametron simulate` runs multiple input cases through the full parse → validate → plan → execute → report pipeline and produces a deterministic summary file.

## Usage

```bash
# Standalone DSL entrypoint
parametron simulate --file <dsl-path> --inputs <cases.json> [flags]

# Project-based entrypoint
parametron simulate --project <project-dir | parametron.project.json> --inputs <cases.json> [flags]
```

`--file` and `--project` are mutually exclusive; exactly one must be provided.

## Flags

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--file` | `-f` | One of | Path to a DSL file (standalone DSL entrypoint); mutually exclusive with `--project` |
| `--project` | | One of | Path to a project directory or `parametron.project.json` file (project-based entrypoint); mutually exclusive with `--file` |
| `--inputs` | | Yes | JSON array of input objects (one per case) |
| `--out` | `-o` | No | Base output directory (default `./output`) |
| `--table` | | No | Load a JSON table from disk (`logical-id=path`), repeatable |
| `--fail-fast` | | No | Stop after the first failing case |
| `--max-errors` | | No | Maximum number of errors before stopping (0 = no limit) |
| `--scripts-dir` | | No | Directory for Python runner scripts |
| `--debug` | `-d` | No | Enable debug logging |

## Table Inputs

When `--table` flags are provided, each JSON table file is loaded from disk before any case is processed. The same set of tables is made available to every case in the run. If no `--table` flags are provided, behavior is unchanged.

```bash
parametron simulate --file model.dsl --inputs cases.json --table fasteners=./tables/fasteners.json
```

See `docs/cli/runtime-behavior.md` for `--table` format rules and error conditions.

## Inputs Format

`--inputs` must be a path to a JSON file containing a JSON array of flat input objects, one per case:

```json
[
  { "width": 100, "material": "Steel" },
  { "width": 200, "material": "Aluminium" }
]
```

## Outputs

Each case produces a subdirectory `case-<NNN>` under `--out`, containing:
- `report.json`
- `metadata.json`
- `manifest.json`
- Generated artifacts
- `parametron-record-package/`

After all cases complete, a `simulate_report.json` is written to `--out`.

## simulate_report.json Schema

Case `status` values in `simulate_report.json` are:
- `passed`
- `failed`
- `skipped`

`skipped` is emitted for remaining cases that were not executed because `--fail-fast` or `--max-errors` stopped the run early.

This status vocabulary is specific to `simulate_report.json`. It does not match `report.json`, which uses the run-level values documented in `docs/engine/reporting.md`.

```json
{
  "schemaVersion": "1.0",
  "file": "<execution entrypoint path: DSL file path in standalone mode, project directory or parametron.project.json path in project mode>",
  "totalCases": 2,
  "passedCases": 1,
  "failedCases": 1,
  "cases": [
    {
      "index": 0,
      "inputs": { "width": 100 },
      "status": "passed",
      "planHash": "<hash>",
      "runRoot": "output/case-000"
    },
    {
      "index": 1,
      "inputs": { "width": 200 },
      "status": "failed",
      "planHash": "<hash>",
      "runRoot": "output/case-001",
      "error": "<error message>"
    }
  ]
}
```

## Cache Behavior

The cache is not used by `simulate`. Every case always executes regardless of cached results.

## Related Documents

- `docs/cli/command-families.md` — command routing and shared infrastructure
- `docs/cli/runtime-behavior.md` — output directory strategy
- `docs/engine/reporting.md` — `report.json` schema
