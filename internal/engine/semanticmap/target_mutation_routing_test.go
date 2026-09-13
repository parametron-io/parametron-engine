package semanticmap

import (
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/semantic"
)

// Phase 5 Task 9 permanently locks the generic part/assembly mutation routing
// that RouteTargetMutations performs on the private Task 8 target-action
// MutationIntent list:
//
//	semantic target metadata
//	    -> generic part / assembly destination (shared resolveMutationDestination)
//	    -> semantic identity -> native Object name (shared resolveManifestEntityName)
//	    -> private RoutedTargetMutation{OperationKind, Object}
//
// Task 9 performs no mutation-family conversion, no boolean encoding, and no
// runtime emission. It does not consult the semantic map, OperationCapabilities,
// SemanticToManifest.Mutations, or captured Targetability. Task 7 owns capability
// eligibility; Task 10+ owns runtime family expansion.
//
// The canonical destination policy locked here (identical for the existing
// ProjectMutations projection and for Task 9) is:
//
//	TargetEntityKind == "assembly"          -> assembly
//	TargetEntityKind == "part"              -> part
//	else Scope == "assembly"                -> assembly
//	else Scope == "part"                    -> part
//	else                                    -> deterministic failure
//
// TargetEntityKind wins before the Scope fallback: malformed direct inputs such
// as (part kind, assembly scope) route to part, and (assembly kind, part scope)
// route to assembly. These tests protect historical routing compatibility and
// deliberately do not introduce a conflict-rejection rule.

// routingModel is a deliberately minimal semantic model. RouteTargetMutations
// iterates Features/Components directly and never validates the model, so the
// fixture only needs the identity/name data the routing path reads.
func routingModel() *semantic.Model {
	return &semantic.Model{
		Components: []semantic.Component{
			{ID: "cmp.root", Kind: "assembly", Name: "RootAssembly"},
			{ID: "cmp.sub", Kind: "assembly", Name: "Sub"},
			{ID: "cmp.cover", Kind: "part", Name: "Cover"},
			{ID: "cmp.zeta", Kind: "part", Name: "Zeta"},
			{ID: "cmp.alpha", Kind: "part", Name: "Alpha"},
			{ID: "cmp.middle", Kind: "part", Name: "Middle"},
			{ID: "cmp.nameless", Kind: "part", Name: ""},
			{ID: "cmp.displayonly", Kind: "part", Name: "", DisplayName: "Cover Visible Label"},
		},
		Features: []semantic.Feature{
			{ID: "feat.pad", ComponentID: "cmp.root", Name: "Pad"},
			{ID: "feat.pocket", ComponentID: "cmp.root", Name: "Pocket"},
			{ID: "feat.chamfer", ComponentID: "cmp.root", Name: "Chamfer"},
			{ID: "feat.nameless", ComponentID: "cmp.root", Name: ""},
			{ID: "feat.displayonly", ComponentID: "cmp.root", Name: "", DisplayName: "Pad Visible Label"},
			{ID: "feat.whitespace", ComponentID: "cmp.root", Name: "   "},
		},
	}
}

// routingLinkage builds an identity linkage carrying exactly the component
// entries requested. Feature routing needs no linkage entry; component routing
// requires one (this is the mandatory identity-linkage gate Task 9 reuses).
func routingLinkage(componentIDs ...string) *IdentityLinkage {
	entries := make([]IdentityLink, 0, len(componentIDs))
	for _, id := range componentIDs {
		entries = append(entries, IdentityLink{
			EntityKind:    identityKindComponent,
			SemanticID:    id,
			CaptureID:     id,
			IdentityField: "capture.id",
		})
	}
	return &IdentityLinkage{Entries: entries}
}

func targetActionMutation(operationKind, targetEntityKind, semanticID, scope string) semantic.MutationIntent {
	return semantic.MutationIntent{
		OperationKind:    operationKind,
		TargetEntityKind: targetEntityKind,
		TargetSemanticID: semanticID,
		Scope:            scope,
		ValueSource:      semantic.MutationValueSource{Kind: "target_action"},
	}
}

