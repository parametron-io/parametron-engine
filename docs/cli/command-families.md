# CLI Command Families

The `parametron` CLI has two command families that coexist without conflict.

## Common Usage

Small DSL example:

```dsl
product Widget {
  width: number = 100
}
```

Representative commands:

```bash
# Standalone DSL entrypoint
parametron validate --file widget.dsl
parametron --file widget.dsl --print-plan
parametron --file widget.dsl --json-plan
parametron simulate --file widget.dsl --inputs cases.json

# Project-based entrypoint
parametron validate --project my-project/
parametron --project my-project/ --dry-run
```

- `validate` is the quickest way to check parse, validation, and planning without execution. See `docs/cli/validate.md`.
- Legacy root mode with `--print-plan` shows the planned steps in a human-readable form.
- Legacy root mode with `--json-plan` emits the resolved execution plan JSON for tooling or inspection.
- The `simulate` harness command runs multiple input cases through the full pipeline and writes `simulate_report.json`. See `docs/cli/simulate.md`.

## Legacy Root Mode

Invoked without a recognized harness command name. Accepts either `--file` (standalone DSL) or `--project` (project-based) and runs the full execution pipeline. Exactly one must be provided; they are mutually exclusive.

```bash
parametron --file model.dsl [flags]
parametron --project <project-dir | parametron.project.json> [flags]
```

Legacy flags:
- `--file / -f`: DSL file path (standalone DSL execution); mutually exclusive with `--project`
- `--project`: project directory or `parametron.project.json` path (project-based execution); mutually exclusive with `--file`
- `--set`: override a parameter value (`key=value`, repeatable)
- `--table`: load a JSON table from disk (`logical-id=path`, repeatable)
- `--out / -o`: base output directory (default `./output`)
- `--dry-run`: run full pipeline, print plan with resolved values, skip file writing and execution
- `--print-plan`: parse, validate, create plan, print steps and payloads — no execution
- `--print-ast`: parse and validate DSL, print raw AST as JSON — no planning or execution
- `--json-plan`: emit fully resolved execution plan as JSON to stdout — no execution
- `--scripts-dir`: directory for Python runner scripts
- `--model-hash`: string identifying the CAD model version (affects cache key computation)
- `--debug / -d`: enable debug logging

## Harness Commands

Explicit named commands in the harness command tree. Engine-independent. All operate on the parse → validate → plan → execute pipeline as appropriate for each command.

| Command | Purpose |
|---------|---------|
| `validate` | Parse, validate, and generate execution plan. No execution. |
| `simulate` | Run multiple input cases through the full pipeline. Produces a deterministic summary. |
| `sweep` | Expand Cartesian product of parameter values and validate planning for each. No execution. |
| `snapshot` | Execute a single case and capture a deterministic snapshot package. |
| `diff` | Compare two plan files or two snapshot directories. |
| `sync` | Create or refresh `parametron.lock.json` for a project. Project-mode only. |

All harness commands accept `--debug / -d`. See individual command documents for their specific flags.

### Command: sync

**Purpose**: Create or refresh `parametron.lock.json` from the current project mapping and captured resources.

**Required input**: a project entrypoint — either a project directory or a `parametron.project.json` file. Standalone DSL input is rejected.

```bash
parametron sync --project <project-dir>
parametron sync --project <project-dir>/parametron.project.json
parametron sync --file <project-dir>          # legacy form, still accepted
```

**Supported entrypoints**:
- `--project <path>` (preferred): accepts a project directory or a `parametron.project.json` file directly.
- `--file <path>` (legacy): same resolution; both forms are accepted and behave identically.
- Standalone DSL input is rejected regardless of flag used.

**Output location**: `<project-root>/parametron.lock.json`. The file is written atomically; if it already exists it is overwritten.

**What the command does**:
1. Loads the project mapping from the resolved entrypoint.
2. Captures current project resources via the `projectinput` boundary: DSL SHA-256, model resource SHA-256 hashes (ordered by logical ID), and table semantic fingerprints.
3. Calls `projectlock.Build` with the project mapping and captured resources to produce the lock.
4. Writes the result atomically to `<project-root>/parametron.lock.json`.

**Deterministic behavior**: identical project mapping and identical resource content produce byte-identical lock file output on every invocation.

**Failure conditions**:
- Standalone DSL path supplied instead of a project entrypoint: rejected immediately.
- Missing or invalid `parametron.project.json`: propagates as a deterministic project-loading error.
- Missing DSL file or missing mapped model file: fails early and deterministically before any lock content is written.

**Scope**:
- The command always performs a full rebuild of the lock from current resources. No diff, partial update, or staleness detection is performed.
- There is no automatic invocation; the user must run `parametron sync` explicitly.
- Lock file presence is not required or enforced at execution time.

## Routing Logic

`main.go` inspects the first non-flag argument:
- If it matches a recognized harness command name → harness tree.
- Otherwise → legacy root mode.

Persistent flags are registered on both roots via `bindPersistentFlags`.

## Shared Execution Infrastructure

Both families use shared utilities from `cmd/parametron/cli_helpers.go`:

- `loadPlannedRun(dslPath, overrides, args)`: runs parse → validate → plan, returns a `plannedRun` struct.
- `executePlanRun(opts)`: runs the scheduler, writes `report.json`, and on success writes `manifest.json` and `metadata.json`. Cache is bypassed when `UseCache: false`.
- `writeJSONFile`: writes JSON atomically using a temp file and rename.

`simulate` and `snapshot` both call `executePlanRun`. `validate` and `sweep` call only `loadPlannedRun`.

## Related Documents

- `docs/cli/validate.md`
- `docs/cli/simulate.md`
- `docs/cli/sweep.md`
- `docs/cli/snapshot.md`
- `docs/cli/diff.md`
- `docs/cli/runtime-behavior.md` — output directory strategy, cache behavior, flag reference
