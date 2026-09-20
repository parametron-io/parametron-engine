package verification

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
)

const targetStateNoRequestCanonical = `{"schemaVersion":"1.0","observe":{"components":false,"parameters":false,"metadata":true,"references":true},"observationContext":{"parameters":[]},"expected":{"components":[],"parameters":[{"id":"par.root.height","name":"height","type":"number","unit":"mm","value":5},{"id":"par.root.length","name":"length","type":"number","unit":"mm","value":35}],"metadata":[{"key":"working_copy_sha256","value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"references":[{"kind":"working_copy_path","name":"/tmp/out/_working/widget.FCStd"}]},"checks":{"components":{"enabled":false},"parameters":{"enabled":false},"metadata":{"enabled":true},"references":{"enabled":true}}}`

// targetStateRequestJSON builds a schema 1.0 verification payload with the
// supplied observe.targetState fragment and observationContext.targetState
// fragment. Empty fragments are omitted entirely.
func targetStateRequestJSON(observeTargetState, contextTargetState string) string {
	observe := `"components":false,"parameters":false,"metadata":true,"references":true`
	if observeTargetState != "" {
		observe += `,"targetState":` + observeTargetState
	}
	context := `"parameters":[]`
	if contextTargetState != "" {
		context += `,"targetState":` + contextTargetState
	}
	return `{"schemaVersion":"1.0","observe":{` + observe + `},"observationContext":{` + context + `},"expected":{"components":[],"parameters":[],"metadata":[],"references":[]},"checks":{"components":{"enabled":false},"parameters":{"enabled":false},"metadata":{"enabled":true},"references":{"enabled":true}}}`
}

func targetStateRequestWith(suppression, visibility, existence string) string {
	return targetStateRequestJSON("true", `{"suppression":`+suppression+`,"visibility":`+visibility+`,"existence":`+existence+`}`)
}

func deriveTargetStateContract(t *testing.T, assembly, part *planner.ExportManifestMutationCollection) *Contract {
	t.Helper()
	root := t.TempDir()
	workingCopy := filepath.Join(root, "attempt", "source", "widget.FCStd")
	writeFile(t, workingCopy, "working copy bytes")
	manifest := planner.WriteExportManifestPayload{
		SchemaVersion:     planner.ExportManifestSchemaVersion,
		AssemblyMutations: assembly,
		PartMutations:     part,
	}
	contract, err := DeriveFromManifestAndWorkingCopy(manifest, workingCopy)
	if err != nil {
		t.Fatalf("DeriveFromManifestAndWorkingCopy returned error: %v", err)
	}
	return contract
}

func mustCanonical(t *testing.T, contract *Contract) []byte {
	t.Helper()
	out, err := CanonicalJSON(contract)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}
	return out
}

func assertTargetStateValidationError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation error containing %q", want)
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation classification, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %v", want, err)
	}
}

func TestTargetStateRequest_AbsentKeepsSchema10CanonicalBytes(t *testing.T) {
	parsed, err := Parse([]byte(validVerificationJSON()))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Observe.TargetState || parsed.ObservationContext.TargetState != nil {
		t.Fatalf("no-target-state contract must not carry target state: %+v", parsed)
	}
	got := mustCanonical(t, parsed)
	if string(got) != targetStateNoRequestCanonical {
		t.Fatalf("canonical bytes changed for no-target-state contract\nwant: %s\ngot:  %s", targetStateNoRequestCanonical, got)
	}
	if bytes.Contains(got, []byte("targetState")) {
		t.Fatalf("canonical JSON must omit targetState: %s", got)
	}
}

func TestTargetStateRequest_ExplicitObserveFalseWithoutContextIsOrdinary(t *testing.T) {
	parsed, err := Parse([]byte(targetStateRequestJSON("false", "")))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := mustCanonical(t, parsed)
	if bytes.Contains(got, []byte("targetState")) {
		t.Fatalf("explicit observe.targetState=false must canonicalize to omission: %s", got)
	}
}

