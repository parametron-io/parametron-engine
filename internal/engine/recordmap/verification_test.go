package recordmap_test

import (
	"errors"
	"reflect"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/verification"
)

const verificationEvidenceRef = "raw/verification/parametron.verification.json"

func TestMapVerificationPassResultMapsValidVerificationRecord(t *testing.T) {
	input := validVerificationMappingInput(passVerificationResult())
	input.Linkage = recordmap.VerificationMappingLinkage{
		JobID:      " job-a ",
		ProductKey: " product-a ",
		StepRef:    " verify ",
	}
	input.Provenance = recordcontract.Provenance{
		SourceRevision: recordcontract.SourceRevisionProvenance{RevisionID: " rev-1 "},
		Inputs: []recordcontract.ProvenanceInput{
			{Kind: " dsl ", Identity: " model.pmtn ", DigestSHA256: digestA()},
		},
		Evidence: []recordcontract.EvidenceReference{
			{Kind: " report ", Ref: " raw/report.json "},
		},
	}

	got, err := recordmap.MapVerification(input)
	if err != nil {
		t.Fatalf("MapVerification(pass) returned error: %v", err)
	}
	record := got.VerificationRecord
	if err := recordcontract.ValidateVerificationRecord(record); err != nil {
		t.Fatalf("ValidateVerificationRecord(mapped) returned error: %v", err)
	}
	if record.Family != recordcontract.FamilyVerification {
		t.Fatalf("Family = %q, want %q", record.Family, recordcontract.FamilyVerification)
	}
	if record.Version != recordcontract.CurrentVersion {
		t.Fatalf("Version = %q, want %q", record.Version, recordcontract.CurrentVersion)
	}
	if record.Verification.Outcome != recordcontract.VerificationOutcomePass {
		t.Fatalf("Verification.Outcome = %q, want pass", record.Verification.Outcome)
	}
	if record.Verification.Message != input.Result.Message {
		t.Fatalf("Verification.Message = %q, want %q", record.Verification.Message, input.Result.Message)
	}
	if record.Verification.Linkage.JobID != "job-a" ||
		record.Verification.Linkage.ProductKey != "product-a" ||
		record.Verification.Linkage.StepRef != "verify" {
		t.Fatalf("Verification.Linkage = %#v, want caller linkage", record.Verification.Linkage)
	}
	if !hasVerificationEvidence(record.Verification.Evidence, "verification", verificationEvidenceRef, "") {
		t.Fatalf("Verification.Evidence = %#v, want raw verification evidence", record.Verification.Evidence)
	}
	if !hasEvidence(record.Provenance.Evidence, "verification", verificationEvidenceRef, "") {
		t.Fatalf("Provenance.Evidence = %#v, want raw verification evidence", record.Provenance.Evidence)
	}
	if hasInputKind(record.Provenance.Inputs, "verification") || hasInputKind(record.Provenance.Inputs, "record") {
		t.Fatalf("raw verification evidence embedded as durable input/record payload: %#v", record.Provenance.Inputs)
	}
}

