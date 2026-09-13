package recordmap_test

import (
	"errors"
	"reflect"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
)

const (
	runtimeResultRecordKey    = "failure:runtime:job-a:step-a"
	runtimeResultEvidenceKind = "runtime-result"
)

func TestMapRuntimeResultSuccessProducesNoFailureRecord(t *testing.T) {
	input := validRuntimeResultMappingInput(runtimeSuccessResult())
	input.Result.Error = &recordmap.RuntimeResultError{
		Classification: "internal_error",
		Message:        "ignored because status is success",
	}

	output, err := recordmap.MapRuntimeResult(input)
	if err != nil {
		t.Fatalf("MapRuntimeResult(success) returned error: %v", err)
	}
	if output.FailureRecord != nil {
		t.Fatalf("FailureRecord = %#v, want nil", output.FailureRecord)
	}

	failure, err := recordmap.MapRuntimeResultToFailureRecord(input)
	if err != nil {
		t.Fatalf("MapRuntimeResultToFailureRecord(success) returned error: %v", err)
	}
	if failure != nil {
		t.Fatalf("MapRuntimeResultToFailureRecord(success) = %#v, want nil", failure)
	}

	t.Run("still validates record key", func(t *testing.T) {
		input := validRuntimeResultMappingInput(runtimeSuccessResult())
		input.RecordKey = ""
		_, err := recordmap.MapRuntimeResult(input)
		assertInvalidRuntimeResultMapping(t, err)
	})

	t.Run("still validates digest", func(t *testing.T) {
		input := validRuntimeResultMappingInput(runtimeSuccessResult())
		input.EvidenceDigestSHA256 = "ABC"
		_, err := recordmap.MapRuntimeResult(input)
		assertInvalidRuntimeResultMapping(t, err)
	})

	t.Run("still validates schema version", func(t *testing.T) {
		input := validRuntimeResultMappingInput(runtimeSuccessResult())
		input.Result.SchemaVersion = "2.0"
		_, err := recordmap.MapRuntimeResult(input)
		assertInvalidRuntimeResultMapping(t, err)
	})
}

func TestMapRuntimeResultFailureProducesValidFailureRecord(t *testing.T) {
	input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "recompute failed"))
	input.Linkage = recordmap.RuntimeResultMappingLinkage{
		JobID:      " job-a ",
		ProductKey: " product-a ",
		StepRef:    " step-a ",
	}

	output, err := recordmap.MapRuntimeResult(input)
	if err != nil {
		t.Fatalf("MapRuntimeResult(failure) returned error: %v", err)
	}
	record := output.FailureRecord
	if record == nil {
		t.Fatal("FailureRecord = nil, want record")
	}
	if err := recordcontract.ValidateFailureRecord(*record); err != nil {
		t.Fatalf("ValidateFailureRecord(mapped) returned error: %v", err)
	}
	if record.RecordKey != runtimeResultRecordKey {
		t.Fatalf("RecordKey = %q, want %q", record.RecordKey, runtimeResultRecordKey)
	}
	if record.Failure.Message != "recompute failed" {
		t.Fatalf("Failure.Message = %q, want source message", record.Failure.Message)
	}
	if record.Failure.Severity != recordcontract.FailureSeverityError {
		t.Fatalf("Failure.Severity = %q, want %q", record.Failure.Severity, recordcontract.FailureSeverityError)
	}
	if record.Failure.Timeout || record.Failure.Canceled {
		t.Fatalf("Failure timeout/canceled = %v/%v, want false/false", record.Failure.Timeout, record.Failure.Canceled)
	}
	if record.Failure.OccurredAt != nil {
		t.Fatalf("Failure.OccurredAt = %#v, want nil", record.Failure.OccurredAt)
	}
	if record.Provenance.SourceRevision != (recordcontract.SourceRevisionProvenance{}) {
		t.Fatalf("SourceRevision = %#v, want empty caller-only identity", record.Provenance.SourceRevision)
	}
	if record.Provenance.Linkage != (recordcontract.LinkageProvenance{}) {
		t.Fatalf("Provenance.Linkage = %#v, want empty caller-only provenance linkage", record.Provenance.Linkage)
	}
	if record.Failure.Linkage.JobID != "job-a" ||
		record.Failure.Linkage.ProductKey != "product-a" ||
		record.Failure.Linkage.StepRef != "step-a" {
		t.Fatalf("Failure.Linkage = %#v, want trimmed caller linkage", record.Failure.Linkage)
	}
}

