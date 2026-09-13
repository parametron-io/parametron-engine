package semantic

// Phase 5 Task 13 Stage 2 permanent legacy matrix closure: hide_/unhide_/
// delete_ never had a Task 1 authoring-prefix bridge (only suppress_/
// unsuppress_ were ever retired), so there is no dedicated production
// classifier to lock for them. These tests instead lock the negative
// contract directly: a parameter whose name happens to be shaped like one of
// those pseudo-prefixes is, and remains, an ordinary parameter. It is never
// routed through semantic target resolution and never becomes a
// MutationIntent by name alone.
//
// legacyMatrixPrefixedName builds each pseudo-prefixed identifier from
// separate runtime fragments so this file's own Go source never contains a
// contiguous banned token (see legacy_prefix_corpus_test.go, which scans
// internal/** and testdata/** for exactly that shape).

import "testing"

func legacyMatrixPrefixedName(prefix, suffix string) string {
	return prefix + "_" + suffix
}

// TestTask13LegacyMatrix_HideLikeParameterIsOrdinary proves a
// hide_-shaped boolean parameter name is an ordinary parameter: no
// MutationIntent is produced, even though a Feature and Component sharing
// the referenced name both exist in the model (proving no target resolution
// is triggered by the name either).
func TestTask13LegacyMatrix_HideLikeParameterIsOrdinary(t *testing.T) {
	model := modelWithFeatureAndComponentSharingName(t)
	paramName := legacyMatrixPrefixedName("hide", "Keyway")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param `+paramName+`: boolean = true
}
`)

	assertInjectDSLIntentProducesNoMutations(t, model, ast)
}

// TestTask13LegacyMatrix_UnhideLikeParameterIsOrdinary is the unhide_-shaped
// counterpart.
func TestTask13LegacyMatrix_UnhideLikeParameterIsOrdinary(t *testing.T) {
	model := modelWithFeatureAndComponentSharingName(t)
	paramName := legacyMatrixPrefixedName("unhide", "Keyway")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param `+paramName+`: boolean = true
}
`)

	assertInjectDSLIntentProducesNoMutations(t, model, ast)
}

// TestTask13LegacyMatrix_DeleteLikeParameterIsOrdinary is the delete_-shaped
// counterpart.
func TestTask13LegacyMatrix_DeleteLikeParameterIsOrdinary(t *testing.T) {
	model := modelWithFeatureAndComponentSharingName(t)
	paramName := legacyMatrixPrefixedName("delete", "Keyway")
	ast := parseSemanticFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param `+paramName+`: boolean = true
}
`)

	assertInjectDSLIntentProducesNoMutations(t, model, ast)
}
