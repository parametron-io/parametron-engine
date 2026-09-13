# parametron sweep

`parametron sweep` expands the Cartesian product of specified parameter values and validates planning for each combination. In the current version (v1), sweep is planning-only — no execution occurs.

## Usage

```bash
# Standalone DSL entrypoint
parametron sweep --file <dsl-path> --product <name> --param <spec> [--param <spec> ...] [flags]

# Project-based entrypoint
parametron sweep --project <project-dir | parametron.project.json> --product <name> --param <spec> [--param <spec> ...] [flags]
```

`--file` and `--project` are mutually exclusive; exactly one must be provided.

## Flags

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--file` | `-f` | One of | Path to a DSL file (standalone DSL entrypoint); mutually exclusive with `--project` |
| `--project` | | One of | Path to a project directory or `parametron.project.json` file (project-based entrypoint); mutually exclusive with `--file` |
| `--product` | | Yes | Product name to sweep |
| `--param` | | Yes | Parameter spec, repeatable |
| `--out` | `-o` | No | Output directory for `sweep_report.json` |
| `--debug` | `-d` | No | Enable debug logging |

## Parameter Spec Syntax

Each `--param` declaration specifies a parameter and its value set:

**Range** (numeric parameters only):
```
name=start..end:step
```
Example: `width=100..500:50`

Requirements: `start <= end`, `step > 0`.

**List** (any type):
```
name=[A,B,C]
```
Example: `material=[Steel,Aluminium,Titanium]`

## Cartesian Product

Multiple `--param` declarations produce the full Cartesian product. For example:
- `--param width=[100,200]`
- `--param height=[50,100,150]`

Produces 6 cases: every combination of `width` and `height`.

## Validation and Rejection

Sweep rejects:
- Malformed spec syntax or missing `=`
- Range spec on a non-numeric parameter
- Non-numeric range boundary values
- Zero or negative step in range
- `end < start` in range
- Empty list
- Empty list item
- Duplicate `--param` for the same parameter name
- Parameter not defined in the specified DSL product

## Outputs

A `sweep_report.json` is written to `--out`.

## sweep_report.json Schema

```json
{
  "schemaVersion": "1.0",
  "product": "<product name>",
  "params": ["width", "height"],
  "totalCases": 6,
  "passedCases": 6,
  "failedCases": 0,
  "cases": [
    {
      "index": 0,
      "inputs": { "width": 100, "height": 50 },
      "status": "success",
      "planHash": "<hash>"
    }
  ]
}
```

`failureCountsByMessage` is included only when there are failures, mapping error message to count.

`error` field appears on failing cases.

## Related Documents

- `docs/cli/command-families.md` — command routing
- `docs/authoring/dsl-semantics.md` — parameter type rules
