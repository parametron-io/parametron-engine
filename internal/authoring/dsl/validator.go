package dsl

import (
	"fmt"
	"sort"
	"strings"

	"parametron/internal/authoring/runtime"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/table"
	"parametron/internal/shared/security"
)

// Validate performs semantic analysis on the AST.
// It checks for:
// - Duplicate product names
// - Empty product names
// - Duplicate parameter names within a product
// - Duplicate constant names
// - Name collisions between constants and parameters
// - Type mismatches between declared types and expressions
// - Compile-time evaluability of constant expressions
func Validate(ast *AST) error {
	return validateAST(ast, nil)
}

// ValidateWithTables performs semantic analysis with planner-visible table schemas.
func ValidateWithTables(ast *AST, tables map[string]*table.Table) error {
	return validateAST(ast, tables)
}

func validateAST(ast *AST, tables map[string]*table.Table) error {
	if ast == nil {
		return fmt.Errorf("AST is nil")
	}
	if strings.TrimSpace(ast.DSLVersion) == "" {
		return fmt.Errorf("missing version directive")
	}
	if ast.DSLVersion != SupportedDSLVersion {
		return fmt.Errorf("unsupported DSL version")
	}

	seenProducts := make(map[string]bool)
	allParamNames := make(map[string]struct{})

	for _, p := range ast.Products {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("product name cannot be empty")
		}

		if seenProducts[p.Name] {
			return fmt.Errorf("duplicate product name: '%s'", p.Name)
		}
		seenProducts[p.Name] = true

		for _, param := range p.Parameters {
			allParamNames[param.Name] = struct{}{}
		}
	}

	constantDecls := make(map[string]*ConstDeclarationNode)
	for _, constDecl := range ast.Constants {
		if strings.TrimSpace(constDecl.Name) == "" {
			return fmt.Errorf("constant name cannot be empty")
		}
		if _, exists := constantDecls[constDecl.Name]; exists {
			return fmt.Errorf("constant already defined: '%s'", constDecl.Name)
		}
		if _, exists := allParamNames[constDecl.Name]; exists {
			return fmt.Errorf("cannot override constant: '%s'", constDecl.Name)
		}
		constantDecls[constDecl.Name] = constDecl
	}

	resolvedConstants := make(map[string]ConstantValue, len(constantDecls))
	visiting := make(map[string]bool, len(constantDecls))
	for name := range constantDecls {
		if _, err := resolveConstant(name, constantDecls, resolvedConstants, visiting, allParamNames); err != nil {
			return err
		}
	}
	ast.ResolvedConstants = resolvedConstants

	if err := validateProfiles(ast, resolvedConstants); err != nil {
		return err
	}

	for _, p := range ast.Products {
		if err := validateProductExecutionDeclarations(p, resolvedConstants); err != nil {
			return fmt.Errorf("validation error in product '%s': %w", p.Name, err)
		}
		if err := validateProductParams(p, resolvedConstants, tables); err != nil {
			return fmt.Errorf("validation error in product '%s': %w", p.Name, err)
		}
		if err := validateProductTargetActions(p, resolvedConstants, tables); err != nil {
			return fmt.Errorf("validation error in product '%s': %w", p.Name, err)
		}
	}
	return nil
}

var canonicalActionValues = []string{
	"keep",
	"suppress",
	"unsuppress",
	"hide",
	"unhide",
	"delete",
}

func isCanonicalActionValue(value string) bool {
	for _, allowed := range canonicalActionValues {
		if value == allowed {
			return true
		}
	}
	return false
}

// CanonicalActionValues returns the ordered finite domain used for contextual
// target-action validation and evaluation.
func CanonicalActionValues() []string {
	return append([]string(nil), canonicalActionValues...)
}

// IsCanonicalActionValue reports whether value is an exact contextual action.
func IsCanonicalActionValue(value string) bool {
	return isCanonicalActionValue(value)
}

var supportedExecutionAdapters = map[string]map[string]struct{}{
	"freecad": {
		"step": {},
		"csv":  {},
		"pdf":  {},
		"none": {},
	},
	"none": {},
}

func validateProfiles(ast *AST, constScope map[string]ConstantValue) error {
	seenNames := make(map[string]struct{}, len(ast.Profiles))

	for _, profile := range ast.Profiles {
		if strings.TrimSpace(profile.Name) == "" {
			return fmt.Errorf("profile name cannot be empty")
		}
		if _, exists := seenNames[profile.Name]; exists {
			return fmt.Errorf("duplicate profile name: '%s'", profile.Name)
		}
		seenNames[profile.Name] = struct{}{}

		seenKeys := make(map[string]struct{}, len(profile.SettingOrder))
		for _, key := range profile.SettingOrder {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("profile '%s' contains empty setting key", profile.Name)
			}
			if _, exists := seenKeys[key]; exists {
				return fmt.Errorf("duplicate profile setting key '%s' in profile '%s'", key, profile.Name)
			}
			seenKeys[key] = struct{}{}
		}

		for key, expr := range profile.Settings {
			if err := validateProfileSettingExpression(expr, constScope); err != nil {
				return fmt.Errorf("profile '%s', setting '%s': %w", profile.Name, key, err)
			}

			// Special validation for output_dir to catch path traversal at DSL parse time
			if key == "output_dir" {
				if err := validateOutputDirSetting(expr, constScope); err != nil {
					return fmt.Errorf("profile '%s', setting '%s': %w", profile.Name, key, err)
				}
			}
			if key == "adapter" || key == "source_model" || key == "outputs" {
				return fmt.Errorf("profile '%s', setting '%s': execution declarations must be declared inside product blocks", profile.Name, key)
			}
		}
	}

	if ast.ActiveProfileName != nil {
		if _, exists := seenNames[*ast.ActiveProfileName]; !exists {
			return fmt.Errorf("selected profile '%s' is not defined", *ast.ActiveProfileName)
		}
	}

	if len(ast.Profiles) == 1 && ast.ActiveProfileName == nil {
		name := ast.Profiles[0].Name
		ast.ActiveProfileName = &name
	}
	if len(ast.Profiles) > 1 && ast.ActiveProfileName == nil {
		return fmt.Errorf("multiple profiles defined; explicit 'use profile <Name>' selection is required")
	}

	return nil
}

