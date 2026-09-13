package observed

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
)

func TestLinkToSemantic_SuccessfulComparabilitySortedAndDeterministic(t *testing.T) {
	observed := validObserved()
	observed.Observation.Components = []Component{
		{ID: "cmp.part", Kind: ComponentKindPart, Name: "Plate", ParentID: "cmp.root"},
		{ID: "cmp.root", Kind: ComponentKindAssembly, Name: "Widget"},
	}
	observed.Observation.Parameters = []Parameter{
		{ID: "par.zeta", Name: "Zeta", GroupID: "grp.root", Value: mustValue(t, []byte(`1`)), ValueKind: "integer"},
		{ID: "par.alpha", Name: "Alpha", Value: mustValue(t, []byte(`2`)), ValueKind: "integer"},
	}
	observed.Observation.Metadata = []Metadata{
		{ID: "meta.zeta", Key: "zeta", OwnerID: "cmp.root", Value: mustValue(t, []byte(`"z"`)), ValueKind: "string"},
		{ID: "meta.alpha", Key: "alpha", OwnerID: "feat.root", Value: mustValue(t, []byte(`"a"`)), ValueKind: "string"},
	}

	model := unsortedSemanticModel()

	first, err := LinkToSemantic(observed, model)
	if err != nil {
		t.Fatalf("LinkToSemantic(first) returned error: %v", err)
	}
	second, err := LinkToSemantic(observed, model)
	if err != nil {
		t.Fatalf("LinkToSemantic(second) returned error: %v", err)
	}

	want := &IdentityComparability{
		Components: []ObservedCaptureIdentityLink{
			{ObservedID: "cmp.part", CaptureID: "cmp.part", Kind: identityKindComponent},
			{ObservedID: "cmp.root", CaptureID: "cmp.root", Kind: identityKindComponent},
		},
		Parameters: []ObservedCaptureIdentityLink{
			{ObservedID: "par.alpha", CaptureID: "par.alpha", Kind: identityKindParameter},
			{ObservedID: "par.zeta", CaptureID: "par.zeta", Kind: identityKindParameter},
		},
		Metadata: []ObservedCaptureIdentityLink{
			{ObservedID: "meta.alpha", CaptureID: "meta.alpha", Kind: identityKindMetadata},
			{ObservedID: "meta.zeta", CaptureID: "meta.zeta", Kind: identityKindMetadata},
		},
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("unexpected comparability\nwant: %#v\ngot:  %#v", want, first)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("comparability changed across repeated calls\nfirst:  %#v\nsecond: %#v", first, second)
	}

	reordered := cloneObserved(observed)
	reordered.Observation.Components = []Component{
		observed.Observation.Components[1],
		observed.Observation.Components[0],
	}
	reordered.Observation.Parameters = []Parameter{
		observed.Observation.Parameters[1],
		observed.Observation.Parameters[0],
	}
	reordered.Observation.Metadata = []Metadata{
		observed.Observation.Metadata[1],
		observed.Observation.Metadata[0],
	}

	third, err := LinkToSemantic(reordered, differentlyOrderedSemanticModel())
	if err != nil {
		t.Fatalf("LinkToSemantic(reordered) returned error: %v", err)
	}
	if !reflect.DeepEqual(first, third) {
		t.Fatalf("comparability changed across input order variants\nfirst: %#v\nthird: %#v", first, third)
	}
}

