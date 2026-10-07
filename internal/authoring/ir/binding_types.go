package ir

import (
	"fmt"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/table"
)

// inferIRProductBindingScope reuses authoring's binding inference without
// serializing inferred let types into the public IR representation.
func inferIRProductBindingScope(product IRProduct, constants map[string]IRConstant, tables map[string]*table.Table) (map[string]*dsl.ParameterNode, error) {
	astProduct := &dsl.ProductNode{Name: product.Name}
	for _, binding := range product.Lets {
		expr, err := irExpressionForBindingInference(&binding.Value)
		if err != nil {
			return nil, fmt.Errorf("let '%s': %w", binding.Name, err)
		}
		astProduct.Lets = append(astProduct.Lets, &dsl.LetNode{Name: binding.Name, Value: expr})
	}
	for _, param := range product.Parameters {
		expr, err := irExpressionForBindingInference(&param.DefaultValue)
		if err != nil {
			return nil, fmt.Errorf("parameter '%s': %w", param.Name, err)
		}
		astProduct.Parameters = append(astProduct.Parameters, &dsl.ParameterNode{
			Name: param.Name, Type: dslTypeForBindingInference(param.Type), DefaultValue: expr,
		})
	}
	constScope := make(map[string]dsl.ConstantValue, len(constants))
	for name, constant := range constants {
		constScope[name] = dsl.ConstantValue{Type: dslTypeForBindingInference(constant.Type), Value: constant.Value}
	}
	return dsl.BuildProductBindingScopeForPlanning(astProduct, constScope, tables)
}

func dslTypeForBindingInference(typ IRType) dsl.ParameterType {
	return dsl.ParameterType{
		Kind: dsl.ParameterTypeKind(typ.Kind), EnumValues: append([]string(nil), typ.EnumValues...),
	}
}

// irExpressionForBindingInference preserves expression structure; it does not
// evaluate defaults or infer enum domains from concrete values.
func irExpressionForBindingInference(expr *IRExpression) (dsl.ExpressionNode, error) {
	if expr == nil {
		return nil, fmt.Errorf("binding expression is nil")
	}
	switch expr.Kind {
	case ExprKindLiteral:
		return &dsl.LiteralExpression{Value: expr.LiteralValue}, nil
	case ExprKindReference:
		return &dsl.IdentifierExpression{Name: expr.ReferenceName}, nil
	case ExprKindInterpolatedString:
		segments := make([]dsl.InterpolatedStringSegment, len(expr.InterpolatedSegments))
		for i, segment := range expr.InterpolatedSegments {
			segments[i] = dsl.InterpolatedStringSegment{Text: segment.Text, ParamName: segment.ParamName}
		}
		return &dsl.InterpolatedStringExpression{Segments: segments}, nil
	case ExprKindBinary:
		left, err := irExpressionForBindingInference(expr.BinaryLeft)
		if err != nil {
			return nil, err
		}
		right, err := irExpressionForBindingInference(expr.BinaryRight)
		if err != nil {
			return nil, err
		}
		return &dsl.BinaryExpression{Left: left, Operator: expr.BinaryOp, Right: right}, nil
	case ExprKindUnary:
		operand, err := irExpressionForBindingInference(expr.UnaryOperand)
		if err != nil {
			return nil, err
		}
		return &dsl.UnaryExpression{Operator: expr.UnaryOp, Operand: operand}, nil
	case ExprKindTernary:
		condition, err := irExpressionForBindingInference(expr.TernaryCond)
		if err != nil {
			return nil, err
		}
		trueExpr, err := irExpressionForBindingInference(expr.TernaryTrue)
		if err != nil {
			return nil, err
		}
		falseExpr, err := irExpressionForBindingInference(expr.TernaryFalse)
		if err != nil {
			return nil, err
		}
		return &dsl.TernaryExpression{Condition: condition, TrueExpr: trueExpr, FalseExpr: falseExpr}, nil
	case ExprKindCall:
		args := make([]dsl.ExpressionNode, len(expr.FuncArgs))
		for i := range expr.FuncArgs {
			arg, err := irExpressionForBindingInference(&expr.FuncArgs[i])
			if err != nil {
				return nil, err
			}
			args[i] = arg
		}
		return &dsl.FunctionCallExpression{Name: expr.FuncName, Args: args}, nil
	default:
		return nil, fmt.Errorf("unsupported binding expression kind: %s", expr.Kind)
	}
}