func validateProfileSettingExpression(expr ExpressionNode, constScope map[string]ConstantValue) error {
	switch e := expr.(type) {
	case *LiteralExpression:
		switch e.Value.(type) {
		case float64, string, bool:
			return nil
		default:
			return fmt.Errorf("unsupported literal type %T", e.Value)
		}
	case *IdentifierExpression:
		if _, exists := constScope[e.Name]; exists {
			return nil
		}
		return fmt.Errorf("undefined constant: %s", e.Name)
	case *ArrayExpression:
		for _, elem := range e.Elements {
			if err := validateProfileSettingExpression(elem, constScope); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("profile setting value must be a literal, identifier, or array")
	}
}

// validateOutputDirSetting validates the output_dir setting for path traversal attacks.
// This provides early (DSL parse time) detection of unsafe paths.
func validateOutputDirSetting(expr ExpressionNode, constScope map[string]ConstantValue) error {
	var outputDir string

	switch e := expr.(type) {
	case *LiteralExpression:
		s, ok := e.Value.(string)
		if !ok {
			return fmt.Errorf("output_dir must be a string, got %T", e.Value)
		}
		outputDir = s
	case *IdentifierExpression:
		// Check if it's a constant reference
		constVal, exists := constScope[e.Name]
		if !exists {
			return fmt.Errorf("output_dir identifier '%s' must reference a defined constant", e.Name)
		}
		if constVal.Type.Kind != ParamTypeString {
			return fmt.Errorf("output_dir constant '%s' must be a string, got %s", e.Name, constVal.Type)
		}
		s, ok := constVal.Value.(string)
		if !ok {
			return fmt.Errorf("output_dir constant '%s' has non-string value", e.Name)
		}
		outputDir = s
	default:
		return fmt.Errorf("output_dir must be a string literal or string constant")
	}

	// Validate the path for security issues
	if err := security.ValidateSafePath(outputDir, ""); err != nil {
		return fmt.Errorf("unsafe output_dir: %w", err)
	}

	return nil
}

func validateStringProfileSetting(settingKey string, expr ExpressionNode, constScope map[string]ConstantValue) error {
	_, err := resolveStringProfileSettingValue(settingKey, expr, constScope)
	return err
}

func resolveStringProfileSettingValue(settingKey string, expr ExpressionNode, constScope map[string]ConstantValue) (string, error) {
	switch e := expr.(type) {
	case *LiteralExpression:
		s, ok := e.Value.(string)
		if !ok {
			return "", fmt.Errorf("%s must be a string, got %T", settingKey, e.Value)
		}
		return s, nil
	case *IdentifierExpression:
		constVal, exists := constScope[e.Name]
		if !exists {
			return "", fmt.Errorf("%s identifier '%s' must reference a defined constant", settingKey, e.Name)
		}
		if constVal.Type.Kind != ParamTypeString {
			return "", fmt.Errorf("%s constant '%s' must be a string, got %s", settingKey, e.Name, constVal.Type)
		}
		s, ok := constVal.Value.(string)
		if !ok {
			return "", fmt.Errorf("%s constant '%s' has non-string value", settingKey, e.Name)
		}
		return s, nil
	default:
		return "", fmt.Errorf("%s must be a string literal or string constant", settingKey)
	}
}

func validateOutputsProfileSetting(expr ExpressionNode, constScope map[string]ConstantValue) error {
	arrayExpr, ok := expr.(*ArrayExpression)
	if !ok {
		return fmt.Errorf("outputs must be a list of strings")
	}
	if len(arrayExpr.Elements) == 0 {
		return fmt.Errorf("outputs must contain at least one format")
	}

	for idx, elem := range arrayExpr.Elements {
		raw, err := resolveStringProfileSettingValue("outputs", elem, constScope)
		if err != nil {
			return fmt.Errorf("outputs[%d]: %w", idx, err)
		}
		format := strings.ToLower(strings.TrimSpace(raw))
		if format == "" {
			return fmt.Errorf("outputs[%d] must not be empty", idx)
		}
		if !artifact.IsSupportedExportOutputType(format) {
			return fmt.Errorf("outputs[%d] has unsupported format %q", idx, raw)
		}
	}

	return nil
}

func validateProductExecutionDeclarations(p *ProductNode, constScope map[string]ConstantValue) error {
	seenKeys := make(map[string]struct{}, len(p.ExecutionDeclarationOrder))
	for _, key := range p.ExecutionDeclarationOrder {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("product '%s' contains empty execution declaration key", p.Name)
		}
		if _, exists := seenKeys[key]; exists {
			return fmt.Errorf("duplicate product execution declaration key '%s'", key)
		}
		seenKeys[key] = struct{}{}
	}

	var adapter string
	if p.Adapter != nil {
		var err error
		adapter, err = resolveStringProfileSettingValue("adapter", p.Adapter, constScope)
		if err != nil {
			return err
		}
		adapter = strings.ToLower(strings.TrimSpace(adapter))
		if _, ok := supportedExecutionAdapters[adapter]; !ok {
			return fmt.Errorf("adapter %q is not supported", adapter)
		}
	}

	if p.SourceModel != nil {
		if _, err := resolveStringProfileSettingValue("source_model", p.SourceModel, constScope); err != nil {
			return err
		}
	}

	outputs, outputsPresent, err := resolveProductOutputs(p.Outputs, constScope)
	if err != nil {
		return err
	}

	if adapter == "" {
		if p.SourceModel != nil {
			return fmt.Errorf("source_model requires adapter to be declared")
		}
		if outputsPresent {
			return fmt.Errorf("outputs requires adapter to be declared")
		}
		return nil
	}

	if adapter == "none" {
		if outputsPresent && len(outputs) > 0 {
			return fmt.Errorf(`outputs must be empty when adapter is "none"`)
		}
		return nil
	}

	if p.SourceModel == nil {
		return fmt.Errorf(`source_model is required when adapter is %q`, adapter)
	}
	for _, output := range outputs {
		if output == "none" && len(outputs) != 1 {
			return fmt.Errorf(`output format "none" must be the only declared output`)
		}
	}
	for idx, output := range outputs {
		if _, ok := supportedExecutionAdapters[adapter][output]; !ok {
			return fmt.Errorf("outputs[%d] has unsupported format %q for adapter %q", idx, output, adapter)
		}
	}

	return nil
}

func resolveProductOutputs(expr ExpressionNode, constScope map[string]ConstantValue) ([]string, bool, error) {
	if expr == nil {
		return nil, false, nil
	}

	arrayExpr, ok := expr.(*ArrayExpression)
	if !ok {
		return nil, true, fmt.Errorf("outputs must be a list of strings")
	}

	outputs := make([]string, 0, len(arrayExpr.Elements))
	for idx, elem := range arrayExpr.Elements {
		raw, err := resolveStringProfileSettingValue("outputs", elem, constScope)
		if err != nil {
			return nil, true, fmt.Errorf("outputs[%d]: %w", idx, err)
		}
		output := strings.ToLower(strings.TrimSpace(raw))
		if output == "" {
			return nil, true, fmt.Errorf("outputs[%d] must not be empty", idx)
		}
		outputs = append(outputs, output)
	}

	return outputs, true, nil
}

func validateProductParams(p *ProductNode, constScope map[string]ConstantValue, tables map[string]*table.Table) error {
	paramScope, err := buildProductBindingScope(p, constScope, tables)
	if err != nil {
		return err
	}

	// Validate each let expression using its inferred type.
	for _, let := range p.Lets {
		letBinding := paramScope[let.Name]
		err := validateExpressionWithTables(let.Value, letBinding.Type, let.Name, paramScope, constScope, tables)
		if err != nil {
			return fmt.Errorf("in let '%s' definition: %w", let.Name, err)
		}
	}

	// Validate each parameter default value expression.
	for _, param := range p.Parameters {
		err := validateExpressionWithTables(param.DefaultValue, param.Type, param.Name, paramScope, constScope, tables)
		if err != nil {
			return fmt.Errorf("in parameter '%s' definition: %w", param.Name, err)
		}
	}

	return nil
}

func validateProductTargetActions(p *ProductNode, constScope map[string]ConstantValue, tables map[string]*table.Table) error {
	scope, err := buildProductBindingScope(p, constScope, tables)
	if err != nil {
		return err
	}

	expectedType := ParameterType{Kind: ParamTypeAction}
	for _, targetAction := range p.TargetActions {
		if err := validateExpressionWithTables(targetAction.Action, expectedType, targetAction.SemanticTarget, scope, constScope, tables); err != nil {
			return fmt.Errorf("in target action '%s': %w", targetAction.SemanticTarget, err)
		}
	}

	return nil
}

type productBindingDecl struct {
	Name         string
	Kind         string
	Type         ParameterType
	Expr         ExpressionNode
	HasFixedType bool
	Order        int
}

