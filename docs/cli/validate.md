# parametron validate

`parametron validate` parses the DSL file, runs semantic validation, and generates an execution plan. No files are written and no execution occurs.

## Usage

```bash
# Standalone DSL entrypoint
parametron validate --file <dsl-path> [flags]

# Project-based entrypoint
parametron validate --project <project-dir | parametron.project.json> [flags]
```

`--file` and `--project` are mutually exclusive; exactly one must be provided.

## Flags

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--file` | `-f` | One of | Path to a DSL file (standalone DSL entrypoint); mutually exclusive with `--project` |
| `--project` | | One of | Path to a project directory or `parametron.project.json` file (project-based entrypoint); mutually exclusive with `--file` |
| `--inputs` | | No | JSON file of parameter overrides (flat object `{key: scalar}`) |
| `--set` | | No | Parameter override (`key=value`), repeatable |
| `--table` | | No | Load a JSON table from disk (`logical-id=path`), repeatable |
| `--debug` | `-d` | No | Enable debug logging |

## Behavior

The command runs the parse → validate → plan pipeline and exits.

- On success: prints `"Validation succeeded for <file>"` and exits with code 0.
- On error: prints the error and exits with a non-zero code.

No execution occurs. No output files are created.

When `--table` flags are provided, each JSON table file is loaded from disk before validation. The loaded tables are made available to validation and planning by their logical IDs. If no `--table` flags are provided, behavior is unchanged.

```bash
parametron validate --file model.dsl --table fasteners=./tables/fasteners.json
parametron validate --file model.dsl --table fasteners=./tables/fasteners.json --table labels=./tables/labels.json
```

See `docs/cli/runtime-behavior.md` for `--table` format rules and error conditions.

## Project Entrypoint

When `--project` is provided, the CLI loads the project mapping before proceeding. The DSL path is resolved from the project mapping; it is not user-provided.

Project entrypoint resolution:

1. The `parametron.project.json` file is read and parsed.
2. The project mapping is validated (schema, path safety, logical ID constraints). Failures exit immediately with a non-zero code.
3. The DSL file declared in the project mapping is resolved relative to the project directory.
4. The resolved DSL path is used as the parse input.
5. Project tables declared in the `tables` field are loaded by logical ID after DSL parse and before DSL validation.
6. Product `source_model` logical IDs are resolved against the project mapping's `resources.models` map after DSL validation and before plan generation. An unrecognized ID fails immediately.
7. When a project-root `parametron.cad.json` is present, capture-backed authoring validation also runs before planning proceeds:
   - numeric manifest-bound params must resolve to exactly one captured parameter by exact name
   - zero matches and multiple exact-name matches fail deterministically
   - matching is case-sensitive
   - `displayName` is not used as a fallback identity
   - `source_model` must exactly match `capture.sourceDocument.logicalId`

`--table` inputs are merged after project tables are loaded. A duplicate logical ID across the project mapping and `--table` fails deterministically.

Failure boundaries when using a project entrypoint:

| Condition | When it fails |
|:----------|:-------------|
| Missing or unreadable `parametron.project.json` | Project file load |
| Invalid project file schema or unknown field | Project mapping validation |
| Model path escaping the project root | Project mapping validation |
| Table path escaping the project root | Project mapping validation |
| `dsl` field is an absolute path | Project mapping validation |
| `dsl` field escapes the project root or normalizes to `.` | Project mapping validation |
| Missing DSL file declared in project | Project resolution |
| Missing mapped model file | Project resolution |
| Missing mapped table file | Project resolution |
| Malformed JSON in a mapped table file | Project resolution |
| Unknown logical model ID in DSL `source_model` | Model ID resolution |
| Case-mismatched logical model ID in DSL `source_model` | Model ID resolution |
| Unknown logical table ID in DSL expression | Table ID resolution |
| `source_model` does not match capture `sourceDocument.logicalId` | Capture-backed authoring validation |
| Numeric manifest-bound DSL parameter has no exact captured parameter-name match | Capture-backed authoring validation |
| Numeric manifest-bound DSL parameter matches multiple captured parameters by exact name | Capture-backed authoring validation |
| Numeric manifest-bound DSL parameter matches only by `displayName` or wrong case | Capture-backed authoring validation |

All project-level failures are deterministic. Identical inputs produce identical errors.

Both accepted project entrypoint forms are explicitly exercised in the project mapping integration corpus. Directory input and direct project file input are treated identically by the CLI.

```bash
# Standalone DSL entrypoint
parametron validate --file model.dsl

# Project directory entrypoint
parametron validate --project testdata/projects/freecad/smoke/minimal-valid-project

# Explicit project file entrypoint
parametron validate --project testdata/projects/freecad/smoke/minimal-valid-project/parametron.project.json
```

See `docs/authoring/project-mapping.md` for the full `parametron.project.json` format reference.

## CI Usage

The process exit code is the pass/fail signal. This makes `validate` suitable for use as a CI gate:

```bash
parametron validate --file model.dsl
echo $?  # 0 = valid, non-zero = invalid
```

## Inputs Format

When using `--inputs`, provide a flat JSON object:

When both `--set` and `--inputs` are provided, values from `--inputs` take precedence.

```json
{
  "width": 100,
  "material": "Steel",
  "enabled": true
}
```

## Related Documents

- `docs/cli/command-families.md` — command routing and shared infrastructure
- `docs/cli/runtime-behavior.md` — `--set` override rules and type conversion
- `docs/authoring/project-mapping.md` — `parametron.project.json` format, path rules, and error classification