func TestTargetStateRequest_DerivationWithoutTargetMutationsOmitsTargetState(t *testing.T) {
	contract := deriveTargetStateContract(t, nil, nil)
	if contract.Observe.TargetState || contract.ObservationContext.TargetState != nil {
		t.Fatalf("derivation without target mutations must not request target state: %+v", contract)
	}
	// Non-target mutation families alone must not request target state either.
	contract = deriveTargetStateContract(t,
		&planner.ExportManifestMutationCollection{
			Properties: []planner.ExportManifestPropertyMutation{{Object: "Box", Property: "Label", Value: "x"}},
		}, nil)
	if contract.Observe.TargetState || contract.ObservationContext.TargetState != nil {
		t.Fatalf("property-only mutations must not request target state: %+v", contract)
	}
	if bytes.Contains(mustCanonical(t, contract), []byte("targetState")) {
		t.Fatal("canonical JSON must omit targetState")
	}
}

func TestTargetStateRequest_DerivesSuppressionOnlyForBothIntents(t *testing.T) {
	contract := deriveTargetStateContract(t,
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{
				{Object: "Sketch", Suppressed: true},
				{Object: "Pad", Suppressed: false},
			},
		}, nil)
	if !contract.Observe.TargetState {
		t.Fatal("observe.targetState must be true")
	}
	got := requestFragment(t, mustCanonical(t, contract))
	want := `"targetState":{"suppression":[{"destination":"assembly","object":"Pad"},{"destination":"assembly","object":"Sketch"}],"visibility":[],"existence":[]}`
	if !bytes.Contains(got, []byte(want)) {
		t.Fatalf("missing suppression-only request\nwant fragment: %s\ngot: %s", want, got)
	}
	// Requested boolean intent must not leak into the request as pretend evidence.
	for _, forbidden := range []string{`"suppressed"`, `"value"`, `"status"`, `"visible"`} {
		if bytes.Contains(got, []byte(forbidden)) {
			t.Fatalf("request leaked %s: %s", forbidden, got)
		}
	}
}

func TestTargetStateRequest_DerivesVisibilityOnlyForBothIntents(t *testing.T) {
	contract := deriveTargetStateContract(t, nil,
		&planner.ExportManifestMutationCollection{
			Visibility: []planner.ExportManifestVisibilityMutation{
				{Object: "Shown", Visible: true},
				{Object: "Hidden", Visible: false},
			},
		})
	got := requestFragment(t, mustCanonical(t, contract))
	want := `"targetState":{"suppression":[],"visibility":[{"destination":"part","object":"Hidden"},{"destination":"part","object":"Shown"}],"existence":[]}`
	if !bytes.Contains(got, []byte(want)) {
		t.Fatalf("missing visibility-only request\nwant fragment: %s\ngot: %s", want, got)
	}
	for _, forbidden := range []string{`"visible"`, `"value"`, `"status"`} {
		if bytes.Contains(got, []byte(forbidden)) {
			t.Fatalf("request leaked %s: %s", forbidden, got)
		}
	}
}

func TestTargetStateRequest_DerivesExistenceOnlyFromDeletion(t *testing.T) {
	contract := deriveTargetStateContract(t,
		&planner.ExportManifestMutationCollection{
			Deletion: []planner.ExportManifestDeletionMutation{{Object: "Doomed"}},
		}, nil)
	got := mustCanonical(t, contract)
	want := `"targetState":{"suppression":[],"visibility":[],"existence":[{"destination":"assembly","object":"Doomed"}]}`
	if !bytes.Contains(got, []byte(want)) {
		t.Fatalf("missing existence-only request\nwant fragment: %s\ngot: %s", want, got)
	}
	if bytes.Contains(got, []byte(`"status"`)) || bytes.Contains(got, []byte(`"absent"`)) {
		t.Fatalf("deletion request must not model success: %s", got)
	}
}

