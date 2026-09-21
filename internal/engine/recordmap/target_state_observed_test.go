package recordmap

import (
	"encoding/json"
	"reflect"
	"testing"

	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
)

// Issue #18: normalized target-state observation facts.

func tsBoolValue(destination, object string, value bool) observed.BooleanTargetEvidence {
	return observed.BooleanTargetEvidence{Destination: destination, Object: object, Status: observed.BooleanEvidenceStatusObserved, Value: &value}
}

func tsBoolStatus(destination, object, status string) observed.BooleanTargetEvidence {
	return observed.BooleanTargetEvidence{Destination: destination, Object: object, Status: status}
}

func tsExistence(destination, object, status string) observed.ExistenceTargetEvidence {
	return observed.ExistenceTargetEvidence{Destination: destination, Object: object, Status: status}
}

func observedWithTargetState() observed.Observed {
	obs := observedMapObserved()
	obs.Observation.TargetState = &observed.TargetStateObservation{
		Suppression: []observed.BooleanTargetEvidence{
			tsBoolValue("part", "Sketch", true),
			tsBoolValue("assembly", "Pad", false),
			tsBoolStatus("part", "Gone", observed.BooleanEvidenceStatusTargetMissing),
			tsBoolStatus("assembly", "Sensor", observed.BooleanEvidenceStatusUnavailable),
		},
		Visibility: []observed.BooleanTargetEvidence{
			tsBoolValue("assembly", "Pad", true),
			tsBoolValue("part", "Body", false),
		},
		Existence: []observed.ExistenceTargetEvidence{
			tsExistence("part", "Old", observed.ExistenceEvidenceStatusExists),
			tsExistence("assembly", "Deleted", observed.ExistenceEvidenceStatusAbsent),
			tsExistence("assembly", "Opaque", observed.ExistenceEvidenceStatusUnavailable),
		},
	}
	return obs
}

func targetStateFacts(record recordcontract.ObservationRecord) []recordcontract.ObservationFact {
	var facts []recordcontract.ObservationFact
	for _, fact := range record.Observation.Facts {
		if fact.Kind == recordcontract.ObservationKindTargetState {
			facts = append(facts, fact)
		}
	}
	return facts
}

func findTargetStateFact(t *testing.T, record recordcontract.ObservationRecord, destination, object, family string) recordcontract.ObservationFact {
	t.Helper()
	for _, fact := range targetStateFacts(record) {
		if fact.Subject.ID == destination && fact.Subject.Name == object && fact.Key == family {
			return fact
		}
	}
	t.Fatalf("no target_state fact for %s/%s/%s in %+v", destination, object, family, record.Observation.Facts)
	return recordcontract.ObservationFact{}
}

