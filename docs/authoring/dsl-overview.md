# DSL Overview

The Parametron DSL is a declarative language for defining parametric models. A DSL file describes products, their parameters, constants, and output profiles. The engine parses this file, validates it, converts it to an IR, and generates an execution plan.

## File Structure

A DSL file contains the following top-level constructs, in this order:

1. **DSL version directive** (required, must be first meaningful token)
2. **Constants** (optional, global scope)
3. **Profiles** (optional)
4. **`use profile`** declaration (required when multiple profiles are defined)
5. **Products** with parameters

## DSL Version Directive

Every DSL file must declare a version. Supported version: `v1.0`.

Accepted forms:
```dsl
dsl v1.0
dsl 1.0
```

The directive must appear as the first meaningful token. BOM, whitespace, and comments may appear before it.

Errors: missing directive, unsupported version, directive not at top of file.

## Products and Parameters

```dsl
product <name> {
    let <name> = <expression>
    param <name>: <type> = <expression>
}
```

Products are the top-level model entities. Inside a product, `let` introduces a product-local binding and `param` declares a typed parameter. See `docs/authoring/dsl-grammar.md` for full syntax reference.

`let` bindings are internal to product evaluation. `param` bindings are the exported/public product bindings that appear on release-facing plan and manifest surfaces.

## Global Constants

```dsl
const NAME = expression
```

Constants are declared at global scope. They are evaluated at validation time (compile-time) and must be fully evaluable without runtime data. They may reference previously defined constants but cannot reference parameters.

Supported constant types: `number`, `string`, `boolean`, enum value.

Constants and parameters share a single namespace. A constant name cannot be redefined. Resolved constants are available to the planner as literal values; changing a constant value changes the plan hash.

## Profiles

```dsl
profile <Name> {
    key = <literal-or-const>
}
use profile <Name>
```

Profiles are configuration containers that control output behavior. A DSL file can define multiple profiles. When multiple profiles are present, a `use profile` declaration is required. When exactly one profile is defined, it is auto-selected.

Profile settings:
- `output_dir` (string): output directory, must be a safe relative path
- `metadata_enabled` (boolean)
- `file_pattern` (string): supports placeholders `{product}`, `{profile}`, `{plan_hash}`, `{param:<name>}`, `{const:<name>}`

## Product-Level Execution Declaration

Products may declare execution intent and target actions directly inside the `product {}` block:

```dsl
product <name> {
    adapter = "<adapter-id>"
    source_model = "<model-id-or-path>"
    outputs = ["<format>", ...]

    let <name> = <expression>
    param <name>: <type> = <expression>

    target <semantic-target>: action = <expression>
}
```

Execution declaration fields:
- `adapter` (string): the adapter to use for this product. Use `"freecad"` to activate the FreeCAD adapter pipeline. Use `"none"` to declare a product with no execution adapter.
- `source_model` (string): required when `adapter != "none"`. In project-mode flows, interpreted as a logical model ID and resolved to a physical path by the project mapping layer before planning; an unresolvable ID fails deterministically after DSL parse/validation and before plan generation. In standalone DSL flows, used as a physical path directly. Not validated for file existence at DSL parse time.
- `outputs` (list of strings): the output formats to produce. Must be a subset of the selected adapter's supported outputs. For FreeCAD: derived formats `"step"`, `"csv"`, `"pdf"`, or the mutually-exclusive native-only sentinel `["none"]`. For `adapter = "none"`: must be empty or absent. Unknown values produce a validation error.

These fields are defined per product. Profile settings (`output_dir`, `metadata_enabled`, `file_pattern`) remain at the profile level and are not affected.

`let` declarations are also allowed inside product blocks using `let <name> = <expression>`. They participate in evaluation like `param` declarations, but remain internal-only and are not exported on public plan or manifest surfaces.

## Target Action Declarations

```dsl
target <semantic-target>: action = <expression>
```

Examples:

```dsl
target Pad:
    action = suppress

target Pocket:
    action = removeHole ? suppress : unsuppress

target Chamfer: action = table_cell("variants", variant, "chamferAction")
```

Target-action declarations express intent for named CAD targets. `hide != suppress` and `unhide != unsuppress`: suppression is lifecycle intent, visibility is presentation intent, and deletion is structural intent. Engine does not prove native execution or GUI-rendered visibility.

Prefix-shaped ordinary parameter names create no target actions and trigger no target resolution. Only `target X: action = Y` authors target actions. Action-valued `let` is outside V1.