func TestTargetStateRequest_DerivesCombinedFamiliesAcrossDestinations(t *testing.T) {
	contract := deriveTargetStateContract(t,
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Shared", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "AsmVis", Visible: false}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "AsmDel"}},
		},
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Shared", Suppressed: false}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "PartVis", Visible: true}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "PartDel"}},
		})
	want := TargetStateRequest{
		Suppression: []TargetIdentity{
			{Destination: "assembly", Object: "Shared"},
			{Destination: "part", Object: "Shared"},
		},
		Visibility: []TargetIdentity{
			{Destination: "assembly", Object: "AsmVis"},
			{Destination: "part", Object: "PartVis"},
		},
		Existence: []TargetIdentity{
			{Destination: "assembly", Object: "AsmDel"},
			{Destination: "part", Object: "PartDel"},
		},
	}
	got := contract.ObservationContext.TargetState
	if got == nil {
		t.Fatal("expected target state request")
	}
	if !equalTargetIdentities(got.Suppression, want.Suppression) ||
		!equalTargetIdentities(got.Visibility, want.Visibility) ||
		!equalTargetIdentities(got.Existence, want.Existence) {
		t.Fatalf("unexpected derived request\nwant: %+v\ngot:  %+v", want, *got)
	}
	canonical := mustCanonical(t, contract)
	wantFragment := `"targetState":{"suppression":[{"destination":"assembly","object":"Shared"},{"destination":"part","object":"Shared"}],"visibility":[{"destination":"assembly","object":"AsmVis"},{"destination":"part","object":"PartVis"}],"existence":[{"destination":"assembly","object":"AsmDel"},{"destination":"part","object":"PartDel"}]}`
	if !bytes.Contains(canonical, []byte(wantFragment)) {
		t.Fatalf("unexpected canonical combined request\nwant fragment: %s\ngot: %s", wantFragment, canonical)
	}
}

func TestTargetStateRequest_SameObjectAcrossFamiliesIsNotAutomaticallyInvalid(t *testing.T) {
	contract := deriveTargetStateContract(t,
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Same", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "Same", Visible: false}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "Same"}},
		}, nil)
	request := contract.ObservationContext.TargetState
	if request == nil || len(request.Suppression) != 1 || len(request.Visibility) != 1 || len(request.Existence) != 1 {
		t.Fatalf("expected one entry per family, got %+v", request)
	}
	if err := Validate(contract); err != nil {
		t.Fatalf("cross-family same-object request must validate: %v", err)
	}
}

func TestTargetStateRequest_DerivationOrderIsIndependentOfMutationInputOrder(t *testing.T) {
	forward := deriveTargetStateContract(t,
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "A", Suppressed: true}, {Object: "B", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "A", Visible: true}, {Object: "B", Visible: false}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "A"}, {Object: "B"}},
		},
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "A", Suppressed: true}, {Object: "B", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "A", Visible: true}, {Object: "B", Visible: false}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "A"}, {Object: "B"}},
		})
	reversed := deriveTargetStateContract(t,
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "B", Suppressed: true}, {Object: "A", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "B", Visible: false}, {Object: "A", Visible: true}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "B"}, {Object: "A"}},
		},
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "B", Suppressed: true}, {Object: "A", Suppressed: true}},
			Visibility:  []planner.ExportManifestVisibilityMutation{{Object: "B", Visible: false}, {Object: "A", Visible: true}},
			Deletion:    []planner.ExportManifestDeletionMutation{{Object: "B"}, {Object: "A"}},
		})
	a, b := requestFragment(t, mustCanonical(t, forward)), requestFragment(t, mustCanonical(t, reversed))
	if !bytes.Equal(a, b) {
		t.Fatalf("derivation must be order-independent\nforward:  %s\nreversed: %s", a, b)
	}
	// Derived in-memory request itself is ordered by destination then object.
	got := forward.ObservationContext.TargetState.Suppression
	want := []TargetIdentity{{"assembly", "A"}, {"assembly", "B"}, {"part", "A"}, {"part", "B"}}
	if !equalTargetIdentities(got, want) {
		t.Fatalf("unexpected derived order: %+v", got)
	}
}