func buildProductBindingScope(product *ProductNode, constScope map[string]ConstantValue, tables map[string]*table.Table) (map[string]*ParameterNode, error) {
	bindings := make(map[string]*productBindingDecl, len(product.Lets)+len(product.Parameters))
	scope := make(map[string]*ParameterNode, len(product.Lets)+len(product.Parameters))
	order := 0

	for _, let := range product.Lets {
		if strings.TrimSpace(let.Name) == "" {
			return nil, fmt.Errorf("let name cannot be empty")
		}
		if existing, exists := bindings[let.Name]; exists {
			switch existing.Kind {
			case "let":
				return nil, fmt.Errorf("duplicate let name: '%s'", let.Name)
			case "parameter":
				return nil, fmt.Errorf("duplicate product binding name: '%s'", let.Name)
			}
		}
		if _, exists := constScope[let.Name]; exists {
			return nil, fmt.Errorf("cannot override constant: '%s'", let.Name)
		}
		bindings[let.Name] = &productBindingDecl{
			Name:  let.Name,
			Kind:  "let",
			Expr:  let.Value,
			Order: order,
		}
		order++
	}

	for _, param := range product.Parameters {
		if strings.TrimSpace(param.Name) == "" {
			return nil, fmt.Errorf("parameter name cannot be empty")
		}
		if existing, exists := bindings[param.Name]; exists {
			switch existing.Kind {
			case "parameter":
				return nil, fmt.Errorf("duplicate parameter name: '%s'", param.Name)
			case "let":
				return nil, fmt.Errorf("duplicate product binding name: '%s'", param.Name)
			}
		}
		if _, exists := constScope[param.Name]; exists {
			return nil, fmt.Errorf("cannot override constant: '%s'", param.Name)
		}

		if param.Type.Kind == ParamTypeEnum {
			seenEnumValues := make(map[string]bool)
			for _, v := range param.Type.EnumValues {
				if seenEnumValues[v] {
					return nil, fmt.Errorf("duplicate enum value '%s' in parameter '%s'", v, param.Name)
				}
				seenEnumValues[v] = true
			}
		}

		bindings[param.Name] = &productBindingDecl{
			Name:         param.Name,
			Kind:         "parameter",
			Type:         param.Type,
			Expr:         param.DefaultValue,
			HasFixedType: true,
			Order:        order,
		}
		scope[param.Name] = param
		order++
	}

	if err := validateProductBindingGraph(product, bindings); err != nil {
		return nil, err
	}

	inferred := make(map[string]ParameterType, len(product.Lets))
	visiting := make(map[string]bool, len(product.Lets))
	for _, let := range product.Lets {
		typ, err := inferProductBindingType(let.Name, bindings, inferred, visiting, constScope, tables)
		if err != nil {
			return nil, fmt.Errorf("in let '%s' definition: %w", let.Name, err)
		}
		scope[let.Name] = &ParameterNode{
			Name: let.Name,
			Type: typ,
		}
	}

	return scope, nil
}

// BuildProductBindingScopeForPlanning returns the validated product-local binding scope
// used by planning and IR conversion. The returned scope includes both lets and params,
// with lets represented as ParameterNode values carrying their inferred type.
func BuildProductBindingScopeForPlanning(product *ProductNode, constScope map[string]ConstantValue, tables map[string]*table.Table) (map[string]*ParameterNode, error) {
	return buildProductBindingScope(product, constScope, tables)
}

func validateProductBindingGraph(product *ProductNode, bindings map[string]*productBindingDecl) error {
	adj := make(map[string][]string, len(bindings))
	for _, binding := range bindings {
		refs := collectBindingRefs(binding.Expr, bindings)
		sort.Slice(refs, func(i, j int) bool {
			return bindings[refs[i]].Order < bindings[refs[j]].Order
		})
		adj[binding.Name] = refs
	}

	// 0 = unvisited, 1 = visiting, 2 = done
	colors := make(map[string]int, len(bindings))
	stack := make([]string, 0, len(bindings))

	var dfs func(string) error
	dfs = func(node string) error {
		colors[node] = 1
		stack = append(stack, node)

		for _, dep := range adj[node] {
			if colors[dep] == 0 {
				if err := dfs(dep); err != nil {
					return err
				}
				continue
			}
			if colors[dep] == 1 {
				cycle := buildCyclePath(stack, dep)
				return fmt.Errorf("circular dependency detected in product '%s': %s", product.Name, strings.Join(cycle, " → "))
			}
		}

		stack = stack[:len(stack)-1]
		colors[node] = 2
		return nil
	}

	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return bindings[names[i]].Order < bindings[names[j]].Order
	})

	for _, name := range names {
		if colors[name] != 0 {
			continue
		}
		if err := dfs(name); err != nil {
			return err
		}
	}

	return nil
}

func buildCyclePath(stack []string, cycleStart string) []string {
	startIdx := 0
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == cycleStart {
			startIdx = i
			break
		}
	}

	cycle := make([]string, 0, len(stack)-startIdx+1)
	cycle = append(cycle, stack[startIdx:]...)
	cycle = append(cycle, cycleStart)
	return cycle
}

func collectBindingRefs(expr ExpressionNode, bindings map[string]*productBindingDecl) []string {
	seen := make(map[string]struct{})
	refs := make([]string, 0)

	var walk func(ExpressionNode)
	walk = func(node ExpressionNode) {
		switch e := node.(type) {
		case *IdentifierExpression:
			if _, ok := bindings[e.Name]; ok {
				if _, exists := seen[e.Name]; !exists {
					seen[e.Name] = struct{}{}
					refs = append(refs, e.Name)
				}
			}
		case *BinaryExpression:
			walk(e.Left)
			walk(e.Right)
		case *UnaryExpression:
			walk(e.Operand)
		case *TernaryExpression:
			walk(e.Condition)
			walk(e.TrueExpr)
			walk(e.FalseExpr)
		case *FunctionCallExpression:
			for _, arg := range e.Args {
				walk(arg)
			}
		case *InterpolatedStringExpression:
			for _, seg := range e.Segments {
				if seg.ParamName == "" {
					continue
				}
				if _, ok := bindings[seg.ParamName]; ok {
					if _, exists := seen[seg.ParamName]; !exists {
						seen[seg.ParamName] = struct{}{}
						refs = append(refs, seg.ParamName)
					}
				}
			}
		case *LiteralExpression:
			return
		}
	}

	walk(expr)
	return refs
}

func inferProductBindingType(name string, bindings map[string]*productBindingDecl, inferred map[string]ParameterType, visiting map[string]bool, constScope map[string]ConstantValue, tables map[string]*table.Table) (ParameterType, error) {
	if typ, ok := inferred[name]; ok {
		return typ, nil
	}

	binding, ok := bindings[name]
	if !ok {
		return ParameterType{}, fmt.Errorf("undefined binding reference: '%s'", name)
	}
	if binding.HasFixedType {
		return binding.Type, nil
	}
	if visiting[name] {
		return ParameterType{}, fmt.Errorf("circular dependency detected in product binding '%s'", name)
	}

	visiting[name] = true
	typ, err := inferExpressionType(binding.Expr, func(refName string) (ParameterType, bool, error) {
		if refBinding, exists := bindings[refName]; exists {
			if refBinding.HasFixedType {
				return refBinding.Type, true, nil
			}
			refType, err := inferProductBindingType(refName, bindings, inferred, visiting, constScope, tables)
			return refType, true, err
		}
		if constVal, exists := constScope[refName]; exists {
			return constVal.Type, true, nil
		}
		return ParameterType{}, false, nil
	}, tables)
	delete(visiting, name)
	if err != nil {
		return ParameterType{}, err
	}

	inferred[name] = typ
	return typ, nil
}

