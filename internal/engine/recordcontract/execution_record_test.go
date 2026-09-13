package recordcontract

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBuildExecutionRecordDerivesNormalizedIdentity(t *testing.T) {
	record, err := BuildExecutionRecord(validExecutionRecordInput())
	if err != nil {
		t.Fatalf("BuildExecutionRecord(valid) returned error: %v", err)
	}

	if record.Family != FamilyExecution {
		t.Fatalf("Family = %q, want %q", record.Family, FamilyExecution)
	}
	if record.Version != CurrentVersion {
		t.Fatalf("Version = %q, want %q", record.Version, CurrentVersion)
	}
	if record.RecordKey != "execution-record-1" {
		t.Fatalf("RecordKey = %q, want trimmed execution-record-1", record.RecordKey)
	}
	if record.Provenance.SourceRevision.RevisionID != "rev-1" {
		t.Fatalf("Provenance.SourceRevision.RevisionID = %q, want normalized rev-1", record.Provenance.SourceRevision.RevisionID)
	}
	if record.Provenance.Inputs[0].Kind != "dsl" || record.Provenance.Inputs[1].Kind != "parameter-table" {
		t.Fatalf("Provenance.Inputs not normalized deterministically: %#v", record.Provenance.Inputs)
	}
	if record.Identity.ID == "" || !isLowerHex64(record.Identity.ID) {
		t.Fatalf("Identity.ID = %q, want non-empty lowercase SHA-256 hex", record.Identity.ID)
	}
	if record.Identity.Family != FamilyExecution {
		t.Fatalf("Identity.Family = %q, want %q", record.Identity.Family, FamilyExecution)
	}
	if record.Identity.Version != CurrentVersion {
		t.Fatalf("Identity.Version = %q, want %q", record.Identity.Version, CurrentVersion)
	}
	if err := ValidateExecutionRecord(record); err != nil {
		t.Fatalf("ValidateExecutionRecord(built) returned error: %v", err)
	}
}