func TestLinkToSemantic_MissingObservedIdentityFailsDeterministically(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Observed)
		want    string
		wantCat string
	}{
		{
			name: "component",
			mutate: func(observed *Observed) {
				observed.Observation.Components[0].ID = "cmp.missing"
			},
			want:    `observed component id "cmp.missing" was not found in semantic component identities`,
			wantCat: identityKindComponent,
		},
		{
			name: "parameter",
			mutate: func(observed *Observed) {
				observed.Observation.Parameters[0].ID = "par.missing"
				observed.Observation.Parameters[0].GroupID = ""
			},
			want:    `observed parameter id "par.missing" was not found in semantic parameter identities`,
			wantCat: identityKindParameter,
		},
		{
			name: "metadata",
			mutate: func(observed *Observed) {
				observed.Observation.Metadata[0].ID = "meta.missing"
				observed.Observation.Metadata[0].OwnerID = ""
			},
			want:    `observed metadata id "meta.missing" was not found in semantic metadata identities`,
			wantCat: identityKindMetadata,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed := comparableObservedFixture(t)
			tt.mutate(observed)

			first, err := LinkToSemantic(observed, unsortedSemanticModel())
			if err == nil {
				t.Fatal("expected LinkToSemantic to fail")
			}
			if !errors.Is(err, ErrIdentityComparableNotFound) {
				t.Fatalf("expected ErrIdentityComparableNotFound, got %v", err)
			}
			if first != nil {
				t.Fatalf("expected nil comparability on failure, got %#v", first)
			}

			var identityErr *IdentityError
			if !errors.As(err, &identityErr) {
				t.Fatalf("expected IdentityError, got %T", err)
			}
			if identityErr.Category != tt.wantCat {
				t.Fatalf("unexpected category: %#v", identityErr)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("unexpected error\nwant substring: %s\ngot:            %v", tt.want, err)
			}

			_, secondErr := LinkToSemantic(observed, unsortedSemanticModel())
			if secondErr == nil {
				t.Fatal("expected repeated LinkToSemantic to fail")
			}
			if err.Error() != secondErr.Error() {
				t.Fatalf("error text changed across repeated calls\nfirst:  %q\nsecond: %q", err.Error(), secondErr.Error())
			}
		})
	}
}

func TestLinkToSemantic_ParameterGroupLinkageRules(t *testing.T) {
	t.Run("empty group id accepted", func(t *testing.T) {
		observed := comparableObservedFixture(t)
		observed.Observation.Parameters[0].GroupID = ""

		comparability, err := LinkToSemantic(observed, unsortedSemanticModel())
		if err != nil {
			t.Fatalf("LinkToSemantic returned error: %v", err)
		}
		if len(comparability.Parameters) != 1 {
			t.Fatalf("unexpected parameter link count: %#v", comparability)
		}
	})

	t.Run("known group id accepted", func(t *testing.T) {
		observed := comparableObservedFixture(t)
		observed.Observation.Parameters[0].GroupID = "grp.root"

		if _, err := LinkToSemantic(observed, unsortedSemanticModel()); err != nil {
			t.Fatalf("LinkToSemantic returned error: %v", err)
		}
	})

	t.Run("unknown group id fails", func(t *testing.T) {
		observed := comparableObservedFixture(t)
		observed.Observation.Parameters[0].GroupID = "grp.missing"

		_, err := LinkToSemantic(observed, unsortedSemanticModel())
		if err == nil {
			t.Fatal("expected LinkToSemantic to fail")
		}
		if !errors.Is(err, ErrIdentityComparableNotFound) {
			t.Fatalf("expected ErrIdentityComparableNotFound, got %v", err)
		}
		want := `observed parameter "par.alpha" groupId "grp.missing" was not found in semantic parameter group identities`
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("unexpected error\nwant substring: %s\ngot:            %v", want, err)
		}
	})
}

func TestLinkToSemantic_MetadataOwnerLinkageRules(t *testing.T) {
	t.Run("empty owner id accepted", func(t *testing.T) {
		observed := comparableObservedFixture(t)
		observed.Observation.Metadata[0].OwnerID = ""

		if _, err := LinkToSemantic(observed, unsortedSemanticModel()); err != nil {
			t.Fatalf("LinkToSemantic returned error: %v", err)
		}
	})

	t.Run("known owner ids accepted", func(t *testing.T) {
		ownerIDs := []string{"cmp.root", "grp.root", "par.alpha", "meta.alpha", "feat.root"}
		for _, ownerID := range ownerIDs {
			observed := comparableObservedFixture(t)
			observed.Observation.Metadata[0].OwnerID = ownerID

			if _, err := LinkToSemantic(observed, unsortedSemanticModel()); err != nil {
				t.Fatalf("ownerId %q should be accepted, got %v", ownerID, err)
			}
		}
	})

	t.Run("unknown owner id fails", func(t *testing.T) {
		observed := comparableObservedFixture(t)
		observed.Observation.Metadata[0].OwnerID = "owner.missing"

		_, err := LinkToSemantic(observed, unsortedSemanticModel())
		if err == nil {
			t.Fatal("expected LinkToSemantic to fail")
		}
		if !errors.Is(err, ErrIdentityComparableNotFound) {
			t.Fatalf("expected ErrIdentityComparableNotFound, got %v", err)
		}
		want := `observed metadata "meta.alpha" ownerId "owner.missing" was not found in semantic owner identities`
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("unexpected error\nwant substring: %s\ngot:            %v", want, err)
		}
	})
}

