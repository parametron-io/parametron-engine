package semanticmap

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
)

func TestLinkCaptureIdentities_SmokeFixtureLinksAllCaptureStableIDs(t *testing.T) {
	linkage, err := LinkCaptureIdentities(loadFixture(t, "smoke/freecad-default"), smokeSemanticModel(t))
	if err != nil {
		t.Fatalf("LinkCaptureIdentities returned error: %v", err)
	}

	want := []IdentityLink{
		{EntityKind: "component", SemanticID: "cmp.root", CaptureID: "cmp.root", IdentityField: "capture.id"},
		{EntityKind: "metadata", SemanticID: "meta.root.part_number", CaptureID: "meta.root.part_number", IdentityField: "capture.id", KeyField: "capture.key"},
		{EntityKind: "parameter", SemanticID: "par.root.main.length", CaptureID: "par.root.main.length", IdentityField: "capture.id", NameField: "capture.name", GroupField: "capture.groupId"},
		{EntityKind: "parameter_group", SemanticID: "grp.root.main", CaptureID: "grp.root.main", IdentityField: "capture.id"},
	}
	if !reflect.DeepEqual(linkage.Entries, want) {
		t.Fatalf("unexpected linkage entries\nwant: %#v\ngot:  %#v", want, linkage.Entries)
	}
}

