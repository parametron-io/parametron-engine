package ir

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateDSL generates DSL source code from an IR program.
// The output is deterministic: constants are sorted alphabetically,
// profiles and products maintain their definition order.
// Indentation uses 4 spaces.
func GenerateDSL(program *IRProgram) string {
	if program == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("dsl v1.0\n")

	// Generate constants (sorted alphabetically for determinism)
	generateConstants(&b, program.Constants)

	// Generate profiles
	generateProfiles(&b, program.Profiles)

	// Generate active profile selection
	if program.ActiveProfileName != nil {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("use profile %s\n", *program.ActiveProfileName))
	}

	// Generate products
	generateProducts(&b, program.Products)

	return strings.TrimSpace(b.String()) + "\n"
}

// generateConstants outputs constant declarations in sorted order.
func generateConstants(b *strings.Builder, constants map[string]IRConstant) {
	if len(constants) == 0 {
		return
	}

	// Sort constant names for deterministic output
	names := make([]string, 0, len(constants))
	for name := range constants {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		c := constants[name]
		b.WriteString(fmt.Sprintf("const %s = %s\n", c.Name, formatLiteral(c.Value)))
	}
}

// generateProfiles outputs profile definitions.
func generateProfiles(b *strings.Builder, profiles []IRProfile) {
	if len(profiles) == 0 {
		return
	}

	for i, profile := range profiles {
		if b.Len() > 0 && (i > 0 || !strings.HasSuffix(b.String(), "\n\n")) {
			if !strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}

		b.WriteString(fmt.Sprintf("profile %s {\n", profile.Name))

		// Sort settings keys for deterministic output
		keys := make([]string, 0, len(profile.Settings))
		for key := range profile.Settings {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			expr := profile.Settings[key]
			exprStr := generateExpression(&expr, 1)
			b.WriteString(fmt.Sprintf("    %s = %s\n", key, exprStr))
		}

		b.WriteString("}")
	}
}

// generateProducts outputs product definitions.
func generateProducts(b *strings.Builder, products []IRProduct) {
	if len(products) == 0 {
		return
	}

	for i, product := range products {
		if b.Len() > 0 && (i > 0 || !strings.HasSuffix(b.String(), "\n\n")) {
			if !strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}

		b.WriteString(fmt.Sprintf("product %s {\n", product.Name))

		for _, let := range product.Lets {
			generateLet(b, let)
		}

		for _, param := range product.Parameters {
			generateParameter(b, param)
		}

		b.WriteString("}")
	}
}

// generateLet outputs a let definition.
func generateLet(b *strings.Builder, binding IRLet) {
	valueStr := generateExpression(&binding.Value, 1)
	b.WriteString(fmt.Sprintf("    let %s = %s\n", binding.Name, valueStr))
}

// generateParameter outputs a parameter definition.
func generateParameter(b *strings.Builder, param IRParameter) {
	typeStr := formatType(param.Type)
	defaultValueStr := generateExpression(&param.DefaultValue, 1)

	b.WriteString(fmt.Sprintf("    param %s: %s = %s\n", param.Name, typeStr, defaultValueStr))
}

// formatType formats an IRType for DSL output.
func formatType(t IRType) string {
	switch t.Kind {
	case IRTypeEnum:
		if len(t.EnumValues) > 0 {
			return fmt.Sprintf("enum { %s }", strings.Join(t.EnumValues, ", "))
		}
		return "enum { }"
	default:
		return string(t.Kind)
	}
}