func TestMapVerificationFailureClasses(t *testing.T) {
	tests := []struct {
		name string
		src  verification.FailureClass
		want recordcontract.VerificationFailureClass
	}{
		{name: "contract invalid", src: verification.FailureClassContractInvalid, want: recordcontract.VerificationFailureClassContractInvalid},
		{name: "observed invalid", src: verification.FailureClassObservedInvalid, want: recordcontract.VerificationFailureClassObservedInvalid},
		{name: "component mismatch", src: verification.FailureClassComponentMismatch, want: recordcontract.VerificationFailureClassComponentMismatch},
		{name: "parameter mismatch", src: verification.FailureClassParameterMismatch, want: recordcontract.VerificationFailureClassParameterMismatch},
		{name: "metadata mismatch", src: verification.FailureClassMetadataMismatch, want: recordcontract.VerificationFailureClassMetadataMismatch},
		{name: "reference mismatch", src: verification.FailureClassReferenceMismatch, want: recordcontract.VerificationFailureClassReferenceMismatch},
		{name: "required observation missing", src: verification.FailureClassRequiredObservationMissing, want: recordcontract.VerificationFailureClassRequiredObservationMissing},
		{name: "internal error", src: verification.FailureClassInternalError, want: recordcontract.VerificationFailureClassInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := failVerificationResult(tt.src, "components failed")
			got, err := recordmap.MapVerificationToVerificationRecord(validVerificationMappingInput(result))
			if err != nil {
				t.Fatalf("MapVerificationToVerificationRecord(%s) returned error: %v", tt.name, err)
			}
			if err := recordcontract.ValidateVerificationRecord(got); err != nil {
				t.Fatalf("ValidateVerificationRecord(%s) returned error: %v", tt.name, err)
			}
			if got.Verification.Outcome != recordcontract.VerificationOutcomeFail {
				t.Fatalf("Verification.Outcome = %q, want fail", got.Verification.Outcome)
			}
			if got.Verification.FailureClass != tt.want {
				t.Fatalf("Verification.FailureClass = %q, want %q", got.Verification.FailureClass, tt.want)
			}
			category := requireVerificationCategory(t, got.Verification.Categories, recordcontract.VerificationCategoryComponents)
			if category.FailureClass != tt.want {
				t.Fatalf("components FailureClass = %q, want %q", category.FailureClass, tt.want)
			}
			if category.Message != "components failed" {
				t.Fatalf("components Message = %q, want source message", category.Message)
			}
			if !hasVerificationEvidence(category.Evidence, "verification", verificationEvidenceRef, "") {
				t.Fatalf("components Evidence = %#v, want raw verification evidence", category.Evidence)
			}
		})
	}
}

func TestMapVerificationCategoryOrderAndStatusAreDeterministic(t *testing.T) {
	result := verification.Result{
		Status:  verification.StatusFail,
		Failure: verification.FailureClassParameterMismatch,
		Message: "parameters failed",
		Categories: verification.CategoryResults{
			References: verificationCategory(true, verification.CategoryStatusSkipped, "references skipped"),
			Metadata:   verificationCategory(true, verification.CategoryStatusPass, "metadata passed"),
			Parameters: verificationCategory(true, verification.CategoryStatusFail, "parameters failed"),
			Components: verificationCategory(true, verification.CategoryStatusPass, "components passed"),
		},
	}

	got, err := recordmap.MapVerificationToVerificationRecord(validVerificationMappingInput(result))
	if err != nil {
		t.Fatalf("MapVerificationToVerificationRecord returned error: %v", err)
	}
	// Mapper output is normalized by the verification record contract, so this
	// assertion follows contract-normalized category order.
	wantCategories := []recordcontract.VerificationCategory{
		recordcontract.VerificationCategoryComponents,
		recordcontract.VerificationCategoryMetadata,
		recordcontract.VerificationCategoryParameters,
		recordcontract.VerificationCategoryReferences,
	}
	wantOutcomes := []recordcontract.VerificationOutcome{
		recordcontract.VerificationOutcomePass,
		recordcontract.VerificationOutcomePass,
		recordcontract.VerificationOutcomeFail,
		recordcontract.VerificationOutcomeSkipped,
	}
	if len(got.Verification.Categories) != len(wantCategories) {
		t.Fatalf("category count = %d, want %d", len(got.Verification.Categories), len(wantCategories))
	}
	for i := range wantCategories {
		category := got.Verification.Categories[i]
		if category.Category != wantCategories[i] {
			t.Fatalf("Categories[%d].Category = %q, want %q", i, category.Category, wantCategories[i])
		}
		if category.Outcome != wantOutcomes[i] {
			t.Fatalf("Categories[%d].Outcome = %q, want %q", i, category.Outcome, wantOutcomes[i])
		}
	}

	second, err := recordmap.MapVerificationToVerificationRecord(validVerificationMappingInput(result))
	if err != nil {
		t.Fatalf("MapVerificationToVerificationRecord repeated call returned error: %v", err)
	}
	if !reflect.DeepEqual(got, second) {
		t.Fatalf("verification mapping is not deterministic:\nfirst:  %#v\nsecond: %#v", got, second)
	}
}