The current contract is:

- **Target Identifier & Semantic Resolution**: The `<semantic-target>` is the exact name of the targeted entity. Target kind (`feature`, `component`) is deliberately omitted from authoring syntax (e.g. `target Pad: action = suppress`, not `target feature Pad`). When semantic-model planning is active, the authored target name is resolved against the semantic model's combined `Feature` and `Component` exact `Name` fields using exact, case-sensitive matching with no kind precedence and no `DisplayName`, alias, or case-folding fallback. Zero candidates or multiple candidates (same-kind or cross-kind) fail deterministically.
- **Action Expression, Capability Gating, Semantic Mutation Lowering, Destination Routing, Family Projection & Aligned Manifest Projection**: The `action` expression RHS is validated against the strict canonical action domain (`keep`, `suppress`, `unsuppress`, `hide`, `unhide`, `delete`). Bare canonical action literals, typed ternary expressions (with boolean conditions and action branches), and ordinary string-backed table lookups (`table_cell`) are supported. After target resolution, the evaluated canonical action is validated against the resolved target's captured `Targetability` capabilities (`keep` requires existence only; `suppress`, `unsuppress`, `hide`, `unhide`, `delete` map to independent captured capability bits). Capability-approved actions lower into canonical semantic `MutationIntent` records (`keep` emits no intent; `suppress`, `unsuppress`, `hide`, `unhide`, `delete` emit exact `OperationKind` records with provenance-only `target_action` value sources), are deterministically routed into part/assembly buckets using CAD-native `Object` names resolved through established semantic/capture identity linkage, project into canonical Engine mutation families (`Suppression`, `Visibility`, `Deletion`), and emit aligned FreeCAD runtime manifests with schema `2.0` when actual target mutations exist (while mutation-less and keep-only manifests retain schema `1.0` compatibility, and executable scalar writes remain isolated to top-level `parameterAssignments`).
- **Table-Backed Actions**: A `table_cell` call may evaluate to an action when backed by an ordinary string table column (e.g. `chamferAction: string`). Table schema remains domain-neutral and generic. The selected string cell value must belong to the canonical six-action domain. Parameter overrides (e.g. `--set variant=B`) participate in row selection (e.g. `variant = "A"` -> `suppress`, `variant = "B"` -> `hide`). Unused rows in the same column are not validated against the action domain.
- **Type Strictness**: Action validation is case-sensitive and does not allow implicit coercion from direct strings, string bindings, enums, numbers, booleans, or constants.
- **Parser / Validator Split**: The parser accepts structurally valid expressions; DSL validation enforces the strict canonical action domain. Unsupported identifiers (e.g. `explode`) parse structurally but fail validation deterministically.
- **Product-Local Scope**: Target actions are product-local. Duplicate exact target declarations within the same product are rejected deterministically by the parser.
- **Declaration Order & Canonical Identity**: Authored declaration order is preserved in the AST. However, the authored order of equivalent target-action declarations is not part of the final Engine mutation identity. After semantic processing and routing, final planner mutation families are canonicalized before plan hashing, job derivation, and runtime manifest emission, ensuring declaration-order permutations converge. `keep` actions produce no runtime mutation and are identity-equivalent to declaration omission when all other execution inputs match.
- **Permanent Matrix Coverage & Planner Handoff**: The complete target-action authoring matrix — literals, ternary/table-backed expressions, exact Feature/Component resolution, capability validation, legacy prefix retirement, canonical projection, and deterministic runtime handoff — is covered by named permanent tests. Root `--json-plan` uses the same canonical planner result consumed by normal execution.

## Supported Parameter Types

| Type | Description |
|------|-------------|
| `number` | Floating-point numeric value |
| `string` | Text value, supports interpolation and concatenation |
| `boolean` | `true` or `false` |
| `enum { A, B, C }` | Closed set of named values |

## Comments

- Line comment: `//` until end of line
- Block comment: `/* ... */` (non-nestable)
- Comment markers inside string literals are not treated as comments.
- Unclosed block comment is a lex error.

## Related Documents

- `docs/authoring/dsl-grammar.md` — full syntax: operators, types, expressions, built-in functions
- `docs/authoring/dsl-semantics.md` — evaluation model, type rules, identifier resolution
- `docs/authoring/ir-and-planning.md` — what happens after parsing: IR and execution plan
- `docs/cli/validate.md` — how to validate a DSL file without executing it
