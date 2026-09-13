# JSON Table Resources

Parametron defines a strict JSON table resource format at the engine layer.
The `internal/engine/table` package provides parsing, validation, canonical
serialization, row lookup, and SHA-256 fingerprinting for tabular engineering
data.

Table resources are authored as standalone JSON files and referenced in DSL
expressions via the `table_cell` function.

## Schema Format

A table resource is a single JSON object.

```json
{
  "schemaVersion": "1.0",
  "name": "fasteners",
  "keyColumn": "sku",
  "columns": [
    { "name": "sku",      "type": "string",  "required": true  },
    { "name": "diameter", "type": "number",  "required": true  },
    { "name": "label",    "type": "string",  "required": true  },
    { "name": "coated",   "type": "boolean", "required": true  },
    { "name": "note",     "type": "string",  "required": false }
  ],
  "rows": [
    {
      "sku": "M8x20",
      "diameter": 8,
      "label": "Hex bolt",
      "coated": true,
      "note": "zinc"
    },
    {
      "sku": "M8x30",
      "diameter": 8,
      "label": "Hex bolt long",
      "coated": false
    }
  ]
}
```

## Field Definitions

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schemaVersion` | string | Yes | Schema version string. Must equal `"1.0"`. |
| `name` | string | Yes | Logical table name. Must be non-empty. |
| `keyColumn` | string | Yes | Name of the column that uniquely identifies each row. |
| `columns` | array | Yes | Ordered array of column definitions. Must be non-empty. |
| `rows` | array | Yes | Array of data row objects. |

### Column Definition

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Column name. Must be unique within the table. |
| `type` | string | Yes | Column type: `"string"`, `"number"`, or `"boolean"`. |
| `required` | boolean | Yes | Whether the column must be present in every row. |

### Column Types

| Type | JSON Representation | Notes |
|------|---------------------|-------|
| `string` | `"text"` | Non-null string. |
| `number` | `100` or `100.5` | Formatted JSON number (raw numeric literal). `null` is rejected. |
| `boolean` | `true` or `false` | Boolean literal. `null` is rejected. |

## Validation Rules

1. **Table structure**:
   - `schemaVersion` must equal `"1.0"`.
   - `name`, `keyColumn`, and `columns` must be non-empty.
   - Unknown root fields are rejected.
2. **Column definitions**:
   - Column names must be unique within the table.
   - `type` must be one of `"string"`, `"number"`, `"boolean"`.
   - `keyColumn` must exist in `columns`, must have type `"string"`, and must have `required: true`.
3. **Row definitions**:
   - Keys in row objects must match declared column names. Unknown column keys are rejected.
   - Required columns must be present in every row.
   - `null` values are prohibited across all column types.
   - Key column values must be non-empty, non-whitespace strings and must be unique across all rows.

## Canonical Serialization and Fingerprinting

`table.CanonicalJSON(t *Table) ([]byte, error)` produces a deterministic,
byte-stable representation:

- Root fields serialize in strict order: `schemaVersion`, `name`, `keyColumn`,
  `columns`, `rows`.
- Within each row, fields are written in the exact order declared by `columns`.
- Numeric literals are preserved as raw lexemes (`json.Number`). Distinct numeric
  representations (e.g. `1` vs `1.0`) produce distinct canonical bytes.

`table.ComputeFingerprint(t *Table) (string, error)` derives the SHA-256 digest
from the canonical JSON bytes and returns a 64-character lowercase hexadecimal
string.

### Cache Key and Metadata Integration

- Table fingerprints participate directly in cache key derivation for geometry,
  artifact, and metadata layers. Changing table content invalidates downstream
  caches without requiring path changes.
- `metadata.json` records logical table IDs, names, and fingerprints in its
  `tables` section.

## Go API and Error Model

The `internal/engine/table` package provides:

- `table.Parse(data []byte) (*Table, error)` — Decodes and validates JSON bytes.
- `table.LoadFile(path string) (*Table, error)` — Reads a file from disk and parses it.
- `table.Validate(t *Table) error` — Validates structural and data constraints.
- `t.RowByKey(key string) (Row, error)` — Constant-time row lookup by key column value.

### Error Taxonomy

| Sentinel | Concrete Type | Cause |
|----------|---------------|-------|
| `ErrIO` | `*FileError` | File read error during `LoadFile`. |
| `ErrDecode` | `*DecodeError` | Malformed JSON or trailing content. |
| `ErrValidation` | `*ValidationError` | Constraint or schema rule violation. |
| `ErrRuntime` | `*RuntimeError` | Runtime lookup failure (e.g. row not found). |

## Filesystem Loading (`tableloader`)

The `internal/engine/tableloader` package loads table files declared via `--table`
flags or project mappings:

```go
tables, err := tableloader.LoadFiles(map[string]string{
    "fasteners": "tables/fasteners.json",
    "materials": "tables/materials.json",
})
```

Files are read in deterministic order (sorted by logical ID), validated, and
injected into the authoring pipeline for planning-stage evaluation.

## Related Documents

- [DSL Semantics](../authoring/dsl-semantics.md) — `table_cell` function semantics and lookups.
- [Execution Runtime](execution-runtime.md) — Cache invalidation and metadata provenance.
- [Repository Structure](../architecture/repository-structure.md) — Package layout and boundaries.
