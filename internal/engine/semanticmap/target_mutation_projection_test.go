package semanticmap

import (
	"reflect"
	"testing"

	"parametron/internal/engine/semantic"
)

// Phase 5 Task 10 permanently locks the canonical Engine mutation-family model.
// Task 9 hands over generic routed target actions:
//
//	RoutedTargetMutation{OperationKind, Object}
//	    -> Assembly / Part destination buckets (TargetMutationRouting)
//
// Task 10 converts each routed entry into one canonical internal mutation-family
// record, preserving the Task 9 destination bucket and the per-family
// input-relative order:
//
//	suppress   -> Suppression {Object, Suppressed:true}
//	unsuppress -> Suppression {Object, Suppressed:false}
//	hide       -> Visibility  {Object, Visible:false}
//	unhide     -> Visibility  {Object, Visible:true}
//	delete     -> Deletion    {Object}
//
// hide is NOT suppress and unhide is NOT unsuppress: suppression and visibility
// are separate semantic/runtime axes. Task 10 performs no semantic target
// resolution, no re-routing, no Targetability check, no lexical/operation
// sorting, and no deduplication. It remains private/internal: no schema 2.0, no
// runtime mutation-section emission, and the broader Parameters/Properties
// families are retained untouched (Task 11 later owns aligned runtime
// filtering).

func rtm(op, object string) RoutedTargetMutation {
	return RoutedTargetMutation{OperationKind: op, Object: object}
}

func projectRoutingOrFail(t *testing.T, routing *TargetMutationRouting) *ProjectedTargetMutationRouting {
	t.Helper()
	projected, err := ProjectTargetMutationRouting(routing)
	if err != nil {
		t.Fatalf("ProjectTargetMutationRouting returned error: %v", err)
	}
	return projected
}

func projectRoutingExpectError(t *testing.T, routing *TargetMutationRouting) error {
	t.Helper()
	projected, err := ProjectTargetMutationRouting(routing)
	if err == nil {
		t.Fatalf("expected ProjectTargetMutationRouting error, got %#v", projected)
	}
	if projected != nil {
		t.Fatalf("expected nil projection on error, got %#v", projected)
	}
	return err
}

func partRouting(mutations ...RoutedTargetMutation) *TargetMutationRouting {
	return &TargetMutationRouting{Part: mutations, Assembly: []RoutedTargetMutation{}}
}

func assemblyRouting(mutations ...RoutedTargetMutation) *TargetMutationRouting {
	return &TargetMutationRouting{Assembly: mutations, Part: []RoutedTargetMutation{}}
}

// ---------------------------------------------------------------------------
// PART A — type / domain contract
// ---------------------------------------------------------------------------

// 1. Suppression family record carries exactly Object + Suppressed.
func TestManifestSuppressionMutation_ExactFieldShape(t *testing.T) {
	assertExactStructShape(t, reflect.TypeOf(ManifestSuppressionMutation{}), map[string]reflect.Kind{
		"Object":     reflect.String,
		"Suppressed": reflect.Bool,
	}, []string{"TargetSemanticID", "TargetEntityKind", "Scope", "Targetability", "ValueSource", "Visible"})
}

// 2. Visibility family record carries exactly Object + Visible.
func TestManifestVisibilityMutation_ExactFieldShape(t *testing.T) {
	assertExactStructShape(t, reflect.TypeOf(ManifestVisibilityMutation{}), map[string]reflect.Kind{
		"Object":  reflect.String,
		"Visible": reflect.Bool,
	}, []string{"TargetSemanticID", "TargetEntityKind", "Scope", "Targetability", "ValueSource", "Suppressed"})
}

// 3. Deletion family record carries exactly Object.
func TestManifestDeletionMutation_ExactFieldShape(t *testing.T) {
	assertExactStructShape(t, reflect.TypeOf(ManifestDeletionMutation{}), map[string]reflect.Kind{
		"Object": reflect.String,
	}, []string{"Force", "Cascade", "TargetKind", "SemanticID", "DependencyPolicy", "Recursive", "Suppressed", "Visible"})
}

func assertExactStructShape(t *testing.T, rt reflect.Type, want map[string]reflect.Kind, forbidden []string) {
	t.Helper()
	got := make(map[string]reflect.Kind, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		got[rt.Field(i).Name] = rt.Field(i).Type.Kind()
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s shape changed\nwant: %#v\ngot:  %#v", rt.Name(), want, got)
	}
	for _, name := range forbidden {
		if _, ok := rt.FieldByName(name); ok {
			t.Fatalf("%s must not carry field %q", rt.Name(), name)
		}
	}
}

// 4. The broader internal mutation collection retains all five families:
// Parameters + Properties survive alongside the new Suppression + Visibility +
// Deletion. Task 10 must not remove Parameters or Properties.
func TestManifestMutationCollection_RetainsAllFamilies(t *testing.T) {
	rt := reflect.TypeOf(ManifestMutationCollection{})
	want := []string{"Parameters", "Properties", "Suppression", "Visibility", "Deletion"}
	if rt.NumField() != len(want) {
		t.Fatalf("ManifestMutationCollection field count changed: want %d, got %d", len(want), rt.NumField())
	}
	for i, name := range want {
		if rt.Field(i).Name != name {
			t.Fatalf("ManifestMutationCollection field %d: want %q, got %q", i, name, rt.Field(i).Name)
		}
	}
	if got := reflect.TypeOf(ManifestMutationCollection{}).Field(3).Type; got != reflect.TypeOf([]ManifestVisibilityMutation(nil)) {
		t.Fatalf("Visibility field type changed: %s", got)
	}
	if got := reflect.TypeOf(ManifestMutationCollection{}).Field(4).Type; got != reflect.TypeOf([]ManifestDeletionMutation(nil)) {
		t.Fatalf("Deletion field type changed: %s", got)
	}
}

