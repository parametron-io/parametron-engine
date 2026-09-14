# DSL Grammar

This document is the syntax reference for the Parametron DSL. It covers operators, types, expressions, built-in functions, and string features.

## Product Declarations

Inside a `product { ... }` block, the DSL currently supports:

- `let <name> = <expression>`
- `param <name>: <type> = <expression>`
- `target <semantic-target>: action = <expression>`
- `adapter = <expression>`
- `source_model = <expression>`
- `outputs = <expression>`

`let` is a product-local internal binding form. `param` is the exported/public binding form. In this slice, `let` syntax is intentionally narrow: no type annotation, no alternate keyword, and no mutability modifiers.

### Output Declarations and Native-Only Execution

`outputs` accepts an array of string literals declaring requested export formats:

- Standard derived export formats: `"step"`, `"csv"`, `"pdf"`, or supported multi-output combinations (e.g. `["step", "pdf"]`).
- Native-only execution sentinel: `outputs = ["none"]`.

When `outputs = ["none"]` is declared:
- `"none"` is an authoring-only sentinel representing native-only CAD execution (execute aligned CAD runtime, apply configuration and parameter mutations, recompute, persist native working document via runtime lifecycle, and request zero derived exports).
- `"none"` is an authoring-only sentinel, **not** an export format, filename, extension, or artifact type.
- `"none"` is **mutually exclusive** with all other output declarations: `outputs = ["none"]` is valid, while combinations such as `["none", "step"]`, `["step", "none"]`, `["none", "pdf"]`, `["pdf", "none"]`, `["none", "csv"]`, `["csv", "none"]`, or `["none", "none"]` are validation errors.

## Expression Parser

Expressions use a Pratt parser (Top-Down Operator Precedence). The single entry point is `parseExpression(rbp int)`. Operator precedence is controlled by binding power. The parser is deterministic with single-token lookahead and no backtracking.

## Operator Precedence

From lowest to highest:

| Level | Operator | Associativity |
|-------|----------|---------------|
| 1 | `?:` (ternary) | right-to-left |
| 2 | `\|\|` (logical OR) | left-to-right |
| 3 | `&&` (logical AND) | left-to-right |
| 4 | `== != < > <= >=` (comparison) | left-to-right |
| 5 | `+ -` (addition/subtraction) | left-to-right |
| 6 | `* /` (multiplication/division) | left-to-right |
| 7 | `! -` (unary) | right-to-left |
| 8 | `()` (grouping) | — |

## Operators

### Arithmetic
`+ - * /` — operands must be `number`. Addition (`+`) is also used for string concatenation when both operands are `string`.

### Comparison
`== != < > <= >=`

For `<`, `>`, `<=`, `>=`: operands must be `number`.

For `==` and `!=`: strict type matrix (no implicit conversions):
- `bool == bool` ✓
- `string == string` ✓
- `number == number` ✓
- `enum == enum` (same type definition) ✓
- Any cross-type comparison ✗

Validation precedence for `==` and `!=`: enum > string > boolean > number.

### Logical
`&&` and `||` — both operands must be `boolean`. Short-circuit evaluation applies.

### Ternary
```
condition ? expr_if_true : expr_if_false
```
Condition must be `boolean`. Both branches must be the same type. Right-associative.

### Unary
- `-` (numeric negation): operand must be `number`.
- `!` (logical NOT): operand must be `boolean`, returns `boolean`.

Unary operators bind tighter than arithmetic.

## Types

### number
Floating-point (float64). Used for all numeric values. Numeric comparison operators apply.

### string
Text value. Supports:
- Concatenation with `+` (both operands must be `string`)
- Interpolation via `{param:<name>}` placeholders (referenced parameter must be `string`)
- Escape sequences: `\\`, `\"`, `\{`, `\}`, `\n`, `\t`
- Unrecognized escape sequences are a parse error.

### boolean
`true` or `false`.

### enum
```dsl
param <name>: enum { Identifier, Identifier, ... } = <expression>
```
- Values are comma-separated identifiers (not quoted strings).
- Empty enum body is invalid.
- Values must be unique within the definition.
- Enum values are case-sensitive.
- Enum values can only participate in `==` and `!=` comparisons. Arithmetic is not allowed.
- Enum comparison requires both operands to share the same enum type definition. Type identity is determined by the definition content (not the parameter name). Value ordering matters.
- CLI overrides accept enum values with or without quotes. Whitespace trimmed, double quotes removed, case-sensitive match.

