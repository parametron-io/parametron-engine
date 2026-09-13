package ir

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"parametron/internal/authoring/planner"
	"parametron/internal/authoring/runtime"
	"parametron/internal/engine/table"
)

// freeCADRuntimeAdapterID mirrors the canonical adapter identity that
// planner.CreatePlan assigns to aligned RunCADRuntime steps. IR only ever
// gates on "adapter == freecad" (see resolveShouldRunCADRunnerFromIR), so the
// resolved identity is always this constant rather than the raw profile
// setting value.
const freeCADRuntimeAdapterID = "freecad"

// CreatePlanFromIR generates an ExecutionPlan from an IR program.
// It applies parameter overrides and evaluates all expressions.
// This function is the IR-based equivalent of planner.CreatePlan.
func CreatePlanFromIR(program *IRProgram, overrides map[string]string) (*planner.ExecutionPlan, error) {
	return createPlanFromIR(program, overrides, nil)
}

// CreatePlanFromIRWithTables generates an ExecutionPlan from IR using planner-visible tables.
func CreatePlanFromIRWithTables(program *IRProgram, overrides map[string]string, tables map[string]*table.Table) (*planner.ExecutionPlan, error) {
	return createPlanFromIR(program, overrides, tables)
}

func createPlanFromIR(program *IRProgram, overrides map[string]string, tables map[string]*table.Table) (*planner.ExecutionPlan, error) {
	if program == nil {
		return nil, fmt.Errorf("IR program is nil")
	}

	// Collect product-local binding names for override validation.
	definedParams := make(map[string]struct{})
	definedLets := make(map[string]struct{})
	definedConstants := make(map[string]struct{})

	for _, product := range program.Products {
		for _, binding := range product.Lets {
			definedLets[binding.Name] = struct{}{}
		}
		for _, param := range product.Parameters {
			definedParams[param.Name] = struct{}{}
		}
	}

	for name := range program.Constants {
		definedConstants[name] = struct{}{}
	}

	// Validate overrides
	for overrideKey := range overrides {
		if _, ok := definedConstants[overrideKey]; ok {
			return nil, fmt.Errorf("cannot override constant: '%s'", overrideKey)
		}
		if _, ok := definedLets[overrideKey]; ok {
			return nil, fmt.Errorf("override target '%s' is not an exported parameter", overrideKey)
		}
		if _, ok := definedParams[overrideKey]; !ok {
			return nil, fmt.Errorf("override parameter '%s' not defined in DSL", overrideKey)
		}
	}

	execPlan := &planner.ExecutionPlan{
		Steps: []planner.Step{},
	}

	// Get active profile info
	activeProfileName := ""
	if program.ActiveProfileName != nil {
		activeProfileName = *program.ActiveProfileName
	}

	filePattern, hasFilePattern, err := resolveActiveFilePatternFromIR(program)
	if err != nil {
		return nil, err
	}
	shouldRunCADRunner, err := resolveShouldRunCADRunnerFromIR(program)
	if err != nil {
		return nil, err
	}

	type productDraft struct {
		name           string
		headers        []string
		values         []interface{}
		resolvedParams map[string]any
		parameters     []IRParameter
	}

	drafts := make([]productDraft, 0, len(program.Products))

	for _, product := range program.Products {
		resolvedValues := make(map[string]interface{})
		productBindingNames := make(map[string]struct{}, len(product.Lets)+len(product.Parameters))
		for _, binding := range product.Lets {
			productBindingNames[binding.Name] = struct{}{}
		}
		for _, param := range product.Parameters {
			productBindingNames[param.Name] = struct{}{}
		}

		// 1. Apply overrides
		for _, param := range product.Parameters {
			if overrideStr, ok := overrides[param.Name]; ok {
				slog.Debug("Applying override", "parameter", param.Name, "value", overrideStr)
				val, err := convertOverrideValue(overrideStr, param.Type)
				if err != nil {
					return nil, fmt.Errorf("failed to apply override for '%s': %w", param.Name, err)
				}
				resolvedValues[param.Name] = val
			}
		}

		// 2. Iteratively evaluate lets and params in one deterministic graph.
		type pendingBinding struct {
			name string
			expr *IRExpression
			kind string
		}
		unresolvedBindings := make([]pendingBinding, 0, len(product.Lets)+len(product.Parameters))
		for i := range product.Lets {
			unresolvedBindings = append(unresolvedBindings, pendingBinding{
				name: product.Lets[i].Name,
				expr: &product.Lets[i].Value,
				kind: "let",
			})
		}
		for _, p := range product.Parameters {
			if _, isResolved := resolvedValues[p.Name]; !isResolved {
				param := p
				unresolvedBindings = append(unresolvedBindings, pendingBinding{
					name: param.Name,
					expr: &param.DefaultValue,
					kind: "parameter",
				})
			}
		}

		progress := true
		for len(unresolvedBindings) > 0 && progress {
			progress = false
			remainingBindings := make([]pendingBinding, 0, len(unresolvedBindings))

			for _, binding := range unresolvedBindings {
				val, err := evaluateIRExpressionWithContext(binding.expr, resolvedValues, productBindingNames, program.Constants, irEvalContext{tables: tables})
				if err == nil {
					resolvedValues[binding.name] = val
					progress = true
				} else if !errors.Is(err, errUnresolvedIdentifier) {
					return nil, fmt.Errorf("error evaluating %s '%s' in product '%s': %w", binding.kind, binding.name, product.Name, err)
				} else {
					remainingBindings = append(remainingBindings, binding)
				}
			}
			unresolvedBindings = remainingBindings
		}

		if len(unresolvedBindings) > 0 {
			var unresolvedNames []string
			for _, binding := range unresolvedBindings {
				unresolvedNames = append(unresolvedNames, binding.name)
			}
			return nil, fmt.Errorf("circular or unresolved binding dependency in product '%s': %v", product.Name, unresolvedNames)
		}

		// 3. Build CSV payload data
		var headers []string
		var values []interface{}
		resolvedParams := make(map[string]any, len(product.Parameters))
		for _, param := range product.Parameters {
			headers = append(headers, param.Name)
			value := resolvedValues[param.Name]
			values = append(values, value)
			resolvedParams[param.Name] = value
		}

		drafts = append(drafts, productDraft{
			name:           product.Name,
			headers:        headers,
			values:         values,
			resolvedParams: resolvedParams,
			parameters:     product.Parameters,
		})
	}

	// Compute plan hash if file_pattern is used
	planHashForNaming := ""
	resolvedConstsForNaming := make(map[string]planner.ResolvedConst, len(program.Constants))
	for name, constVal := range program.Constants {
		resolvedConstsForNaming[name] = planner.ResolvedConst{
			TypeName: string(constVal.Type.Kind),
			Value:    constVal.Value,
		}
	}
	if hasFilePattern {
		stepsPerProduct := 2
		if shouldRunCADRunner {
			stepsPerProduct = 3
		}
		draftPlan := &planner.ExecutionPlan{Steps: make([]planner.Step, 0, len(drafts)*stepsPerProduct)}
		for _, d := range drafts {
			defaultCSVName := fmt.Sprintf("%s.csv", d.name)
			defaultSTEPName := strings.TrimSuffix(defaultCSVName, ".csv") + ".step"
			parameterAssignments, err := buildExportManifestParameterAssignmentsFromIR(d.parameters, d.resolvedParams)
			if err != nil {
				return nil, fmt.Errorf("failed to build default parameter assignments for product '%s': %w", d.name, err)
			}
			draftPlan.Steps = append(draftPlan.Steps, planner.Step{
				Type: planner.StepWriteCSV,
				Payload: planner.WriteCSVPayload{
					ProductKey: d.name,
					Filename:   defaultCSVName,
					Headers:    d.headers,
					Values:     d.values,
				},
			})
			draftPlan.Steps = append(draftPlan.Steps, planner.Step{
				Type: planner.StepWriteExportManifest,
				Payload: planner.WriteExportManifestPayload{
					ProductKey:       d.name,
					ManifestFilename: planner.ExportManifestFilename,
					SchemaVersion:    planner.ExportManifestSchemaVersion,
					Product: planner.ExportManifestProduct{
						ID: d.name,
					},
					Inputs:               planner.ExportManifestInputs{},
					Values:               copyResolvedValues(d.resolvedParams),
					ParameterAssignments: parameterAssignments,
					Outputs: []planner.ExportManifestOutput{
						{
							Type:     "step",
							Filename: defaultSTEPName,
						},
					},
				},
			})
			if shouldRunCADRunner {
				draftPlan.Steps = append(draftPlan.Steps, planner.Step{
					Type: planner.StepRunCADRuntime,
					Payload: planner.RunCADRuntimePayload{
						ProductKey:       d.name,
						Adapter:          freeCADRuntimeAdapterID,
						ManifestFilename: planner.ExportManifestFilename,
						ResultFilename:   planner.FreeCADRuntimeResultFilename,
					},
				})
			}
		}

		seedHash, err := planner.ComputePlanHash(draftPlan, nil) // Note: ComputePlanHash may need adjustment
		if err != nil {
			return nil, fmt.Errorf("failed to compute plan hash for file_pattern interpolation: %w", err)
		}
		planHashForNaming = seedHash
	}

	// Build final execution steps
	for _, d := range drafts {
		baseName := d.name
		if hasFilePattern {
			resolvedName, err := planner.ResolveFilePattern(filePattern, planner.NamingContext{
				ProductName:    d.name,
				ProfileName:    activeProfileName,
				PlanHash:       planHashForNaming,
				ResolvedParams: d.resolvedParams,
				ResolvedConsts: resolvedConstsForNaming,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to resolve file_pattern for product '%s': %w", d.name, err)
			}
			normalizedName := normalizePatternBaseName(resolvedName)
			if err := validateFileBaseName(normalizedName); err != nil {
				return nil, fmt.Errorf("failed to validate file_pattern output for product '%s': %w", d.name, err)
			}
			baseName = normalizedName
		}

		csvFileName := fmt.Sprintf("%s.csv", baseName)
		stepFileName := strings.TrimSuffix(csvFileName, ".csv") + ".step"
		parameterAssignments, err := buildExportManifestParameterAssignmentsFromIR(d.parameters, d.resolvedParams)
		if err != nil {
			return nil, fmt.Errorf("failed to build parameter assignments for product '%s': %w", d.name, err)
		}

		// Step 1: Write CSV
		execPlan.Steps = append(execPlan.Steps, planner.Step{
			Type: planner.StepWriteCSV,
			Payload: planner.WriteCSVPayload{
				ProductKey: d.name,
				Filename:   csvFileName,
				Headers:    d.headers,
				Values:     d.values,
			},
		})

		// Step 2: Write exporter manifest.
		execPlan.Steps = append(execPlan.Steps, planner.Step{
			Type: planner.StepWriteExportManifest,
			Payload: planner.WriteExportManifestPayload{
				ProductKey:       d.name,
				ManifestFilename: planner.ExportManifestFilename,
				SchemaVersion:    planner.ExportManifestSchemaVersion,
				Product: planner.ExportManifestProduct{
					ID: d.name,
				},
				Inputs:               planner.ExportManifestInputs{},
				Values:               copyResolvedValues(d.resolvedParams),
				ParameterAssignments: parameterAssignments,
				Outputs: []planner.ExportManifestOutput{
					{
						Type:     "step",
						Filename: stepFileName,
					},
				},
			},
		})

		// Step 3: Run CAD adapter worker (only when adapter requires it).
		if shouldRunCADRunner {
			execPlan.Steps = append(execPlan.Steps, planner.Step{
				Type: planner.StepRunCADRuntime,
				Payload: planner.RunCADRuntimePayload{
					ProductKey:       d.name,
					Adapter:          freeCADRuntimeAdapterID,
					ManifestFilename: planner.ExportManifestFilename,
					ResultFilename:   planner.FreeCADRuntimeResultFilename,
				},
			})
		}
	}

	if err := validateUniqueProductFilenames(execPlan); err != nil {
		return nil, err
	}

	planHash, err := planner.ComputePlanHash(execPlan, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to compute plan hash: %w", err)
	}
	for i := range execPlan.Steps {
		if execPlan.Steps[i].Type != planner.StepWriteExportManifest {
			continue
		}
		payload, ok := execPlan.Steps[i].Payload.(planner.WriteExportManifestPayload)
		if !ok {
			continue
		}
		payload.PlanHash = planHash
		execPlan.Steps[i].Payload = payload
	}

	return execPlan, nil
}

func buildExportManifestParameterAssignmentsFromIR(params []IRParameter, resolvedValues map[string]any) ([]planner.ExportManifestParameterAssignment, error) {
	assignments := make([]planner.ExportManifestParameterAssignment, 0, len(params))
	for _, param := range params {
		if param.Type.Kind != IRTypeNumber {
			continue
		}
		resolved, ok := resolvedValues[param.Name]
		if !ok {
			return nil, fmt.Errorf("resolved value missing for numeric parameter '%s'", param.Name)
		}
		value, ok := resolved.(float64)
		if !ok {
			return nil, fmt.Errorf("numeric parameter '%s' resolved to %T", param.Name, resolved)
		}
		assignments = append(assignments, planner.ExportManifestParameterAssignment{
			Name:  param.Name,
			Value: value,
			Type:  string(IRTypeNumber),
			Unit:  "mm",
		})
	}
	return assignments, nil
}

// evaluateIRExpression evaluates an IR expression to a concrete value.
// It supports short-circuit evaluation for logical operators and ternary expressions.
var errUnresolvedIdentifier = errors.New("unresolved identifier")

type irEvalContext struct {
	tables map[string]*table.Table
}

func evaluateIRExpression(
	expr *IRExpression,
	scope map[string]interface{},
	paramNames map[string]struct{},
	constants map[string]IRConstant,
) (interface{}, error) {
	return evaluateIRExpressionWithContext(expr, scope, paramNames, constants, irEvalContext{})
}

func evaluateIRExpressionWithContext(
	expr *IRExpression,
	scope map[string]interface{},
	paramNames map[string]struct{},
	constants map[string]IRConstant,
	ctx irEvalContext,
) (interface{}, error) {
	switch expr.Kind {
	case ExprKindLiteral:
		return expr.LiteralValue, nil

	case ExprKindReference:
		// Try to resolve as parameter
		if val, ok := scope[expr.ReferenceName]; ok {
			return val, nil
		}
		// Try to resolve as constant
		if constVal, ok := constants[expr.ReferenceName]; ok {
			return constVal.Value, nil
		}
		// Check if it's a known parameter (but not yet resolved)
		if _, isParam := paramNames[expr.ReferenceName]; isParam {
			return nil, fmt.Errorf("%w: %s", errUnresolvedIdentifier, expr.ReferenceName)
		}
		// Treat as enum value
		return expr.ReferenceName, nil

	case ExprKindInterpolatedString:
		var b strings.Builder
		for _, seg := range expr.InterpolatedSegments {
			if seg.ParamName == "" {
				b.WriteString(seg.Text)
				continue
			}
			val, ok := scope[seg.ParamName]
			if !ok {
				if _, isParam := paramNames[seg.ParamName]; isParam {
					return nil, fmt.Errorf("%w: %s", errUnresolvedIdentifier, seg.ParamName)
				}
				return nil, fmt.Errorf("undefined interpolation parameter '%s'", seg.ParamName)
			}
			strVal, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("interpolation parameter '%s' requires string value, got %T", seg.ParamName, val)
			}
			b.WriteString(strVal)
		}
		return b.String(), nil

	case ExprKindBinary:
		return evaluateBinaryExpression(expr, scope, paramNames, constants, ctx)

	case ExprKindUnary:
		return evaluateUnaryExpression(expr, scope, paramNames, constants, ctx)

	case ExprKindTernary:
		return evaluateTernaryExpression(expr, scope, paramNames, constants, ctx)

	case ExprKindCall:
		return evaluateCallExpression(expr, scope, paramNames, constants, ctx)

	default:
		return nil, fmt.Errorf("unknown expression kind: %s", expr.Kind)
	}
}