// 5. Exact canonical operation constant literals. Derived from literal data,
// never from the production constants themselves.
func TestCanonicalOperationConstantLiterals(t *testing.T) {
	cases := []struct {
		got  OperationName
		want string
	}{
		{OperationSuppress, "suppress"},
		{OperationUnsuppress, "unsuppress"},
		{OperationHide, "hide"},
		{OperationUnhide, "unhide"},
		{OperationDelete, "delete"},
	}
	for _, tc := range cases {
		if string(tc.got) != tc.want {
			t.Fatalf("operation constant literal drift: got %q, want %q", string(tc.got), tc.want)
		}
	}
}

// 6. Supported operation registry: exact deterministic ordered list, unsuppress
// and unhide are permanent members, and no name appears twice.
func TestSupportedOperationRegistry_ExactExpandedDomain(t *testing.T) {
	got := SupportedOperationNames()
	want := []string{"delete", "hide", "suppress", "unhide", "unsuppress", "write_metadata", "write_parameter"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("supported operation registry drift\nwant: %#v\ngot:  %#v", want, got)
	}

	seen := map[string]int{}
	for _, name := range got {
		seen[name]++
	}
	for name, count := range seen {
		if count != 1 {
			t.Fatalf("supported operation %q appears %d times, want exactly once", name, count)
		}
	}
	for _, required := range []string{"suppress", "unsuppress", "hide", "unhide", "delete"} {
		if seen[required] != 1 {
			t.Fatalf("supported operation registry missing canonical member %q", required)
		}
	}
}

// ---------------------------------------------------------------------------
// PART B — internal manifest-collection recognition
// ---------------------------------------------------------------------------

