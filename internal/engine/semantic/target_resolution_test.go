package semantic

import (
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/cad"
)

// targetResolutionModel builds a minimal valid semantic model rooted at
// cmp.root, with the given extra components and features appended. It is the
// shared fixture builder for the Task 6 permanent Feature+Component
// exact-name target resolution regressions in this file.
func targetResolutionModel(t *testing.T, extraComponents []Component, features []Feature) *Model {
	t.Helper()

	model := &Model{
		SchemaVersion:           SchemaVersion,
		SourceDocumentLogicalID: "target_resolution_model",
		RootComponentID:         "cmp.root",
		Components: append([]Component{{
			ID:          "cmp.root",
			Kind:        "assembly",
			Name:        "RootAssembly",
			DisplayName: "Root Assembly",
			ChildrenIDs: []string{},
			Quantity:    1,
		}}, extraComponents...),
		Features: features,
	}
	if err := Validate(model); err != nil {
		t.Fatalf("targetResolutionModel: invalid fixture: %v", err)
	}
	return model
}

// 1. Unique Feature resolution.
func TestResolveSemanticTargetByExactName_UniqueFeature(t *testing.T) {
	model := targetResolutionModel(t, nil, []Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
		DisplayName: "Pad",
	}})

	got, err := ResolveSemanticTargetByExactName(model, "Pad")
	if err != nil {
		t.Fatalf("ResolveSemanticTargetByExactName returned error: %v", err)
	}
	want := ResolvedSemanticTarget{
		SemanticEntityKind: "feature",
		TargetEntityKind:   "feature",
		SemanticID:         "feat.pad",
		Scope:              "assembly",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected resolved target\nwant: %#v\ngot:  %#v", want, got)
	}
}

// 2. Unique Component resolution (part and assembly scope).
func TestResolveSemanticTargetByExactName_UniqueComponent_Part(t *testing.T) {
	model := targetResolutionModel(t, []Component{{
		ID:          "cmp.cover",
		Kind:        "part",
		Name:        "Cover",
		DisplayName: "Cover",
		ChildrenIDs: []string{},
		Quantity:    1,
	}}, nil)

	got, err := ResolveSemanticTargetByExactName(model, "Cover")
	if err != nil {
		t.Fatalf("ResolveSemanticTargetByExactName returned error: %v", err)
	}
	want := ResolvedSemanticTarget{
		SemanticEntityKind: "component",
		TargetEntityKind:   "part",
		SemanticID:         "cmp.cover",
		Scope:              "part",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected resolved target\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestResolveSemanticTargetByExactName_UniqueComponent_Assembly(t *testing.T) {
	model := targetResolutionModel(t, []Component{{
		ID:          "cmp.sub",
		Kind:        "assembly",
		Name:        "SubAssembly",
		DisplayName: "Sub Assembly",
		ChildrenIDs: []string{},
		Quantity:    1,
	}}, nil)

	got, err := ResolveSemanticTargetByExactName(model, "SubAssembly")
	if err != nil {
		t.Fatalf("ResolveSemanticTargetByExactName returned error: %v", err)
	}
	want := ResolvedSemanticTarget{
		SemanticEntityKind: "component",
		TargetEntityKind:   "assembly",
		SemanticID:         "cmp.sub",
		Scope:              "assembly",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected resolved target\nwant: %#v\ngot:  %#v", want, got)
	}
}

// 3. Zero-candidate failure.
func TestResolveSemanticTargetByExactName_Missing(t *testing.T) {
	model := targetResolutionModel(t, nil, nil)

	_, err := ResolveSemanticTargetByExactName(model, "DoesNotExist")
	if err == nil {
		t.Fatal("expected zero-candidate resolution to fail")
	}
	want := `semantic target "DoesNotExist" does not resolve to a Feature or Component by exact Name`
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}
}

// 4. Exact-case success, kept separate from the case-negative matrix below.
func TestResolveSemanticTargetByExactName_ExactCaseMatches(t *testing.T) {
	model := targetResolutionModel(t, nil, []Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})

	got, err := ResolveSemanticTargetByExactName(model, "Pad")
	if err != nil {
		t.Fatalf("ResolveSemanticTargetByExactName returned error: %v", err)
	}
	if got.SemanticID != "feat.pad" {
		t.Fatalf("expected resolved SemanticID 'feat.pad', got %q", got.SemanticID)
	}
}