// evaluateBinaryExpression evaluates a binary expression with short-circuit support.
func evaluateBinaryExpression(
	expr *IRExpression,
	scope map[string]interface{},
	paramNames map[string]struct{},
	constants map[string]IRConstant,
	ctx irEvalContext,
) (interface{}, error) {
	// Handle short-circuit operators first
	switch expr.BinaryOp {
	case "&&":
		leftVal, err := evaluateIRExpressionWithContext(expr.BinaryLeft, scope, paramNames, constants, ctx)
		if err != nil {
			return nil, err
		}
		leftBool, ok := leftVal.(bool)
		if !ok {
			return nil, fmt.Errorf("logical operator '&&' requires boolean operands, got %T", leftVal)
		}
		if !leftBool {
			return false, nil // Short-circuit
		}
		rightVal, err := evaluateIRExpressionWithContext(expr.BinaryRight, scope, paramNames, constants, ctx)
		if err != nil {
			return nil, err
		}
		rightBool, ok := rightVal.(bool)
		if !ok {
			return nil, fmt.Errorf("logical operator '&&' requires boolean operands, got %T", rightVal)
		}
		return leftBool && rightBool, nil

	case "||":
		leftVal, err := evaluateIRExpressionWithContext(expr.BinaryLeft, scope, paramNames, constants, ctx)
		if err != nil {
			return nil, err
		}
		leftBool, ok := leftVal.(bool)
		if !ok {
			return nil, fmt.Errorf("logical operator '||' requires boolean operands, got %T", leftVal)
		}
		if leftBool {
			return true, nil // Short-circuit
		}
		rightVal, err := evaluateIRExpressionWithContext(expr.BinaryRight, scope, paramNames, constants, ctx)
		if err != nil {
			return nil, err
		}
		rightBool, ok := rightVal.(bool)
		if !ok {
			return nil, fmt.Errorf("logical operator '||' requires boolean operands, got %T", rightVal)
		}
		return leftBool || rightBool, nil
	}

	// Evaluate both operands for non-short-circuit operators
	leftVal, err := evaluateIRExpressionWithContext(expr.BinaryLeft, scope, paramNames, constants, ctx)
	if err != nil {
		return nil, err
	}

	rightVal, err := evaluateIRExpressionWithContext(expr.BinaryRight, scope, paramNames, constants, ctx)
	if err != nil {
		return nil, err
	}

	switch expr.BinaryOp {
	case "+":
		leftNum, okLNum := leftVal.(float64)
		rightNum, okRNum := rightVal.(float64)
		if okLNum && okRNum {
			return leftNum + rightNum, nil
		}
		leftStr, okLStr := leftVal.(string)
		rightStr, okRStr := rightVal.(string)
		if okLStr && okRStr {
			return leftStr + rightStr, nil
		}
		return nil, fmt.Errorf("operator '+' requires number+number or string+string, got %T and %T", leftVal, rightVal)
	case "-", "*", "/":
		leftNum, okL := leftVal.(float64)
		rightNum, okR := rightVal.(float64)
		if !okL || !okR {
			return nil, fmt.Errorf("arithmetic operator '%s' requires number operands, got %T and %T", expr.BinaryOp, leftVal, rightVal)
		}
		switch expr.BinaryOp {
		case "-":
			return leftNum - rightNum, nil
		case "*":
			return leftNum * rightNum, nil
		case "/":
			if rightNum == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			return leftNum / rightNum, nil
		}

	case "==", "!=":
		// Support both number and string comparison
		leftNum, okL := leftVal.(float64)
		rightNum, okR := rightVal.(float64)
		if okL && okR {
			if expr.BinaryOp == "==" {
				return leftNum == rightNum, nil
			}
			return leftNum != rightNum, nil
		}
		leftStr, okL := leftVal.(string)
		rightStr, okR := rightVal.(string)
		if okL && okR {
			if expr.BinaryOp == "==" {
				return leftStr == rightStr, nil
			}
			return leftStr != rightStr, nil
		}
		leftBool, okL := leftVal.(bool)
		rightBool, okR := rightVal.(bool)
		if okL && okR {
			if expr.BinaryOp == "==" {
				return leftBool == rightBool, nil
			}
			return leftBool != rightBool, nil
		}
		return nil, fmt.Errorf("comparison operator '%s' requires operands of the same type", expr.BinaryOp)

	case "<", "<=", ">", ">=":
		leftNum, okL := leftVal.(float64)
		rightNum, okR := rightVal.(float64)
		if okL && okR {
			switch expr.BinaryOp {
			case "<":
				return leftNum < rightNum, nil
			case "<=":
				return leftNum <= rightNum, nil
			case ">":
				return leftNum > rightNum, nil
			case ">=":
				return leftNum >= rightNum, nil
			}
		}
		// String comparison
		leftStr, okL := leftVal.(string)
		rightStr, okR := rightVal.(string)
		if okL && okR {
			switch expr.BinaryOp {
			case "<":
				return leftStr < rightStr, nil
			case "<=":
				return leftStr <= rightStr, nil
			case ">":
				return leftStr > rightStr, nil
			case ">=":
				return leftStr >= rightStr, nil
			}
		}
		return nil, fmt.Errorf("comparison operator '%s' requires number or string operands", expr.BinaryOp)

	default:
		return nil, fmt.Errorf("unknown binary operator: %s", expr.BinaryOp)
	}

	return nil, fmt.Errorf("unhandled binary operator: %s", expr.BinaryOp)
}