// mutationCollectionContract clones the smoke contract and replaces its
// mutation mapping with a single entry for the given manifestCollection. It is
// the minimal way to prove the internal semantic-map model recognizes (or
// rejects) a manifest mutation collection through the real Validate contract.
func mutationCollectionContract(t *testing.T, manifestCollection string) *SemanticMap {
	t.Helper()
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	mapping := MutationMapping{ManifestCollection: manifestCollection, TargetField: "object"}
	switch {
	case endsWith(manifestCollection, ".suppression"), endsWith(manifestCollection, ".visibility"):
		mapping.ValueField = "suppressed"
		if endsWith(manifestCollection, ".visibility") {
			mapping.ValueField = "visible"
		}
		mapping.Value = boolPtr(true)
	case endsWith(manifestCollection, ".parameters"):
		mapping.PropertyField = "property"
		mapping.ValueField = "valueParam"
	case endsWith(manifestCollection, ".properties"):
		mapping.PropertyField = "property"
		mapping.ValueField = "value"
	}
	contract.SemanticToManifest.Mutations = map[string]MutationMapping{"op": mapping}
	return contract
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func assertCollectionRecognized(t *testing.T, manifestCollection string) {
	t.Helper()
	if err := Validate(mutationCollectionContract(t, manifestCollection)); err != nil {
		t.Fatalf("expected internal semantic-map model to recognize %q, got: %v", manifestCollection, err)
	}
}

// 7 / 8. Part and assembly visibility collections are recognized.
func TestInternalCollectionRecognition_Visibility(t *testing.T) {
	assertCollectionRecognized(t, "partMutations.visibility")
	assertCollectionRecognized(t, "assemblyMutations.visibility")
}

// 9 / 10. Part and assembly deletion collections are recognized.
func TestInternalCollectionRecognition_Deletion(t *testing.T) {
	assertCollectionRecognized(t, "partMutations.deletion")
	assertCollectionRecognized(t, "assemblyMutations.deletion")
}

// 11 / 12. Existing part/assembly suppression collections remain recognized.
func TestInternalCollectionRecognition_SuppressionRegression(t *testing.T) {
	assertCollectionRecognized(t, "partMutations.suppression")
	assertCollectionRecognized(t, "assemblyMutations.suppression")
}

// 13 / 14. Existing parameters/properties collections remain recognized.
func TestInternalCollectionRecognition_ParametersPropertiesRegression(t *testing.T) {
	assertCollectionRecognized(t, "partMutations.parameters")
	assertCollectionRecognized(t, "assemblyMutations.parameters")
	assertCollectionRecognized(t, "partMutations.properties")
	assertCollectionRecognized(t, "assemblyMutations.properties")
}

// 15. An actually unknown collection still fails deterministically with the
// stable diagnostic the migrated break fixture also locks.
func TestInternalCollectionRecognition_UnknownCollectionRejected(t *testing.T) {
	err := Validate(mutationCollectionContract(t, "partMutations.unsupported"))
	if err == nil {
		t.Fatal("expected unknown manifest mutation collection to fail validation")
	}
	want := `manifestCollection "partMutations.unsupported" is not supported`
	first := err.Error()
	if !contains(first, want) {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
	for i := 0; i < 5; i++ {
		if got := Validate(mutationCollectionContract(t, "partMutations.unsupported")).Error(); got != first {
			t.Fatalf("unstable unknown-collection diagnostic\nfirst: %q\ngot:   %q", first, got)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// 16. A malformed visibility mapping fails deterministically: the visibility
// contract requires object + visible fields exactly.
func TestInternalCollectionRecognition_MalformedVisibilityMappingFails(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.SemanticToManifest.Mutations = map[string]MutationMapping{
		"hide": {ManifestCollection: "partMutations.visibility", TargetField: "object", ValueField: "hidden", Value: boolPtr(false)},
	}
	err := Validate(contract)
	if err == nil {
		t.Fatal("expected malformed visibility mapping to fail")
	}
	if !contains(err.Error(), `valueField must equal "visible"`) {
		t.Fatalf("unexpected diagnostic: %v", err)
	}

	missingValue := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	missingValue.SemanticToManifest.Mutations = map[string]MutationMapping{
		"hide": {ManifestCollection: "partMutations.visibility", TargetField: "object", ValueField: "visible"},
	}
	if err := Validate(missingValue); err == nil || !contains(err.Error(), "value is required") {
		t.Fatalf("expected visibility mapping to require a value, got: %v", err)
	}
}

// 17. A malformed deletion mapping fails deterministically: the deletion
// contract forbids valueField and value.
func TestInternalCollectionRecognition_MalformedDeletionMappingFails(t *testing.T) {
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.SemanticToManifest.Mutations = map[string]MutationMapping{
		"delete": {ManifestCollection: "partMutations.deletion", TargetField: "object", ValueField: "deleted"},
	}
	if err := Validate(contract); err == nil || !contains(err.Error(), "valueField must not be set") {
		t.Fatalf("expected deletion mapping to forbid valueField, got: %v", err)
	}

	withValue := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	withValue.SemanticToManifest.Mutations = map[string]MutationMapping{
		"delete": {ManifestCollection: "partMutations.deletion", TargetField: "object", Value: boolPtr(true)},
	}
	if err := Validate(withValue); err == nil || !contains(err.Error(), "value must not be set") {
		t.Fatalf("expected deletion mapping to forbid value, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PART C — dedicated routed target-family projection (ProjectTargetMutationRouting)
// ---------------------------------------------------------------------------

// 18. suppress -> Suppression {Suppressed:true}, all other families empty.
func TestProjectTargetMutationRouting_SuppressToSuppressionTrue(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(rtm("suppress", "Pad")))
	if projected.Assembly != nil {
		t.Fatalf("expected empty assembly, got %#v", projected.Assembly)
	}
	want := []ManifestSuppressionMutation{{Object: "Pad", Suppressed: true}}
	if !reflect.DeepEqual(projected.Part.Suppression, want) {
		t.Fatalf("suppression\nwant: %#v\ngot:  %#v", want, projected.Part.Suppression)
	}
	if len(projected.Part.Visibility) != 0 || len(projected.Part.Deletion) != 0 ||
		len(projected.Part.Parameters) != 0 || len(projected.Part.Properties) != 0 {
		t.Fatalf("expected only suppression populated, got %#v", projected.Part)
	}
}

// 19. unsuppress -> Suppression {Suppressed:false} (same family, not a new one).
func TestProjectTargetMutationRouting_UnsuppressToSuppressionFalse(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(rtm("unsuppress", "Pocket")))
	want := []ManifestSuppressionMutation{{Object: "Pocket", Suppressed: false}}
	if !reflect.DeepEqual(projected.Part.Suppression, want) {
		t.Fatalf("suppression\nwant: %#v\ngot:  %#v", want, projected.Part.Suppression)
	}
	if len(projected.Part.Visibility) != 0 || len(projected.Part.Deletion) != 0 {
		t.Fatalf("unsuppress leaked into another family: %#v", projected.Part)
	}
}

// 20. hide -> Visibility {Visible:false}, and Suppression stays empty. Mandatory
// hide != suppress regression.
func TestProjectTargetMutationRouting_HideToVisibilityFalseNotSuppression(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(rtm("hide", "Body")))
	want := []ManifestVisibilityMutation{{Object: "Body", Visible: false}}
	if !reflect.DeepEqual(projected.Part.Visibility, want) {
		t.Fatalf("visibility\nwant: %#v\ngot:  %#v", want, projected.Part.Visibility)
	}
	if len(projected.Part.Suppression) != 0 {
		t.Fatalf("hide must not populate the suppression family: %#v", projected.Part.Suppression)
	}
}

// 21. unhide -> Visibility {Visible:true}, and Suppression stays empty.
// Mandatory unhide != unsuppress regression.
func TestProjectTargetMutationRouting_UnhideToVisibilityTrueNotUnsuppress(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(rtm("unhide", "Cover")))
	want := []ManifestVisibilityMutation{{Object: "Cover", Visible: true}}
	if !reflect.DeepEqual(projected.Part.Visibility, want) {
		t.Fatalf("visibility\nwant: %#v\ngot:  %#v", want, projected.Part.Visibility)
	}
	if len(projected.Part.Suppression) != 0 {
		t.Fatalf("unhide must not populate the suppression family: %#v", projected.Part.Suppression)
	}
}

// 22. delete -> Deletion {Object}, no boolean, no policy fields.
func TestProjectTargetMutationRouting_DeleteToDeletionObjectOnly(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(rtm("delete", "Chamfer")))
	want := []ManifestDeletionMutation{{Object: "Chamfer"}}
	if !reflect.DeepEqual(projected.Part.Deletion, want) {
		t.Fatalf("deletion\nwant: %#v\ngot:  %#v", want, projected.Part.Deletion)
	}
	if reflect.TypeOf(ManifestDeletionMutation{}).NumField() != 1 {
		t.Fatal("deletion record must carry no boolean or policy fields")
	}
}

// 23. The routed native Object string is copied verbatim: no semantic lookup,
// no rewriting, even for a nontrivial FreeCAD-style native name.
func TestProjectTargetMutationRouting_ObjectCopiedVerbatim(t *testing.T) {
	const native = "Body001.Pad.Face7"
	projected := projectRoutingOrFail(t, partRouting(rtm("suppress", native)))
	if projected.Part.Suppression[0].Object != native {
		t.Fatalf("Object not copied verbatim: want %q, got %q", native, projected.Part.Suppression[0].Object)
	}
}

// 24. Complete five-action matrix, one routed bucket. Expected family + boolean
// data is literal, never derived from production constants.
func TestProjectTargetMutationRouting_CompleteFiveActionMatrix(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(
		rtm("suppress", "S"),
		rtm("unsuppress", "US"),
		rtm("hide", "H"),
		rtm("unhide", "UH"),
		rtm("delete", "D"),
	))
	wantSuppression := []ManifestSuppressionMutation{{Object: "S", Suppressed: true}, {Object: "US", Suppressed: false}}
	wantVisibility := []ManifestVisibilityMutation{{Object: "H", Visible: false}, {Object: "UH", Visible: true}}
	wantDeletion := []ManifestDeletionMutation{{Object: "D"}}
	if !reflect.DeepEqual(projected.Part.Suppression, wantSuppression) {
		t.Fatalf("suppression\nwant: %#v\ngot:  %#v", wantSuppression, projected.Part.Suppression)
	}
	if !reflect.DeepEqual(projected.Part.Visibility, wantVisibility) {
		t.Fatalf("visibility\nwant: %#v\ngot:  %#v", wantVisibility, projected.Part.Visibility)
	}
	if !reflect.DeepEqual(projected.Part.Deletion, wantDeletion) {
		t.Fatalf("deletion\nwant: %#v\ngot:  %#v", wantDeletion, projected.Part.Deletion)
	}
}

// ---------------------------------------------------------------------------
// PART D — Part / Assembly isolation
// ---------------------------------------------------------------------------

// 25 - 30. Each family stays in its Task 9 destination bucket; the other bucket
// is nil.
func TestProjectTargetMutationRouting_DestinationIsolation(t *testing.T) {
	cases := []struct {
		name string
		op   string
	}{
		{"suppress", "suppress"},
		{"visibility", "hide"},
		{"deletion", "delete"},
	}
	for _, tc := range cases {
		t.Run("part/"+tc.name, func(t *testing.T) {
			projected := projectRoutingOrFail(t, partRouting(rtm(tc.op, "X")))
			if projected.Assembly != nil {
				t.Fatalf("part-only routed action produced assembly output: %#v", projected.Assembly)
			}
			if projected.Part == nil {
				t.Fatal("expected part output")
			}
		})
		t.Run("assembly/"+tc.name, func(t *testing.T) {
			projected := projectRoutingOrFail(t, assemblyRouting(rtm(tc.op, "X")))
			if projected.Part != nil {
				t.Fatalf("assembly-only routed action produced part output: %#v", projected.Part)
			}
			if projected.Assembly == nil {
				t.Fatal("expected assembly output")
			}
		})
	}
}

// 31. All five operations are destination-independent: run once under Part and
// once under Assembly; only the bucket differs, the family conversion is byte
// identical.
func TestProjectTargetMutationRouting_DestinationIndependentConversion(t *testing.T) {
	ops := []RoutedTargetMutation{
		rtm("suppress", "a"), rtm("unsuppress", "b"), rtm("hide", "c"), rtm("unhide", "d"), rtm("delete", "e"),
	}
	part := projectRoutingOrFail(t, &TargetMutationRouting{Part: ops}).Part
	assembly := projectRoutingOrFail(t, &TargetMutationRouting{Assembly: ops}).Assembly
	if !reflect.DeepEqual(part, assembly) {
		t.Fatalf("family conversion differs by destination\npart:     %#v\nassembly: %#v", part, assembly)
	}
}

// 32. No re-routing / no semantic resolution: the routed entries carry only an
// OperationKind + a native Object and no semantic metadata is available at all,
// yet projection succeeds purely from the Task 9 hand-off. The projection
// function's only input is *TargetMutationRouting.
func TestProjectTargetMutationRouting_NoSemanticResolutionInputs(t *testing.T) {
	fn := reflect.TypeOf(ProjectTargetMutationRouting)
	if fn.NumIn() != 1 || fn.In(0) != reflect.TypeOf((*TargetMutationRouting)(nil)) {
		t.Fatalf("ProjectTargetMutationRouting signature changed: %s", fn)
	}
	// Purely-routed input, no model / linkage / contract in scope.
	projected := projectRoutingOrFail(t, partRouting(rtm("suppress", "AlreadyRoutedNativeName")))
	if projected.Part.Suppression[0].Object != "AlreadyRoutedNativeName" {
		t.Fatalf("projection consulted something other than the routed entry: %#v", projected.Part)
	}
}

// ---------------------------------------------------------------------------
// PART E — ordering / duplicate preservation
// ---------------------------------------------------------------------------

// 33. Mixed Part family ordering: each family preserves input-relative order.
func TestProjectTargetMutationRouting_MixedPartFamilyOrdering(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(
		rtm("hide", "Cover"),
		rtm("suppress", "Pad"),
		rtm("delete", "Chamfer"),
		rtm("unhide", "Body"),
		rtm("unsuppress", "Pocket"),
		rtm("hide", "Bracket"),
	))
	wantSuppression := []ManifestSuppressionMutation{{Object: "Pad", Suppressed: true}, {Object: "Pocket", Suppressed: false}}
	wantVisibility := []ManifestVisibilityMutation{{Object: "Cover", Visible: false}, {Object: "Body", Visible: true}, {Object: "Bracket", Visible: false}}
	wantDeletion := []ManifestDeletionMutation{{Object: "Chamfer"}}
	if !reflect.DeepEqual(projected.Part.Suppression, wantSuppression) {
		t.Fatalf("suppression\nwant: %#v\ngot:  %#v", wantSuppression, projected.Part.Suppression)
	}
	if !reflect.DeepEqual(projected.Part.Visibility, wantVisibility) {
		t.Fatalf("visibility\nwant: %#v\ngot:  %#v", wantVisibility, projected.Part.Visibility)
	}
	if !reflect.DeepEqual(projected.Part.Deletion, wantDeletion) {
		t.Fatalf("deletion\nwant: %#v\ngot:  %#v", wantDeletion, projected.Part.Deletion)
	}
}

// 34. The same mixed matrix under Assembly.
func TestProjectTargetMutationRouting_MixedAssemblyFamilyOrdering(t *testing.T) {
	projected := projectRoutingOrFail(t, assemblyRouting(
		rtm("hide", "Cover"),
		rtm("suppress", "Pad"),
		rtm("delete", "Chamfer"),
		rtm("unhide", "Body"),
		rtm("unsuppress", "Pocket"),
	))
	wantSuppression := []ManifestSuppressionMutation{{Object: "Pad", Suppressed: true}, {Object: "Pocket", Suppressed: false}}
	wantVisibility := []ManifestVisibilityMutation{{Object: "Cover", Visible: false}, {Object: "Body", Visible: true}}
	if !reflect.DeepEqual(projected.Assembly.Suppression, wantSuppression) {
		t.Fatalf("suppression\nwant: %#v\ngot:  %#v", wantSuppression, projected.Assembly.Suppression)
	}
	if !reflect.DeepEqual(projected.Assembly.Visibility, wantVisibility) {
		t.Fatalf("visibility\nwant: %#v\ngot:  %#v", wantVisibility, projected.Assembly.Visibility)
	}
}

// 35 - 38. No lexical sorting: deliberately non-lexical native object names keep
// input order in every family.
func TestProjectTargetMutationRouting_NoLexicalSorting(t *testing.T) {
	suppression := projectRoutingOrFail(t, partRouting(
		rtm("suppress", "Zeta"), rtm("unsuppress", "Alpha"), rtm("suppress", "Middle"),
	)).Part.Suppression
	if got := []string{suppression[0].Object, suppression[1].Object, suppression[2].Object}; !reflect.DeepEqual(got, []string{"Zeta", "Alpha", "Middle"}) {
		t.Fatalf("suppression order was sorted: %v", got)
	}

	visibility := projectRoutingOrFail(t, partRouting(
		rtm("hide", "Zeta"), rtm("unhide", "Alpha"), rtm("hide", "Middle"),
	)).Part.Visibility
	if got := []string{visibility[0].Object, visibility[1].Object, visibility[2].Object}; !reflect.DeepEqual(got, []string{"Zeta", "Alpha", "Middle"}) {
		t.Fatalf("visibility order was sorted: %v", got)
	}

	deletion := projectRoutingOrFail(t, partRouting(
		rtm("delete", "Zeta"), rtm("delete", "Alpha"), rtm("delete", "Middle"),
	)).Part.Deletion
	if got := []string{deletion[0].Object, deletion[1].Object, deletion[2].Object}; !reflect.DeepEqual(got, []string{"Zeta", "Alpha", "Middle"}) {
		t.Fatalf("deletion order was sorted: %v", got)
	}
}

// 39. No operation sorting: repeated distinct operations within a bucket append
// in encounter order to the suppression family.
func TestProjectTargetMutationRouting_NoOperationSorting(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(
		rtm("unsuppress", "a"), rtm("suppress", "b"), rtm("unsuppress", "c"),
	))
	want := []ManifestSuppressionMutation{
		{Object: "a", Suppressed: false}, {Object: "b", Suppressed: true}, {Object: "c", Suppressed: false},
	}
	if !reflect.DeepEqual(projected.Part.Suppression, want) {
		t.Fatalf("operation order was sorted\nwant: %#v\ngot:  %#v", want, projected.Part.Suppression)
	}
}