func TestBuildExecutionRecordDeterministicForEquivalentInput(t *testing.T) {
	firstInput := validExecutionRecordInput()
	secondInput := equivalentExecutionRecordInput()

	first, err := BuildExecutionRecord(firstInput)
	if err != nil {
		t.Fatalf("BuildExecutionRecord(first) returned error: %v", err)
	}
	second, err := BuildExecutionRecord(secondInput)
	if err != nil {
		t.Fatalf("BuildExecutionRecord(second) returned error: %v", err)
	}

	if first.Identity != second.Identity {
		t.Fatalf("equivalent identities differ:\nfirst:  %#v\nsecond: %#v", first.Identity, second.Identity)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent records differ:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if got := first.RelatedRecords.ArtifactRecordIDs; !reflect.DeepEqual(got, []string{"artifact-1", "artifact-2"}) {
		t.Fatalf("ArtifactRecordIDs = %#v, want sorted/deduped IDs", got)
	}
	if got := first.Jobs[0].JobID; got != "job-a" {
		t.Fatalf("first job ID = %q, want job-a", got)
	}
	if got := first.Jobs[1].Steps[0].Index; got != 1 {
		t.Fatalf("job-b first step index = %d, want deterministic order by explicit index", got)
	}
	if first.Execution.StartedAt.Location() != time.UTC {
		t.Fatalf("Execution.StartedAt location = %v, want UTC", first.Execution.StartedAt.Location())
	}
	if first.Jobs[1].Steps[1].StartedAt.Location() != time.UTC {
		t.Fatalf("step StartedAt location = %v, want UTC", first.Jobs[1].Steps[1].StartedAt.Location())
	}
}

func TestExecutionRecordIdentityChangesWithSemanticMaterial(t *testing.T) {
	base := validExecutionRecordInput()
	baseRecord, err := BuildExecutionRecord(base)
	if err != nil {
		t.Fatalf("BuildExecutionRecord(base) returned error: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ExecutionRecordInput)
	}{
		{name: "execution outcome", mutate: func(input *ExecutionRecordInput) { input.Execution.Outcome = ExecutionOutcomeFailed }},
		{name: "job outcome", mutate: func(input *ExecutionRecordInput) { input.Jobs[0].Outcome = ExecutionOutcomeFailed }},
		{name: "step outcome", mutate: func(input *ExecutionRecordInput) { input.Jobs[0].Steps[0].Outcome = ExecutionStepOutcomeFailed }},
		{name: "plan material", mutate: func(input *ExecutionRecordInput) { input.Execution.PlanHash = "plan-hash-2" }},
		{name: "related failure link", mutate: func(input *ExecutionRecordInput) { input.RelatedRecords.FailureRecordIDs = []string{"failure-2"} }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneExecutionRecordInput(base)
			tc.mutate(&changed)

			got, err := BuildExecutionRecord(changed)
			if err != nil {
				t.Fatalf("BuildExecutionRecord(changed) returned error: %v", err)
			}
			if got.Identity.ID == baseRecord.Identity.ID {
				t.Fatalf("identity ID did not change after %s changed: %q", tc.name, got.Identity.ID)
			}
		})
	}
}

func TestNormalizeExecutionRecordCanonicalizesAndCopies(t *testing.T) {
	record, err := BuildExecutionRecord(validExecutionRecordInput())
	if err != nil {
		t.Fatalf("BuildExecutionRecord(valid) returned error: %v", err)
	}

	normalized := NormalizeExecutionRecord(ExecutionRecord{
		Family:    Family(" \t" + string(FamilyExecution) + "\n"),
		Version:   " " + CurrentVersion + "\t",
		RecordKey: "\n" + record.RecordKey + " ",
		Identity:  record.Identity,
		Provenance: Provenance{
			SourceRevision: SourceRevisionProvenance{RevisionID: " rev-1 "},
			Inputs: []ProvenanceInput{
				{Kind: " parameter-table ", Identity: " params-a ", DigestSHA256: digestC()},
				{Kind: "\tdsl", Identity: "model-a\n", DigestSHA256: digestD()},
			},
			Plan:    PlanProvenance{PlanID: " plan-1 ", PlanHash: " plan-hash-1 "},
			Linkage: LinkageProvenance{ProductKey: " product-a ", JobID: " job-a ", StepRef: " parse "},
			Evidence: []EvidenceReference{
				{Kind: " log ", Ref: "logs/run.log ", DigestSHA256: digestE()},
				{Kind: " artifact ", Ref: " out/model.step ", DigestSHA256: digestF()},
			},
			Runtime: RuntimeProvenance{ToolID: " parametron ", RuntimeID: " runtime-1 ", Adapter: " freecad "},
		},
		Execution: ExecutionSummary{
			Outcome:   " succeeded ",
			PlanID:    " plan-1 ",
			PlanHash:  " plan-hash-1 ",
			StartedAt: timePtr(time.Date(2026, 3, 4, 13, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))),
			EndedAt:   timePtr(time.Date(2026, 3, 4, 13, 5, 0, 0, time.FixedZone("UTC+3", 3*60*60))),
			Duration:  5 * time.Minute,
		},
		Jobs: []ExecutionJobSummary{
			{
				JobID:      " job-b ",
				ProductKey: " product-b ",
				Outcome:    " succeeded ",
				Steps: []ExecutionStepSummary{
					{Index: 2, StepRef: " export ", Outcome: " succeeded ", Attempts: 2},
					{Index: 1, StepRef: " solve ", Outcome: " succeeded ", Attempts: 1},
				},
			},
			{
				JobID:      " job-a ",
				ProductKey: " product-a ",
				Outcome:    " succeeded ",
				Steps: []ExecutionStepSummary{
					{Index: 2, StepRef: " validate ", Outcome: " skipped "},
					{Index: 1, StepRef: " parse ", Outcome: " succeeded "},
				},
			},
		},
		RelatedRecords: ExecutionRelatedRecords{
			ArtifactRecordIDs:     []string{" artifact-2 ", "artifact-1", "artifact-2"},
			FailureRecordIDs:      []string{" failure-1 ", "failure-1"},
			ObservationRecordIDs:  []string{" observation-2 ", "observation-1"},
			ReferenceRecordIDs:    []string{" reference-1 ", "reference-1"},
			VerificationRecordIDs: []string{" verification-2 ", "verification-1"},
		},
	})

	if normalized.Family != FamilyExecution || normalized.Version != CurrentVersion || normalized.RecordKey != record.RecordKey {
		t.Fatalf("root fields not normalized: %#v", normalized)
	}
	if normalized.Execution.PlanID != "plan-1" || normalized.Execution.StartedAt.Location() != time.UTC {
		t.Fatalf("execution summary not normalized: %#v", normalized.Execution)
	}
	if got := normalized.Jobs[0].JobID; got != "job-a" {
		t.Fatalf("Jobs[0].JobID = %q, want job-a", got)
	}
	if got := normalized.Jobs[0].Steps[0].Index; got != 1 {
		t.Fatalf("Jobs[0].Steps[0].Index = %d, want preserved explicit index 1", got)
	}
	if got := normalized.RelatedRecords.ArtifactRecordIDs; !reflect.DeepEqual(got, []string{"artifact-1", "artifact-2"}) {
		t.Fatalf("ArtifactRecordIDs = %#v, want sorted/deduped IDs", got)
	}

	second := NormalizeExecutionRecord(ExecutionRecord{
		Jobs: []ExecutionJobSummary{
			{JobID: "job-a", ProductKey: "product-a", Steps: []ExecutionStepSummary{{Index: 1, StepRef: "parse"}}},
		},
		RelatedRecords: ExecutionRelatedRecords{ArtifactRecordIDs: []string{"artifact-1"}},
	})
	normalized.Jobs[0].Steps[0].StepRef = "mutated"
	normalized.RelatedRecords.ArtifactRecordIDs[0] = "mutated"
	if second.Jobs[0].Steps[0].StepRef != "parse" {
		t.Fatalf("normalized job steps share mutable storage: %#v", second.Jobs)
	}
	if second.RelatedRecords.ArtifactRecordIDs[0] != "artifact-1" {
		t.Fatalf("normalized related IDs share mutable storage: %#v", second.RelatedRecords.ArtifactRecordIDs)
	}
}

