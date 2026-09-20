package observed

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const targetStateWorkingCopyJSON = `"workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`

func targetStateObservedJSON(targetState string) []byte {
	observation := `{"parameters":[],"metadata":[],"references":[],"components":[]`
	if targetState != "" {
		observation += `,"targetState":` + targetState
	}
	observation += `}`
	return []byte(`{"schemaVersion":"1.0",` + targetStateWorkingCopyJSON + `,"observation":` + observation + `}`)
}

func targetStateWith(suppression, visibility, existence string) []byte {
	return targetStateObservedJSON(`{"suppression":` + suppression + `,"visibility":` + visibility + `,"existence":` + existence + `}`)
}

func boolPtr(v bool) *bool { return &v }

func mustCanonicalObserved(t *testing.T, in *Observed) []byte {
	t.Helper()
	out, err := CanonicalJSON(in)
	if err != nil {
		t.Fatalf("CanonicalJSON returned error: %v", err)
	}
	return out
}

func assertObservedValidation(t *testing.T, err error, want string) {
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

func TestTargetStateObserved_AbsentPreservesSchema10Behavior(t *testing.T) {
	want := `{"schemaVersion":"1.0","workingCopy":{"path":"/tmp/model.FCStd","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"observation":{"parameters":[],"metadata":[],"references":[],"components":[]}}`
	parsed, err := Parse(targetStateObservedJSON(""))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Observation.TargetState != nil {
		t.Fatalf("absent targetState must stay nil: %+v", parsed.Observation.TargetState)
	}
	got := mustCanonicalObserved(t, parsed)
	if string(got) != want {
		t.Fatalf("canonical bytes changed\nwant: %s\ngot:  %s", want, got)
	}
	// Fully populated pre-existing observation also omits targetState.
	full := mustCanonicalObserved(t, validObserved())
	if bytes.Contains(full, []byte("targetState")) {
		t.Fatalf("existing observation must omit targetState: %s", full)
	}
	reparsed, err := Parse(full)
	if err != nil {
		t.Fatalf("Parse(full): %v", err)
	}
	if !bytes.Equal(full, mustCanonicalObserved(t, reparsed)) {
		t.Fatal("existing observation round trip changed bytes")
	}
	if len(reparsed.Observation.Parameters) != 1 || len(reparsed.Observation.Metadata) != 1 ||
		len(reparsed.Observation.References) != 1 || len(reparsed.Observation.Components) != 2 {
		t.Fatalf("existing categories lost: %+v", reparsed.Observation)
	}
}

func TestTargetStateObserved_BooleanEvidenceStatesParseAndRoundTrip(t *testing.T) {
	for _, family := range []string{"suppression", "visibility"} {
		t.Run(family, func(t *testing.T) {
			entries := `[` +
				`{"destination":"assembly","object":"False","status":"observed","value":false},` +
				`{"destination":"assembly","object":"Missing","status":"target_missing"},` +
				`{"destination":"assembly","object":"True","status":"observed","value":true},` +
				`{"destination":"assembly","object":"Unavailable","status":"unavailable"}` +
				`]`
			args := map[string]string{"suppression": `[]`, "visibility": `[]`}
			args[family] = entries
			parsed, err := Parse(targetStateWith(args["suppression"], args["visibility"], `[]`))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got := parsed.Observation.TargetState.Suppression
			if family == "visibility" {
				got = parsed.Observation.TargetState.Visibility
			}
			if len(got) != 4 {
				t.Fatalf("expected 4 entries, got %+v", got)
			}
			byObject := map[string]BooleanTargetEvidence{}
			for _, entry := range got {
				byObject[entry.Object] = entry
			}
			if e := byObject["True"]; e.Status != BooleanEvidenceStatusObserved || e.Value == nil || !*e.Value {
				t.Fatalf("observed true lost: %+v", e)
			}
			if e := byObject["False"]; e.Status != BooleanEvidenceStatusObserved || e.Value == nil || *e.Value {
				t.Fatalf("observed false lost (must be non-nil false): %+v", e)
			}
			if e := byObject["Missing"]; e.Status != BooleanEvidenceStatusTargetMissing || e.Value != nil {
				t.Fatalf("target_missing must have no value: %+v", e)
			}
			if e := byObject["Unavailable"]; e.Status != BooleanEvidenceStatusUnavailable || e.Value != nil {
				t.Fatalf("unavailable must have no value: %+v", e)
			}
			first := mustCanonicalObserved(t, parsed)
			for _, fragment := range []string{
				`{"destination":"assembly","object":"False","status":"observed","value":false}`,
				`{"destination":"assembly","object":"Missing","status":"target_missing"}`,
				`{"destination":"assembly","object":"True","status":"observed","value":true}`,
				`{"destination":"assembly","object":"Unavailable","status":"unavailable"}`,
			} {
				if !bytes.Contains(first, []byte(fragment)) {
					t.Fatalf("canonical bytes missing %s\n%s", fragment, first)
				}
			}
			reparsed, err := Parse(first)
			if err != nil {
				t.Fatalf("Parse(canonical): %v", err)
			}
			if second := mustCanonicalObserved(t, reparsed); !bytes.Equal(first, second) {
				t.Fatalf("round trip changed bytes\nfirst:  %s\nsecond: %s", first, second)
			}
		})
	}
}

func TestTargetStateObserved_ExistenceStatesAreThreeDistinctValidStates(t *testing.T) {
	payload := targetStateWith(`[]`, `[]`,
		`[{"destination":"part","object":"U","status":"unavailable"},{"destination":"part","object":"A","status":"absent"},{"destination":"part","object":"E","status":"exists"}]`)
	parsed, err := Parse(payload)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	statuses := map[string]string{}
	for _, entry := range parsed.Observation.TargetState.Existence {
		statuses[entry.Object] = entry.Status
	}
	if statuses["E"] != ExistenceEvidenceStatusExists || statuses["A"] != ExistenceEvidenceStatusAbsent || statuses["U"] != ExistenceEvidenceStatusUnavailable {
		t.Fatalf("statuses collapsed: %+v", statuses)
	}
	if ExistenceEvidenceStatusAbsent == ExistenceEvidenceStatusUnavailable || ExistenceEvidenceStatusExists == ExistenceEvidenceStatusAbsent {
		t.Fatal("existence status constants must be distinct")
	}
	first := mustCanonicalObserved(t, parsed)
	want := `"existence":[{"destination":"part","object":"A","status":"absent"},{"destination":"part","object":"E","status":"exists"},{"destination":"part","object":"U","status":"unavailable"}]`
	if !bytes.Contains(first, []byte(want)) {
		t.Fatalf("unexpected existence bytes\nwant fragment: %s\ngot: %s", want, first)
	}
	reparsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse(canonical): %v", err)
	}
	if !bytes.Equal(first, mustCanonicalObserved(t, reparsed)) {
		t.Fatal("existence round trip changed bytes")
	}
}

