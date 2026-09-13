// Package ir provides the Intermediate Representation (IR) layer for the Parametron DSL.
//
// The IR layer sits between the AST (parser/lexer dependent) and the ExecutionPlan,
// providing a serializable, semantic-validated, and language-independent representation
// of the DSL program.
//
// Key features:
//   - Parser/lexer independent (no text binding)
//   - Semantic validation guaranteed
//   - Expression trees preserved for Designer editing
//   - Constants fully resolved at compile time
//   - JSON serializable for REST API
//
// This layer enables roundtrip conversion (DSL ↔ IR ↔ UI) and serves as the foundation
// for distributed planning and execution.
package ir

// IRProgram is the root IR node representing a complete DSL program.
type IRProgram struct {
	Version           string                `json:"version"`           // IR schema version (e.g., "1.0")
	Constants         map[string]IRConstant `json:"constants"`         // Resolved constant values
	Profiles          []IRProfile           `json:"profiles"`          // Profile definitions (unresolved expressions)
	ActiveProfileName *string               `json:"activeProfileName"` // Selected profile
	Products          []IRProduct           `json:"products"`          // Product definitions
}

// IRConstant represents a fully resolved compile-time constant.
// Constants are evaluated during AST validation and stored with their concrete values.
type IRConstant struct {
	Name  string      `json:"name"`
	Type  IRType      `json:"type"`
	Value interface{} `json:"value"` // Concrete value (float64, string, bool)
}

// IRProfile represents a named profile configuration with unresolved expressions.
// Profile settings are resolved at plan creation time based on the active profile.
type IRProfile struct {
	Name     string                  `json:"name"`
	Settings map[string]IRExpression `json:"settings"` // e.g., "output_dir", "file_pattern"
}

// IRProduct represents a product definition with its parameters.
type IRProduct struct {
	Name       string        `json:"name"`
	Lets       []IRLet       `json:"lets,omitempty"`
	Parameters []IRParameter `json:"parameters"`
}

// IRLet represents a product-local internal binding within a product.
type IRLet struct {
	Name  string       `json:"name"`
	Value IRExpression `json:"value"`
}

// IRParameter represents a parameter definition within a product.
type IRParameter struct {
	Name         string       `json:"name"`
	Type         IRType       `json:"type"`
	DefaultValue IRExpression `json:"defaultValue"` // Expression tree, not resolved value
}

// IRType represents rich type information for parameters and constants.
type IRType struct {
	Kind       IRTypeKind `json:"kind"`                 // number, string, boolean, enum
	EnumValues []string   `json:"enumValues,omitempty"` // Only for enum type
}

// IRTypeKind defines the supported type kinds.
type IRTypeKind string

const (
	// IRTypeNumber represents numeric values (stored as float64).
	IRTypeNumber IRTypeKind = "number"
	// IRTypeString represents string values.
	IRTypeString IRTypeKind = "string"
	// IRTypeBoolean represents boolean values.
	IRTypeBoolean IRTypeKind = "boolean"
	// IRTypeEnum represents enumeration values.
	IRTypeEnum IRTypeKind = "enum"
)

// IRExpression represents an expression tree using a discriminated union pattern.
// The Kind field determines which other fields are valid.
// This structure is designed to be JSON-friendly for API serialization.
type IRExpression struct {
	Kind IRExpressionKind `json:"kind"`

	// Literal - valid when Kind == ExprKindLiteral
	LiteralValue interface{} `json:"literalValue,omitempty"`

	// Reference - valid when Kind == ExprKindReference
	ReferenceName string  `json:"referenceName,omitempty"`
	ReferenceType RefType `json:"referenceType,omitempty"` // "param", "constant", "enum"

	// Interpolated String - valid when Kind == ExprKindInterpolatedString
	InterpolatedSegments []IRInterpolatedSegment `json:"interpolatedSegments,omitempty"`

	// Binary - valid when Kind == ExprKindBinary
	BinaryOp    string        `json:"binaryOp,omitempty"`
	BinaryLeft  *IRExpression `json:"binaryLeft,omitempty"`
	BinaryRight *IRExpression `json:"binaryRight,omitempty"`

	// Unary - valid when Kind == ExprKindUnary
	UnaryOp      string        `json:"unaryOp,omitempty"`
	UnaryOperand *IRExpression `json:"unaryOperand,omitempty"`

	// Ternary - valid when Kind == ExprKindTernary
	TernaryCond  *IRExpression `json:"ternaryCond,omitempty"`
	TernaryTrue  *IRExpression `json:"ternaryTrue,omitempty"`
	TernaryFalse *IRExpression `json:"ternaryFalse,omitempty"`

	// Function Call - valid when Kind == ExprKindCall
	FuncName string         `json:"funcName,omitempty"`
	FuncArgs []IRExpression `json:"funcArgs,omitempty"`
}

// IRExpressionKind identifies the type of expression.
type IRExpressionKind string

const (
	// ExprKindLiteral represents a literal value (number, string, boolean).
	ExprKindLiteral IRExpressionKind = "literal"
	// ExprKindReference represents a reference to a parameter, constant, or enum value.
	ExprKindReference IRExpressionKind = "reference"
	// ExprKindInterpolatedString represents a string literal with {param:name} placeholders.
	ExprKindInterpolatedString IRExpressionKind = "interpolated_string"
	// ExprKindBinary represents a binary operation (+, -, *, /, ==, !=, <, <=, >, >=, &&, ||).
	ExprKindBinary IRExpressionKind = "binary"
	// ExprKindUnary represents a unary operation (-, !).
	ExprKindUnary IRExpressionKind = "unary"
	// ExprKindTernary represents a ternary conditional expression (cond ? true : false).
	ExprKindTernary IRExpressionKind = "ternary"
	// ExprKindCall represents a function call.
	ExprKindCall IRExpressionKind = "call"
)

// RefType identifies the type of reference in an ExprKindReference.
type RefType string

const (
	// RefTypeParam references a product parameter.
	RefTypeParam RefType = "param"
	// RefTypeLet references a product-local let binding.
	RefTypeLet RefType = "let"
	// RefTypeConstant references a global constant.
	RefTypeConstant RefType = "constant"
	// RefTypeEnumValue references an enum value literal.
	RefTypeEnumValue RefType = "enum"
)

// IsValid returns true if the IRTypeKind is a valid type kind.
func (k IRTypeKind) IsValid() bool {
	switch k {
	case IRTypeNumber, IRTypeString, IRTypeBoolean, IRTypeEnum:
		return true
	}
	return false
}

// IsValid returns true if the IRExpressionKind is a valid expression kind.
func (k IRExpressionKind) IsValid() bool {
	switch k {
	case ExprKindLiteral, ExprKindReference, ExprKindInterpolatedString, ExprKindBinary, ExprKindUnary, ExprKindTernary, ExprKindCall:
		return true
	}
	return false
}

// IRInterpolatedSegment is one segment of an interpolated string expression.
// If ParamName is set, the segment is a parameter placeholder; otherwise Text is literal text.
type IRInterpolatedSegment struct {
	Text      string `json:"text,omitempty"`
	ParamName string `json:"paramName,omitempty"`
}

// IsValid returns true if the RefType is a valid reference type.
func (r RefType) IsValid() bool {
	switch r {
	case RefTypeParam, RefTypeLet, RefTypeConstant, RefTypeEnumValue:
		return true
	}
	return false
}