func TestValidateExecutionRecordAcceptsBuiltRecord(t *testing.T) {
	record, err := BuildExecutionRecord(validExecutionRecordInput())
	if err != nil {
		t.Fatalf("BuildExecutionRecord(valid) returned error: %v", err)
	}
	if err := ValidateExecutionRecord(record); err != nil {
		t.Fatalf("ValidateExecutionRecord(built) returned error: %v", err)
	}
}

func TestValidateExecutionRecordRejectsInvalidRecords(t *testing.T) {
	valid := mustBuildExecutionRecord(t, validExecutionRecordInput())

	tests := []struct {
		name   string
		mutate func(*ExecutionRecord)
	}{
		{name: "wrong family", mutate: func(record *ExecutionRecord) { record.Family = FamilyArtifact }},
		{name: "unsupported version", mutate: func(record *ExecutionRecord) { record.Version = "1.1" }},
		{name: "missing record key", mutate: func(record *ExecutionRecord) { record.RecordKey = " " }},
		{name: "invalid provenance", mutate: func(record *ExecutionRecord) { record.Provenance.Inputs[0].DigestSHA256 = upperDigestA() }},
		{name: "missing identity", mutate: func(record *ExecutionRecord) { record.Identity = Identity{} }},
		{name: "identity family mismatch", mutate: func(record *ExecutionRecord) { record.Identity.Family = FamilyArtifact }},
		{name: "identity version mismatch", mutate: func(record *ExecutionRecord) { record.Identity.Version = "1.1" }},
		{name: "identity digest mismatch after payload mutation", mutate: func(record *ExecutionRecord) { record.Execution.PlanHash = "plan-hash-2" }},
		{name: "invalid execution outcome", mutate: func(record *ExecutionRecord) { record.Execution.Outcome = "partial" }},
		{name: "invalid job outcome", mutate: func(record *ExecutionRecord) { record.Jobs[0].Outcome = "queued" }},
		{name: "missing job ID", mutate: func(record *ExecutionRecord) { record.Jobs[0].JobID = " " }},
		{name: "missing product key", mutate: func(record *ExecutionRecord) { record.Jobs[0].ProductKey = "\t" }},
		{name: "invalid step outcome", mutate: func(record *ExecutionRecord) { record.Jobs[0].Steps[0].Outcome = "running" }},
		{name: "missing step ref", mutate: func(record *ExecutionRecord) { record.Jobs[0].Steps[0].StepRef = " " }},
		{name: "negative step index", mutate: func(record *ExecutionRecord) { record.Jobs[0].Steps[0].Index = -1 }},
		{name: "negative attempts", mutate: func(record *ExecutionRecord) { record.Jobs[0].Steps[0].Attempts = -1 }},
		{name: "negative execution duration", mutate: func(record *ExecutionRecord) { record.Execution.Duration = -time.Second }},
		{name: "negative step duration", mutate: func(record *ExecutionRecord) { record.Jobs[0].Steps[0].Duration = -time.Second }},
		{name: "execution ended before start", mutate: func(record *ExecutionRecord) {
			record.Execution.StartedAt = timePtr(time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC))
			record.Execution.EndedAt = timePtr(time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC))
		}},
		{name: "step ended before start", mutate: func(record *ExecutionRecord) {
			record.Jobs[0].Steps[0].StartedAt = timePtr(time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC))
			record.Jobs[0].Steps[0].EndedAt = timePtr(time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC))
		}},
		{name: "duplicate step index", mutate: func(record *ExecutionRecord) { record.Jobs[0].Steps[1].Index = record.Jobs[0].Steps[0].Index }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := cloneExecutionRecord(valid)
			tc.mutate(&record)

			if err := ValidateExecutionRecord(record); !errors.Is(err, ErrInvalidExecutionRecord) {
				t.Fatalf("ValidateExecutionRecord error = %v, want ErrInvalidExecutionRecord", err)
			}
		})
	}
}

