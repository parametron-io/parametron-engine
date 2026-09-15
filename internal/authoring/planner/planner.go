package planner

import (
	"errors"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/runtime"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
	"parametron/internal/engine/table"
)

// StepType defines the type of operation to be performed.
type StepType string

const (
	StepWriteCSV            StepType = "WriteCSV"
	StepWriteExportManifest StepType = "WriteExportManifest"
	StepRunCADRuntime       StepType = "RunCADRuntime"
)

const (
	FreeCADRuntimeExportManifestFilename = "prm.export-manifest.json"

	// FreeCADRuntimeExportManifestFilename is the canonical FreeCAD runtime-facing filename.
	// ExportManifestFilename is the active Engine runtime-facing manifest filename.
	ExportManifestFilename                      = FreeCADRuntimeExportManifestFilename
	ExportManifestSchemaVersion                 = "1.0"
	FreeCADRuntimeMutationManifestSchemaVersion = "2.0"
	FreeCADRuntimeResultFilename                = "prm.result.json"
	FreeCADRuntimeOutputDirectory               = "outputs"
	freeCADExportObjectName                     = "Body"
	defaultOutputFormat                         = artifact.ExportOutputTypeSTEP
	freeCADRuntimeAdapterID                     = "freecad"
)

// StepPayload defines the interface for step configuration payloads.
type StepPayload interface {
	isStepPayload()
}

// WriteCSVPayload defines the payload for the WriteCSV step.
type WriteCSVPayload struct {
	ProductKey string        `json:"productKey,omitempty"`
	Filename   string        `json:"filename"`
	Headers    []string      `json:"headers"`
	Values     []interface{} `json:"values"`
}

func (WriteCSVPayload) isStepPayload() {}

// RunCADRuntimePayload defines the payload for the RunCADRuntime step.
// It is a logical CAD-runtime execution intent only: adapter identity plus
// already-planned manifest and required raw result filenames. It does not
// select executables, paths, environments, or verification policy.
type RunCADRuntimePayload struct {
	ProductKey       string `json:"productKey,omitempty"`
	Adapter          string `json:"adapter"`
	ManifestFilename string `json:"manifestFilename"`
	ResultFilename   string `json:"resultFilename"`
}

func (RunCADRuntimePayload) isStepPayload() {}

type ExportManifestProjectionMode string

const (
	ExportManifestProjectionModeFreeCADRuntimeNative ExportManifestProjectionMode = "freecad-runtime-native"
)

// WriteExportManifestPayload defines the payload for the WriteExportManifest step.
type WriteExportManifestPayload struct {
	ProductKey             string                              `json:"-"`
	ManifestFilename       string                              `json:"-"`
	ManifestProjectionMode ExportManifestProjectionMode        `json:"-"`
	SchemaVersion          string                              `json:"schemaVersion"`
	PlanHash               string                              `json:"planHash"`
	Adapter                string                              `json:"adapter,omitempty"`
	Product                ExportManifestProduct               `json:"product"`
	SourceDocument         string                              `json:"sourceDocument,omitempty"`
	Inputs                 ExportManifestInputs                `json:"inputs"`
	Values                 map[string]interface{}              `json:"values"`
	ParameterAssignments   []ExportManifestParameterAssignment `json:"parameterAssignments"`
	AssemblyMutations      *ExportManifestMutationCollection   `json:"assemblyMutations,omitempty"`
	PartMutations          *ExportManifestMutationCollection   `json:"partMutations,omitempty"`
	Outputs                []ExportManifestOutput              `json:"outputs"`
	Verification           VerificationManifestIntent          `json:"-"`
}

type ExportManifestProduct struct {
	ID string `json:"id"`
}

type ExportManifestInputs struct {
	SourceModel string `json:"sourceModel,omitempty"`
}

type ExportManifestOutput struct {
	Type     string   `json:"type"`
	Filename string   `json:"filename"`
	Object   string   `json:"object,omitempty"`
	Target   string   `json:"target,omitempty"`
	Rollup   string   `json:"rollup,omitempty"`
	Columns  []string `json:"columns,omitempty"`
}

type ExportManifestParameterAssignment struct {
	Name   string  `json:"name"`
	Target string  `json:"target,omitempty"`
	Value  float64 `json:"value"`
	Type   string  `json:"type"`
	Unit   string  `json:"unit"`
}

type VerificationManifestIntent struct {
	ExpectedParameters        []VerificationExpectedParameter
	ObservationParameterLinks []VerificationObservationParameterLink
}

type VerificationExpectedParameter struct {
	ID    string
	Name  string
	Value float64
	Type  string
	Unit  string
}

type VerificationObservationParameterLink struct {
	ID        string
	Name      string
	GroupName string
}

type ExportManifestMutationCollection struct {
	Parameters  []ExportManifestParameterMutation   `json:"parameters,omitempty"`
	Properties  []ExportManifestPropertyMutation    `json:"properties,omitempty"`
	Suppression []ExportManifestSuppressionMutation `json:"suppression,omitempty"`
	Visibility  []ExportManifestVisibilityMutation  `json:"visibility,omitempty"`
	Deletion    []ExportManifestDeletionMutation    `json:"deletion,omitempty"`
}

type ExportManifestParameterMutation struct {
	Object     string `json:"object"`
	Property   string `json:"property"`
	ValueParam string `json:"valueParam"`
	Type       string `json:"type"`
	Unit       string `json:"unit"`
}

type ExportManifestPropertyMutation struct {
	Object   string      `json:"object"`
	Property string      `json:"property"`
	Value    interface{} `json:"value"`
}

type ExportManifestSuppressionMutation struct {
	Object     string `json:"object"`
	Suppressed bool   `json:"suppressed"`
}

type ExportManifestVisibilityMutation struct {
	Object  string `json:"object"`
	Visible bool   `json:"visible"`
}

type ExportManifestDeletionMutation struct {
	Object string `json:"object"`
}

func (WriteExportManifestPayload) isStepPayload() {}

// Step represents a single execution step with a type and configuration payload.
type Step struct {
	Type    StepType
	Payload StepPayload
}

// ExecutionPlan contains the sequence of steps to be executed.
type ExecutionPlan struct {
	Steps []Step
}

// CreatePlan generates an execution plan from the provided AST.
// It always creates CSV + exporter manifest steps; CAD runner steps are adapter-gated.
func CreatePlan(ast *dsl.AST, overrides map[string]string) (*ExecutionPlan, error) {
	return createPlan(ast, overrides, nil, nil, nil, false)
}

// CreatePlanWithTables generates an execution plan using planner-visible in-memory tables.
func CreatePlanWithTables(ast *dsl.AST, overrides map[string]string, tables map[string]*table.Table) (*ExecutionPlan, error) {
	if err := dsl.ValidateWithTables(ast, tables); err != nil {
		return nil, err
	}
	return createPlan(ast, overrides, tables, nil, nil, false)
}

// CreatePlanWithTablesAndSemanticModel generates an execution plan using
// planner-visible tables and a canonical semantic model for capture-backed
// manifest intent.
func CreatePlanWithTablesAndSemanticModel(ast *dsl.AST, overrides map[string]string, tables map[string]*table.Table, model *semantic.Model, contract *semanticmap.SemanticMap) (*ExecutionPlan, error) {
	if err := dsl.ValidateWithTables(ast, tables); err != nil {
		return nil, err
	}
	if model == nil {
		return createPlan(ast, overrides, tables, nil, nil, false)
	}
	canonical, err := semantic.Canonicalize(model)
	if err != nil {
		return nil, err
	}
	return createPlan(ast, overrides, tables, canonical, contract, true)
}