func TestTargetStateObserved_MissingIsDistinctFromUnavailable(t *testing.T) {
	missing, err := Parse(targetStateWith(`[{"destination":"part","object":"X","status":"target_missing"}]`, `[]`, `[{"destination":"part","object":"X","status":"absent"}]`))
	if err != nil {
		t.Fatalf("Parse(missing/absent): %v", err)
	}
	unavailable, err := Parse(targetStateWith(`[{"destination":"part","object":"X","status":"unavailable"}]`, `[]`, `[{"destination":"part","object":"X","status":"unavailable"}]`))
	if err != nil {
		t.Fatalf("Parse(unavailable): %v", err)
	}
	if bytes.Equal(mustCanonicalObserved(t, missing), mustCanonicalObserved(t, unavailable)) {
		t.Fatal("missing/absent and unavailable must serialize differently")
	}
	for name, parsed := range map[string]*Observed{"missing": missing, "unavailable": unavailable} {
		reparsed, err := Parse(mustCanonicalObserved(t, parsed))
		if err != nil {
			t.Fatalf("%s: Parse(canonical): %v", name, err)
		}
		if reparsed.Observation.TargetState.Suppression[0].Status != parsed.Observation.TargetState.Suppression[0].Status ||
			reparsed.Observation.TargetState.Existence[0].Status != parsed.Observation.TargetState.Existence[0].Status {
			t.Fatalf("%s: round trip changed status", name)
		}
	}
	if BooleanEvidenceStatusTargetMissing == BooleanEvidenceStatusUnavailable {
		t.Fatal("boolean status constants must be distinct")
	}
	// target_missing / unavailable must not be representable as a boolean value.
	if missing.Observation.TargetState.Suppression[0].Value != nil || unavailable.Observation.TargetState.Suppression[0].Value != nil {
		t.Fatal("non-observed boolean evidence must carry no value")
	}
}

