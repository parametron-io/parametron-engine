package planner

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/engine/semantic"
	"parametron/internal/engine/semanticmap"
	"parametron/internal/engine/table"
)

// Phase 5 Task 12 permanent identity contract: keep is identical to
// declaration omission after semantic validation; suppress / unsuppress /
// hide / unhide / delete all participate distinctly in Engine-owned identity
// (mutation payload, serialized plan JSON, and plan hash); equivalent
// declaration-order permutations converge to the same canonical Part and
// Assembly mutation collections and therefore the same plan JSON and plan
// hash; canonicalization sits strictly downstream of the file_pattern draft
// hash seed and table-backed action selection remains deterministic.
//
// These tests drive the real capture-backed planner entry point
// (parseValidateAndPlanCaptureBacked / CreatePlanWithTablesAndSemanticModel)
// using the committed Task 11 fixture model (runtimeMutationManifestModel,
// runtimeMutationManifestProduct) from planner_runtime_mutation_manifest_test.go:
//
//   Part scope   (cmp.leg):  Pad, Slot
//   Assembly scope (cmp.root): Rail, Chamfer, Latch
//
// Task 8/9/10 private authored-order preservation itself is already locked by
// existing permanent tests (TestLowerResolvedTargetActions_AuthoredOrderWithKeepFiltering,
// TestPlannerTargetRouting_MixedFeatureComponentProductPreservesAuthoredOrder,
// TestRouteTargetMutations_MixedFeatureComponentOrdering,
// TestProjectTargetMutationRouting_MixedPartFamilyOrdering); this file adds the
// Task-12-specific boundary proof that those same authored orders converge to
// one canonical order only at the final planner surface.

// parseValidateAndPlanCaptureBackedWithTables extends
// parseValidateAndPlanCaptureBacked (planner_test.go) with real table
// evaluation support, which that helper intentionally omits (it always
// passes a nil table map). It does not change parseValidateAndPlanCaptureBacked's
// existing contract; it is an additive local helper for the table-backed
// determinism tests in PART M below.
func parseValidateAndPlanCaptureBackedWithTables(t *testing.T, dslContent string, model *semantic.Model, contract *semanticmap.SemanticMap, tables map[string]*table.Table) *ExecutionPlan {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "planner_capture_backed_tables_*.dsl")
	if err != nil {
		t.Fatalf("failed to create temp DSL: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(dslContent))); err != nil {
		t.Fatalf("failed to write temp DSL: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp DSL: %v", err)
	}

	ast, err := dsl.Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	if err := dsl.ValidateWithTables(ast, tables); err != nil {
		t.Fatalf("failed to validate DSL with tables: %v", err)
	}

	intentModel, err := semantic.InjectDSLIntent(semantic.Clone(model), ast)
	if err != nil {
		t.Fatalf("failed to inject DSL intent: %v", err)
	}

	plan, err := CreatePlanWithTablesAndSemanticModel(ast, map[string]string{}, tables, intentModel, contract)
	if err != nil {
		t.Fatalf("failed to create capture-backed plan with tables: %v", err)
	}
	return plan
}

// mutationIdentityPlan builds a full capture-backed plan for the given
// product body against the shared Task 11 fixture model.
func mutationIdentityPlan(t *testing.T, body string) *ExecutionPlan {
	t.Helper()
	_, plan := parseValidateAndPlanCaptureBacked(t, runtimeMutationManifestProduct(body), runtimeMutationManifestModel(), testProjectionContract())
	return plan
}

func mutationIdentityPlanJSON(t *testing.T, plan *ExecutionPlan) []byte {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	return data
}

func mutationIdentityPlanHash(t *testing.T, plan *ExecutionPlan) string {
	t.Helper()
	hash, err := ComputePlanHash(plan, nil)
	if err != nil {
		t.Fatalf("compute plan hash: %v", err)
	}
	return hash
}

