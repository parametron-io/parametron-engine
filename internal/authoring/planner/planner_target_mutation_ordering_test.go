package planner

import "testing"

// Phase 5 Task 12 permanent contract: canonicalizeRuntimeTargetMutationOrdering
// is the single Engine-owned canonicalization boundary for final Part / Assembly
// target-mutation family ordering. It sorts Suppression, Visibility, and
// Deletion each by Object ascending (boolean families tie-break false-before-
// true on equal Object), never dedups, never merges, never moves entries
// across families or across the Part/Assembly buckets it is called on, and
// never mutates its input. Parameters and Properties are untouched: they carry
// no target-mutation identity and Task 12 does not reorder them.
//
// These tests exercise canonicalizeRuntimeTargetMutationOrdering directly,
// independent of the DSL/semantic pipeline that produces its input in normal
// planner operation (that integration is covered by
// planner_target_mutation_identity_test.go).

// ---------------------------------------------------------------------------
// PART A — canonicalization helper contract
// ---------------------------------------------------------------------------

func TestCanonicalizeRuntimeTargetMutationOrdering_Nil(t *testing.T) {
	if got := canonicalizeRuntimeTargetMutationOrdering(nil); got != nil {
		t.Fatalf("nil collection: got %#v, want nil", got)
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_EmptyCollectionNoAllocation(t *testing.T) {
	collection := &ExportManifestMutationCollection{}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	if got != collection {
		t.Fatalf("empty non-nil collection with no target families must be returned unchanged (same pointer, no allocation): got %p want %p", got, collection)
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_ParametersUntouched(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Parameters:  []ExportManifestParameterMutation{{Object: "Zulu", Property: "Width", ValueParam: "w"}, {Object: "Alpha", Property: "Height", ValueParam: "h"}},
		Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	want := []ExportManifestParameterMutation{{Object: "Zulu", Property: "Width", ValueParam: "w"}, {Object: "Alpha", Property: "Height", ValueParam: "h"}}
	if len(got.Parameters) != 2 || got.Parameters[0] != want[0] || got.Parameters[1] != want[1] {
		t.Fatalf("Parameters reordered: got %#v want %#v", got.Parameters, want)
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_PropertiesUntouched(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Properties:  []ExportManifestPropertyMutation{{Object: "Zulu", Property: "Label", Value: "z"}, {Object: "Alpha", Property: "Label", Value: "a"}},
		Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	if len(got.Properties) != 2 || got.Properties[0].Object != "Zulu" || got.Properties[1].Object != "Alpha" {
		t.Fatalf("Properties reordered: got %#v", got.Properties)
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_SuppressionLexicalOrder(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{
			{Object: "Zulu", Suppressed: true},
			{Object: "Alpha", Suppressed: true},
			{Object: "Middle", Suppressed: false},
			{Object: "Beta", Suppressed: true},
		},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	want := []ExportManifestSuppressionMutation{
		{Object: "Alpha", Suppressed: true},
		{Object: "Beta", Suppressed: true},
		{Object: "Middle", Suppressed: false},
		{Object: "Zulu", Suppressed: true},
	}
	assertSuppressionOrder(t, got.Suppression, want)
}

func TestCanonicalizeRuntimeTargetMutationOrdering_SuppressionBooleanTieBreak(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{
			{Object: "Pad", Suppressed: true},
			{Object: "Pad", Suppressed: false},
			{Object: "Pad", Suppressed: true},
		},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	want := []ExportManifestSuppressionMutation{
		{Object: "Pad", Suppressed: false},
		{Object: "Pad", Suppressed: true},
		{Object: "Pad", Suppressed: true},
	}
	assertSuppressionOrder(t, got.Suppression, want)
	if len(got.Suppression) != 3 {
		t.Fatalf("duplicate count not preserved: got %d want 3", len(got.Suppression))
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_VisibilityLexicalOrder(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Visibility: []ExportManifestVisibilityMutation{
			{Object: "Zulu", Visible: true},
			{Object: "Alpha", Visible: true},
			{Object: "Middle", Visible: false},
			{Object: "Beta", Visible: true},
		},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	want := []ExportManifestVisibilityMutation{
		{Object: "Alpha", Visible: true},
		{Object: "Beta", Visible: true},
		{Object: "Middle", Visible: false},
		{Object: "Zulu", Visible: true},
	}
	assertVisibilityOrder(t, got.Visibility, want)
}

func TestCanonicalizeRuntimeTargetMutationOrdering_VisibilityBooleanTieBreak(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Visibility: []ExportManifestVisibilityMutation{
			{Object: "Body", Visible: true},
			{Object: "Body", Visible: false},
			{Object: "Body", Visible: true},
		},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	want := []ExportManifestVisibilityMutation{
		{Object: "Body", Visible: false},
		{Object: "Body", Visible: true},
		{Object: "Body", Visible: true},
	}
	assertVisibilityOrder(t, got.Visibility, want)
}

func TestCanonicalizeRuntimeTargetMutationOrdering_DeletionLexicalOrder(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Deletion: []ExportManifestDeletionMutation{
			{Object: "Zulu"}, {Object: "Alpha"}, {Object: "Middle"}, {Object: "Alpha"},
		},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	want := []ExportManifestDeletionMutation{{Object: "Alpha"}, {Object: "Alpha"}, {Object: "Middle"}, {Object: "Zulu"}}
	assertDeletionOrder(t, got.Deletion, want)
	if len(got.Deletion) != 4 {
		t.Fatalf("duplicate deletion count not preserved: got %d want 4", len(got.Deletion))
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_NoDedup(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}, {Object: "Pad", Suppressed: true}},
		Visibility:  []ExportManifestVisibilityMutation{{Object: "Body", Visible: false}, {Object: "Body", Visible: false}},
		Deletion:    []ExportManifestDeletionMutation{{Object: "Chamfer"}, {Object: "Chamfer"}},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	if len(got.Suppression) != 2 || len(got.Visibility) != 2 || len(got.Deletion) != 2 {
		t.Fatalf("duplicates were dropped: suppression=%d visibility=%d deletion=%d", len(got.Suppression), len(got.Visibility), len(got.Deletion))
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_NoMergeSameObjectAcrossFamilies(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{{Object: "Body", Suppressed: true}},
		Visibility:  []ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	if len(got.Suppression) != 1 || got.Suppression[0].Object != "Body" {
		t.Fatalf("suppression entry lost: %#v", got.Suppression)
	}
	if len(got.Visibility) != 1 || got.Visibility[0].Object != "Body" {
		t.Fatalf("visibility entry lost: %#v", got.Visibility)
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_NoCrossFamilyFlattening(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}},
		Visibility:  []ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
		Deletion:    []ExportManifestDeletionMutation{{Object: "Chamfer"}},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	if len(got.Suppression) != 1 || len(got.Visibility) != 1 || len(got.Deletion) != 1 {
		t.Fatalf("families collapsed into each other: %#v", got)
	}
	// Typed slices remain distinct Go types; a compile-time guarantee, verified
	// here by confirming each family only carries its own entries.
	if got.Suppression[0].Object != "Pad" || got.Visibility[0].Object != "Body" || got.Deletion[0].Object != "Chamfer" {
		t.Fatalf("family contents crossed boundaries: %#v", got)
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_InputNotMutated(t *testing.T) {
	original := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{{Object: "Zulu", Suppressed: true}, {Object: "Alpha", Suppressed: true}},
		Visibility:  []ExportManifestVisibilityMutation{{Object: "Zulu", Visible: true}, {Object: "Alpha", Visible: true}},
		Deletion:    []ExportManifestDeletionMutation{{Object: "Zulu"}, {Object: "Alpha"}},
	}
	originalSuppression := append([]ExportManifestSuppressionMutation(nil), original.Suppression...)
	originalVisibility := append([]ExportManifestVisibilityMutation(nil), original.Visibility...)
	originalDeletion := append([]ExportManifestDeletionMutation(nil), original.Deletion...)

	got := canonicalizeRuntimeTargetMutationOrdering(original)
	if got == original {
		t.Fatal("canonicalization must return a distinct collection when it reorders entries")
	}

	// Mutate the returned canonical slices; the original input must be unaffected.
	for i := range got.Suppression {
		got.Suppression[i].Object = "MUTATED"
	}
	for i := range got.Visibility {
		got.Visibility[i].Object = "MUTATED"
	}
	for i := range got.Deletion {
		got.Deletion[i].Object = "MUTATED"
	}

	assertSuppressionOrder(t, original.Suppression, originalSuppression)
	assertVisibilityOrder(t, original.Visibility, originalVisibility)
	assertDeletionOrder(t, original.Deletion, originalDeletion)
}

// ---------------------------------------------------------------------------
// PART B — Part / Assembly isolation
// ---------------------------------------------------------------------------

func TestCanonicalizeRuntimeTargetMutationOrdering_PartCanonicalizesIndependently(t *testing.T) {
	part := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{{Object: "Zulu", Suppressed: true}, {Object: "Alpha", Suppressed: true}},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(part)
	assertSuppressionOrder(t, got.Suppression, []ExportManifestSuppressionMutation{{Object: "Alpha", Suppressed: true}, {Object: "Zulu", Suppressed: true}})
}

func TestCanonicalizeRuntimeTargetMutationOrdering_AssemblyCanonicalizesIndependently(t *testing.T) {
	assembly := &ExportManifestMutationCollection{
		Deletion: []ExportManifestDeletionMutation{{Object: "Zulu"}, {Object: "Alpha"}},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(assembly)
	assertDeletionOrder(t, got.Deletion, []ExportManifestDeletionMutation{{Object: "Alpha"}, {Object: "Zulu"}})
}

func TestCanonicalizeRuntimeTargetMutationOrdering_NoPartToAssemblyMovement(t *testing.T) {
	part := &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{{Object: "OnlyInPart", Suppressed: true}}}
	assembly := &ExportManifestMutationCollection{Visibility: []ExportManifestVisibilityMutation{{Object: "OnlyInAssembly", Visible: false}}}

	gotPart := canonicalizeRuntimeTargetMutationOrdering(part)
	gotAssembly := canonicalizeRuntimeTargetMutationOrdering(assembly)

	if len(gotPart.Visibility) != 0 || len(gotPart.Deletion) != 0 {
		t.Fatalf("assembly family leaked into part collection: %#v", gotPart)
	}
	if len(gotAssembly.Suppression) != 0 || len(gotAssembly.Deletion) != 0 {
		t.Fatalf("part family leaked into assembly collection: %#v", gotAssembly)
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_NoAssemblyToPartMovement(t *testing.T) {
	part := &ExportManifestMutationCollection{Deletion: []ExportManifestDeletionMutation{{Object: "PartOnly"}}}
	assembly := &ExportManifestMutationCollection{Deletion: []ExportManifestDeletionMutation{{Object: "AssemblyOnly"}}}

	gotPart := canonicalizeRuntimeTargetMutationOrdering(part)
	gotAssembly := canonicalizeRuntimeTargetMutationOrdering(assembly)

	if len(gotPart.Deletion) != 1 || gotPart.Deletion[0].Object != "PartOnly" {
		t.Fatalf("part deletion collection contaminated: %#v", gotPart.Deletion)
	}
	if len(gotAssembly.Deletion) != 1 || gotAssembly.Deletion[0].Object != "AssemblyOnly" {
		t.Fatalf("assembly deletion collection contaminated: %#v", gotAssembly.Deletion)
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_SameObjectInBothBucketsRemainsInBoth(t *testing.T) {
	part := &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{{Object: "Shared", Suppressed: true}}}
	assembly := &ExportManifestMutationCollection{Visibility: []ExportManifestVisibilityMutation{{Object: "Shared", Visible: false}}}

	gotPart := canonicalizeRuntimeTargetMutationOrdering(part)
	gotAssembly := canonicalizeRuntimeTargetMutationOrdering(assembly)

	if len(gotPart.Suppression) != 1 || gotPart.Suppression[0].Object != "Shared" {
		t.Fatalf("part bucket lost the shared object: %#v", gotPart.Suppression)
	}
	if len(gotAssembly.Visibility) != 1 || gotAssembly.Visibility[0].Object != "Shared" {
		t.Fatalf("assembly bucket lost the shared object: %#v", gotAssembly.Visibility)
	}
}

// ---------------------------------------------------------------------------
// PART U — boolean total-order adversarial cases
// ---------------------------------------------------------------------------

func TestCanonicalizeRuntimeTargetMutationOrdering_SuppressionSameObjectPermutationsConverge(t *testing.T) {
	a := &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{
		{Object: "Pad", Suppressed: true}, {Object: "Pad", Suppressed: false}, {Object: "Pad", Suppressed: true},
	}}
	b := &ExportManifestMutationCollection{Suppression: []ExportManifestSuppressionMutation{
		{Object: "Pad", Suppressed: true}, {Object: "Pad", Suppressed: true}, {Object: "Pad", Suppressed: false},
	}}
	gotA := canonicalizeRuntimeTargetMutationOrdering(a)
	gotB := canonicalizeRuntimeTargetMutationOrdering(b)
	assertSuppressionOrder(t, gotA.Suppression, gotB.Suppression)
	want := []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: false}, {Object: "Pad", Suppressed: true}, {Object: "Pad", Suppressed: true}}
	assertSuppressionOrder(t, gotA.Suppression, want)
}

func TestCanonicalizeRuntimeTargetMutationOrdering_VisibilitySameObjectPermutationsConverge(t *testing.T) {
	a := &ExportManifestMutationCollection{Visibility: []ExportManifestVisibilityMutation{
		{Object: "Body", Visible: true}, {Object: "Body", Visible: false}, {Object: "Body", Visible: true},
	}}
	b := &ExportManifestMutationCollection{Visibility: []ExportManifestVisibilityMutation{
		{Object: "Body", Visible: true}, {Object: "Body", Visible: true}, {Object: "Body", Visible: false},
	}}
	gotA := canonicalizeRuntimeTargetMutationOrdering(a)
	gotB := canonicalizeRuntimeTargetMutationOrdering(b)
	assertVisibilityOrder(t, gotA.Visibility, gotB.Visibility)
}

func TestCanonicalizeRuntimeTargetMutationOrdering_DeletionDuplicatesPermutationsConverge(t *testing.T) {
	a := &ExportManifestMutationCollection{Deletion: []ExportManifestDeletionMutation{{Object: "Zulu"}, {Object: "Alpha"}, {Object: "Alpha"}}}
	b := &ExportManifestMutationCollection{Deletion: []ExportManifestDeletionMutation{{Object: "Alpha"}, {Object: "Zulu"}, {Object: "Alpha"}}}
	gotA := canonicalizeRuntimeTargetMutationOrdering(a)
	gotB := canonicalizeRuntimeTargetMutationOrdering(b)
	assertDeletionOrder(t, gotA.Deletion, gotB.Deletion)
	if len(gotA.Deletion) != 3 {
		t.Fatalf("entry dropped during convergence: %#v", gotA.Deletion)
	}
}

// ---------------------------------------------------------------------------
// PART V — nil / empty adversarial cases
// ---------------------------------------------------------------------------

func TestCanonicalizeRuntimeTargetMutationOrdering_NilTargetFamilySlicesRemainNil(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Visibility: []ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
	}
	if collection.Suppression != nil || collection.Deletion != nil {
		t.Fatal("test setup invariant broken")
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	if got.Suppression != nil {
		t.Fatalf("nil Suppression family activated by an unrelated family's canonicalization: %#v", got.Suppression)
	}
	if got.Deletion != nil {
		t.Fatalf("nil Deletion family activated by an unrelated family's canonicalization: %#v", got.Deletion)
	}
}

func TestCanonicalizeRuntimeTargetMutationOrdering_EmptyNonNilTargetFamilySlices(t *testing.T) {
	collection := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{},
		Visibility:  []ExportManifestVisibilityMutation{{Object: "Body", Visible: false}},
		Deletion:    []ExportManifestDeletionMutation{},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	if len(got.Suppression) != 0 {
		t.Fatalf("empty Suppression gained entries: %#v", got.Suppression)
	}
	if len(got.Deletion) != 0 {
		t.Fatalf("empty Deletion gained entries: %#v", got.Deletion)
	}
	if len(got.Visibility) != 1 || got.Visibility[0].Object != "Body" {
		t.Fatalf("Visibility entry lost: %#v", got.Visibility)
	}
}

// ---------------------------------------------------------------------------
// PART W — host / runtime order independence
// ---------------------------------------------------------------------------

func TestCanonicalizeRuntimeTargetMutationOrdering_GoLexicalStringOrderNoLocale(t *testing.T) {
	// "Beta-2" < "Beta_1" < "Middle" < "Zulu" < "alpha" in plain Go byte-wise
	// string comparison ('-' 0x2D < '_' 0x5F, and uppercase < lowercase), which
	// is deliberately not the locale-aware/case-insensitive order a human reader
	// might expect.
	collection := &ExportManifestMutationCollection{
		Deletion: []ExportManifestDeletionMutation{
			{Object: "Zulu"}, {Object: "alpha"}, {Object: "Alpha"}, {Object: "Middle"}, {Object: "Beta-2"}, {Object: "Beta_1"},
		},
	}
	got := canonicalizeRuntimeTargetMutationOrdering(collection)
	want := []ExportManifestDeletionMutation{
		{Object: "Alpha"}, {Object: "Beta-2"}, {Object: "Beta_1"}, {Object: "Middle"}, {Object: "Zulu"}, {Object: "alpha"},
	}
	assertDeletionOrder(t, got.Deletion, want)
}

func TestCanonicalizeRuntimeTargetMutationOrdering_RepeatedCanonicalizationIsStable(t *testing.T) {
	build := func() *ExportManifestMutationCollection {
		return &ExportManifestMutationCollection{
			Suppression: []ExportManifestSuppressionMutation{{Object: "Zulu", Suppressed: true}, {Object: "Alpha", Suppressed: false}, {Object: "Middle", Suppressed: true}},
			Visibility:  []ExportManifestVisibilityMutation{{Object: "Zulu", Visible: true}, {Object: "Alpha", Visible: false}},
			Deletion:    []ExportManifestDeletionMutation{{Object: "Zulu"}, {Object: "Alpha"}},
		}
	}
	first := canonicalizeRuntimeTargetMutationOrdering(build())
	for i := 0; i < 10; i++ {
		got := canonicalizeRuntimeTargetMutationOrdering(build())
		assertSuppressionOrder(t, got.Suppression, first.Suppression)
		assertVisibilityOrder(t, got.Visibility, first.Visibility)
		assertDeletionOrder(t, got.Deletion, first.Deletion)
	}
}

// ---------------------------------------------------------------------------
// shared assertion helpers
// ---------------------------------------------------------------------------

func assertSuppressionOrder(t *testing.T, got, want []ExportManifestSuppressionMutation) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("suppression length: got %d want %d (%#v vs %#v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("suppression[%d]: got %#v want %#v (full got=%#v want=%#v)", i, got[i], want[i], got, want)
		}
	}
}

func assertVisibilityOrder(t *testing.T, got, want []ExportManifestVisibilityMutation) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("visibility length: got %d want %d (%#v vs %#v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("visibility[%d]: got %#v want %#v (full got=%#v want=%#v)", i, got[i], want[i], got, want)
		}
	}
}

func assertDeletionOrder(t *testing.T, got, want []ExportManifestDeletionMutation) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("deletion length: got %d want %d (%#v vs %#v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("deletion[%d]: got %#v want %#v (full got=%#v want=%#v)", i, got[i], want[i], got, want)
		}
	}
}