// evaluateUnaryExpression evaluates a unary expression.
func evaluateUnaryExpression(
	expr *IRExpression,
	scope map[string]interface{},
	paramNames map[string]struct{},
	constants map[string]IRConstant,
	ctx irEvalContext,
) (interface{}, error) {
	val, err := evaluateIRExpressionWithContext(expr.UnaryOperand, scope, paramNames, constants, ctx)
	if err != nil {
		return nil, err
	}

	switch expr.UnaryOp {
	case "-":
		num, ok := val.(float64)
		if !ok {
			return nil, fmt.Errorf("unary operator '-' requires number operand, got %T", val)
		}
		return -num, nil
	case "!":
		b, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("unary operator '!' requires boolean operand, got %T", val)
		}
		return !b, nil
	default:
		return nil, fmt.Errorf("unknown unary operator: %s", expr.UnaryOp)
	}
}

// evaluateTernaryExpression evaluates a ternary expression with short-circuit support.
func evaluateTernaryExpression(
	expr *IRExpression,
	scope map[string]interface{},
	paramNames map[string]struct{},
	constants map[string]IRConstant,
	ctx irEvalContext,
) (interface{}, error) {
	condVal, err := evaluateIRExpressionWithContext(expr.TernaryCond, scope, paramNames, constants, ctx)
	if err != nil {
		return nil, err
	}

	condBool, ok := condVal.(bool)
	if !ok {
		return nil, fmt.Errorf("ternary condition must evaluate to boolean, got %T", condVal)
	}

	if condBool {
		return evaluateIRExpressionWithContext(expr.TernaryTrue, scope, paramNames, constants, ctx)
	}
	return evaluateIRExpressionWithContext(expr.TernaryFalse, scope, paramNames, constants, ctx)
}