func TestMapVerificationEvidenceDigestBehavior(t *testing.T) {
	t.Run("valid digest propagates to verification and provenance evidence", func(t *testing.T) {
		input := validVerificationMappingInput(passVerificationResult())
		input.EvidenceDigestSHA256 = digestA()

		got, err := recordmap.MapVerificationToVerificationRecord(input)
		if err != nil {
			t.Fatalf("MapVerificationToVerificationRecord returned error: %v", err)
		}
		if !hasVerificationEvidence(got.Verification.Evidence, "verification", verificationEvidenceRef, digestA()) {
			t.Fatalf("Verification.Evidence = %#v, want digest %q", got.Verification.Evidence, digestA())
		}
		if !hasEvidence(got.Provenance.Evidence, "verification", verificationEvidenceRef, digestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want digest %q", got.Provenance.Evidence, digestA())
		}
		for _, category := range got.Verification.Categories {
			if !hasVerificationEvidence(category.Evidence, "verification", verificationEvidenceRef, digestA()) {
				t.Fatalf("%s Evidence = %#v, want digest %q", category.Category, category.Evidence, digestA())
			}
		}
	})

	tests := []struct {
		name   string
		digest string
	}{
		{name: "uppercase", digest: "A" + digestA()[1:]},
		{name: "non hex", digest: "g" + digestA()[1:]},
		{name: "too short", digest: digestA()[:63]},
		{name: "too long", digest: digestA() + "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validVerificationMappingInput(passVerificationResult())
			input.EvidenceDigestSHA256 = tt.digest

			_, err := recordmap.MapVerificationToVerificationRecord(input)
			assertInvalidVerificationMapping(t, err)
		})
	}
}

func TestMapVerificationProvenanceEvidenceDedupeAndDigestConflict(t *testing.T) {
	t.Run("matching evidence is not duplicated", func(t *testing.T) {
		input := validVerificationMappingInput(passVerificationResult())
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "verification", Ref: verificationEvidenceRef},
		}

		got, err := recordmap.MapVerificationToVerificationRecord(input)
		if err != nil {
			t.Fatalf("MapVerificationToVerificationRecord returned error: %v", err)
		}
		if countEvidence(got.Provenance.Evidence, "verification", verificationEvidenceRef) != 1 {
			t.Fatalf("verification evidence count = %d, want 1 in %#v", countEvidence(got.Provenance.Evidence, "verification", verificationEvidenceRef), got.Provenance.Evidence)
		}
	})

	t.Run("conflicting digest is rejected", func(t *testing.T) {
		input := validVerificationMappingInput(passVerificationResult())
		input.EvidenceDigestSHA256 = digestB()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "verification", Ref: verificationEvidenceRef, DigestSHA256: digestA()},
		}

		_, err := recordmap.MapVerificationToVerificationRecord(input)
		assertInvalidVerificationMapping(t, err)
	})

	t.Run("matching digest is preserved without duplication", func(t *testing.T) {
		input := validVerificationMappingInput(passVerificationResult())
		input.EvidenceDigestSHA256 = digestA()
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "verification", Ref: verificationEvidenceRef, DigestSHA256: digestA()},
		}

		got, err := recordmap.MapVerificationToVerificationRecord(input)
		if err != nil {
			t.Fatalf("MapVerificationToVerificationRecord returned error: %v", err)
		}
		if countEvidence(got.Provenance.Evidence, "verification", verificationEvidenceRef) != 1 {
			t.Fatalf("verification evidence count = %d, want 1 in %#v", countEvidence(got.Provenance.Evidence, "verification", verificationEvidenceRef), got.Provenance.Evidence)
		}
		if !hasEvidence(got.Provenance.Evidence, "verification", verificationEvidenceRef, digestA()) {
			t.Fatalf("Provenance.Evidence = %#v, want matching digest", got.Provenance.Evidence)
		}
	})
}

