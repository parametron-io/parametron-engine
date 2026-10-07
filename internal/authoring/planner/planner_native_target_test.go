package planner

import (
	"reflect"
	"testing"

	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
)

func nativeTargetPlanModel(t *testing.T) *semantic.Model {
	t.Helper()
	capture, err := cad.Load("../../engine/semantic/testdata/native-target/prm.cad.json")
	if err != nil {
		t.Fatal(err)
	}
	model, err := semantic.BuildFromCapture(capture)
	if err != nil {
		t.Fatal(err)
	}
	model.Parameters = runtimeMutationManifestModel().Parameters
	return model
}

func nativeTargetPlan(t *testing.T, model *semantic.Model, body string) *ExecutionPlan {
	t.Helper()
	_, plan := parseValidateAndPlanCaptureBacked(t, runtimeMutationManifestProduct(body), model, testProjectionContract())
	return plan
}

func TestPlannerNativeTarget_ActionFamiliesAndSemanticLowering(t *testing.T) {
	for _, action := range []string{"suppress", "unsuppress", "hide", "unhide", "delete", "keep"} {
		t.Run(action, func(t *testing.T) {
			model := nativeTargetPlanModel(t)
			if action == "keep" {
				model.Features[0].NativeRef = ""
			}
			plan := nativeTargetPlan(t, model, "    target mounting_bracket: action = "+action)
			payload := manifestPayloadFromPlan(t, plan)
			if payload.PartMutations != nil {
				t.Fatalf("assembly-owned feature routed to part: %+v", payload)
			}
			if action == "keep" {
				omitted := nativeTargetPlan(t, model, "")
				if !reflect.DeepEqual(plan, omitted) {
					t.Fatal("keep differs from omission without a NativeRef")
				}
				return
			}
			want := &ExportManifestMutationCollection{Parameters: []ExportManifestParameterMutation{{Object: "bracket_assembly", Property: "width", ValueParam: "width", Type: "number", Unit: "mm"}}}
			switch action {
			case "suppress", "unsuppress":
				want.Suppression = []ExportManifestSuppressionMutation{{Object: "Body01", Suppressed: action == "suppress"}}
			case "hide", "unhide":
				want.Visibility = []ExportManifestVisibilityMutation{{Object: "Body01", Visible: action == "unhide"}}
			case "delete":
				want.Deletion = []ExportManifestDeletionMutation{{Object: "Body01"}}
			}
			if !reflect.DeepEqual(payload.AssemblyMutations, want) {
				t.Fatalf("native projection: got %+v want %+v", payload.AssemblyMutations, want)
			}
			target, err := semantic.ResolveSemanticTargetByExactName(model, "mounting_bracket")
			if err != nil {
				t.Fatal(err)
			}
			mutation, err := semantic.LowerTargetActionMutationIntent(target, action)
			if err != nil || mutation.TargetSemanticID != "fea.mountingBracket" || mutation.TargetEntityKind != "feature" || mutation.Scope != "assembly" {
				t.Fatalf("lowered intent: %+v, %v", mutation, err)
			}
		})
	}
}

func TestPlannerNativeTarget_IdentityFollowsResolvedIntent(t *testing.T) {
	const body = "    target mounting_bracket: action = suppress"
	first := nativeTargetPlan(t, nativeTargetPlanModel(t), body)
	firstJSON, firstHash := string(mutationIdentityPlanJSON(t, first)), mutationIdentityPlanHash(t, first)
	for i := 0; i < 10; i++ {
		got := nativeTargetPlan(t, nativeTargetPlanModel(t), body)
		if string(mutationIdentityPlanJSON(t, got)) != firstJSON || mutationIdentityPlanHash(t, got) != firstHash {
			t.Fatal("equivalent mapping changed plan identity")
		}
	}
	display := nativeTargetPlanModel(t)
	display.Features[0].DisplayName = "A different label"
	got := nativeTargetPlan(t, display, body)
	if string(mutationIdentityPlanJSON(t, got)) != firstJSON || mutationIdentityPlanHash(t, got) != firstHash {
		t.Fatal("display-only change affected resolved identity")
	}
	changed := nativeTargetPlanModel(t)
	changed.Features[0].NativeRef = "Body02"
	got = nativeTargetPlan(t, changed, body)
	if manifestPayloadFromPlan(t, got).AssemblyMutations.Suppression[0].Object != "Body02" || mutationIdentityPlanHash(t, got) == firstHash || string(mutationIdentityPlanJSON(t, got)) == firstJSON {
		t.Fatal("used mapping did not change serialized intent and hash")
	}
	unused := nativeTargetPlanModel(t)
	unused.Components[0].NativeRef = "UnusedAssembly02"
	got = nativeTargetPlan(t, unused, body)
	if string(mutationIdentityPlanJSON(t, got)) != firstJSON || mutationIdentityPlanHash(t, got) != firstHash {
		t.Fatal("unused native mapping affected identity")
	}
}

func TestPlannerNativeTarget_OrderingUsesProjectedObjects(t *testing.T) {
	model := nativeTargetPlanModel(t)
	other := model.Features[0]
	other.ID = "fea.other"
	other.Name = "aaa"
	other.NativeRef = "ZBody"
	model.Features = append(model.Features, other)
	for _, action := range []string{"suppress", "hide", "delete"} {
		t.Run(action, func(t *testing.T) {
			a := nativeTargetPlan(t, model, "    target aaa: action = "+action+"\n    target mounting_bracket: action = "+action)
			b := nativeTargetPlan(t, model, "    target mounting_bracket: action = "+action+"\n    target aaa: action = "+action)
			if string(mutationIdentityPlanJSON(t, a)) != string(mutationIdentityPlanJSON(t, b)) || mutationIdentityPlanHash(t, a) != mutationIdentityPlanHash(t, b) {
				t.Fatal("authored order affected canonical plan")
			}
			m := manifestPayloadFromPlan(t, a).AssemblyMutations
			var objects []string
			switch action {
			case "suppress":
				for _, v := range m.Suppression {
					objects = append(objects, v.Object)
				}
			case "hide":
				for _, v := range m.Visibility {
					objects = append(objects, v.Object)
				}
			case "delete":
				for _, v := range m.Deletion {
					objects = append(objects, v.Object)
				}
			}
			if !reflect.DeepEqual(objects, []string{"Body01", "ZBody"}) {
				t.Fatalf("ordering followed semantic names: %v", objects)
			}
		})
	}
}

func TestPlannerNativeTarget_DestinationIndependentFromNativeObject(t *testing.T) {
	for _, kind := range []string{"part", "assembly"} {
		for _, name := range []string{"mounting_bracket", "bracket_assembly"} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				model := nativeTargetPlanModel(t)
				model.Components[0].Kind = kind
				plan := nativeTargetPlan(t, model, "    target "+name+": action = hide")
				payload := manifestPayloadFromPlan(t, plan)
				mutations, other := payload.PartMutations, payload.AssemblyMutations
				if kind == "assembly" {
					mutations, other = payload.AssemblyMutations, payload.PartMutations
				}
				want := "Body01"
				if name == "bracket_assembly" {
					want = "Assembly01"
				}
				if other != nil || mutations == nil || len(mutations.Visibility) != 1 || mutations.Visibility[0].Object != want {
					t.Fatalf("%s destination, native object %s: %+v", kind, want, payload)
				}
			})
		}
	}
}