// evaluateCallExpression evaluates a function call expression.
func evaluateCallExpression(
	expr *IRExpression,
	scope map[string]interface{},
	paramNames map[string]struct{},
	constants map[string]IRConstant,
	ctx irEvalContext,
) (interface{}, error) {
	if expr.FuncName == "table_cell" {
		return evaluateIRTableCellExpression(expr, scope, paramNames, constants, ctx)
	}

	fn, ok := runtime.Builtins[expr.FuncName]
	if !ok {
		return nil, fmt.Errorf("unknown function: '%s'", expr.FuncName)
	}

	// Evaluate all arguments
	args := make([]interface{}, len(expr.FuncArgs))
	for i, argExpr := range expr.FuncArgs {
		val, err := evaluateIRExpressionWithContext(&argExpr, scope, paramNames, constants, ctx)
		if err != nil {
			return nil, fmt.Errorf("argument %d of '%s': %w", i+1, expr.FuncName, err)
		}
		args[i] = val
	}

	return fn(args)
}

func evaluateIRTableCellExpression(
	expr *IRExpression,
	scope map[string]interface{},
	paramNames map[string]struct{},
	constants map[string]IRConstant,
	ctx irEvalContext,
) (interface{}, error) {
	if len(expr.FuncArgs) != 3 {
		return nil, fmt.Errorf("function '%s' expects 3 arguments, but got %d", expr.FuncName, len(expr.FuncArgs))
	}
	tableName, ok := irStringLiteralValue(expr.FuncArgs[0])
	if !ok {
		return nil, fmt.Errorf("argument 1 of '%s' must be a string literal", expr.FuncName)
	}
	columnName, ok := irStringLiteralValue(expr.FuncArgs[2])
	if !ok {
		return nil, fmt.Errorf("argument 3 of '%s' must be a string literal", expr.FuncName)
	}
	rowKeyValue, err := evaluateIRExpressionWithContext(&expr.FuncArgs[1], scope, paramNames, constants, ctx)
	if err != nil {
		return nil, fmt.Errorf("argument 2 of '%s': %w", expr.FuncName, err)
	}
	rowKey, ok := rowKeyValue.(string)
	if !ok {
		return nil, fmt.Errorf("argument 2 of '%s' must resolve to string, got %T", expr.FuncName, rowKeyValue)
	}
	if ctx.tables == nil {
		return nil, fmt.Errorf("function '%s' references unknown table '%s'", expr.FuncName, tableName)
	}
	tbl, ok := ctx.tables[tableName]
	if !ok || tbl == nil {
		return nil, fmt.Errorf("function '%s' references unknown table '%s'", expr.FuncName, tableName)
	}
	if err := table.Validate(tbl); err != nil {
		return nil, fmt.Errorf("function '%s' table '%s' is invalid: %w", expr.FuncName, tableName, err)
	}
	column, ok := irFindTableColumn(tbl, columnName)
	if !ok {
		return nil, fmt.Errorf("function '%s' references unknown column '%s' in table '%s'", expr.FuncName, columnName, tableName)
	}
	row, err := tbl.RowByKey(rowKey)
	if err != nil {
		return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': %w", expr.FuncName, tableName, rowKey, columnName, err)
	}
	value, ok := row.Values[columnName]
	if !ok {
		if column.Required {
			return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': required column value is missing", expr.FuncName, tableName, rowKey, columnName)
		}
		return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': optional column value is missing", expr.FuncName, tableName, rowKey, columnName)
	}
	switch value.Type {
	case table.ColumnTypeString:
		return value.String, nil
	case table.ColumnTypeNumber:
		number, err := value.Number.Float64()
		if err != nil {
			return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': invalid number value: %w", expr.FuncName, tableName, rowKey, columnName, err)
		}
		return number, nil
	case table.ColumnTypeBoolean:
		return value.Boolean, nil
	default:
		return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': unsupported column type '%s'", expr.FuncName, tableName, rowKey, columnName, value.Type)
	}
}