func TestMapVerificationProvenanceCopySafety(t *testing.T) {
	input := validVerificationMappingInput(passVerificationResult())
	input.Provenance = recordcontract.Provenance{
		Inputs: []recordcontract.ProvenanceInput{
			{Kind: "dsl", Identity: "model.pmtn", DigestSHA256: digestA()},
		},
		Evidence: []recordcontract.EvidenceReference{
			{Kind: "report", Ref: "raw/report.json"},
		},
	}
	original := cloneProvenance(input.Provenance)

	first, err := recordmap.MapVerificationToVerificationRecord(input)
	if err != nil {
		t.Fatalf("MapVerificationToVerificationRecord(first) returned error: %v", err)
	}
	if !reflect.DeepEqual(input.Provenance, original) {
		t.Fatalf("mapper mutated caller provenance:\nwant: %#v\ngot:  %#v", original, input.Provenance)
	}

	first.Provenance.Inputs[0].Identity = "mutated-input"
	first.Provenance.Evidence[0].Ref = "mutated-evidence"
	first.Verification.Evidence[0].SourceRef = "mutated-verification-evidence"
	first.Verification.Categories[0].Evidence[0].SourceRef = "mutated-category-evidence"
	if !reflect.DeepEqual(input.Provenance, original) {
		t.Fatalf("mutating returned record changed caller provenance:\nwant: %#v\ngot:  %#v", original, input.Provenance)
	}

	input.Provenance = cloneProvenance(original)
	second, err := recordmap.MapVerificationToVerificationRecord(input)
	if err != nil {
		t.Fatalf("MapVerificationToVerificationRecord(second) returned error: %v", err)
	}
	wantInputIdentity := original.Inputs[0].Identity
	input.Provenance.Inputs[0].Identity = "mutated-caller-input-after-second-map"
	input.Provenance.Evidence[0].Ref = "mutated-caller-evidence-after-second-map"
	if second.Provenance.Inputs[0].Identity != wantInputIdentity {
		t.Fatalf("returned provenance input shares backing storage with caller: %#v", second.Provenance.Inputs)
	}
	if !hasEvidence(second.Provenance.Evidence, "report", "raw/report.json", "") {
		t.Fatalf("returned provenance evidence changed after caller mutation: %#v", second.Provenance.Evidence)
	}

	input.Provenance = cloneProvenance(original)
	third, err := recordmap.MapVerificationToVerificationRecord(input)
	if err != nil {
		t.Fatalf("MapVerificationToVerificationRecord(third) returned error: %v", err)
	}
	if !reflect.DeepEqual(second, third) {
		t.Fatalf("repeated mapping with same input differs:\nsecond: %#v\nthird:  %#v", second, third)
	}
	if !reflect.DeepEqual(input.Provenance, original) {
		t.Fatalf("repeated mapping mutated caller provenance:\nwant: %#v\ngot:  %#v", original, input.Provenance)
	}
}

func TestMapVerificationInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*recordmap.VerificationMappingInput)
	}{
		{name: "missing record key", mutate: func(input *recordmap.VerificationMappingInput) {
			input.RecordKey = ""
		}},
		{name: "blank record key", mutate: func(input *recordmap.VerificationMappingInput) {
			input.RecordKey = " \t "
		}},
		{name: "invalid caller provenance", mutate: func(input *recordmap.VerificationMappingInput) {
			input.Provenance.Inputs = []recordcontract.ProvenanceInput{{Kind: "dsl"}}
		}},
		{name: "unsupported verification status", mutate: func(input *recordmap.VerificationMappingInput) {
			input.Result.Status = verification.Status("unknown")
		}},
		{name: "unsupported category status", mutate: func(input *recordmap.VerificationMappingInput) {
			input.Result.Categories.Metadata.Status = verification.CategoryStatus("unknown")
		}},
		{name: "unsupported failure class", mutate: func(input *recordmap.VerificationMappingInput) {
			input.Result = failVerificationResult(verification.FailureClass("new_failure"), "components failed")
		}},
		{name: "failed category missing failure class falls back to internal error", mutate: func(input *recordmap.VerificationMappingInput) {
			input.Result = failVerificationResult(verification.FailureClassNone, "components failed")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validVerificationMappingInput(passVerificationResult())
			tt.mutate(&input)

			got, err := recordmap.MapVerificationToVerificationRecord(input)
			if tt.name == "failed category missing failure class falls back to internal error" {
				if err != nil {
					t.Fatalf("MapVerificationToVerificationRecord returned error: %v", err)
				}
				if got.Verification.FailureClass != recordcontract.VerificationFailureClassInternalError {
					t.Fatalf("FailureClass = %q, want internal error", got.Verification.FailureClass)
				}
				category := requireVerificationCategory(t, got.Verification.Categories, recordcontract.VerificationCategoryComponents)
				if category.FailureClass != recordcontract.VerificationFailureClassInternalError {
					t.Fatalf("components FailureClass = %q, want internal error", category.FailureClass)
				}
				return
			}
			assertInvalidVerificationMapping(t, err)
		})
	}
}