func inferExpressionType(expr ExpressionNode, resolveName func(string) (ParameterType, bool, error), tables map[string]*table.Table) (ParameterType, error) {
	switch e := expr.(type) {
	case *LiteralExpression:
		switch e.Value.(type) {
		case float64:
			return ParameterType{Kind: ParamTypeNumber}, nil
		case string:
			return ParameterType{Kind: ParamTypeString}, nil
		case bool:
			return ParameterType{Kind: ParamTypeBoolean}, nil
		default:
			return ParameterType{}, fmt.Errorf("unsupported literal type %T", e.Value)
		}

	case *InterpolatedStringExpression:
		for _, seg := range e.Segments {
			if seg.ParamName == "" {
				continue
			}
			refType, ok, err := resolveName(seg.ParamName)
			if err != nil {
				return ParameterType{}, err
			}
			if !ok {
				return ParameterType{}, fmt.Errorf("undefined binding reference in interpolation: '%s'", seg.ParamName)
			}
			if refType.Kind != ParamTypeString {
				return ParameterType{}, fmt.Errorf("interpolation binding '%s' has type '%s', but expected 'string'", seg.ParamName, refType)
			}
		}
		return ParameterType{Kind: ParamTypeString}, nil

	case *IdentifierExpression:
		if typ, ok, err := resolveName(e.Name); err != nil {
			return ParameterType{}, err
		} else if ok {
			return typ, nil
		}
		return ParameterType{}, fmt.Errorf("undefined binding reference: '%s'", e.Name)

	case *BinaryExpression:
		leftType, err := inferExpressionType(e.Left, resolveName, tables)
		if err != nil {
			return ParameterType{}, fmt.Errorf("left side of operator '%s': %w", e.Operator, err)
		}
		rightType, err := inferExpressionType(e.Right, resolveName, tables)
		if err != nil {
			return ParameterType{}, fmt.Errorf("right side of operator '%s': %w", e.Operator, err)
		}

		switch e.Operator {
		case "+":
			if leftType.Kind == ParamTypeString || rightType.Kind == ParamTypeString {
				if leftType.Kind != ParamTypeString || rightType.Kind != ParamTypeString {
					return ParameterType{}, fmt.Errorf("operator '+' requires both operands to be 'string' or both to be 'number'")
				}
				return ParameterType{Kind: ParamTypeString}, nil
			}
			if leftType.Kind != ParamTypeNumber || rightType.Kind != ParamTypeNumber {
				return ParameterType{}, fmt.Errorf("operator '+' requires both operands to be 'string' or both to be 'number'")
			}
			return ParameterType{Kind: ParamTypeNumber}, nil
		case "-", "*", "/":
			if leftType.Kind != ParamTypeNumber || rightType.Kind != ParamTypeNumber {
				return ParameterType{}, fmt.Errorf("operator '%s' requires number operands", e.Operator)
			}
			return ParameterType{Kind: ParamTypeNumber}, nil
		case "==", "!=":
			if leftType.Kind != rightType.Kind {
				return ParameterType{}, fmt.Errorf("operator '%s' requires operands of the same type", e.Operator)
			}
			if leftType.Kind == ParamTypeEnum && !enumTypesEqual(leftType, rightType) {
				return ParameterType{}, fmt.Errorf("operator '%s' requires matching enum types", e.Operator)
			}
			return ParameterType{Kind: ParamTypeBoolean}, nil
		case "<", "<=", ">", ">=":
			if leftType.Kind != rightType.Kind {
				return ParameterType{}, fmt.Errorf("operator '%s' requires operands of the same type", e.Operator)
			}
			if leftType.Kind != ParamTypeNumber && leftType.Kind != ParamTypeString {
				return ParameterType{}, fmt.Errorf("operator '%s' requires number or string operands", e.Operator)
			}
			return ParameterType{Kind: ParamTypeBoolean}, nil
		case "&&", "||":
			if leftType.Kind != ParamTypeBoolean || rightType.Kind != ParamTypeBoolean {
				return ParameterType{}, fmt.Errorf("operator '%s' requires boolean operands", e.Operator)
			}
			return ParameterType{Kind: ParamTypeBoolean}, nil
		default:
			return ParameterType{}, fmt.Errorf("unknown operator in binary expression: '%s'", e.Operator)
		}

	case *UnaryExpression:
		operandType, err := inferExpressionType(e.Operand, resolveName, tables)
		if err != nil {
			return ParameterType{}, fmt.Errorf("operand of unary '%s': %w", e.Operator, err)
		}
		switch e.Operator {
		case "-":
			if operandType.Kind != ParamTypeNumber {
				return ParameterType{}, fmt.Errorf("unary operator '-' requires number operand")
			}
			return ParameterType{Kind: ParamTypeNumber}, nil
		case "!":
			if operandType.Kind != ParamTypeBoolean {
				return ParameterType{}, fmt.Errorf("unary operator '!' requires boolean operand")
			}
			return ParameterType{Kind: ParamTypeBoolean}, nil
		default:
			return ParameterType{}, fmt.Errorf("unknown unary operator: '%s'", e.Operator)
		}

	case *TernaryExpression:
		condType, err := inferExpressionType(e.Condition, resolveName, tables)
		if err != nil {
			return ParameterType{}, fmt.Errorf("ternary condition must be a boolean expression: %w", err)
		}
		if condType.Kind != ParamTypeBoolean {
			return ParameterType{}, fmt.Errorf("ternary condition must be boolean")
		}
		trueType, err := inferExpressionType(e.TrueExpr, resolveName, tables)
		if err != nil {
			return ParameterType{}, fmt.Errorf("ternary 'true' branch error: %w", err)
		}
		falseType, err := inferExpressionType(e.FalseExpr, resolveName, tables)
		if err != nil {
			return ParameterType{}, fmt.Errorf("ternary 'false' branch error: %w", err)
		}
		if trueType.Kind != falseType.Kind || (trueType.Kind == ParamTypeEnum && !enumTypesEqual(trueType, falseType)) {
			return ParameterType{}, fmt.Errorf("ternary branches must return the same type")
		}
		return trueType, nil

	case *FunctionCallExpression:
		if e.Name == "table_cell" {
			if kind, ok := inferTableCellResultKind(e, tables); ok {
				return ParameterType{Kind: kind}, nil
			}
			return ParameterType{}, fmt.Errorf("function '%s' references unknown table '%s'", e.Name, firstTableCellArgString(e.Args))
		}
		if _, ok := runtime.Builtins[e.Name]; !ok {
			return ParameterType{}, fmt.Errorf("unknown function: '%s'", e.Name)
		}
		if err := validateBuiltinFunctionArity(e.Name, len(e.Args)); err != nil {
			return ParameterType{}, err
		}
		for i, arg := range e.Args {
			argType, err := inferExpressionType(arg, resolveName, tables)
			if err != nil {
				if len(e.Args) == 1 {
					return ParameterType{}, fmt.Errorf("argument of '%s': %w", e.Name, err)
				}
				return ParameterType{}, fmt.Errorf("argument %d of '%s': %w", i+1, e.Name, err)
			}
			if argType.Kind != ParamTypeNumber {
				return ParameterType{}, fmt.Errorf("argument %d of '%s' must be number", i+1, e.Name)
			}
		}
		return ParameterType{Kind: ParamTypeNumber}, nil
	}

	return ParameterType{}, fmt.Errorf("unknown expression type: %T", expr)
}

// validateExpression checks if an expression tree is valid and matches the expected type.
func validateExpression(expr ExpressionNode, expectedType ParameterType, paramName string, scope map[string]*ParameterNode, constScope map[string]ConstantValue) error {
	return validateExpressionWithTables(expr, expectedType, paramName, scope, constScope, nil)
}

