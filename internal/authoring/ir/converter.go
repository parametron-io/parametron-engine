package ir

import (
	"fmt"
	"sort"

	"parametron/internal/authoring/dsl"
)

// ConvertAST converts a validated DSL AST to the Intermediate Representation.
// The AST must have passed validation (ResolvedConstants must be populated).
// Returns an error if the AST is nil or has not been validated.
func ConvertAST(ast *dsl.AST) (*IRProgram, error) {
	if ast == nil {
		return nil, fmt.Errorf("cannot convert nil AST")
	}

	// Check that validation has been performed
	if len(ast.Constants) > 0 && len(ast.ResolvedConstants) == 0 {
		return nil, fmt.Errorf("AST must be validated before conversion (ResolvedConstants is nil)")
	}

	program := &IRProgram{
		Version:   "1.0",
		Constants: make(map[string]IRConstant),
		Profiles:  make([]IRProfile, 0, len(ast.Profiles)),
		Products:  make([]IRProduct, 0, len(ast.Products)),
	}

	// Copy active profile name
	if ast.ActiveProfileName != nil {
		name := *ast.ActiveProfileName
		program.ActiveProfileName = &name
	}

	// Convert constants (already resolved during validation)
	if err := convertConstants(ast, program); err != nil {
		return nil, fmt.Errorf("failed to convert constants: %w", err)
	}

	// Convert profiles (expressions preserved, not resolved)
	if err := convertProfiles(ast, program); err != nil {
		return nil, fmt.Errorf("failed to convert profiles: %w", err)
	}

	// Convert products with their parameters
	if err := convertProducts(ast, program); err != nil {
		return nil, fmt.Errorf("failed to convert products: %w", err)
	}

	return program, nil
}

// convertConstants converts resolved constants from AST to IR.
func convertConstants(ast *dsl.AST, program *IRProgram) error {
	// Get sorted list of constant names for deterministic output
	names := make([]string, 0, len(ast.ResolvedConstants))
	for name := range ast.ResolvedConstants {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cv := ast.ResolvedConstants[name]
		program.Constants[name] = IRConstant{
			Name:  name,
			Type:  convertType(cv.Type),
			Value: cv.Value,
		}
	}

	return nil
}

// convertProfiles converts profile definitions from AST to IR.
func convertProfiles(ast *dsl.AST, program *IRProgram) error {
	for _, profile := range ast.Profiles {
		irProfile := IRProfile{
			Name:     profile.Name,
			Settings: make(map[string]IRExpression),
		}

		for key, expr := range profile.Settings {
			irExpr, err := convertExpression(expr, nil, ast.ResolvedConstants, nil)
			if err != nil {
				return fmt.Errorf("profile '%s', setting '%s': %w", profile.Name, key, err)
			}
			irProfile.Settings[key] = irExpr
		}

		program.Profiles = append(program.Profiles, irProfile)
	}

	return nil
}

// convertProducts converts product definitions from AST to IR.
func convertProducts(ast *dsl.AST, program *IRProgram) error {
	for _, product := range ast.Products {
		irProduct := IRProduct{
			Name:       product.Name,
			Lets:       make([]IRLet, 0, len(product.Lets)),
			Parameters: make([]IRParameter, 0, len(product.Parameters)),
		}

		// Build unified binding scope for this product. Lets are represented as
		// parameter-shaped entries carrying their inferred type.
		bindingScope, err := dsl.BuildProductBindingScopeForPlanning(product, ast.ResolvedConstants, nil)
		if err != nil {
			return fmt.Errorf("product '%s': %w", product.Name, err)
		}

		for _, let := range product.Lets {
			letBinding := bindingScope[let.Name]
			irExpr, err := convertExpression(let.Value, bindingScope, ast.ResolvedConstants, &letBinding.Type)
			if err != nil {
				return fmt.Errorf("product '%s', let '%s': %w", product.Name, let.Name, err)
			}
			irProduct.Lets = append(irProduct.Lets, IRLet{
				Name:  let.Name,
				Value: irExpr,
			})
		}

		for _, param := range product.Parameters {
			irParam := IRParameter{
				Name: param.Name,
				Type: convertType(param.Type),
			}

			// Convert the default value expression
			irExpr, err := convertExpression(param.DefaultValue, bindingScope, ast.ResolvedConstants, &param.Type)
			if err != nil {
				return fmt.Errorf("product '%s', parameter '%s': %w", product.Name, param.Name, err)
			}
			irParam.DefaultValue = irExpr

			irProduct.Parameters = append(irProduct.Parameters, irParam)
		}

		program.Products = append(program.Products, irProduct)
	}

	return nil
}