func TestMapRuntimeResultKnownClassifications(t *testing.T) {
	tests := []struct {
		classification string
		wantClass      recordcontract.FailureClass
		wantStage      recordcontract.FailureStage
	}{
		{"preflight_validation_error", recordcontract.FailureClassValidation, recordcontract.FailureStageValidation},
		{"working_copy_materialization_error", recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime},
		{"document_open_error", recordcontract.FailureClassAdapter, recordcontract.FailureStageAdapter},
		{"assembly_mutations_error", recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime},
		{"part_mutations_error", recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime},
		{"recompute_error", recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime},
		{"bom_generation_error", recordcontract.FailureClassExport, recordcontract.FailureStageExport},
		{"geometry_export_error", recordcontract.FailureClassExport, recordcontract.FailureStageExport},
		{"drawing_update_error", recordcontract.FailureClassExport, recordcontract.FailureStageExport},
		{"drawing_export_error", recordcontract.FailureClassExport, recordcontract.FailureStageExport},
		{"internal_error", recordcontract.FailureClassInternal, recordcontract.FailureStageRuntime},
	}

	for _, tt := range tests {
		t.Run(tt.classification, func(t *testing.T) {
			first := mapRuntimeFailure(t, runtimeFailureResult(tt.classification, "runtime failed"))
			second := mapRuntimeFailure(t, runtimeFailureResult(tt.classification, "runtime failed"))

			if first.Failure.Class != tt.wantClass {
				t.Fatalf("Failure.Class = %q, want %q", first.Failure.Class, tt.wantClass)
			}
			if first.Failure.Stage != tt.wantStage {
				t.Fatalf("Failure.Stage = %q, want %q", first.Failure.Stage, tt.wantStage)
			}
			if first.Failure.Code != tt.classification {
				t.Fatalf("Failure.Code = %q, want source classification %q", first.Failure.Code, tt.classification)
			}
			if err := recordcontract.ValidateFailureRecord(first); err != nil {
				t.Fatalf("ValidateFailureRecord(%s) returned error: %v", tt.classification, err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("mapping is not deterministic:\nfirst:  %#v\nsecond: %#v", first, second)
			}
		})
	}
}

func TestMapRuntimeResultUnknownAndEmptyClassifications(t *testing.T) {
	tests := []struct {
		name           string
		classification string
		wantCode       string
	}{
		{name: "unknown", classification: "new_runner_error", wantCode: "new_runner_error"},
		{name: "empty", classification: "", wantCode: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := mapRuntimeFailure(t, runtimeFailureResult(tt.classification, "runtime failed"))
			if record.Failure.Class != recordcontract.FailureClassRuntime {
				t.Fatalf("Failure.Class = %q, want runtime", record.Failure.Class)
			}
			if record.Failure.Stage != recordcontract.FailureStageRuntime {
				t.Fatalf("Failure.Stage = %q, want runtime", record.Failure.Stage)
			}
			if record.Failure.Code != tt.wantCode {
				t.Fatalf("Failure.Code = %q, want %q", record.Failure.Code, tt.wantCode)
			}
			if err := recordcontract.ValidateFailureRecord(record); err != nil {
				t.Fatalf("ValidateFailureRecord(%s) returned error: %v", tt.name, err)
			}
		})
	}
}

