package semanticmap

import (
	"strings"
	"testing"

	"parametron/internal/engine/cad"
	"parametron/internal/engine/semantic"
)

func nativeTargetModel(t *testing.T) *semantic.Model {
	t.Helper()
	capture, err := cad.Load("../semantic/testdata/native-target/prm.cad.json")
	if err != nil {
		t.Fatal(err)
	}
	model, err := semantic.BuildFromCapture(capture)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func TestResolveTargetNativeObject_StableIdentityAndDestination(t *testing.T) {
	for _, kind := range []string{"feature", "part", "assembly"} {
		t.Run(kind, func(t *testing.T) {
			model := nativeTargetModel(t)
			id, want := "fea.mountingBracket", "Body01"
			if kind != "feature" {
				model.Components[0].Kind = kind
				id, want = "cmp.root", "Assembly01"
			}
			got, err := ResolveTargetNativeObject(model, routingLinkage("cmp.root"), kind, id)
			if err != nil || got != want {
				t.Fatalf("native resolution = %q, %v", got, err)
			}
			for _, scope := range []string{"part", "assembly"} {
				routing, err := RouteTargetMutations(&TargetMutationRoutingRequest{Model: model, IdentityLinkage: routingLinkage("cmp.root"), Mutations: []semantic.MutationIntent{targetActionMutation("suppress", kind, id, scope)}})
				if err != nil {
					t.Fatal(err)
				}
				destination := scope
				if kind != "feature" {
					destination = kind
				}
				routes := routing.Part
				if destination == "assembly" {
					routes = routing.Assembly
				}
				if len(routes) != 1 || routes[0].Object != want {
					t.Fatalf("destination %s: %+v", destination, routing)
				}
			}
		})
	}
}

func TestResolveTargetNativeObject_InvalidInputsAreDeterministic(t *testing.T) {
	type resolverCase struct {
		name, kind, id, fragment string
		mutate                   func(*semantic.Model)
		nilModel, noLink         bool
	}
	cases := []resolverCase{
		{name: "nil model", kind: "feature", id: "fea.mountingBracket", fragment: "requires a semantic model", nilModel: true},
		{name: "unsupported kind", kind: "parameter", id: "fea.mountingBracket", fragment: "unsupported semantic target kind"},
		{name: "missing feature", kind: "feature", id: "fea.missing", fragment: "missing target"},
		{name: "empty semantic ID", kind: "feature", fragment: "missing target"},
		{name: "missing component", kind: "part", id: "cmp.missing", fragment: "missing target"},
		{name: "component kind mismatch", kind: "part", id: "cmp.root", fragment: "missing target"},
		{name: "component linkage", kind: "assembly", id: "cmp.root", fragment: "missing identity linkage", noLink: true},
	}
	for _, kind := range []string{"feature", "assembly", "part"} {
		for _, ref := range []string{"", "   ", " Body01", "Body01 ", "Body\x0001"} {
			kind, ref := kind, ref
			id := "fea.mountingBracket"
			if kind != "feature" {
				id = "cmp.root"
			}
			fragment := "invalid native target mapping"
			if strings.TrimSpace(ref) == "" {
				fragment = "missing native target mapping"
			}
			cases = append(cases, resolverCase{name: kind + "/" + ref, kind: kind, id: id, fragment: fragment, mutate: func(m *semantic.Model) {
				if kind == "feature" {
					m.Features[0].NativeRef = ref
				} else {
					m.Components[0].Kind = kind
					m.Components[0].NativeRef = ref
				}
			}})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := nativeTargetModel(t)
			if tc.mutate != nil {
				tc.mutate(model)
			}
			if tc.nilModel {
				model = nil
			}
			link := routingLinkage("cmp.root")
			if tc.noLink {
				link = routingLinkage()
			}
			first := ""
			for i := 0; i < 3; i++ {
				got, err := ResolveTargetNativeObject(model, link, tc.kind, tc.id)
				if err == nil || got != "" || !strings.Contains(err.Error(), tc.fragment) {
					t.Fatalf("got %q, %v; want %s", got, err, tc.fragment)
				}
				if i == 0 {
					first = err.Error()
				} else if first != err.Error() {
					t.Fatal("unstable error")
				}
			}
		})
	}
}