// convertType converts a DSL ParameterType to an IR IRType.
func convertType(pt dsl.ParameterType) IRType {
	return IRType{
		Kind:       convertTypeKind(pt.Kind),
		EnumValues: append([]string(nil), pt.EnumValues...), // Copy slice
	}
}

// convertTypeKind converts a DSL ParameterTypeKind to an IR IRTypeKind.
func convertTypeKind(kind dsl.ParameterTypeKind) IRTypeKind {
	switch kind {
	case dsl.ParamTypeNumber:
		return IRTypeNumber
	case dsl.ParamTypeString:
		return IRTypeString
	case dsl.ParamTypeBoolean:
		return IRTypeBoolean
	case dsl.ParamTypeEnum:
		return IRTypeEnum
	default:
		// This should not happen for validated ASTs
		return IRTypeNumber
	}
}

// convertExpression converts a DSL ExpressionNode to an IR IRExpression.
// The paramScope contains parameters in the current product context.
// The constScope contains resolved constants.
func convertExpression(
	expr dsl.ExpressionNode,
	paramScope map[string]*dsl.ParameterNode,
	constScope map[string]dsl.ConstantValue,
	enumHint *dsl.ParameterType,
) (IRExpression, error) {
	switch e := expr.(type) {
	case *dsl.LiteralExpression:
		return convertLiteralExpression(e)

	case *dsl.IdentifierExpression:
		return convertIdentifierExpression(e, paramScope, constScope, enumHint)

	case *dsl.InterpolatedStringExpression:
		return convertInterpolatedStringExpression(e)

	case *dsl.BinaryExpression:
		return convertBinaryExpression(e, paramScope, constScope, enumHint)

	case *dsl.UnaryExpression:
		return convertUnaryExpression(e, paramScope, constScope, enumHint)

	case *dsl.TernaryExpression:
		return convertTernaryExpression(e, paramScope, constScope, enumHint)

	case *dsl.FunctionCallExpression:
		return convertFunctionCallExpression(e, paramScope, constScope, enumHint)

	default:
		return IRExpression{}, fmt.Errorf("unknown expression type: %T", expr)
	}
}

// convertLiteralExpression converts a literal expression.
func convertLiteralExpression(e *dsl.LiteralExpression) (IRExpression, error) {
	return IRExpression{
		Kind:         ExprKindLiteral,
		LiteralValue: e.Value,
	}, nil
}

// convertIdentifierExpression converts an identifier expression.
// It determines whether the identifier refers to a parameter, constant, or enum value.
func convertIdentifierExpression(
	e *dsl.IdentifierExpression,
	paramScope map[string]*dsl.ParameterNode,
	constScope map[string]dsl.ConstantValue,
	enumHint *dsl.ParameterType,
) (IRExpression, error) {
	// Check if it's a product-local binding reference.
	if paramScope != nil {
		if binding, ok := paramScope[e.Name]; ok {
			refType := RefTypeParam
			if binding.DefaultValue == nil {
				refType = RefTypeLet
			}
			return IRExpression{
				Kind:          ExprKindReference,
				ReferenceName: e.Name,
				ReferenceType: refType,
			}, nil
		}
	}

	// Check if it's a constant reference
	if _, ok := constScope[e.Name]; ok {
		return IRExpression{
			Kind:          ExprKindReference,
			ReferenceName: e.Name,
			ReferenceType: RefTypeConstant,
		}, nil
	}

	if enumHint != nil && enumHint.Kind == dsl.ParamTypeEnum {
		for _, value := range enumHint.EnumValues {
			if value == e.Name {
				return IRExpression{
					Kind:          ExprKindReference,
					ReferenceName: e.Name,
					ReferenceType: RefTypeEnumValue,
				}, nil
			}
		}
		return IRExpression{}, fmt.Errorf("invalid enum value '%s', allowed values are %v", e.Name, enumHint.EnumValues)
	}

	return IRExpression{}, fmt.Errorf("undefined identifier '%s'", e.Name)
}

func convertInterpolatedStringExpression(e *dsl.InterpolatedStringExpression) (IRExpression, error) {
	segments := make([]IRInterpolatedSegment, 0, len(e.Segments))
	for _, seg := range e.Segments {
		segments = append(segments, IRInterpolatedSegment{
			Text:      seg.Text,
			ParamName: seg.ParamName,
		})
	}
	return IRExpression{
		Kind:                 ExprKindInterpolatedString,
		InterpolatedSegments: segments,
	}, nil
}