// 40. Duplicate suppression object: suppress then unsuppress the same object
// yields two ordered entries, no merge.
func TestProjectTargetMutationRouting_DuplicateSuppressionObjectPreserved(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(rtm("suppress", "Pad"), rtm("unsuppress", "Pad")))
	want := []ManifestSuppressionMutation{{Object: "Pad", Suppressed: true}, {Object: "Pad", Suppressed: false}}
	if !reflect.DeepEqual(projected.Part.Suppression, want) {
		t.Fatalf("want: %#v\ngot:  %#v", want, projected.Part.Suppression)
	}
}

// 41. Duplicate visibility object: hide then unhide the same object yields two
// ordered entries.
func TestProjectTargetMutationRouting_DuplicateVisibilityObjectPreserved(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(rtm("hide", "Body"), rtm("unhide", "Body")))
	want := []ManifestVisibilityMutation{{Object: "Body", Visible: false}, {Object: "Body", Visible: true}}
	if !reflect.DeepEqual(projected.Part.Visibility, want) {
		t.Fatalf("want: %#v\ngot:  %#v", want, projected.Part.Visibility)
	}
}

// 42. Duplicate deletion entries are preserved verbatim: Task 10 has no
// dedup policy of its own. Runtime conflict validation belongs later.
func TestProjectTargetMutationRouting_DuplicateDeletionObjectPreserved(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(rtm("delete", "Optional"), rtm("delete", "Optional")))
	want := []ManifestDeletionMutation{{Object: "Optional"}, {Object: "Optional"}}
	if !reflect.DeepEqual(projected.Part.Deletion, want) {
		t.Fatalf("want: %#v\ngot:  %#v", want, projected.Part.Deletion)
	}
}