func routeOrFail(t *testing.T, linkage *IdentityLinkage, mutations ...semantic.MutationIntent) *TargetMutationRouting {
	t.Helper()
	routing, err := RouteTargetMutations(&TargetMutationRoutingRequest{
		Model:           routingModel(),
		IdentityLinkage: linkage,
		Mutations:       mutations,
	})
	if err != nil {
		t.Fatalf("RouteTargetMutations returned error: %v", err)
	}
	if routing == nil {
		t.Fatal("RouteTargetMutations returned nil routing without error")
	}
	return routing
}

func routeExpectError(t *testing.T, linkage *IdentityLinkage, mutations ...semantic.MutationIntent) error {
	t.Helper()
	routing, err := RouteTargetMutations(&TargetMutationRoutingRequest{
		Model:           routingModel(),
		IdentityLinkage: linkage,
		Mutations:       mutations,
	})
	if err == nil {
		t.Fatalf("expected RouteTargetMutations error, got routing %#v", routing)
	}
	if routing != nil {
		t.Fatalf("expected nil routing on error, got %#v", routing)
	}
	return err
}

// 1. part TargetEntityKind routes to Part.
func TestRouteTargetMutations_PartTargetEntityKindRoutesToPart(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.cover"),
		targetActionMutation("suppress", "part", "cmp.cover", "part"))
	if len(routing.Part) != 1 || len(routing.Assembly) != 0 {
		t.Fatalf("expected one part route and empty assembly, got %#v", routing)
	}
	if routing.Part[0] != (RoutedTargetMutation{OperationKind: "suppress", Object: "Cover"}) {
		t.Fatalf("unexpected part route: %#v", routing.Part[0])
	}
}

// 2. assembly TargetEntityKind routes to Assembly.
func TestRouteTargetMutations_AssemblyTargetEntityKindRoutesToAssembly(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.root"),
		targetActionMutation("unsuppress", "assembly", "cmp.root", "assembly"))
	if len(routing.Assembly) != 1 || len(routing.Part) != 0 {
		t.Fatalf("expected one assembly route and empty part, got %#v", routing)
	}
	if routing.Assembly[0] != (RoutedTargetMutation{OperationKind: "unsuppress", Object: "RootAssembly"}) {
		t.Fatalf("unexpected assembly route: %#v", routing.Assembly[0])
	}
}

// 3. Feature + part Scope routes to Part.
func TestRouteTargetMutations_FeaturePartScopeRoutesToPart(t *testing.T) {
	routing := routeOrFail(t, routingLinkage(),
		targetActionMutation("hide", "feature", "feat.pad", "part"))
	if len(routing.Part) != 1 || len(routing.Assembly) != 0 {
		t.Fatalf("expected feature part-scope route into Part, got %#v", routing)
	}
	if routing.Part[0].Object != "Pad" || routing.Part[0].OperationKind != "hide" {
		t.Fatalf("unexpected feature part route: %#v", routing.Part[0])
	}
}

// 4. Feature + assembly Scope routes to Assembly. Permanently locks the Feature
// Scope fallback.
func TestRouteTargetMutations_FeatureAssemblyScopeRoutesToAssembly(t *testing.T) {
	routing := routeOrFail(t, routingLinkage(),
		targetActionMutation("unhide", "feature", "feat.pad", "assembly"))
	if len(routing.Assembly) != 1 || len(routing.Part) != 0 {
		t.Fatalf("expected feature assembly-scope route into Assembly, got %#v", routing)
	}
	if routing.Assembly[0].Object != "Pad" || routing.Assembly[0].OperationKind != "unhide" {
		t.Fatalf("unexpected feature assembly route: %#v", routing.Assembly[0])
	}
}

// 5. Precedence: part TargetEntityKind beats assembly Scope. This protects the
// existing historical routing precedence; it is not a validation failure.
func TestRouteTargetMutations_PartKindBeatsAssemblyScope(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.cover"),
		targetActionMutation("delete", "part", "cmp.cover", "assembly"))
	if len(routing.Part) != 1 || len(routing.Assembly) != 0 {
		t.Fatalf("expected (part kind, assembly scope) to route to Part, got %#v", routing)
	}
	if routing.Part[0].Object != "Cover" {
		t.Fatalf("unexpected object: %#v", routing.Part[0])
	}
}

