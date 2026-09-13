package semantic

import (
	"strings"
	"testing"

	"parametron/internal/engine/cad"
)

// Phase 5 Task 7 permanently locks semantic.ValidateTargetActionCapability:
// the shared action-capability gate that maps one canonical authored action to
// the captured Targetability bit that must be true for a resolved Feature or
// Component. The canonical contract, encoded explicitly by these tests and
// never derived from production logic, is:
//
//	keep       -> target existence only (no capability bit)
//	suppress   -> Targetability.Suppress
//	unsuppress -> Targetability.Unsuppress
//	hide       -> Targetability.Hide
//	unhide     -> Targetability.Unhide
//	delete     -> Targetability.Delete
//
// Feature and Component share this exact gate. Scope, TargetEntityKind, and
// SemanticEntityKind never influence the decision (SemanticEntityKind appears
// in diagnostics only). There is no CAD-kind policy and no semantic-map
// per-target capability override.

// featureCapabilityTarget builds a resolved Feature target carrying tb. The
// SemanticID and part scope are fixed so diagnostic assertions are stable.
func featureCapabilityTarget(tb cad.Targetability) ResolvedSemanticTarget {
	return ResolvedSemanticTarget{
		SemanticEntityKind: "feature",
		TargetEntityKind:   "feature",
		SemanticID:         "feat.pad",
		Scope:              "part",
		Targetability:      tb,
	}
}

// componentCapabilityTarget builds a resolved Component target carrying tb,
// using a part TargetEntityKind and part scope.
func componentCapabilityTarget(tb cad.Targetability) ResolvedSemanticTarget {
	return ResolvedSemanticTarget{
		SemanticEntityKind: "component",
		TargetEntityKind:   "part",
		SemanticID:         "cmp.cover",
		Scope:              "part",
		Targetability:      tb,
	}
}

// canonicalCapabilityMatrixRows is the explicit allow/deny contract for the
// five capability-bearing actions plus keep. Every row states the action, the
// exact Targetability value, and whether an error is wanted. Nothing here is
// computed from production code.
var canonicalCapabilityMatrixRows = []struct {
	name    string
	action  string
	tb      cad.Targetability
	wantErr bool
}{
	{"keepAllFalse", "keep", cad.Targetability{}, false},

	{"suppressBitTrue", "suppress", cad.Targetability{Suppress: true}, false},
	{"suppressBitFalse", "suppress", cad.Targetability{}, true},

	{"unsuppressBitTrue", "unsuppress", cad.Targetability{Unsuppress: true}, false},
	{"unsuppressBitFalse", "unsuppress", cad.Targetability{}, true},

	{"hideBitTrue", "hide", cad.Targetability{Hide: true}, false},
	{"hideBitFalse", "hide", cad.Targetability{}, true},

	{"unhideBitTrue", "unhide", cad.Targetability{Unhide: true}, false},
	{"unhideBitFalse", "unhide", cad.Targetability{}, true},

	{"deleteBitTrue", "delete", cad.Targetability{Delete: true}, false},
	{"deleteBitFalse", "delete", cad.Targetability{}, true},
}

func runCanonicalCapabilityMatrix(t *testing.T, makeTarget func(cad.Targetability) ResolvedSemanticTarget) {
	t.Helper()
	for _, row := range canonicalCapabilityMatrixRows {
		t.Run(row.name, func(t *testing.T) {
			err := ValidateTargetActionCapability(makeTarget(row.tb), row.action)
			if row.wantErr && err == nil {
				t.Fatalf("action %q with %#v: expected capability error, got nil", row.action, row.tb)
			}
			if !row.wantErr && err != nil {
				t.Fatalf("action %q with %#v: expected no error, got %v", row.action, row.tb, err)
			}
		})
	}
}

// 1. keep with zero-value Targetability passes for both entity kinds.
func TestValidateTargetActionCapability_KeepZeroValueTargetability(t *testing.T) {
	if err := ValidateTargetActionCapability(featureCapabilityTarget(cad.Targetability{}), "keep"); err != nil {
		t.Fatalf("feature keep with zero Targetability: expected nil, got %v", err)
	}
	if err := ValidateTargetActionCapability(componentCapabilityTarget(cad.Targetability{}), "keep"); err != nil {
		t.Fatalf("component keep with zero Targetability: expected nil, got %v", err)
	}
}