func createPlan(ast *dsl.AST, overrides map[string]string, tables map[string]*table.Table, semanticModel *semantic.Model, semanticMapContract *semanticmap.SemanticMap, captureBacked bool) (*ExecutionPlan, error) {
	if ast == nil {
		return nil, fmt.Errorf("AST is nil")
	}
	if len(ast.Constants) > 0 && len(ast.ResolvedConstants) == 0 {
		return nil, fmt.Errorf("constants must be validated before planning")
	}

	// 1. Collect product-local binding names to validate overrides against.
	definedParams := make(map[string]struct{})
	definedLets := make(map[string]struct{})
	for _, product := range ast.Products {
		for _, let := range product.Lets {
			definedLets[let.Name] = struct{}{}
		}
		for _, param := range product.Parameters {
			definedParams[param.Name] = struct{}{}
		}
	}
	definedConstants := make(map[string]struct{})
	for _, constDecl := range ast.Constants {
		definedConstants[constDecl.Name] = struct{}{}
	}

	// 2. Ensure all provided overrides exist in the DSL.
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

	plan := &ExecutionPlan{
		Steps: []Step{},
	}

	activeProfileName := ""
	if ast.ActiveProfileName != nil {
		activeProfileName = *ast.ActiveProfileName
	}
	filePattern, hasFilePattern, err := resolveActiveFilePattern(ast)
	if err != nil {
		return nil, err
	}

	type productDraft struct {
		name                  string
		headers               []string
		values                []interface{}
		exportedParams        map[string]any
		parameters            []*dsl.ParameterNode
		semanticIntent        *semantic.ProductIntent
		targetActionMutations []semantic.MutationIntent
		contract              exportManifestContract
	}
	drafts := make([]productDraft, 0, len(ast.Products))

	for _, product := range ast.Products {
		productSemanticIntent, err := resolveSemanticProductIntent(semanticModel, product, captureBacked)
		if err != nil {
			return nil, err
		}

		manifestContract, err := resolveProductExportManifestContract(ast, product, productSemanticIntent)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve export manifest contract for product '%s': %w", product.Name, err)
		}

		resolvedValues := make(map[string]interface{})
		productBindingNames := make(map[string]struct{}, len(product.Lets)+len(product.Parameters))
		productBindingScope, err := dsl.BuildProductBindingScopeForPlanning(product, ast.ResolvedConstants, tables)
		if err != nil {
			return nil, fmt.Errorf("failed to build binding scope for product '%s': %w", product.Name, err)
		}
		for _, let := range product.Lets {
			productBindingNames[let.Name] = struct{}{}
		}
		for _, param := range product.Parameters {
			productBindingNames[param.Name] = struct{}{}
		}

		// 1. Apply overrides for parameters belonging to this product.
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

		// 2. Iteratively evaluate all product-local bindings in one graph.
		type pendingBinding struct {
			name         string
			expr         dsl.ExpressionNode
			expectedType *dsl.ParameterType
			kind         string
		}
		unresolvedBindings := make([]pendingBinding, 0, len(product.Lets)+len(product.Parameters))
		for _, let := range product.Lets {
			letBinding := productBindingScope[let.Name]
			unresolvedBindings = append(unresolvedBindings, pendingBinding{
				name:         let.Name,
				expr:         let.Value,
				expectedType: &letBinding.Type,
				kind:         "let",
			})
		}
		for _, p := range product.Parameters {
			if _, isResolved := resolvedValues[p.Name]; !isResolved {
				paramType := p.Type
				unresolvedBindings = append(unresolvedBindings, pendingBinding{
					name:         p.Name,
					expr:         p.DefaultValue,
					expectedType: &paramType,
					kind:         "parameter",
				})
			}
		}

		progress := true
		for len(unresolvedBindings) > 0 && progress {
			progress = false
			remainingBindings := make([]pendingBinding, 0, len(unresolvedBindings))
			for _, binding := range unresolvedBindings {
				val, err := evaluateExpressionWithContext(binding.expr, resolvedValues, productBindingNames, productBindingScope, ast.ResolvedConstants, binding.expectedType, plannerEvalContext{
					tables: tables,
				})
				if err == nil {
					resolvedValues[binding.name] = val
					progress = true
				} else if !errors.Is(err, errUnresolvedIdentifier) {
					// A non-recoverable evaluation error occurred.
					return nil, fmt.Errorf("error evaluating %s '%s' in product '%s': %w", binding.kind, binding.name, product.Name, err)
				} else {
					// Not all dependencies are met yet, try again in the next iteration.
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

		evaluatedActions, err := evaluateProductTargetActions(product, resolvedValues, productBindingNames, productBindingScope, ast.ResolvedConstants, tables)
		if err != nil {
			return nil, fmt.Errorf("error evaluating target actions in product '%s': %w", product.Name, err)
		}
		var targetActionMutations []semantic.MutationIntent
		if semanticModel != nil {
			resolvedActions, err := resolveEvaluatedTargetActions(semanticModel, evaluatedActions)
			if err != nil {
				return nil, fmt.Errorf("error resolving target actions in product '%s': %w", product.Name, err)
			}
			if err := validateResolvedTargetActionCapabilities(resolvedActions); err != nil {
				return nil, fmt.Errorf("error validating target action capabilities in product '%s': %w", product.Name, err)
			}
			targetActionMutations, err = lowerResolvedTargetActions(resolvedActions)
			if err != nil {
				return nil, fmt.Errorf("error lowering target action mutations in product '%s': %w", product.Name, err)
			}
		}

		// 3. Now that all values are resolved, build the CSV step payload.
		headers, values, exportedParams, err := buildExportedParameterValues(product.Parameters, resolvedValues)
		if err != nil {
			return nil, fmt.Errorf("failed to build exported parameter values for product '%s': %w", product.Name, err)
		}

		drafts = append(drafts, productDraft{
			name:                  product.Name,
			headers:               headers,
			values:                values,
			exportedParams:        exportedParams,
			parameters:            product.Parameters,
			semanticIntent:        productSemanticIntent,
			targetActionMutations: targetActionMutations,
			contract:              manifestContract,
		})
	}

	planHashForNaming := ""
	resolvedConstsForNaming := make(map[string]ResolvedConst, len(ast.ResolvedConstants))
	for name, constVal := range ast.ResolvedConstants {
		resolvedConstsForNaming[name] = ResolvedConst{
			TypeName: string(constVal.Type.Kind),
			Value:    constVal.Value,
		}
	}
	if hasFilePattern {
		draftPlan := &ExecutionPlan{Steps: make([]Step, 0, len(drafts)*3)}
		for _, d := range drafts {
			defaultCSVName := fmt.Sprintf("%s.csv", d.name)
			includeObject := shouldRunCADAdapter(d.contract.Adapter)
			manifestIntent, err := buildExportManifestIntent(d.name, d.contract.DefaultOutputFormats, includeObject, d.parameters, d.exportedParams, captureBacked, semanticModel, d.semanticIntent, d.targetActionMutations, semanticMapContract)
			if err != nil {
				return nil, fmt.Errorf("failed to build default export manifest intent for product '%s': %w", d.name, err)
			}
			if includeObject {
				if err := normalizeFreeCADRuntimeOutputsForRequest(manifestIntent.Outputs, d.contract.DefaultOutputFormats); err != nil {
					return nil, fmt.Errorf("failed to build default export manifest intent for product '%s': %w", d.name, err)
				}
			}
			projectionMode := projectionModeForAdapter(d.contract.Adapter)
			draftPlan.Steps = append(draftPlan.Steps, Step{
				Type: StepWriteCSV,
				Payload: WriteCSVPayload{
					ProductKey: d.name,
					Filename:   defaultCSVName,
					Headers:    d.headers,
					Values:     d.values,
				},
			})
			draftPlan.Steps = append(draftPlan.Steps, Step{
				Type: StepWriteExportManifest,
				Payload: WriteExportManifestPayload{
					ProductKey:             d.name,
					ManifestFilename:       ExportManifestFilename,
					ManifestProjectionMode: projectionMode,
					SchemaVersion:          exportManifestSchemaVersionForProjection(projectionMode, manifestIntent.AssemblyMutations, manifestIntent.PartMutations),
					Adapter:                canonicalAlignedAdapter(d.contract.Adapter),
					Product: ExportManifestProduct{
						ID: d.name,
					},
					Inputs: ExportManifestInputs{
						SourceModel: d.contract.SourceModel,
					},
					SourceDocument:       alignedSourceDocument(d.contract),
					Values:               copyResolvedValues(d.exportedParams),
					ParameterAssignments: manifestIntent.ParameterAssignments,
					AssemblyMutations:    manifestIntent.AssemblyMutations,
					PartMutations:        manifestIntent.PartMutations,
					Outputs:              manifestIntent.Outputs,
					Verification:         manifestIntent.Verification,
				},
			})
			if includeObject {
				draftPlan.Steps = append(draftPlan.Steps, Step{
					Type: StepRunCADRuntime,
					Payload: RunCADRuntimePayload{
						ProductKey:       d.name,
						Adapter:          freeCADRuntimeAdapterID,
						ManifestFilename: ExportManifestFilename,
						ResultFilename:   FreeCADRuntimeResultFilename,
					},
				})
			}
		}

		seedHash, err := ComputePlanHash(draftPlan, ast)
		if err != nil {
			return nil, fmt.Errorf("failed to compute plan hash for file_pattern interpolation: %w", err)
		}
		planHashForNaming = seedHash
	}

	for _, d := range drafts {
		includeObject := shouldRunCADAdapter(d.contract.Adapter)
		baseName := d.name
		if hasFilePattern {
			resolvedName, err := ResolveFilePattern(filePattern, NamingContext{
				ProductName:    d.name,
				ProfileName:    activeProfileName,
				PlanHash:       planHashForNaming,
				ResolvedParams: d.exportedParams,
				ResolvedConsts: resolvedConstsForNaming,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to resolve file_pattern for product '%s': %w", d.name, err)
			}
			normalizedName := normalizePatternBaseName(resolvedName)
			if err := ValidateFileBaseName(normalizedName); err != nil {
				return nil, fmt.Errorf("failed to validate file_pattern output for product '%s': %w", d.name, err)
			}
			baseName = normalizedName
		}

		csvFileName := fmt.Sprintf("%s.csv", baseName)
		manifestIntent, err := buildExportManifestIntent(baseName, d.contract.DefaultOutputFormats, includeObject, d.parameters, d.exportedParams, captureBacked, semanticModel, d.semanticIntent, d.targetActionMutations, semanticMapContract)
		if err != nil {
			return nil, fmt.Errorf("failed to build export manifest intent for product '%s': %w", d.name, err)
		}
		if includeObject {
			if err := normalizeFreeCADRuntimeOutputsForRequest(manifestIntent.Outputs, d.contract.DefaultOutputFormats); err != nil {
				return nil, fmt.Errorf("failed to build export manifest intent for product '%s': %w", d.name, err)
			}
		}

		// Step 1: Write CSV
		projectionMode := projectionModeForAdapter(d.contract.Adapter)
		plan.Steps = append(plan.Steps, Step{
			Type: StepWriteCSV,
			Payload: WriteCSVPayload{
				ProductKey: d.name,
				Filename:   csvFileName,
				Headers:    d.headers,
				Values:     d.values,
			},
		})

		// Step 2: Write exporter manifest.
		plan.Steps = append(plan.Steps, Step{
			Type: StepWriteExportManifest,
			Payload: WriteExportManifestPayload{
				ProductKey:             d.name,
				ManifestFilename:       ExportManifestFilename,
				ManifestProjectionMode: projectionMode,
				SchemaVersion:          exportManifestSchemaVersionForProjection(projectionMode, manifestIntent.AssemblyMutations, manifestIntent.PartMutations),
				Adapter:                canonicalAlignedAdapter(d.contract.Adapter),
				Product: ExportManifestProduct{
					ID: d.name,
				},
				Inputs: ExportManifestInputs{
					SourceModel: d.contract.SourceModel,
				},
				SourceDocument:       alignedSourceDocument(d.contract),
				Values:               copyResolvedValues(d.exportedParams),
				ParameterAssignments: manifestIntent.ParameterAssignments,
				AssemblyMutations:    manifestIntent.AssemblyMutations,
				PartMutations:        manifestIntent.PartMutations,
				Outputs:              manifestIntent.Outputs,
				Verification:         manifestIntent.Verification,
			},
		})

		// Step 3: Run CAD adapter worker (only when adapter requires it).
		if includeObject {
			plan.Steps = append(plan.Steps, Step{
				Type: StepRunCADRuntime,
				Payload: RunCADRuntimePayload{
					ProductKey:       d.name,
					Adapter:          freeCADRuntimeAdapterID,
					ManifestFilename: ExportManifestFilename,
					ResultFilename:   FreeCADRuntimeResultFilename,
				},
			})
		}
	}

	if err := validateUniqueProductFilenames(plan); err != nil {
		return nil, err
	}

	// Keep manifest planHash profile-agnostic so identical step graphs yield the same value.
	planHash, err := ComputePlanHash(plan, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to compute plan hash: %w", err)
	}
	for i := range plan.Steps {
		if plan.Steps[i].Type != StepWriteExportManifest {
			continue
		}
		payload, ok := plan.Steps[i].Payload.(WriteExportManifestPayload)
		if !ok {
			continue
		}
		payload.PlanHash = planHash
		plan.Steps[i].Payload = payload
	}

	return plan, nil
}

func validateUniqueProductFilenames(plan *ExecutionPlan) error {
	seen := make(map[string]string)
	for _, step := range plan.Steps {
		if step.Type != StepWriteCSV {
			continue
		}
		payload, ok := step.Payload.(WriteCSVPayload)
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

// convertOverrideValue parses a string override value into the correct DSL type.
func convertOverrideValue(value string, paramType dsl.ParameterType) (interface{}, error) {
	value = normalizeOverrideValue(value)

	switch paramType.Kind {
	case dsl.ParamTypeNumber:
		return strconv.ParseFloat(value, 64)
	case dsl.ParamTypeString:
		// The value is already a string, no conversion needed.
		return value, nil
	case dsl.ParamTypeBoolean:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("invalid boolean value '%s'", value)
		}
		return b, nil
	case dsl.ParamTypeEnum:
		for _, allowedValue := range paramType.EnumValues {
			if value == allowedValue {
				return value, nil
			}
		}
		return nil, fmt.Errorf("invalid enum override '%s', allowed values are %v", value, paramType.EnumValues)
	default:
		return nil, fmt.Errorf("unsupported parameter type for override conversion: %s", paramType)
	}
}

func normalizeOverrideValue(raw string) string {
	normalized := strings.TrimSpace(raw)
	if len(normalized) >= 2 && strings.HasPrefix(normalized, "\"") && strings.HasSuffix(normalized, "\"") {
		return normalized[1 : len(normalized)-1]
	}
	return normalized
}

type exportManifestContract struct {
	Adapter              string
	SourceModel          string
	DefaultOutputFormats []string
}

type semanticManifestProjectionRequest struct {
	BaseName              string
	CaptureBacked         bool
	Model                 *semantic.Model
	Intent                *semantic.ProductIntent
	TargetActionMutations []semantic.MutationIntent
	ResolvedValues        map[string]any
	Contract              *semanticmap.SemanticMap
}

type semanticManifestProjectionResult struct {
	ParameterAssignments            []ExportManifestParameterAssignment
	Outputs                         []ExportManifestOutput
	AssemblyMutations               *ExportManifestMutationCollection
	PartMutations                   *ExportManifestMutationCollection
	TargetActionRouting             *semanticmap.TargetMutationRouting
	TargetActionMutationCollections *semanticmap.ProjectedTargetMutationRouting
	Verification                    VerificationManifestIntent
}

func resolveProductExportManifestContract(ast *dsl.AST, product *dsl.ProductNode, intent *semantic.ProductIntent) (exportManifestContract, error) {
	contract := exportManifestContract{
		DefaultOutputFormats: []string{defaultOutputFormat},
	}
	if product == nil {
		return contract, nil
	}

	adapter := ""
	outputs := []string(nil)
	outputsPresent := false
	if intent != nil {
		adapter = strings.TrimSpace(intent.Adapter)
		if len(intent.RequestedOutputFormats) > 0 {
			outputs = append([]string(nil), intent.RequestedOutputFormats...)
			outputsPresent = true
		}
	} else {
		var err error
		adapter, err = resolveOptionalExecutionString(product.Adapter, ast)
		if err != nil {
			return exportManifestContract{}, err
		}
		outputs, outputsPresent, err = resolveExecutionOutputFormats(product.Outputs, ast)
		if err != nil {
			return exportManifestContract{}, err
		}
	}
	if outputsPresent {
		contract.DefaultOutputFormats = outputs
	}
	if strings.EqualFold(adapter, "none") {
		contract.Adapter = "none"
		contract.DefaultOutputFormats = []string{}
		return contract, nil
	}

	contract.Adapter = adapter
	if !shouldRunCADAdapter(adapter) {
		return contract, nil
	}
	contract.Adapter = freeCADRuntimeAdapterID

	sourceModel, err := resolveOptionalExecutionString(product.SourceModel, ast)
	if err != nil {
		return exportManifestContract{}, err
	}
	if sourceModel == "" {
		return exportManifestContract{}, fmt.Errorf(`product field %q is required when adapter is %q`, "source_model", "freecad")
	}

	baseDir := "."
	if ast != nil && ast.SourcePath != "" {
		baseDir = filepath.Dir(ast.SourcePath)
	}
	resolvedSource := sourceModel
	if !filepath.IsAbs(resolvedSource) {
		resolvedSource = filepath.Join(baseDir, resolvedSource)
	}
	absSource, err := filepath.Abs(resolvedSource)
	if err != nil {
		return exportManifestContract{}, fmt.Errorf("failed to resolve absolute source_model path %q: %w", sourceModel, err)
	}
	contract.SourceModel = absSource
	return contract, nil
}

func resolveOptionalExecutionString(expr dsl.ExpressionNode, ast *dsl.AST) (string, error) {
	if expr == nil {
		return "", nil
	}
	value, err := resolveProfileSettingValue(expr, resolvedConstants(ast))
	if err != nil {
		return "", err
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("execution declaration must resolve to string, got %T", value)
	}
	return strings.TrimSpace(s), nil
}

func resolveExecutionOutputFormats(expr dsl.ExpressionNode, ast *dsl.AST) ([]string, bool, error) {
	if expr == nil {
		return nil, false, nil
	}

	value, err := resolveProfileSettingValue(expr, resolvedConstants(ast))
	if err != nil {
		return nil, true, err
	}
	items, ok := value.([]interface{})
	if !ok {
		return nil, true, fmt.Errorf("product field %q must be an array of strings, got %T", "outputs", value)
	}
	formats := make([]string, 0, len(items))
	for idx, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, true, fmt.Errorf("product field %q item %d must be a string, got %T", "outputs", idx, item)
		}
		format := strings.ToLower(strings.TrimSpace(s))
		if format == "" {
			return nil, true, fmt.Errorf("product field %q item %d must not be empty", "outputs", idx)
		}
		if format != "none" && !artifact.IsSupportedExportOutputType(format) {
			return nil, true, fmt.Errorf("product field %q item %d has unsupported format %q", "outputs", idx, s)
		}
		formats = append(formats, format)
	}
	return formats, true, nil
}

func resolvedConstants(ast *dsl.AST) map[string]dsl.ConstantValue {
	if ast == nil {
		return nil
	}
	return ast.ResolvedConstants
}

func shouldRunCADAdapter(adapter string) bool {
	normalized := strings.ToLower(strings.TrimSpace(adapter))
	return normalized == "freecad"
}

func canonicalAlignedAdapter(adapter string) string {
	if shouldRunCADAdapter(adapter) {
		return freeCADRuntimeAdapterID
	}
	return adapter
}

func projectionModeForAdapter(adapter string) ExportManifestProjectionMode {
	if shouldRunCADAdapter(adapter) {
		return ExportManifestProjectionModeFreeCADRuntimeNative
	}
	return ""
}

func alignedSourceDocument(contract exportManifestContract) string {
	if !shouldRunCADAdapter(contract.Adapter) || strings.TrimSpace(contract.SourceModel) == "" {
		return ""
	}
	return path.Join("source", filepath.Base(contract.SourceModel))
}

func normalizeFreeCADRuntimeOutputs(outputs []ExportManifestOutput) error {
	if len(outputs) == 0 {
		return fmt.Errorf("FreeCAD runtime outputs must contain at least one item")
	}
	seen := make(map[string]struct{}, len(outputs))
	for i := range outputs {
		name := strings.TrimSpace(strings.ReplaceAll(outputs[i].Filename, "\\", "/"))
		if name == "" || path.IsAbs(name) {
			return fmt.Errorf("FreeCAD runtime output %d has invalid filename %q", i, outputs[i].Filename)
		}
		name = strings.TrimPrefix(name, FreeCADRuntimeOutputDirectory+"/")
		cleaned := path.Clean(name)
		if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return fmt.Errorf("FreeCAD runtime output %d has invalid filename %q", i, outputs[i].Filename)
		}
		final := path.Join(FreeCADRuntimeOutputDirectory, cleaned)
		if _, dup := seen[final]; dup {
			return fmt.Errorf("FreeCAD runtime output %d filename %q collides with another declared output", i, final)
		}
		seen[final] = struct{}{}
		outputs[i].Filename = final
	}
	return nil
}

func normalizeFreeCADRuntimeOutputsForRequest(outputs []ExportManifestOutput, requestedFormats []string) error {
	if len(outputs) == 0 && isNativeOnlyOutputRequest(requestedFormats) {
		return nil
	}
	return normalizeFreeCADRuntimeOutputs(outputs)
}

func isNativeOnlyOutputRequest(formats []string) bool {
	return len(formats) == 1 && strings.EqualFold(strings.TrimSpace(formats[0]), "none")
}

func resolveSemanticProductIntent(model *semantic.Model, product *dsl.ProductNode, captureBacked bool) (*semantic.ProductIntent, error) {
	if model == nil || product == nil {
		return nil, nil
	}

	for _, intent := range model.CanonicalProductIntents() {
		if intent.Name != product.Name {
			continue
		}
		if !shouldRunCADAdapter(intent.Adapter) {
			return nil, nil
		}
		intentCopy := intent
		return &intentCopy, nil
	}

	if captureBacked {
		return nil, fmt.Errorf("failed to resolve semantic product intent for product '%s': capture-backed manifest generation requires semantic product intent", product.Name)
	}
	return nil, fmt.Errorf("failed to resolve semantic product intent for product '%s': missing semantic product intent", product.Name)
}

func buildExportedParameterValues(params []*dsl.ParameterNode, resolvedValues map[string]interface{}) ([]string, []interface{}, map[string]any, error) {
	headers := make([]string, 0, len(params))
	values := make([]interface{}, 0, len(params))
	exportedValues := make(map[string]any, len(params))
	for _, param := range params {
		value, ok := resolvedValues[param.Name]
		if !ok {
			return nil, nil, nil, fmt.Errorf("parameter %q is missing a resolved value", param.Name)
		}
		headers = append(headers, param.Name)
		values = append(values, value)
		exportedValues[param.Name] = value
	}
	return headers, values, exportedValues, nil
}

func buildExportManifestOutputs(baseName string, outputTypes []string, includeObject bool) ([]ExportManifestOutput, error) {
	if isNativeOnlyOutputRequest(outputTypes) {
		return []ExportManifestOutput{}, nil
	}
	outputs := make([]ExportManifestOutput, 0, len(outputTypes))
	for _, outputType := range outputTypes {
		spec, ok := artifact.ExportOutputSpecFor(outputType)
		if !ok {
			return nil, fmt.Errorf("unsupported output format %q", outputType)
		}
		output := ExportManifestOutput{
			Type:     artifact.NormalizeExportOutputType(outputType),
			Filename: baseName + spec.FilenameExtension,
		}
		if includeObject && spec.RequiresObject {
			output.Object = freeCADExportObjectName
		}
		outputs = append(outputs, output)
	}
	return outputs, nil
}

func buildExportManifestIntent(baseName string, outputTypes []string, includeObject bool, params []*dsl.ParameterNode, resolvedValues map[string]any, captureBacked bool, model *semantic.Model, intent *semantic.ProductIntent, targetActionMutations []semantic.MutationIntent, contract *semanticmap.SemanticMap) (*semanticManifestProjectionResult, error) {
	if captureBacked {
		result, err := projectSemanticManifestIntent(&semanticManifestProjectionRequest{
			BaseName:              baseName,
			CaptureBacked:         true,
			Model:                 model,
			Intent:                intent,
			TargetActionMutations: targetActionMutations,
			ResolvedValues:        resolvedValues,
			Contract:              contract,
		})
		if err != nil {
			return nil, err
		}
		if includeObject {
			result.AssemblyMutations = mergeExportManifestMutationCollections(result.AssemblyMutations, result.TargetActionMutationCollections, true)
			result.PartMutations = mergeExportManifestMutationCollections(result.PartMutations, result.TargetActionMutationCollections, false)
			result.AssemblyMutations = canonicalizeRuntimeTargetMutationOrdering(result.AssemblyMutations)
			result.PartMutations = canonicalizeRuntimeTargetMutationOrdering(result.PartMutations)
			if err := ValidateFreeCADCADTargetMappings(result.ParameterAssignments, result.AssemblyMutations, result.PartMutations); err != nil {
				return nil, err
			}
		}
		return result, nil
	}

	outputs, err := buildExportManifestOutputs(baseName, outputTypes, includeObject)
	if err != nil {
		return nil, err
	}

	assignments, err := buildDeclaredExportManifestParameterAssignments(params, resolvedValues)
	if err != nil {
		return nil, err
	}
	if includeObject {
		if err := ValidateFreeCADCADTargetMappings(assignments, nil, nil); err != nil {
			return nil, err
		}
	}

	return &semanticManifestProjectionResult{
		ParameterAssignments: assignments,
		Outputs:              outputs,
	}, nil
}

func ValidateFreeCADNameOnlyParameterAssignments(assignments []ExportManifestParameterAssignment, assembly, part *ExportManifestMutationCollection) error {
	return ValidateFreeCADCADTargetMappings(assignments, assembly, part)
}

func ValidateFreeCADCADTargetMappings(assignments []ExportManifestParameterAssignment, assembly, part *ExportManifestMutationCollection) error {
	for i, assignment := range assignments {
		if strings.TrimSpace(assignment.Target) != "" {
			continue
		}
		name := strings.TrimSpace(assignment.Name)
		matches := countMatchingParameterMutationSources(name, assembly, part)
		switch {
		case matches == 1:
			continue
		case matches == 0:
			return fmt.Errorf("parameterAssignments[%d] freecad parameter assignment %q is missing an explicit CAD target mapping; name-only parameter assignments are not supported", i, name)
		default:
			return fmt.Errorf("parameterAssignments[%d] freecad parameter assignment %q has ambiguous CAD target mappings", i, name)
		}
	}
	return nil
}

func hasDeterministicParameterMutationSource(name string, assembly, part *ExportManifestMutationCollection) bool {
	return countMatchingParameterMutationSources(name, assembly, part) == 1
}

func countMatchingParameterMutationSources(name string, assembly, part *ExportManifestMutationCollection) int {
	if name == "" {
		return 0
	}

	matches := 0
	if assembly != nil {
		matches += countMatchingParameterMutations(assembly.Parameters, name)
	}
	if part != nil {
		matches += countMatchingParameterMutations(part.Parameters, name)
	}
	return matches
}

func countMatchingParameterMutations(mutations []ExportManifestParameterMutation, name string) int {
	count := 0
	for _, mutation := range mutations {
		if mutation.ValueParam == name {
			count++
		}
	}
	return count
}

func buildDeclaredExportManifestParameterAssignments(params []*dsl.ParameterNode, resolvedValues map[string]any) ([]ExportManifestParameterAssignment, error) {
	if len(params) == 0 {
		return []ExportManifestParameterAssignment{}, nil
	}

	assignments := make([]ExportManifestParameterAssignment, 0, len(params))
	for _, param := range params {
		if param.Type.Kind != dsl.ParamTypeNumber {
			continue
		}

		rawValue, ok := resolvedValues[param.Name]
		if !ok {
			return nil, fmt.Errorf("parameter %q is missing a resolved value", param.Name)
		}
		value, ok := rawValue.(float64)
		if !ok {
			return nil, fmt.Errorf("parameter %q resolved to %T; expected float64", param.Name, rawValue)
		}

		assignments = append(assignments, ExportManifestParameterAssignment{
			Name:  param.Name,
			Value: value,
			Type:  string(dsl.ParamTypeNumber),
			Unit:  "mm",
		})
	}
	return assignments, nil
}

func projectSemanticManifestIntent(request *semanticManifestProjectionRequest) (*semanticManifestProjectionResult, error) {
	if request == nil {
		return nil, fmt.Errorf("semantic manifest projection request must not be nil")
	}
	if request.Intent == nil {
		if request.CaptureBacked {
			return nil, fmt.Errorf("capture-backed manifest generation requires semantic product intent")
		}
		return nil, fmt.Errorf("semantic product intent must not be nil")
	}
	if request.Model == nil {
		if request.CaptureBacked {
			return nil, fmt.Errorf("capture-backed manifest generation requires semantic model")
		}
		return nil, fmt.Errorf("semantic model must not be nil")
	}
	if request.Contract == nil {
		if request.CaptureBacked {
			return nil, fmt.Errorf("capture-backed manifest generation requires semantic map")
		}
		return nil, fmt.Errorf("semantic map must not be nil")
	}
	if err := semanticmap.ValidateProjectionContract(request.Contract); err != nil {
		return nil, err
	}

	linkage, err := semanticmap.LinkCaptureIdentities(request.Contract, request.Model)
	if err != nil {
		return nil, err
	}

	assignments, err := semanticmap.ProjectParameterAssignments(&semanticmap.ParameterProjectionRequest{
		Model:          request.Model,
		Intent:         request.Intent,
		ResolvedValues: request.ResolvedValues,
		Contract:       request.Contract,
	})
	if err != nil {
		return nil, err
	}

	projectedOutputs, err := semanticmap.ProjectOutputs(&semanticmap.OutputProjectionRequest{
		Model:           request.Model,
		Intent:          request.Intent,
		Contract:        request.Contract,
		IdentityLinkage: linkage,
	})
	if err != nil {
		return nil, err
	}

	projectedMutations, err := semanticmap.ProjectMutations(&semanticmap.MutationProjectionRequest{
		Model:           request.Model,
		Intent:          request.Intent,
		ResolvedValues:  request.ResolvedValues,
		Contract:        request.Contract,
		IdentityLinkage: linkage,
	})
	if err != nil {
		return nil, err
	}

	var targetActionRouting *semanticmap.TargetMutationRouting
	var targetActionMutationCollections *semanticmap.ProjectedTargetMutationRouting
	if len(request.TargetActionMutations) > 0 {
		targetActionRouting, err = semanticmap.RouteTargetMutations(&semanticmap.TargetMutationRoutingRequest{
			Model:           request.Model,
			IdentityLinkage: linkage,
			Mutations:       request.TargetActionMutations,
		})
		if err != nil {
			return nil, err
		}
		targetActionMutationCollections, err = semanticmap.ProjectTargetMutationRouting(targetActionRouting)
		if err != nil {
			return nil, err
		}
	}

	outputs := make([]ExportManifestOutput, 0, len(projectedOutputs))
	for _, output := range projectedOutputs {
		spec, ok := artifact.ExportOutputSpecFor(output.Type)
		if !ok {
			return nil, fmt.Errorf("unsupported output format %q", output.Type)
		}
		outputs = append(outputs, ExportManifestOutput{
			Type:     output.Type,
			Filename: request.BaseName + spec.FilenameExtension,
			Object:   output.Object,
			Target:   output.Target,
			Rollup:   output.Rollup,
			Columns:  append([]string(nil), output.Columns...),
		})
	}

	var assemblyMutations *ExportManifestMutationCollection
	var partMutations *ExportManifestMutationCollection
	if projectedMutations != nil {
		assemblyMutations = toExportManifestMutationCollection(projectedMutations.Assembly)
		partMutations = toExportManifestMutationCollection(projectedMutations.Part)
	}

	out := &semanticManifestProjectionResult{
		ParameterAssignments:            make([]ExportManifestParameterAssignment, len(assignments)),
		Outputs:                         outputs,
		AssemblyMutations:               assemblyMutations,
		PartMutations:                   partMutations,
		TargetActionRouting:             targetActionRouting,
		TargetActionMutationCollections: targetActionMutationCollections,
	}
	for i, assignment := range assignments {
		out.ParameterAssignments[i] = ExportManifestParameterAssignment{
			Name:   assignment.Name,
			Target: explicitParameterAssignmentTarget(assignment.Name, assemblyMutations, partMutations),
			Value:  assignment.Value,
			Type:   assignment.Type,
			Unit:   assignment.Unit,
		}
	}
	parametersByID := make(map[string]semantic.Parameter, len(request.Model.Parameters))
	for _, parameter := range request.Model.CanonicalParameters() {
		parametersByID[parameter.ID] = parameter
	}
	groupsByID := make(map[string]semantic.ParameterGroup, len(request.Model.ParameterGroups))
	for _, group := range request.Model.CanonicalParameterGroups() {
		groupsByID[group.ID] = group
	}
	for _, exportedParam := range request.Intent.ExportedParameters {
		assignment, ok := findProjectedAssignment(assignments, exportedParam.DSLParameterName)
		if !ok {
			continue
		}
		semanticParameter, ok := parametersByID[exportedParam.SemanticParameterID]
		if !ok {
			continue
		}
		out.Verification.ExpectedParameters = append(out.Verification.ExpectedParameters, VerificationExpectedParameter{
			ID:    exportedParam.SemanticParameterID,
			Name:  assignment.Name,
			Value: assignment.Value,
			Type:  assignment.Type,
			Unit:  assignment.Unit,
		})
		groupName := ""
		if semanticParameter.GroupID != "" {
			group, ok := groupsByID[semanticParameter.GroupID]
			if !ok {
				continue
			}
			groupName = group.Name
		}
		out.Verification.ObservationParameterLinks = append(out.Verification.ObservationParameterLinks, VerificationObservationParameterLink{
			ID:        exportedParam.SemanticParameterID,
			Name:      semanticParameter.Name,
			GroupName: groupName,
		})
	}
	return out, nil
}

func explicitParameterAssignmentTarget(name string, assembly, part *ExportManifestMutationCollection) string {
	matches := make([]ExportManifestParameterMutation, 0, 2)
	if assembly != nil {
		matches = appendMatchingParameterMutations(matches, assembly.Parameters, name)
	}
	if part != nil {
		matches = appendMatchingParameterMutations(matches, part.Parameters, name)
	}
	if len(matches) != 1 {
		return ""
	}

	mutation := matches[0]
	return mutation.Object + "." + mutation.Property
}

func appendMatchingParameterMutations(dst []ExportManifestParameterMutation, mutations []ExportManifestParameterMutation, name string) []ExportManifestParameterMutation {
	for _, mutation := range mutations {
		if mutation.ValueParam == name {
			dst = append(dst, mutation)
		}
	}
	return dst
}

func findProjectedAssignment(assignments []semanticmap.ManifestParameterAssignment, name string) (semanticmap.ManifestParameterAssignment, bool) {
	for _, assignment := range assignments {
		if assignment.Name == name {
			return assignment, true
		}
	}
	return semanticmap.ManifestParameterAssignment{}, false
}

func toExportManifestMutationCollection(projected *semanticmap.ManifestMutationCollection) *ExportManifestMutationCollection {
	if projected == nil {
		return nil
	}

	collection := &ExportManifestMutationCollection{}
	for _, mutation := range projected.Parameters {
		collection.Parameters = append(collection.Parameters, ExportManifestParameterMutation{
			Object:     mutation.Object,
			Property:   mutation.Property,
			ValueParam: mutation.ValueParam,
			Type:       mutation.Type,
			Unit:       mutation.Unit,
		})
	}
	for _, mutation := range projected.Properties {
		collection.Properties = append(collection.Properties, ExportManifestPropertyMutation{
			Object:   mutation.Object,
			Property: mutation.Property,
			Value:    mutation.Value,
		})
	}
	for _, mutation := range projected.Suppression {
		collection.Suppression = append(collection.Suppression, ExportManifestSuppressionMutation{
			Object:     mutation.Object,
			Suppressed: mutation.Suppressed,
		})
	}
	for _, mutation := range projected.Visibility {
		collection.Visibility = append(collection.Visibility, ExportManifestVisibilityMutation{
			Object: mutation.Object, Visible: mutation.Visible,
		})
	}
	for _, mutation := range projected.Deletion {
		collection.Deletion = append(collection.Deletion, ExportManifestDeletionMutation{Object: mutation.Object})
	}

	if mutationCollectionEmpty(collection) {
		return nil
	}
	return collection
}

func mergeExportManifestMutationCollections(base *ExportManifestMutationCollection, routed *semanticmap.ProjectedTargetMutationRouting, assembly bool) *ExportManifestMutationCollection {
	var target *semanticmap.ManifestMutationCollection
	if routed != nil {
		if assembly {
			target = routed.Assembly
		} else {
			target = routed.Part
		}
	}
	targetCollection := toExportManifestMutationCollection(target)
	if base == nil {
		return targetCollection
	}
	if targetCollection == nil {
		return base
	}
	return &ExportManifestMutationCollection{
		Parameters:  append(append([]ExportManifestParameterMutation(nil), base.Parameters...), targetCollection.Parameters...),
		Properties:  append(append([]ExportManifestPropertyMutation(nil), base.Properties...), targetCollection.Properties...),
		Suppression: append(append([]ExportManifestSuppressionMutation(nil), base.Suppression...), targetCollection.Suppression...),
		Visibility:  append(append([]ExportManifestVisibilityMutation(nil), base.Visibility...), targetCollection.Visibility...),
		Deletion:    append(append([]ExportManifestDeletionMutation(nil), base.Deletion...), targetCollection.Deletion...),
	}
}

func canonicalizeRuntimeTargetMutationOrdering(collection *ExportManifestMutationCollection) *ExportManifestMutationCollection {
	if collection == nil || !hasRuntimeTargetMutations(collection) {
		return collection
	}

	canonical := *collection
	canonical.Suppression = append([]ExportManifestSuppressionMutation(nil), collection.Suppression...)
	canonical.Visibility = append([]ExportManifestVisibilityMutation(nil), collection.Visibility...)
	canonical.Deletion = append([]ExportManifestDeletionMutation(nil), collection.Deletion...)

	sort.Slice(canonical.Suppression, func(i, j int) bool {
		left, right := canonical.Suppression[i], canonical.Suppression[j]
		if left.Object != right.Object {
			return left.Object < right.Object
		}
		return !left.Suppressed && right.Suppressed
	})
	sort.Slice(canonical.Visibility, func(i, j int) bool {
		left, right := canonical.Visibility[i], canonical.Visibility[j]
		if left.Object != right.Object {
			return left.Object < right.Object
		}
		return !left.Visible && right.Visible
	})
	sort.Slice(canonical.Deletion, func(i, j int) bool {
		return canonical.Deletion[i].Object < canonical.Deletion[j].Object
	})

	return &canonical
}

func mutationCollectionEmpty(collection *ExportManifestMutationCollection) bool {
	return collection == nil || (len(collection.Parameters) == 0 && len(collection.Properties) == 0 && len(collection.Suppression) == 0 && len(collection.Visibility) == 0 && len(collection.Deletion) == 0)
}

func exportManifestSchemaVersionForProjection(mode ExportManifestProjectionMode, assembly, part *ExportManifestMutationCollection) string {
	if mode == ExportManifestProjectionModeFreeCADRuntimeNative && (hasRuntimeTargetMutations(assembly) || hasRuntimeTargetMutations(part)) {
		return FreeCADRuntimeMutationManifestSchemaVersion
	}
	return ExportManifestSchemaVersion
}

func hasRuntimeTargetMutations(collection *ExportManifestMutationCollection) bool {
	return collection != nil && (len(collection.Suppression) > 0 || len(collection.Visibility) > 0 || len(collection.Deletion) > 0)
}

func copyResolvedValues(values map[string]any) map[string]interface{} {
	copied := make(map[string]interface{}, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

var errUnresolvedIdentifier = errors.New("unresolved identifier")

// evaluateExpression recursively evaluates an expression node to a concrete value.
func evaluateExpression(
	expr dsl.ExpressionNode,
	scope map[string]interface{},
	paramNames map[string]struct{},
	paramScope map[string]*dsl.ParameterNode,
	constValues map[string]dsl.ConstantValue,
	expectedType *dsl.ParameterType,
) (interface{}, error) {
	return evaluateExpressionWithContext(expr, scope, paramNames, paramScope, constValues, expectedType, plannerEvalContext{})
}

type plannerEvalContext struct {
	tables map[string]*table.Table
}

type evaluatedTargetAction struct {
	SemanticTarget string
	Action         string
}

type resolvedTargetAction struct {
	SemanticTarget string
	Action         string
	Target         semantic.ResolvedSemanticTarget
}

func resolveEvaluatedTargetActions(model *semantic.Model, actions []evaluatedTargetAction) ([]resolvedTargetAction, error) {
	resolved := make([]resolvedTargetAction, 0, len(actions))
	for _, action := range actions {
		target, err := semantic.ResolveSemanticTargetByExactName(model, action.SemanticTarget)
		if err != nil {
			return nil, fmt.Errorf("target action '%s': %w", action.SemanticTarget, err)
		}
		resolved = append(resolved, resolvedTargetAction{
			SemanticTarget: action.SemanticTarget,
			Action:         action.Action,
			Target:         target,
		})
	}
	return resolved, nil
}

func validateResolvedTargetActionCapabilities(actions []resolvedTargetAction) error {
	for _, action := range actions {
		if err := semantic.ValidateTargetActionCapability(action.Target, action.Action); err != nil {
			return fmt.Errorf("target action '%s': %w", action.SemanticTarget, err)
		}
	}
	return nil
}

func lowerResolvedTargetActions(actions []resolvedTargetAction) ([]semantic.MutationIntent, error) {
	mutations := make([]semantic.MutationIntent, 0, len(actions))
	for _, action := range actions {
		mutation, err := semantic.LowerTargetActionMutationIntent(action.Target, action.Action)
		if err != nil {
			return nil, fmt.Errorf("target action '%s': %w", action.SemanticTarget, err)
		}
		if mutation != nil {
			mutations = append(mutations, *mutation)
		}
	}
	return mutations, nil
}

func evaluateProductTargetActions(
	product *dsl.ProductNode,
	resolvedValues map[string]interface{},
	productBindingNames map[string]struct{},
	productBindingScope map[string]*dsl.ParameterNode,
	constants map[string]dsl.ConstantValue,
	tables map[string]*table.Table,
) ([]evaluatedTargetAction, error) {
	evaluated := make([]evaluatedTargetAction, 0, len(product.TargetActions))
	expectedType := &dsl.ParameterType{Kind: dsl.ParamTypeAction}
	for _, targetAction := range product.TargetActions {
		value, err := evaluateExpressionWithContext(targetAction.Action, resolvedValues, productBindingNames, productBindingScope, constants, expectedType, plannerEvalContext{tables: tables})
		if err != nil {
			return nil, fmt.Errorf("target action '%s': %w", targetAction.SemanticTarget, err)
		}
		action, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("target action '%s' evaluated to %T, but expected string action", targetAction.SemanticTarget, value)
		}
		if !dsl.IsCanonicalActionValue(action) {
			return nil, fmt.Errorf("target action '%s' evaluated to invalid action value '%s', allowed values are %v", targetAction.SemanticTarget, action, dsl.CanonicalActionValues())
		}
		evaluated = append(evaluated, evaluatedTargetAction{SemanticTarget: targetAction.SemanticTarget, Action: action})
	}
	return evaluated, nil
}

func evaluateExpressionWithContext(
	expr dsl.ExpressionNode,
	scope map[string]interface{},
	paramNames map[string]struct{},
	paramScope map[string]*dsl.ParameterNode,
	constValues map[string]dsl.ConstantValue,
	expectedType *dsl.ParameterType,
	ctx plannerEvalContext,
) (interface{}, error) {
	switch e := expr.(type) {
	case *dsl.LiteralExpression:
		return e.Value, nil

	case *dsl.InterpolatedStringExpression:
		var b strings.Builder
		for _, seg := range e.Segments {
			if seg.ParamName == "" {
				b.WriteString(seg.Text)
				continue
			}
			val, ok := scope[seg.ParamName]
			if !ok {
				if _, isParameter := paramNames[seg.ParamName]; isParameter {
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

	case *dsl.IdentifierExpression:
		if expectedType != nil && expectedType.Kind == dsl.ParamTypeAction {
			if dsl.IsCanonicalActionValue(e.Name) {
				return e.Name, nil
			}
			return nil, fmt.Errorf("invalid action value '%s', allowed values are %v", e.Name, dsl.CanonicalActionValues())
		}
		val, ok := scope[e.Name]
		if ok {
			return val, nil
		}
		if constVal, ok := constValues[e.Name]; ok {
			return constVal.Value, nil
		}
		if _, isParameter := paramNames[e.Name]; isParameter {
			return nil, fmt.Errorf("%w: %s", errUnresolvedIdentifier, e.Name)
		}
		if expectedType != nil && expectedType.Kind == dsl.ParamTypeEnum {
			for _, allowedValue := range expectedType.EnumValues {
				if e.Name == allowedValue {
					return e.Name, nil
				}
			}
			return nil, fmt.Errorf("invalid enum value '%s', allowed values are %v", e.Name, expectedType.EnumValues)
		}
		return nil, fmt.Errorf("undefined identifier '%s'", e.Name)

	case *dsl.TernaryExpression:
		condVal, err := evaluateExpressionWithContext(e.Condition, scope, paramNames, paramScope, constValues, &dsl.ParameterType{Kind: dsl.ParamTypeBoolean}, ctx)
		if err != nil {
			return nil, err
		}

		condBool, ok := condVal.(bool)
		if !ok {
			// This should be caught by the validator, but it's good practice to check at runtime.
			return nil, fmt.Errorf("ternary condition did not evaluate to a boolean, got %T", condVal)
		}

		if condBool {
			return evaluateExpressionWithContext(e.TrueExpr, scope, paramNames, paramScope, constValues, expectedType, ctx)
		}
		return evaluateExpressionWithContext(e.FalseExpr, scope, paramNames, paramScope, constValues, expectedType, ctx)

	case *dsl.FunctionCallExpression:
		if e.Name == "table_cell" {
			return evaluateTableCellExpression(e, scope, paramNames, paramScope, constValues, expectedType, ctx)
		}

		// 1. Evaluate all arguments first.
		evaluatedArgs := make([]interface{}, len(e.Args))
		for i, argExpr := range e.Args {
			val, err := evaluateExpressionWithContext(argExpr, scope, paramNames, paramScope, constValues, &dsl.ParameterType{Kind: dsl.ParamTypeNumber}, ctx)
			if err != nil {
				return nil, err // Propagate unresolved identifier errors
			}
			evaluatedArgs[i] = val
		}

		// 2. Look up the function in the registry.
		fn, ok := runtime.Builtins[e.Name]
		if !ok {
			// This should be caught by the validator, but it's good practice to check.
			return nil, fmt.Errorf("unknown function called: %s", e.Name)
		}

		// 3. Call the function and return its result.
		return fn(evaluatedArgs)

	case *dsl.UnaryExpression:
		var operandType *dsl.ParameterType
		switch e.Operator {
		case "-":
			operandType = &dsl.ParameterType{Kind: dsl.ParamTypeNumber}
		case "!":
			operandType = &dsl.ParameterType{Kind: dsl.ParamTypeBoolean}
		}
		val, err := evaluateExpressionWithContext(e.Operand, scope, paramNames, paramScope, constValues, operandType, ctx)
		if err != nil {
			return nil, err
		}

		switch e.Operator {
		case "-":
			num, ok := val.(float64)
			if !ok {
				return nil, fmt.Errorf("unary operator '-' requires number operand, but got %T", val)
			}
			return -num, nil
		case "!":
			b, ok := val.(bool)
			if !ok {
				return nil, fmt.Errorf("unary operator '!' requires boolean operand, but got %T", val)
			}
			return !b, nil
		default:
			return nil, fmt.Errorf("unknown unary operator: %s", e.Operator)
		}

	case *dsl.BinaryExpression:
		leftVal, err := evaluateExpressionWithContext(e.Left, scope, paramNames, paramScope, constValues, nil, ctx)
		if err != nil {
			return nil, err
		}

		switch e.Operator {
		case "&&", "||":
			leftBool, ok := leftVal.(bool)
			if !ok {
				return nil, fmt.Errorf("logical operator '%s' requires boolean operands, but got %T", e.Operator, leftVal)
			}
			if e.Operator == "&&" {
				if !leftBool {
					return false, nil
				}
				rightVal, err := evaluateExpressionWithContext(e.Right, scope, paramNames, paramScope, constValues, &dsl.ParameterType{Kind: dsl.ParamTypeBoolean}, ctx)
				if err != nil {
					return nil, err
				}
				rightBool, ok := rightVal.(bool)
				if !ok {
					return nil, fmt.Errorf("logical operator '%s' requires boolean operands, but got %T", e.Operator, rightVal)
				}
				return leftBool && rightBool, nil
			}
			// e.Operator == "||"
			if leftBool {
				return true, nil
			}
			rightVal, err := evaluateExpressionWithContext(e.Right, scope, paramNames, paramScope, constValues, &dsl.ParameterType{Kind: dsl.ParamTypeBoolean}, ctx)
			if err != nil {
				return nil, err
			}
			rightBool, ok := rightVal.(bool)
			if !ok {
				return nil, fmt.Errorf("logical operator '%s' requires boolean operands, but got %T", e.Operator, rightVal)
			}
			return leftBool || rightBool, nil

		default:
			var operandType dsl.ParameterType
			switch e.Operator {
			case "==", "!=":
				if enumType, ok := inferEnumTypeFromExpr(e.Left, paramScope, constValues); ok {
					operandType = enumType
				} else if enumType, ok := inferEnumTypeFromExpr(e.Right, paramScope, constValues); ok {
					operandType = enumType
				} else if isStringExpr(e.Left, paramScope, constValues, ctx.tables) || isStringExpr(e.Right, paramScope, constValues, ctx.tables) {
					operandType = dsl.ParameterType{Kind: dsl.ParamTypeString}
				} else if isBooleanExpr(e.Left, paramScope, constValues, ctx.tables) || isBooleanExpr(e.Right, paramScope, constValues, ctx.tables) {
					operandType = dsl.ParameterType{Kind: dsl.ParamTypeBoolean}
				} else {
					operandType = dsl.ParameterType{Kind: dsl.ParamTypeNumber}
				}
			case "<", "<=", ">", ">=":
				if isStringExpr(e.Left, paramScope, constValues, ctx.tables) || isStringExpr(e.Right, paramScope, constValues, ctx.tables) {
					operandType = dsl.ParameterType{Kind: dsl.ParamTypeString}
				} else {
					operandType = dsl.ParameterType{Kind: dsl.ParamTypeNumber}
				}
			case "+":
				if isStringExpr(e.Left, paramScope, constValues, ctx.tables) || isStringExpr(e.Right, paramScope, constValues, ctx.tables) {
					operandType = dsl.ParameterType{Kind: dsl.ParamTypeString}
				} else {
					operandType = dsl.ParameterType{Kind: dsl.ParamTypeNumber}
				}
			case "-", "*", "/":
				operandType = dsl.ParameterType{Kind: dsl.ParamTypeNumber}
			default:
				return nil, fmt.Errorf("unknown operator: %s", e.Operator)
			}

			leftVal, err = evaluateExpressionWithContext(e.Left, scope, paramNames, paramScope, constValues, &operandType, ctx)
			if err != nil {
				return nil, err
			}

			rightVal, err := evaluateExpressionWithContext(e.Right, scope, paramNames, paramScope, constValues, &operandType, ctx)
			if err != nil {
				return nil, err
			}

			if operandType.Kind == dsl.ParamTypeString {
				leftStr, okL := leftVal.(string)
				rightStr, okR := rightVal.(string)
				if !okL || !okR {
					return nil, fmt.Errorf("operator '%s' requires string operands, but got %T and %T", e.Operator, leftVal, rightVal)
				}
				switch e.Operator {
				case "+":
					return leftStr + rightStr, nil
				case "==":
					return leftStr == rightStr, nil
				case "!=":
					return leftStr != rightStr, nil
				case "<":
					return leftStr < rightStr, nil
				case "<=":
					return leftStr <= rightStr, nil
				case ">":
					return leftStr > rightStr, nil
				case ">=":
					return leftStr >= rightStr, nil
				default:
					return nil, fmt.Errorf("operator '%s' requires number operands, but got %T and %T", e.Operator, leftVal, rightVal)
				}
			}

			if operandType.Kind == dsl.ParamTypeBoolean {
				leftBool, okL := leftVal.(bool)
				rightBool, okR := rightVal.(bool)
				if !okL || !okR {
					return nil, fmt.Errorf("operator '%s' requires boolean operands, but got %T and %T", e.Operator, leftVal, rightVal)
				}
				switch e.Operator {
				case "==":
					return leftBool == rightBool, nil
				case "!=":
					return leftBool != rightBool, nil
				default:
					return nil, fmt.Errorf("operator '%s' requires number operands, but got %T and %T", e.Operator, leftVal, rightVal)
				}
			}

			if operandType.Kind == dsl.ParamTypeEnum {
				leftStr, okL := leftVal.(string)
				rightStr, okR := rightVal.(string)
				if !okL || !okR {
					return nil, fmt.Errorf("operator '%s' requires enum operands, but got %T and %T", e.Operator, leftVal, rightVal)
				}
				switch e.Operator {
				case "==":
					return leftStr == rightStr, nil
				case "!=":
					return leftStr != rightStr, nil
				default:
					return nil, fmt.Errorf("operator '%s' requires number operands, but got %T and %T", e.Operator, leftVal, rightVal)
				}
			}

			leftNum, okL := leftVal.(float64)
			rightNum, okR := rightVal.(float64)
			if !okL || !okR {
				return nil, fmt.Errorf("operator '%s' requires number operands, but got %T and %T", e.Operator, leftVal, rightVal)
			}

			switch e.Operator {
			// Comparisons (return bool)
			case "==":
				return leftNum == rightNum, nil
			case "!=":
				return leftNum != rightNum, nil
			case "<":
				return leftNum < rightNum, nil
			case "<=":
				return leftNum <= rightNum, nil
			case ">":
				return leftNum > rightNum, nil
			case ">=":
				return leftNum >= rightNum, nil
			// Arithmetic (return float64)
			case "+":
				return leftNum + rightNum, nil
			case "-":
				return leftNum - rightNum, nil
			case "*":
				return leftNum * rightNum, nil
			case "/":
				if rightNum == 0 {
					return nil, fmt.Errorf("division by zero")
				}
				return leftNum / rightNum, nil
			default:
				return nil, fmt.Errorf("unknown operator: %s", e.Operator)
			}
		}
	}
	return nil, fmt.Errorf("unknown expression type: %T", expr)
}

func evaluateTableCellExpression(
	expr *dsl.FunctionCallExpression,
	scope map[string]interface{},
	paramNames map[string]struct{},
	paramScope map[string]*dsl.ParameterNode,
	constValues map[string]dsl.ConstantValue,
	expectedType *dsl.ParameterType,
	ctx plannerEvalContext,
) (interface{}, error) {
	if len(expr.Args) != 3 {
		return nil, fmt.Errorf("function '%s' expects 3 arguments, but got %d", expr.Name, len(expr.Args))
	}

	tableName, ok := plannerStringLiteral(expr.Args[0])
	if !ok {
		return nil, fmt.Errorf("argument 1 of '%s' must be a string literal", expr.Name)
	}
	columnName, ok := plannerStringLiteral(expr.Args[2])
	if !ok {
		return nil, fmt.Errorf("argument 3 of '%s' must be a string literal", expr.Name)
	}

	rowKeyValue, err := evaluateExpressionWithContext(expr.Args[1], scope, paramNames, paramScope, constValues, &dsl.ParameterType{Kind: dsl.ParamTypeString}, ctx)
	if err != nil {
		return nil, fmt.Errorf("argument 2 of '%s': %w", expr.Name, err)
	}
	rowKey, ok := rowKeyValue.(string)
	if !ok {
		return nil, fmt.Errorf("argument 2 of '%s' must resolve to string, got %T", expr.Name, rowKeyValue)
	}

	if ctx.tables == nil {
		return nil, fmt.Errorf("function '%s' references unknown table '%s'", expr.Name, tableName)
	}
	tbl, ok := ctx.tables[tableName]
	if !ok || tbl == nil {
		return nil, fmt.Errorf("function '%s' references unknown table '%s'", expr.Name, tableName)
	}
	if err := table.Validate(tbl); err != nil {
		return nil, fmt.Errorf("function '%s' table '%s' is invalid: %w", expr.Name, tableName, err)
	}

	column, ok := plannerFindTableColumn(tbl, columnName)
	if !ok {
		return nil, fmt.Errorf("function '%s' references unknown column '%s' in table '%s'", expr.Name, columnName, tableName)
	}
	if expectedType != nil && expectedType.Kind == dsl.ParamTypeAction && column.Type != table.ColumnTypeString {
		return nil, fmt.Errorf("function '%s' returns a %s from table '%s' column '%s', but expected type is 'action'", expr.Name, column.Type, tableName, columnName)
	}

	row, err := tbl.RowByKey(rowKey)
	if err != nil {
		return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': %w", expr.Name, tableName, rowKey, columnName, err)
	}

	value, ok := row.Values[columnName]
	if !ok {
		if column.Required {
			return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': required column value is missing", expr.Name, tableName, rowKey, columnName)
		}
		return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': optional column value is missing", expr.Name, tableName, rowKey, columnName)
	}

	switch value.Type {
	case table.ColumnTypeString:
		if expectedType != nil && expectedType.Kind == dsl.ParamTypeAction && !dsl.IsCanonicalActionValue(value.String) {
			return nil, fmt.Errorf("function '%s' selected invalid action value '%s' from table '%s' row %q column '%s'; allowed values are %v", expr.Name, value.String, tableName, rowKey, columnName, dsl.CanonicalActionValues())
		}
		return value.String, nil
	case table.ColumnTypeNumber:
		number, err := value.Number.Float64()
		if err != nil {
			return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': invalid number value: %w", expr.Name, tableName, rowKey, columnName, err)
		}
		return number, nil
	case table.ColumnTypeBoolean:
		return value.Boolean, nil
	default:
		return nil, fmt.Errorf("function '%s' lookup failed for table '%s' row %q column '%s': unsupported column type '%s'", expr.Name, tableName, rowKey, columnName, value.Type)
	}
}

func isStringExpr(expr dsl.ExpressionNode, paramScope map[string]*dsl.ParameterNode, constScope map[string]dsl.ConstantValue, tables map[string]*table.Table) bool {
	switch e := expr.(type) {
	case *dsl.LiteralExpression:
		_, ok := e.Value.(string)
		return ok
	case *dsl.InterpolatedStringExpression:
		return true
	case *dsl.IdentifierExpression:
		if param, exists := paramScope[e.Name]; exists && param.Type.Kind == dsl.ParamTypeString {
			return true
		}
		if c, exists := constScope[e.Name]; exists && c.Type.Kind == dsl.ParamTypeString {
			return true
		}
	case *dsl.FunctionCallExpression:
		if kind, ok := plannerTableCellResultKind(e, tables); ok && kind == dsl.ParamTypeString {
			return true
		}
	}
	return false
}

func isBooleanExpr(expr dsl.ExpressionNode, paramScope map[string]*dsl.ParameterNode, constScope map[string]dsl.ConstantValue, tables map[string]*table.Table) bool {
	switch e := expr.(type) {
	case *dsl.LiteralExpression:
		_, ok := e.Value.(bool)
		return ok
	case *dsl.IdentifierExpression:
		if param, exists := paramScope[e.Name]; exists && param.Type.Kind == dsl.ParamTypeBoolean {
			return true
		}
		if c, exists := constScope[e.Name]; exists && c.Type.Kind == dsl.ParamTypeBoolean {
			return true
		}
	case *dsl.FunctionCallExpression:
		if kind, ok := plannerTableCellResultKind(e, tables); ok && kind == dsl.ParamTypeBoolean {
			return true
		}
	}
	return false
}

func plannerStringLiteral(expr dsl.ExpressionNode) (string, bool) {
	lit, ok := expr.(*dsl.LiteralExpression)
	if !ok {
		return "", false
	}
	value, ok := lit.Value.(string)
	return value, ok
}

func plannerFindTableColumn(tbl *table.Table, name string) (table.Column, bool) {
	for _, column := range tbl.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return table.Column{}, false
}

func plannerTableCellResultKind(expr *dsl.FunctionCallExpression, tables map[string]*table.Table) (dsl.ParameterTypeKind, bool) {
	if expr.Name != "table_cell" || len(expr.Args) != 3 || tables == nil {
		return "", false
	}
	tableName, ok := plannerStringLiteral(expr.Args[0])
	if !ok {
		return "", false
	}
	columnName, ok := plannerStringLiteral(expr.Args[2])
	if !ok {
		return "", false
	}
	tbl, ok := tables[tableName]
	if !ok || tbl == nil {
		return "", false
	}
	column, ok := plannerFindTableColumn(tbl, columnName)
	if !ok {
		return "", false
	}
	switch column.Type {
	case table.ColumnTypeString:
		return dsl.ParamTypeString, true
	case table.ColumnTypeNumber:
		return dsl.ParamTypeNumber, true
	case table.ColumnTypeBoolean:
		return dsl.ParamTypeBoolean, true
	default:
		return "", false
	}
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
	if param, exists := paramScope[ident.Name]; exists && param.Type.Kind == dsl.ParamTypeEnum && len(param.Type.EnumValues) > 0 {
		return param.Type, true
	}
	if c, exists := constScope[ident.Name]; exists && c.Type.Kind == dsl.ParamTypeEnum && len(c.Type.EnumValues) > 0 {
		return c.Type, true
	}
	return dsl.ParameterType{}, false
}