func TestMapRuntimeResultRawEvidenceLinkage(t *testing.T) {
	input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
	input.EvidenceDigestSHA256 = runtimeDigestA()

	record := mapRuntimeFailureInput(t, input)
	if !hasRuntimeEvidence(record.Provenance.Evidence, runtimeResultEvidenceKind, runtimeResultEvidenceRef(), runtimeDigestA()) {
		t.Fatalf("Provenance.Evidence = %#v, want runtime result evidence", record.Provenance.Evidence)
	}
	if !hasRuntimeFailureEvidence(record.Failure.Evidence, runtimeResultEvidenceKind, runtimeResultEvidenceRef(), runtimeDigestA()) {
		t.Fatalf("Failure.Evidence = %#v, want runtime result evidence", record.Failure.Evidence)
	}
	if recordpackage.IsNormalizedRecordContractPath(runtimeResultEvidenceRef()) {
		t.Fatalf("runtime result evidence ref %q classified as normalized record", runtimeResultEvidenceRef())
	}
	for _, entry := range recordpackage.RecordEntries() {
		if hasRuntimeEvidenceRef(record.Provenance.Evidence, entry.ContractPath) ||
			hasRuntimeFailureEvidenceRef(record.Failure.Evidence, entry.ContractPath) {
			t.Fatalf("normalized record path used as evidence: provenance=%#v failure=%#v", record.Provenance.Evidence, record.Failure.Evidence)
		}
	}
	if hasRuntimeEvidenceRef(record.Provenance.Evidence, input.Result.Artifacts[0].Path) ||
		hasRuntimeFailureEvidenceRef(record.Failure.Evidence, input.Result.Artifacts[0].Path) {
		t.Fatalf("runtime artifact path used as result evidence: provenance=%#v failure=%#v", record.Provenance.Evidence, record.Failure.Evidence)
	}
	if hasRuntimeEvidenceRef(record.Provenance.Evidence, "records/parametron.failure-record.json") ||
		hasRuntimeFailureEvidenceRef(record.Failure.Evidence, "records/parametron.failure-record.json") {
		t.Fatalf("failure record path used as runtime evidence: provenance=%#v failure=%#v", record.Provenance.Evidence, record.Failure.Evidence)
	}
}

func TestMapRuntimeResultEvidenceDigestValidation(t *testing.T) {
	t.Run("valid digest propagates", func(t *testing.T) {
		input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
		input.EvidenceDigestSHA256 = runtimeDigestA()

		record := mapRuntimeFailureInput(t, input)
		if !hasRuntimeEvidence(record.Provenance.Evidence, runtimeResultEvidenceKind, runtimeResultEvidenceRef(), runtimeDigestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want digest %q", record.Provenance.Evidence, runtimeDigestA())
		}
		if !hasRuntimeFailureEvidence(record.Failure.Evidence, runtimeResultEvidenceKind, runtimeResultEvidenceRef(), runtimeDigestA()) {
			t.Fatalf("Failure.Evidence = %#v, want digest %q", record.Failure.Evidence, runtimeDigestA())
		}
	})

	tests := []struct {
		name   string
		digest string
	}{
		{name: "uppercase", digest: "A" + runtimeDigestA()[1:]},
		{name: "short", digest: runtimeDigestA()[:63]},
		{name: "non hex", digest: "g" + runtimeDigestA()[1:]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
			input.EvidenceDigestSHA256 = tt.digest
			_, err := recordmap.MapRuntimeResultToFailureRecord(input)
			assertInvalidRuntimeResultMapping(t, err)
		})
	}
}

func TestMapRuntimeResultEvidenceDedupeAndDigestConflict(t *testing.T) {
	t.Run("matching evidence is not duplicated", func(t *testing.T) {
		input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
		input.EvidenceDigestSHA256 = runtimeDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: runtimeResultEvidenceKind, Ref: runtimeResultEvidenceRef(), DigestSHA256: runtimeDigestA()},
		}

		record := mapRuntimeFailureInput(t, input)
		if countRuntimeEvidence(record.Provenance.Evidence, runtimeResultEvidenceKind, runtimeResultEvidenceRef()) != 1 {
			t.Fatalf("Provenance.Evidence = %#v, want one runtime result reference", record.Provenance.Evidence)
		}
	})

	t.Run("empty existing digest is enriched without conflict", func(t *testing.T) {
		input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
		input.EvidenceDigestSHA256 = runtimeDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: runtimeResultEvidenceKind, Ref: runtimeResultEvidenceRef()},
		}

		first := mapRuntimeFailureInput(t, input)
		second := mapRuntimeFailureInput(t, input)
		if countRuntimeEvidence(first.Provenance.Evidence, runtimeResultEvidenceKind, runtimeResultEvidenceRef()) != 1 {
			t.Fatalf("Provenance.Evidence = %#v, want one runtime result reference", first.Provenance.Evidence)
		}
		if !hasRuntimeEvidence(first.Provenance.Evidence, runtimeResultEvidenceKind, runtimeResultEvidenceRef(), runtimeDigestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want enriched digest", first.Provenance.Evidence)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("digest enrichment is not deterministic:\nfirst:  %#v\nsecond: %#v", first, second)
		}
	})

	t.Run("conflicting non-empty digest fails", func(t *testing.T) {
		input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
		input.EvidenceDigestSHA256 = runtimeDigestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: runtimeResultEvidenceKind, Ref: runtimeResultEvidenceRef(), DigestSHA256: runtimeDigestB()},
		}

		_, err := recordmap.MapRuntimeResultToFailureRecord(input)
		assertInvalidRuntimeResultMapping(t, err)
	})
}

