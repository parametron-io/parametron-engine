package planner

import (
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
	"parametron/internal/engine/table"
)

// plannerTargetMetadataModel is the capture-backed semantic fixture used by
// target-scoped metadata planner tests. NativeRef values are the only native
// addressing surface; semantic IDs and display names must never leak into the
// runtime manifest.
func plannerTargetMetadataModel() *semantic.Model {
	return &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "box_model",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{
				ID:          "cmp.root",
				Kind:        "assembly",
				Name:        "RootAssembly",
				DisplayName: "Root Assembly",
				NativeRef:   "Assembly01",
				ChildrenIDs: []string{"cmp.bracket"},
				Quantity:    1,
			},
			{
				ID:          "cmp.bracket",
				Kind:        "part",
				Name:        "bracket",
				DisplayName: "Bracket",
				NativeRef:   "Body01",
				ParentID:    "cmp.root",
				Quantity:    1,
				Targetability: cad.Targetability{
					Suppress:   true,
					Unsuppress: true,
				},
			},
		},
		Features: []semantic.Feature{
			{
				ID:          "fea.mounting",
				ComponentID: "cmp.bracket",
				Name:        "mounting_bracket",
				DisplayName: "Mounting Bracket",
				NativeRef:   "MountingBracket01",
			},
		},
		Parameters: []semantic.Parameter{
			{
				ID:          "par.root.width",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.root",
				ComponentID: "cmp.root",
				Name:        "width",
				DisplayName: "Width",
				ValueType:   "number",
				NativeType:  "Length",
				Unit:        "mm",
				Observable:  true,
				Writable:    true,
			},
		},
		Metadata: []semantic.Metadata{
			{
				ID:          "meta.bracket.material",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.bracket",
				ComponentID: "cmp.bracket",
				Key:         "material",
				DisplayName: "Material",
				ValueType:   "string",
				NativeType:  "App::PropertyString",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.root.material",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.root",
				ComponentID: "cmp.root",
				Key:         "material",
				DisplayName: "Root Material",
				ValueType:   "string",
				NativeType:  "App::PropertyString",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.feature.partNumber",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "fea.mounting",
				ComponentID: "cmp.bracket",
				Key:         "part_number",
				DisplayName: "Part Number",
				ValueType:   "string",
				NativeType:  "App::PropertyString",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.feature.revision",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "fea.mounting",
				ComponentID: "cmp.bracket",
				Key:         "revision",
				DisplayName: "Revision",
				ValueType:   "number",
				NativeType:  "App::PropertyInteger",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.feature.tagged",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "fea.mounting",
				ComponentID: "cmp.bracket",
				Key:         "tagged",
				DisplayName: "Tagged",
				ValueType:   "boolean",
				NativeType:  "App::PropertyBool",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.root.assemblyCount",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.root",
				ComponentID: "cmp.root",
				Key:         "assembly_count",
				DisplayName: "Assembly Count",
				ValueType:   "number",
				NativeType:  "App::PropertyLength",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.feature.lockedCode",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "fea.mounting",
				ComponentID: "cmp.bracket",
				Key:         "locked_code",
				DisplayName: "Locked Code",
				ValueType:   "string",
				NativeType:  "App::PropertyString",
				Observable:  true,
				Writable:    false,
			},
			{
				ID:          "meta.feature.hiddenCode",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "fea.mounting",
				ComponentID: "cmp.bracket",
				Key:         "hidden_code",
				DisplayName: "Hidden Code",
				ValueType:   "string",
				NativeType:  "App::PropertyString",
				Observable:  false,
				Writable:    true,
			},
			{
				ID:          "meta.feature.releasedOn",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "fea.mounting",
				ComponentID: "cmp.bracket",
				Key:         "released_on",
				DisplayName: "Released On",
				ValueType:   "date",
				NativeType:  "App::PropertyString",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.feature.mismatchedKind",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "fea.mounting",
				ComponentID: "cmp.bracket",
				Key:         "mismatched_kind",
				DisplayName: "Mismatched Kind",
				ValueType:   "string",
				NativeType:  "App::PropertyInteger",
				Observable:  true,
				Writable:    true,
			},
		},
	}
}