// 6. Precedence: assembly TargetEntityKind beats part Scope.
func TestRouteTargetMutations_AssemblyKindBeatsPartScope(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.root"),
		targetActionMutation("delete", "assembly", "cmp.root", "part"))
	if len(routing.Assembly) != 1 || len(routing.Part) != 0 {
		t.Fatalf("expected (assembly kind, part scope) to route to Assembly, got %#v", routing)
	}
	if routing.Assembly[0].Object != "RootAssembly" {
		t.Fatalf("unexpected object: %#v", routing.Assembly[0])
	}
}

// 7. Unsupported destination metadata fails deterministically: no default-to-part
// and no default-to-assembly.
func TestRouteTargetMutations_UnsupportedDestinationMetadataFails(t *testing.T) {
	err := routeExpectError(t, routingLinkage(),
		targetActionMutation("suppress", "feature", "feat.pad", ""))
	if !strings.Contains(err.Error(), "unsupported target entity kind") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 8. The shared destination helper is identical for the existing generic
// projection and Task 9: resolveMutationDestination is not forked, and the
// actual routed bucket agrees with it for every representative metadata shape.
func TestRouteTargetMutations_SharedDestinationHelperParityWithProjection(t *testing.T) {
	cases := []struct {
		name             string
		targetEntityKind string
		scope            string
		semanticID       string
		wantDestination  string
	}{
		{"part kind", "part", "part", "cmp.cover", "part"},
		{"assembly kind", "assembly", "assembly", "cmp.root", "assembly"},
		{"feature + part scope", "feature", "part", "feat.pad", "part"},
		{"feature + assembly scope", "feature", "assembly", "feat.pad", "assembly"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Existing generic projection semantic mutation (parameter provenance).
			generic := semantic.MutationIntent{
				OperationKind:    "set_parameter",
				TargetEntityKind: tc.targetEntityKind,
				TargetSemanticID: tc.semanticID,
				Scope:            tc.scope,
				ValueSource:      semantic.MutationValueSource{Kind: "dsl_parameter"},
			}
			genericDestination, err := resolveMutationDestination(generic)
			if err != nil {
				t.Fatalf("generic resolveMutationDestination error: %v", err)
			}

			// Task 9 target-action mutation with identical routing metadata.
			action := targetActionMutation("suppress", tc.targetEntityKind, tc.semanticID, tc.scope)
			actionDestination, err := resolveMutationDestination(action)
			if err != nil {
				t.Fatalf("target-action resolveMutationDestination error: %v", err)
			}

			if genericDestination != actionDestination || actionDestination != tc.wantDestination {
				t.Fatalf("destination policy diverged: generic=%q action=%q want=%q", genericDestination, actionDestination, tc.wantDestination)
			}

			routing := routeOrFail(t, routingLinkage("cmp.cover", "cmp.root"), action)
			routedBucket := "part"
			if len(routing.Assembly) == 1 && len(routing.Part) == 0 {
				routedBucket = "assembly"
			} else if !(len(routing.Part) == 1 && len(routing.Assembly) == 0) {
				t.Fatalf("expected exactly one routed entry, got %#v", routing)
			}
			if routedBucket != tc.wantDestination {
				t.Fatalf("routed bucket %q does not match shared destination policy %q", routedBucket, tc.wantDestination)
			}
		})
	}
}

// 9 / 10. Feature semantic identity resolves to the native Feature.Name, never
// to the semantic ID that is the lookup key.
func TestRouteTargetMutations_FeatureSemanticIDResolvesToNativeName(t *testing.T) {
	routing := routeOrFail(t, routingLinkage(),
		targetActionMutation("suppress", "feature", "feat.pad", "part"))
	if routing.Part[0].Object != "Pad" {
		t.Fatalf("expected Object %q, got %q", "Pad", routing.Part[0].Object)
	}
	if routing.Part[0].Object == "feat.pad" {
		t.Fatal("semantic ID must never be emitted as the routed Object")
	}
}