func TestTargetStateRequest_CanonicalOrderingIsDestinationThenObject(t *testing.T) {
	unordered := targetStateRequestWith(
		`[{"destination":"part","object":"A"},{"destination":"assembly","object":"Z"},{"destination":"assembly","object":"B"}]`,
		`[{"destination":"part","object":"b"},{"destination":"part","object":"B"}]`,
		`[{"destination":"part","object":"Q"},{"destination":"assembly","object":"Q"}]`,
	)
	ordered := targetStateRequestWith(
		`[{"destination":"assembly","object":"B"},{"destination":"assembly","object":"Z"},{"destination":"part","object":"A"}]`,
		`[{"destination":"part","object":"B"},{"destination":"part","object":"b"}]`,
		`[{"destination":"assembly","object":"Q"},{"destination":"part","object":"Q"}]`,
	)
	first, err := Parse([]byte(unordered))
	if err != nil {
		t.Fatalf("Parse(unordered): %v", err)
	}
	second, err := Parse([]byte(ordered))
	if err != nil {
		t.Fatalf("Parse(ordered): %v", err)
	}
	a, b := mustCanonical(t, first), mustCanonical(t, second)
	if !bytes.Equal(a, b) {
		t.Fatalf("equivalent requests must canonicalize identically\nA: %s\nB: %s", a, b)
	}
	want := `"targetState":{"suppression":[{"destination":"assembly","object":"B"},{"destination":"assembly","object":"Z"},{"destination":"part","object":"A"}],"visibility":[{"destination":"part","object":"B"},{"destination":"part","object":"b"}],"existence":[{"destination":"assembly","object":"Q"},{"destination":"part","object":"Q"}]}`
	if !bytes.Contains(a, []byte(want)) {
		t.Fatalf("unexpected canonical ordering\nwant fragment: %s\ngot: %s", want, a)
	}
	// Programmatic construction in yet another order is also stable.
	programmatic := validContract()
	programmatic.Observe.TargetState = true
	programmatic.ObservationContext.TargetState = &TargetStateRequest{
		Suppression: []TargetIdentity{{"part", "A"}, {"assembly", "B"}, {"assembly", "Z"}},
		Visibility:  []TargetIdentity{{"part", "b"}, {"part", "B"}},
		Existence:   []TargetIdentity{{"part", "Q"}, {"assembly", "Q"}},
	}
	programmatic.Expected = first.Expected
	programmatic.Checks = first.Checks
	if got := mustCanonical(t, programmatic); !bytes.Equal(got, a) {
		t.Fatalf("programmatic ordering differs\nwant: %s\ngot:  %s", a, got)
	}
}

