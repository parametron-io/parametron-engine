// Package ir provides the Intermediate Representation (IR) layer for the Parametron DSL.
//
// # Purpose
//
// The IR layer sits between the AST (Abstract Syntax Tree) and the ExecutionPlan,
// providing a serializable, semantic-validated, and language-independent representation
// of the DSL program. It serves as the foundation for:
//
//   - Roundtrip conversion (DSL ↔ IR ↔ UI) for the Designer
//   - JSON serialization for REST API communication
//   - DAG partitioning for distributed planning
//   - Caching and incremental builds
//
// # Architecture
//
// The IR package consists of the following components:
//
//	types.go     - Core IR type definitions (IRProgram, IRExpression, etc.)
//	converter.go - Converts validated AST to IR
//	codegen.go   - Generates DSL source code from IR
//	planner.go   - Creates ExecutionPlan from IR
//	json.go      - JSON serialization and validation
//
// # Key Features
//
// 1. Parser/Lexer Independent: IR is not bound to the text representation
// 2. Semantic Validation: All type checking and constant folding is complete
// 3. Expression Trees: Preserved for Designer editing capabilities
// 4. Constant Resolution: All constants have concrete values
// 5. JSON Serializable: Enables REST API and persistence
//
// # Type System
//
// IR supports the following types:
//   - number  (float64)
//   - string  (string)
//   - boolean (bool)
//   - enum    (string with allowed values)
//
// # Expression System
//
// IR expressions use a discriminated union pattern via the Kind field:
//   - literal   - Literal values (numbers, strings, booleans)
//   - reference - References to parameters, constants, or enum values
//   - binary    - Binary operations (+, -, *, /, ==, !=, <, <=, >, >=, &&, ||)
//   - unary     - Unary operations (-, !)
//   - ternary   - Conditional expressions (cond ? true : false)
//   - call      - Function calls (max, min, abs, round)
//
// # Usage
//
// Convert AST to IR:
//
//	ast, err := dsl.Parse("input.dsl")
//	if err != nil { /* handle */ }
//
//	if err := dsl.Validate(ast); err != nil { /* handle */ }
//
//	program, err := ir.ConvertAST(ast)
//	if err != nil { /* handle */ }
//
// Generate DSL from IR:
//
//	dslCode := ir.GenerateDSL(program)
//
// Create ExecutionPlan from IR:
//
//	plan, err := ir.CreatePlanFromIR(program, overrides)
//	if err != nil { /* handle */ }
//
// Serialize to JSON:
//
//	jsonData, err := program.ToJSON()
//	if err != nil { /* handle */ }
//
// Deserialize from JSON:
//
//	program, err := ir.FromJSON(jsonData)
//	if err != nil { /* handle */ }
//
// # Determinism
//
// IR generation is deterministic: the same DSL input always produces the same IR.
// Code generation is also deterministic: constants are sorted alphabetically,
// and output follows a consistent format with 4-space indentation.
//
// # Versioning
//
// The IR format is versioned via the Version field in IRProgram.
// Current version: "1.0"
package ir