func validateExpressionWithTables(expr ExpressionNode, expectedType ParameterType, paramName string, scope map[string]*ParameterNode, constScope map[string]ConstantValue, tables map[string]*table.Table) error {
	switch e := expr.(type) {
	case *LiteralExpression:
		switch expectedType.Kind {
		case ParamTypeNumber:
			_, ok := e.Value.(float64)
			if !ok {
				return fmt.Errorf("expected number literal, but got type %T", e.Value)
			}
		case ParamTypeString:
			s, ok := e.Value.(string)
			if !ok {
				return fmt.Errorf("expected string literal, but got type %T", e.Value)
			}
			if err := validateStringLiteralControlChars(s, paramName); err != nil {
				return err
			}
		case ParamTypeBoolean:
			_, ok := e.Value.(bool)
			if !ok {
				return fmt.Errorf("expected boolean literal, but got type %T", e.Value)
			}
		case ParamTypeEnum:
			if _, ok := e.Value.(string); ok {
				return fmt.Errorf("expected enum literal, but got string literal")
			}
			return fmt.Errorf("expected enum literal, but got type %T", e.Value)
		case ParamTypeAction:
			switch e.Value.(type) {
			case string:
				return fmt.Errorf("expected action literal, but got string literal")
			case bool:
				return fmt.Errorf("expected action literal, but got boolean literal")
			case float64:
				return fmt.Errorf("expected action literal, but got number literal")
			default:
				return fmt.Errorf("expected action literal, but got type %T", e.Value)
			}
		}
		return nil

	case *InterpolatedStringExpression:
		if expectedType.Kind != ParamTypeString {
			return fmt.Errorf("interpolated string expression results in a string, but expected type is '%s'", expectedType)
		}
		for _, seg := range e.Segments {
			if seg.ParamName == "" {
				if err := validateStringLiteralControlChars(seg.Text, paramName); err != nil {
					return err
				}
				continue
			}
			refParam, ok := scope[seg.ParamName]
			if !ok {
				return fmt.Errorf("undefined binding reference in interpolation: '%s'", seg.ParamName)
			}
			if refParam.Type.Kind != ParamTypeString {
				return fmt.Errorf("interpolation binding '%s' has type '%s', but expected 'string'", seg.ParamName, refParam.Type)
			}
		}
		return nil

	case *IdentifierExpression:
		if expectedType.Kind == ParamTypeAction {
			if isCanonicalActionValue(e.Name) {
				return nil
			}

			if refParam, ok := scope[e.Name]; ok {
				if refParam.Type.Kind != ParamTypeAction {
					return fmt.Errorf("binding reference '%s' has type '%s', but expected '%s'", e.Name, refParam.Type, expectedType)
				}
				return nil
			}

			if constVal, ok := constScope[e.Name]; ok {
				if constVal.Type.Kind != ParamTypeAction {
					return fmt.Errorf("constant reference '%s' has type '%s', but expected '%s'", e.Name, constVal.Type, expectedType)
				}
				return nil
			}

			return fmt.Errorf("invalid action value '%s', allowed values are %v", e.Name, canonicalActionValues)
		}

		if expectedType.Kind == ParamTypeEnum {
			if len(expectedType.EnumValues) == 0 {
				return fmt.Errorf("enum type definition is required for '%s'", e.Name)
			}

			for _, enumValue := range expectedType.EnumValues {
				if e.Name == enumValue {
					return nil
				}
			}

			if refParam, ok := scope[e.Name]; ok {
				if refParam.Type.Kind != ParamTypeEnum || !enumTypesEqual(refParam.Type, expectedType) {
					return fmt.Errorf("enum type mismatch between '%s' and '%s'", e.Name, expectedType)
				}
				return nil
			}

			if constVal, ok := constScope[e.Name]; ok {
				if constVal.Type.Kind != ParamTypeEnum {
					return fmt.Errorf("constant reference '%s' has type '%s', but expected '%s'", e.Name, constVal.Type, expectedType)
				}
				enumVal, ok := constVal.Value.(string)
				if !ok {
					return fmt.Errorf("invalid enum value '%v' from constant '%s'", constVal.Value, e.Name)
				}
				for _, allowed := range expectedType.EnumValues {
					if enumVal == allowed {
						return nil
					}
				}
				return fmt.Errorf("invalid enum value '%s', allowed values are %v", enumVal, expectedType.EnumValues)
			}

			return fmt.Errorf("invalid enum value '%s', allowed values are %v", e.Name, expectedType.EnumValues)
		}

		if refParam, ok := scope[e.Name]; ok {
			if refParam.Type.Kind != expectedType.Kind {
				return fmt.Errorf("binding reference '%s' has type '%s', but expected '%s'", e.Name, refParam.Type, expectedType)
			}
			return nil
		}

		if constVal, ok := constScope[e.Name]; ok {
			if constVal.Type.Kind != expectedType.Kind {
				return fmt.Errorf("constant reference '%s' has type '%s', but expected '%s'", e.Name, constVal.Type, expectedType)
			}
			if expectedType.Kind == ParamTypeString {
				s, ok := constVal.Value.(string)
				if !ok {
					return fmt.Errorf("constant reference '%s' has non-string value type %T", e.Name, constVal.Value)
				}
				if err := validateStringLiteralControlChars(s, paramName); err != nil {
					return err
				}
			}
			return nil
		}

		return fmt.Errorf("undefined binding reference: '%s'", e.Name)

	case *BinaryExpression:
		if expectedType.Kind == ParamTypeEnum {
			return fmt.Errorf("enum type does not support operator '%s'", e.Operator)
		}

		switch e.Operator {
		case "+":
			switch expectedType.Kind {
			case ParamTypeNumber:
				if err := validateExpressionWithTables(e.Left, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("left side of operator '%s': %w", e.Operator, err)
				}
				if err := validateExpressionWithTables(e.Right, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("right side of operator '%s': %w", e.Operator, err)
				}
				return nil
			case ParamTypeString:
				if err := validateExpressionWithTables(e.Left, ParameterType{Kind: ParamTypeString}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("left side of operator '%s': %w", e.Operator, err)
				}
				if err := validateExpressionWithTables(e.Right, ParameterType{Kind: ParamTypeString}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("right side of operator '%s': %w", e.Operator, err)
				}
				return nil
			default:
				return fmt.Errorf("operator '+' results in number or string, but expected type is '%s'", expectedType)
			}
		case "-", "*", "/":
			if expectedType.Kind != ParamTypeNumber {
				return fmt.Errorf("arithmetic expression results in a number, but expected type is '%s'", expectedType)
			}
			if err := validateExpressionWithTables(e.Left, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("left side of operator '%s': %w", e.Operator, err)
			}
			if err := validateExpressionWithTables(e.Right, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("right side of operator '%s': %w", e.Operator, err)
			}
			return nil
		case "==", "!=":
			if expectedType.Kind != ParamTypeBoolean {
				return fmt.Errorf("comparison expression results in a boolean, but expected type is '%s'", expectedType)
			}
			// Deterministic equality type resolution precedence:
			// enum -> string -> boolean -> number.
			if enumType, ok := inferEnumTypeFromExpr(e.Left, scope, constScope); ok {
				if err := validateExpressionWithTables(e.Left, enumType, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("left side of comparison '%s': %w", e.Operator, err)
				}
				if err := validateExpressionWithTables(e.Right, enumType, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("right side of comparison '%s': %w", e.Operator, err)
				}
				return nil
			}
			if enumType, ok := inferEnumTypeFromExpr(e.Right, scope, constScope); ok {
				if err := validateExpressionWithTables(e.Left, enumType, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("left side of comparison '%s': %w", e.Operator, err)
				}
				if err := validateExpressionWithTables(e.Right, enumType, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("right side of comparison '%s': %w", e.Operator, err)
				}
				return nil
			}
			// String comparison: if either operand is a string, both must be strings.
			if isStringExpr(e.Left, scope, constScope, tables) || isStringExpr(e.Right, scope, constScope, tables) {
				if err := validateExpressionWithTables(e.Left, ParameterType{Kind: ParamTypeString}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("left side of comparison '%s': %w", e.Operator, err)
				}
				if err := validateExpressionWithTables(e.Right, ParameterType{Kind: ParamTypeString}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("right side of comparison '%s': %w", e.Operator, err)
				}
				return nil
			}
			// Boolean comparison: if either operand is a boolean, both must be booleans.
			if isBooleanExpr(e.Left, scope, constScope, tables) || isBooleanExpr(e.Right, scope, constScope, tables) {
				if err := validateExpressionWithTables(e.Left, ParameterType{Kind: ParamTypeBoolean}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("left side of comparison '%s': %w", e.Operator, err)
				}
				if err := validateExpressionWithTables(e.Right, ParameterType{Kind: ParamTypeBoolean}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("right side of comparison '%s': %w", e.Operator, err)
				}
				return nil
			}
			// Fallback: operands must be numbers for comparison.
			if err := validateExpressionWithTables(e.Left, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("left side of comparison '%s': %w", e.Operator, err)
			}
			if err := validateExpressionWithTables(e.Right, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("right side of comparison '%s': %w", e.Operator, err)
			}
			return nil
		case "<", "<=", ">", ">=":
			if expectedType.Kind != ParamTypeBoolean {
				return fmt.Errorf("comparison expression results in a boolean, but expected type is '%s'", expectedType)
			}
			// String comparison: if either operand is a string, both must be strings.
			if isStringExpr(e.Left, scope, constScope, tables) || isStringExpr(e.Right, scope, constScope, tables) {
				if err := validateExpressionWithTables(e.Left, ParameterType{Kind: ParamTypeString}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("left side of comparison '%s': %w", e.Operator, err)
				}
				if err := validateExpressionWithTables(e.Right, ParameterType{Kind: ParamTypeString}, paramName, scope, constScope, tables); err != nil {
					return fmt.Errorf("right side of comparison '%s': %w", e.Operator, err)
				}
				return nil
			}
			// Operands must be numbers for comparison.
			if err := validateExpressionWithTables(e.Left, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("left side of comparison '%s': %w", e.Operator, err)
			}
			if err := validateExpressionWithTables(e.Right, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("right side of comparison '%s': %w", e.Operator, err)
			}
			return nil
		case "&&", "||":
			if expectedType.Kind != ParamTypeBoolean {
				return fmt.Errorf("logical expression results in a boolean, but expected type is '%s'", expectedType)
			}
			// Operands must be booleans for logical operations.
			if err := validateExpressionWithTables(e.Left, ParameterType{Kind: ParamTypeBoolean}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("left side of logical operator '%s': %w", e.Operator, err)
			}
			if err := validateExpressionWithTables(e.Right, ParameterType{Kind: ParamTypeBoolean}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("right side of logical operator '%s': %w", e.Operator, err)
			}
			return nil
		default:
			return fmt.Errorf("unknown operator in binary expression: '%s'", e.Operator)
		}
	case *UnaryExpression:
		switch e.Operator {
		case "-":
			if expectedType.Kind != ParamTypeNumber {
				return fmt.Errorf("unary expression results in a number, but expected type is '%s'", expectedType)
			}
			if err := validateExpressionWithTables(e.Operand, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("operand of unary '-': %w", err)
			}
			return nil
		case "!":
			if expectedType.Kind != ParamTypeBoolean {
				return fmt.Errorf("unary expression results in a boolean, but expected type is '%s'", expectedType)
			}
			if err := validateExpressionWithTables(e.Operand, ParameterType{Kind: ParamTypeBoolean}, paramName, scope, constScope, tables); err != nil {
				return fmt.Errorf("operand of unary '!': %w", err)
			}
			return nil
		default:
			return fmt.Errorf("unknown unary operator: '%s'", e.Operator)
		}
	case *TernaryExpression:
		// Condition must evaluate to a boolean.
		if err := validateExpressionWithTables(e.Condition, ParameterType{Kind: ParamTypeBoolean}, paramName, scope, constScope, tables); err != nil {
			return fmt.Errorf("ternary condition must be a boolean expression: %w", err)
		}
		// The true and false branches must both conform to the expected type of the whole expression.
		if err := validateExpressionWithTables(e.TrueExpr, expectedType, paramName, scope, constScope, tables); err != nil {
			return fmt.Errorf("ternary 'true' branch error: %w", err)
		}
		if err := validateExpressionWithTables(e.FalseExpr, expectedType, paramName, scope, constScope, tables); err != nil {
			return fmt.Errorf("ternary 'false' branch error: %w", err)
		}
		return nil
	case *FunctionCallExpression:
		if e.Name == "table_cell" {
			return validateTableCellExpression(e, expectedType, paramName, scope, constScope, tables)
		}

		// 1. Check if function exists
		if _, ok := runtime.Builtins[e.Name]; !ok {
			return fmt.Errorf("unknown function: '%s'", e.Name)
		}

		if err := validateBuiltinFunctionArity(e.Name, len(e.Args)); err != nil {
			return err
		}
		if expectedType.Kind != ParamTypeNumber {
			return fmt.Errorf("function '%s' returns a number, but expected type is '%s'", e.Name, expectedType)
		}

		for i, arg := range e.Args {
			if err := validateExpressionWithTables(arg, ParameterType{Kind: ParamTypeNumber}, paramName, scope, constScope, tables); err != nil {
				if len(e.Args) == 1 {
					return fmt.Errorf("argument of '%s': %w", e.Name, err)
				}
				return fmt.Errorf("argument %d of '%s': %w", i+1, e.Name, err)
			}
		}

		if err := validateBuiltinFunctionStaticSemantics(e.Name, e.Args, constScope); err != nil {
			return err
		}

		return nil
	}
	return fmt.Errorf("unknown expression type: %T", expr)
}

