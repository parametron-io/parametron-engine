# parametron diff

`parametron diff` compares two plan files or two snapshot directories and reports whether they are equal.

## Usage

**Plan mode:**
```bash
parametron diff --plan <path1> --plan <path2> [--json]
```

**Snapshot mode:**
```bash
parametron diff --snapshot <dir1> --snapshot <dir2> [--json]
```

## Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--plan` | Yes (plan mode) | Path to a plan file. Must be provided exactly twice. |
| `--snapshot` | Yes (snapshot mode) | Path to a snapshot directory. Must be provided exactly twice. |
| `--json` | No | Emit JSON output instead of human-readable text |
| `--debug / -d` | No | Enable debug logging |

Exactly one of `--plan` or `--snapshot` must be provided per invocation.

## Plan Mode

Parses and normalizes both plan files to canonical JSON, then compares byte-by-byte.

- Reports equal or reports the byte offset, line, and column of the first difference.
- Without `--json`: human-readable output to stdout. Exit 0 if equal.
- With `--json`: JSON output (see below).

## Snapshot Mode

Reads `snapshot.json` from each directory and compares:
- `planHash`
- `dslHash`
- `inputs`
- `generatedFiles`

Also checks for presence of `report.json`, `metadata.json`, and `manifest.json` in each directory. If both directories contain a `report.json`, compares the `status` field.

Snapshot diff does not deep-compare artifact file contents.

## JSON Output Schema

```json
{
  "schemaVersion": "1.0",
  "mode": "plan",
  "equal": false,
  "summary": "Plans differ at byte 142",
  "firstDiffByte": 142,
  "firstDiffLine": 7,
  "firstDiffColumn": 4
}
```

For snapshot mode when not equal:
```json
{
  "schemaVersion": "1.0",
  "mode": "snapshot",
  "equal": false,
  "summary": "Snapshots differ",
  "differences": [
    "planHash differs",
    "generatedFiles differ"
  ]
}
```

- `differences` is omitted when `equal` is true.
- `firstDiffByte`, `firstDiffLine`, `firstDiffColumn` apply to plan mode only and are omitted when equal.

## Exit Codes

- Exit 0: inputs are equal (or command succeeded with `--json`).
- Non-zero: inputs differ, or an error occurred.

## Related Documents

- `docs/cli/snapshot.md` — creating snapshot directories to compare
- `docs/cli/command-families.md` — command routing
