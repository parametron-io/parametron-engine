# DSL Semantics

This page defines evaluation and semantic behavior. See
[DSL overview](dsl-overview.md) for file structure and
[DSL grammar](dsl-grammar.md) for syntax and operator precedence.

## Bindings and evaluation

Each product has one local namespace shared by `let` and `param`. Duplicate
binding names and collisions with file-level constants are rejected. Bindings
may refer to other bindings in the same product, including forward references.
Validation detects cycles across the combined binding graph.

Parameters are exported product values; lets are internal computation.
Overrides target parameters only and replace their default expressions before
planning resolves the binding graph. Unknown override names, constant overrides,
and let overrides fail. Override text is converted according to the declared
parameter type; this boundary conversion does not imply implicit casts inside
DSL expressions.

The planner repeatedly evaluates bindings whose dependencies are available.
Unresolved dependencies cannot become arbitrary values. Only resolved parameters
enter CSV values and manifest parameter surfaces; lets may influence those values
without becoming exported bindings themselves.

## Identifiers and constants

Ordinary identifiers refer to product bindings or file-level constants.
Unknown identifiers in product expressions outside a contextual enum/action
domain are errors.
During planning, resolved product bindings are consulted before constants;
unresolved product bindings remain dependencies. Enum literals are accepted
only in an enum context with an explicit allowed-value definition.

Constants are resolved during validation from constant expressions and other
constants. Forward constant references are supported; cyclic references and
references to product parameters are rejected. Resolved constants retain their
types and values. In constant expressions, an otherwise unbound identifier is
treated as an enum literal; when used in an enum parameter, its value must belong
to that parameter's declared set. This constant-expression rule does not permit
unknown identifiers in ordinary product expressions.

Constants affect plan identity through the resolved plan and
selected profile settings, rather than through their declaration text alone.

## Types and operators

Numbers use `float64` values. String, boolean, and enum values remain distinct
types; expressions do not implicitly convert between them.

| Operation | Semantics |
| --- | --- |
| Arithmetic | Numeric operands for `+`, `-`, `*`, and `/`; division by zero fails during evaluation. |
| String addition | `string + string` concatenates; mixed string/numeric addition is invalid. |
| Numeric comparison | `<`, `<=`, `>`, and `>=` require numbers and return boolean values. |
| Equality | `==` and `!=` require matching number, string, boolean, or compatible enum types. Cross-type equality is invalid. |
| Logical operations | Boolean operands; `&&` and `||` short-circuit during evaluation. |
| Unary operations | Numeric negation and boolean `!` retain their respective type requirements. |
| Conditional | Boolean condition and compatible branch types; only the selected branch is evaluated. Both branches are validated. |

Enum parameters require an explicit, nonempty value definition. Their literals are
case-sensitive identifiers, not quoted strings. Enum equality requires the same
values in the same order. Enum values do not participate in arithmetic or
ordered numeric comparison.

String interpolation `{param:<name>}` resolves a string-valued product binding,
including a string-valued let. It does not stringify numbers, booleans, or enums.
String validation rejects control characters; literal and constant strings used
for path-like binding names also undergo safe-path validation.

## Numeric functions

All numeric built-ins validate argument count and numeric types.

| Function | Behavior |
| --- | --- |
| `min(a, b)`, `max(a, b)` | Select the smaller/larger value. |
| `abs(x)` | Absolute value. |
| `round(x)` | Nearest integer, with halfway cases rounded away from zero. |
| `floor(x)`, `ceil(x)` | Round toward negative/positive infinity. |
| `clamp(x, lo, hi)` | Bound the value inclusively; requires `lo <= hi`. |
| `round_up(x, step)`, `round_down(x, step)` | Round toward positive/negative infinity to a step multiple; require `step > 0`. |

Step rounding applies a `1e-12` tolerance to the quotient before rounding.
Invalid bounds or step sizes fail when statically known or during evaluation.

## Tables

`table_cell` resolves a selected cell during planning; it does not add a runtime
table step. Tables are supplied by logical ID through table-aware validation and
planning entry points. The table and column names are string literals; the row
key is a string expression resolved from current binding values.

Rows use exact key matching without trimming, case folding, or coercion. Unknown
tables/columns, missing rows, absent selected values, and invalid table schemas
fail. Duplicate row keys are rejected by table validation. The column's declared
string, number, or boolean type must match the receiving expression context.

The table-aware entry points are `dsl.ValidateWithTables`,
`planner.CreatePlanWithTables`, and `ir.CreatePlanFromIRWithTables`.
Non-table-aware entry points have no tables to resolve. See
[JSON table resources](../engine/json-table-resources.md) for table schema rules.

## Profiles and product execution intent

One profile is selected automatically when it is the only profile. Multiple
profiles require explicit `use profile` selection. Profiles carry settings
such as `file_pattern` and `output_dir`; their values resolve from supported
literal, constant, and array expressions. `file_pattern` must resolve to a
string, and invalid expansion or filename collisions fail during planning.
`output_dir` must be a safe relative path.

`adapter`, `source_model`, and `outputs` are product declarations; placing
them in a profile is a validation error.

- Supported adapters are `"freecad"` and `"none"`. Adapter and output-format
  strings are trimmed and case-normalized for validation.
- FreeCAD requires `source_model`. Standalone planning resolves its path
  relative to the DSL file; project mode resolves mapped model identity.
- FreeCAD output formats are `"step"`, `"csv"`, and `"pdf"`.
  `outputs = ["none"]` requests CAD execution with zero derived exports;
  `"none"` cannot be combined with any other output entry.
- `adapter = "none"` requests no CAD-runtime step and permits only omitted or
  empty outputs. Source/output declarations require an adapter declaration.
- Omitted FreeCAD outputs use the planner's default STEP intent. Explicit
  native-only intent is distinct from omission.

These declarations express requested work. They do not implement native CAD
operations. Output locations and command configuration belong to
[CLI runtime behavior](../cli/runtime-behavior.md).

## Authoring target actions

A target declaration expresses a desired action for a named target. The action
domain is exactly `keep`, `suppress`, `unsuppress`, `hide`, `unhide`,
and `delete`, with case-sensitive spelling and no aliases. Direct strings or
ordinary string/enum/number/boolean bindings do not coerce to action values.
Canonical action literals take precedence in an action context.

Typed ternaries and direct `table_cell` calls backed by string columns can
select actions. Only the selected table cell is checked against the action
domain; dynamic selection is evaluated after overrides and binding resolution.
This contextual table conversion does not make a string-valued let an action.

DSL validation checks expression types and action values. With a semantic model,
planning also resolves the target and validates its captured capabilities before
accepting mutation intent. Model-free planning does not fabricate target lookup
or capability checks. `keep` requires an existing target when semantic resolution
is available and contributes no mutation intent.

The [target-action contract](../reference/target-action-contract.md) is the
complete specification for resolution, capability rules, lowering, routing,
identity, and runtime handoff. Those details are not redefined here.

## Planning boundary

Validated expressions describe engineering intent. The
[IR and planning](ir-and-planning.md) layer resolves that intent into an ordered
execution plan. Capture schema fields are defined in the
[CAD capture contract](cad-contract.md); execution begins at the
[execution model](../architecture/execution-model.md).