// plannerTargetMetadataContract extends the shared projection contract with the
// additive v1.0 native-value-kind mapping required for target-scoped metadata.
func plannerTargetMetadataContract() *semanticmap.SemanticMap {
	contract := testProjectionContract()
	contract.CaptureToSemantic.Metadata[0].NativeValueKinds = map[string]string{
		"App::PropertyString":  "string",
		"App::PropertyBool":    "boolean",
		"App::PropertyInteger": "integer",
		"App::PropertyLength":  "number",
	}
	return contract
}

func plannerMetadataTables() map[string]*table.Table {
	return map[string]*table.Table{
		"materials": {
			SchemaVersion: table.SchemaVersion,
			Name:          "materials",
			KeyColumn:     "grade",
			Columns: []table.Column{
				{Name: "grade", Type: table.ColumnTypeString, Required: true},
				{Name: "part_number", Type: table.ColumnTypeString, Required: true},
			},
			Rows: []table.Row{
				{Values: map[string]table.Value{
					"grade":       {Type: table.ColumnTypeString, String: "S355"},
					"part_number": {Type: table.ColumnTypeString, String: "PN-1001"},
				}},
			},
		},
	}
}

func planTargetMetadataWithTables(t *testing.T, dslContent string, tables map[string]*table.Table, model *semantic.Model, contract *semanticmap.SemanticMap) (*dsl.AST, *ExecutionPlan, error) {
	t.Helper()

	ast := parseDSLForPlannerTest(t, dslContent)
	if err := dsl.ValidateWithTables(ast, tables); err != nil {
		return ast, nil, err
	}

	intentModel, err := semantic.InjectDSLIntent(semantic.Clone(model), ast)
	if err != nil {
		return ast, nil, err
	}

	plan, err := CreatePlanWithTablesAndSemanticModel(ast, map[string]string{}, tables, intentModel, contract)
	return ast, plan, err
}

func findMetadataMutation(collection *ExportManifestMutationCollection, object, property string) *ExportManifestMetadataMutation {
	if collection == nil {
		return nil
	}
	for i := range collection.Metadata {
		if collection.Metadata[i].Object == object && collection.Metadata[i].Property == property {
			return &collection.Metadata[i]
		}
	}
	return nil
}

func literalMetadataValue(value any) dsl.ExpressionNode {
	return &dsl.LiteralExpression{Value: value}
}