func irStringLiteralValue(expr IRExpression) (string, bool) {
	if expr.Kind != ExprKindLiteral {
		return "", false
	}
	value, ok := expr.LiteralValue.(string)
	return value, ok
}

func irFindTableColumn(tbl *table.Table, name string) (table.Column, bool) {
	for _, column := range tbl.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return table.Column{}, false
}

// convertOverrideValue parses a string override value into the correct IR type.
func convertOverrideValue(value string, paramType IRType) (interface{}, error) {
	value = normalizeOverrideValue(value)

	switch paramType.Kind {
	case IRTypeNumber:
		return strconv.ParseFloat(value, 64)
	case IRTypeString:
		return value, nil
	case IRTypeBoolean:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("invalid boolean value '%s'", value)
		}
		return b, nil
	case IRTypeEnum:
		for _, allowedValue := range paramType.EnumValues {
			if value == allowedValue {
				return value, nil
			}
		}
		return nil, fmt.Errorf("invalid enum override '%s', allowed values are %v", value, paramType.EnumValues)
	default:
		return nil, fmt.Errorf("unsupported parameter type for override conversion: %s", paramType.Kind)
	}
}

// normalizeOverrideValue trims whitespace and removes surrounding quotes if present.
func normalizeOverrideValue(raw string) string {
	normalized := strings.TrimSpace(raw)
	if len(normalized) >= 2 && strings.HasPrefix(normalized, "\"") && strings.HasSuffix(normalized, "\"") {
		return normalized[1 : len(normalized)-1]
	}
	return normalized
}

