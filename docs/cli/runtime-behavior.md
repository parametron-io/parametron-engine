# CLI Runtime Behavior

The CLI parses and validates DSL/project inputs, plans requested work, and
executes through Engine adapters and external runtime capabilities. This page
owns command configuration, output locations, overrides, and cache behavior.
The [execution runtime](../engine/execution-runtime.md) owns the detailed
runtime and evidence-processing lifecycle.

## Entry points and overrides

Use exactly one of `--file` or `--project`. Project input is a directory
containing `parametron.project.json` or the project file itself.

```bash
go run ./cmd/parametron --file model.dsl --set width=100 --out ./output
go run ./cmd/parametron --project ./project --set width=100
```

Repeat `--set key=value` to override exported parameters. Unknown parameter
names, constants, and internal lets cannot be overridden. Values are converted
to declared parameter types before planning. Repeating the same key uses the
last supplied value. See [DSL semantics](../authoring/dsl-semantics.md).

Project mode resolves the DSL and mapped model/table resources. When a capture
contract is present, Engine builds a capture-backed semantic model and uses
semantic planning. Mapped model paths are resolved before plan construction.
See [project mapping](../authoring/project-mapping.md) for the project contract.

## Table inputs

Repeat `--table logical-id=path` to load planner-visible JSON tables:

```bash
go run ./cmd/parametron validate --file model.dsl --table fasteners=./tables/fasteners.json
```

The supplied logical ID keys the table, independently of its embedded name.
Malformed flag values and duplicate flag IDs fail before loading. Project tables
and flag tables are merged; a duplicate ID across the two sources is rejected.
Loaded tables feed table-aware validation/planning and contribute names and
fingerprints to metadata. I/O, decoding, and schema validation failures remain
distinct table error categories. See
[JSON table resources](../engine/json-table-resources.md).

## Output directories

Root execution uses `<base>/<plan-hash>/`. The base defaults to `./output`;
`--out` supplies another base unless the selected profile has `output_dir`.
That profile setting takes precedence and resolves under
`./output/<profile-output-dir>/`; absolute or escaping profile paths fail.

```text
<base>/<plan-hash>/
├── products/<product-key>/
│   ├── <parameter-csv>
│   ├── export_manifest_v1.json
│   └── _working/<attempt-id>/
├── report.json
├── manifest.json
├── metadata.json
└── parametron-record-package/
```

The CLI canonicalizes the run root to a clean absolute path. Product roots are
under `products/`. Runtime evidence and derived CAD outputs use attempt
workspaces described in [adapter architecture](../adapters/adapter-architecture.md).

The tree lists output roles, not a guarantee that every file exists after every
failure. After scheduler execution, the CLI attempts to write `report.json`
for success or failure. On success it also writes artifact inventory and
metadata. Report/metadata/inventory write errors are logged as warnings.
Record-package emission runs when report construction succeeds; an emission
failure prevents a successful return and cache completion. See
[record contracts](../reference/record-contracts.md) for package contents.

Snapshot uses its selected output directory directly, without a plan-hash
subdirectory; other command-specific layouts are defined in their command pages.

## Cache behavior

Root execution uses the signature-v2 cache strategy. Execution is skipped when
all geometry, artifact, and metadata layer markers exist for the computed keys.
Markers live under `.cache/<layer>/<key>.done` relative to the current working
directory. A cache hit returns before execution and output publication; it does
not reconstruct missing output files or create a new record package.

The signature incorporates:

- The plan identity.
- An explicit `--model-hash`, when supplied.
- Otherwise, in project mode, a combined signature of captured mapped-model
  content, ordered by logical ID.
- Loaded table fingerprints, independent of flag ordering.

Without model or table contributions, the default key is the plan hash.
Table-content changes affect cache identity; changing a table path without
changing its content does not affect its fingerprint contribution.
`--model-hash` is a caller-supplied model-version string, not a command to hash
a file.

Cache setup/check failures are logged and execution continues. Successful
execution and record-package emission precede cache-marker updates.
`snapshot` and `simulate` execute without this cache. `sweep` validates and
plans cases without executing CAD. See
[execution runtime](../engine/execution-runtime.md) for cache internals.

## External runtime configuration

`PARAMETRON_FREECAD_RUNTIME` selects the Engine-facing external executable.
When unset, Engine resolves `parametron-freecad` from `PATH`. A configured
value is an executable reference, not a shell command with arguments.

```bash
PARAMETRON_FREECAD_RUNTIME=/opt/parametron/bin/parametron-freecad \
  go run ./cmd/parametron --project ./project
```

Configuration loading starts no process. Executable lookup is deferred until a
CAD-runtime package needs dispatch; planning-only and non-CAD work do not require
an installed CAD runtime. An explicitly empty or invalid configured reference
fails when selected rather than falling back silently.

Engine invokes the external runtime capability. FreeCAD application discovery,
API interaction, mutation, recompute, native save, export, and native observation
belong to `parametron-freecad`. Engine owns verification and interpretation of
returned evidence. Application-specific runtime configuration belongs to that
runtime's documentation.

## Inspection and debugging

These root-command flags return before execution:

| Flag | Output |
| --- | --- |
| `--print-ast` | Validated AST JSON with expression trees. |
| `--json-plan` | JSON object containing `plan` and `hash`. |
| `--print-plan` | Human-readable steps, payloads, and plan hash. |
| `--dry-run` | Resolved plan and hash plus a no-execution message. |

All four use the shared input/validation/planning preparation, including
`--print-ast`; an error during that preparation can prevent AST output.
If combined, precedence is `--print-ast`, `--json-plan`, `--print-plan`,
then `--dry-run`. AST and JSON-plan modes suppress application logs.
`--debug`/`-d` enables debug logging otherwise.

```bash
go run ./cmd/parametron --file model.dsl --json-plan
```

The JSON plan is the resolved `ExecutionPlan`, not a report or a runtime result.
See [IR and planning](../authoring/ir-and-planning.md) and
[command families](command-families.md) for planning and command navigation.