func TestPlannerTargetMetadata_PopulatesVerificationExpectedMetadata(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 12

    target bracket:
        metadata material = "S235JR"
    target mounting_bracket:
        metadata part_number = "PN-1001"
        metadata revision = 7
    target RootAssembly:
        metadata assembly_count = 8
}
`

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, plannerTargetMetadataModel(), plannerTargetMetadataContract())
	payload := manifestPayloadFromPlan(t, plan)

	want := []VerificationExpectedMetadata{
		{ID: "meta.bracket.material", Key: "material", OwnerID: "Body01", Value: "S235JR", ValueKind: "string"},
		{ID: "meta.feature.partNumber", Key: "part_number", OwnerID: "MountingBracket01", Value: "PN-1001", ValueKind: "string"},
		{ID: "meta.feature.revision", Key: "revision", OwnerID: "MountingBracket01", Value: float64(7), ValueKind: "integer"},
		{ID: "meta.root.assemblyCount", Key: "assembly_count", OwnerID: "Assembly01", Value: float64(8), ValueKind: "number"},
	}
	if !reflect.DeepEqual(payload.Verification.ExpectedMetadata, want) {
		t.Fatalf("expected metadata intent\nwant: %#v\ngot:  %#v", want, payload.Verification.ExpectedMetadata)
	}

	// Semantic metadata identity must remain separate from the native owner and
	// property identity used by the runtime mutation and verification request.
	mutationIdentities := map[string]bool{}
	for _, collection := range []*ExportManifestMutationCollection{payload.AssemblyMutations, payload.PartMutations} {
		if collection == nil {
			continue
		}
		for _, mutation := range collection.Metadata {
			mutationIdentities[mutation.Object+"\x00"+mutation.Property] = true
		}
	}
	for _, entry := range payload.Verification.ExpectedMetadata {
		if entry.ID == entry.OwnerID {
			t.Fatalf("semantic metadata ID must not be the native owner selector: %#v", entry)
		}
		if !mutationIdentities[entry.OwnerID+"\x00"+entry.Key] {
			t.Fatalf("expected metadata identity does not match any native mutation: %#v", entry)
		}
	}
}

func TestPlannerTargetMetadata_ProjectsComponentAndFeatureMutations(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 12

    target bracket:
        metadata material = "S235JR"
    target mounting_bracket:
        metadata part_number = "PN-1001"
        metadata revision = 7
        metadata tagged = true
    target RootAssembly:
        metadata assembly_count = 8
}
`

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, plannerTargetMetadataModel(), plannerTargetMetadataContract())
	payload := manifestPayloadFromPlan(t, plan)

	material := findMetadataMutation(payload.PartMutations, "Body01", "material")
	if material == nil {
		t.Fatalf("expected part metadata mutation for Body01.material, got %+v", payload.PartMutations)
	}
	if material.Value != "S235JR" || material.ValueKind != "string" {
		t.Fatalf("unexpected material mutation: %+v", *material)
	}

	partNumber := findMetadataMutation(payload.PartMutations, "MountingBracket01", "part_number")
	if partNumber == nil {
		t.Fatalf("expected feature metadata mutation for MountingBracket01.part_number, got %+v", payload.PartMutations)
	}
	if partNumber.Value != "PN-1001" || partNumber.ValueKind != "string" {
		t.Fatalf("unexpected part_number mutation: %+v", *partNumber)
	}

	revision := findMetadataMutation(payload.PartMutations, "MountingBracket01", "revision")
	if revision == nil {
		t.Fatalf("expected feature metadata mutation for MountingBracket01.revision, got %+v", payload.PartMutations)
	}
	if revision.Value != float64(7) || revision.ValueKind != "integer" {
		t.Fatalf("unexpected revision mutation: %+v", *revision)
	}

	tagged := findMetadataMutation(payload.PartMutations, "MountingBracket01", "tagged")
	if tagged == nil {
		t.Fatalf("expected feature metadata mutation for MountingBracket01.tagged, got %+v", payload.PartMutations)
	}
	if tagged.Value != true || tagged.ValueKind != "boolean" {
		t.Fatalf("unexpected tagged mutation: %+v", *tagged)
	}

	assemblyCount := findMetadataMutation(payload.AssemblyMutations, "Assembly01", "assembly_count")
	if assemblyCount == nil {
		t.Fatalf("expected assembly metadata mutation for Assembly01.assembly_count, got %+v", payload.AssemblyMutations)
	}
	if assemblyCount.Value != float64(8) || assemblyCount.ValueKind != "number" {
		t.Fatalf("unexpected assembly_count mutation: %+v", *assemblyCount)
	}

	if findMetadataMutation(payload.AssemblyMutations, "Body01", "material") != nil {
		t.Fatal("part metadata must not be projected into the assembly mutation bucket")
	}

	manifestJSON := manifestPayloadJSONFromPlan(t, plan)
	for _, forbidden := range []string{"meta.bracket.material", "meta.feature.partNumber", "cmp.bracket", "fea.mounting", "Root Assembly", "Mounting Bracket", "Part Number"} {
		if strings.Contains(manifestJSON, forbidden) {
			t.Fatalf("runtime manifest must not leak semantic provenance %q: %s", forbidden, manifestJSON)
		}
	}
}