// 5. Case mismatch matrix: no case folding, no whitespace normalization.
func TestResolveSemanticTargetByExactName_CaseAndWhitespaceMismatchFails(t *testing.T) {
	model := targetResolutionModel(t, nil, []Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})

	for _, name := range []string{"pad", "PAD", "Pad "} {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveSemanticTargetByExactName(model, name); err == nil {
				t.Fatalf("expected %q to fail to resolve against authored Name 'Pad'", name)
			}
		})
	}
}

// 6. DisplayName must not resolve a Feature: Name is authoritative.
func TestResolveSemanticTargetByExactName_FeatureDisplayNameNotAuthoritative(t *testing.T) {
	model := targetResolutionModel(t, nil, []Feature{{
		ID:          "feat.pocket",
		ComponentID: "cmp.root",
		Name:        "Pocket001",
		DisplayName: "Keyway",
	}})

	if _, err := ResolveSemanticTargetByExactName(model, "Keyway"); err == nil {
		t.Fatal("expected DisplayName-only match against a Feature to fail")
	}

	got, err := ResolveSemanticTargetByExactName(model, "Pocket001")
	if err != nil {
		t.Fatalf("expected exact Name match to succeed, got error: %v", err)
	}
	if got.SemanticID != "feat.pocket" {
		t.Fatalf("expected resolved SemanticID 'feat.pocket', got %q", got.SemanticID)
	}
}

// 7. DisplayName must not resolve a Component: Name is authoritative.
func TestResolveSemanticTargetByExactName_ComponentDisplayNameNotAuthoritative(t *testing.T) {
	model := targetResolutionModel(t, []Component{{
		ID:          "cmp.internal",
		Kind:        "part",
		Name:        "cmp_internal",
		DisplayName: "Cover",
		ChildrenIDs: []string{},
		Quantity:    1,
	}}, nil)

	if _, err := ResolveSemanticTargetByExactName(model, "Cover"); err == nil {
		t.Fatal("expected DisplayName-only match against a Component to fail")
	}

	got, err := ResolveSemanticTargetByExactName(model, "cmp_internal")
	if err != nil {
		t.Fatalf("expected exact Name match to succeed, got error: %v", err)
	}
	if got.SemanticID != "cmp.internal" {
		t.Fatalf("expected resolved SemanticID 'cmp.internal', got %q", got.SemanticID)
	}
}

// 8. Excluded semantic entity kinds: a same-named Parameter, Metadata entry,
// or ParameterGroup never creates a target candidate.
func TestResolveSemanticTargetByExactName_ExcludesNonTargetEntityKinds(t *testing.T) {
	base := func() *Model {
		return &Model{
			SchemaVersion:           SchemaVersion,
			SourceDocumentLogicalID: "target_resolution_model",
			RootComponentID:         "cmp.root",
			Components: []Component{{
				ID:          "cmp.root",
				Kind:        "assembly",
				Name:        "RootAssembly",
				ChildrenIDs: []string{},
				Quantity:    1,
			}},
		}
	}

	t.Run("parameter_name_collision", func(t *testing.T) {
		model := base()
		model.Parameters = []Parameter{{
			ID:          "par.pad",
			OwnerKind:   OwnerKindComponent,
			OwnerID:     "cmp.root",
			ComponentID: "cmp.root",
			Name:        "Pad",
			ValueType:   "number",
			NativeType:  "App::PropertyFloat",
			Observable:  true,
			Writable:    true,
		}}
		if err := Validate(model); err != nil {
			t.Fatalf("invalid fixture: %v", err)
		}
		if _, err := ResolveSemanticTargetByExactName(model, "Pad"); err == nil {
			t.Fatal("expected a Parameter name collision to not resolve as a target")
		}
	})

	t.Run("metadata_key_collision", func(t *testing.T) {
		model := base()
		model.Metadata = []Metadata{{
			ID:          "meta.pad",
			OwnerKind:   OwnerKindComponent,
			OwnerID:     "cmp.root",
			ComponentID: "cmp.root",
			Key:         "Pad",
			ValueType:   "string",
			NativeType:  "App::PropertyString",
			Observable:  true,
			Writable:    true,
		}}
		if err := Validate(model); err != nil {
			t.Fatalf("invalid fixture: %v", err)
		}
		if _, err := ResolveSemanticTargetByExactName(model, "Pad"); err == nil {
			t.Fatal("expected a Metadata key collision to not resolve as a target")
		}
	})

	t.Run("parameter_group_name_collision", func(t *testing.T) {
		model := base()
		model.ParameterGroups = []ParameterGroup{{
			ID:               "grp.pad",
			OwnerComponentID: "cmp.root",
			Name:             "Pad",
			GroupKind:        "varset",
			NativeType:       "Spreadsheet::Sheet",
			Observable:       true,
			Writable:         true,
		}}
		if err := Validate(model); err != nil {
			t.Fatalf("invalid fixture: %v", err)
		}
		if _, err := ResolveSemanticTargetByExactName(model, "Pad"); err == nil {
			t.Fatal("expected a ParameterGroup name collision to not resolve as a target")
		}
	})
}