func TestLinkToSemantic_DuplicateObservedIDsFailDeterministically(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Observed)
		want    string
		wantErr error
	}{
		{
			name: "components",
			mutate: func(observed *Observed) {
				observed.Observation.Components = []Component{
					{ID: "cmp.dup", Kind: ComponentKindAssembly, Name: "Root"},
					{ID: "cmp.dup", Kind: ComponentKindPart, Name: "Part", ParentID: "cmp.dup"},
				}
			},
			want:    `duplicate observed component id "cmp.dup"`,
			wantErr: ErrIdentityDuplicateObservedID,
		},
		{
			name: "parameters",
			mutate: func(observed *Observed) {
				observed.Observation.Parameters = []Parameter{
					{ID: "par.dup", Name: "Alpha", Value: mustValue(t, []byte(`1`)), ValueKind: "integer"},
					{ID: "par.dup", Name: "Beta", Value: mustValue(t, []byte(`2`)), ValueKind: "integer"},
				}
			},
			want:    `duplicate observed parameter id "par.dup"`,
			wantErr: ErrIdentityDuplicateObservedID,
		},
		{
			name: "metadata",
			mutate: func(observed *Observed) {
				observed.Observation.Metadata = []Metadata{
					{ID: "meta.dup", Key: "alpha", Value: mustValue(t, []byte(`"a"`)), ValueKind: "string"},
					{ID: "meta.dup", Key: "beta", Value: mustValue(t, []byte(`"b"`)), ValueKind: "string"},
				}
			},
			want:    `duplicate observed metadata id "meta.dup"`,
			wantErr: ErrIdentityDuplicateObservedID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed := comparableObservedFixture(t)
			tt.mutate(observed)

			_, firstErr := LinkToSemantic(observed, unsortedSemanticModel())
			if firstErr == nil {
				t.Fatal("expected LinkToSemantic to fail")
			}
			if !errors.Is(firstErr, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, firstErr)
			}
			if !strings.Contains(firstErr.Error(), tt.want) {
				t.Fatalf("unexpected error\nwant substring: %s\ngot:            %v", tt.want, firstErr)
			}

			_, secondErr := LinkToSemantic(observed, unsortedSemanticModel())
			if secondErr == nil {
				t.Fatal("expected repeated LinkToSemantic to fail")
			}
			if firstErr.Error() != secondErr.Error() {
				t.Fatalf("expected deterministic error text\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
			}
		})
	}
}

func TestLinkToSemantic_NoFallbackMatching(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Observed)
		want   string
	}{
		{
			name: "component name and case do not rescue mismatch",
			mutate: func(observed *Observed) {
				observed.Observation.Components[0] = Component{
					ID:   "CMP.ROOT",
					Kind: ComponentKindAssembly,
					Name: "Widget Root",
				}
			},
			want: `observed component id "CMP.ROOT" was not found in semantic component identities`,
		},
		{
			name: "parameter name does not rescue mismatch",
			mutate: func(observed *Observed) {
				observed.Observation.Parameters[0] = Parameter{
					ID:        "PAR.ALPHA",
					Name:      "Alpha",
					GroupID:   "grp.root",
					Value:     mustValue(t, []byte(`1`)),
					ValueKind: "integer",
				}
			},
			want: `observed parameter id "PAR.ALPHA" was not found in semantic parameter identities`,
		},
		{
			name: "metadata key and owner do not rescue mismatch",
			mutate: func(observed *Observed) {
				observed.Observation.Metadata[0] = Metadata{
					ID:        "META.ALPHA",
					Key:       "alpha",
					OwnerID:   "cmp.root",
					Value:     mustValue(t, []byte(`"widget"`)),
					ValueKind: "string",
				}
			},
			want: `observed metadata id "META.ALPHA" was not found in semantic metadata identities`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed := comparableObservedFixture(t)
			tt.mutate(observed)

			_, err := LinkToSemantic(observed, unsortedSemanticModel())
			if err == nil {
				t.Fatal("expected LinkToSemantic to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("unexpected error\nwant substring: %s\ngot:            %v", tt.want, err)
			}
		})
	}
}