func validateStringLiteralControlChars(s, paramName string) error {
	for i := 0; i < len(s); i++ {
		b := s[i]

		if b < 0x20 || b == 0x7F {
			return fmt.Errorf("string value for parameter '%s' contains invalid control character (0x%02X)", paramName, b)
		}
	}

	if isPathLikeParameterName(paramName) {
		if err := security.ValidateSafePath(s, ""); err != nil {
			return fmt.Errorf("string value for parameter '%s' is not a safe path: %w", paramName, err)
		}
	}

	return nil
}

func isPathLikeParameterName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "path") || strings.Contains(lower, "file") || strings.Contains(lower, "dir")
}

func resolveConstant(name string, constDecls map[string]*ConstDeclarationNode, resolved map[string]ConstantValue, visiting map[string]bool, paramNames map[string]struct{}) (ConstantValue, error) {
	if v, ok := resolved[name]; ok {
		return v, nil
	}
	if visiting[name] {
		return ConstantValue{}, fmt.Errorf("invalid constant expression: circular reference involving '%s'", name)
	}

	decl, ok := constDecls[name]
	if !ok {
		return ConstantValue{}, fmt.Errorf("invalid constant expression: undefined constant '%s'", name)
	}

	visiting[name] = true
	value, err := evaluateConstantExpression(decl.Value, constDecls, resolved, visiting, paramNames)
	delete(visiting, name)
	if err != nil {
		return ConstantValue{}, fmt.Errorf("invalid constant expression: %w", err)
	}

	resolved[name] = value
	return value, nil
}