func TestTargetStateObserved_EmptyCollectionsAreValidRawEvidence(t *testing.T) {
	parsed, err := Parse(targetStateWith(`[]`, `[]`, `[]`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Observation.TargetState == nil {
		t.Fatal("empty targetState object must be retained")
	}
	got := mustCanonicalObserved(t, parsed)
	if !bytes.Contains(got, []byte(`"targetState":{"suppression":[],"visibility":[],"existence":[]}`)) {
		t.Fatalf("empty targetState not serialized canonically: %s", got)
	}
}

func TestTargetStateObserved_InvalidBooleanStatusValueCombinationsRejected(t *testing.T) {
	cases := []struct {
		name  string
		entry string
		want  string
	}{
		{"observed without value", `{"destination":"part","object":"X","status":"observed"}`, "value is required when status is observed"},
		{"target_missing with true", `{"destination":"part","object":"X","status":"target_missing","value":true}`, "value must be absent unless status is observed"},
		{"target_missing with false", `{"destination":"part","object":"X","status":"target_missing","value":false}`, "value must be absent unless status is observed"},
		{"unavailable with true", `{"destination":"part","object":"X","status":"unavailable","value":true}`, "value must be absent unless status is observed"},
		{"unavailable with false", `{"destination":"part","object":"X","status":"unavailable","value":false}`, "value must be absent unless status is observed"},
		{"unknown status", `{"destination":"part","object":"X","status":"hidden","value":true}`, "status must be one of"},
		{"existence status in boolean family", `{"destination":"part","object":"X","status":"exists"}`, "status must be one of"},
		{"absent status in boolean family", `{"destination":"part","object":"X","status":"absent"}`, "status must be one of"},
		{"empty status", `{"destination":"part","object":"X","status":""}`, "status"},
		{"uppercase status", `{"destination":"part","object":"X","status":"Observed","value":true}`, "status must be one of"},
		{"string value", `{"destination":"part","object":"X","status":"observed","value":"true"}`, "value must be a boolean"},
		{"numeric value", `{"destination":"part","object":"X","status":"observed","value":1}`, "value must be a boolean"},
		{"null value", `{"destination":"part","object":"X","status":"observed","value":null}`, "value must be a boolean"},
		{"null value on missing", `{"destination":"part","object":"X","status":"target_missing","value":null}`, "value must be a boolean"},
		{"object value", `{"destination":"part","object":"X","status":"observed","value":{}}`, "value must be a boolean"},
		{"missing status", `{"destination":"part","object":"X","value":true}`, "status"},
		{"non-string status", `{"destination":"part","object":"X","status":true,"value":true}`, "status"},
	}
	for _, family := range []string{"suppression", "visibility"} {
		for _, tc := range cases {
			t.Run(family+"/"+tc.name, func(t *testing.T) {
				args := map[string]string{"suppression": `[]`, "visibility": `[]`}
				args[family] = `[` + tc.entry + `]`
				_, err := Parse(targetStateWith(args["suppression"], args["visibility"], `[]`))
				assertObservedValidation(t, err, tc.want)
				if !strings.Contains(err.Error(), "observation.targetState."+family+"[0]") {
					t.Fatalf("error must locate the entry: %v", err)
				}
			})
		}
	}
}

func TestTargetStateObserved_InvalidExistenceEntriesRejected(t *testing.T) {
	cases := []struct {
		name  string
		entry string
		want  string
	}{
		{"unknown status", `{"destination":"part","object":"X","status":"deleted"}`, "status must be one of"},
		{"observed status", `{"destination":"part","object":"X","status":"observed"}`, "status must be one of"},
		{"target_missing status", `{"destination":"part","object":"X","status":"target_missing"}`, "status must be one of"},
		{"empty status", `{"destination":"part","object":"X","status":""}`, "status"},
		{"missing status", `{"destination":"part","object":"X"}`, "status"},
		{"extra value field", `{"destination":"part","object":"X","status":"exists","value":true}`, `unknown field "value"`},
		{"extra unknown field", `{"destination":"part","object":"X","status":"exists","note":"x"}`, `unknown field "note"`},
		{"bad destination", `{"destination":"sketch","object":"X","status":"exists"}`, "destination must be one of"},
		{"empty object", `{"destination":"part","object":"","status":"exists"}`, "[0].object"},
		{"whitespace object", `{"destination":"part","object":"  ","status":"exists"}`, "[0].object"},
		{"padded object", `{"destination":"part","object":" X","status":"absent"}`, "object must be a non-blank exact native object name"},
		{"NUL object", `{"destination":"part","object":"X\u0000","status":"absent"}`, "object must be a non-blank exact native object name"},
		{"non-string object", `{"destination":"part","object":7,"status":"absent"}`, "object"},
		{"entry not an object", `"part/X"`, "must be an object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(targetStateWith(`[]`, `[]`, `[`+tc.entry+`]`))
			assertObservedValidation(t, err, tc.want)
			if !strings.Contains(err.Error(), "observation.targetState.existence[0]") {
				t.Fatalf("error must locate the entry: %v", err)
			}
		})
	}
}

func TestTargetStateObserved_MalformedBooleanIdentityRejected(t *testing.T) {
	cases := []struct{ name, entry, want string }{
		{"bad destination", `{"destination":"root","object":"X","status":"target_missing"}`, "destination must be one of"},
		{"uppercase destination", `{"destination":"Part","object":"X","status":"target_missing"}`, "destination must be one of"},
		{"empty object", `{"destination":"part","object":"","status":"target_missing"}`, "[0].object"},
		{"whitespace-only object", `{"destination":"part","object":" \t","status":"target_missing"}`, "[0].object"},
		{"surrounding whitespace", `{"destination":"part","object":"X ","status":"target_missing"}`, "object must be a non-blank exact native object name"},
		{"embedded NUL", `{"destination":"part","object":"a\u0000b","status":"target_missing"}`, "object must be a non-blank exact native object name"},
		{"missing destination", `{"object":"X","status":"target_missing"}`, "destination"},
		{"missing object", `{"destination":"part","status":"target_missing"}`, "object"},
		{"non-string destination", `{"destination":false,"object":"X","status":"target_missing"}`, "destination"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(targetStateWith(`[`+tc.entry+`]`, `[]`, `[]`))
			assertObservedValidation(t, err, tc.want)
		})
	}
}