// 9. Same-kind Feature ambiguity, with sorted candidate evidence.
func TestResolveSemanticTargetByExactName_SameKindFeatureAmbiguity(t *testing.T) {
	model := targetResolutionModel(t, nil, []Feature{
		{ID: "feat.b", ComponentID: "cmp.root", Name: "Pad"},
		{ID: "feat.a", ComponentID: "cmp.root", Name: "Pad"},
	})

	_, err := ResolveSemanticTargetByExactName(model, "Pad")
	if err == nil {
		t.Fatal("expected same-kind Feature ambiguity to fail")
	}
	want := `semantic target "Pad" is ambiguous across candidates [feature:feat.a feature:feat.b]`
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}
}

// 10. Same-kind Component ambiguity, with sorted candidate evidence.
func TestResolveSemanticTargetByExactName_SameKindComponentAmbiguity(t *testing.T) {
	model := targetResolutionModel(t, []Component{
		{ID: "cmp.b", Kind: "part", Name: "Cover", ChildrenIDs: []string{}, Quantity: 1},
		{ID: "cmp.a", Kind: "part", Name: "Cover", ChildrenIDs: []string{}, Quantity: 1},
	}, nil)

	_, err := ResolveSemanticTargetByExactName(model, "Cover")
	if err == nil {
		t.Fatal("expected same-kind Component ambiguity to fail")
	}
	want := `semantic target "Cover" is ambiguous across candidates [component:cmp.a component:cmp.b]`
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}
}

// 11. Cross-kind ambiguity: the highest-priority Task 6 regression. Neither
// Feature nor Component wins by kind precedence.
func TestResolveSemanticTargetByExactName_CrossKindAmbiguity_NoPrecedence(t *testing.T) {
	model := targetResolutionModel(t,
		[]Component{{ID: "cmp.keyway", Kind: "part", Name: "Keyway", ChildrenIDs: []string{}, Quantity: 1}},
		[]Feature{{ID: "feat.keyway", ComponentID: "cmp.root", Name: "Keyway"}},
	)

	_, err := ResolveSemanticTargetByExactName(model, "Keyway")
	if err == nil {
		t.Fatal("expected Feature+Component same-Name collision to be ambiguous, not resolved by kind precedence")
	}
	if !strings.Contains(err.Error(), "component:cmp.keyway") {
		t.Fatalf("expected ambiguity evidence to include the Component candidate, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "feature:feat.keyway") {
		t.Fatalf("expected ambiguity evidence to include the Feature candidate, got %q", err.Error())
	}
}

// 12. Cross-kind ambiguity diagnostic, locked byte-for-byte.
func TestResolveSemanticTargetByExactName_CrossKindAmbiguityDiagnosticExact(t *testing.T) {
	model := targetResolutionModel(t,
		[]Component{{ID: "cmp.keyway", Kind: "part", Name: "Keyway", ChildrenIDs: []string{}, Quantity: 1}},
		[]Feature{{ID: "feat.keyway", ComponentID: "cmp.root", Name: "Keyway"}},
	)

	_, err := ResolveSemanticTargetByExactName(model, "Keyway")
	if err == nil {
		t.Fatal("expected cross-kind ambiguity error")
	}
	want := `semantic target "Keyway" is ambiguous across candidates [component:cmp.keyway feature:feat.keyway]`
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}
}