func evaluateConstantExpression(expr ExpressionNode, constDecls map[string]*ConstDeclarationNode, resolved map[string]ConstantValue, visiting map[string]bool, paramNames map[string]struct{}) (ConstantValue, error) {
	switch e := expr.(type) {
	case *LiteralExpression:
		switch v := e.Value.(type) {
		case float64:
			return ConstantValue{Type: ParameterType{Kind: ParamTypeNumber}, Value: v}, nil
		case string:
			return ConstantValue{Type: ParameterType{Kind: ParamTypeString}, Value: v}, nil
		case bool:
			return ConstantValue{Type: ParameterType{Kind: ParamTypeBoolean}, Value: v}, nil
		default:
			return ConstantValue{}, fmt.Errorf("unsupported literal type %T", e.Value)
		}

	case *IdentifierExpression:
		if _, exists := paramNames[e.Name]; exists {
			return ConstantValue{}, fmt.Errorf("identifier '%s' is a parameter, constants cannot depend on runtime values", e.Name)
		}
		if _, exists := constDecls[e.Name]; exists {
			return resolveConstant(e.Name, constDecls, resolved, visiting, paramNames)
		}
		// Any unknown identifier in a constant expression is treated as an enum literal.
		return ConstantValue{Type: ParameterType{Kind: ParamTypeEnum}, Value: e.Name}, nil

	case *InterpolatedStringExpression:
		for _, seg := range e.Segments {
			if seg.ParamName != "" {
				return ConstantValue{}, fmt.Errorf("interpolation parameter '%s' is a parameter, constants cannot depend on runtime values", seg.ParamName)
			}
		}
		var b strings.Builder
		for _, seg := range e.Segments {
			b.WriteString(seg.Text)
		}
		return ConstantValue{Type: ParameterType{Kind: ParamTypeString}, Value: b.String()}, nil

	case *UnaryExpression:
		operand, err := evaluateConstantExpression(e.Operand, constDecls, resolved, visiting, paramNames)
		if err != nil {
			return ConstantValue{}, err
		}
		switch e.Operator {
		case "-":
			num, ok := operand.Value.(float64)
			if !ok || operand.Type.Kind != ParamTypeNumber {
				return ConstantValue{}, fmt.Errorf("unary operator '-' requires number operand")
			}
			return ConstantValue{Type: ParameterType{Kind: ParamTypeNumber}, Value: -num}, nil
		case "!":
			b, ok := operand.Value.(bool)
			if !ok || operand.Type.Kind != ParamTypeBoolean {
				return ConstantValue{}, fmt.Errorf("unary operator '!' requires boolean operand")
			}
			return ConstantValue{Type: ParameterType{Kind: ParamTypeBoolean}, Value: !b}, nil
		default:
			return ConstantValue{}, fmt.Errorf("unknown unary operator '%s'", e.Operator)
		}

	case *BinaryExpression:
		left, err := evaluateConstantExpression(e.Left, constDecls, resolved, visiting, paramNames)
		if err != nil {
			return ConstantValue{}, err
		}
		right, err := evaluateConstantExpression(e.Right, constDecls, resolved, visiting, paramNames)
		if err != nil {
			return ConstantValue{}, err
		}

		switch e.Operator {
		case "+":
			if left.Type.Kind == ParamTypeNumber && right.Type.Kind == ParamTypeNumber {
				leftNum := left.Value.(float64)
				rightNum := right.Value.(float64)
				return ConstantValue{Type: ParameterType{Kind: ParamTypeNumber}, Value: leftNum + rightNum}, nil
			}
			if left.Type.Kind == ParamTypeString && right.Type.Kind == ParamTypeString {
				leftStr := left.Value.(string)
				rightStr := right.Value.(string)
				return ConstantValue{Type: ParameterType{Kind: ParamTypeString}, Value: leftStr + rightStr}, nil
			}
			return ConstantValue{}, fmt.Errorf("operator '+' requires operands of the same type (number+number or string+string)")
		case "-", "*", "/":
			if left.Type.Kind != ParamTypeNumber || right.Type.Kind != ParamTypeNumber {
				return ConstantValue{}, fmt.Errorf("operator '%s' requires number operands", e.Operator)
			}
			leftNum := left.Value.(float64)
			rightNum := right.Value.(float64)
			switch e.Operator {
			case "-":
				return ConstantValue{Type: ParameterType{Kind: ParamTypeNumber}, Value: leftNum - rightNum}, nil
			case "*":
				return ConstantValue{Type: ParameterType{Kind: ParamTypeNumber}, Value: leftNum * rightNum}, nil
			case "/":
				if rightNum == 0 {
					return ConstantValue{}, fmt.Errorf("division by zero")
				}
				return ConstantValue{Type: ParameterType{Kind: ParamTypeNumber}, Value: leftNum / rightNum}, nil
			}

		case "&&", "||":
			if left.Type.Kind != ParamTypeBoolean || right.Type.Kind != ParamTypeBoolean {
				return ConstantValue{}, fmt.Errorf("operator '%s' requires boolean operands", e.Operator)
			}
			leftBool := left.Value.(bool)
			rightBool := right.Value.(bool)
			if e.Operator == "&&" {
				return ConstantValue{Type: ParameterType{Kind: ParamTypeBoolean}, Value: leftBool && rightBool}, nil
			}
			return ConstantValue{Type: ParameterType{Kind: ParamTypeBoolean}, Value: leftBool || rightBool}, nil

		case "==", "!=":
			if left.Type.Kind != right.Type.Kind {
				return ConstantValue{}, fmt.Errorf("operator '%s' requires operands of the same type", e.Operator)
			}
			var result bool
			switch left.Type.Kind {
			case ParamTypeNumber:
				result = left.Value.(float64) == right.Value.(float64)
			case ParamTypeString:
				result = left.Value.(string) == right.Value.(string)
			case ParamTypeBoolean:
				result = left.Value.(bool) == right.Value.(bool)
			case ParamTypeEnum:
				result = left.Value.(string) == right.Value.(string)
			default:
				return ConstantValue{}, fmt.Errorf("operator '%s' does not support type '%s'", e.Operator, left.Type)
			}
			if e.Operator == "!=" {
				result = !result
			}
			return ConstantValue{Type: ParameterType{Kind: ParamTypeBoolean}, Value: result}, nil

		case "<", "<=", ">", ">=":
			if left.Type.Kind != right.Type.Kind {
				return ConstantValue{}, fmt.Errorf("operator '%s' requires operands of the same type", e.Operator)
			}
			var result bool
			switch left.Type.Kind {
			case ParamTypeNumber:
				ln := left.Value.(float64)
				rn := right.Value.(float64)
				switch e.Operator {
				case "<":
					result = ln < rn
				case "<=":
					result = ln <= rn
				case ">":
					result = ln > rn
				case ">=":
					result = ln >= rn
				}
			case ParamTypeString:
				ls := left.Value.(string)
				rs := right.Value.(string)
				switch e.Operator {
				case "<":
					result = ls < rs
				case "<=":
					result = ls <= rs
				case ">":
					result = ls > rs
				case ">=":
					result = ls >= rs
				}
			default:
				return ConstantValue{}, fmt.Errorf("operator '%s' requires number or string operands", e.Operator)
			}
			return ConstantValue{Type: ParameterType{Kind: ParamTypeBoolean}, Value: result}, nil

		default:
			return ConstantValue{}, fmt.Errorf("unknown operator '%s'", e.Operator)
		}

	case *TernaryExpression:
		cond, err := evaluateConstantExpression(e.Condition, constDecls, resolved, visiting, paramNames)
		if err != nil {
			return ConstantValue{}, err
		}
		if cond.Type.Kind != ParamTypeBoolean {
			return ConstantValue{}, fmt.Errorf("ternary condition must be boolean")
		}
		trueVal, err := evaluateConstantExpression(e.TrueExpr, constDecls, resolved, visiting, paramNames)
		if err != nil {
			return ConstantValue{}, err
		}
		falseVal, err := evaluateConstantExpression(e.FalseExpr, constDecls, resolved, visiting, paramNames)
		if err != nil {
			return ConstantValue{}, err
		}
		if trueVal.Type.Kind != falseVal.Type.Kind {
			return ConstantValue{}, fmt.Errorf("ternary branches must return the same type")
		}
		if cond.Value.(bool) {
			return trueVal, nil
		}
		return falseVal, nil

	case *FunctionCallExpression:
		if _, ok := runtime.Builtins[e.Name]; !ok {
			return ConstantValue{}, fmt.Errorf("unknown function: '%s'", e.Name)
		}

		if err := validateBuiltinFunctionArity(e.Name, len(e.Args)); err != nil {
			return ConstantValue{}, err
		}

		args := make([]interface{}, 0, len(e.Args))
		for i, argExpr := range e.Args {
			arg, err := evaluateConstantExpression(argExpr, constDecls, resolved, visiting, paramNames)
			if err != nil {
				return ConstantValue{}, err
			}
			if arg.Type.Kind != ParamTypeNumber {
				return ConstantValue{}, fmt.Errorf("argument %d of '%s' must be number", i+1, e.Name)
			}
			args = append(args, arg.Value)
		}

		fn := runtime.Builtins[e.Name]
		v, err := fn(args)
		if err != nil {
			return ConstantValue{}, err
		}
		n, ok := v.(float64)
		if !ok {
			return ConstantValue{}, fmt.Errorf("function '%s' returned non-number type %T", e.Name, v)
		}
		return ConstantValue{Type: ParameterType{Kind: ParamTypeNumber}, Value: n}, nil
	}

	return ConstantValue{}, fmt.Errorf("unknown expression type: %T", expr)
}

// isStringExpr reports whether expr is a string literal or a reference to a string parameter/constant.
func isStringExpr(expr ExpressionNode, scope map[string]*ParameterNode, constScope map[string]ConstantValue, tables map[string]*table.Table) bool {
	switch e := expr.(type) {
	case *LiteralExpression:
		_, ok := e.Value.(string)
		return ok
	case *InterpolatedStringExpression:
		return true
	case *IdentifierExpression:
		if param, exists := scope[e.Name]; exists && param.Type.Kind == ParamTypeString {
			return true
		}
		if c, exists := constScope[e.Name]; exists && c.Type.Kind == ParamTypeString {
			return true
		}
	case *FunctionCallExpression:
		if kind, ok := inferTableCellResultKind(e, tables); ok && kind == ParamTypeString {
			return true
		}
	}
	return false
}

// isBooleanExpr reports whether expr is a boolean literal or a reference to a boolean parameter/constant.
func isBooleanExpr(expr ExpressionNode, scope map[string]*ParameterNode, constScope map[string]ConstantValue, tables map[string]*table.Table) bool {
	switch e := expr.(type) {
	case *LiteralExpression:
		_, ok := e.Value.(bool)
		return ok
	case *IdentifierExpression:
		if param, exists := scope[e.Name]; exists && param.Type.Kind == ParamTypeBoolean {
			return true
		}
		if c, exists := constScope[e.Name]; exists && c.Type.Kind == ParamTypeBoolean {
			return true
		}
	case *FunctionCallExpression:
		if kind, ok := inferTableCellResultKind(e, tables); ok && kind == ParamTypeBoolean {
			return true
		}
	}
	return false
}