func TestExecutionRecordErrorsAreInspectable(t *testing.T) {
	record := mustBuildExecutionRecord(t, validExecutionRecordInput())

	tests := []struct {
		name       string
		record     ExecutionRecord
		wantErrors []error
	}{
		{
			name: "invalid execution record",
			record: withExecutionRecord(record, func(record *ExecutionRecord) {
				record.Execution.Outcome = "unknown"
			}),
			wantErrors: []error{ErrInvalidExecutionRecord},
		},
		{
			name: "invalid provenance",
			record: withExecutionRecord(record, func(record *ExecutionRecord) {
				record.Provenance.Inputs[0].DigestSHA256 = "abc123"
			}),
			wantErrors: []error{ErrInvalidExecutionRecord, ErrInvalidProvenance},
		},
		{
			name: "invalid identity",
			record: withExecutionRecord(record, func(record *ExecutionRecord) {
				record.Identity.ID = "not-a-digest"
			}),
			wantErrors: []error{ErrInvalidExecutionRecord, ErrInvalidIdentity},
		},
		{
			name: "invalid version",
			record: withExecutionRecord(record, func(record *ExecutionRecord) {
				record.Version = "2.0"
			}),
			wantErrors: []error{ErrInvalidExecutionRecord, ErrInvalidVersion},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateExecutionRecord(tc.record)
			if err == nil {
				t.Fatal("ValidateExecutionRecord returned nil error, want validation failure")
			}
			for _, want := range tc.wantErrors {
				if !errors.Is(err, want) {
					t.Fatalf("ValidateExecutionRecord error = %v, want errors.Is(..., %v)", err, want)
				}
			}
		})
	}
}

func TestExecutionRecordBuildAndNormalizeAreCopySafe(t *testing.T) {
	input := validExecutionRecordInput()
	built, err := BuildExecutionRecord(input)
	if err != nil {
		t.Fatalf("BuildExecutionRecord(valid) returned error: %v", err)
	}

	input.Jobs[0].JobID = "mutated-job"
	input.Jobs[0].Steps[0].StepRef = "mutated-step"
	input.RelatedRecords.ArtifactRecordIDs[0] = "mutated-artifact"
	input.Provenance.Inputs[0].Identity = "mutated-input"
	if built.Jobs[0].JobID != "job-a" {
		t.Fatalf("built jobs changed after input mutation: %#v", built.Jobs)
	}
	if built.Jobs[0].Steps[0].StepRef != "parse" {
		t.Fatalf("built steps changed after input mutation: %#v", built.Jobs[0].Steps)
	}
	if built.RelatedRecords.ArtifactRecordIDs[0] != "artifact-1" {
		t.Fatalf("built related IDs changed after input mutation: %#v", built.RelatedRecords.ArtifactRecordIDs)
	}
	if built.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("built provenance inputs changed after input mutation: %#v", built.Provenance.Inputs)
	}

	normalizedInput := ExecutionRecord{
		Provenance: Provenance{
			Inputs: []ProvenanceInput{{Kind: "dsl", Identity: "model-a"}},
		},
		Jobs: []ExecutionJobSummary{{
			JobID: "job-a",
			Steps: []ExecutionStepSummary{{
				Index:   1,
				StepRef: "parse",
			}},
		}},
		RelatedRecords: ExecutionRelatedRecords{ArtifactRecordIDs: []string{"artifact-1"}},
	}
	normalized := NormalizeExecutionRecord(normalizedInput)
	normalizedInput.Provenance.Inputs[0].Identity = "mutated-input"
	normalizedInput.Jobs[0].JobID = "mutated-job"
	normalizedInput.Jobs[0].Steps[0].StepRef = "mutated-step"
	normalizedInput.RelatedRecords.ArtifactRecordIDs[0] = "mutated-artifact"
	if normalized.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("normalized provenance inputs changed after input mutation: %#v", normalized.Provenance.Inputs)
	}
	if normalized.Jobs[0].JobID != "job-a" || normalized.Jobs[0].Steps[0].StepRef != "parse" {
		t.Fatalf("normalized jobs changed after input mutation: %#v", normalized.Jobs)
	}
	if normalized.RelatedRecords.ArtifactRecordIDs[0] != "artifact-1" {
		t.Fatalf("normalized related IDs changed after input mutation: %#v", normalized.RelatedRecords.ArtifactRecordIDs)
	}
}