func TestMapVerificationHelperParity(t *testing.T) {
	input := validVerificationMappingInput(failVerificationResult(verification.FailureClassMetadataMismatch, "metadata failed"))
	input.EvidenceDigestSHA256 = digestA()

	output, err := recordmap.MapVerification(input)
	if err != nil {
		t.Fatalf("MapVerification returned error: %v", err)
	}
	record, err := recordmap.MapVerificationToVerificationRecord(input)
	if err != nil {
		t.Fatalf("MapVerificationToVerificationRecord returned error: %v", err)
	}
	if !reflect.DeepEqual(output.VerificationRecord, record) {
		t.Fatalf("helper outputs differ:\nMapVerification: %#v\nMapVerificationToVerificationRecord: %#v", output.VerificationRecord, record)
	}
}

func validVerificationMappingInput(result verification.Result) recordmap.VerificationMappingInput {
	return recordmap.VerificationMappingInput{
		Result:    result,
		RecordKey: " verification:run-1 ",
		Provenance: recordcontract.Provenance{
			Plan:    recordcontract.PlanProvenance{PlanHash: digestB()},
			Runtime: recordcontract.RuntimeProvenance{ToolID: "parametron"},
		},
	}
}

func passVerificationResult() verification.Result {
	return verification.Result{
		Status:  verification.StatusPass,
		Message: "verification passed",
		Categories: verification.CategoryResults{
			Components: verificationCategory(true, verification.CategoryStatusPass, "components passed"),
			Parameters: verificationCategory(true, verification.CategoryStatusPass, "parameters passed"),
			Metadata:   verificationCategory(true, verification.CategoryStatusPass, "metadata passed"),
			References: verificationCategory(false, verification.CategoryStatusSkipped, "references skipped"),
		},
	}
}

func failVerificationResult(class verification.FailureClass, message string) verification.Result {
	return verification.Result{
		Status:  verification.StatusFail,
		Failure: class,
		Message: message,
		Categories: verification.CategoryResults{
			Components: verificationCategory(true, verification.CategoryStatusFail, message),
			Parameters: verificationCategory(true, verification.CategoryStatusPass, "parameters passed"),
			Metadata:   verificationCategory(true, verification.CategoryStatusPass, "metadata passed"),
			References: verificationCategory(false, verification.CategoryStatusSkipped, "references skipped"),
		},
	}
}

func verificationCategory(enabled bool, status verification.CategoryStatus, message string) verification.CategoryResult {
	return verification.CategoryResult{
		Enabled: enabled,
		Status:  status,
		Message: message,
	}
}

func requireVerificationCategory(t *testing.T, categories []recordcontract.VerificationCategoryResult, category recordcontract.VerificationCategory) recordcontract.VerificationCategoryResult {
	t.Helper()
	for _, item := range categories {
		if item.Category == category {
			return item
		}
	}
	t.Fatalf("category %q not found in %#v", category, categories)
	return recordcontract.VerificationCategoryResult{}
}

func hasVerificationEvidence(evidence []recordcontract.VerificationEvidence, kind, ref, digest string) bool {
	for _, item := range evidence {
		if item.SourceKind == kind && item.SourceRef == ref && item.DigestSHA256 == digest {
			return true
		}
	}
	return false
}

func assertInvalidVerificationMapping(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want invalid verification mapping error")
	}
	if !errors.Is(err, recordmap.ErrInvalidVerificationMapping) {
		t.Fatalf("error = %v, want errors.Is(..., ErrInvalidVerificationMapping)", err)
	}
}