// formatLiteral formats a literal value for DSL output.
func formatLiteral(v interface{}) string {
	switch val := v.(type) {
	case float64:
		// Format without unnecessary decimal places
		if val == float64(int64(val)) {
			return fmt.Sprintf("%.0f", val)
		}
		return fmt.Sprintf("%g", val)
	case float32:
		if val == float32(int32(val)) {
			return fmt.Sprintf("%.0f", val)
		}
		return fmt.Sprintf("%g", val)
	case int:
		return fmt.Sprintf("%d", val)
	case int32:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case string:
		return escapeString(val)
	case bool:
		return fmt.Sprintf("%t", val)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// escapeString escapes a string for DSL output.
func escapeString(s string) string {
	// Escape backslashes first, then quotes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return fmt.Sprintf("\"%s\"", s)
}

// generateExpression generates DSL code for an expression.
// indentLevel is used for proper indentation of multi-line expressions.
func generateExpression(expr *IRExpression, indentLevel int) string {
	if expr == nil {
		return "null"
	}

	switch expr.Kind {
	case ExprKindLiteral:
		return formatLiteral(expr.LiteralValue)

	case ExprKindReference:
		return expr.ReferenceName

	case ExprKindInterpolatedString:
		return generateInterpolatedStringExpression(expr)

	case ExprKindBinary:
		return generateBinaryExpression(expr, indentLevel)

	case ExprKindUnary:
		return generateUnaryExpression(expr, indentLevel)

	case ExprKindTernary:
		return generateTernaryExpression(expr, indentLevel)

	case ExprKindCall:
		return generateCallExpression(expr, indentLevel)

	default:
		return fmt.Sprintf("/* unknown expression kind: %s */", expr.Kind)
	}
}

func generateInterpolatedStringExpression(expr *IRExpression) string {
	var b strings.Builder
	for _, seg := range expr.InterpolatedSegments {
		if seg.ParamName != "" {
			b.WriteString("{param:")
			b.WriteString(seg.ParamName)
			b.WriteString("}")
			continue
		}
		b.WriteString(seg.Text)
	}
	return escapeString(b.String())
}

// generateBinaryExpression generates DSL code for a binary expression.
// Adds parentheses where needed based on operator precedence.
func generateBinaryExpression(expr *IRExpression, indentLevel int) string {
	left := generateExpression(expr.BinaryLeft, indentLevel)
	right := generateExpression(expr.BinaryRight, indentLevel)

	// Handle operator precedence by adding parentheses when needed
	left = parenthesizeIfNeeded(left, expr.BinaryLeft, expr.BinaryOp, true)
	right = parenthesizeIfNeeded(right, expr.BinaryRight, expr.BinaryOp, false)

	return fmt.Sprintf("%s %s %s", left, expr.BinaryOp, right)
}

// generateUnaryExpression generates DSL code for a unary expression.
func generateUnaryExpression(expr *IRExpression, indentLevel int) string {
	operand := generateExpression(expr.UnaryOperand, indentLevel)

	// Parenthesize the operand if it's a binary or ternary expression
	if expr.UnaryOperand != nil {
		switch expr.UnaryOperand.Kind {
		case ExprKindBinary, ExprKindTernary:
			operand = fmt.Sprintf("(%s)", operand)
		}
	}

	return fmt.Sprintf("%s%s", expr.UnaryOp, operand)
}

// generateTernaryExpression generates DSL code for a ternary expression.
func generateTernaryExpression(expr *IRExpression, indentLevel int) string {
	cond := generateExpression(expr.TernaryCond, indentLevel)
	trueExpr := generateExpression(expr.TernaryTrue, indentLevel)
	falseExpr := generateExpression(expr.TernaryFalse, indentLevel)

	// Parenthesize condition if it's a binary expression
	if expr.TernaryCond != nil && expr.TernaryCond.Kind == ExprKindBinary {
		cond = fmt.Sprintf("(%s)", cond)
	}

	return fmt.Sprintf("%s ? %s : %s", cond, trueExpr, falseExpr)
}

// generateCallExpression generates DSL code for a function call expression.
func generateCallExpression(expr *IRExpression, indentLevel int) string {
	args := make([]string, len(expr.FuncArgs))
	for i, arg := range expr.FuncArgs {
		args[i] = generateExpression(&arg, indentLevel)
	}

	return fmt.Sprintf("%s(%s)", expr.FuncName, strings.Join(args, ", "))
}

// operatorPrecedence returns the precedence level of an operator.
// Higher values mean higher precedence (binds tighter).
func operatorPrecedence(op string) int {
	switch op {
	case "||":
		return 1
	case "&&":
		return 2
	case "==", "!=":
		return 3
	case "<", "<=", ">", ">=":
		return 4
	case "+", "-":
		return 5
	case "*", "/":
		return 6
	default:
		return 0
	}
}

// parenthesizeIfNeeded adds parentheses around an operand if needed based on operator precedence.
func parenthesizeIfNeeded(operandStr string, operand *IRExpression, parentOp string, isLeft bool) string {
	if operand == nil || operand.Kind != ExprKindBinary {
		return operandStr
	}

	parentPrec := operatorPrecedence(parentOp)
	childPrec := operatorPrecedence(operand.BinaryOp)

	// Parenthesize if child has lower precedence
	if childPrec < parentPrec {
		return fmt.Sprintf("(%s)", operandStr)
	}

	// For same precedence, consider associativity
	if childPrec == parentPrec {
		// For non-associative operators, always parenthesize
		if isNonAssociative(parentOp) {
			return fmt.Sprintf("(%s)", operandStr)
		}

		// For right operand of left-associative operators, parenthesize
		// This ensures a - (b - c) is preserved
		if !isLeft && isLeftAssociative(parentOp) {
			return fmt.Sprintf("(%s)", operandStr)
		}

		// For left operand of right-associative operators, parenthesize
		if isLeft && isRightAssociative(parentOp) {
			return fmt.Sprintf("(%s)", operandStr)
		}
	}

	return operandStr
}

// isNonAssociative returns true if the operator is non-associative.
func isNonAssociative(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	default:
		return false
	}
}

// isLeftAssociative returns true if the operator is left-associative.
func isLeftAssociative(op string) bool {
	switch op {
	case "+", "-", "*", "/", "&&", "||":
		return true
	default:
		return false
	}
}

// isRightAssociative returns true if the operator is right-associative.
func isRightAssociative(op string) bool {
	// Currently no right-associative operators in the DSL
	return false
}