// 2. Feature allow/deny matrix across keep and all five capability actions.
func TestValidateTargetActionCapability_FeatureMatrix(t *testing.T) {
	runCanonicalCapabilityMatrix(t, featureCapabilityTarget)
}

// 3. Component allow/deny matrix: identical canonical contract to the Feature
// matrix. The part-kind component covers TargetEntityKind=part / Scope=part; a
// separate probe repeats the matrix against assembly metadata.
func TestValidateTargetActionCapability_ComponentMatrix(t *testing.T) {
	runCanonicalCapabilityMatrix(t, componentCapabilityTarget)

	t.Run("assembly_component_parity", func(t *testing.T) {
		runCanonicalCapabilityMatrix(t, func(tb cad.Targetability) ResolvedSemanticTarget {
			return ResolvedSemanticTarget{
				SemanticEntityKind: "component",
				TargetEntityKind:   "assembly",
				SemanticID:         "cmp.sub",
				Scope:              "assembly",
				Targetability:      tb,
			}
		})
	})
}

// 4. suppress / unsuppress are independent bits: neither implies the other.
func TestValidateTargetActionCapability_SuppressUnsuppressIndependence(t *testing.T) {
	caseA := cad.Targetability{Suppress: false, Unsuppress: true}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseA), "suppress"); err == nil {
		t.Fatal("caseA: expected suppress to fail when Suppress=false")
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseA), "unsuppress"); err != nil {
		t.Fatalf("caseA: expected unsuppress to pass when Unsuppress=true, got %v", err)
	}

	caseB := cad.Targetability{Suppress: true, Unsuppress: false}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseB), "suppress"); err != nil {
		t.Fatalf("caseB: expected suppress to pass when Suppress=true, got %v", err)
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseB), "unsuppress"); err == nil {
		t.Fatal("caseB: expected unsuppress to fail when Unsuppress=false")
	}
}

// 5. hide / unhide are independent bits.
func TestValidateTargetActionCapability_HideUnhideIndependence(t *testing.T) {
	caseA := cad.Targetability{Hide: false, Unhide: true}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseA), "hide"); err == nil {
		t.Fatal("caseA: expected hide to fail when Hide=false")
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseA), "unhide"); err != nil {
		t.Fatalf("caseA: expected unhide to pass when Unhide=true, got %v", err)
	}

	caseB := cad.Targetability{Hide: true, Unhide: false}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseB), "hide"); err != nil {
		t.Fatalf("caseB: expected hide to pass when Hide=true, got %v", err)
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseB), "unhide"); err == nil {
		t.Fatal("caseB: expected unhide to fail when Unhide=false")
	}
}

// 6. Suppression and visibility are independent: suppress != hide.
func TestValidateTargetActionCapability_SuppressHideIndependence(t *testing.T) {
	caseA := cad.Targetability{Suppress: true, Hide: false}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseA), "suppress"); err != nil {
		t.Fatalf("caseA: expected suppress to pass, got %v", err)
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseA), "hide"); err == nil {
		t.Fatal("caseA: expected hide to fail when Hide=false")
	}

	caseB := cad.Targetability{Suppress: false, Hide: true}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseB), "suppress"); err == nil {
		t.Fatal("caseB: expected suppress to fail when Suppress=false")
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseB), "hide"); err != nil {
		t.Fatalf("caseB: expected hide to pass, got %v", err)
	}
}

// 7. unhide != unsuppress.
func TestValidateTargetActionCapability_UnsuppressUnhideIndependence(t *testing.T) {
	caseA := cad.Targetability{Unsuppress: true, Unhide: false}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseA), "unsuppress"); err != nil {
		t.Fatalf("caseA: expected unsuppress to pass, got %v", err)
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseA), "unhide"); err == nil {
		t.Fatal("caseA: expected unhide to fail when Unhide=false")
	}

	caseB := cad.Targetability{Unsuppress: false, Unhide: true}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseB), "unsuppress"); err == nil {
		t.Fatal("caseB: expected unsuppress to fail when Unsuppress=false")
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(caseB), "unhide"); err != nil {
		t.Fatalf("caseB: expected unhide to pass, got %v", err)
	}
}