func TestMapRuntimeResultProvenanceCopySafety(t *testing.T) {
	input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
	input.EvidenceDigestSHA256 = runtimeDigestA()
	input.Provenance = recordcontract.Provenance{
		SourceRevision: recordcontract.SourceRevisionProvenance{RevisionID: " rev-1 "},
		Inputs: []recordcontract.ProvenanceInput{
			{Kind: " dsl ", Identity: " model.pmtn ", DigestSHA256: runtimeDigestB()},
		},
		Evidence: []recordcontract.EvidenceReference{
			{Kind: " report ", Ref: " raw/report.json "},
		},
		Runtime: recordcontract.RuntimeProvenance{ToolID: " freecad ", RuntimeID: " runtime-1 ", Adapter: " freecad-runner "},
	}
	original := input.Provenance

	first := mapRuntimeFailureInput(t, input)
	second := mapRuntimeFailureInput(t, input)

	if !reflect.DeepEqual(input.Provenance, original) {
		t.Fatalf("input provenance mutated:\n got: %#v\nwant: %#v", input.Provenance, original)
	}
	if !hasRuntimeEvidence(first.Provenance.Evidence, "report", "raw/report.json", "") {
		t.Fatalf("caller evidence not preserved: %#v", first.Provenance.Evidence)
	}
	if !hasRuntimeEvidence(first.Provenance.Evidence, runtimeResultEvidenceKind, runtimeResultEvidenceRef(), runtimeDigestA()) {
		t.Fatalf("runtime evidence not added: %#v", first.Provenance.Evidence)
	}
	if first.Provenance.SourceRevision.RevisionID != "rev-1" ||
		first.Provenance.Runtime.ToolID != "freecad" ||
		len(first.Provenance.Inputs) != 1 ||
		first.Provenance.Inputs[0].Kind != "dsl" {
		t.Fatalf("caller provenance not preserved after normalization: %#v", first.Provenance)
	}

	first.Provenance.Evidence[0].DigestSHA256 = runtimeDigestB()
	if input.Provenance.Evidence[0].DigestSHA256 != "" {
		t.Fatalf("mutating returned provenance changed input provenance: %#v", input.Provenance.Evidence)
	}
	if !reflect.DeepEqual(second, mapRuntimeFailureInput(t, input)) {
		t.Fatalf("repeated equivalent calls are not deterministic")
	}
}

func TestMapRuntimeResultLinkageIsCallerSuppliedOnly(t *testing.T) {
	input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
	input.Linkage = recordmap.RuntimeResultMappingLinkage{
		JobID:      " job-a ",
		ProductKey: " product-a ",
		StepRef:    " step-a ",
	}
	input.Result.Artifacts = []recordmap.RuntimeResultArtifact{
		{Type: "source", Filename: "job-from-artifact-product-from-artifact.step", Path: "pdm://products/invented/jobs/invented/source.fcstd"},
	}

	record := mapRuntimeFailureInput(t, input)
	if record.Failure.Linkage.JobID != "job-a" ||
		record.Failure.Linkage.ProductKey != "product-a" ||
		record.Failure.Linkage.StepRef != "step-a" {
		t.Fatalf("Failure.Linkage = %#v, want trimmed caller linkage", record.Failure.Linkage)
	}
	if record.Provenance.SourceRevision != (recordcontract.SourceRevisionProvenance{}) {
		t.Fatalf("SourceRevision = %#v, want no artifact-inferred source identity", record.Provenance.SourceRevision)
	}
	if record.Provenance.Linkage != (recordcontract.LinkageProvenance{}) {
		t.Fatalf("Provenance.Linkage = %#v, want no artifact-inferred PDM linkage", record.Provenance.Linkage)
	}

	empty := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
	empty.Result.Artifacts = input.Result.Artifacts
	emptyRecord := mapRuntimeFailureInput(t, empty)
	if emptyRecord.Failure.Linkage != (recordcontract.FailureLinkage{}) {
		t.Fatalf("empty linkage mapped to %#v, want empty", emptyRecord.Failure.Linkage)
	}
	if emptyRecord.Provenance.SourceRevision != (recordcontract.SourceRevisionProvenance{}) ||
		emptyRecord.Provenance.Linkage != (recordcontract.LinkageProvenance{}) {
		t.Fatalf("artifact paths inferred identity: provenance=%#v", emptyRecord.Provenance)
	}
}

