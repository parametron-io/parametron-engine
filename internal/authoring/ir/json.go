package ir

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON serializes an IRProgram to JSON.
func (p *IRProgram) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}

	// Use the standard marshaler with the struct tags already defined
	return json.Marshal((*plainIRProgram)(p))
}

// UnmarshalJSON deserializes an IRProgram from JSON.
func (p *IRProgram) UnmarshalJSON(data []byte) error {
	if p == nil {
		return fmt.Errorf("cannot unmarshal into nil IRProgram")
	}

	// Use the standard unmarshaler
	if err := json.Unmarshal(data, (*plainIRProgram)(p)); err != nil {
		return fmt.Errorf("failed to unmarshal IRProgram: %w", err)
	}

	// Validate the program structure
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid IR program: %w", err)
	}

	return nil
}

// plainIRProgram is a type alias to avoid infinite recursion in MarshalJSON/UnmarshalJSON.
type plainIRProgram IRProgram

// Validate checks that the IR program is valid.
// It verifies type consistency and reference validity.
func (p *IRProgram) Validate() error {
	if p == nil {
		return fmt.Errorf("IR program is nil")
	}

	if p.Version == "" {
		return fmt.Errorf("IR program version is required")
	}

	// Validate constants
	for name, c := range p.Constants {
		if err := validateConstant(name, c); err != nil {
			return fmt.Errorf("constant '%s': %w", name, err)
		}
	}

	// Validate profiles
	profileNames := make(map[string]struct{})
	for _, profile := range p.Profiles {
		if err := validateProfile(profile, profileNames); err != nil {
			return fmt.Errorf("profile '%s': %w", profile.Name, err)
		}
	}

	// Validate active profile exists
	if p.ActiveProfileName != nil {
		if _, ok := profileNames[*p.ActiveProfileName]; !ok {
			return fmt.Errorf("active profile '%s' is not defined", *p.ActiveProfileName)
		}
	}

	// Validate products
	productNames := make(map[string]struct{})
	for _, product := range p.Products {
		if err := validateProduct(product, productNames); err != nil {
			return fmt.Errorf("product '%s': %w", product.Name, err)
		}
	}

	return nil
}

// validateConstant validates a single constant definition.
func validateConstant(name string, c IRConstant) error {
	if c.Name != name {
		return fmt.Errorf("constant name mismatch: map key '%s' vs struct field '%s'", name, c.Name)
	}
	if c.Name == "" {
		return fmt.Errorf("constant name cannot be empty")
	}
	if !c.Type.Kind.IsValid() {
		return fmt.Errorf("invalid constant type: %s", c.Type.Kind)
	}
	if c.Value == nil {
		return fmt.Errorf("constant value cannot be nil")
	}

	// Validate value type matches declared type
	switch c.Type.Kind {
	case IRTypeNumber:
		if _, ok := c.Value.(float64); !ok {
			// Also accept other numeric types that JSON might produce
			switch c.Value.(type) {
			case float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
				// Accept these but they should ideally be float64
			default:
				return fmt.Errorf("number constant must have numeric value, got %T", c.Value)
			}
		}
	case IRTypeString:
		if _, ok := c.Value.(string); !ok {
			return fmt.Errorf("string constant must have string value, got %T", c.Value)
		}
	case IRTypeBoolean:
		if _, ok := c.Value.(bool); !ok {
			return fmt.Errorf("boolean constant must have boolean value, got %T", c.Value)
		}
	case IRTypeEnum:
		// Enum values are stored as strings
		if _, ok := c.Value.(string); !ok {
			return fmt.Errorf("enum constant must have string value, got %T", c.Value)
		}
	}

	return nil
}

// validateProfile validates a profile definition.
func validateProfile(profile IRProfile, seenNames map[string]struct{}) error {
	if profile.Name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if _, exists := seenNames[profile.Name]; exists {
		return fmt.Errorf("duplicate profile name")
	}
	seenNames[profile.Name] = struct{}{}

	for key, expr := range profile.Settings {
		if key == "" {
			return fmt.Errorf("empty setting key")
		}
		if err := validateExpression(&expr); err != nil {
			return fmt.Errorf("setting '%s': %w", key, err)
		}
	}

	return nil
}

// validateProduct validates a product definition.
func validateProduct(product IRProduct, seenNames map[string]struct{}) error {
	if product.Name == "" {
		return fmt.Errorf("product name cannot be empty")
	}
	if _, exists := seenNames[product.Name]; exists {
		return fmt.Errorf("duplicate product name")
	}
	seenNames[product.Name] = struct{}{}

	bindingNames := make(map[string]struct{})
	for _, let := range product.Lets {
		if err := validateLet(let, bindingNames); err != nil {
			return fmt.Errorf("let '%s': %w", let.Name, err)
		}
	}
	for _, param := range product.Parameters {
		if err := validateParameter(param, bindingNames); err != nil {
			return fmt.Errorf("parameter '%s': %w", param.Name, err)
		}
	}

	return nil
}