func TestExecutionRecordJSONShapeUsesContractFields(t *testing.T) {
	record := mustBuildExecutionRecord(t, validExecutionRecordInput())

	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal(ExecutionRecord) returned error: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal(root) returned error: %v", err)
	}
	for _, field := range []string{"family", "version", "recordKey", "identity", "provenance", "execution", "jobs", "relatedRecords"} {
		if _, ok := got[field]; !ok {
			t.Fatalf("root JSON field %q missing from %s", field, payload)
		}
	}
}

func validExecutionRecordInput() ExecutionRecordInput {
	started := time.Date(2026, 3, 4, 13, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	ended := time.Date(2026, 3, 4, 13, 5, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	stepStarted := time.Date(2026, 3, 4, 13, 1, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	stepEnded := time.Date(2026, 3, 4, 13, 2, 0, 0, time.FixedZone("UTC+3", 3*60*60))

	return ExecutionRecordInput{
		RecordKey: " execution-record-1 ",
		Provenance: Provenance{
			SourceRevision: SourceRevisionProvenance{
				RevisionID:   " rev-1 ",
				AssetID:      " asset-1 ",
				DigestSHA256: digestA(),
			},
			Inputs: []ProvenanceInput{
				{Kind: " parameter-table ", Identity: " params-a ", DigestSHA256: digestC()},
				{Kind: " dsl ", Identity: " model-a ", DigestSHA256: digestD()},
			},
			Plan: PlanProvenance{
				PlanID:   " plan-1 ",
				PlanHash: " plan-hash-1 ",
			},
			Linkage: LinkageProvenance{
				ProductKey: " product-a ",
				JobID:      " job-a ",
				StepRef:    " parse ",
			},
			Evidence: []EvidenceReference{
				{Kind: " log ", Ref: " logs/run.log ", DigestSHA256: digestE()},
				{Kind: " artifact ", Ref: " out/model.step ", DigestSHA256: digestF()},
			},
			Runtime: RuntimeProvenance{
				ToolID:    " parametron ",
				RuntimeID: " runtime-1 ",
				Adapter:   " freecad ",
			},
		},
		Execution: ExecutionSummary{
			Outcome:   " succeeded ",
			PlanID:    " plan-1 ",
			PlanHash:  " plan-hash-1 ",
			StartedAt: &started,
			EndedAt:   &ended,
			Duration:  5 * time.Minute,
		},
		Jobs: []ExecutionJobSummary{
			{
				JobID:      " job-b ",
				ProductKey: " product-b ",
				Outcome:    " succeeded ",
				Steps: []ExecutionStepSummary{
					{Index: 2, StepRef: " export ", Outcome: " succeeded ", Attempts: 2, StartedAt: &stepStarted, EndedAt: &stepEnded, Duration: time.Minute},
					{Index: 1, StepRef: " solve ", Outcome: " succeeded ", Attempts: 1},
				},
			},
			{
				JobID:      " job-a ",
				ProductKey: " product-a ",
				Outcome:    " succeeded ",
				Steps: []ExecutionStepSummary{
					{Index: 2, StepRef: " validate ", Outcome: " skipped "},
					{Index: 1, StepRef: " parse ", Outcome: " succeeded ", Attempts: 1},
				},
			},
		},
		RelatedRecords: ExecutionRelatedRecords{
			ArtifactRecordIDs:     []string{" artifact-2 ", "artifact-1", "artifact-2", " "},
			FailureRecordIDs:      []string{" failure-1 ", "failure-1"},
			ObservationRecordIDs:  []string{" observation-2 ", "observation-1"},
			ReferenceRecordIDs:    []string{" reference-1 ", "reference-1"},
			VerificationRecordIDs: []string{" verification-2 ", "verification-1"},
		},
	}
}

func equivalentExecutionRecordInput() ExecutionRecordInput {
	input := validExecutionRecordInput()
	input.RecordKey = "\texecution-record-1\n"
	input.Provenance.Inputs = []ProvenanceInput{
		{Kind: "dsl", Identity: "model-a", DigestSHA256: " " + digestD() + " "},
		{Kind: "parameter-table", Identity: "params-a", DigestSHA256: digestC()},
	}
	input.Provenance.Evidence = []EvidenceReference{
		{Kind: "artifact", Ref: "out/model.step", DigestSHA256: digestF()},
		{Kind: "log", Ref: "logs/run.log", DigestSHA256: digestE()},
	}
	input.Jobs = []ExecutionJobSummary{
		input.Jobs[1],
		input.Jobs[0],
	}
	input.Jobs[0].Steps = []ExecutionStepSummary{
		input.Jobs[0].Steps[1],
		input.Jobs[0].Steps[0],
	}
	input.Jobs[1].Steps = []ExecutionStepSummary{
		input.Jobs[1].Steps[1],
		input.Jobs[1].Steps[0],
	}
	input.RelatedRecords.ArtifactRecordIDs = []string{"artifact-1", " artifact-2 ", "artifact-1"}
	input.RelatedRecords.FailureRecordIDs = []string{"failure-1"}
	input.RelatedRecords.ObservationRecordIDs = []string{"observation-1", "observation-2"}
	input.RelatedRecords.ReferenceRecordIDs = []string{"reference-1"}
	input.RelatedRecords.VerificationRecordIDs = []string{"verification-1", "verification-2"}
	return input
}

func mustBuildExecutionRecord(t *testing.T, input ExecutionRecordInput) ExecutionRecord {
	t.Helper()
	record, err := BuildExecutionRecord(input)
	if err != nil {
		t.Fatalf("BuildExecutionRecord(%#v) returned error: %v", input, err)
	}
	return record
}

func withExecutionRecord(base ExecutionRecord, mutate func(*ExecutionRecord)) ExecutionRecord {
	base = cloneExecutionRecord(base)
	mutate(&base)
	return base
}

func cloneExecutionRecord(record ExecutionRecord) ExecutionRecord {
	record.Provenance.Inputs = append([]ProvenanceInput(nil), record.Provenance.Inputs...)
	record.Provenance.Evidence = append([]EvidenceReference(nil), record.Provenance.Evidence...)
	record.Jobs = cloneExecutionJobs(record.Jobs)
	record.RelatedRecords = cloneExecutionRelatedRecords(record.RelatedRecords)
	return record
}

func cloneExecutionRecordInput(input ExecutionRecordInput) ExecutionRecordInput {
	input.Provenance.Inputs = append([]ProvenanceInput(nil), input.Provenance.Inputs...)
	input.Provenance.Evidence = append([]EvidenceReference(nil), input.Provenance.Evidence...)
	input.Jobs = cloneExecutionJobs(input.Jobs)
	input.RelatedRecords = cloneExecutionRelatedRecords(input.RelatedRecords)
	return input
}

func cloneExecutionJobs(jobs []ExecutionJobSummary) []ExecutionJobSummary {
	out := append([]ExecutionJobSummary(nil), jobs...)
	for i := range out {
		out[i].Steps = append([]ExecutionStepSummary(nil), out[i].Steps...)
	}
	return out
}

func cloneExecutionRelatedRecords(related ExecutionRelatedRecords) ExecutionRelatedRecords {
	return ExecutionRelatedRecords{
		ArtifactRecordIDs:     append([]string(nil), related.ArtifactRecordIDs...),
		FailureRecordIDs:      append([]string(nil), related.FailureRecordIDs...),
		ObservationRecordIDs:  append([]string(nil), related.ObservationRecordIDs...),
		ReferenceRecordIDs:    append([]string(nil), related.ReferenceRecordIDs...),
		VerificationRecordIDs: append([]string(nil), related.VerificationRecordIDs...),
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}