func TestTargetStateRequest_RoundTripIsByteStable(t *testing.T) {
	payload := targetStateRequestWith(
		`[{"destination":"assembly","object":"Sk"}]`,
		`[{"destination":"part","object":"Body"}]`,
		`[{"destination":"assembly","object":"Gone"},{"destination":"part","object":"Gone"}]`,
	)
	parsed, err := Parse([]byte(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	first := mustCanonical(t, parsed)
	reparsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse(canonical): %v", err)
	}
	second := mustCanonical(t, reparsed)
	if !bytes.Equal(first, second) {
		t.Fatalf("round trip changed bytes\nfirst:  %s\nsecond: %s", first, second)
	}
	if !bytes.Contains(first, []byte(`"observe":{"components":false,"parameters":false,"metadata":true,"references":true,"targetState":true}`)) {
		t.Fatalf("observe.targetState must serialize as true: %s", first)
	}
	if !parsed.Observe.TargetState || parsed.ObservationContext.TargetState == nil {
		t.Fatalf("parsed typed value lost target state: %+v", parsed)
	}
}

func TestTargetStateRequest_DerivedContractRoundTripsThroughFile(t *testing.T) {
	contract := deriveTargetStateContract(t,
		&planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "S", Suppressed: true}},
		},
		&planner.ExportManifestMutationCollection{
			Deletion: []planner.ExportManifestDeletionMutation{{Object: "D"}},
		})
	first := mustCanonical(t, contract)
	parsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if second := mustCanonical(t, parsed); !bytes.Equal(first, second) {
		t.Fatalf("derived contract round trip changed bytes\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestTargetStateRequest_DuplicateIdentitiesRejectedPerFamily(t *testing.T) {
	dup := `[{"destination":"part","object":"X"},{"destination":"part","object":"X"}]`
	for _, family := range []string{"suppression", "visibility", "existence"} {
		t.Run(family, func(t *testing.T) {
			args := map[string]string{"suppression": `[]`, "visibility": `[]`, "existence": `[]`}
			args[family] = dup
			_, err := Parse([]byte(targetStateRequestWith(args["suppression"], args["visibility"], args["existence"])))
			assertTargetStateValidationError(t, err, "observationContext.targetState."+family+"[1] duplicates observationContext.targetState."+family+"[0]")
		})
	}
	// Same object in different destinations is distinct, not a duplicate.
	if _, err := Parse([]byte(targetStateRequestWith(`[{"destination":"assembly","object":"X"},{"destination":"part","object":"X"}]`, `[]`, `[]`))); err != nil {
		t.Fatalf("distinct destinations must not collide: %v", err)
	}
}

func TestTargetStateRequest_DerivedDuplicateIdentityIsRejectedByDerivation(t *testing.T) {
	root := t.TempDir()
	workingCopy := filepath.Join(root, "attempt", "source", "widget.FCStd")
	writeFile(t, workingCopy, "working copy bytes")
	manifest := planner.WriteExportManifestPayload{
		SchemaVersion: planner.ExportManifestSchemaVersion,
		AssemblyMutations: &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Dup", Suppressed: true}, {Object: "Dup", Suppressed: false}},
		},
	}
	contract, err := DeriveFromManifestAndWorkingCopy(manifest, workingCopy)
	if contract != nil {
		t.Fatalf("derivation must not return a contract with duplicate identities: %+v", contract)
	}
	assertTargetStateValidationError(t, err, "observationContext.targetState.suppression[1] duplicates observationContext.targetState.suppression[0]")
}

func TestTargetStateRequest_MalformedTargetIdentityRejected(t *testing.T) {
	cases := []struct {
		name   string
		entry  string
		want   string
		isType bool
	}{
		{name: "invalid destination", entry: `{"destination":"sketch","object":"X"}`, want: "destination must be one of"},
		{name: "uppercase destination", entry: `{"destination":"Assembly","object":"X"}`, want: "destination must be one of"},
		{name: "empty destination", entry: `{"destination":"","object":"X"}`, want: "[0].destination"},
		{name: "empty object", entry: `{"destination":"part","object":""}`, want: "[0].object"},
		{name: "whitespace-only object", entry: `{"destination":"part","object":"   "}`, want: "[0].object"},
		{name: "leading whitespace", entry: `{"destination":"part","object":" X"}`, want: "object must be a non-blank exact native object name"},
		{name: "trailing whitespace", entry: `{"destination":"part","object":"X\n"}`, want: "object must be a non-blank exact native object name"},
		{name: "embedded NUL", entry: `{"destination":"part","object":"X\u0000Y"}`, want: "object must be a non-blank exact native object name"},
		{name: "missing destination", entry: `{"object":"X"}`, want: "destination"},
		{name: "missing object", entry: `{"destination":"part"}`, want: "object"},
		{name: "numeric destination", entry: `{"destination":1,"object":"X"}`, want: "destination"},
		{name: "numeric object", entry: `{"destination":"part","object":1}`, want: "object"},
		{name: "null object", entry: `{"destination":"part","object":null}`, want: "object"},
		{name: "unknown identity field", entry: `{"destination":"part","object":"X","id":"sem.x"}`, want: `unknown field "id"`},
		{name: "entry not an object", entry: `"part/X"`, want: "must be an object"},
	}
	for _, family := range []string{"suppression", "visibility", "existence"} {
		for _, tc := range cases {
			t.Run(family+"/"+tc.name, func(t *testing.T) {
				args := map[string]string{"suppression": `[]`, "visibility": `[]`, "existence": `[]`}
				args[family] = `[` + tc.entry + `]`
				_, err := Parse([]byte(targetStateRequestWith(args["suppression"], args["visibility"], args["existence"])))
				assertTargetStateValidationError(t, err, tc.want)
				if !strings.Contains(err.Error(), "observationContext.targetState."+family+"[0]") {
					t.Fatalf("error must locate the entry, got %v", err)
				}
			})
		}
	}
}