// 8. delete is authorized only by Targetability.Delete: no other bit grants it,
// and Delete alone is sufficient.
func TestValidateTargetActionCapability_DeleteIsolation(t *testing.T) {
	everythingButDelete := cad.Targetability{
		Suppress:   true,
		Unsuppress: true,
		Hide:       true,
		Unhide:     true,
		Delete:     false,
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(everythingButDelete), "delete"); err == nil {
		t.Fatal("expected delete to fail when Delete=false regardless of other bits")
	}

	onlyDelete := cad.Targetability{Delete: true}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(onlyDelete), "delete"); err != nil {
		t.Fatalf("expected delete to pass when Delete=true and all other bits false, got %v", err)
	}
}

// 9. Zero-value Targetability: keep passes, every capability action fails, for
// both Feature and Component.
func TestValidateTargetActionCapability_ZeroValueCompleteMatrix(t *testing.T) {
	cases := map[string]func(cad.Targetability) ResolvedSemanticTarget{
		"feature":   featureCapabilityTarget,
		"component": componentCapabilityTarget,
	}
	for kind, makeTarget := range cases {
		t.Run(kind, func(t *testing.T) {
			target := makeTarget(cad.Targetability{})
			if err := ValidateTargetActionCapability(target, "keep"); err != nil {
				t.Fatalf("keep: expected nil, got %v", err)
			}
			for _, action := range []string{"suppress", "unsuppress", "hide", "unhide", "delete"} {
				if err := ValidateTargetActionCapability(target, action); err == nil {
					t.Fatalf("%s: expected capability error under zero-value Targetability", action)
				}
			}
		})
	}
}

// 10. All-true Targetability: every canonical action passes.
func TestValidateTargetActionCapability_AllTrueCompleteMatrix(t *testing.T) {
	allTrue := cad.Targetability{
		Suppress:   true,
		Unsuppress: true,
		Hide:       true,
		Unhide:     true,
		Delete:     true,
	}
	for _, action := range []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"} {
		if err := ValidateTargetActionCapability(featureCapabilityTarget(allTrue), action); err != nil {
			t.Fatalf("feature %s: expected pass under all-true Targetability, got %v", action, err)
		}
		if err := ValidateTargetActionCapability(componentCapabilityTarget(allTrue), action); err != nil {
			t.Fatalf("component %s: expected pass under all-true Targetability, got %v", action, err)
		}
	}
}

// 11. Feature / Component parity across every canonical action, including mixed
// Targetability where some actions pass and some fail.
func TestValidateTargetActionCapability_FeatureComponentParity(t *testing.T) {
	mixes := []cad.Targetability{
		{},
		{Suppress: true, Unhide: true},
		{Hide: true, Delete: true},
		{Suppress: true, Unsuppress: true, Hide: true, Unhide: true, Delete: true},
		{Unsuppress: true},
	}
	actions := []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"}

	for _, tb := range mixes {
		for _, action := range actions {
			featErr := ValidateTargetActionCapability(featureCapabilityTarget(tb), action)
			compErr := ValidateTargetActionCapability(componentCapabilityTarget(tb), action)
			if (featErr == nil) != (compErr == nil) {
				t.Fatalf("parity break for action %q with %#v: feature err=%v component err=%v", action, tb, featErr, compErr)
			}
		}
	}
}

// 12. Scope must not affect the capability decision.
func TestValidateTargetActionCapability_ScopeDoesNotAffectDecision(t *testing.T) {
	tb := cad.Targetability{Suppress: true, Hide: false}
	for _, action := range []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"} {
		part := ResolvedSemanticTarget{SemanticEntityKind: "feature", TargetEntityKind: "feature", SemanticID: "feat.pad", Scope: "part", Targetability: tb}
		assembly := ResolvedSemanticTarget{SemanticEntityKind: "feature", TargetEntityKind: "feature", SemanticID: "feat.pad", Scope: "assembly", Targetability: tb}
		if (ValidateTargetActionCapability(part, action) == nil) != (ValidateTargetActionCapability(assembly, action) == nil) {
			t.Fatalf("scope changed the decision for action %q", action)
		}
	}
}