func TestPlannerTargetMetadata_EvaluatesLetParamConstantTableAndTernary(t *testing.T) {
	dslContent := `
const MATERIAL = "S235JR"

product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    let suffix = "-A"
    param width: number = 12
    param grade: string = "S355"

    target bracket:
        metadata material = MATERIAL + suffix
    target mounting_bracket:
        metadata part_number = table_cell("materials", grade, "part_number")
        metadata revision = width / 2
        metadata tagged = width > 10 ? true : false
    target RootAssembly:
        metadata assembly_count = width + 4
}
`

	_, plan, err := planTargetMetadataWithTables(t, dslContent, plannerMetadataTables(), plannerTargetMetadataModel(), plannerTargetMetadataContract())
	if err != nil {
		t.Fatalf("unexpected planning error: %v", err)
	}
	payload := manifestPayloadFromPlan(t, plan)

	assertMetadataValue := func(collection *ExportManifestMutationCollection, object, property string, wantValue any, wantKind string) {
		t.Helper()
		mutation := findMetadataMutation(collection, object, property)
		if mutation == nil {
			t.Fatalf("missing metadata mutation %s.%s in %+v", object, property, collection)
		}
		if mutation.Value != wantValue || mutation.ValueKind != wantKind {
			t.Fatalf("metadata mutation %s.%s: got value=%#v kind=%q want value=%#v kind=%q", object, property, mutation.Value, mutation.ValueKind, wantValue, wantKind)
		}
	}

	assertMetadataValue(payload.PartMutations, "Body01", "material", "S235JR-A", "string")
	assertMetadataValue(payload.PartMutations, "MountingBracket01", "part_number", "PN-1001", "string")
	assertMetadataValue(payload.PartMutations, "MountingBracket01", "revision", float64(6), "integer")
	assertMetadataValue(payload.PartMutations, "MountingBracket01", "tagged", true, "boolean")
	assertMetadataValue(payload.AssemblyMutations, "Assembly01", "assembly_count", float64(16), "number")
}

func TestPlannerTargetMetadata_RejectsTypeFailures(t *testing.T) {
	tests := []struct {
		name          string
		metadata      string
		errorContains string
	}{
		{
			name:          "string metadata with number value",
			metadata:      `target bracket: metadata material = 42`,
			errorContains: `metadata value kind "string" requires a string`,
		},
		{
			name:          "boolean metadata with string value",
			metadata:      `target mounting_bracket: metadata tagged = "yes"`,
			errorContains: `metadata value kind "boolean" requires a boolean`,
		},
		{
			name:          "integer metadata with non-integral number",
			metadata:      `target mounting_bracket: metadata revision = 1.5`,
			errorContains: `metadata value kind "integer" requires an exact integral number`,
		},
		{
			name:          "unsupported captured valueType",
			metadata:      `target mounting_bracket: metadata released_on = "2026-01-01"`,
			errorContains: `valueType "date" is not supported`,
		},
		{
			name:          "captured valueType and native kind mismatch",
			metadata:      `target mounting_bracket: metadata mismatched_kind = "abc"`,
			errorContains: `valueType "string" is incompatible with native value kind "integer"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    ` + tt.metadata + `
}
`
			err := parseValidateAndPlanCaptureBackedError(t, dslContent, plannerTargetMetadataModel(), plannerTargetMetadataContract(), true)
			if err == nil {
				t.Fatal("expected planning to fail")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %v", tt.errorContains, err)
			}
		})
	}
}