// 11. part Component resolves to Component.Name.
func TestRouteTargetMutations_PartComponentResolvesToComponentName(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.cover"),
		targetActionMutation("hide", "part", "cmp.cover", "part"))
	if routing.Part[0].Object != "Cover" {
		t.Fatalf("expected Object %q, got %q", "Cover", routing.Part[0].Object)
	}
}

// 12. assembly Component resolves to Component.Name.
func TestRouteTargetMutations_AssemblyComponentResolvesToComponentName(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.root"),
		targetActionMutation("delete", "assembly", "cmp.root", "assembly"))
	if routing.Assembly[0].Object != "RootAssembly" {
		t.Fatalf("expected Object %q, got %q", "RootAssembly", routing.Assembly[0].Object)
	}
}

// 13. Component identity-linkage is mandatory: a valid component identity with no
// linkage entry fails deterministically. No Component.Name shortcut.
func TestRouteTargetMutations_ComponentIdentityLinkageMandatory(t *testing.T) {
	err := routeExpectError(t, routingLinkage(),
		targetActionMutation("suppress", "part", "cmp.cover", "part"))
	if !strings.Contains(err.Error(), "missing identity linkage") {
		t.Fatalf("expected missing identity linkage failure, got: %v", err)
	}
}

// 14. Valid Component identity-linkage resolves to the exact native Name.
func TestRouteTargetMutations_ValidComponentLinkageResolvesExactName(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.sub"),
		targetActionMutation("delete", "assembly", "cmp.sub", "assembly"))
	if routing.Assembly[0].Object != "Sub" {
		t.Fatalf("expected Object %q, got %q", "Sub", routing.Assembly[0].Object)
	}
}