// convertBinaryExpression converts a binary expression.
func convertBinaryExpression(
	e *dsl.BinaryExpression,
	paramScope map[string]*dsl.ParameterNode,
	constScope map[string]dsl.ConstantValue,
	enumHint *dsl.ParameterType,
) (IRExpression, error) {
	var operandEnumHint *dsl.ParameterType
	if e.Operator == "==" || e.Operator == "!=" {
		if inferred, ok := inferEnumTypeFromExpr(e.Left, paramScope, constScope); ok {
			operandEnumHint = &inferred
		} else if inferred, ok := inferEnumTypeFromExpr(e.Right, paramScope, constScope); ok {
			operandEnumHint = &inferred
		}
	}

	leftHint := enumHint
	rightHint := enumHint
	if operandEnumHint != nil {
		leftHint = operandEnumHint
		rightHint = operandEnumHint
	}

	left, err := convertExpression(e.Left, paramScope, constScope, leftHint)
	if err != nil {
		return IRExpression{}, fmt.Errorf("left operand: %w", err)
	}

	right, err := convertExpression(e.Right, paramScope, constScope, rightHint)
	if err != nil {
		return IRExpression{}, fmt.Errorf("right operand: %w", err)
	}

	return IRExpression{
		Kind:        ExprKindBinary,
		BinaryOp:    e.Operator,
		BinaryLeft:  &left,
		BinaryRight: &right,
	}, nil
}

// convertUnaryExpression converts a unary expression.
func convertUnaryExpression(
	e *dsl.UnaryExpression,
	paramScope map[string]*dsl.ParameterNode,
	constScope map[string]dsl.ConstantValue,
	enumHint *dsl.ParameterType,
) (IRExpression, error) {
	operand, err := convertExpression(e.Operand, paramScope, constScope, enumHint)
	if err != nil {
		return IRExpression{}, fmt.Errorf("operand: %w", err)
	}

	return IRExpression{
		Kind:         ExprKindUnary,
		UnaryOp:      e.Operator,
		UnaryOperand: &operand,
	}, nil
}

// convertTernaryExpression converts a ternary expression.
func convertTernaryExpression(
	e *dsl.TernaryExpression,
	paramScope map[string]*dsl.ParameterNode,
	constScope map[string]dsl.ConstantValue,
	enumHint *dsl.ParameterType,
) (IRExpression, error) {
	cond, err := convertExpression(e.Condition, paramScope, constScope, nil)
	if err != nil {
		return IRExpression{}, fmt.Errorf("condition: %w", err)
	}

	trueExpr, err := convertExpression(e.TrueExpr, paramScope, constScope, enumHint)
	if err != nil {
		return IRExpression{}, fmt.Errorf("true branch: %w", err)
	}

	falseExpr, err := convertExpression(e.FalseExpr, paramScope, constScope, enumHint)
	if err != nil {
		return IRExpression{}, fmt.Errorf("false branch: %w", err)
	}

	return IRExpression{
		Kind:         ExprKindTernary,
		TernaryCond:  &cond,
		TernaryTrue:  &trueExpr,
		TernaryFalse: &falseExpr,
	}, nil
}

// convertFunctionCallExpression converts a function call expression.
func convertFunctionCallExpression(
	e *dsl.FunctionCallExpression,
	paramScope map[string]*dsl.ParameterNode,
	constScope map[string]dsl.ConstantValue,
	_ *dsl.ParameterType,
) (IRExpression, error) {
	args := make([]IRExpression, 0, len(e.Args))
	for i, arg := range e.Args {
		irArg, err := convertExpression(arg, paramScope, constScope, nil)
		if err != nil {
			return IRExpression{}, fmt.Errorf("argument %d: %w", i+1, err)
		}
		args = append(args, irArg)
	}

	return IRExpression{
		Kind:     ExprKindCall,
		FuncName: e.Name,
		FuncArgs: args,
	}, nil
}

func inferEnumTypeFromExpr(
	expr dsl.ExpressionNode,
	paramScope map[string]*dsl.ParameterNode,
	constScope map[string]dsl.ConstantValue,
) (dsl.ParameterType, bool) {
	ident, ok := expr.(*dsl.IdentifierExpression)
	if !ok {
		return dsl.ParameterType{}, false
	}
	if param, exists := paramScope[ident.Name]; exists && param.Type.Kind == dsl.ParamTypeEnum {
		return param.Type, true
	}
	if c, exists := constScope[ident.Name]; exists && c.Type.Kind == dsl.ParamTypeEnum && len(c.Type.EnumValues) > 0 {
		return c.Type, true
	}
	return dsl.ParameterType{}, false
}