func TestPlannerTargetMetadata_RejectsCapabilityFailures(t *testing.T) {
	tests := []struct {
		name          string
		metadata      string
		errorContains string
	}{
		{
			name:          "non-writable metadata",
			metadata:      `target mounting_bracket: metadata locked_code = "L-1"`,
			errorContains: "is not writable",
		},
		{
			name:          "non-observable metadata",
			metadata:      `target mounting_bracket: metadata hidden_code = "H-1"`,
			errorContains: "is not observable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    ` + tt.metadata + `
}
`
			err := parseValidateAndPlanCaptureBackedError(t, dslContent, plannerTargetMetadataModel(), plannerTargetMetadataContract(), true)
			if err == nil {
				t.Fatal("expected planning to fail")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %v", tt.errorContains, err)
			}
		})
	}
}

func TestEvaluateProductTargetMetadata_DetectsConflicts(t *testing.T) {
	contract := plannerTargetMetadataContract()

	t.Run("same semantic target and metadata key", func(t *testing.T) {
		product := &dsl.ProductNode{
			Name: "Box",
			TargetMetadata: []*dsl.TargetMetadataNode{
				{SemanticTarget: "bracket", Key: "material", Value: literalMetadataValue("A")},
				{SemanticTarget: "bracket", Key: "material", Value: literalMetadataValue("B")},
			},
		}

		_, err := evaluateProductTargetMetadata(product, nil, nil, nil, nil, nil, plannerTargetMetadataModel(), contract)
		if err == nil {
			t.Fatal("expected duplicate semantic target/key conflict")
		}
		if !strings.Contains(err.Error(), "conflicts with target metadata") {
			t.Fatalf("expected semantic conflict error, got %v", err)
		}
	})

	t.Run("same native owner and property", func(t *testing.T) {
		model := plannerTargetMetadataModel()
		model.Components[0].ChildrenIDs = append(model.Components[0].ChildrenIDs, "cmp.bracketAlias")
		model.Components = append(model.Components, semantic.Component{
			ID:          "cmp.bracketAlias",
			Kind:        "part",
			Name:        "bracket_alias",
			DisplayName: "Bracket Alias",
			NativeRef:   "Body01",
			ParentID:    "cmp.root",
			Quantity:    1,
		})
		model.Metadata = append(model.Metadata, semantic.Metadata{
			ID:          "meta.bracketAlias.material",
			OwnerKind:   semantic.OwnerKindComponent,
			OwnerID:     "cmp.bracketAlias",
			ComponentID: "cmp.bracketAlias",
			Key:         "material",
			DisplayName: "Bracket Alias Material",
			ValueType:   "string",
			NativeType:  "App::PropertyString",
			Observable:  true,
			Writable:    true,
		})

		product := &dsl.ProductNode{
			Name: "Box",
			TargetMetadata: []*dsl.TargetMetadataNode{
				{SemanticTarget: "bracket", Key: "material", Value: literalMetadataValue("A")},
				{SemanticTarget: "bracket_alias", Key: "material", Value: literalMetadataValue("B")},
			},
		}

		_, err := evaluateProductTargetMetadata(product, nil, nil, nil, nil, nil, model, contract)
		if err == nil {
			t.Fatal("expected duplicate native owner/property conflict")
		}
		if !strings.Contains(err.Error(), "on native property 'Body01.material'") {
			t.Fatalf("expected native conflict error, got %v", err)
		}
	})
}

func TestPlannerTargetMetadata_DeterministicAndOrderIndependent(t *testing.T) {
	dslContentA := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    target bracket:
        metadata material = "S235JR"
    target mounting_bracket:
        metadata part_number = "PN-1001"
        metadata revision = 7
}
`
	dslContentB := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    target mounting_bracket:
        metadata revision = 7
        metadata part_number = "PN-1001"
    target bracket:
        metadata material = "S235JR"
}
`

	_, planA1 := parseValidateAndPlanCaptureBacked(t, dslContentA, plannerTargetMetadataModel(), plannerTargetMetadataContract())
	_, planA2 := parseValidateAndPlanCaptureBacked(t, dslContentA, plannerTargetMetadataModel(), plannerTargetMetadataContract())
	_, planB := parseValidateAndPlanCaptureBacked(t, dslContentB, plannerTargetMetadataModel(), plannerTargetMetadataContract())

	if jsonA1, jsonA2 := manifestPayloadJSONFromPlan(t, planA1), manifestPayloadJSONFromPlan(t, planA2); jsonA1 != jsonA2 {
		t.Fatalf("expected repeated target-metadata planning to be byte identical\nfirst:  %s\nsecond: %s", jsonA1, jsonA2)
	}
	if jsonA, jsonB := manifestPayloadJSONFromPlan(t, planA1), manifestPayloadJSONFromPlan(t, planB); jsonA != jsonB {
		t.Fatalf("expected source order to not affect the projected metadata ordering\nfirst:  %s\nsecond: %s", jsonA, jsonB)
	}

	hashA1, err := ComputePlanHash(planA1, nil)
	if err != nil {
		t.Fatalf("failed to hash first plan: %v", err)
	}
	hashA2, err := ComputePlanHash(planA2, nil)
	if err != nil {
		t.Fatalf("failed to hash second plan: %v", err)
	}
	hashB, err := ComputePlanHash(planB, nil)
	if err != nil {
		t.Fatalf("failed to hash reordered plan: %v", err)
	}
	if hashA1 != hashA2 || hashA1 != hashB {
		t.Fatalf("expected deterministic plan identity, got %q, %q, %q", hashA1, hashA2, hashB)
	}
}

func TestPlannerTargetMetadata_ParticipatesInPlanIdentity(t *testing.T) {
	base := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    target bracket:
        metadata material = "S235JR"
}
`
	changedValue := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    target bracket:
        metadata material = "S355"
}
`
	changedKey := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    target mounting_bracket:
        metadata part_number = "S235JR"
}
`
	changedTarget := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    target RootAssembly:
        metadata material = "S235JR"
}
`

	hashOf := func(dslContent string, model *semantic.Model) string {
		t.Helper()
		_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, model, plannerTargetMetadataContract())
		hash, err := ComputePlanHash(plan, nil)
		if err != nil {
			t.Fatalf("failed to hash plan: %v", err)
		}
		return hash
	}

	baseModel := plannerTargetMetadataModel()
	baseHash := hashOf(base, baseModel)
	if changed := hashOf(changedValue, plannerTargetMetadataModel()); changed == baseHash {
		t.Fatal("expected metadata value change to change plan identity")
	}
	if changed := hashOf(changedKey, plannerTargetMetadataModel()); changed == baseHash {
		t.Fatal("expected metadata key change to change plan identity")
	}
	if changed := hashOf(changedTarget, plannerTargetMetadataModel()); changed == baseHash {
		t.Fatal("expected metadata target change to change plan identity")
	}

	displayLabelModel := plannerTargetMetadataModel()
	for i := range displayLabelModel.Metadata {
		if displayLabelModel.Metadata[i].ID == "meta.bracket.material" {
			displayLabelModel.Metadata[i].DisplayName = "A Completely Different Label"
		}
	}
	displayLabelModel.Components[1].DisplayName = "Renamed Bracket"
	if changed := hashOf(base, displayLabelModel); changed != baseHash {
		t.Fatal("expected display labels to not change plan identity")
	}
}

func TestPlannerTargetMetadata_ImplicitParamPathProducesNoMetadataMutation(t *testing.T) {
	// A string parameter whose name equals a captured metadata key must not
	// resurrect the retired implicit param -> set_property mutation path.
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param material: string = "S235JR"
}
`

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, plannerTargetMetadataModel(), plannerTargetMetadataContract())
	payload := manifestPayloadFromPlan(t, plan)

	if payload.AssemblyMutations != nil && len(payload.AssemblyMutations.Metadata) > 0 {
		t.Fatalf("expected no implicit assembly metadata mutation, got %+v", payload.AssemblyMutations.Metadata)
	}
	if payload.PartMutations != nil && len(payload.PartMutations.Metadata) > 0 {
		t.Fatalf("expected no implicit part metadata mutation, got %+v", payload.PartMutations.Metadata)
	}
}

func TestPlannerTargetMetadata_CoexistsWithParameterAssignmentsAndTargetActions(t *testing.T) {
	dslContent := `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 12

    target bracket:
        action = suppress
        metadata material = "S235JR"
}
`

	_, plan := parseValidateAndPlanCaptureBacked(t, dslContent, plannerTargetMetadataModel(), plannerTargetMetadataContract())
	payload := manifestPayloadFromPlan(t, plan)

	if len(payload.ParameterAssignments) != 1 || payload.ParameterAssignments[0].Name != "width" {
		t.Fatalf("expected numeric parameter assignment to remain valid, got %+v", payload.ParameterAssignments)
	}
	if payload.PartMutations == nil {
		t.Fatal("expected part mutation collection")
	}
	if len(payload.PartMutations.Suppression) != 1 || payload.PartMutations.Suppression[0].Object != "Body01" {
		t.Fatalf("expected target action suppression mutation to remain valid, got %+v", payload.PartMutations.Suppression)
	}
	if findMetadataMutation(payload.PartMutations, "Body01", "material") == nil {
		t.Fatalf("expected metadata mutation to coexist with suppression, got %+v", payload.PartMutations)
	}
}