// 15. Missing Feature semantic ID fails deterministically; no semantic-ID
// fallback.
func TestRouteTargetMutations_MissingFeatureSemanticIDFails(t *testing.T) {
	err := routeExpectError(t, routingLinkage(),
		targetActionMutation("suppress", "feature", "feat.missing", "part"))
	if !strings.Contains(err.Error(), `missing target for feature "feat.missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 16. Missing Component semantic ID fails deterministically.
func TestRouteTargetMutations_MissingComponentSemanticIDFails(t *testing.T) {
	err := routeExpectError(t, routingLinkage("cmp.missing"),
		targetActionMutation("suppress", "part", "cmp.missing", "part"))
	if !strings.Contains(err.Error(), `missing target for part "cmp.missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 17 / 20. Empty Feature Name fails; DisplayName is never a fallback.
func TestRouteTargetMutations_EmptyFeatureNameFailsNoDisplayNameFallback(t *testing.T) {
	err := routeExpectError(t, routingLinkage(),
		targetActionMutation("suppress", "feature", "feat.nameless", "part"))
	if !strings.Contains(err.Error(), "missing manifest-facing name for feature") {
		t.Fatalf("unexpected error: %v", err)
	}

	err = routeExpectError(t, routingLinkage(),
		targetActionMutation("suppress", "feature", "feat.displayonly", "part"))
	if strings.Contains(err.Error(), "Pad Visible Label") {
		t.Fatalf("DisplayName leaked into routing diagnostic: %v", err)
	}
}

// 18. Whitespace-only Feature Name is rejected deterministically (name resolution
// trims before the non-empty check).
func TestRouteTargetMutations_WhitespaceOnlyFeatureNameFails(t *testing.T) {
	err := routeExpectError(t, routingLinkage(),
		targetActionMutation("suppress", "feature", "feat.whitespace", "part"))
	if !strings.Contains(err.Error(), "missing manifest-facing name for feature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 19. Empty Component Name fails at the same native-name boundary.
func TestRouteTargetMutations_EmptyComponentNameFails(t *testing.T) {
	err := routeExpectError(t, routingLinkage("cmp.nameless"),
		targetActionMutation("suppress", "part", "cmp.nameless", "part"))
	if !strings.Contains(err.Error(), "missing manifest-facing name for part") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 21. No semantic-ID fallback: a missing entity never yields Object == semantic
// ID; the call fails and returns no routing at all.
func TestRouteTargetMutations_NoSemanticIDAsObjectFallback(t *testing.T) {
	routing, err := RouteTargetMutations(&TargetMutationRoutingRequest{
		Model:           routingModel(),
		IdentityLinkage: routingLinkage(),
		Mutations:       []semantic.MutationIntent{targetActionMutation("delete", "feature", "feat.missing", "part")},
	})
	if err == nil {
		t.Fatal("expected deterministic failure for missing semantic identity")
	}
	if routing != nil {
		t.Fatalf("expected no routing (no semantic-ID-as-object fallback), got %#v", routing)
	}
}

// 22. Exact routed struct shape: RoutedTargetMutation carries only OperationKind
// and Object. It must not carry semantic ID, target kind, Scope, Targetability,
// ValueSource, or SemanticEntityKind.
func TestRoutedTargetMutation_ExactFieldShape(t *testing.T) {
	rt := reflect.TypeOf(RoutedTargetMutation{})
	got := make(map[string]reflect.Kind, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		got[rt.Field(i).Name] = rt.Field(i).Type.Kind()
	}
	want := map[string]reflect.Kind{
		"OperationKind": reflect.String,
		"Object":        reflect.String,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RoutedTargetMutation shape changed\nwant: %#v\ngot:  %#v", want, got)
	}
	for _, forbidden := range []string{
		"TargetSemanticID", "SemanticID", "TargetEntityKind", "SemanticEntityKind",
		"Scope", "Targetability", "ValueSource", "DisplayName", "Label", "Target",
	} {
		if _, ok := rt.FieldByName(forbidden); ok {
			t.Fatalf("RoutedTargetMutation must not carry field %q", forbidden)
		}
	}
}

// 22b. TargetMutationRouting is exactly two ordered buckets of RoutedTargetMutation.
func TestTargetMutationRouting_ExactBucketShape(t *testing.T) {
	rt := reflect.TypeOf(TargetMutationRouting{})
	if rt.NumField() != 2 {
		t.Fatalf("expected 2 buckets, got %d", rt.NumField())
	}
	for _, name := range []string{"Assembly", "Part"} {
		field, ok := rt.FieldByName(name)
		if !ok {
			t.Fatalf("missing bucket %q", name)
		}
		if field.Type != reflect.TypeOf([]RoutedTargetMutation(nil)) {
			t.Fatalf("bucket %q has type %s, want []RoutedTargetMutation", name, field.Type)
		}
	}
}

// 23. Complete five-operation routing matrix: the same semantic target and
// destination, five literal OperationKinds, each preserved exactly.
func TestRouteTargetMutations_FiveOperationMatrixPreservesOperationKind(t *testing.T) {
	for _, operation := range []string{"suppress", "unsuppress", "hide", "unhide", "delete"} {
		t.Run(operation, func(t *testing.T) {
			routing := routeOrFail(t, routingLinkage(),
				targetActionMutation(operation, "feature", "feat.pad", "part"))
			if len(routing.Part) != 1 || len(routing.Assembly) != 0 {
				t.Fatalf("expected one part route, got %#v", routing)
			}
			if routing.Part[0] != (RoutedTargetMutation{OperationKind: operation, Object: "Pad"}) {
				t.Fatalf("operation %q not preserved: %#v", operation, routing.Part[0])
			}
		})
	}
}

// 24 - 28. Each operation keeps its exact OperationKind and gains no boolean /
// visibility / deletion field (RoutedTargetMutation has none).
func TestRouteTargetMutations_NoBooleanOrFamilyConversion(t *testing.T) {
	routing := routeOrFail(t, routingLinkage(),
		targetActionMutation("unsuppress", "feature", "feat.pad", "part"),
		targetActionMutation("unhide", "feature", "feat.pocket", "part"),
	)
	for _, routed := range routing.Part {
		if routed.OperationKind == "suppress" || routed.OperationKind == "hide" {
			t.Fatalf("unsuppress/unhide collapsed into suppress/hide: %#v", routed)
		}
	}
	if routing.Part[0].OperationKind != "unsuppress" || routing.Part[1].OperationKind != "unhide" {
		t.Fatalf("unexpected operation kinds: %#v", routing.Part)
	}
}

// 29. keep is rejected by the dedicated target-action route.
func TestRouteTargetMutations_KeepRejected(t *testing.T) {
	err := routeExpectError(t, routingLinkage(),
		targetActionMutation("keep", "feature", "feat.pad", "part"))
	if !strings.Contains(err.Error(), `unsupported target-action mutation operation "keep"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 30 / 31. set_parameter and set_property are rejected by the dedicated Task 9
// route, whether they arrive with target-action provenance or their real
// parameter/property provenance. The existing generic projection still supports
// them (covered by output_mutation_projection_test.go).
func TestRouteTargetMutations_ExistingSemanticOperationsRejected(t *testing.T) {
	for _, operation := range []string{"set_parameter", "set_property"} {
		withActionProvenance := targetActionMutation(operation, "part", "cmp.cover", "part")
		if err := routeExpectError(t, routingLinkage("cmp.cover"), withActionProvenance); !strings.Contains(err.Error(), "unsupported target-action mutation operation") {
			t.Fatalf("%s (action provenance): unexpected error: %v", operation, err)
		}

		withRealProvenance := semantic.MutationIntent{
			OperationKind:    operation,
			TargetEntityKind: "part",
			TargetSemanticID: "cmp.cover",
			Scope:            "part",
			ValueSource:      semantic.MutationValueSource{Kind: "dsl_parameter"},
		}
		if err := routeExpectError(t, routingLinkage("cmp.cover"), withRealProvenance); !strings.Contains(err.Error(), "not a canonical target-action mutation") {
			t.Fatalf("%s (real provenance): unexpected error: %v", operation, err)
		}
	}
}

// 32. target_action provenance is accepted.
func TestRouteTargetMutations_TargetActionProvenanceAccepted(t *testing.T) {
	routeOrFail(t, routingLinkage(), targetActionMutation("suppress", "feature", "feat.pad", "part"))
}

// 33. Wrong provenance is rejected deterministically.
func TestRouteTargetMutations_WrongProvenanceRejected(t *testing.T) {
	mutation := targetActionMutation("suppress", "feature", "feat.pad", "part")
	mutation.ValueSource = semantic.MutationValueSource{Kind: "dsl_parameter"}
	err := routeExpectError(t, routingLinkage(), mutation)
	if !strings.Contains(err.Error(), "not a canonical target-action mutation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 34. Empty provenance is rejected deterministically.
func TestRouteTargetMutations_EmptyProvenanceRejected(t *testing.T) {
	mutation := targetActionMutation("suppress", "feature", "feat.pad", "part")
	mutation.ValueSource = semantic.MutationValueSource{}
	err := routeExpectError(t, routingLinkage(), mutation)
	if !strings.Contains(err.Error(), "not a canonical target-action mutation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 35 - 38. Semantic-map / capability independence is structural: the routing
// request has no SemanticMap, contract, capability, manifest, or Targetability
// input, so hide/unhide/delete cannot depend on SemanticToManifest.Mutations,
// OperationCapabilities, RequiresTargetability, or AllowedSemanticTargets.
func TestTargetMutationRoutingRequest_HasNoSemanticMapOrCapabilityInput(t *testing.T) {
	rt := reflect.TypeOf(TargetMutationRoutingRequest{})
	gotFields := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		gotFields = append(gotFields, rt.Field(i).Name)
		name := rt.Field(i).Name
		for _, forbidden := range []string{"SemanticMap", "Contract", "Capabilit", "Manifest", "Targetab", "Family", "Operation"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("routing request must not carry field %q", name)
			}
		}
	}
	want := []string{"Model", "IdentityLinkage", "Mutations"}
	if !reflect.DeepEqual(gotFields, want) {
		t.Fatalf("routing request field set changed\nwant: %v\ngot:  %v", want, gotFields)
	}
}

// 35 - 37. hide / unhide / delete route successfully with no semantic-map family
// mapping in the picture at all.
func TestRouteTargetMutations_VisibilityAndDeletionRouteWithoutFamilySupport(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.cover", "cmp.root"),
		targetActionMutation("hide", "feature", "feat.pad", "part"),
		targetActionMutation("unhide", "part", "cmp.cover", "part"),
		targetActionMutation("delete", "assembly", "cmp.root", "assembly"),
	)
	if len(routing.Part) != 2 || len(routing.Assembly) != 1 {
		t.Fatalf("expected 2 part + 1 assembly routes, got %#v", routing)
	}
	if routing.Part[0].OperationKind != "hide" || routing.Part[1].OperationKind != "unhide" || routing.Assembly[0].OperationKind != "delete" {
		t.Fatalf("visibility/deletion operations not routed verbatim: %#v", routing)
	}
}

// 41. Mixed Feature/Component routing partitions into the two buckets while
// preserving relative input order within each bucket.
func TestRouteTargetMutations_MixedFeatureComponentOrdering(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.root", "cmp.cover"),
		targetActionMutation("suppress", "feature", "feat.pad", "part"),
		targetActionMutation("hide", "assembly", "cmp.root", "assembly"),
		targetActionMutation("delete", "part", "cmp.cover", "part"),
		targetActionMutation("unsuppress", "feature", "feat.pocket", "assembly"),
		targetActionMutation("unhide", "feature", "feat.chamfer", "part"),
	)
	wantPart := []RoutedTargetMutation{
		{OperationKind: "suppress", Object: "Pad"},
		{OperationKind: "delete", Object: "Cover"},
		{OperationKind: "unhide", Object: "Chamfer"},
	}
	wantAssembly := []RoutedTargetMutation{
		{OperationKind: "hide", Object: "RootAssembly"},
		{OperationKind: "unsuppress", Object: "Pocket"},
	}
	if !reflect.DeepEqual(routing.Part, wantPart) {
		t.Fatalf("part bucket\nwant: %#v\ngot:  %#v", wantPart, routing.Part)
	}
	if !reflect.DeepEqual(routing.Assembly, wantAssembly) {
		t.Fatalf("assembly bucket\nwant: %#v\ngot:  %#v", wantAssembly, routing.Assembly)
	}
}

// 42. No lexical sorting: routed order follows input order even when object
// names are deliberately out of lexical order.
func TestRouteTargetMutations_NoLexicalSorting(t *testing.T) {
	routing := routeOrFail(t, routingLinkage("cmp.zeta", "cmp.alpha", "cmp.middle"),
		targetActionMutation("suppress", "part", "cmp.zeta", "part"),
		targetActionMutation("suppress", "part", "cmp.alpha", "part"),
		targetActionMutation("suppress", "part", "cmp.middle", "part"),
	)
	got := []string{routing.Part[0].Object, routing.Part[1].Object, routing.Part[2].Object}
	want := []string{"Zeta", "Alpha", "Middle"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routed order was sorted\nwant: %v\ngot:  %v", want, got)
	}
}

// 43. No OperationKind sorting: distinct operations for one destination keep
// input order.
func TestRouteTargetMutations_NoOperationKindSorting(t *testing.T) {
	routing := routeOrFail(t, routingLinkage(),
		targetActionMutation("delete", "feature", "feat.pad", "part"),
		targetActionMutation("suppress", "feature", "feat.pocket", "part"),
		targetActionMutation("hide", "feature", "feat.chamfer", "part"),
	)
	got := []string{routing.Part[0].OperationKind, routing.Part[1].OperationKind, routing.Part[2].OperationKind}
	want := []string{"delete", "suppress", "hide"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operation order was sorted\nwant: %v\ngot:  %v", want, got)
	}
}

// 44 / 45. No deduplication: distinct semantic targets that share an
// OperationKind yield distinct routed entries.
func TestRouteTargetMutations_NoDeduplication(t *testing.T) {
	routing := routeOrFail(t, routingLinkage(),
		targetActionMutation("suppress", "feature", "feat.pad", "part"),
		targetActionMutation("suppress", "feature", "feat.pocket", "part"),
	)
	if len(routing.Part) != 2 {
		t.Fatalf("expected 2 entries (no merge on matching OperationKind), got %#v", routing.Part)
	}
	if routing.Part[0].Object != "Pad" || routing.Part[1].Object != "Pocket" {
		t.Fatalf("distinct native objects not preserved: %#v", routing.Part)
	}
}

// 46. Repeated mixed routing is deterministic across many runs.
func TestRouteTargetMutations_MixedRoutingDeterminism(t *testing.T) {
	mutations := []semantic.MutationIntent{
		targetActionMutation("suppress", "feature", "feat.pad", "part"),
		targetActionMutation("hide", "assembly", "cmp.root", "assembly"),
		targetActionMutation("delete", "part", "cmp.cover", "part"),
		targetActionMutation("unsuppress", "feature", "feat.pocket", "assembly"),
	}
	first := routeOrFail(t, routingLinkage("cmp.root", "cmp.cover"), mutations...)
	for i := 0; i < 10; i++ {
		got := routeOrFail(t, routingLinkage("cmp.root", "cmp.cover"), mutations...)
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: non-deterministic routing\nfirst: %#v\ngot:   %#v", i, first, got)
		}
	}
}

// 47. Routing failure diagnostics are deterministic across many runs.
func TestRouteTargetMutations_InvalidSemanticIDDiagnosticDeterminism(t *testing.T) {
	mutation := targetActionMutation("suppress", "feature", "feat.missing", "part")
	first := routeExpectError(t, routingLinkage(), mutation).Error()
	for i := 0; i < 10; i++ {
		got := routeExpectError(t, routingLinkage(), mutation).Error()
		if got != first {
			t.Fatalf("iteration %d: unstable diagnostic\nfirst: %q\ngot:   %q", i, first, got)
		}
	}
}

// 48. Provenance failure diagnostics are deterministic across many runs.
func TestRouteTargetMutations_WrongProvenanceDiagnosticDeterminism(t *testing.T) {
	mutation := targetActionMutation("suppress", "feature", "feat.pad", "part")
	mutation.ValueSource = semantic.MutationValueSource{Kind: "dsl_parameter"}
	first := routeExpectError(t, routingLinkage(), mutation).Error()
	for i := 0; i < 10; i++ {
		got := routeExpectError(t, routingLinkage(), mutation).Error()
		if got != first {
			t.Fatalf("iteration %d: unstable diagnostic\nfirst: %q\ngot:   %q", i, first, got)
		}
	}
}

// 67. Zero Task 8 mutations preserve the zero/legacy result shape: an empty
// mutation slice yields empty (non-nil) buckets and never errors.
func TestRouteTargetMutations_ZeroMutationsPreserveEmptyShape(t *testing.T) {
	routing, err := RouteTargetMutations(&TargetMutationRoutingRequest{
		Model:           routingModel(),
		IdentityLinkage: routingLinkage(),
		Mutations:       nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if routing == nil || len(routing.Assembly) != 0 || len(routing.Part) != 0 {
		t.Fatalf("expected empty routing, got %#v", routing)
	}
}

// Guard rails: nil request / nil model / nil linkage fail deterministically
// rather than panicking or fabricating output.
func TestRouteTargetMutations_NilInputsFailDeterministically(t *testing.T) {
	if _, err := RouteTargetMutations(nil); err == nil {
		t.Fatal("expected nil request failure")
	}
	if _, err := RouteTargetMutations(&TargetMutationRoutingRequest{IdentityLinkage: routingLinkage()}); err == nil {
		t.Fatal("expected nil model failure")
	}
	if _, err := RouteTargetMutations(&TargetMutationRoutingRequest{Model: routingModel()}); err == nil {
		t.Fatal("expected nil identity linkage failure")
	}
}

// Targetability independence: two routing requests whose mutations differ only in
// unrelated fields but share OperationKind + routing metadata + identity produce
// an identical routed result. RouteTargetMutations never reads Targetability
// (MutationIntent carries none), so this is proven at the type + behavior level.
func TestRouteTargetMutations_IndependentOfNonRoutingMetadata(t *testing.T) {
	base := targetActionMutation("suppress", "feature", "feat.pad", "part")
	variant := base
	variant.TargetField = "ignored-by-routing"

	a := routeOrFail(t, routingLinkage(), base)
	b := routeOrFail(t, routingLinkage(), variant)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("routing changed with a non-routing field\nbase:    %#v\nvariant: %#v", a, b)
	}
}