func TestLinkToSemantic_NilAndInvalidInputsFail(t *testing.T) {
	_, err := LinkToSemantic(nil, unsortedSemanticModel())
	if err == nil {
		t.Fatal("expected nil observed to fail")
	}
	if !errors.Is(err, ErrIdentityInvalidObserved) {
		t.Fatalf("expected ErrIdentityInvalidObserved, got %v", err)
	}
	if !strings.Contains(err.Error(), "observed artifact must not be nil") {
		t.Fatalf("unexpected error: %v", err)
	}

	observed := comparableObservedFixture(t)
	observed.WorkingCopy.Path = "relative.FCStd"
	_, err = LinkToSemantic(observed, unsortedSemanticModel())
	if err == nil {
		t.Fatal("expected invalid observed to fail")
	}
	if !errors.Is(err, ErrIdentityInvalidObserved) {
		t.Fatalf("expected ErrIdentityInvalidObserved, got %v", err)
	}
	if !strings.Contains(err.Error(), "workingCopy.path must be an absolute path") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = LinkToSemantic(comparableObservedFixture(t), nil)
	if err == nil {
		t.Fatal("expected nil model to fail")
	}
	if !errors.Is(err, ErrIdentityInvalidSemanticModel) {
		t.Fatalf("expected ErrIdentityInvalidSemanticModel, got %v", err)
	}
	if !strings.Contains(err.Error(), "semantic model must not be nil") {
		t.Fatalf("unexpected error: %v", err)
	}

	invalidModel := unsortedSemanticModel()
	invalidModel.RootComponentID = "cmp.missing"
	_, err = LinkToSemantic(comparableObservedFixture(t), invalidModel)
	if err == nil {
		t.Fatal("expected invalid semantic model to fail")
	}
	if !errors.Is(err, ErrIdentityInvalidSemanticModel) {
		t.Fatalf("expected ErrIdentityInvalidSemanticModel, got %v", err)
	}
	if !strings.Contains(err.Error(), `rootComponentId "cmp.missing" must resolve to an existing component`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLinkToSemantic_ParameterNameMatchDoesNotRescueIDMismatch is a regression
// guard: parameter identity is determined solely by ID. A matching Name, a
// matching group, or a case-insensitive ID variant must never act as a fallback.
func TestLinkToSemantic_ParameterNameMatchDoesNotRescueIDMismatch(t *testing.T) {
	model := holeDiaSemanticModel()

	tests := []struct {
		name       string
		observedID string
		groupID    string
		wantMsg    string
	}{
		{
			name:       "different id same name no group",
			observedID: "param.other",
			groupID:    "",
			wantMsg:    `observed parameter id "param.other" was not found in semantic parameter identities`,
		},
		{
			name:       "different id same name matching group",
			observedID: "param.other",
			groupID:    "grp.root",
			wantMsg:    `observed parameter id "param.other" was not found in semantic parameter identities`,
		},
		{
			name:       "case-variant id same name no group",
			observedID: "Param.HoleDia",
			groupID:    "",
			wantMsg:    `observed parameter id "Param.HoleDia" was not found in semantic parameter identities`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed := comparableObservedFixture(t)
			observed.Observation.Parameters = []Parameter{
				{
					ID:        tt.observedID,
					Name:      "holeDia",
					GroupID:   tt.groupID,
					Value:     mustValue(t, []byte(`12`)),
					ValueKind: "integer",
				},
			}

			_, firstErr := LinkToSemantic(observed, model)
			if firstErr == nil {
				t.Fatal("expected LinkToSemantic to fail: name match must not rescue ID mismatch")
			}
			if !errors.Is(firstErr, ErrIdentityComparableNotFound) {
				t.Fatalf("expected ErrIdentityComparableNotFound, got %v", firstErr)
			}
			if !strings.Contains(firstErr.Error(), tt.wantMsg) {
				t.Fatalf("unexpected error\nwant substring: %s\ngot:            %v", tt.wantMsg, firstErr)
			}

			_, secondErr := LinkToSemantic(observed, model)
			if secondErr == nil {
				t.Fatal("expected repeated LinkToSemantic to fail")
			}
			if firstErr.Error() != secondErr.Error() {
				t.Fatalf("error text changed across repeated calls\nfirst:  %q\nsecond: %q", firstErr.Error(), secondErr.Error())
			}
		})
	}
}

func comparableObservedFixture(t *testing.T) *Observed {
	t.Helper()
	observed := validObserved()
	observed.Observation.Components = []Component{
		{ID: "cmp.root", Kind: ComponentKindAssembly, Name: "Widget Root"},
	}
	observed.Observation.Parameters = []Parameter{
		{ID: "par.alpha", Name: "Alpha", GroupID: "grp.root", Value: mustValue(t, []byte(`1`)), ValueKind: "integer"},
	}
	observed.Observation.Metadata = []Metadata{
		{ID: "meta.alpha", Key: "alpha", OwnerID: "cmp.root", Value: mustValue(t, []byte(`"widget"`)), ValueKind: "string"},
	}
	return observed
}

func unsortedSemanticModel() *semantic.Model {
	return &semantic.Model{
		SchemaVersion:           semantic.SchemaVersion,
		SourceDocumentLogicalID: "doc.widget",
		RootComponentID:         "cmp.root",
		Components: []semantic.Component{
			{
				ID:            "cmp.root",
				Kind:          "assembly",
				Name:          "WidgetRootNative",
				DisplayName:   "Widget Root",
				ChildrenIDs:   []string{"cmp.part"},
				Quantity:      1,
				Targetability: cad.Targetability{},
			},
			{
				ID:            "cmp.part",
				Kind:          "part",
				Name:          "PlateNative",
				DisplayName:   "Plate",
				ParentID:      "cmp.root",
				ChildrenIDs:   []string{},
				Quantity:      1,
				Targetability: cad.Targetability{},
			},
		},
		Features: []semantic.Feature{
			{
				ID:            "feat.root",
				ComponentID:   "cmp.root",
				Name:          "Sketch001",
				DisplayName:   "Root Sketch",
				NativeType:    "Sketch",
				Targetability: cad.Targetability{},
			},
		},
		ParameterGroups: []semantic.ParameterGroup{
			{
				ID:               "grp.root",
				OwnerComponentID: "cmp.root",
				Name:             "DimensionsNative",
				DisplayName:      "Dimensions",
				GroupKind:        "varset",
				NativeType:       "Spreadsheet",
				Observable:       true,
				Writable:         true,
			},
		},
		Parameters: []semantic.Parameter{
			{
				ID:          "par.zeta",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.root",
				ComponentID: "cmp.root",
				Name:        "ZetaNative",
				DisplayName: "Zeta",
				ValueType:   "integer",
				NativeType:  "Integer",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "par.alpha",
				OwnerKind:   semantic.OwnerKindGroup,
				OwnerID:     "grp.root",
				ComponentID: "cmp.root",
				GroupID:     "grp.root",
				Name:        "AlphaNative",
				DisplayName: "Alpha",
				ValueType:   "integer",
				NativeType:  "Integer",
				Observable:  true,
				Writable:    true,
			},
		},
		Metadata: []semantic.Metadata{
			{
				ID:          "meta.zeta",
				OwnerKind:   semantic.OwnerKindComponent,
				OwnerID:     "cmp.root",
				ComponentID: "cmp.root",
				Key:         "zeta_native",
				DisplayName: "Zeta",
				ValueType:   "string",
				NativeType:  "String",
				Observable:  true,
				Writable:    true,
			},
			{
				ID:          "meta.alpha",
				OwnerKind:   semantic.OwnerKindFeature,
				OwnerID:     "feat.root",
				ComponentID: "cmp.root",
				Key:         "alpha_native",
				DisplayName: "Alpha",
				ValueType:   "string",
				NativeType:  "String",
				Observable:  true,
				Writable:    true,
			},
		},
	}
}

// holeDiaSemanticModel returns a semantic model whose single parameter has
// ID "param.holeDia" and display name "holeDia". It is used to verify that an
// observed parameter sharing only the name cannot match via any fallback path.
func holeDiaSemanticModel() *semantic.Model {
	model := unsortedSemanticModel()
	model.Parameters = []semantic.Parameter{
		{
			ID:          "param.holeDia",
			OwnerKind:   semantic.OwnerKindGroup,
			OwnerID:     "grp.root",
			ComponentID: "cmp.root",
			GroupID:     "grp.root",
			Name:        "holeDia_native",
			DisplayName: "holeDia",
			ValueType:   "integer",
			NativeType:  "Integer",
			Observable:  true,
			Writable:    true,
		},
	}
	return model
}

func differentlyOrderedSemanticModel() *semantic.Model {
	model := unsortedSemanticModel()
	model.Components = []semantic.Component{
		model.Components[1],
		model.Components[0],
	}
	model.Parameters = []semantic.Parameter{
		model.Parameters[1],
		model.Parameters[0],
	}
	model.Metadata = []semantic.Metadata{
		model.Metadata[1],
		model.Metadata[0],
	}
	return model
}