func TestMapObservedTargetStateFactsRetainEvidenceStatusAndValue(t *testing.T) {
	input := observedMapInput(observedWithTargetState())
	record, err := MapObservedToObservationRecord(input)
	if err != nil {
		t.Fatalf("MapObservedToObservationRecord: %v", err)
	}
	if err := recordcontract.ValidateObservationRecord(record); err != nil {
		t.Fatalf("mapped record invalid: %v", err)
	}
	if got := len(targetStateFacts(record)); got != 9 {
		t.Fatalf("target-state facts=%d, want 9", got)
	}

	tests := []struct {
		destination, object, family string
		wantRaw                     string
		wantBool                    *bool
	}{
		{"part", "Sketch", "suppression", `{"status":"observed","value":true}`, tsBoolPtr(true)},
		{"assembly", "Pad", "suppression", `{"status":"observed","value":false}`, tsBoolPtr(false)},
		{"part", "Gone", "suppression", `{"status":"target_missing"}`, nil},
		{"assembly", "Sensor", "suppression", `{"status":"unavailable"}`, nil},
		{"assembly", "Pad", "visibility", `{"status":"observed","value":true}`, tsBoolPtr(true)},
		{"part", "Body", "visibility", `{"status":"observed","value":false}`, tsBoolPtr(false)},
		{"part", "Old", "existence", `{"status":"exists"}`, nil},
		{"assembly", "Deleted", "existence", `{"status":"absent"}`, nil},
		{"assembly", "Opaque", "existence", `{"status":"unavailable"}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.destination+"/"+tt.object+"/"+tt.family, func(t *testing.T) {
			fact := findTargetStateFact(t, record, tt.destination, tt.object, tt.family)
			if fact.Value.Kind != "target_state_evidence" || fact.Value.Raw != tt.wantRaw {
				t.Fatalf("value=%+v, want raw %s", fact.Value, tt.wantRaw)
			}
			var decoded map[string]json.RawMessage
			if err := json.Unmarshal([]byte(fact.Value.Raw), &decoded); err != nil {
				t.Fatalf("raw is not JSON: %v", err)
			}
			_, hasValue := decoded["value"]
			if hasValue != (tt.wantBool != nil) {
				t.Fatalf("boolean present=%t, want %t: %s", hasValue, tt.wantBool != nil, fact.Value.Raw)
			}
			if tt.wantBool != nil {
				var got bool
				if err := json.Unmarshal(decoded["value"], &got); err != nil || got != *tt.wantBool {
					t.Fatalf("boolean=%v err=%v, want %t", got, err, *tt.wantBool)
				}
			}
			if fact.Evidence.SourceKind != observedEvidenceKind || fact.Evidence.SourceRef != observedEvidenceRef || fact.Evidence.DigestSHA256 != observedMapDigestA() {
				t.Fatalf("evidence=%+v", fact.Evidence)
			}
			if fact.Linkage.JobID != "job-123" || fact.Linkage.ProductKey != "product-abc" || fact.Linkage.StepRef != "observe" {
				t.Fatalf("linkage=%+v", fact.Linkage)
			}
		})
	}
}

func TestMapObservedTargetStateNeverNormalizesNonObservationsToFalse(t *testing.T) {
	record, err := MapObservedToObservationRecord(observedMapInput(observedWithTargetState()))
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range targetStateFacts(record) {
		var decoded struct {
			Status string `json:"status"`
			Value  *bool  `json:"value"`
		}
		if err := json.Unmarshal([]byte(fact.Value.Raw), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Status != observed.BooleanEvidenceStatusObserved && decoded.Value != nil {
			t.Fatalf("status %q carries boolean %v: %+v", decoded.Status, *decoded.Value, fact)
		}
		if decoded.Status == observed.BooleanEvidenceStatusObserved && decoded.Value == nil {
			t.Fatalf("observed fact lost its boolean: %+v", fact)
		}
	}
	// target_missing and unavailable stay distinguishable from observed false.
	missing := findTargetStateFact(t, record, "part", "Gone", "suppression").Value.Raw
	unavailable := findTargetStateFact(t, record, "assembly", "Sensor", "suppression").Value.Raw
	observedFalse := findTargetStateFact(t, record, "assembly", "Pad", "suppression").Value.Raw
	if missing == observedFalse || unavailable == observedFalse || missing == unavailable {
		t.Fatalf("statuses collapsed: %s %s %s", missing, unavailable, observedFalse)
	}
}

func TestMapObservedTargetStateUsesExactIdentityAndFamily(t *testing.T) {
	obs := observedMapObserved()
	obs.Observation.TargetState = &observed.TargetStateObservation{
		Suppression: []observed.BooleanTargetEvidence{tsBoolValue("assembly", "Pad", true), tsBoolValue("part", "Pad", false), tsBoolValue("part", "pad", true)},
		Visibility:  []observed.BooleanTargetEvidence{tsBoolValue("assembly", "Pad", false)},
		Existence:   []observed.ExistenceTargetEvidence{},
	}
	record, err := MapObservedToObservationRecord(observedMapInput(obs))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(targetStateFacts(record)); got != 4 {
		t.Fatalf("facts=%d, want 4 distinct (destination, object, family) facts", got)
	}
	findTargetStateFact(t, record, "part", "pad", "suppression")
	findTargetStateFact(t, record, "part", "Pad", "suppression")
	findTargetStateFact(t, record, "assembly", "Pad", "visibility")
}

func TestMapObservedTargetStateOrderingAndIdentityAreDeterministic(t *testing.T) {
	base, err := MapObservedToObservationRecord(observedMapInput(observedWithTargetState()))
	if err != nil {
		t.Fatal(err)
	}

	reordered := observedWithTargetState()
	ts := reordered.Observation.TargetState
	reverseBool := func(in []observed.BooleanTargetEvidence) {
		for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
			in[i], in[j] = in[j], in[i]
		}
	}
	reverseBool(ts.Suppression)
	reverseBool(ts.Visibility)
	for i, j := 0, len(ts.Existence)-1; i < j; i, j = i+1, j-1 {
		ts.Existence[i], ts.Existence[j] = ts.Existence[j], ts.Existence[i]
	}
	other, err := MapObservedToObservationRecord(observedMapInput(reordered))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(base, other) || base.Identity != other.Identity {
		t.Fatalf("evidence ordering leaked into the record\nbase:  %+v\nother: %+v", base.Observation.Facts, other.Observation.Facts)
	}
	for i := 0; i < 5; i++ {
		again, err := MapObservedToObservationRecord(observedMapInput(observedWithTargetState()))
		if err != nil || !reflect.DeepEqual(base, again) {
			t.Fatalf("repeat %d differs (err=%v)", i, err)
		}
	}

	// The canonical fact order is destination, object, then family.
	facts := targetStateFacts(base)
	for i := 1; i < len(facts); i++ {
		p, c := facts[i-1], facts[i]
		if p.Subject.ID > c.Subject.ID || (p.Subject.ID == c.Subject.ID && (p.Subject.Name > c.Subject.Name ||
			(p.Subject.Name == c.Subject.Name && p.Key > c.Key))) {
			t.Fatalf("facts not canonically ordered at %d: %+v then %+v", i, p, c)
		}
	}
}

func TestMapObservedTargetStateSemanticChangesChangeRecordIdentity(t *testing.T) {
	base, err := MapObservedToObservationRecord(observedMapInput(observedWithTargetState()))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*observed.TargetStateObservation)
	}{
		{"observed boolean flips", func(ts *observed.TargetStateObservation) { *ts.Suppression[0].Value = false }},
		{"target_missing becomes unavailable", func(ts *observed.TargetStateObservation) { ts.Suppression[2].Status = observed.BooleanEvidenceStatusUnavailable }},
		{"unavailable becomes observed false", func(ts *observed.TargetStateObservation) { ts.Suppression[3] = tsBoolValue("assembly", "Sensor", false) }},
		{"existence exists becomes absent", func(ts *observed.TargetStateObservation) { ts.Existence[0].Status = observed.ExistenceEvidenceStatusAbsent }},
		{"object renamed", func(ts *observed.TargetStateObservation) { ts.Visibility[1].Object = "Body2" }},
		{"destination changed", func(ts *observed.TargetStateObservation) { ts.Visibility[1].Destination = "assembly" }},
		{"entry dropped", func(ts *observed.TargetStateObservation) { ts.Existence = ts.Existence[:2] }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := observedWithTargetState()
			tt.mutate(obs.Observation.TargetState)
			got, err := MapObservedToObservationRecord(observedMapInput(obs))
			if err != nil {
				t.Fatal(err)
			}
			if got.Identity.ID == base.Identity.ID {
				t.Fatalf("identity did not change")
			}
		})
	}
}

func TestMapObservedTargetStateAbsentAddsNoFacts(t *testing.T) {
	record, err := MapObservedToObservationRecord(observedMapInput(observedMapObserved()))
	if err != nil {
		t.Fatal(err)
	}
	if got := targetStateFacts(record); len(got) != 0 {
		t.Fatalf("no target-state evidence must yield no target_state facts: %+v", got)
	}

	// An empty (but present) target-state observation contributes nothing either.
	obs := observedMapObserved()
	obs.Observation.TargetState = &observed.TargetStateObservation{
		Suppression: []observed.BooleanTargetEvidence{}, Visibility: []observed.BooleanTargetEvidence{}, Existence: []observed.ExistenceTargetEvidence{},
	}
	withEmpty, err := MapObservedToObservationRecord(observedMapInput(obs))
	if err != nil || !reflect.DeepEqual(record, withEmpty) {
		t.Fatalf("empty target-state changed the record (err=%v)", err)
	}
}

func TestMapObservedTargetStateDoesNotMutateInput(t *testing.T) {
	input := observedMapInput(observedWithTargetState())
	before := observedWithTargetState()

	output, err := MapObserved(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input.Observed, before) {
		t.Fatal("MapObserved mutated the observed input")
	}

	// Mutating the input afterwards, including through boolean pointers, does not reach the record.
	snapshot := output.ObservationRecord
	snapshotRaw := findTargetStateFact(t, snapshot, "part", "Sketch", "suppression").Value.Raw
	*input.Observed.Observation.TargetState.Suppression[0].Value = false
	input.Observed.Observation.TargetState.Suppression[0].Object = "mutated"
	input.Observed.Observation.TargetState.Existence[0].Status = observed.ExistenceEvidenceStatusAbsent
	if got := findTargetStateFact(t, output.ObservationRecord, "part", "Sketch", "suppression").Value.Raw; got != snapshotRaw {
		t.Fatalf("record aliases input: %s vs %s", got, snapshotRaw)
	}
}

func tsBoolPtr(value bool) *bool { return &value }
