package recordcontract

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBuildFailureRecordValidatesNormalizedRecord(t *testing.T) {
	record, err := BuildFailureRecord(validFailureRecordInput())
	if err != nil {
		t.Fatalf("BuildFailureRecord(valid) returned error: %v", err)
	}

	if record.Family != FamilyFailure {
		t.Fatalf("Family = %q, want %q", record.Family, FamilyFailure)
	}
	if record.Version != CurrentVersion {
		t.Fatalf("Version = %q, want %q", record.Version, CurrentVersion)
	}
	if record.RecordKey != "failure-record-1" {
		t.Fatalf("RecordKey = %q, want trimmed failure-record-1", record.RecordKey)
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
	if record.Identity.Algorithm != IdentityAlgorithmSHA256CanonicalV1 {
		t.Fatalf("Identity.Algorithm = %q, want %q", record.Identity.Algorithm, IdentityAlgorithmSHA256CanonicalV1)
	}
	if record.Identity.Family != FamilyFailure {
		t.Fatalf("Identity.Family = %q, want %q", record.Identity.Family, FamilyFailure)
	}
	if record.Identity.Version != CurrentVersion {
		t.Fatalf("Identity.Version = %q, want %q", record.Identity.Version, CurrentVersion)
	}
	if record.Failure.Class != FailureClassExecution {
		t.Fatalf("Failure.Class = %q, want %q", record.Failure.Class, FailureClassExecution)
	}
	if record.Failure.Message != "solver failed" {
		t.Fatalf("Failure.Message = %q, want normalized solver failed", record.Failure.Message)
	}
	if record.Failure.Severity != FailureSeverityError || record.Failure.Stage != FailureStageExecution {
		t.Fatalf("Failure severity/stage not normalized: %#v", record.Failure)
	}
	if record.Failure.OccurredAt == nil || record.Failure.OccurredAt.Location() != time.UTC {
		t.Fatalf("Failure.OccurredAt = %#v, want UTC timestamp", record.Failure.OccurredAt)
	}
	if err := ValidateFailureRecord(record); err != nil {
		t.Fatalf("ValidateFailureRecord(built) returned error: %v", err)
	}
}

func TestBuildFailureRecordDeterministicForEquivalentInput(t *testing.T) {
	first, err := BuildFailureRecord(validFailureRecordInput())
	if err != nil {
		t.Fatalf("BuildFailureRecord(first) returned error: %v", err)
	}
	second, err := BuildFailureRecord(equivalentFailureRecordInput())
	if err != nil {
		t.Fatalf("BuildFailureRecord(second) returned error: %v", err)
	}

	if first.Identity != second.Identity {
		t.Fatalf("equivalent identities differ:\nfirst:  %#v\nsecond: %#v", first.Identity, second.Identity)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent records differ:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if got := first.Failure.Evidence; !reflect.DeepEqual(got, []FailureEvidence{
		{SourceKind: "log", SourceRef: "logs/run.log", DigestSHA256: digestE()},
		{SourceKind: "runtime", SourceRef: "runtime://failure/solver", DigestSHA256: digestF()},
	}) {
		t.Fatalf("Failure.Evidence = %#v, want sorted/normalized evidence", got)
	}
}

func TestFailureRecordIdentityChangesWhenPayloadChanges(t *testing.T) {
	base := validFailureRecordInput()
	baseRecord := mustBuildFailureRecord(t, base)

	tests := []struct {
		name   string
		mutate func(*FailureRecordInput)
	}{
		{name: "message", mutate: func(input *FailureRecordInput) { input.Failure.Message = "runtime failed" }},
		{name: "class", mutate: func(input *FailureRecordInput) { input.Failure.Class = FailureClassRuntime }},
		{name: "code", mutate: func(input *FailureRecordInput) { input.Failure.Code = "E-RUNTIME" }},
		{name: "evidence digest", mutate: func(input *FailureRecordInput) { input.Failure.Evidence[0].DigestSHA256 = digestA() }},
		{name: "linkage", mutate: func(input *FailureRecordInput) { input.Failure.Linkage.StepRef = "export" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneFailureRecordInput(base)
			tc.mutate(&changed)

			got, err := BuildFailureRecord(changed)
			if err != nil {
				t.Fatalf("BuildFailureRecord(changed) returned error: %v", err)
			}
			if got.Identity.ID == baseRecord.Identity.ID {
				t.Fatalf("identity ID did not change after %s changed: %q", tc.name, got.Identity.ID)
			}
		})
	}
}

func TestValidateFailureRecordRejectsIdentityMismatch(t *testing.T) {
	record := mustBuildFailureRecord(t, validFailureRecordInput())
	record.Failure.Message = "different failure"

	err := ValidateFailureRecord(record)
	if !errors.Is(err, ErrInvalidFailureRecord) {
		t.Fatalf("ValidateFailureRecord error = %v, want ErrInvalidFailureRecord", err)
	}
}

func TestValidateFailureRecordRejectsInvalidRequiredFields(t *testing.T) {
	valid := mustBuildFailureRecord(t, validFailureRecordInput())

	tests := []struct {
		name       string
		mutate     func(*FailureRecord)
		wantErrors []error
	}{
		{
			name:       "blank record key",
			mutate:     func(record *FailureRecord) { record.RecordKey = " " },
			wantErrors: []error{ErrInvalidFailureRecord},
		},
		{
			name:       "blank failure class",
			mutate:     func(record *FailureRecord) { record.Failure.Class = "\t" },
			wantErrors: []error{ErrInvalidFailureRecord},
		},
		{
			name:       "blank failure message",
			mutate:     func(record *FailureRecord) { record.Failure.Message = "\n" },
			wantErrors: []error{ErrInvalidFailureRecord},
		},
		{
			name:       "missing identity",
			mutate:     func(record *FailureRecord) { record.Identity = Identity{} },
			wantErrors: []error{ErrInvalidFailureRecord, ErrInvalidIdentity},
		},
		{
			name: "invalid provenance",
			mutate: func(record *FailureRecord) {
				record.Provenance.Inputs[0].DigestSHA256 = upperDigestA()
			},
			wantErrors: []error{ErrInvalidFailureRecord, ErrInvalidProvenance},
		},
		{
			name:       "wrong family",
			mutate:     func(record *FailureRecord) { record.Family = FamilyExecution },
			wantErrors: []error{ErrInvalidFailureRecord},
		},
		{
			name:       "unsupported version",
			mutate:     func(record *FailureRecord) { record.Version = "1.1" },
			wantErrors: []error{ErrInvalidFailureRecord, ErrInvalidVersion},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := cloneFailureRecord(valid)
			tc.mutate(&record)

			err := ValidateFailureRecord(record)
			if err == nil {
				t.Fatal("ValidateFailureRecord returned nil error, want validation failure")
			}
			for _, want := range tc.wantErrors {
				if !errors.Is(err, want) {
					t.Fatalf("ValidateFailureRecord error = %v, want errors.Is(..., %v)", err, want)
				}
			}
		})
	}
}

func TestValidateFailureRecordRejectsUnknownVocabulary(t *testing.T) {
	valid := mustBuildFailureRecord(t, validFailureRecordInput())

	tests := []struct {
		name   string
		mutate func(*FailureRecord)
	}{
		{name: "unknown class", mutate: func(record *FailureRecord) { record.Failure.Class = "transient" }},
		{name: "unknown severity", mutate: func(record *FailureRecord) { record.Failure.Severity = "warning" }},
		{name: "unknown stage", mutate: func(record *FailureRecord) { record.Failure.Stage = "handoff" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := cloneFailureRecord(valid)
			tc.mutate(&record)

			if err := ValidateFailureRecord(record); !errors.Is(err, ErrInvalidFailureRecord) {
				t.Fatalf("ValidateFailureRecord error = %v, want ErrInvalidFailureRecord", err)
			}
		})
	}
}

func TestFailureRecordNormalizesEvidenceDeterministically(t *testing.T) {
	normalized := NormalizeFailureRecord(FailureRecord{
		Family:    Family(" " + string(FamilyFailure) + "\t"),
		Version:   " " + CurrentVersion + "\n",
		RecordKey: "\tfailure-record-1 ",
		Failure: FailureSummary{
			Class:   " execution ",
			Message: " solver failed ",
			Evidence: []FailureEvidence{
				{SourceKind: " runtime ", SourceRef: " runtime://failure/solver ", DigestSHA256: " " + digestF() + " "},
				{SourceKind: " log ", SourceRef: " logs/run.log ", DigestSHA256: "\t" + digestE() + "\n"},
			},
		},
	})

	wantEvidence := []FailureEvidence{
		{SourceKind: "log", SourceRef: "logs/run.log", DigestSHA256: digestE()},
		{SourceKind: "runtime", SourceRef: "runtime://failure/solver", DigestSHA256: digestF()},
	}
	if !reflect.DeepEqual(normalized.Failure.Evidence, wantEvidence) {
		t.Fatalf("Failure.Evidence = %#v, want %#v", normalized.Failure.Evidence, wantEvidence)
	}

	empty := NormalizeFailureRecord(FailureRecord{})
	if empty.Failure.Evidence == nil {
		t.Fatal("NormalizeFailureRecord empty Evidence = nil, want empty non-nil slice")
	}
	if len(empty.Failure.Evidence) != 0 {
		t.Fatalf("NormalizeFailureRecord empty Evidence length = %d, want 0", len(empty.Failure.Evidence))
	}
}

func TestValidateFailureRecordRejectsInvalidEvidence(t *testing.T) {
	valid := mustBuildFailureRecord(t, validFailureRecordInput())

	tests := []struct {
		name   string
		mutate func(*FailureRecord)
	}{
		{name: "duplicate normalized evidence", mutate: func(record *FailureRecord) {
			record.Failure.Evidence = append(record.Failure.Evidence, FailureEvidence{
				SourceKind:   " log ",
				SourceRef:    " logs/run.log ",
				DigestSHA256: " " + digestE() + " ",
			})
		}},
		{name: "malformed digest", mutate: func(record *FailureRecord) {
			record.Failure.Evidence[0].DigestSHA256 = "abc123"
		}},
		{name: "source kind without ref", mutate: func(record *FailureRecord) {
			record.Failure.Evidence[0].SourceKind = "log"
			record.Failure.Evidence[0].SourceRef = " "
		}},
		{name: "source ref without kind", mutate: func(record *FailureRecord) {
			record.Failure.Evidence[0].SourceKind = "\t"
			record.Failure.Evidence[0].SourceRef = "logs/run.log"
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := cloneFailureRecord(valid)
			tc.mutate(&record)

			if err := ValidateFailureRecord(record); !errors.Is(err, ErrInvalidFailureRecord) {
				t.Fatalf("ValidateFailureRecord error = %v, want ErrInvalidFailureRecord", err)
			}
		})
	}
}

func TestValidateFailureRecordChecksTimeoutCanceledConsistency(t *testing.T) {
	tests := []struct {
		name      string
		summary   FailureSummary
		wantValid bool
	}{
		{name: "timeout flag requires timeout class", summary: FailureSummary{Class: FailureClassExecution, Message: "timed out", Timeout: true}},
		{name: "canceled flag requires canceled class", summary: FailureSummary{Class: FailureClassExecution, Message: "canceled", Canceled: true}},
		{name: "timeout and canceled mutually exclusive", summary: FailureSummary{Class: FailureClassTimeout, Message: "timed out", Timeout: true, Canceled: true}},
		{name: "timeout class requires timeout flag", summary: FailureSummary{Class: FailureClassTimeout, Message: "timed out"}},
		{name: "canceled class requires canceled flag", summary: FailureSummary{Class: FailureClassCanceled, Message: "canceled"}},
		{name: "timeout aligned", summary: FailureSummary{Class: FailureClassTimeout, Message: "timed out", Timeout: true}, wantValid: true},
		{name: "canceled aligned", summary: FailureSummary{Class: FailureClassCanceled, Message: "canceled", Canceled: true}, wantValid: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validFailureRecordInput()
			input.Failure = tc.summary

			record, err := BuildFailureRecord(input)
			if tc.wantValid {
				if err != nil {
					t.Fatalf("BuildFailureRecord(valid flag alignment) returned error: %v", err)
				}
				if err := ValidateFailureRecord(record); err != nil {
					t.Fatalf("ValidateFailureRecord(valid flag alignment) returned error: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidFailureRecord) {
				t.Fatalf("BuildFailureRecord error = %v, want ErrInvalidFailureRecord", err)
			}
		})
	}
}

func TestValidateFailureRecordRejectsNegativeRetryCount(t *testing.T) {
	tests := []struct {
		name       string
		retryCount int
		wantValid  bool
	}{
		{name: "negative", retryCount: -1},
		{name: "zero", retryCount: 0, wantValid: true},
		{name: "positive", retryCount: 2, wantValid: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validFailureRecordInput()
			input.Failure.RetryCount = tc.retryCount

			record, err := BuildFailureRecord(input)
			if tc.wantValid {
				if err != nil {
					t.Fatalf("BuildFailureRecord(valid retry count) returned error: %v", err)
				}
				if err := ValidateFailureRecord(record); err != nil {
					t.Fatalf("ValidateFailureRecord(valid retry count) returned error: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidFailureRecord) {
				t.Fatalf("BuildFailureRecord error = %v, want ErrInvalidFailureRecord", err)
			}
		})
	}
}

func TestFailureRecordNormalizesOccurredAtToUTC(t *testing.T) {
	firstInput := validFailureRecordInput()
	secondInput := equivalentFailureRecordInput()

	first := mustBuildFailureRecord(t, firstInput)
	second := mustBuildFailureRecord(t, secondInput)

	if first.Failure.OccurredAt == nil {
		t.Fatal("Failure.OccurredAt = nil, want timestamp")
	}
	if first.Failure.OccurredAt.Location() != time.UTC {
		t.Fatalf("Failure.OccurredAt location = %v, want UTC", first.Failure.OccurredAt.Location())
	}
	if !first.Failure.OccurredAt.Equal(time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("Failure.OccurredAt = %v, want equivalent UTC instant", first.Failure.OccurredAt)
	}
	if first.Identity != second.Identity {
		t.Fatalf("equivalent timestamp identities differ:\nfirst:  %#v\nsecond: %#v", first.Identity, second.Identity)
	}
}

func TestBuildFailureRecordCopiesInputSlices(t *testing.T) {
	input := validFailureRecordInput()
	built := mustBuildFailureRecord(t, input)

	input.Provenance.Inputs[0].Identity = "mutated-input"
	input.Provenance.Evidence[0].Ref = "mutated-provenance-evidence"
	input.Failure.Evidence[0].SourceRef = "mutated-failure-evidence"

	if built.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("built provenance inputs changed after input mutation: %#v", built.Provenance.Inputs)
	}
	if built.Provenance.Evidence[0].Ref != "out/model.step" {
		t.Fatalf("built provenance evidence changed after input mutation: %#v", built.Provenance.Evidence)
	}
	if built.Failure.Evidence[0].SourceRef != "logs/run.log" {
		t.Fatalf("built failure evidence changed after input mutation: %#v", built.Failure.Evidence)
	}
	if err := ValidateFailureRecord(built); err != nil {
		t.Fatalf("ValidateFailureRecord(built after caller mutation) returned error: %v", err)
	}
}

func TestNormalizeFailureRecordCopiesSlices(t *testing.T) {
	input := FailureRecord{
		Provenance: Provenance{
			Inputs:   []ProvenanceInput{{Kind: "dsl", Identity: "model-a"}},
			Evidence: []EvidenceReference{{Kind: "artifact", Ref: "out/model.step"}},
		},
		Failure: FailureSummary{
			Class:    FailureClassExecution,
			Message:  "solver failed",
			Evidence: []FailureEvidence{{SourceKind: "log", SourceRef: "logs/run.log"}},
		},
	}

	normalized := NormalizeFailureRecord(input)
	input.Provenance.Inputs[0].Identity = "mutated-input"
	input.Provenance.Evidence[0].Ref = "mutated-provenance-evidence"
	input.Failure.Evidence[0].SourceRef = "mutated-failure-evidence"

	if normalized.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("normalized provenance inputs changed after input mutation: %#v", normalized.Provenance.Inputs)
	}
	if normalized.Provenance.Evidence[0].Ref != "out/model.step" {
		t.Fatalf("normalized provenance evidence changed after input mutation: %#v", normalized.Provenance.Evidence)
	}
	if normalized.Failure.Evidence[0].SourceRef != "logs/run.log" {
		t.Fatalf("normalized failure evidence changed after input mutation: %#v", normalized.Failure.Evidence)
	}
}

func validFailureRecordInput() FailureRecordInput {
	occurredAt := time.Date(2026, 3, 4, 13, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	return FailureRecordInput{
		RecordKey: " failure-record-1 ",
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
				StepRef:    " solve ",
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
		Failure: FailureSummary{
			Class:      " execution ",
			Severity:   " error ",
			Stage:      " execution ",
			Code:       " E-SOLVE ",
			Message:    " solver failed ",
			RetryCount: 1,
			OccurredAt: &occurredAt,
			Linkage: FailureLinkage{
				JobID:      " job-a ",
				ProductKey: " product-a ",
				StepRef:    " solve ",
			},
			Location: FailureLocation{
				Path:      " model.root.sketch ",
				Component: " sketcher ",
				Field:     " constraint ",
			},
			Evidence: []FailureEvidence{
				{SourceKind: " runtime ", SourceRef: " runtime://failure/solver ", DigestSHA256: " " + digestF() + " "},
				{SourceKind: " log ", SourceRef: " logs/run.log ", DigestSHA256: "\t" + digestE() + "\n"},
			},
		},
	}
}

func equivalentFailureRecordInput() FailureRecordInput {
	input := validFailureRecordInput()
	occurredAt := time.Date(2026, 3, 4, 5, 0, 0, 0, time.FixedZone("UTC-5", -5*60*60))
	input.RecordKey = "\tfailure-record-1\n"
	input.Provenance.Inputs = []ProvenanceInput{
		{Kind: "dsl", Identity: "model-a", DigestSHA256: " " + digestD() + " "},
		{Kind: "parameter-table", Identity: "params-a", DigestSHA256: digestC()},
	}
	input.Provenance.Evidence = []EvidenceReference{
		{Kind: "artifact", Ref: "out/model.step", DigestSHA256: digestF()},
		{Kind: "log", Ref: "logs/run.log", DigestSHA256: digestE()},
	}
	input.Failure = FailureSummary{
		Class:      "\texecution\n",
		Severity:   "\terror ",
		Stage:      " execution\t",
		Code:       "\tE-SOLVE ",
		Message:    " solver failed\n",
		RetryCount: 1,
		OccurredAt: &occurredAt,
		Linkage: FailureLinkage{
			JobID:      "\tjob-a ",
			ProductKey: " product-a\n",
			StepRef:    "\tsolve ",
		},
		Location: FailureLocation{
			Path:      "\tmodel.root.sketch ",
			Component: " sketcher\n",
			Field:     "\tconstraint ",
		},
		Evidence: []FailureEvidence{
			{SourceKind: "log", SourceRef: "logs/run.log", DigestSHA256: " " + digestE() + " "},
			{SourceKind: "\truntime ", SourceRef: " runtime://failure/solver\n", DigestSHA256: digestF()},
		},
	}
	return input
}

func mustBuildFailureRecord(t *testing.T, input FailureRecordInput) FailureRecord {
	t.Helper()
	record, err := BuildFailureRecord(input)
	if err != nil {
		t.Fatalf("BuildFailureRecord(%#v) returned error: %v", input, err)
	}
	return record
}

func cloneFailureRecordInput(input FailureRecordInput) FailureRecordInput {
	input.Provenance.Inputs = append([]ProvenanceInput(nil), input.Provenance.Inputs...)
	input.Provenance.Evidence = append([]EvidenceReference(nil), input.Provenance.Evidence...)
	input.Failure.Evidence = append([]FailureEvidence(nil), input.Failure.Evidence...)
	return input
}

func cloneFailureRecord(record FailureRecord) FailureRecord {
	record.Provenance.Inputs = append([]ProvenanceInput(nil), record.Provenance.Inputs...)
	record.Provenance.Evidence = append([]EvidenceReference(nil), record.Provenance.Evidence...)
	record.Failure.Evidence = append([]FailureEvidence(nil), record.Failure.Evidence...)
	return record
}