func TestTargetStateRequest_ProgrammaticMalformedIdentityRejected(t *testing.T) {
	for name, identity := range map[string]TargetIdentity{
		"bad destination": {Destination: "root", Object: "X"},
		"empty object":    {Destination: "part", Object: ""},
		"blank object":    {Destination: "part", Object: " \t"},
		"padded object":   {Destination: "part", Object: "X "},
		"NUL object":      {Destination: "assembly", Object: "a\x00b"},
	} {
		t.Run(name, func(t *testing.T) {
			contract := validContract()
			contract.Observe.TargetState = true
			contract.ObservationContext.TargetState = &TargetStateRequest{
				Suppression: []TargetIdentity{},
				Visibility:  []TargetIdentity{identity},
				Existence:   []TargetIdentity{},
			}
			assertTargetStateValidationError(t, Validate(contract), "observationContext.targetState.visibility[0]")
			if _, err := CanonicalJSON(contract); err == nil {
				t.Fatal("CanonicalJSON must reject malformed identity")
			}
		})
	}
}

func TestTargetStateRequest_StructuralValidation(t *testing.T) {
	one := `[{"destination":"part","object":"X"}]`
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "observe true without context",
			payload: targetStateRequestJSON("true", ""),
			want:    "observe.targetState must be true exactly when observationContext.targetState is present",
		},
		{
			name:    "context present with observe absent",
			payload: targetStateRequestJSON("", `{"suppression":`+one+`,"visibility":[],"existence":[]}`),
			want:    "observe.targetState must be true exactly when observationContext.targetState is present",
		},
		{
			name:    "context present with observe false",
			payload: targetStateRequestJSON("false", `{"suppression":`+one+`,"visibility":[],"existence":[]}`),
			want:    "observe.targetState must be true exactly when observationContext.targetState is present",
		},
		{
			name:    "all collections empty",
			payload: targetStateRequestWith(`[]`, `[]`, `[]`),
			want:    "must request at least one fact",
		},
		{
			name:    "missing suppression array",
			payload: targetStateRequestJSON("true", `{"visibility":[],"existence":`+one+`}`),
			want:    "suppression",
		},
		{
			name:    "missing visibility array",
			payload: targetStateRequestJSON("true", `{"suppression":[],"existence":`+one+`}`),
			want:    "visibility",
		},
		{
			name:    "missing existence array",
			payload: targetStateRequestJSON("true", `{"suppression":`+one+`,"visibility":[]}`),
			want:    "existence",
		},
		{
			name:    "null family",
			payload: targetStateRequestJSON("true", `{"suppression":null,"visibility":[],"existence":`+one+`}`),
			want:    "suppression",
		},
		{
			name:    "family is object",
			payload: targetStateRequestJSON("true", `{"suppression":{},"visibility":[],"existence":`+one+`}`),
			want:    "suppression",
		},
		{
			name:    "family is string",
			payload: targetStateRequestJSON("true", `{"suppression":[],"visibility":"x","existence":`+one+`}`),
			want:    "visibility",
		},
		{
			name:    "targetState context is array",
			payload: targetStateRequestJSON("true", `[]`),
			want:    "targetState",
		},
		{
			name:    "targetState context is string",
			payload: targetStateRequestJSON("true", `"x"`),
			want:    "targetState",
		},
		{
			name:    "observe targetState is string",
			payload: targetStateRequestJSON(`"true"`, ""),
			want:    "targetState",
		},
		{
			name:    "observe targetState is number",
			payload: targetStateRequestJSON(`1`, ""),
			want:    "targetState",
		},
		{
			name:    "observe targetState is null",
			payload: targetStateRequestJSON(`null`, ""),
			want:    "targetState",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			assertTargetStateValidationError(t, err, tc.want)
		})
	}
}