// ---------------------------------------------------------------------------
// PART E / F — keep == declaration omission, no artificial identity
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_KeepEqualsOmission(t *testing.T) {
	omitted := mutationIdentityPlan(t, "")
	kept := mutationIdentityPlan(t, "    target Pad: action = keep")

	omittedPayload := manifestPayloadFromPlan(t, omitted)
	keptPayload := manifestPayloadFromPlan(t, kept)

	if !reflect.DeepEqual(omittedPayload.AssemblyMutations, keptPayload.AssemblyMutations) {
		t.Fatalf("AssemblyMutations differ: omitted=%#v kept=%#v", omittedPayload.AssemblyMutations, keptPayload.AssemblyMutations)
	}
	if !reflect.DeepEqual(omittedPayload.PartMutations, keptPayload.PartMutations) {
		t.Fatalf("PartMutations differ: omitted=%#v kept=%#v", omittedPayload.PartMutations, keptPayload.PartMutations)
	}
	if omittedPayload.SchemaVersion != "1.0" || keptPayload.SchemaVersion != omittedPayload.SchemaVersion {
		t.Fatalf("schemaVersion mismatch: omitted=%q kept=%q", omittedPayload.SchemaVersion, keptPayload.SchemaVersion)
	}
	if !reflect.DeepEqual(omitted, kept) {
		t.Fatalf("ExecutionPlan differs between omission and keep:\nomitted=%#v\nkept=%#v", omitted, kept)
	}

	omittedJSON := mutationIdentityPlanJSON(t, omitted)
	keptJSON := mutationIdentityPlanJSON(t, kept)
	if string(omittedJSON) != string(keptJSON) {
		t.Fatalf("plan JSON differs between omission and keep:\nomitted=%s\nkept=%s", omittedJSON, keptJSON)
	}

	omittedHash := mutationIdentityPlanHash(t, omitted)
	keptHash := mutationIdentityPlanHash(t, kept)
	if omittedHash != keptHash {
		t.Fatalf("plan hash differs between omission and keep: omitted=%q kept=%q", omittedHash, keptHash)
	}
}

func TestTargetMutationIdentity_KeepEqualsOmissionDeterministic10x(t *testing.T) {
	firstOmitted := mutationIdentityPlanHash(t, mutationIdentityPlan(t, ""))
	firstKept := mutationIdentityPlanHash(t, mutationIdentityPlan(t, "    target Pad: action = keep"))
	if firstOmitted != firstKept {
		t.Fatalf("baseline keep/omission hash mismatch: omitted=%q kept=%q", firstOmitted, firstKept)
	}
	for i := 0; i < 10; i++ {
		omittedHash := mutationIdentityPlanHash(t, mutationIdentityPlan(t, ""))
		keptHash := mutationIdentityPlanHash(t, mutationIdentityPlan(t, "    target Pad: action = keep"))
		if omittedHash != firstOmitted {
			t.Fatalf("iteration %d: omission hash drifted: got %q want %q", i, omittedHash, firstOmitted)
		}
		if keptHash != firstKept {
			t.Fatalf("iteration %d: keep hash drifted: got %q want %q", i, keptHash, firstKept)
		}
	}
}

