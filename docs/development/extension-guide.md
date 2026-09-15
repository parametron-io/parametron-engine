# Extension Guide

This document describes how to extend Parametron Engine: adding new syntax to the
DSL, introducing new step types, and implementing new CAD adapters.

## Extending the DSL

When introducing new syntax or operators to the DSL, update the authoring pipeline
in the following order:

1. **AST Definition (`internal/authoring/dsl/ast.go`)**:
   Add the new AST node struct or expression type.
2. **Parser (`internal/authoring/dsl/parser.go`)**:
   Implement Pratt parselet dispatch (`nud` for prefix/nullary expressions, `led`
   for infix/postfix operators).
3. **Semantic Validator (`internal/authoring/dsl/validator.go`)**:
   Add semantic checks, type inference rules, and diagnostic reporting for the new
   construct.
4. **Intermediate Representation (`internal/authoring/ir/`)**:
   - `converter.go`: Map the new AST node to an `IRNode`.
   - `codegen.go`: Implement IR-to-DSL formatting to support serialization roundtrips.
   - `planner.go`: Implement IR-to-plan evaluation for the new construct.
5. **Planner Payloads (`internal/authoring/planner/`)**:
   If the language feature changes execution step semantics, define or update the
   corresponding step payload.

### DSL Design Constraints

When adding new language constructs, preserve the following core design principles:
- **No untyped identifiers**: Identifiers must resolve explicitly to constants,
  parameters, enums, or built-in functions.
- **Strict type checking**: No silent coercion between incompatible types.
- **Deterministic evaluation**: Expressions must produce identical results
  regardless of map iteration order or environment state.

### Verification Gate

After modifying the DSL, run the regression gate to verify syntax and planning:

```bash
go run ./cmd/parametron --file testdata/dsl/stress.dsl --print-ast
go run ./cmd/parametron --file testdata/dsl/stress.dsl --print-plan
go run ./cmd/parametron --file testdata/dsl/stress.dsl --json-plan
```

## Adding New Step Types

To introduce a new execution step type:

1. **Step Definition (`internal/authoring/planner/step.go`)**:
   Define the new `StepType` constant and payload struct.
2. **Product Key Derivation (`internal/engine/job/job.go`)**:
   Update `job.ProductKeyFromStep` to associate the step with a product key if
   applicable.
3. **Handoff Packaging (`internal/engine/handoff/handoff.go`)**:
   Update `handoff.FromJob` and `Package.Plan()` to validate and roundtrip the
   step snapshot.
4. **Expected Artifacts (`internal/engine/handoff/`)**:
   Update `Package.ExpectedArtifacts()` if the step declares output files.
5. **Adapter Dispatch (`internal/engine/adapter/` or `internal/engine/executor/`)**:
   Implement execution handling in the target adapter's `Run` method or dedicated
   step orchestrator.

## Adding a New CAD Adapter

To add support for a new CAD tool:

1. **Implement the Adapter Interface (`internal/engine/adapter/`)**:
   ```go
   type Adapter interface {
       Run(ctx context.Context, step planner.Step) error
   }
   ```
2. **Adapter Package (`internal/engine/adapter/<name>/`)**:
   Implement adapter-specific manifest projection and path templates.
3. **Profile Validation (`internal/authoring/dsl/validator.go`)**:
   Add the adapter name to allowed profile targets (alongside `"freecad"` and
   `"none"`).
4. **Runtime Wrapper (`internal/engine/cadruntime/`)**:
   If the adapter supports external subprocess orchestration, implement the
   attempt management and evidence intake contracts following the FreeCAD
   reference implementation.

## Related Documents

Current public DSL grammar, semantics, IR/planning, and adapter architecture
documentation is maintained in `parametron-docs`. See the canonical
[Engine authoring documentation](https://github.com/parametron-io/parametron-docs/tree/main/docs/engine/authoring)
and [Engine adapter documentation](https://github.com/parametron-io/parametron-docs/blob/main/docs/engine/adapters/README.md).

- [Testing Strategy](testing-strategy.md) — Test layers and verification commands.