func TestTargetStateObserved_StructuralValidation(t *testing.T) {
	cases := []struct{ name, targetState, want string }{
		{"missing suppression", `{"visibility":[],"existence":[]}`, "observation.targetState.suppression is required"},
		{"missing visibility", `{"suppression":[],"existence":[]}`, "observation.targetState.visibility is required"},
		{"missing existence", `{"suppression":[],"visibility":[]}`, "observation.targetState.existence is required"},
		{"null suppression", `{"suppression":null,"visibility":[],"existence":[]}`, "observation.targetState.suppression must be an array"},
		{"object visibility", `{"suppression":[],"visibility":{},"existence":[]}`, "observation.targetState.visibility must be an array"},
		{"string existence", `{"suppression":[],"visibility":[],"existence":"x"}`, "observation.targetState.existence must be an array"},
		{"targetState array", `[]`, "observation.targetState must be an object"},
		{"targetState null", `null`, "observation.targetState must be an object"},
		{"targetState bool", `true`, "observation.targetState must be an object"},
		{"suppression entry not object", `{"suppression":[1],"visibility":[],"existence":[]}`, "must be an object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(targetStateObservedJSON(tc.targetState))
			assertObservedValidation(t, err, tc.want)
		})
	}
}

func TestTargetStateObserved_UnknownFieldsRejectedAtEveryOwnedLevel(t *testing.T) {
	ok := `{"destination":"part","object":"X","status":"target_missing"}`
	cases := []struct{ name, targetState, want string }{
		{"targetState", `{"suppression":[],"visibility":[],"existence":[],"extra":[]}`, `observation.targetState has unknown field "extra"`},
		{"legacy family name", `{"suppression":[],"visibility":[],"existence":[],"deletion":[]}`, `unknown field "deletion"`},
		{"boolean entry", `{"suppression":[{"destination":"part","object":"X","status":"target_missing","extra":1}],"visibility":[],"existence":[]}`, `observation.targetState.suppression[0] has unknown field "extra"`},
		{"visibility entry", `{"suppression":[],"visibility":[` + strings.Replace(ok, `}`, `,"requested":true}`, 1) + `],"existence":[]}`, `observation.targetState.visibility[0] has unknown field "requested"`},
		{"existence entry", `{"suppression":[],"visibility":[],"existence":[{"destination":"part","object":"X","status":"exists","extra":1}]}`, `observation.targetState.existence[0] has unknown field "extra"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(targetStateObservedJSON(tc.targetState))
			assertObservedValidation(t, err, tc.want)
		})
	}
	// observation level: an unknown sibling of targetState is still rejected.
	_, err := Parse([]byte(`{"schemaVersion":"1.0",` + targetStateWorkingCopyJSON + `,"observation":{"parameters":[],"metadata":[],"references":[],"components":[],"targetStates":{}}}`))
	assertObservedValidation(t, err, `unknown field "targetStates"`)
}

func TestTargetStateObserved_UnsupportedSchemaRejected(t *testing.T) {
	base := string(targetStateWith(`[{"destination":"part","object":"X","status":"target_missing"}]`, `[]`, `[]`))
	for _, version := range []string{`"2.0"`, `"1"`, `2`} {
		t.Run(version, func(t *testing.T) {
			payload := strings.Replace(base, `"schemaVersion":"1.0"`, `"schemaVersion":`+version, 1)
			if _, err := Parse([]byte(payload)); err == nil || !strings.Contains(err.Error(), "schemaVersion") {
				t.Fatalf("expected schemaVersion rejection, got %v", err)
			}
		})
	}
	parsed, err := Parse([]byte(base))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	parsed.SchemaVersion = "2.0"
	if _, err := CanonicalJSON(parsed); err == nil {
		t.Fatal("CanonicalJSON must reject schema 2.0")
	}
	if SchemaVersion != "1.0" {
		t.Fatalf("schema constant changed: %q", SchemaVersion)
	}
}

func TestTargetStateObserved_DuplicateIdentitiesRejectedPerFamily(t *testing.T) {
	boolDup := `[{"destination":"part","object":"X","status":"observed","value":true},{"destination":"part","object":"X","status":"observed","value":true}]`
	conflicting := `[{"destination":"part","object":"X","status":"observed","value":true},{"destination":"part","object":"X","status":"target_missing"}]`
	existDup := `[{"destination":"part","object":"X","status":"exists"},{"destination":"part","object":"X","status":"absent"}]`
	cases := []struct {
		name, family string
		payload      []byte
	}{
		{"suppression identical", "suppression", targetStateWith(boolDup, `[]`, `[]`)},
		{"suppression conflicting", "suppression", targetStateWith(conflicting, `[]`, `[]`)},
		{"visibility identical", "visibility", targetStateWith(`[]`, boolDup, `[]`)},
		{"visibility conflicting", "visibility", targetStateWith(`[]`, conflicting, `[]`)},
		{"existence conflicting", "existence", targetStateWith(`[]`, `[]`, existDup)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.payload)
			assertObservedValidation(t, err, "observation.targetState."+tc.family+"[1] duplicates")
			if !strings.Contains(err.Error(), "[0]") {
				t.Fatalf("error must name the first occurrence: %v", err)
			}
		})
	}
	// Same object in distinct destinations or distinct families is not a duplicate.
	ok := targetStateWith(
		`[{"destination":"assembly","object":"X","status":"target_missing"},{"destination":"part","object":"X","status":"target_missing"}]`,
		`[{"destination":"part","object":"X","status":"unavailable"}]`,
		`[{"destination":"part","object":"X","status":"absent"}]`)
	if _, err := Parse(ok); err != nil {
		t.Fatalf("cross-destination / cross-family same object must be valid: %v", err)
	}
}

func TestTargetStateObserved_ProgrammaticValidationAndDuplicates(t *testing.T) {
	in := validObserved()
	in.Observation.TargetState = &TargetStateObservation{
		Suppression: []BooleanTargetEvidence{
			{Destination: "part", Object: "X", Status: BooleanEvidenceStatusObserved, Value: boolPtr(true)},
			{Destination: "part", Object: "X", Status: BooleanEvidenceStatusUnavailable},
		},
		Visibility: []BooleanTargetEvidence{},
		Existence:  []ExistenceTargetEvidence{},
	}
	assertObservedValidation(t, Validate(in), "duplicates")
	if _, err := CanonicalJSON(in); err == nil {
		t.Fatal("CanonicalJSON must reject duplicates")
	}

	in.Observation.TargetState = &TargetStateObservation{Suppression: nil, Visibility: []BooleanTargetEvidence{}, Existence: []ExistenceTargetEvidence{}}
	assertObservedValidation(t, Validate(in), "observation.targetState collections are required")
	in.Observation.TargetState = &TargetStateObservation{Suppression: []BooleanTargetEvidence{}, Visibility: []BooleanTargetEvidence{}, Existence: nil}
	assertObservedValidation(t, Validate(in), "observation.targetState collections are required")
}

func TestTargetStateObserved_CanonicalOrderingIsDeterministicAcrossInputOrders(t *testing.T) {
	orderA := targetStateWith(
		`[{"destination":"part","object":"B","status":"observed","value":false},{"destination":"assembly","object":"Z","status":"unavailable"},{"destination":"assembly","object":"A","status":"observed","value":true}]`,
		`[{"destination":"part","object":"b","status":"target_missing"},{"destination":"part","object":"B","status":"observed","value":true}]`,
		`[{"destination":"part","object":"Q","status":"absent"},{"destination":"assembly","object":"Q","status":"exists"},{"destination":"assembly","object":"P","status":"unavailable"}]`)
	orderB := targetStateWith(
		`[{"destination":"assembly","object":"A","status":"observed","value":true},{"destination":"part","object":"B","status":"observed","value":false},{"destination":"assembly","object":"Z","status":"unavailable"}]`,
		`[{"destination":"part","object":"B","status":"observed","value":true},{"destination":"part","object":"b","status":"target_missing"}]`,
		`[{"destination":"assembly","object":"P","status":"unavailable"},{"destination":"assembly","object":"Q","status":"exists"},{"destination":"part","object":"Q","status":"absent"}]`)
	a, err := Parse(orderA)
	if err != nil {
		t.Fatalf("Parse(A): %v", err)
	}
	b, err := Parse(orderB)
	if err != nil {
		t.Fatalf("Parse(B): %v", err)
	}
	gotA, gotB := mustCanonicalObserved(t, a), mustCanonicalObserved(t, b)
	if !bytes.Equal(gotA, gotB) {
		t.Fatalf("equivalent evidence must canonicalize identically\nA: %s\nB: %s", gotA, gotB)
	}
	want := `"targetState":{"suppression":[{"destination":"assembly","object":"A","status":"observed","value":true},{"destination":"assembly","object":"Z","status":"unavailable"},{"destination":"part","object":"B","status":"observed","value":false}],"visibility":[{"destination":"part","object":"B","status":"observed","value":true},{"destination":"part","object":"b","status":"target_missing"}],"existence":[{"destination":"assembly","object":"P","status":"unavailable"},{"destination":"assembly","object":"Q","status":"exists"},{"destination":"part","object":"Q","status":"absent"}]}`
	if !bytes.Contains(gotA, []byte(want)) {
		t.Fatalf("unexpected ordering\nwant fragment: %s\ngot: %s", want, gotA)
	}
	// Repeated serialization is stable and non-mutating.
	for i := 0; i < 3; i++ {
		if again := mustCanonicalObserved(t, a); !bytes.Equal(again, gotA) {
			t.Fatalf("serialization %d differs", i)
		}
	}
	if a.Observation.TargetState.Suppression[0].Object != "B" {
		t.Fatal("CanonicalJSON must not reorder the caller's slices")
	}
}

func TestTargetStateObserved_ProgrammaticOrderMatchesParsedOrder(t *testing.T) {
	in := validObserved()
	in.Observation.TargetState = &TargetStateObservation{
		Suppression: []BooleanTargetEvidence{
			{Destination: "part", Object: "B", Status: BooleanEvidenceStatusTargetMissing},
			{Destination: "assembly", Object: "B", Status: BooleanEvidenceStatusObserved, Value: boolPtr(false)},
		},
		Visibility: []BooleanTargetEvidence{},
		Existence: []ExistenceTargetEvidence{
			{Destination: "part", Object: "A", Status: ExistenceEvidenceStatusAbsent},
			{Destination: "assembly", Object: "A", Status: ExistenceEvidenceStatusExists},
		},
	}
	got := mustCanonicalObserved(t, in)
	for _, want := range []string{
		`"suppression":[{"destination":"assembly","object":"B","status":"observed","value":false},{"destination":"part","object":"B","status":"target_missing"}]`,
		`"existence":[{"destination":"assembly","object":"A","status":"exists"},{"destination":"part","object":"A","status":"absent"}]`,
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	reparsed, err := Parse(got)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !bytes.Equal(got, mustCanonicalObserved(t, reparsed)) {
		t.Fatal("round trip changed bytes")
	}
	// target-state evidence coexists with the existing categories.
	if !bytes.Contains(got, []byte(`"parameters":[{`)) || !bytes.Contains(got, []byte(`"components":[{`)) {
		t.Fatalf("existing categories missing: %s", got)
	}
}

// Issue #17: raw native evidence is independent from requested mutation intent.
// A request for visible=false may legitimately be answered with observed true;
// deciding whether that passes belongs to later verification work.
func TestTargetStateObserved_RawEvidenceMayDifferFromRequestedIntent(t *testing.T) {
	// Requested intent: visible=false, suppressed=true, deleted=true.
	const requestedVisible, requestedSuppressed = false, true

	parsed, err := Parse(targetStateWith(
		`[{"destination":"part","object":"Pad","status":"observed","value":false}]`,
		`[{"destination":"part","object":"Pad","status":"observed","value":true}]`,
		`[{"destination":"part","object":"Pad","status":"exists"}]`))
	if err != nil {
		t.Fatalf("raw evidence contradicting intent must still parse: %v", err)
	}
	visibility := parsed.Observation.TargetState.Visibility[0]
	if visibility.Value == nil || *visibility.Value != !requestedVisible {
		t.Fatalf("visibility raw evidence not retained: %+v", visibility)
	}
	suppression := parsed.Observation.TargetState.Suppression[0]
	if suppression.Value == nil || *suppression.Value == requestedSuppressed {
		t.Fatalf("suppression raw evidence not retained: %+v", suppression)
	}
	if parsed.Observation.TargetState.Existence[0].Status != ExistenceEvidenceStatusExists {
		t.Fatalf("existence raw evidence not retained: %+v", parsed.Observation.TargetState.Existence[0])
	}
	first := mustCanonicalObserved(t, parsed)
	reparsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse(canonical): %v", err)
	}
	if !bytes.Equal(first, mustCanonicalObserved(t, reparsed)) {
		t.Fatal("round trip changed raw evidence bytes")
	}
	if !bytes.Contains(first, []byte(`"visibility":[{"destination":"part","object":"Pad","status":"observed","value":true}]`)) {
		t.Fatalf("visibility true evidence not serialized: %s", first)
	}
}

func TestTargetStateObserved_LoadAndWriteFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/prm.observed.json"
	in := validObserved()
	in.Observation.TargetState = &TargetStateObservation{
		Suppression: []BooleanTargetEvidence{{Destination: "part", Object: "S", Status: BooleanEvidenceStatusObserved, Value: boolPtr(false)}},
		Visibility:  []BooleanTargetEvidence{{Destination: "part", Object: "V", Status: BooleanEvidenceStatusUnavailable}},
		Existence:   []ExistenceTargetEvidence{{Destination: "assembly", Object: "E", Status: ExistenceEvidenceStatusAbsent}},
	}
	if err := WriteFile(path, in); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !bytes.Equal(mustCanonicalObserved(t, in), mustCanonicalObserved(t, loaded)) {
		t.Fatal("file round trip changed canonical bytes")
	}
	if v := loaded.Observation.TargetState.Suppression[0].Value; v == nil || *v {
		t.Fatalf("observed false lost through file round trip: %v", v)
	}
}