// 13. TargetEntityKind must not affect the capability decision.
func TestValidateTargetActionCapability_TargetEntityKindDoesNotAffectDecision(t *testing.T) {
	tb := cad.Targetability{Suppress: true, Delete: false}
	for _, action := range []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"} {
		var base error
		for i, kind := range []string{"feature", "part", "assembly"} {
			target := ResolvedSemanticTarget{SemanticEntityKind: "component", TargetEntityKind: kind, SemanticID: "cmp.x", Scope: "part", Targetability: tb}
			err := ValidateTargetActionCapability(target, action)
			if i == 0 {
				base = err
				continue
			}
			if (base == nil) != (err == nil) {
				t.Fatalf("TargetEntityKind %q changed the decision for action %q", kind, action)
			}
		}
	}
}

// 14. SemanticEntityKind must not drive policy: identical bits produce the same
// decision for feature and component. It may appear in diagnostics only.
func TestValidateTargetActionCapability_SemanticEntityKindDoesNotDrivePolicy(t *testing.T) {
	tb := cad.Targetability{Hide: true}
	for _, action := range []string{"keep", "suppress", "unsuppress", "hide", "unhide", "delete"} {
		feature := ResolvedSemanticTarget{SemanticEntityKind: "feature", TargetEntityKind: "feature", SemanticID: "x", Scope: "part", Targetability: tb}
		component := ResolvedSemanticTarget{SemanticEntityKind: "component", TargetEntityKind: "feature", SemanticID: "x", Scope: "part", Targetability: tb}
		if (ValidateTargetActionCapability(feature, action) == nil) != (ValidateTargetActionCapability(component, action) == nil) {
			t.Fatalf("SemanticEntityKind changed the decision for action %q", action)
		}
	}
}

// 15. An unsupported action is rejected deterministically: it does not pass as
// keep, does not map to a capability bit, and does not panic.
func TestValidateTargetActionCapability_UnsupportedActionRejected(t *testing.T) {
	allTrue := cad.Targetability{Suppress: true, Unsuppress: true, Hide: true, Unhide: true, Delete: true}
	err := ValidateTargetActionCapability(featureCapabilityTarget(allTrue), "explode")
	if err == nil {
		t.Fatal("expected unsupported action to fail even with all Targetability bits true")
	}
	want := `unsupported target action "explode" for capability validation`
	if err.Error() != want {
		t.Fatalf("expected exact diagnostic:\n%q\ngot:\n%q", want, err.Error())
	}
}

// 16. Exact capability denial diagnostic, asserted byte-for-byte, with the
// required context: action, the required Targetability field, the semantic
// entity kind, and the semantic ID.
func TestValidateTargetActionCapability_ExactDenialDiagnostic(t *testing.T) {
	target := ResolvedSemanticTarget{
		SemanticEntityKind: "feature",
		TargetEntityKind:   "feature",
		SemanticID:         "feat.pad",
		Scope:              "part",
		Targetability:      cad.Targetability{Suppress: false},
	}
	err := ValidateTargetActionCapability(target, "suppress")
	if err == nil {
		t.Fatal("expected suppress denial")
	}
	want := `action "suppress" requires captured Targetability.Suppress=true for semantic feature "feat.pad"`
	if err.Error() != want {
		t.Fatalf("expected exact diagnostic:\n%q\ngot:\n%q", want, err.Error())
	}
	for _, fragment := range []string{`"suppress"`, "Targetability.Suppress=true", "feature", `"feat.pad"`} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("expected diagnostic to contain %q, got %q", fragment, err.Error())
		}
	}
}

