package semantic

// Phase 5 Task 13 Stage 2 permanent resolution-matrix closure: an explicit
// NoAliasFallback regression alongside the existing Task 6 target resolution
// suite (target_resolution_test.go).
//
// semantic.Model has exactly one non-authoritative name-shaped surface per
// entity: DisplayName. That is already locked as the "label" invariant by
// TestResolveSemanticTargetByExactName_FeatureDisplayNameNotAuthoritative and
// _ComponentDisplayNameNotAuthoritative. There is no separate alias field in
// production. The closest distinct non-authoritative identity surface is the
// entity's own SemanticID (its capture identity) -- a caller might reasonably
// expect an identifier to double as an alternate lookup key, the way a
// spreadsheet or CAD property alias does. This file proves that expectation
// false: only exact Name resolves a target. SemanticID is never consulted as
// a fallback identifier, for either entity kind.

import "testing"

// 1. A Feature's own SemanticID never resolves it: only its exact Name does.
func TestResolveSemanticTargetByExactName_NoAliasFallback_Feature(t *testing.T) {
	const aliasLikeID = "feat.pad-internal-alias-7f3a"
	model := targetResolutionModel(t, nil, []Feature{{
		ID:          aliasLikeID,
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})

	if _, err := ResolveSemanticTargetByExactName(model, aliasLikeID); err == nil {
		t.Fatalf("expected the Feature's own SemanticID %q to fail as an alias lookup key", aliasLikeID)
	}

	got, err := ResolveSemanticTargetByExactName(model, "Pad")
	if err != nil {
		t.Fatalf("expected exact Name match to still succeed, got error: %v", err)
	}
	if got.SemanticID != aliasLikeID {
		t.Fatalf("expected resolved SemanticID %q, got %q", aliasLikeID, got.SemanticID)
	}
}

// 2. A Component's own SemanticID never resolves it either.
func TestResolveSemanticTargetByExactName_NoAliasFallback_Component(t *testing.T) {
	const aliasLikeID = "cmp.cover-internal-alias-9d2c"
	model := targetResolutionModel(t, []Component{{
		ID:          aliasLikeID,
		Kind:        "part",
		Name:        "Cover",
		ChildrenIDs: []string{},
		Quantity:    1,
	}}, nil)

	if _, err := ResolveSemanticTargetByExactName(model, aliasLikeID); err == nil {
		t.Fatalf("expected the Component's own SemanticID %q to fail as an alias lookup key", aliasLikeID)
	}

	got, err := ResolveSemanticTargetByExactName(model, "Cover")
	if err != nil {
		t.Fatalf("expected exact Name match to still succeed, got error: %v", err)
	}
	if got.SemanticID != aliasLikeID {
		t.Fatalf("expected resolved SemanticID %q, got %q", aliasLikeID, got.SemanticID)
	}
}

// 3. NoAliasFallback produces a deterministic not-found diagnostic, proving
// no fallback candidate is generated at all (not merely that this specific ID
// string happens to be unused as a Name elsewhere).
func TestResolveSemanticTargetByExactName_NoAliasFallback_DeterministicNotFound(t *testing.T) {
	const aliasLikeID = "feat.alias-id-only"
	model := targetResolutionModel(t, nil, []Feature{{
		ID:          aliasLikeID,
		ComponentID: "cmp.root",
		Name:        "Pad",
	}})

	_, err := ResolveSemanticTargetByExactName(model, aliasLikeID)
	if err == nil {
		t.Fatal("expected zero-candidate resolution by SemanticID")
	}
	want := `semantic target "feat.alias-id-only" does not resolve to a Feature or Component by exact Name`
	if err.Error() != want {
		t.Fatalf("expected exact deterministic not-found diagnostic:\n%q\ngot:\n%q", want, err.Error())
	}
}