func resolveShouldRunCADRunnerFromIR(program *IRProgram) (bool, error) {
	if program == nil || program.ActiveProfileName == nil {
		return false, nil
	}

	var activeProfile *IRProfile
	for i := range program.Profiles {
		if program.Profiles[i].Name == *program.ActiveProfileName {
			activeProfile = &program.Profiles[i]
			break
		}
	}
	if activeProfile == nil {
		return false, fmt.Errorf("selected profile '%s' is not defined", *program.ActiveProfileName)
	}

	expr, ok := activeProfile.Settings["adapter"]
	if !ok {
		return false, nil
	}

	adapter, err := resolveStringSettingFromIR(expr, program.Constants, "adapter")
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(adapter), "freecad"), nil
}

func resolveStringSettingFromIR(expr IRExpression, constants map[string]IRConstant, settingName string) (string, error) {
	switch expr.Kind {
	case ExprKindLiteral:
		s, ok := expr.LiteralValue.(string)
		if !ok {
			return "", fmt.Errorf("profile %s must be a string, got %T", settingName, expr.LiteralValue)
		}
		return strings.TrimSpace(s), nil
	case ExprKindReference:
		if expr.ReferenceType != RefTypeConstant {
			return "", fmt.Errorf("profile %s must reference a string constant", settingName)
		}
		constVal, ok := constants[expr.ReferenceName]
		if !ok {
			return "", fmt.Errorf("profile %s references undefined constant '%s'", settingName, expr.ReferenceName)
		}
		if constVal.Type.Kind != IRTypeString {
			return "", fmt.Errorf("profile %s constant '%s' must be a string, got %s", settingName, expr.ReferenceName, constVal.Type.Kind)
		}
		s, ok := constVal.Value.(string)
		if !ok {
			return "", fmt.Errorf("profile %s constant '%s' has non-string value", settingName, expr.ReferenceName)
		}
		return strings.TrimSpace(s), nil
	default:
		return "", fmt.Errorf("profile %s must be a string literal or string constant", settingName)
	}
}