## Product-Local Bindings

```dsl
let <name> = <expression>
```

- `let` is valid only inside `product { ... }`.
- `let` participates in product evaluation but is not exported on public plan or manifest surfaces.
- Type annotations such as `let x: number = 5` are not part of the grammar in this slice.

## Built-in Math Functions

All built-in functions operate on `number` (float64) and return `number`. Nesting is supported. Functions are pure and evaluated deterministically during planning.

| Function | Signature | Notes |
|----------|-----------|-------|
| `min` | `min(a, b)` | |
| `max` | `max(a, b)` | |
| `abs` | `abs(x)` | |
| `round` | `round(x)` | |
| `floor` | `floor(x)` | |
| `ceil` | `ceil(x)` | |
| `clamp` | `clamp(x, lo, hi)` | requires `lo <= hi` |
| `round_up` | `round_up(x, step)` | requires `step > 0`; epsilon-stabilized |
| `round_down` | `round_down(x, step)` | requires `step > 0`; epsilon-stabilized |

Validation is performed at compile time: function existence, argument count, argument types.

Error examples:
- `unknown function 'foo'`
- `'max' expects 2 arguments but got 1`
- `argument of 'abs': expected number literal but got type string`

## Table Lookup Function

`table_cell(table_name, row_key, column_name)` performs a planner-stage lookup against a named table resource.

| Argument | Kind | Type |
|----------|------|------|
| `table_name` | string literal | `string` |
| `row_key` | string expression | `string` |
| `column_name` | string literal | `string` |

- `table_name` must be a string literal (not a reference or expression).
- `row_key` is a string expression; it is fully resolved at planning time before the lookup is performed.
- `column_name` must be a string literal (not a reference or expression).
- The return type is inferred from the declared column type in the table schema: `string`, `number`, or `boolean`.
- Arity must be exactly 3. Any other argument count is a validation error.

Table existence and column existence are validated in table-aware flows (`ValidateWithTables`, `CreatePlanWithTables`, `CreatePlanFromIRWithTables`). In the standard (non-table-aware) flow, `table_cell` calls are not resolved.

Strict column-to-target type matching is enforced. No implicit casts are applied. The resolved value must satisfy the declared column type exactly.

Validation errors:
- Incorrect argument count
- First or third argument is not a string literal
- Row key expression does not resolve to `string`
- Table not found (table-aware flow)
- Column not found (table-aware flow)
- Type mismatch between resolved column value and declared column type

## File Pattern Placeholders

Used in profile `file_pattern` setting:

This section covers placeholder syntax only. Validation rules for `file_pattern`, resolved filename checks, and collision behavior are documented in `docs/authoring/dsl-semantics.md`.

| Placeholder | Resolves to |
|-------------|-------------|
| `{product}` | Product name |
| `{profile}` | Profile name |
| `{plan_hash}` | Plan hash |
| `{param:<name>}` | Resolved parameter value |
| `{const:<name>}` | Resolved constant value (must be `string` type) |

Unknown placeholder → planning error.

For `{const:<name>}`:
- Constant must be declared in same file and resolve to `string` type.
- Empty name → `"invalid placeholder '{const:}'"`.
- Missing colon → `"unknown placeholder '{const}'"`.
- Extra segment → `"invalid placeholder '...'"`.

Deterministic parameter formatting: `number` uses stable float representation, `bool` is `true`/`false`, `enum` and `string` use the raw value.

## Identifier Rules

An identifier is valid only as:
- A parameter reference
- A constant reference
- An enum literal (only within an enum context)

Unknown identifiers outside an enum context are rejected. The fallback of treating unknown identifiers as enum literals was removed in Phase 6.

## Related Documents

- `docs/authoring/dsl-overview.md` — file structure and top-level constructs
- `docs/authoring/dsl-semantics.md` — evaluation model, dependency resolution, type rules, table lookup semantics
- `docs/engine/json-table-resources.md` — JSON table resource format and validation rules
- `docs/reference/target-action-contract.md` — target-action semantics,
  resolution, capability validation, mutation routing, and runtime contract