func TestMapRuntimeResultSchemaVersionBehavior(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "one zero accepted", version: "1.0"},
		{name: "empty accepted", version: ""},
		{name: "unsupported rejected", version: "2.0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
			input.Result.SchemaVersion = tt.version
			_, err := recordmap.MapRuntimeResultToFailureRecord(input)
			if tt.wantErr {
				assertInvalidRuntimeResultMapping(t, err)
				return
			}
			if err != nil {
				t.Fatalf("MapRuntimeResultToFailureRecord returned error: %v", err)
			}
		})
	}
}

func TestMapRuntimeResultStatusNormalizationAndInvalidStatus(t *testing.T) {
	t.Run("success whitespace is trimmed", func(t *testing.T) {
		input := validRuntimeResultMappingInput(runtimeSuccessResult())
		input.Result.Status = " success "
		record, err := recordmap.MapRuntimeResultToFailureRecord(input)
		if err != nil {
			t.Fatalf("MapRuntimeResultToFailureRecord(success) returned error: %v", err)
		}
		if record != nil {
			t.Fatalf("record = %#v, want nil", record)
		}
	})

	t.Run("failure whitespace is trimmed", func(t *testing.T) {
		input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
		input.Result.Status = " failure "
		record := mapRuntimeFailureInput(t, input)
		if record.Failure.Message != "runtime failed" {
			t.Fatalf("Failure.Message = %q, want runtime failed", record.Failure.Message)
		}
	})

	for _, status := range []string{"unknown", ""} {
		t.Run("invalid "+status, func(t *testing.T) {
			input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
			input.Result.Status = status
			_, err := recordmap.MapRuntimeResultToFailureRecord(input)
			assertInvalidRuntimeResultMapping(t, err)
		})
	}
}

func TestMapRuntimeResultRequiredFailureFields(t *testing.T) {
	tests := []struct {
		name   string
		result recordmap.RuntimeResult
	}{
		{name: "nil error", result: recordmap.RuntimeResult{SchemaVersion: "1.0", Status: "failure"}},
		{name: "empty message", result: runtimeFailureResult("recompute_error", "")},
		{name: "whitespace message", result: runtimeFailureResult("recompute_error", "   ")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := recordmap.MapRuntimeResultToFailureRecord(validRuntimeResultMappingInput(tt.result))
			assertInvalidRuntimeResultMapping(t, err)
		})
	}
}

func TestMapRuntimeResultRequiredRecordKey(t *testing.T) {
	for _, recordKey := range []string{"", "   "} {
		t.Run("record key "+recordKey, func(t *testing.T) {
			input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
			input.RecordKey = recordKey
			_, err := recordmap.MapRuntimeResultToFailureRecord(input)
			assertInvalidRuntimeResultMapping(t, err)
		})
	}
}

func TestMapRuntimeResultDeterminism(t *testing.T) {
	input := validRuntimeResultMappingInput(runtimeFailureResult("recompute_error", "runtime failed"))
	input.EvidenceDigestSHA256 = runtimeDigestA()
	input.Provenance = recordcontract.Provenance{
		Inputs: []recordcontract.ProvenanceInput{
			{Kind: "table", Identity: "b.csv"},
			{Kind: "dsl", Identity: "a.pmtn"},
		},
		Evidence: []recordcontract.EvidenceReference{
			{Kind: "z", Ref: "raw/z.json"},
			{Kind: "a", Ref: "raw/a.json"},
		},
	}

	first := mapRuntimeFailureInput(t, input)
	second := mapRuntimeFailureInput(t, input)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated equivalent failed mapping is not deterministic:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if first.Failure.OccurredAt != nil {
		t.Fatalf("OccurredAt = %#v, want nil wall-clock timestamp", first.Failure.OccurredAt)
	}
	wantEvidence := []recordcontract.EvidenceReference{
		{Kind: "a", Ref: "raw/a.json"},
		{Kind: runtimeResultEvidenceKind, Ref: runtimeResultEvidenceRef(), DigestSHA256: runtimeDigestA()},
		{Kind: "z", Ref: "raw/z.json"},
	}
	if !reflect.DeepEqual(first.Provenance.Evidence, wantEvidence) {
		t.Fatalf("Provenance.Evidence = %#v, want deterministic order %#v", first.Provenance.Evidence, wantEvidence)
	}
}