// resolveActiveFilePatternFromIR resolves the file_pattern from the active profile.
func resolveActiveFilePatternFromIR(program *IRProgram) (string, bool, error) {
	if program.ActiveProfileName == nil {
		return "", false, nil
	}

	var activeProfile *IRProfile
	for _, profile := range program.Profiles {
		if profile.Name == *program.ActiveProfileName {
			activeProfile = &profile
			break
		}
	}
	if activeProfile == nil {
		return "", false, fmt.Errorf("selected profile '%s' is not defined", *program.ActiveProfileName)
	}

	expr, ok := activeProfile.Settings["file_pattern"]
	if !ok {
		return "", false, nil
	}

	// file_pattern must be a literal string or constant reference
	if expr.Kind != ExprKindLiteral && expr.Kind != ExprKindReference {
		return "", false, fmt.Errorf("profile file_pattern must be a string literal or string constant")
	}

	if expr.Kind == ExprKindLiteral {
		v, ok := expr.LiteralValue.(string)
		if !ok {
			return "", false, fmt.Errorf("profile file_pattern must be a string literal or string constant, got %T", expr.LiteralValue)
		}
		return v, true, nil
	}

	// It's a reference - check if it's a constant
	if expr.ReferenceType == RefTypeConstant {
		constVal, ok := program.Constants[expr.ReferenceName]
		if !ok {
			return "", false, fmt.Errorf("profile file_pattern references undefined constant '%s'", expr.ReferenceName)
		}
		if constVal.Type.Kind != IRTypeString {
			return "", false, fmt.Errorf("profile file_pattern constant '%s' must be a string, got %s", expr.ReferenceName, constVal.Type.Kind)
		}
		s, ok := constVal.Value.(string)
		if !ok {
			return "", false, fmt.Errorf("profile file_pattern constant '%s' has non-string value", expr.ReferenceName)
		}
		return s, true, nil
	}

	return "", false, fmt.Errorf("profile file_pattern must reference a string constant")
}

// validateUniqueProductFilenames checks that all product filenames are unique.
func validateUniqueProductFilenames(plan *planner.ExecutionPlan) error {
	seen := make(map[string]string)
	for _, step := range plan.Steps {
		if step.Type != planner.StepWriteCSV {
			continue
		}
		payload, ok := step.Payload.(planner.WriteCSVPayload)
		if !ok {
			continue
		}
		owner, exists := seen[payload.Filename]
		if exists {
			return fmt.Errorf("file_pattern collision: filename '%s' is shared by products '%s' and '%s'", payload.Filename, owner, payload.ProductKey)
		}
		seen[payload.Filename] = payload.ProductKey
	}
	return nil
}

func copyResolvedValues(values map[string]any) map[string]interface{} {
	copied := make(map[string]interface{}, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

// normalizePatternBaseName removes .csv or .step extensions from a filename.
func normalizePatternBaseName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".csv"):
		return name[:len(name)-len(".csv")]
	case strings.HasSuffix(lower, ".step"):
		return name[:len(name)-len(".step")]
	default:
		return name
	}
}

// validateFileBaseName validates that a filename is safe to use.
func validateFileBaseName(name string) error {
	return planner.ValidateFileBaseName(name)
}