// 17. Each denied action names its own Targetability field and no other field:
// this catches copy/paste bugs (e.g. unsuppress reading Suppress) immediately.
func TestValidateTargetActionCapability_DenialNamesCorrectField(t *testing.T) {
	rows := []struct {
		action string
		field  string
	}{
		{"suppress", "Targetability.Suppress"},
		{"unsuppress", "Targetability.Unsuppress"},
		{"hide", "Targetability.Hide"},
		{"unhide", "Targetability.Unhide"},
		{"delete", "Targetability.Delete"},
	}
	allFields := []string{
		"Targetability.Suppress",
		"Targetability.Unsuppress",
		"Targetability.Hide",
		"Targetability.Unhide",
		"Targetability.Delete",
	}

	for _, row := range rows {
		t.Run(row.action, func(t *testing.T) {
			err := ValidateTargetActionCapability(featureCapabilityTarget(cad.Targetability{}), row.action)
			if err == nil {
				t.Fatalf("expected %s denial", row.action)
			}
			if !strings.Contains(err.Error(), row.field+"=true") {
				t.Fatalf("expected diagnostic to require %s=true, got %q", row.field, err.Error())
			}
			for _, other := range allFields {
				if other == row.field {
					continue
				}
				// Unsuppress contains "Suppress" as a substring, and Unhide
				// contains "Hide"; guard against those false positives by
				// checking the exact "<field>=true" token.
				if strings.Contains(err.Error(), other+"=true") {
					t.Fatalf("action %q diagnostic wrongly names %s: %q", row.action, other, err.Error())
				}
			}
		})
	}
}

// 18. Denial determinism: the representative Feature suppress denial and a
// second axis (unhide) each produce an identical diagnostic across repeats.
func TestValidateTargetActionCapability_DenialDeterminism(t *testing.T) {
	wantSuppress := `action "suppress" requires captured Targetability.Suppress=true for semantic feature "feat.pad"`
	wantUnhide := `action "unhide" requires captured Targetability.Unhide=true for semantic feature "feat.pad"`
	for i := 0; i < 10; i++ {
		if got := ValidateTargetActionCapability(featureCapabilityTarget(cad.Targetability{}), "suppress"); got == nil || got.Error() != wantSuppress {
			t.Fatalf("iteration %d: suppress denial not stable: %v", i, got)
		}
		if got := ValidateTargetActionCapability(featureCapabilityTarget(cad.Targetability{}), "unhide"); got == nil || got.Error() != wantUnhide {
			t.Fatalf("iteration %d: unhide denial not stable: %v", i, got)
		}
	}
}

// 19. The gate is validation-only: it never mutates the resolved target, on
// either the passing or the failing path.
func TestValidateTargetActionCapability_DoesNotMutateTarget(t *testing.T) {
	target := featureCapabilityTarget(cad.Targetability{Suppress: true, Hide: false})
	snapshot := target

	if err := ValidateTargetActionCapability(target, "suppress"); err != nil {
		t.Fatalf("expected suppress to pass, got %v", err)
	}
	if target != snapshot {
		t.Fatalf("passing path mutated target\nwant: %#v\ngot:  %#v", snapshot, target)
	}

	if err := ValidateTargetActionCapability(target, "hide"); err == nil {
		t.Fatal("expected hide to fail")
	}
	if target != snapshot {
		t.Fatalf("failing path mutated target\nwant: %#v\ngot:  %#v", snapshot, target)
	}
}

// 20. The low-level gate does not depend on semantic-map operation-domain
// completeness: unsuppress and unhide are authorized purely by their captured
// Targetability bits even though semantic-map operation metadata may not
// expose equivalent independent operations. Task 7 consumes no
// semanticmap.OperationCapabilities input.
func TestValidateTargetActionCapability_IndependentOfSemanticMapOperationDomain(t *testing.T) {
	if err := ValidateTargetActionCapability(featureCapabilityTarget(cad.Targetability{Unsuppress: true}), "unsuppress"); err != nil {
		t.Fatalf("expected unsuppress to pass on Targetability.Unsuppress=true alone, got %v", err)
	}
	if err := ValidateTargetActionCapability(featureCapabilityTarget(cad.Targetability{Unhide: true}), "unhide"); err != nil {
		t.Fatalf("expected unhide to pass on Targetability.Unhide=true alone, got %v", err)
	}
}