func TestTargetMutationIdentity_KeepDoesNotCreateArtificialIdentity(t *testing.T) {
	// The shared fixture model's "width" parameter carries its own mature
	// (pre-Task-8) Parameters-family mutation metadata independent of any
	// target-action declaration -- that metadata is identical whether Pad is
	// declared keep or omitted entirely (proven by
	// TestTargetMutationIdentity_KeepEqualsOmission). What "keep produces no
	// artificial identity" means at the Task 12 boundary is narrower: keep
	// must not itself introduce a runtime target-mutation family
	// (Suppression / Visibility / Deletion) or flip schema selection to 2.0.
	payload := manifestPayloadFromPlan(t, mutationIdentityPlan(t, "    target Pad: action = keep"))
	if hasRuntimeTargetMutations(payload.PartMutations) {
		t.Fatalf("keep produced a Part runtime target-mutation family: %#v", payload.PartMutations)
	}
	if hasRuntimeTargetMutations(payload.AssemblyMutations) {
		t.Fatalf("keep produced an Assembly runtime target-mutation family: %#v", payload.AssemblyMutations)
	}
	if payload.SchemaVersion != "1.0" {
		t.Fatalf("keep activated schema 2.0: %q", payload.SchemaVersion)
	}
	raw := mutationIdentityPlanJSON(t, mutationIdentityPlan(t, "    target Pad: action = keep"))
	for _, forbidden := range []string{`"suppression"`, `"visibility"`, `"deletion"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("keep-only plan JSON unexpectedly contains %s: %s", forbidden, raw)
		}
	}
}

// ---------------------------------------------------------------------------
// PART G / H — mutation actions affect identity; opposite-state distinction
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_EachActionDiffersFromOmission(t *testing.T) {
	omitted := mutationIdentityPlan(t, "")
	omittedPayload := manifestPayloadFromPlan(t, omitted)
	omittedJSON := mutationIdentityPlanJSON(t, omitted)
	omittedHash := mutationIdentityPlanHash(t, omitted)

	actions := []string{"suppress", "unsuppress", "hide", "unhide", "delete"}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			plan := mutationIdentityPlan(t, "    target Pad: action = "+action)
			payload := manifestPayloadFromPlan(t, plan)
			if reflect.DeepEqual(payload.PartMutations, omittedPayload.PartMutations) {
				t.Fatalf("%s mutation payload equals omission: %#v", action, payload.PartMutations)
			}
			planJSON := mutationIdentityPlanJSON(t, plan)
			if string(planJSON) == string(omittedJSON) {
				t.Fatalf("%s plan JSON equals omission", action)
			}
			hash := mutationIdentityPlanHash(t, plan)
			if hash == omittedHash {
				t.Fatalf("%s plan hash equals omission: %q", action, hash)
			}
		})
	}
}

func TestTargetMutationIdentity_OppositeStateDistinction(t *testing.T) {
	cases := []struct{ name, a, b string }{
		{"SuppressVsUnsuppress", "suppress", "unsuppress"},
		{"HideVsUnhide", "hide", "unhide"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			planA := mutationIdentityPlan(t, "    target Pad: action = "+tc.a)
			planB := mutationIdentityPlan(t, "    target Pad: action = "+tc.b)
			payloadA := manifestPayloadFromPlan(t, planA)
			payloadB := manifestPayloadFromPlan(t, planB)
			if reflect.DeepEqual(payloadA.PartMutations, payloadB.PartMutations) {
				t.Fatalf("%s vs %s produced identical mutation payload: %#v", tc.a, tc.b, payloadA.PartMutations)
			}
			if string(mutationIdentityPlanJSON(t, planA)) == string(mutationIdentityPlanJSON(t, planB)) {
				t.Fatalf("%s vs %s produced identical plan JSON", tc.a, tc.b)
			}
			if mutationIdentityPlanHash(t, planA) == mutationIdentityPlanHash(t, planB) {
				t.Fatalf("%s vs %s produced identical plan hash", tc.a, tc.b)
			}
		})
	}
}

func TestTargetMutationIdentity_SuppressionVsVisibilityVsDeletionDistinction(t *testing.T) {
	suppressPlan := mutationIdentityPlan(t, "    target Pad: action = suppress")
	hidePlan := mutationIdentityPlan(t, "    target Pad: action = hide")
	deletePlan := mutationIdentityPlan(t, "    target Pad: action = delete")

	suppressPayload := manifestPayloadFromPlan(t, suppressPlan)
	hidePayload := manifestPayloadFromPlan(t, hidePlan)
	deletePayload := manifestPayloadFromPlan(t, deletePlan)

	if reflect.DeepEqual(suppressPayload.PartMutations, hidePayload.PartMutations) {
		t.Fatal("suppress and hide on the same Object collapsed into the same identity")
	}
	if reflect.DeepEqual(suppressPayload.PartMutations, deletePayload.PartMutations) {
		t.Fatal("suppress and delete on the same Object collapsed into the same identity")
	}
	if reflect.DeepEqual(hidePayload.PartMutations, deletePayload.PartMutations) {
		t.Fatal("hide and delete on the same Object collapsed into the same identity")
	}

	hashes := map[string]string{
		"suppress": mutationIdentityPlanHash(t, suppressPlan),
		"hide":     mutationIdentityPlanHash(t, hidePlan),
		"delete":   mutationIdentityPlanHash(t, deletePlan),
	}
	if hashes["suppress"] == hashes["hide"] || hashes["suppress"] == hashes["delete"] || hashes["hide"] == hashes["delete"] {
		t.Fatalf("family-distinct actions produced colliding plan hashes: %#v", hashes)
	}
}

// ---------------------------------------------------------------------------
// PART C / D — private-seam boundary: authored order converges only at the
// final planner surface, downstream of the Task 11 merge.
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_FinalPlannerSurfaceCanonicalizesAuthoredOrder(t *testing.T) {
	// Pad, Slot are the two Part-scope features of the shared fixture model;
	// declaring Slot before Pad is deliberately non-lexical authored order.
	plan := mutationIdentityPlan(t, "    target Slot: action = suppress\n    target Pad: action = suppress")
	payload := manifestPayloadFromPlan(t, plan)
	if payload.PartMutations == nil || len(payload.PartMutations.Suppression) != 2 {
		t.Fatalf("expected two Part suppression entries, got %#v", payload.PartMutations)
	}
	want := []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}, {Object: "Slot", Suppressed: true}}
	assertSuppressionOrder(t, payload.PartMutations.Suppression, want)
}

func TestTargetMutationIdentity_Task11MergeHelperRemainsMatureFirstNotSorted(t *testing.T) {
	// The Task 11 merge helper itself (mergeExportManifestMutationCollections)
	// is base-then-routed (mature-first) and performs no sort; only the final
	// planner surface (buildExportManifestIntent, via
	// canonicalizeRuntimeTargetMutationOrdering) reorders the merged result.
	base := &ExportManifestMutationCollection{
		Suppression: []ExportManifestSuppressionMutation{{Object: "Zulu", Suppressed: true}},
	}
	routed := &semanticmap.ProjectedTargetMutationRouting{
		Part: &semanticmap.ManifestMutationCollection{
			Suppression: []semanticmap.ManifestSuppressionMutation{{Object: "Alpha", Suppressed: true}},
		},
	}
	merged := mergeExportManifestMutationCollections(base, routed, false)
	// Mature-first, unsorted: Zulu (mature) then Alpha (routed) -- NOT
	// lexically ordered. Confirms the merge helper does not itself sort.
	wantMerged := []ExportManifestSuppressionMutation{{Object: "Zulu", Suppressed: true}, {Object: "Alpha", Suppressed: true}}
	assertSuppressionOrder(t, merged.Suppression, wantMerged)

	// The final planner surface may reorder that same merged result canonically.
	canonical := canonicalizeRuntimeTargetMutationOrdering(merged)
	wantCanonical := []ExportManifestSuppressionMutation{{Object: "Alpha", Suppressed: true}, {Object: "Zulu", Suppressed: true}}
	assertSuppressionOrder(t, canonical.Suppression, wantCanonical)
}

// ---------------------------------------------------------------------------
// PART I / J / K — declaration-order permutation equivalence
// ---------------------------------------------------------------------------

func assertMutationIdentityEquivalentPermutations(t *testing.T, bodyA, bodyB string) {
	t.Helper()
	planA := mutationIdentityPlan(t, bodyA)
	planB := mutationIdentityPlan(t, bodyB)

	payloadA := manifestPayloadFromPlan(t, planA)
	payloadB := manifestPayloadFromPlan(t, planB)
	if !reflect.DeepEqual(payloadA.PartMutations, payloadB.PartMutations) {
		t.Fatalf("Part mutation collections diverge across permutations:\nA=%#v\nB=%#v", payloadA.PartMutations, payloadB.PartMutations)
	}
	if !reflect.DeepEqual(payloadA.AssemblyMutations, payloadB.AssemblyMutations) {
		t.Fatalf("Assembly mutation collections diverge across permutations:\nA=%#v\nB=%#v", payloadA.AssemblyMutations, payloadB.AssemblyMutations)
	}

	jsonA := mutationIdentityPlanJSON(t, planA)
	jsonB := mutationIdentityPlanJSON(t, planB)
	if string(jsonA) != string(jsonB) {
		t.Fatalf("plan JSON diverges across permutations:\nA=%s\nB=%s", jsonA, jsonB)
	}

	hashA := mutationIdentityPlanHash(t, planA)
	hashB := mutationIdentityPlanHash(t, planB)
	if hashA != hashB {
		t.Fatalf("plan hash diverges across permutations: A=%q B=%q", hashA, hashB)
	}
}

func TestTargetMutationIdentity_PartSuppressionPermutation(t *testing.T) {
	assertMutationIdentityEquivalentPermutations(t,
		"    target Slot: action = suppress\n    target Pad: action = suppress",
		"    target Pad: action = suppress\n    target Slot: action = suppress",
	)
}

func TestTargetMutationIdentity_PartVisibilityPermutation(t *testing.T) {
	assertMutationIdentityEquivalentPermutations(t,
		"    target Slot: action = hide\n    target Pad: action = hide",
		"    target Pad: action = hide\n    target Slot: action = hide",
	)
}

func TestTargetMutationIdentity_PartDeletionPermutation(t *testing.T) {
	assertMutationIdentityEquivalentPermutations(t,
		"    target Slot: action = delete\n    target Pad: action = delete",
		"    target Pad: action = delete\n    target Slot: action = delete",
	)
}

func TestTargetMutationIdentity_AssemblyPermutation(t *testing.T) {
	// Rail, Chamfer, Latch are the three Assembly-scope (cmp.root-owned)
	// features of the shared fixture model.
	assertMutationIdentityEquivalentPermutations(t,
		"    target Rail: action = suppress\n    target Chamfer: action = suppress\n    target Latch: action = suppress",
		"    target Latch: action = suppress\n    target Rail: action = suppress\n    target Chamfer: action = suppress",
	)
	plan := mutationIdentityPlan(t, "    target Latch: action = suppress\n    target Rail: action = suppress\n    target Chamfer: action = suppress")
	payload := manifestPayloadFromPlan(t, plan)
	want := []ExportManifestSuppressionMutation{
		{Object: "Chamfer", Suppressed: true}, {Object: "Latch", Suppressed: true}, {Object: "Rail", Suppressed: true},
	}
	assertSuppressionOrder(t, payload.AssemblyMutations.Suppression, want)
}

func TestTargetMutationIdentity_MixedFamilyPermutation(t *testing.T) {
	// Multiset: Pad suppress, Slot hide, Chamfer delete, Rail unsuppress, Latch unhide.
	orderA := "    target Pad: action = suppress\n" +
		"    target Slot: action = hide\n" +
		"    target Chamfer: action = delete\n" +
		"    target Rail: action = unsuppress\n" +
		"    target Latch: action = unhide"
	orderB := "    target Latch: action = unhide\n" +
		"    target Rail: action = unsuppress\n" +
		"    target Chamfer: action = delete\n" +
		"    target Slot: action = hide\n" +
		"    target Pad: action = suppress"
	assertMutationIdentityEquivalentPermutations(t, orderA, orderB)

	plan := mutationIdentityPlan(t, orderA)
	payload := manifestPayloadFromPlan(t, plan)
	assertSuppressionOrder(t, payload.PartMutations.Suppression, []ExportManifestSuppressionMutation{{Object: "Pad", Suppressed: true}})
	assertVisibilityOrder(t, payload.PartMutations.Visibility, []ExportManifestVisibilityMutation{{Object: "Slot", Visible: false}})
	assertSuppressionOrder(t, payload.AssemblyMutations.Suppression, []ExportManifestSuppressionMutation{{Object: "Rail", Suppressed: false}})
	assertVisibilityOrder(t, payload.AssemblyMutations.Visibility, []ExportManifestVisibilityMutation{{Object: "Latch", Visible: true}})
	assertDeletionOrder(t, payload.AssemblyMutations.Deletion, []ExportManifestDeletionMutation{{Object: "Chamfer"}})
}

func TestTargetMutationIdentity_MixedFeatureComponentPermutation(t *testing.T) {
	// Leg is a Part-kind Component (not a Feature) of the shared fixture
	// model; Rail is an Assembly-scope Feature. No global Part/Assembly sort:
	// each bucket canonicalizes independently regardless of declaration order
	// or whether the entry originated from a Feature or a Component target.
	assertMutationIdentityEquivalentPermutations(t,
		"    target Leg: action = delete\n    target Rail: action = delete",
		"    target Rail: action = delete\n    target Leg: action = delete",
	)
	plan := mutationIdentityPlan(t, "    target Rail: action = delete\n    target Leg: action = delete")
	payload := manifestPayloadFromPlan(t, plan)
	assertDeletionOrder(t, payload.PartMutations.Deletion, []ExportManifestDeletionMutation{{Object: "Leg"}})
	assertDeletionOrder(t, payload.AssemblyMutations.Deletion, []ExportManifestDeletionMutation{{Object: "Rail"}})
}

// ---------------------------------------------------------------------------
// PART L — file-pattern hash determinism (canonicalization precedes the
// draft/seed hash used for file_pattern interpolation)
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_FilePatternHashDeterminism(t *testing.T) {
	product := func(body string) string {
		return `
profile Prod {
    file_pattern = "{product}_{plan_hash}"
}
use profile Prod

product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 12
` + body + `
}
`
	}
	orderA := "    target Pad: action = suppress\n    target Chamfer: action = delete"
	orderB := "    target Chamfer: action = delete\n    target Pad: action = suppress"

	_, planA := parseValidateAndPlanCaptureBacked(t, product(orderA), runtimeMutationManifestModel(), testProjectionContract())
	_, planB := parseValidateAndPlanCaptureBacked(t, product(orderB), runtimeMutationManifestModel(), testProjectionContract())

	csvA, ok := planA.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload, got %T", planA.Steps[0].Payload)
	}
	csvB, ok := planB.Steps[0].Payload.(WriteCSVPayload)
	if !ok {
		t.Fatalf("expected WriteCSVPayload, got %T", planB.Steps[0].Payload)
	}
	if csvA.Filename != csvB.Filename {
		t.Fatalf("plan-hash-derived output filenames diverge across permutations: A=%q B=%q", csvA.Filename, csvB.Filename)
	}

	payloadA := manifestPayloadFromPlan(t, planA)
	payloadB := manifestPayloadFromPlan(t, planB)
	if payloadA.PlanHash == "" {
		t.Fatal("expected non-empty plan hash on the manifest payload")
	}
	if payloadA.PlanHash != payloadB.PlanHash {
		t.Fatalf("final plan hash diverges across permutations: A=%q B=%q", payloadA.PlanHash, payloadB.PlanHash)
	}
	if string(mutationIdentityPlanJSON(t, planA)) != string(mutationIdentityPlanJSON(t, planB)) {
		t.Fatal("final plan JSON diverges across permutations despite equal draft/final hashing")
	}
}

// ---------------------------------------------------------------------------
// PART M — table-backed action determinism
// ---------------------------------------------------------------------------

func tableBackedMutationIdentityPlan(t *testing.T, variant string) *ExecutionPlan {
	t.Helper()
	body := `
    param variant: string = "` + variant + `"
    target Chamfer: action = table_cell("variants", variant, "chamferAction")
`
	return parseValidateAndPlanCaptureBackedWithTables(t, runtimeMutationManifestProduct(body), runtimeMutationManifestModel(), testProjectionContract(), actionPlannerTestTables())
}

func TestTargetMutationIdentity_TableBackedSelectedActionDeterministic10x(t *testing.T) {
	first := tableBackedMutationIdentityPlan(t, "A") // "A" -> suppress
	firstPayload := manifestPayloadFromPlan(t, first)
	firstJSON := mutationIdentityPlanJSON(t, first)
	firstHash := mutationIdentityPlanHash(t, first)

	if firstPayload.AssemblyMutations == nil || len(firstPayload.AssemblyMutations.Suppression) != 1 || firstPayload.AssemblyMutations.Suppression[0].Object != "Chamfer" {
		t.Fatalf("expected Chamfer suppression from table row A, got %#v", firstPayload.AssemblyMutations)
	}

	for i := 0; i < 10; i++ {
		plan := tableBackedMutationIdentityPlan(t, "A")
		payload := manifestPayloadFromPlan(t, plan)
		if !reflect.DeepEqual(payload.AssemblyMutations, firstPayload.AssemblyMutations) {
			t.Fatalf("iteration %d: mutation payload drifted: %#v", i, payload.AssemblyMutations)
		}
		if string(mutationIdentityPlanJSON(t, plan)) != string(firstJSON) {
			t.Fatalf("iteration %d: plan JSON drifted", i)
		}
		if mutationIdentityPlanHash(t, plan) != firstHash {
			t.Fatalf("iteration %d: plan hash drifted", i)
		}
	}
}

func TestTargetMutationIdentity_ChangedTableSelectedActionChangesIdentity(t *testing.T) {
	suppressPlan := tableBackedMutationIdentityPlan(t, "A") // suppress
	hidePlan := tableBackedMutationIdentityPlan(t, "B")     // hide

	suppressPayload := manifestPayloadFromPlan(t, suppressPlan)
	hidePayload := manifestPayloadFromPlan(t, hidePlan)
	if reflect.DeepEqual(suppressPayload.AssemblyMutations, hidePayload.AssemblyMutations) {
		t.Fatal("changing the table-selected action did not change the mutation payload")
	}
	if mutationIdentityPlanHash(t, suppressPlan) == mutationIdentityPlanHash(t, hidePlan) {
		t.Fatal("changing the table-selected action did not change the plan hash")
	}
}

// ---------------------------------------------------------------------------
// PART N — source-form boundary: mutation identity contains no source-form
// salt. Two syntactically different action expressions that resolve to the
// same canonical action produce the same final target-mutation payload.
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_SourceFormBoundary(t *testing.T) {
	literalPlan := mutationIdentityPlan(t, "    target Pad: action = suppress")
	ternaryPlan := mutationIdentityPlan(t, "    target Pad: action = true ? suppress : hide")

	literalPayload := manifestPayloadFromPlan(t, literalPlan)
	ternaryPayload := manifestPayloadFromPlan(t, ternaryPlan)
	if !reflect.DeepEqual(literalPayload.PartMutations, ternaryPayload.PartMutations) {
		t.Fatalf("equivalent action expressions produced different mutation identity: literal=%#v ternary=%#v", literalPayload.PartMutations, ternaryPayload.PartMutations)
	}
}

// ---------------------------------------------------------------------------
// PART O / P — plan JSON canonicality and plan hash
// ---------------------------------------------------------------------------

func TestTargetMutationIdentity_PlanJSONItselfIsCanonical(t *testing.T) {
	plan := mutationIdentityPlan(t, "    target Slot: action = hide\n    target Pad: action = hide")
	payload := manifestPayloadFromPlan(t, plan)

	// The canonical order is visible on the decoded Go structure directly
	// (not only after re-hashing), proving the plan itself -- not a
	// hash-only side channel -- carries the canonical order.
	assertVisibilityOrder(t, payload.PartMutations.Visibility, []ExportManifestVisibilityMutation{{Object: "Pad", Visible: false}, {Object: "Slot", Visible: false}})

	// And the same order is visible directly in the serialized bytes.
	raw := string(mutationIdentityPlanJSON(t, plan))
	padIdx := strings.Index(raw, `"Pad"`)
	slotIdx := strings.Index(raw, `"Slot"`)
	if padIdx < 0 || slotIdx < 0 || padIdx > slotIdx {
		t.Fatalf("canonical order not visible in plan JSON bytes: %s", raw)
	}
}

func TestTargetMutationIdentity_PlanHashChangesWithMutation(t *testing.T) {
	base := mutationIdentityPlanHash(t, mutationIdentityPlan(t, "    target Pad: action = suppress"))
	changed := mutationIdentityPlanHash(t, mutationIdentityPlan(t, "    target Pad: action = hide"))
	if base == changed {
		t.Fatal("plan hash did not change when the mutation family changed")
	}
}

func TestTargetMutationIdentity_PlanHashRepeatedEquivalentPlans10x(t *testing.T) {
	orderA := "    target Slot: action = suppress\n    target Pad: action = suppress"
	orderB := "    target Pad: action = suppress\n    target Slot: action = suppress"
	first := mutationIdentityPlanHash(t, mutationIdentityPlan(t, orderA))
	for i := 0; i < 10; i++ {
		gotA := mutationIdentityPlanHash(t, mutationIdentityPlan(t, orderA))
		gotB := mutationIdentityPlanHash(t, mutationIdentityPlan(t, orderB))
		if gotA != first || gotB != first {
			t.Fatalf("iteration %d: plan hash drifted: A=%q B=%q want %q", i, gotA, gotB, first)
		}
	}
}