// 43. Cross-family same object: suppress + hide the same object are preserved on
// independent axes, no conflict rejection.
func TestProjectTargetMutationRouting_CrossFamilySameObjectPreserved(t *testing.T) {
	projected := projectRoutingOrFail(t, partRouting(rtm("suppress", "Body"), rtm("hide", "Body")))
	if !reflect.DeepEqual(projected.Part.Suppression, []ManifestSuppressionMutation{{Object: "Body", Suppressed: true}}) {
		t.Fatalf("unexpected suppression: %#v", projected.Part.Suppression)
	}
	if !reflect.DeepEqual(projected.Part.Visibility, []ManifestVisibilityMutation{{Object: "Body", Visible: false}}) {
		t.Fatalf("unexpected visibility: %#v", projected.Part.Visibility)
	}
}

// ---------------------------------------------------------------------------
// PART F — unsupported inputs
// ---------------------------------------------------------------------------

// 44 - 49. The dedicated routed-target projector rejects every operation outside
// the five-action target-action domain, deterministically, with nil output.
func TestProjectTargetMutationRouting_UnsupportedOperationsRejected(t *testing.T) {
	for _, op := range []string{"keep", "set_parameter", "set_property", "write_parameter", "write_metadata", ""} {
		t.Run("op="+op, func(t *testing.T) {
			err := projectRoutingExpectError(t, partRouting(rtm(op, "Pad")))
			want := `unsupported routed target mutation operation "` + op + `"`
			if err.Error() != want {
				t.Fatalf("unexpected error\nwant: %q\ngot:  %q", want, err.Error())
			}
		})
	}
}