func validateLet(binding IRLet, seenNames map[string]struct{}) error {
	if binding.Name == "" {
		return fmt.Errorf("let name cannot be empty")
	}
	if _, exists := seenNames[binding.Name]; exists {
		return fmt.Errorf("duplicate product binding name")
	}
	seenNames[binding.Name] = struct{}{}

	if err := validateExpression(&binding.Value); err != nil {
		return fmt.Errorf("value: %w", err)
	}
	return nil
}

// validateParameter validates a parameter definition.
func validateParameter(param IRParameter, seenNames map[string]struct{}) error {
	if param.Name == "" {
		return fmt.Errorf("parameter name cannot be empty")
	}
	if _, exists := seenNames[param.Name]; exists {
		return fmt.Errorf("duplicate parameter name")
	}
	seenNames[param.Name] = struct{}{}

	if !param.Type.Kind.IsValid() {
		return fmt.Errorf("invalid parameter type: %s", param.Type.Kind)
	}

	// Validate enum values if type is enum
	if param.Type.Kind == IRTypeEnum && len(param.Type.EnumValues) == 0 {
		// This is a warning, not an error - empty enums are allowed
	}

	if err := validateExpression(&param.DefaultValue); err != nil {
		return fmt.Errorf("default value: %w", err)
	}

	return nil
}

// validateExpression validates an expression tree.
func validateExpression(expr *IRExpression) error {
	if expr == nil {
		return fmt.Errorf("expression is nil")
	}

	if !expr.Kind.IsValid() {
		return fmt.Errorf("invalid expression kind: %s", expr.Kind)
	}

	switch expr.Kind {
	case ExprKindLiteral:
		if expr.LiteralValue == nil {
			return fmt.Errorf("literal expression has nil value")
		}

	case ExprKindReference:
		if expr.ReferenceName == "" {
			return fmt.Errorf("reference expression has empty name")
		}
		if !expr.ReferenceType.IsValid() {
			return fmt.Errorf("invalid reference type: %s", expr.ReferenceType)
		}

	case ExprKindInterpolatedString:
		if len(expr.InterpolatedSegments) == 0 {
			return fmt.Errorf("interpolated string expression has no segments")
		}
		for i, seg := range expr.InterpolatedSegments {
			if seg.ParamName == "" && seg.Text == "" {
				return fmt.Errorf("interpolated segment %d is empty", i+1)
			}
			if seg.ParamName != "" && seg.Text != "" {
				return fmt.Errorf("interpolated segment %d cannot have both text and paramName", i+1)
			}
		}

	case ExprKindBinary:
		if expr.BinaryOp == "" {
			return fmt.Errorf("binary expression has empty operator")
		}
		if expr.BinaryLeft == nil {
			return fmt.Errorf("binary expression has nil left operand")
		}
		if err := validateExpression(expr.BinaryLeft); err != nil {
			return fmt.Errorf("left operand: %w", err)
		}
		if expr.BinaryRight == nil {
			return fmt.Errorf("binary expression has nil right operand")
		}
		if err := validateExpression(expr.BinaryRight); err != nil {
			return fmt.Errorf("right operand: %w", err)
		}

	case ExprKindUnary:
		if expr.UnaryOp == "" {
			return fmt.Errorf("unary expression has empty operator")
		}
		if expr.UnaryOperand == nil {
			return fmt.Errorf("unary expression has nil operand")
		}
		if err := validateExpression(expr.UnaryOperand); err != nil {
			return fmt.Errorf("operand: %w", err)
		}

	case ExprKindTernary:
		if expr.TernaryCond == nil {
			return fmt.Errorf("ternary expression has nil condition")
		}
		if err := validateExpression(expr.TernaryCond); err != nil {
			return fmt.Errorf("condition: %w", err)
		}
		if expr.TernaryTrue == nil {
			return fmt.Errorf("ternary expression has nil true branch")
		}
		if err := validateExpression(expr.TernaryTrue); err != nil {
			return fmt.Errorf("true branch: %w", err)
		}
		if expr.TernaryFalse == nil {
			return fmt.Errorf("ternary expression has nil false branch")
		}
		if err := validateExpression(expr.TernaryFalse); err != nil {
			return fmt.Errorf("false branch: %w", err)
		}

	case ExprKindCall:
		if expr.FuncName == "" {
			return fmt.Errorf("function call has empty name")
		}
		for i, arg := range expr.FuncArgs {
			if err := validateExpression(&arg); err != nil {
				return fmt.Errorf("argument %d: %w", i+1, err)
			}
		}
	}

	return nil
}

// ToJSON serializes the IR program to a JSON string with indentation.
func (p *IRProgram) ToJSON() (string, error) {
	bytes, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal IR program to JSON: %w", err)
	}
	return string(bytes), nil
}

// FromJSON deserializes an IR program from a JSON string.
func FromJSON(data string) (*IRProgram, error) {
	var program IRProgram
	if err := json.Unmarshal([]byte(data), &program); err != nil {
		return nil, fmt.Errorf("failed to unmarshal IR program from JSON: %w", err)
	}
	return &program, nil
}

// MustToJSON serializes the IR program to JSON, panicking on error.
// This is useful for testing and situations where errors are unexpected.
func (p *IRProgram) MustToJSON() string {
	s, err := p.ToJSON()
	if err != nil {
		panic(err)
	}
	return s
}
