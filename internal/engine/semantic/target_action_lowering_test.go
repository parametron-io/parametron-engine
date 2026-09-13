package semantic

import (
	"reflect"
	"testing"

	"parametron/internal/engine/cad"
)

// Phase 5 Task 8 permanently locks semantic.LowerTargetActionMutationIntent:
// the shared, entity-kind-agnostic mapper that lowers one already
// capability-approved canonical target action into the existing
// semantic.MutationIntent contract. The canonical mapping, encoded here as
// literal test data and never derived from production logic, is:
//
//	keep       -> no MutationIntent (nil, nil)
//	suppress   -> OperationKind "suppress"
//	unsuppress -> OperationKind "unsuppress"
//	hide       -> OperationKind "hide"
//	unhide     -> OperationKind "unhide"
//	delete     -> OperationKind "delete"
//
// The action is encoded solely by MutationIntent.OperationKind. BooleanState
// is never used: unsuppress is not "suppress + false", unhide is not
// "hide + false", and delete carries no BooleanState. TargetField is always
// empty. Resolved target metadata (TargetEntityKind, TargetSemanticID, Scope)
// is copied verbatim from the ResolvedSemanticTarget; nothing is re-resolved.
// The mapper is independent of the captured Targetability bits (Task 7 owns
// eligibility; Task 8 owns mapping) and of SemanticEntityKind / CAD-native
// type. Feature and Component share this exact mapper.

// loweringTarget builds a resolved semantic target for the Task 8 mapper. The
// captured Targetability is a caller-supplied value so the tests can prove the
// mapper ignores it.
func loweringTarget(semanticKind, targetEntityKind, id, scope string, tb cad.Targetability) ResolvedSemanticTarget {
	return ResolvedSemanticTarget{
		SemanticEntityKind: semanticKind,
		TargetEntityKind:   targetEntityKind,
		SemanticID:         id,
		Scope:              scope,
		Targetability:      tb,
	}
}

func loweringFeatureTarget(tb cad.Targetability) ResolvedSemanticTarget {
	return loweringTarget("feature", "feature", "feat.pad", "part", tb)
}

func loweringComponentTarget(tb cad.Targetability) ResolvedSemanticTarget {
	return loweringTarget("component", "part", "cmp.cover", "part", tb)
}

// canonicalLoweringMatrix is the explicit six-action contract. wantKind is the
// literal expected OperationKind; an empty wantKind means the mapper must
// return (nil, nil). Nothing here is computed from the production helper.
var canonicalLoweringMatrix = []struct {
	action   string
	wantKind string
}{
	{"keep", ""},
	{"suppress", "suppress"},
	{"unsuppress", "unsuppress"},
	{"hide", "hide"},
	{"unhide", "unhide"},
	{"delete", "delete"},
}

var mutationProducingActions = []string{"suppress", "unsuppress", "hide", "unhide", "delete"}

// 1. keep lowers to nil for Feature and Component: no MutationIntent, no error,
// no OperationKind "keep", no no-op record.
func TestLowerTargetActionMutationIntent_KeepLowersToNil(t *testing.T) {
	for name, target := range map[string]ResolvedSemanticTarget{
		"feature":   loweringFeatureTarget(cad.Targetability{}),
		"component": loweringComponentTarget(cad.Targetability{}),
	} {
		t.Run(name, func(t *testing.T) {
			intent, err := LowerTargetActionMutationIntent(target, "keep")
			if err != nil {
				t.Fatalf("keep: expected nil error, got %v", err)
			}
			if intent != nil {
				t.Fatalf("keep: expected nil MutationIntent, got %#v", intent)
			}
		})
	}
}