// 13. Model input order determinism: diagnostics do not depend on model
// slice order, for both a cross-kind collision and a same-kind collision.
func TestResolveSemanticTargetByExactName_CrossKindModelOrderIndependent(t *testing.T) {
	forward := targetResolutionModel(t,
		[]Component{{ID: "cmp.keyway", Kind: "part", Name: "Keyway", ChildrenIDs: []string{}, Quantity: 1}},
		[]Feature{{ID: "feat.keyway", ComponentID: "cmp.root", Name: "Keyway"}},
	)

	reversed := targetResolutionModel(t, nil, nil)
	reversed.Components = []Component{
		{ID: "cmp.keyway", Kind: "part", Name: "Keyway", ChildrenIDs: []string{}, Quantity: 1},
		{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly", DisplayName: "Root Assembly", ChildrenIDs: []string{}, Quantity: 1},
	}
	reversed.Features = []Feature{{ID: "feat.keyway", ComponentID: "cmp.root", Name: "Keyway"}}
	if err := Validate(reversed); err != nil {
		t.Fatalf("invalid reversed fixture: %v", err)
	}

	_, forwardErr := ResolveSemanticTargetByExactName(forward, "Keyway")
	_, reversedErr := ResolveSemanticTargetByExactName(reversed, "Keyway")
	if forwardErr == nil || reversedErr == nil {
		t.Fatalf("expected both orderings to fail with ambiguity, got forward=%v reversed=%v", forwardErr, reversedErr)
	}
	if forwardErr.Error() != reversedErr.Error() {
		t.Fatalf("expected identical diagnostic regardless of Components slice order\nforward:  %q\nreversed: %q", forwardErr.Error(), reversedErr.Error())
	}
}

func TestResolveSemanticTargetByExactName_SameKindModelOrderIndependent(t *testing.T) {
	forward := targetResolutionModel(t, nil, []Feature{
		{ID: "feat.b", ComponentID: "cmp.root", Name: "Pad"},
		{ID: "feat.a", ComponentID: "cmp.root", Name: "Pad"},
	})
	reversed := targetResolutionModel(t, nil, []Feature{
		{ID: "feat.a", ComponentID: "cmp.root", Name: "Pad"},
		{ID: "feat.b", ComponentID: "cmp.root", Name: "Pad"},
	})

	_, forwardErr := ResolveSemanticTargetByExactName(forward, "Pad")
	_, reversedErr := ResolveSemanticTargetByExactName(reversed, "Pad")
	if forwardErr == nil || reversedErr == nil {
		t.Fatalf("expected both orderings to fail with ambiguity, got forward=%v reversed=%v", forwardErr, reversedErr)
	}
	if forwardErr.Error() != reversedErr.Error() {
		t.Fatalf("expected identical diagnostic regardless of Features slice order\nforward:  %q\nreversed: %q", forwardErr.Error(), reversedErr.Error())
	}
}

// 14. Candidate sorting determinism with three intentionally out-of-order
// IDs: sorted by SemanticEntityKind then SemanticID, not input order.
func TestResolveSemanticTargetByExactName_CandidateSortingIsIDOrdered(t *testing.T) {
	model := targetResolutionModel(t, nil, []Feature{
		{ID: "feat.z", ComponentID: "cmp.root", Name: "Pad"},
		{ID: "feat.a", ComponentID: "cmp.root", Name: "Pad"},
		{ID: "feat.m", ComponentID: "cmp.root", Name: "Pad"},
	})

	_, err := ResolveSemanticTargetByExactName(model, "Pad")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	want := `semantic target "Pad" is ambiguous across candidates [feature:feat.a feature:feat.m feature:feat.z]`
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}
}

