package dsl

// Node is the base interface for all nodes in the Abstract Syntax Tree.
type Node interface {
	// Marker method to ensure type safety for AST nodes
	isNode()
}

// AST represents the root of the parsed DSL document.
type AST struct {
	DSLVersion        string
	SourcePath        string `json:"-"`
	Constants         []*ConstDeclarationNode
	Profiles          []*ProfileDeclarationNode
	ActiveProfileName *string
	Products          []*ProductNode
	ResolvedConstants map[string]ConstantValue `json:"-"`
}

func (a *AST) isNode() {}

// ConstantValue stores the compile-time evaluated value and inferred type of a constant.
type ConstantValue struct {
	Type  ParameterType
	Value interface{}
}

// ProductNode represents a high-level product definition.
// Example: product MyTable { ... }
type ProductNode struct {
	Name                      string
	Lets                      []*LetNode
	Parameters                []*ParameterNode
	TargetActions             []*TargetActionNode
	Adapter                   ExpressionNode
	SourceModel               ExpressionNode
	Outputs                   ExpressionNode
	ExecutionDeclarationOrder []string `json:"-"`
	// Future: Rules, Constraints, and Output mappings can be added here.
}

func (p *ProductNode) isNode() {}

// TargetActionNode represents an action expression authored for a semantic target.
// Example: target Pad: action = suppress
type TargetActionNode struct {
	SemanticTarget string
	Action         ExpressionNode
}

func (t *TargetActionNode) isNode() {}

// LetNode represents a product-local binding inside a product.
// Example: let area = width * height
type LetNode struct {
	Name  string
	Value ExpressionNode
}

func (l *LetNode) isNode() {}

// ConstDeclarationNode represents a global constant declaration.
// Example: const BaseWidth = 1200
type ConstDeclarationNode struct {
	Name  string
	Value ExpressionNode
}

func (c *ConstDeclarationNode) isNode() {}

// ProfileDeclarationNode represents a named profile configuration block.
// Example:
//
//	profile Production {
//	  output_dir = "out/prod"
//	  metadata_enabled = true
//	}
type ProfileDeclarationNode struct {
	Name string
	// Settings stores final key -> expression mapping.
	Settings map[string]ExpressionNode
	// SettingOrder keeps insertion order and preserves duplicates for validation.
	SettingOrder []string `json:"-"`
}

func (p *ProfileDeclarationNode) isNode() {}

// ParameterTypeKind defines the strong types supported by the DSL.
type ParameterTypeKind string

const (
	ParamTypeNumber  ParameterTypeKind = "number"
	ParamTypeString  ParameterTypeKind = "string"
	ParamTypeBoolean ParameterTypeKind = "boolean"
	ParamTypeEnum    ParameterTypeKind = "enum"
	ParamTypeAction  ParameterTypeKind = "action"
)

// ParameterType represents the type of a parameter, potentially including enum values.
type ParameterType struct {
	Kind       ParameterTypeKind `json:"Kind"`
	EnumValues []string          `json:"EnumValues,omitempty"`
}

func (pt ParameterType) String() string {
	return string(pt.Kind)
}

// ParameterNode represents a parameter definition inside a product.
// Example: length: number = 1200
type ParameterNode struct {
	Name         string
	Type         ParameterType
	DefaultValue ExpressionNode // Can be a literal, identifier, or binary expression.
}

func (p *ParameterNode) isNode() {}

// ExpressionNode is a placeholder interface for future logic implementation.
// It will allow DefaultValue or Rule definitions to contain complex expressions.
type ExpressionNode interface {
	Node
	isExpression()
}

// LiteralExpression represents a literal value like a number, string, or boolean.
type LiteralExpression struct {
	Value interface{}
}

func (l *LiteralExpression) isNode()       {}
func (l *LiteralExpression) isExpression() {}

// InterpolatedStringSegment is one segment in an interpolated string expression.
// If ParamName is set, this segment references a parameter; otherwise Text is literal text.
type InterpolatedStringSegment struct {
	Text      string
	ParamName string
}

// InterpolatedStringExpression represents a string literal with {param:name} placeholders.
type InterpolatedStringExpression struct {
	Segments []InterpolatedStringSegment
}

func (i *InterpolatedStringExpression) isNode()       {}
func (i *InterpolatedStringExpression) isExpression() {}

// IdentifierExpression represents a reference to another parameter.
type IdentifierExpression struct {
	Name string
}

func (i *IdentifierExpression) isNode()       {}
func (i *IdentifierExpression) isExpression() {}

// BinaryExpression represents an operation between two expressions.
type BinaryExpression struct {
	Left     ExpressionNode
	Operator string
	Right    ExpressionNode
}

func (b *BinaryExpression) isNode()       {}
func (b *BinaryExpression) isExpression() {}

// UnaryExpression represents a unary operation (e.g. -x, !b).
type UnaryExpression struct {
	Operator string
	Operand  ExpressionNode
}

func (u *UnaryExpression) isNode()       {}
func (u *UnaryExpression) isExpression() {}

// TernaryExpression represents a conditional expression.
// Example: condition ? value_if_true : value_if_false
type TernaryExpression struct {
	Condition ExpressionNode
	TrueExpr  ExpressionNode
	FalseExpr ExpressionNode
}

func (t *TernaryExpression) isNode()       {}
func (t *TernaryExpression) isExpression() {}

// FunctionCallExpression represents a call to a built-in function.
// Example: max(a, b)
type FunctionCallExpression struct {
	Name string
	Args []ExpressionNode
}

func (f *FunctionCallExpression) isNode()       {}
func (f *FunctionCallExpression) isExpression() {}

// ArrayExpression represents an array literal expression.
// Example: ["step", "stl"]
type ArrayExpression struct {
	Elements []ExpressionNode
}

func (a *ArrayExpression) isNode()       {}
func (a *ArrayExpression) isExpression() {}