// 2. suppress lowers to an exact MutationIntent.
func TestLowerTargetActionMutationIntent_SuppressExactLowering(t *testing.T) {
	got, err := LowerTargetActionMutationIntent(
		loweringTarget("feature", "feature", "feat.pad", "part", cad.Targetability{}), "suppress")
	if err != nil {
		t.Fatalf("suppress: unexpected error: %v", err)
	}
	want := &MutationIntent{
		OperationKind:    "suppress",
		TargetEntityKind: "feature",
		TargetSemanticID: "feat.pad",
		TargetField:      "",
		Scope:            "part",
		ValueSource: MutationValueSource{
			Kind: "target_action",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suppress lowering mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

// 3. unsuppress lowers to OperationKind "unsuppress" with a nil BooleanState.
// The conceptual alternative "suppress" + BooleanState=false is permanently
// rejected.
func TestLowerTargetActionMutationIntent_UnsuppressIsNotSuppressPlusFalse(t *testing.T) {
	got, err := LowerTargetActionMutationIntent(loweringFeatureTarget(cad.Targetability{}), "unsuppress")
	if err != nil {
		t.Fatalf("unsuppress: unexpected error: %v", err)
	}
	if got.OperationKind != "unsuppress" {
		t.Fatalf("unsuppress: expected OperationKind \"unsuppress\", got %q", got.OperationKind)
	}
	if got.OperationKind == "suppress" {
		t.Fatal("unsuppress must not be encoded as OperationKind \"suppress\"")
	}
	if got.ValueSource.BooleanState != nil {
		t.Fatalf("unsuppress must not carry a BooleanState, got %v", *got.ValueSource.BooleanState)
	}
}

// 4. hide lowers to OperationKind "hide" with no BooleanState and no
// suppression operation.
func TestLowerTargetActionMutationIntent_HideExactLowering(t *testing.T) {
	got, err := LowerTargetActionMutationIntent(loweringFeatureTarget(cad.Targetability{}), "hide")
	if err != nil {
		t.Fatalf("hide: unexpected error: %v", err)
	}
	if got.OperationKind != "hide" {
		t.Fatalf("hide: expected OperationKind \"hide\", got %q", got.OperationKind)
	}
	if got.OperationKind == "suppress" {
		t.Fatal("hide must not lower to a suppression operation")
	}
	if got.ValueSource.BooleanState != nil {
		t.Fatalf("hide must not carry a BooleanState, got %v", *got.ValueSource.BooleanState)
	}
}

// 5. unhide lowers to OperationKind "unhide"; the conceptual alternative
// "hide" + BooleanState=false is permanently rejected.
func TestLowerTargetActionMutationIntent_UnhideIsNotHidePlusFalse(t *testing.T) {
	got, err := LowerTargetActionMutationIntent(loweringFeatureTarget(cad.Targetability{}), "unhide")
	if err != nil {
		t.Fatalf("unhide: unexpected error: %v", err)
	}
	if got.OperationKind != "unhide" {
		t.Fatalf("unhide: expected OperationKind \"unhide\", got %q", got.OperationKind)
	}
	if got.OperationKind == "hide" {
		t.Fatal("unhide must not be encoded as OperationKind \"hide\"")
	}
	if got.ValueSource.BooleanState != nil {
		t.Fatalf("unhide must not carry a BooleanState, got %v", *got.ValueSource.BooleanState)
	}
}

// 6. delete lowers to OperationKind "delete" with an empty TargetField, no
// BooleanState, and no force / cascade / dependency-policy fields (the
// MutationIntent contract has none, and Task 8 introduces none).
func TestLowerTargetActionMutationIntent_DeleteExactLowering(t *testing.T) {
	got, err := LowerTargetActionMutationIntent(loweringFeatureTarget(cad.Targetability{}), "delete")
	if err != nil {
		t.Fatalf("delete: unexpected error: %v", err)
	}
	if got.OperationKind != "delete" {
		t.Fatalf("delete: expected OperationKind \"delete\", got %q", got.OperationKind)
	}
	if got.TargetField != "" {
		t.Fatalf("delete: expected empty TargetField, got %q", got.TargetField)
	}
	if got.ValueSource.BooleanState != nil {
		t.Fatalf("delete must not carry a BooleanState, got %v", *got.ValueSource.BooleanState)
	}
}

// 7. Complete six-action matrix with literal expected OperationKinds. The
// expected strings are test data only; the production helper is never
// consulted to derive them.
func TestLowerTargetActionMutationIntent_SixActionMatrix(t *testing.T) {
	target := loweringTarget("feature", "feature", "feat.pad", "part", cad.Targetability{})
	for _, row := range canonicalLoweringMatrix {
		t.Run(row.action, func(t *testing.T) {
			got, err := LowerTargetActionMutationIntent(target, row.action)
			if err != nil {
				t.Fatalf("action %q: unexpected error: %v", row.action, err)
			}
			if row.wantKind == "" {
				if got != nil {
					t.Fatalf("action %q: expected nil MutationIntent, got %#v", row.action, got)
				}
				return
			}
			want := &MutationIntent{
				OperationKind:    row.wantKind,
				TargetEntityKind: "feature",
				TargetSemanticID: "feat.pad",
				TargetField:      "",
				Scope:            "part",
				ValueSource:      MutationValueSource{Kind: "target_action"},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("action %q lowering mismatch\nwant: %#v\ngot:  %#v", row.action, want, got)
			}
		})
	}
}

// 8. The target-action ValueSource is provenance-only: it carries the
// canonical marker Kind and nothing else. No DSL parameter name, no semantic
// parameter ID, no scalar, no boolean state.
func TestLowerTargetActionMutationIntent_ProvenanceOnlyValueSource(t *testing.T) {
	for _, action := range mutationProducingActions {
		t.Run(action, func(t *testing.T) {
			got, err := LowerTargetActionMutationIntent(loweringFeatureTarget(cad.Targetability{}), action)
			if err != nil {
				t.Fatalf("action %q: unexpected error: %v", action, err)
			}
			vs := got.ValueSource
			if vs.Kind != "target_action" {
				t.Fatalf("action %q: expected ValueSource.Kind \"target_action\", got %q", action, vs.Kind)
			}
			if vs.DSLParameterName != "" {
				t.Fatalf("action %q: expected empty DSLParameterName, got %q", action, vs.DSLParameterName)
			}
			if vs.SemanticParameterID != "" {
				t.Fatalf("action %q: expected empty SemanticParameterID, got %q", action, vs.SemanticParameterID)
			}
			if !vs.Scalar.IsZero() {
				t.Fatalf("action %q: expected zero-value Scalar, got %v", action, vs.Scalar)
			}
			if vs.BooleanState != nil {
				t.Fatalf("action %q: expected nil BooleanState, got %v", action, *vs.BooleanState)
			}
		})
	}
}

// 9. BooleanState is nil for every mutation-producing action. This is an
// explicit standalone regression so the contract is obvious from the test
// registry, independent of any deep-equal comparison.
func TestLowerTargetActionMutationIntent_BooleanStateAlwaysNil(t *testing.T) {
	for _, action := range mutationProducingActions {
		got, err := LowerTargetActionMutationIntent(loweringFeatureTarget(cad.Targetability{
			Suppress: true, Unsuppress: true, Hide: true, Unhide: true, Delete: true,
		}), action)
		if err != nil {
			t.Fatalf("action %q: unexpected error: %v", action, err)
		}
		if got.ValueSource.BooleanState != nil {
			t.Fatalf("action %q: BooleanState must be nil, got %v", action, *got.ValueSource.BooleanState)
		}
	}
}

// 10. TargetField is empty for every mutation-producing action: no
// Suppressed / Visibility / Delete field-name encoding.
func TestLowerTargetActionMutationIntent_TargetFieldAlwaysEmpty(t *testing.T) {
	for _, action := range mutationProducingActions {
		got, err := LowerTargetActionMutationIntent(loweringComponentTarget(cad.Targetability{}), action)
		if err != nil {
			t.Fatalf("action %q: unexpected error: %v", action, err)
		}
		if got.TargetField != "" {
			t.Fatalf("action %q: expected empty TargetField, got %q", action, got.TargetField)
		}
	}
}

// 11. Feature target metadata is copied exactly.
func TestLowerTargetActionMutationIntent_FeatureMetadataPreservation(t *testing.T) {
	target := loweringTarget("feature", "feature", "feat.pad", "part", cad.Targetability{})
	got, err := LowerTargetActionMutationIntent(target, "suppress")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TargetEntityKind != "feature" || got.TargetSemanticID != "feat.pad" || got.Scope != "part" {
		t.Fatalf("feature metadata not preserved: %#v", got)
	}
}

// 12. part Component target metadata is copied exactly.
func TestLowerTargetActionMutationIntent_PartComponentMetadataPreservation(t *testing.T) {
	target := loweringTarget("component", "part", "cmp.cover", "part", cad.Targetability{})
	got, err := LowerTargetActionMutationIntent(target, "hide")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &MutationIntent{
		OperationKind:    "hide",
		TargetEntityKind: "part",
		TargetSemanticID: "cmp.cover",
		TargetField:      "",
		Scope:            "part",
		ValueSource:      MutationValueSource{Kind: "target_action"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("part component lowering mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

// 13. assembly Component target metadata is copied exactly. No routing
// assertion: destination projection is Task 9+ scope.
func TestLowerTargetActionMutationIntent_AssemblyComponentMetadataPreservation(t *testing.T) {
	target := loweringTarget("component", "assembly", "cmp.root", "assembly", cad.Targetability{})
	got, err := LowerTargetActionMutationIntent(target, "delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &MutationIntent{
		OperationKind:    "delete",
		TargetEntityKind: "assembly",
		TargetSemanticID: "cmp.root",
		TargetField:      "",
		Scope:            "assembly",
		ValueSource:      MutationValueSource{Kind: "target_action"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("assembly component lowering mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

// 14. Feature / Component parity: for the same action, the OperationKind,
// TargetField, and ValueSource are identical; only the copied target identity
// metadata differs.
func TestLowerTargetActionMutationIntent_FeatureComponentParity(t *testing.T) {
	for _, action := range mutationProducingActions {
		t.Run(action, func(t *testing.T) {
			feat, err := LowerTargetActionMutationIntent(
				loweringTarget("feature", "feature", "feat.thing", "part", cad.Targetability{}), action)
			if err != nil {
				t.Fatalf("feature %q: unexpected error: %v", action, err)
			}
			comp, err := LowerTargetActionMutationIntent(
				loweringTarget("component", "part", "cmp.thing", "part", cad.Targetability{}), action)
			if err != nil {
				t.Fatalf("component %q: unexpected error: %v", action, err)
			}
			if feat.OperationKind != comp.OperationKind {
				t.Fatalf("action %q: OperationKind parity break: feature %q component %q", action, feat.OperationKind, comp.OperationKind)
			}
			if feat.TargetField != comp.TargetField {
				t.Fatalf("action %q: TargetField parity break: feature %q component %q", action, feat.TargetField, comp.TargetField)
			}
			if !reflect.DeepEqual(feat.ValueSource, comp.ValueSource) {
				t.Fatalf("action %q: ValueSource parity break\nfeature: %#v\ncomponent: %#v", action, feat.ValueSource, comp.ValueSource)
			}
			if feat.TargetSemanticID == comp.TargetSemanticID {
				t.Fatalf("action %q: expected distinct target identity metadata", action)
			}
		})
	}
}

// 15. Scope does not alter the action mapping: it only passes through.
func TestLowerTargetActionMutationIntent_ScopeDoesNotAlterMapping(t *testing.T) {
	for _, action := range mutationProducingActions {
		part, err := LowerTargetActionMutationIntent(
			loweringTarget("feature", "feature", "feat.pad", "part", cad.Targetability{}), action)
		if err != nil {
			t.Fatalf("part %q: unexpected error: %v", action, err)
		}
		assembly, err := LowerTargetActionMutationIntent(
			loweringTarget("feature", "feature", "feat.pad", "assembly", cad.Targetability{}), action)
		if err != nil {
			t.Fatalf("assembly %q: unexpected error: %v", action, err)
		}
		if part.OperationKind != assembly.OperationKind {
			t.Fatalf("action %q: scope changed OperationKind: %q vs %q", action, part.OperationKind, assembly.OperationKind)
		}
		if !reflect.DeepEqual(part.ValueSource, assembly.ValueSource) {
			t.Fatalf("action %q: scope changed ValueSource shape", action)
		}
		if part.Scope != "part" || assembly.Scope != "assembly" {
			t.Fatalf("action %q: scope not passed through: %q / %q", action, part.Scope, assembly.Scope)
		}
	}
}

// 16. TargetEntityKind does not alter the action mapping.
func TestLowerTargetActionMutationIntent_TargetEntityKindDoesNotAlterMapping(t *testing.T) {
	for _, action := range mutationProducingActions {
		var baseKind string
		for i, kind := range []string{"feature", "part", "assembly"} {
			got, err := LowerTargetActionMutationIntent(
				loweringTarget("component", kind, "sem.x", "part", cad.Targetability{}), action)
			if err != nil {
				t.Fatalf("kind %q action %q: unexpected error: %v", kind, action, err)
			}
			if i == 0 {
				baseKind = got.OperationKind
				continue
			}
			if got.OperationKind != baseKind {
				t.Fatalf("action %q: TargetEntityKind %q changed the mapping to %q (base %q)", action, kind, got.OperationKind, baseKind)
			}
			if got.TargetEntityKind != kind {
				t.Fatalf("action %q: expected copied TargetEntityKind %q, got %q", action, kind, got.TargetEntityKind)
			}
		}
	}
}

// 17. Targetability independence: two otherwise-identical resolved targets,
// one with all-false and one with all-true captured Targetability, produce an
// identical MutationIntent for the same non-keep action. Task 7 eligibility
// policy must never leak into Task 8 mapping.
func TestLowerTargetActionMutationIntent_TargetabilityIndependence(t *testing.T) {
	allFalse := cad.Targetability{}
	allTrue := cad.Targetability{Suppress: true, Unsuppress: true, Hide: true, Unhide: true, Delete: true}

	for _, action := range mutationProducingActions {
		t.Run(action, func(t *testing.T) {
			a, err := LowerTargetActionMutationIntent(loweringFeatureTarget(allFalse), action)
			if err != nil {
				t.Fatalf("all-false %q: unexpected error: %v", action, err)
			}
			b, err := LowerTargetActionMutationIntent(loweringFeatureTarget(allTrue), action)
			if err != nil {
				t.Fatalf("all-true %q: unexpected error: %v", action, err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("action %q: Targetability altered the low-level mapping\nall-false: %#v\nall-true:  %#v", action, a, b)
			}
		})
	}
}

// 18 / 19. An unsupported action is rejected: no panic, no nil success, no
// keep fallback, and the diagnostic is asserted byte-for-byte.
func TestLowerTargetActionMutationIntent_UnsupportedActionRejected(t *testing.T) {
	got, err := LowerTargetActionMutationIntent(loweringFeatureTarget(cad.Targetability{
		Suppress: true, Unsuppress: true, Hide: true, Unhide: true, Delete: true,
	}), "explode")
	if err == nil {
		t.Fatal("expected unsupported action to fail even with all Targetability bits true")
	}
	if got != nil {
		t.Fatalf("expected nil MutationIntent on failure, got %#v", got)
	}
	want := `unsupported target action "explode" for semantic mutation lowering`
	if err.Error() != want {
		t.Fatalf("expected exact diagnostic:\n%q\ngot:\n%q", want, err.Error())
	}
}

// 20. The unsupported-action diagnostic is deterministic across repeats.
func TestLowerTargetActionMutationIntent_UnsupportedActionDeterminism(t *testing.T) {
	want := `unsupported target action "explode" for semantic mutation lowering`
	for i := 0; i < 10; i++ {
		got, err := LowerTargetActionMutationIntent(loweringFeatureTarget(cad.Targetability{}), "explode")
		if err == nil || err.Error() != want {
			t.Fatalf("iteration %d: unstable diagnostic: %v", i, err)
		}
		if got != nil {
			t.Fatalf("iteration %d: expected nil MutationIntent, got %#v", i, got)
		}
	}
}

// 21. Lowering never mutates the input resolved target, on the keep path, a
// mutation-producing path, or the unsupported-action path.
func TestLowerTargetActionMutationIntent_DoesNotMutateInput(t *testing.T) {
	target := loweringFeatureTarget(cad.Targetability{Suppress: true, Hide: false})
	snapshot := target

	for _, action := range []string{"keep", "suppress", "delete", "explode"} {
		_, _ = LowerTargetActionMutationIntent(target, action)
		if target != snapshot {
			t.Fatalf("action %q mutated the input target\nwant: %#v\ngot:  %#v", action, snapshot, target)
		}
	}
}

// 22. Representative lowering is deterministic across repeats: Feature
// feat.pad + suppress yields an identical MutationIntent every time.
func TestLowerTargetActionMutationIntent_ResultDeterminism(t *testing.T) {
	target := loweringTarget("feature", "feature", "feat.pad", "part", cad.Targetability{})
	first, err := LowerTargetActionMutationIntent(target, "suppress")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 10; i++ {
		got, err := LowerTargetActionMutationIntent(target, "suppress")
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: non-deterministic MutationIntent\nfirst: %#v\ngot:   %#v", i, first, got)
		}
	}
}

// 23. A canonical target-action MutationIntent is accepted by the existing
// semantic validation path: Kind="target_action", no BooleanState, empty
// TargetField is a valid semantic mutation structure. No Task 8-specific
// validator exists.
func TestLowerTargetActionMutationIntent_CanonicalIntentPassesSemanticValidation(t *testing.T) {
	intent, err := LowerTargetActionMutationIntent(
		loweringTarget("feature", "feature", "feat.pad", "part", cad.Targetability{}), "suppress")
	if err != nil {
		t.Fatalf("lowering error: %v", err)
	}

	model := &Model{
		SchemaVersion:           SchemaVersion,
		SourceDocumentLogicalID: "task8_validation_model",
		RootComponentID:         "cmp.root",
		Components: []Component{{
			ID:          "cmp.root",
			Kind:        "assembly",
			Name:        "RootAssembly",
			ChildrenIDs: []string{},
			Quantity:    1,
		}},
		Features: []Feature{{
			ID:          "feat.pad",
			ComponentID: "cmp.root",
			Name:        "Pad",
		}},
		ProductIntents: []ProductIntent{{
			Name:                   "Demo",
			RequestedOutputFormats: []string{},
			Outputs:                []OutputIntent{},
			ExportedParameters:     []ExportedParameterIntent{},
			Mutations:              []MutationIntent{*intent},
		}},
	}
	if err := Validate(model); err != nil {
		t.Fatalf("expected canonical target-action MutationIntent to pass semantic validation, got %v", err)
	}
}