func TestLinkCaptureIdentities_DisplayNameOnlyIdentityIsRejected(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.CaptureToSemantic.Components[0].Identity = "capture.displayName"

	_, err := LinkCaptureIdentities(contract, smokeSemanticModel(t))
	if err == nil {
		t.Fatal("expected displayName identity failure")
	}
	if !errors.Is(err, ErrIdentityInvalidSemanticMap) {
		t.Fatalf("expected invalid semantic map sentinel, got %v", err)
	}
	if !strings.Contains(err.Error(), `captureToSemantic.components[0].identity must equal "capture.id"`) {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestLinkCaptureIdentities_NameOnlyIdentityIsRejected(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.CaptureToSemantic.Parameters[0].Identity = "capture.name"

	_, err := LinkCaptureIdentities(contract, smokeSemanticModel(t))
	if err == nil {
		t.Fatal("expected name identity failure")
	}
	if !errors.Is(err, ErrIdentityInvalidSemanticMap) {
		t.Fatalf("expected invalid semantic map sentinel, got %v", err)
	}
	if !strings.Contains(err.Error(), `captureToSemantic.parameters[0].identity must equal "capture.id"`) {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestLinkCaptureIdentities_OutputOrderingIsDeterministic(t *testing.T) {
	linkage, err := LinkCaptureIdentities(loadFixture(t, "smoke/freecad-default"), linkageModelUnsorted())
	if err != nil {
		t.Fatalf("LinkCaptureIdentities returned error: %v", err)
	}

	got := identityTriples(linkage.Entries)
	want := []string{
		"component|cmp.assembly|cmp.assembly",
		"component|cmp.part|cmp.part",
		"metadata|meta.a|meta.a",
		"metadata|meta.b|meta.b",
		"parameter|par.a|par.a",
		"parameter|par.b|par.b",
		"parameter_group|grp.a|grp.a",
		"parameter_group|grp.b|grp.b",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected linkage order\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestLinkCaptureIdentities_RepeatedInputProducesDeepEqualOutput(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")
	model := linkageModelUnsorted()

	first, firstErr := LinkCaptureIdentities(contract, model)
	second, secondErr := LinkCaptureIdentities(contract, model)
	if firstErr != nil || secondErr != nil {
		t.Fatalf("expected repeated success, got first=%v second=%v", firstErr, secondErr)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deep-equal linkage\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestLinkCaptureIdentities_InputOrderChangesDoNotChangeOutput(t *testing.T) {
	contract := loadFixture(t, "smoke/freecad-default")

	first, err := LinkCaptureIdentities(contract, linkageModelUnsorted())
	if err != nil {
		t.Fatalf("first LinkCaptureIdentities returned error: %v", err)
	}

	second, err := LinkCaptureIdentities(contract, linkageModelDifferentOrder())
	if err != nil {
		t.Fatalf("second LinkCaptureIdentities returned error: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected input order changes not to affect output\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestLinkCaptureIdentities_NilMappingFailsDeterministically(t *testing.T) {
	_, err := LinkCaptureIdentities(nil, smokeSemanticModel(t))
	if err == nil {
		t.Fatal("expected nil mapping failure")
	}
	if !errors.Is(err, ErrIdentityMissingSemanticMap) {
		t.Fatalf("expected missing semantic map sentinel, got %v", err)
	}
}

func TestLinkCaptureIdentities_NilModelFailsDeterministically(t *testing.T) {
	_, err := LinkCaptureIdentities(loadFixture(t, "smoke/freecad-default"), nil)
	if err == nil {
		t.Fatal("expected nil model failure")
	}
	if !errors.Is(err, ErrIdentityInvalidModel) {
		t.Fatalf("expected invalid model sentinel, got %v", err)
	}
}

func TestLinkCaptureIdentities_InvalidMappingFailsDeterministically(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.Determinism.AllowImplicitFallback = boolPtr(true)

	first, firstErr := LinkCaptureIdentities(contract, smokeSemanticModel(t))
	second, secondErr := LinkCaptureIdentities(contract, smokeSemanticModel(t))
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected repeated invalid mapping failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no linkage on invalid mapping, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrIdentityInvalidSemanticMap) {
		t.Fatalf("expected invalid semantic map sentinel, got %v", firstErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic invalid mapping text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestLinkCaptureIdentities_InvalidSemanticModelFailsDeterministically(t *testing.T) {
	model := linkageModelUnsorted()
	model.RootComponentID = "cmp.missing"

	first, firstErr := LinkCaptureIdentities(loadFixture(t, "smoke/freecad-default"), model)
	second, secondErr := LinkCaptureIdentities(loadFixture(t, "smoke/freecad-default"), model)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected repeated invalid model failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no linkage on invalid model, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrIdentityInvalidModel) {
		t.Fatalf("expected invalid model sentinel, got %v", firstErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic invalid model text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestLinkCaptureIdentities_DuplicateLinkageDetectionFailsDeterministically(t *testing.T) {
	model := linkageModelUnsorted()
	model.Components = append(model.Components, semantic.Component{
		ID:            "cmp.assembly",
		Kind:          "assembly",
		Name:          "DuplicateAssembly",
		DisplayName:   "Duplicate Assembly",
		ChildrenIDs:   []string{},
		Quantity:      1,
		Targetability: cad.Targetability{},
	})

	first, firstErr := LinkCaptureIdentities(loadFixture(t, "smoke/freecad-default"), model)
	second, secondErr := LinkCaptureIdentities(loadFixture(t, "smoke/freecad-default"), model)
	if firstErr == nil || secondErr == nil {
		t.Fatalf("expected repeated duplicate linkage failure, got first=%v second=%v", firstErr, secondErr)
	}
	if first != nil || second != nil {
		t.Fatalf("expected no linkage on duplicate failure, got first=%#v second=%#v", first, second)
	}
	if !errors.Is(firstErr, ErrIdentityDuplicate) {
		t.Fatalf("expected duplicate linkage sentinel, got %v", firstErr)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("expected deterministic duplicate linkage text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
	}
}

func TestLinkCaptureIdentities_DeterminismFlagsRemainEnforced(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SemanticMap)
		want   string
	}{
		{
			name: "implicit_fallback",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowImplicitFallback = boolPtr(true)
			},
			want: "determinism.allowImplicitFallback must be false",
		},
		{
			name: "case_insensitive_match",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowCaseInsensitiveMatch = boolPtr(true)
			},
			want: "determinism.allowCaseInsensitiveMatch must be false",
		},
		{
			name: "display_name_identity",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowDisplayNameIdentity = boolPtr(true)
			},
			want: "determinism.allowDisplayNameIdentity must be false",
		},
		{
			name: "adapter_inference",
			mutate: func(contract *SemanticMap) {
				contract.Determinism.AllowAdapterInference = boolPtr(true)
			},
			want: "determinism.allowAdapterInference must be false",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
			tc.mutate(contract)

			_, err := LinkCaptureIdentities(contract, smokeSemanticModel(t))
			if err == nil {
				t.Fatal("expected determinism enforcement failure")
			}
			if !errors.Is(err, ErrIdentityInvalidSemanticMap) {
				t.Fatalf("expected invalid semantic map sentinel, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func smokeSemanticModel(t *testing.T) *semantic.Model {
	t.Helper()

	contract, err := cad.Load(filepath.Join("..", "cad", "testdata", "smoke", "with-parameter-group", "parametron.cad.json"))
	if err != nil {
		t.Fatalf("cad.Load returned error: %v", err)
	}

	model, err := semantic.BuildFromCapture(contract)
	if err != nil {
		t.Fatalf("semantic.BuildFromCapture returned error: %v", err)
	}
	return model
}

func linkageModelUnsorted() *semantic.Model {
	return &semantic.Model{
		SchemaVersion:   semantic.SchemaVersion,
		RootComponentID: "cmp.assembly",
		Components: []semantic.Component{
			{ID: "cmp.part", Kind: "part", Name: "PartB", DisplayName: "Part B", ParentID: "cmp.assembly", ChildrenIDs: []string{}, Quantity: 1, Targetability: cad.Targetability{}},
			{ID: "cmp.assembly", Kind: "assembly", Name: "AssemblyA", DisplayName: "Assembly A", ChildrenIDs: []string{"cmp.part"}, Quantity: 1, Targetability: cad.Targetability{}},
		},
		ParameterGroups: []semantic.ParameterGroup{
			{ID: "grp.b", OwnerComponentID: "cmp.part", Name: "GroupB", DisplayName: "Group B", GroupKind: "parameter_group", NativeType: "Spreadsheet::Sheet", Observable: true, Writable: true},
			{ID: "grp.a", OwnerComponentID: "cmp.assembly", Name: "GroupA", DisplayName: "Group A", GroupKind: "parameter_group", NativeType: "Spreadsheet::Sheet", Observable: true, Writable: true},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.b", OwnerKind: semantic.OwnerKindGroup, OwnerID: "grp.b", ComponentID: "cmp.part", GroupID: "grp.b", Name: "width", DisplayName: "Width", ValueType: "number", NativeType: "Length", Unit: "mm", Observable: true, Writable: true},
			{ID: "par.a", OwnerKind: semantic.OwnerKindGroup, OwnerID: "grp.a", ComponentID: "cmp.assembly", GroupID: "grp.a", Name: "length", DisplayName: "Length", ValueType: "number", NativeType: "Length", Unit: "mm", Observable: true, Writable: true},
		},
		Metadata: []semantic.Metadata{
			{ID: "meta.b", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.part", ComponentID: "cmp.part", Key: "part_number_b", DisplayName: "Part Number B", ValueType: "string", NativeType: "App::PropertyString", Observable: true, Writable: true},
			{ID: "meta.a", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.assembly", ComponentID: "cmp.assembly", Key: "part_number_a", DisplayName: "Part Number A", ValueType: "string", NativeType: "App::PropertyString", Observable: true, Writable: true},
		},
	}
}

func linkageModelDifferentOrder() *semantic.Model {
	return &semantic.Model{
		SchemaVersion:   semantic.SchemaVersion,
		RootComponentID: "cmp.assembly",
		Components: []semantic.Component{
			{ID: "cmp.assembly", Kind: "assembly", Name: "AssemblyA", DisplayName: "Assembly A", ChildrenIDs: []string{"cmp.part"}, Quantity: 1, Targetability: cad.Targetability{}},
			{ID: "cmp.part", Kind: "part", Name: "PartB", DisplayName: "Part B", ParentID: "cmp.assembly", ChildrenIDs: []string{}, Quantity: 1, Targetability: cad.Targetability{}},
		},
		ParameterGroups: []semantic.ParameterGroup{
			{ID: "grp.a", OwnerComponentID: "cmp.assembly", Name: "GroupA", DisplayName: "Group A", GroupKind: "parameter_group", NativeType: "Spreadsheet::Sheet", Observable: true, Writable: true},
			{ID: "grp.b", OwnerComponentID: "cmp.part", Name: "GroupB", DisplayName: "Group B", GroupKind: "parameter_group", NativeType: "Spreadsheet::Sheet", Observable: true, Writable: true},
		},
		Parameters: []semantic.Parameter{
			{ID: "par.a", OwnerKind: semantic.OwnerKindGroup, OwnerID: "grp.a", ComponentID: "cmp.assembly", GroupID: "grp.a", Name: "length", DisplayName: "Length", ValueType: "number", NativeType: "Length", Unit: "mm", Observable: true, Writable: true},
			{ID: "par.b", OwnerKind: semantic.OwnerKindGroup, OwnerID: "grp.b", ComponentID: "cmp.part", GroupID: "grp.b", Name: "width", DisplayName: "Width", ValueType: "number", NativeType: "Length", Unit: "mm", Observable: true, Writable: true},
		},
		Metadata: []semantic.Metadata{
			{ID: "meta.a", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.assembly", ComponentID: "cmp.assembly", Key: "part_number_a", DisplayName: "Part Number A", ValueType: "string", NativeType: "App::PropertyString", Observable: true, Writable: true},
			{ID: "meta.b", OwnerKind: semantic.OwnerKindComponent, OwnerID: "cmp.part", ComponentID: "cmp.part", Key: "part_number_b", DisplayName: "Part Number B", ValueType: "string", NativeType: "App::PropertyString", Observable: true, Writable: true},
		},
	}
}

func identityTriples(entries []IdentityLink) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.EntityKind+"|"+entry.SemanticID+"|"+entry.CaptureID)
	}
	return out
}