func validRuntimeResultMappingInput(result recordmap.RuntimeResult) recordmap.RuntimeResultMappingInput {
	return recordmap.RuntimeResultMappingInput{
		Result:    result,
		RecordKey: runtimeResultRecordKey,
	}
}

func runtimeResultEvidenceRef() string {
	return recordpackage.RawRuntimeResultContractPath()
}

func runtimeSuccessResult() recordmap.RuntimeResult {
	return recordmap.RuntimeResult{
		SchemaVersion: "1.0",
		Status:        "success",
	}
}

func runtimeFailureResult(classification, message string) recordmap.RuntimeResult {
	return recordmap.RuntimeResult{
		SchemaVersion: "1.0",
		Status:        "failure",
		Artifacts: []recordmap.RuntimeResultArtifact{
			{Type: "geometry", Filename: "part.step", Path: "artifacts/geometry/part.step"},
		},
		Error: &recordmap.RuntimeResultError{
			Classification: classification,
			Message:        message,
		},
	}
}

func mapRuntimeFailure(t *testing.T, result recordmap.RuntimeResult) recordcontract.FailureRecord {
	t.Helper()
	return mapRuntimeFailureInput(t, validRuntimeResultMappingInput(result))
}

func mapRuntimeFailureInput(t *testing.T, input recordmap.RuntimeResultMappingInput) recordcontract.FailureRecord {
	t.Helper()
	record, err := recordmap.MapRuntimeResultToFailureRecord(input)
	if err != nil {
		t.Fatalf("MapRuntimeResultToFailureRecord returned error: %v", err)
	}
	if record == nil {
		t.Fatal("MapRuntimeResultToFailureRecord returned nil record, want failure record")
	}
	if err := recordcontract.ValidateFailureRecord(*record); err != nil {
		t.Fatalf("ValidateFailureRecord(mapped) returned error: %v", err)
	}
	return *record
}

func hasRuntimeEvidence(evidence []recordcontract.EvidenceReference, kind, ref, digest string) bool {
	for _, item := range evidence {
		if item.Kind == kind && item.Ref == ref && item.DigestSHA256 == digest {
			return true
		}
	}
	return false
}

func hasRuntimeEvidenceRef(evidence []recordcontract.EvidenceReference, ref string) bool {
	for _, item := range evidence {
		if item.Ref == ref {
			return true
		}
	}
	return false
}

func hasRuntimeFailureEvidence(evidence []recordcontract.FailureEvidence, kind, ref, digest string) bool {
	for _, item := range evidence {
		if item.SourceKind == kind && item.SourceRef == ref && item.DigestSHA256 == digest {
			return true
		}
	}
	return false
}

func hasRuntimeFailureEvidenceRef(evidence []recordcontract.FailureEvidence, ref string) bool {
	for _, item := range evidence {
		if item.SourceRef == ref {
			return true
		}
	}
	return false
}

func countRuntimeEvidence(evidence []recordcontract.EvidenceReference, kind, ref string) int {
	count := 0
	for _, item := range evidence {
		if item.Kind == kind && item.Ref == ref {
			count++
		}
	}
	return count
}

func assertInvalidRuntimeResultMapping(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want invalid runtime result mapping error")
	}
	if !errors.Is(err, recordmap.ErrInvalidRuntimeResultMapping) {
		t.Fatalf("error = %v, want errors.Is(..., ErrInvalidRuntimeResultMapping)", err)
	}
}

func runtimeDigestA() string {
	return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}

func runtimeDigestB() string {
	return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
}