func TestTargetStateRequest_UnknownFieldsRejectedAtEveryOwnedLevel(t *testing.T) {
	one := `[{"destination":"part","object":"X"}]`
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "observe",
			payload: strings.Replace(targetStateRequestWith(one, `[]`, `[]`), `"targetState":true`, `"targetState":true,"extra":true`, 1),
			want:    `observe contains unknown field "extra"`,
		},
		{
			name:    "observationContext.targetState",
			payload: targetStateRequestJSON("true", `{"suppression":`+one+`,"visibility":[],"existence":[],"extra":[]}`),
			want:    `observationContext.targetState contains unknown field "extra"`,
		},
		{
			name:    "misspelled family key",
			payload: targetStateRequestJSON("true", `{"suppression":`+one+`,"visibility":[],"existence":[],"deletion":[]}`),
			want:    `unknown field "deletion"`,
		},
		{
			name:    "target identity entry",
			payload: targetStateRequestJSON("true", `{"suppression":[{"destination":"part","object":"X","value":true}],"visibility":[],"existence":[]}`),
			want:    `observationContext.targetState.suppression[0] contains unknown field "value"`,
		},
		{
			name:    "observationContext",
			payload: strings.Replace(targetStateRequestWith(one, `[]`, `[]`), `"observationContext":{`, `"observationContext":{"extra":1,`, 1),
			want:    `unknown field "extra"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.payload))
			assertTargetStateValidationError(t, err, tc.want)
		})
	}
}

func TestTargetStateRequest_UnsupportedSchemaVersionRejected(t *testing.T) {
	base := targetStateRequestWith(`[{"destination":"part","object":"X"}]`, `[]`, `[]`)
	for _, version := range []string{`"2.0"`, `"1"`, `"1.1"`, `2`, `null`} {
		t.Run(version, func(t *testing.T) {
			payload := strings.Replace(base, `"schemaVersion":"1.0"`, `"schemaVersion":`+version, 1)
			if _, err := Parse([]byte(payload)); err == nil {
				t.Fatalf("schemaVersion %s must be rejected", version)
			} else if !strings.Contains(err.Error(), "schemaVersion") {
				t.Fatalf("expected schemaVersion error, got %v", err)
			}
		})
	}
	// A schema-2.0 style payload must not be accepted even with valid target state.
	contract, err := Parse([]byte(base))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	contract.SchemaVersion = "2.0"
	if _, err := CanonicalJSON(contract); err == nil {
		t.Fatal("CanonicalJSON must reject schema 2.0 target-state contracts")
	}
	if got := SchemaVersion; got != "1.0" {
		t.Fatalf("schema version constant changed: %q", got)
	}
}

func TestTargetStateRequest_ContractFilenameUnchanged(t *testing.T) {
	root := t.TempDir()
	contract := deriveTargetStateContract(t, &planner.ExportManifestMutationCollection{
		Deletion: []planner.ExportManifestDeletionMutation{{Object: "D"}},
	}, nil)
	path := filepath.Join(root, "prm.verification.json")
	if err := WriteFile(path, contract); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !bytes.Equal(mustCanonical(t, loaded), mustCanonical(t, contract)) {
		t.Fatal("file round trip changed canonical bytes")
	}
}

func equalTargetIdentities(a, b []TargetIdentity) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// requestFragment returns only the observationContext.targetState JSON so that
// assertions are not confused by unrelated fields such as metadata "value".
func requestFragment(t *testing.T, canonical []byte) []byte {
	t.Helper()
	start := bytes.Index(canonical, []byte(`"targetState":{"suppression"`))
	end := bytes.Index(canonical, []byte(`},"expected"`))
	if start < 0 || end < start {
		t.Fatalf("no target-state request in canonical JSON: %s", canonical)
	}
	return canonical[start:end]
}