// 50. The unsupported-operation diagnostic is exact and stable across repeats.
func TestProjectTargetMutationRouting_UnsupportedOperationDiagnosticDeterminism(t *testing.T) {
	const want = `unsupported routed target mutation operation "explode"`
	for i := 0; i < 10; i++ {
		if got := projectRoutingExpectError(t, partRouting(rtm("explode", "Pad"))).Error(); got != want {
			t.Fatalf("iteration %d: unstable diagnostic\nwant: %q\ngot:  %q", i, want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// PART G — nil / zero compatibility
// ---------------------------------------------------------------------------

// 51. nil / zero routing produces the canonical zero result (nil), never an
// allocated populated collection.
func TestProjectTargetMutationRouting_NilRoutingZeroBehavior(t *testing.T) {
	projected := projectRoutingOrFail(t, nil)
	if projected != nil {
		t.Fatalf("expected nil projection for nil routing, got %#v", projected)
	}
	projected = projectRoutingOrFail(t, &TargetMutationRouting{})
	if projected != nil {
		t.Fatalf("expected nil projection for zero routing, got %#v", projected)
	}
}

// 52. Empty (non-nil) Part + Assembly buckets are also zero.
func TestProjectTargetMutationRouting_EmptyBucketsZeroBehavior(t *testing.T) {
	projected := projectRoutingOrFail(t, &TargetMutationRouting{
		Assembly: []RoutedTargetMutation{},
		Part:     []RoutedTargetMutation{},
	})
	if projected != nil {
		t.Fatalf("expected nil projection for empty buckets, got %#v", projected)
	}
}

// ---------------------------------------------------------------------------
// PART J — mature semantic-map projection extension (ProjectMutations)
// ---------------------------------------------------------------------------

// matureVisibilityDeletionContract clones the smoke contract and adds
// hide/unhide/delete mutation mappings so the mature ProjectMutations path can
// project the visibility and deletion families.
func matureVisibilityDeletionContract(t *testing.T) *SemanticMap {
	t.Helper()
	contract := cloneSemanticMap(loadFixture(t, "smoke/freecad-default"))
	contract.SemanticToManifest.Mutations["hide"] = MutationMapping{ManifestCollection: "partMutations.visibility", TargetField: "object", ValueField: "visible", Value: boolPtr(false)}
	contract.SemanticToManifest.Mutations["unhide"] = MutationMapping{ManifestCollection: "partMutations.visibility", TargetField: "object", ValueField: "visible", Value: boolPtr(true)}
	contract.SemanticToManifest.Mutations["delete"] = MutationMapping{ManifestCollection: "partMutations.deletion", TargetField: "object"}
	return contract
}

func matureProjectMutations(t *testing.T, mutations ...semantic.MutationIntent) *ManifestMutationProjection {
	t.Helper()
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)
	projection, err := ProjectMutations(&MutationProjectionRequest{
		Model:           model,
		Intent:          outputMutationProjectionIntentWithMutations(mutations...),
		ResolvedValues:  map[string]any{},
		Contract:        matureVisibilityDeletionContract(t),
		IdentityLinkage: linkage,
	})
	if err != nil {
		t.Fatalf("ProjectMutations returned error: %v", err)
	}
	return projection
}

func matureIntent(op, entityKind, semanticID, scope string) semantic.MutationIntent {
	return semantic.MutationIntent{
		OperationKind:    op,
		TargetEntityKind: entityKind,
		TargetSemanticID: semanticID,
		Scope:            scope,
		ValueSource:      semantic.MutationValueSource{Kind: "dsl_parameter"},
	}
}

// 77 / 78. Mature hide / unhide project to the internal Visibility family with
// the correct boolean.
func TestProjectMutations_MatureHideAndUnhideToVisibility(t *testing.T) {
	hide := matureProjectMutations(t, matureIntent("hide", "feature", "feat.part", "part"))
	if !reflect.DeepEqual(hide.Part.Visibility, []ManifestVisibilityMutation{{Object: "Pocket", Visible: false}}) {
		t.Fatalf("mature hide visibility mismatch: %#v", hide.Part)
	}
	if len(hide.Part.Suppression) != 0 {
		t.Fatalf("mature hide leaked into suppression: %#v", hide.Part.Suppression)
	}

	unhide := matureProjectMutations(t, matureIntent("unhide", "feature", "feat.part", "part"))
	if !reflect.DeepEqual(unhide.Part.Visibility, []ManifestVisibilityMutation{{Object: "Pocket", Visible: true}}) {
		t.Fatalf("mature unhide visibility mismatch: %#v", unhide.Part)
	}
}

// 79. Mature delete projects to the internal Deletion family.
func TestProjectMutations_MatureDeleteToDeletion(t *testing.T) {
	projection := matureProjectMutations(t, matureIntent("delete", "feature", "feat.part", "part"))
	if !reflect.DeepEqual(projection.Part.Deletion, []ManifestDeletionMutation{{Object: "Pocket"}}) {
		t.Fatalf("mature delete deletion mismatch: %#v", projection.Part)
	}
}

// 80 / 81. Mature suppress / unsuppress remain the suppression family with the
// correct boolean (regression).
func TestProjectMutations_MatureSuppressUnsuppressRegression(t *testing.T) {
	suppress := matureProjectMutations(t, matureIntent("suppress", "feature", "feat.part", "part"))
	if !reflect.DeepEqual(suppress.Part.Suppression, []ManifestSuppressionMutation{{Object: "Pocket", Suppressed: true}}) {
		t.Fatalf("mature suppress mismatch: %#v", suppress.Part)
	}
	unsuppress := matureProjectMutations(t, matureIntent("unsuppress", "feature", "feat.part", "part"))
	if !reflect.DeepEqual(unsuppress.Part.Suppression, []ManifestSuppressionMutation{{Object: "Pocket", Suppressed: false}}) {
		t.Fatalf("mature unsuppress mismatch: %#v", unsuppress.Part)
	}
}

// 82 / 83. Mature visibility placement follows the intent destination: feature
// part-scope -> Part, assembly kind -> Assembly.
func TestProjectMutations_MatureVisibilityPlacement(t *testing.T) {
	part := matureProjectMutations(t, matureIntent("hide", "feature", "feat.part", "part"))
	if part.Part == nil || part.Assembly != nil {
		t.Fatalf("expected part-only visibility placement, got %#v", part)
	}
	assembly := matureProjectMutations(t, matureIntent("hide", "feature", "feat.assembly", "assembly"))
	if assembly.Assembly == nil || assembly.Part != nil {
		t.Fatalf("expected assembly-only visibility placement, got %#v", assembly)
	}
	if !reflect.DeepEqual(assembly.Assembly.Visibility, []ManifestVisibilityMutation{{Object: "Bracket-1", Visible: false}}) {
		t.Fatalf("assembly visibility mismatch: %#v", assembly.Assembly)
	}
}

// 84 / 85. Mature deletion placement follows the intent destination.
func TestProjectMutations_MatureDeletionPlacement(t *testing.T) {
	part := matureProjectMutations(t, matureIntent("delete", "feature", "feat.part", "part"))
	if part.Part == nil || part.Assembly != nil {
		t.Fatalf("expected part-only deletion placement, got %#v", part)
	}
	assembly := matureProjectMutations(t, matureIntent("delete", "feature", "feat.assembly", "assembly"))
	if assembly.Assembly == nil || assembly.Part != nil {
		t.Fatalf("expected assembly-only deletion placement, got %#v", assembly)
	}
	if !reflect.DeepEqual(assembly.Assembly.Deletion, []ManifestDeletionMutation{{Object: "Bracket-1"}}) {
		t.Fatalf("assembly deletion mismatch: %#v", assembly.Assembly)
	}
}

// 86. Mature ProjectMutations retains its established canonical (lexical)
// sorting for the new families — Task 9/10 private encounter-order semantics are
// NOT forced onto this path.
func TestProjectMutations_MatureVisibilityDeletionCanonicalSorting(t *testing.T) {
	model := outputMutationProjectionModel()
	linkage := mustLinkOutputMutationProjectionModel(t, model)
	contract := matureVisibilityDeletionContract(t)

	first, err := ProjectMutations(&MutationProjectionRequest{
		Model: model, Contract: contract, IdentityLinkage: linkage, ResolvedValues: map[string]any{},
		Intent: outputMutationProjectionIntentWithMutations(
			matureIntent("hide", "feature", "feat.part", "part"),
			matureIntent("hide", "feature", "feat.assembly", "assembly"),
		),
	})
	if err != nil {
		t.Fatalf("first ProjectMutations error: %v", err)
	}
	second, err := ProjectMutations(&MutationProjectionRequest{
		Model: outputMutationProjectionModel(), Contract: contract, IdentityLinkage: linkage, ResolvedValues: map[string]any{},
		Intent: outputMutationProjectionIntentWithMutations(
			matureIntent("hide", "feature", "feat.assembly", "assembly"),
			matureIntent("hide", "feature", "feat.part", "part"),
		),
	})
	if err != nil {
		t.Fatalf("second ProjectMutations error: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("mature visibility projection not canonical across input orders\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

// ---------------------------------------------------------------------------
// PART J (reduced execution projection preservation)
// ---------------------------------------------------------------------------

// 87 - 89. cloneManifestMutationCollection (reduced execution projection)
// preserves Visibility and Deletion alongside the existing families.
func TestReducedExecutionProjection_PreservesAllFamilies(t *testing.T) {
	projected := &ManifestMutationProjection{
		Part: &ManifestMutationCollection{
			Parameters:  []ManifestParameterMutation{{Object: "GroupB", Property: "width", ValueParam: "width", Type: "number", Unit: "mm"}},
			Properties:  []ManifestPropertyMutation{{Object: "PartB", Property: "pn", Value: "PN-1"}},
			Suppression: []ManifestSuppressionMutation{{Object: "Pocket", Suppressed: true}},
			Visibility:  []ManifestVisibilityMutation{{Object: "Body", Visible: false}},
			Deletion:    []ManifestDeletionMutation{{Object: "Chamfer"}},
		},
	}
	reduced := newReducedExecutionProjection(nil, nil, projected)
	if reduced.PartMutations == nil {
		t.Fatal("expected part mutations to survive reduction")
	}
	if !reflect.DeepEqual(reduced.PartMutations.Visibility, projected.Part.Visibility) {
		t.Fatalf("visibility not preserved: %#v", reduced.PartMutations.Visibility)
	}
	if !reflect.DeepEqual(reduced.PartMutations.Deletion, projected.Part.Deletion) {
		t.Fatalf("deletion not preserved: %#v", reduced.PartMutations.Deletion)
	}
	if len(reduced.PartMutations.Parameters) != 1 || len(reduced.PartMutations.Properties) != 1 || len(reduced.PartMutations.Suppression) != 1 {
		t.Fatalf("existing families not preserved: %#v", reduced.PartMutations)
	}
}

// 76. A visibility-only / deletion-only collection still reduces (is not dropped
// as "empty" now that the two new families count).
func TestReducedExecutionProjection_VisibilityOnlyCollectionSurvives(t *testing.T) {
	reduced := newReducedExecutionProjection(nil, nil, &ManifestMutationProjection{
		Part: &ManifestMutationCollection{Visibility: []ManifestVisibilityMutation{{Object: "Body", Visible: true}}},
	})
	if reduced.PartMutations == nil || len(reduced.PartMutations.Visibility) != 1 {
		t.Fatalf("visibility-only collection was dropped: %#v", reduced.PartMutations)
	}
}

// ---------------------------------------------------------------------------
// PART K — legacy contract migration regressions
// ---------------------------------------------------------------------------

// 90. The supported-operation migration is intentional and permanent: no
// expectation still treats unsuppress / unhide as unsupported.
func TestSupportedOperations_UnsuppressUnhideAreSupported(t *testing.T) {
	for _, op := range []string{"unsuppress", "unhide"} {
		if _, ok := supportedOperationNames[op]; !ok {
			t.Fatalf("operation %q must be a permanent supported operation", op)
		}
	}
}

// 91 / 92. Positive validation: partMutations.visibility and
// partMutations.deletion are now recognized (previously visibility was used as
// the invalid-collection example).
func TestSupportedCollections_VisibilityDeletionArePositive(t *testing.T) {
	assertCollectionRecognized(t, "partMutations.visibility")
	assertCollectionRecognized(t, "partMutations.deletion")
}

// 93. The migrated invalid fixture still proves an actually unknown collection
// fails; visibility is no longer used as the invalid example.
func TestBreakFixture_InvalidMutationCollectionStillUnknown(t *testing.T) {
	contract := rawFixture(t, "break/invalid-mutation-manifest-collection")
	err := Validate(contract)
	if err == nil {
		t.Fatal("expected the invalid-mutation-manifest-collection fixture to fail validation")
	}
	if !contains(err.Error(), `manifestCollection "partMutations.unsupported" is not supported`) {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
	if contains(err.Error(), `"partMutations.visibility" is not supported`) {
		t.Fatalf("visibility must no longer be an invalid-collection example: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PART M — determinism
// ---------------------------------------------------------------------------

// 101. Mixed Part + Assembly five-operation projection is identical 10/10.
func TestProjectTargetMutationRouting_MixedProjectionDeterminism(t *testing.T) {
	routing := func() *TargetMutationRouting {
		return &TargetMutationRouting{
			Part: []RoutedTargetMutation{
				rtm("hide", "Cover"), rtm("suppress", "Pad"), rtm("delete", "Chamfer"),
				rtm("unhide", "Body"), rtm("unsuppress", "Pocket"),
			},
			Assembly: []RoutedTargetMutation{
				rtm("suppress", "Sub"), rtm("hide", "Root"), rtm("delete", "Old"),
				rtm("unsuppress", "Mid"), rtm("unhide", "Top"),
			},
		}
	}
	first := projectRoutingOrFail(t, routing())
	for i := 0; i < 10; i++ {
		if got := projectRoutingOrFail(t, routing()); !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: non-deterministic projection\nfirst: %#v\ngot:   %#v", i, first, got)
		}
	}
}

// 103. The mature visibility/deletion projection is deterministic across
// repeats.
func TestProjectMutations_MatureVisibilityDeletionDeterminism(t *testing.T) {
	first := matureProjectMutations(t,
		matureIntent("hide", "feature", "feat.part", "part"),
		matureIntent("delete", "feature", "feat.assembly", "assembly"),
	)
	for i := 0; i < 10; i++ {
		got := matureProjectMutations(t,
			matureIntent("hide", "feature", "feat.part", "part"),
			matureIntent("delete", "feature", "feat.assembly", "assembly"),
		)
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: non-deterministic mature projection\nfirst: %#v\ngot:   %#v", i, first, got)
		}
	}
}