func TestResolveMutationManifestTarget_AllTargetFamiliesUseNativeObject(t *testing.T) {
	model := nativeTargetModel(t)
	for _, operation := range []string{"suppress", "unsuppress", "hide", "unhide", "delete"} {
		t.Run(operation, func(t *testing.T) {
			object, property, err := resolveMutationManifestTarget(nil, model, routingLinkage(), targetActionMutation(operation, "feature", "fea.mountingBracket", "assembly"))
			if err != nil || object != "Body01" || property != "" {
				t.Fatalf("projected target = %q/%q, %v", object, property, err)
			}
		})
	}
}

func TestProjectMutations_SeparatedNativeTargetFamilies(t *testing.T) {
	model := nativeTargetModel(t)
	linkage := routingLinkage("cmp.root")
	for _, operation := range []string{"suppress", "unsuppress", "hide", "unhide", "delete"} {
		t.Run(operation, func(t *testing.T) {
			projection, err := ProjectMutations(&MutationProjectionRequest{Model: model, IdentityLinkage: linkage, Contract: matureVisibilityDeletionContract(t), ResolvedValues: map[string]any{}, Intent: outputMutationProjectionIntentWithMutations(matureIntent(operation, "feature", "fea.mountingBracket", "assembly"))})
			if err != nil {
				t.Fatal(err)
			}
			if projection.Part != nil || projection.Assembly == nil {
				t.Fatalf("destination changed: %+v", projection)
			}
			switch operation {
			case "suppress", "unsuppress":
				if len(projection.Assembly.Suppression) != 1 || projection.Assembly.Suppression[0].Object != "Body01" || projection.Assembly.Suppression[0].Suppressed != (operation == "suppress") {
					t.Fatalf("suppression: %+v", projection.Assembly)
				}
			case "hide", "unhide":
				if len(projection.Assembly.Visibility) != 1 || projection.Assembly.Visibility[0].Object != "Body01" || projection.Assembly.Visibility[0].Visible != (operation == "unhide") {
					t.Fatalf("visibility: %+v", projection.Assembly)
				}
			case "delete":
				if len(projection.Assembly.Deletion) != 1 || projection.Assembly.Deletion[0].Object != "Body01" {
					t.Fatalf("deletion: %+v", projection.Assembly)
				}
			}
		})
	}
}

func TestResolveTargetNativeObject_PreservesAcceptedSelectorExactly(t *testing.T) {
	model := nativeTargetModel(t)
	for _, selector := range []string{"Body01", "Body.01-2", "Body 01"} {
		model.Features[0].NativeRef = selector
		got, err := ResolveTargetNativeObject(model, nil, "feature", "fea.mountingBracket")
		if err != nil || got != selector {
			t.Fatalf("selector %q became %q: %v", selector, got, err)
		}
	}
}

func TestRouteTargetMutations_MissingCapturedNativeMappingFails(t *testing.T) {
	for _, absent := range []bool{true, false} {
		capture, err := cad.Load("../semantic/testdata/native-target/prm.cad.json")
		if err != nil {
			t.Fatal(err)
		}
		if absent {
			capture.Entities.Features[0].IdentitySource = nil
		} else {
			capture.Entities.Features[0].IdentitySource.NativeRef = ""
		}
		model, err := semantic.BuildFromCapture(capture)
		if err != nil {
			t.Fatal(err)
		}
		target, err := semantic.ResolveSemanticTargetByExactName(model, "mounting_bracket")
		if err != nil {
			t.Fatal(err)
		}
		mutation, err := semantic.LowerTargetActionMutationIntent(target, "suppress")
		if err != nil {
			t.Fatal(err)
		}
		routing, err := RouteTargetMutations(&TargetMutationRoutingRequest{Model: model, IdentityLinkage: routingLinkage(), Mutations: []semantic.MutationIntent{*mutation}})
		if err == nil || routing != nil || !strings.Contains(err.Error(), "missing native target mapping") {
			t.Fatalf("missing capture mapping routed: %+v, %v", routing, err)
		}
	}
}