func validateTableCellExpression(e *FunctionCallExpression, expectedType ParameterType, paramName string, scope map[string]*ParameterNode, constScope map[string]ConstantValue, tables map[string]*table.Table) error {
	if len(e.Args) != 3 {
		return fmt.Errorf("function '%s' expects 3 arguments, but got %d", e.Name, len(e.Args))
	}

	tableName, ok := stringLiteralValue(e.Args[0])
	if !ok {
		return fmt.Errorf("argument 1 of '%s' must be a string literal", e.Name)
	}
	columnName, ok := stringLiteralValue(e.Args[2])
	if !ok {
		return fmt.Errorf("argument 3 of '%s' must be a string literal", e.Name)
	}
	if err := validateExpressionWithTables(e.Args[1], ParameterType{Kind: ParamTypeString}, paramName, scope, constScope, tables); err != nil {
		return fmt.Errorf("argument 2 of '%s': %w", e.Name, err)
	}

	if tables == nil {
		return fmt.Errorf("function '%s' references unknown table '%s'", e.Name, tableName)
	}
	tbl, ok := tables[tableName]
	if !ok || tbl == nil {
		return fmt.Errorf("function '%s' references unknown table '%s'", e.Name, tableName)
	}
	if err := table.Validate(tbl); err != nil {
		return fmt.Errorf("function '%s' table '%s' is invalid: %w", e.Name, tableName, err)
	}

	column, ok := findTableColumn(tbl, columnName)
	if !ok {
		return fmt.Errorf("function '%s' references unknown column '%s' in table '%s'", e.Name, columnName, tableName)
	}

	actualKind, err := parameterTypeKindForColumn(column.Type)
	if err != nil {
		return fmt.Errorf("function '%s' column '%s' in table '%s': %w", e.Name, columnName, tableName, err)
	}
	if expectedType.Kind == ParamTypeAction && actualKind == ParamTypeString {
		if rowKey, staticallyKnown := stringLiteralValue(e.Args[1]); staticallyKnown {
			row, err := tbl.RowByKey(rowKey)
			if err != nil {
				return fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': %w", e.Name, tableName, rowKey, columnName, err)
			}
			value, exists := row.Values[columnName]
			if !exists {
				if column.Required {
					return fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': required column value is missing", e.Name, tableName, rowKey, columnName)
				}
				return fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': optional column value is missing", e.Name, tableName, rowKey, columnName)
			}
			if !isCanonicalActionValue(value.String) {
				return fmt.Errorf("function '%s' returns a string from table '%s' column '%s', but expected type is 'action': selected invalid action value '%s' from row %q; allowed values are %v", e.Name, tableName, columnName, value.String, rowKey, canonicalActionValues)
			}
		}
		return nil
	}
	if expectedType.Kind != actualKind {
		return fmt.Errorf("function '%s' returns a %s from table '%s' column '%s', but expected type is '%s'", e.Name, actualKind, tableName, columnName, expectedType)
	}

	return nil
}

func inferTableCellResultKind(e *FunctionCallExpression, tables map[string]*table.Table) (ParameterTypeKind, bool) {
	if e.Name != "table_cell" || len(e.Args) != 3 {
		return "", false
	}
	tableName, ok := stringLiteralValue(e.Args[0])
	if !ok || tables == nil {
		return "", false
	}
	columnName, ok := stringLiteralValue(e.Args[2])
	if !ok {
		return "", false
	}
	tbl, ok := tables[tableName]
	if !ok || tbl == nil {
		return "", false
	}
	column, ok := findTableColumn(tbl, columnName)
	if !ok {
		return "", false
	}
	kind, err := parameterTypeKindForColumn(column.Type)
	if err != nil {
		return "", false
	}
	return kind, true
}

func stringLiteralValue(expr ExpressionNode) (string, bool) {
	lit, ok := expr.(*LiteralExpression)
	if !ok {
		return "", false
	}
	value, ok := lit.Value.(string)
	return value, ok
}

func firstTableCellArgString(args []ExpressionNode) string {
	if len(args) == 0 {
		return ""
	}
	if value, ok := stringLiteralValue(args[0]); ok {
		return value
	}
	return ""
}

func findTableColumn(tbl *table.Table, name string) (table.Column, bool) {
	for _, column := range tbl.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return table.Column{}, false
}

func parameterTypeKindForColumn(columnType table.ColumnType) (ParameterTypeKind, error) {
	switch columnType {
	case table.ColumnTypeString:
		return ParamTypeString, nil
	case table.ColumnTypeNumber:
		return ParamTypeNumber, nil
	case table.ColumnTypeBoolean:
		return ParamTypeBoolean, nil
	default:
		return "", fmt.Errorf("unsupported table column type '%s'", columnType)
	}
}

// inferEnumTypeFromExpr returns the enum ParameterType of an expression if it is
// an IdentifierExpression that references an enum parameter or constant in scope.
func inferEnumTypeFromExpr(expr ExpressionNode, scope map[string]*ParameterNode, constScope map[string]ConstantValue) (ParameterType, bool) {
	ident, ok := expr.(*IdentifierExpression)
	if !ok {
		return ParameterType{}, false
	}
	if param, exists := scope[ident.Name]; exists && param.Type.Kind == ParamTypeEnum && len(param.Type.EnumValues) > 0 {
		return param.Type, true
	}
	if c, exists := constScope[ident.Name]; exists && c.Type.Kind == ParamTypeEnum && len(c.Type.EnumValues) > 0 {
		return c.Type, true
	}
	return ParameterType{}, false
}

func validateBuiltinFunctionArity(name string, argCount int) error {
	var expected int
	switch name {
	case "min", "max", "round_up", "round_down":
		expected = 2
	case "abs", "round", "floor", "ceil":
		expected = 1
	case "clamp":
		expected = 3
	default:
		return fmt.Errorf("validation not implemented for function '%s'", name)
	}
	if argCount != expected {
		return fmt.Errorf("function '%s' expects %d arguments, but got %d", name, expected, argCount)
	}
	return nil
}

func validateBuiltinFunctionStaticSemantics(name string, args []ExpressionNode, constScope map[string]ConstantValue) error {
	switch name {
	case "clamp":
		lo, hasLo := staticNumericValue(args[1], constScope)
		hi, hasHi := staticNumericValue(args[2], constScope)
		if hasLo && hasHi && lo > hi {
			return fmt.Errorf("clamp requires lo <= hi, but got lo=%v and hi=%v", lo, hi)
		}
	case "round_up", "round_down":
		step, hasStep := staticNumericValue(args[1], constScope)
		if hasStep && step <= 0 {
			return fmt.Errorf("%s requires step > 0, but got %v", name, step)
		}
	}
	return nil
}

func staticNumericValue(expr ExpressionNode, constScope map[string]ConstantValue) (float64, bool) {
	switch e := expr.(type) {
	case *LiteralExpression:
		n, ok := e.Value.(float64)
		return n, ok
	case *IdentifierExpression:
		if c, ok := constScope[e.Name]; ok && c.Type.Kind == ParamTypeNumber {
			n, ok := c.Value.(float64)
			return n, ok
		}
	case *UnaryExpression:
		if e.Operator != "-" {
			return 0, false
		}
		n, ok := staticNumericValue(e.Operand, constScope)
		if !ok {
			return 0, false
		}
		return -n, true
	}
	return 0, false
}

func enumTypesEqual(a, b ParameterType) bool {
	if a.Kind != ParamTypeEnum || b.Kind != ParamTypeEnum {
		return false
	}
	if len(a.EnumValues) != len(b.EnumValues) {
		return false
	}
	for i := range a.EnumValues {
		if a.EnumValues[i] != b.EnumValues[i] {
			return false
		}
	}
	return true
}