// 15. Targetability is carried passively on a resolved Feature: it is never
// enforced by Task 6, so Suppress=false must not block resolution.
func TestResolveSemanticTargetByExactName_FeatureTargetabilityPassivelyPreserved(t *testing.T) {
	model := targetResolutionModel(t, nil, []Feature{{
		ID:          "feat.pad",
		ComponentID: "cmp.root",
		Name:        "Pad",
		Targetability: cad.Targetability{
			Suppress:   false,
			Unsuppress: true,
			Hide:       true,
			Unhide:     true,
			Delete:     false,
		},
	}})

	got, err := ResolveSemanticTargetByExactName(model, "Pad")
	if err != nil {
		t.Fatalf("expected Suppress=false to not block resolution, got error: %v", err)
	}
	want := cad.Targetability{Suppress: false, Unsuppress: true, Hide: true, Unhide: true, Delete: false}
	if got.Targetability != want {
		t.Fatalf("expected Targetability to be passively preserved\nwant: %#v\ngot:  %#v", want, got.Targetability)
	}
}

// 16. Targetability is carried passively on a resolved Component too.
func TestResolveSemanticTargetByExactName_ComponentTargetabilityPassivelyPreserved(t *testing.T) {
	model := targetResolutionModel(t, []Component{{
		ID:          "cmp.cover",
		Kind:        "part",
		Name:        "Cover",
		ChildrenIDs: []string{},
		Quantity:    1,
		Targetability: cad.Targetability{
			Suppress: false,
			Hide:     true,
		},
	}}, nil)

	got, err := ResolveSemanticTargetByExactName(model, "Cover")
	if err != nil {
		t.Fatalf("expected Suppress=false to not block resolution, got error: %v", err)
	}
	want := cad.Targetability{Suppress: false, Hide: true}
	if got.Targetability != want {
		t.Fatalf("expected Targetability to be passively preserved\nwant: %#v\ngot:  %#v", want, got.Targetability)
	}
}

// 17. The resolver must not mutate the semantic model: the original Features
// slice order is unchanged after a successful or failed resolution.
func TestResolveSemanticTargetByExactName_DoesNotMutateModel(t *testing.T) {
	model := targetResolutionModel(t, nil, []Feature{
		{ID: "feat.z", ComponentID: "cmp.root", Name: "Pad"},
		{ID: "feat.a", ComponentID: "cmp.root", Name: "Pad"},
	})
	originalOrder := []string{model.Features[0].ID, model.Features[1].ID}

	if _, err := ResolveSemanticTargetByExactName(model, "Pad"); err == nil {
		t.Fatal("expected ambiguity error")
	}

	gotOrder := []string{model.Features[0].ID, model.Features[1].ID}
	if !reflect.DeepEqual(originalOrder, gotOrder) {
		t.Fatalf("resolver mutated model Features slice order\nwant: %#v\ngot:  %#v", originalOrder, gotOrder)
	}
}

// 18. Nil model resolves deterministically to the same not-found diagnostic
// as an empty model, since resolveFeaturesByExactName/resolveComponentsByExactName
// both treat a nil model as having zero candidates.
func TestResolveSemanticTargetByExactName_NilModel(t *testing.T) {
	_, err := ResolveSemanticTargetByExactName(nil, "Pad")
	if err == nil {
		t.Fatal("expected nil model to fail deterministically")
	}
	want := `semantic target "Pad" does not resolve to a Feature or Component by exact Name`
	if err.Error() != want {
		t.Fatalf("expected exact error:\n%q\ngot:\n%q", want, err.Error())
	}
}

// Extra determinism proof: the cross-kind ambiguity diagnostic is stable
// across repeated calls against the same model, in addition to the
// external repeated `go test -run ... -count=1` determinism proof.
func TestResolveSemanticTargetByExactName_CrossKindAmbiguityDiagnosticIsRepeatable(t *testing.T) {
	model := targetResolutionModel(t,
		[]Component{{ID: "cmp.keyway", Kind: "part", Name: "Keyway", ChildrenIDs: []string{}, Quantity: 1}},
		[]Feature{{ID: "feat.keyway", ComponentID: "cmp.root", Name: "Keyway"}},
	)
	want := `semantic target "Keyway" is ambiguous across candidates [component:cmp.keyway feature:feat.keyway]`

	for i := 0; i < 10; i++ {
		_, err := ResolveSemanticTargetByExactName(model, "Keyway")
		if err == nil || err.Error() != want {
			t.Fatalf("iteration %d: expected stable diagnostic %q, got %v", i, want, err)
		}
	}
}
