package recordcontract

import (
	"errors"
	"reflect"
	"testing"
)

func TestBuildVerificationRecordProducesValidDeterministicRecord(t *testing.T) {
	record, err := BuildVerificationRecord(validVerificationRecordInput())
	if err != nil {
		t.Fatalf("BuildVerificationRecord(valid) returned error: %v", err)
	}

	if record.Family != FamilyVerification {
		t.Fatalf("Family = %q, want %q", record.Family, FamilyVerification)
	}
	if record.Version != CurrentVersion {
		t.Fatalf("Version = %q, want %q", record.Version, CurrentVersion)
	}
	if record.RecordKey != "verification-record-1" {
		t.Fatalf("RecordKey = %q, want trimmed verification-record-1", record.RecordKey)
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
	if record.Identity.Family != FamilyVerification {
		t.Fatalf("Identity.Family = %q, want %q", record.Identity.Family, FamilyVerification)
	}
	if record.Identity.Version != CurrentVersion {
		t.Fatalf("Identity.Version = %q, want %q", record.Identity.Version, CurrentVersion)
	}
	if record.Verification.Outcome != VerificationOutcomeFail || record.Verification.Message != "verification failed" {
		t.Fatalf("Verification summary not normalized: %#v", record.Verification)
	}
	if record.Verification.Categories[0].Category != VerificationCategoryComponents {
		t.Fatalf("first category = %q, want sorted components", record.Verification.Categories[0].Category)
	}
	if record.Verification.Evidence[0].SourceKind != "artifact" {
		t.Fatalf("first evidence = %#v, want sorted artifact evidence first", record.Verification.Evidence[0])
	}
	if err := ValidateVerificationRecord(record); err != nil {
		t.Fatalf("ValidateVerificationRecord(built) returned error: %v", err)
	}
}

func TestVerificationRecordIdentityIsStableForEquivalentInput(t *testing.T) {
	first, err := BuildVerificationRecord(validVerificationRecordInput())
	if err != nil {
		t.Fatalf("BuildVerificationRecord(first) returned error: %v", err)
	}
	second, err := BuildVerificationRecord(equivalentVerificationRecordInput())
	if err != nil {
		t.Fatalf("BuildVerificationRecord(second) returned error: %v", err)
	}

	if first.Identity != second.Identity {
		t.Fatalf("equivalent identities differ:\nfirst:  %#v\nsecond: %#v", first.Identity, second.Identity)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent records differ:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestVerificationRecordIdentityChangesWhenVerificationMaterialChanges(t *testing.T) {
	base := validVerificationRecordInput()
	baseRecord := mustBuildVerificationRecord(t, base)

	tests := []struct {
		name   string
		mutate func(*VerificationRecordInput)
	}{
		{name: "outcome", mutate: func(input *VerificationRecordInput) {
			input.Verification.Outcome = VerificationOutcomeSkipped
			input.Verification.Message = ""
			input.Verification.FailureClass = ""
		}},
		{name: "message", mutate: func(input *VerificationRecordInput) { input.Verification.Message = "metadata mismatch" }},
		{name: "failure class", mutate: func(input *VerificationRecordInput) {
			input.Verification.FailureClass = VerificationFailureClassMetadataMismatch
		}},
		{name: "linkage", mutate: func(input *VerificationRecordInput) { input.Verification.Linkage.StepRef = "export" }},
		{name: "category outcome", mutate: func(input *VerificationRecordInput) {
			input.Verification.Categories[0].Outcome = VerificationOutcomeSkipped
			input.Verification.Categories[0].Message = ""
			input.Verification.Categories[0].FailureClass = ""
		}},
		{name: "evidence digest", mutate: func(input *VerificationRecordInput) {
			input.Verification.Evidence[0].DigestSHA256 = digestA()
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneVerificationRecordInput(base)
			tc.mutate(&changed)

			got, err := BuildVerificationRecord(changed)
			if err != nil {
				t.Fatalf("BuildVerificationRecord(changed) returned error: %v", err)
			}
			if got.Identity.ID == baseRecord.Identity.ID {
				t.Fatalf("identity ID did not change after %s changed: %q", tc.name, got.Identity.ID)
			}
		})
	}
}

func TestNormalizeVerificationRecordCanonicalizesOrderingAndStrings(t *testing.T) {
	record := mustBuildVerificationRecord(t, validVerificationRecordInput())

	normalized := NormalizeVerificationRecord(VerificationRecord{
		Family:     Family(" \t" + string(FamilyVerification) + "\n"),
		Version:    " " + CurrentVersion + "\t",
		RecordKey:  "\nverification-record-1 ",
		Identity:   record.Identity,
		Provenance: equivalentVerificationRecordInput().Provenance,
		Verification: VerificationSummary{
			Outcome:      " fail ",
			Message:      " verification failed ",
			FailureClass: " component_mismatch ",
			Linkage: VerificationLinkage{
				JobID:      " job-a ",
				ProductKey: " product-a ",
				StepRef:    " verify ",
			},
			Categories: []VerificationCategoryResult{
				{
					Category:     " parameters ",
					Enabled:      true,
					Outcome:      " pass ",
					Evidence:     []VerificationEvidence{{SourceKind: " table ", SourceRef: " params.csv ", DigestSHA256: " " + digestE() + " "}},
					FailureClass: " ",
				},
				{
					Category:     " components ",
					Enabled:      true,
					Outcome:      " fail ",
					Message:      " component mismatch ",
					FailureClass: " component_mismatch ",
					Evidence: []VerificationEvidence{
						{SourceKind: " runtime ", SourceRef: " runtime://verification/components ", DigestSHA256: " " + digestF() + " "},
						{SourceKind: " artifact ", SourceRef: " out/model.step ", DigestSHA256: "\t" + digestD() + "\n"},
					},
				},
			},
			Evidence: []VerificationEvidence{
				{SourceKind: " log ", SourceRef: " logs/verify.log ", DigestSHA256: " " + digestC() + " "},
				{SourceKind: " artifact ", SourceRef: " out/model.step ", DigestSHA256: "\t" + digestB() + "\n"},
			},
		},
	})

	if normalized.Family != FamilyVerification || normalized.Version != CurrentVersion || normalized.RecordKey != "verification-record-1" {
		t.Fatalf("root fields not normalized: %#v", normalized)
	}
	if !reflect.DeepEqual(normalized.Provenance, record.Provenance) {
		t.Fatalf("NormalizeVerificationRecord provenance = %#v, want %#v", normalized.Provenance, record.Provenance)
	}
	if got := normalized.Verification.Linkage; got != record.Verification.Linkage {
		t.Fatalf("Verification.Linkage = %#v, want %#v", got, record.Verification.Linkage)
	}
	if got := normalized.Verification.Categories[0].Category; got != VerificationCategoryComponents {
		t.Fatalf("Categories[0].Category = %q, want components", got)
	}
	if got := normalized.Verification.Categories[0].Evidence[0].SourceKind; got != "artifact" {
		t.Fatalf("category evidence first source kind = %q, want artifact", got)
	}
	if got := normalized.Verification.Evidence[0].SourceKind; got != "artifact" {
		t.Fatalf("top-level evidence first source kind = %q, want artifact", got)
	}

	empty := NormalizeVerificationRecord(VerificationRecord{})
	if empty.Verification.Categories == nil {
		t.Fatal("NormalizeVerificationRecord empty Categories = nil, want empty non-nil slice")
	}
	if empty.Verification.Evidence == nil {
		t.Fatal("NormalizeVerificationRecord empty Evidence = nil, want empty non-nil slice")
	}
}

func TestVerificationRecordNormalizationIsCopySafe(t *testing.T) {
	input := validVerificationRecordInput()
	built := mustBuildVerificationRecord(t, input)

	input.Provenance.Inputs[0].Identity = "mutated-input"
	input.Provenance.Evidence[0].Ref = "mutated-provenance-evidence"
	input.Verification.Categories[0].Evidence[0].SourceRef = "mutated-category-evidence"
	input.Verification.Evidence[0].SourceRef = "mutated-verification-evidence"

	if built.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("built provenance inputs changed after input mutation: %#v", built.Provenance.Inputs)
	}
	if built.Provenance.Evidence[0].Ref != "out/model.step" {
		t.Fatalf("built provenance evidence changed after input mutation: %#v", built.Provenance.Evidence)
	}
	if built.Verification.Categories[0].Evidence[0].SourceRef != "out/model.step" {
		t.Fatalf("built category evidence changed after input mutation: %#v", built.Verification.Categories[0].Evidence)
	}
	if built.Verification.Evidence[0].SourceRef != "out/model.step" {
		t.Fatalf("built verification evidence changed after input mutation: %#v", built.Verification.Evidence)
	}

	normalizedInput := VerificationRecord{
		Provenance: Provenance{
			Inputs:   []ProvenanceInput{{Kind: "dsl", Identity: "model-a"}},
			Evidence: []EvidenceReference{{Kind: "artifact", Ref: "out/model.step"}},
		},
		Verification: VerificationSummary{
			Categories: []VerificationCategoryResult{
				{Category: VerificationCategoryComponents, Evidence: []VerificationEvidence{{SourceKind: "artifact", SourceRef: "out/model.step"}}},
			},
			Evidence: []VerificationEvidence{{SourceKind: "log", SourceRef: "logs/verify.log"}},
		},
	}
	normalized := NormalizeVerificationRecord(normalizedInput)
	normalizedInput.Provenance.Inputs[0].Identity = "mutated-input"
	normalizedInput.Provenance.Evidence[0].Ref = "mutated-provenance-evidence"
	normalizedInput.Verification.Categories[0].Evidence[0].SourceRef = "mutated-category-evidence"
	normalizedInput.Verification.Evidence[0].SourceRef = "mutated-verification-evidence"

	if normalized.Provenance.Inputs[0].Identity != "model-a" {
		t.Fatalf("normalized provenance inputs changed after input mutation: %#v", normalized.Provenance.Inputs)
	}
	if normalized.Provenance.Evidence[0].Ref != "out/model.step" {
		t.Fatalf("normalized provenance evidence changed after input mutation: %#v", normalized.Provenance.Evidence)
	}
	if normalized.Verification.Categories[0].Evidence[0].SourceRef != "out/model.step" {
		t.Fatalf("normalized category evidence changed after input mutation: %#v", normalized.Verification.Categories[0].Evidence)
	}
	if normalized.Verification.Evidence[0].SourceRef != "logs/verify.log" {
		t.Fatalf("normalized verification evidence changed after input mutation: %#v", normalized.Verification.Evidence)
	}

	normalized.Verification.Categories[0].Evidence[0].SourceRef = "mutated"
	normalized.Verification.Evidence[0].SourceRef = "mutated"
	rebuilt := NormalizeVerificationRecord(normalizedInput)
	if rebuilt.Verification.Categories[0].Evidence[0].SourceRef != "mutated-category-evidence" {
		t.Fatalf("normalization shared category evidence storage across calls: %#v", rebuilt.Verification.Categories[0].Evidence)
	}
	if rebuilt.Verification.Evidence[0].SourceRef != "mutated-verification-evidence" {
		t.Fatalf("normalization shared evidence storage across calls: %#v", rebuilt.Verification.Evidence)
	}
}

func TestValidateVerificationRecordAcceptsValidOutcomeCombinations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*VerificationRecordInput)
	}{
		{name: "top-level pass without failure class", mutate: func(input *VerificationRecordInput) {
			input.Verification.Outcome = VerificationOutcomePass
			input.Verification.Message = ""
			input.Verification.FailureClass = ""
			input.Verification.Categories[0].Outcome = VerificationOutcomePass
			input.Verification.Categories[0].Message = ""
			input.Verification.Categories[0].FailureClass = ""
		}},
		{name: "top-level fail with message and failure class", mutate: func(input *VerificationRecordInput) {}},
		{name: "top-level skipped with disabled skipped category", mutate: func(input *VerificationRecordInput) {
			input.Verification.Outcome = VerificationOutcomeSkipped
			input.Verification.Message = ""
			input.Verification.FailureClass = ""
			input.Verification.Categories[1].Enabled = false
			input.Verification.Categories[1].Outcome = VerificationOutcomeSkipped
			input.Verification.Categories[1].Message = ""
			input.Verification.Categories[1].FailureClass = ""
		}},
		{name: "enabled failed category has message and failure class", mutate: func(input *VerificationRecordInput) {}},
		{name: "disabled category uses skipped outcome", mutate: func(input *VerificationRecordInput) {
			input.Verification.Categories[1].Enabled = false
			input.Verification.Categories[1].Outcome = VerificationOutcomeSkipped
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validVerificationRecordInput()
			tc.mutate(&input)
			record := mustBuildVerificationRecord(t, input)
			if err := ValidateVerificationRecord(record); err != nil {
				t.Fatalf("ValidateVerificationRecord(valid %s) returned error: %v", tc.name, err)
			}
		})
	}
}

func TestValidateVerificationRecordRejectsInvalidPayloads(t *testing.T) {
	valid := mustBuildVerificationRecord(t, validVerificationRecordInput())

	tests := []struct {
		name   string
		mutate func(*VerificationRecord)
	}{
		{name: "wrong family", mutate: func(record *VerificationRecord) { record.Family = FamilyExecution }},
		{name: "unsupported version", mutate: func(record *VerificationRecord) { record.Version = "1.1" }},
		{name: "empty record key", mutate: func(record *VerificationRecord) { record.RecordKey = " " }},
		{name: "invalid provenance", mutate: func(record *VerificationRecord) { record.Provenance.Inputs[0].DigestSHA256 = upperDigestA() }},
		{name: "empty top-level outcome", mutate: func(record *VerificationRecord) { record.Verification.Outcome = " " }},
		{name: "unknown top-level outcome", mutate: func(record *VerificationRecord) { record.Verification.Outcome = "unknown" }},
		{name: "pass with failure class", mutate: func(record *VerificationRecord) {
			record.Verification.Outcome = VerificationOutcomePass
			record.Verification.FailureClass = VerificationFailureClassComponentMismatch
		}},
		{name: "fail without message", mutate: func(record *VerificationRecord) { record.Verification.Message = " " }},
		{name: "fail without failure class", mutate: func(record *VerificationRecord) { record.Verification.FailureClass = " " }},
		{name: "unknown failure class", mutate: func(record *VerificationRecord) { record.Verification.FailureClass = "other" }},
		{name: "empty category name", mutate: func(record *VerificationRecord) { record.Verification.Categories[0].Category = " " }},
		{name: "unknown category", mutate: func(record *VerificationRecord) { record.Verification.Categories[0].Category = "geometry" }},
		{name: "duplicate category after normalization", mutate: func(record *VerificationRecord) {
			record.Verification.Categories[1].Category = " components "
		}},
		{name: "empty category outcome", mutate: func(record *VerificationRecord) { record.Verification.Categories[0].Outcome = " " }},
		{name: "unknown category outcome", mutate: func(record *VerificationRecord) { record.Verification.Categories[0].Outcome = "partial" }},
		{name: "disabled category with non-skipped outcome", mutate: func(record *VerificationRecord) {
			record.Verification.Categories[0].Enabled = false
			record.Verification.Categories[0].Outcome = VerificationOutcomePass
			record.Verification.Categories[0].FailureClass = ""
			record.Verification.Categories[0].Message = ""
		}},
		{name: "enabled failed category without message", mutate: func(record *VerificationRecord) {
			record.Verification.Categories[0].Message = " "
		}},
		{name: "enabled failed category without failure class", mutate: func(record *VerificationRecord) {
			record.Verification.Categories[0].FailureClass = " "
		}},
		{name: "passed category with failure class", mutate: func(record *VerificationRecord) {
			record.Verification.Categories[1].FailureClass = VerificationFailureClassParameterMismatch
		}},
		{name: "evidence digest is not lowercase sha256", mutate: func(record *VerificationRecord) {
			record.Verification.Evidence[0].DigestSHA256 = upperDigestA()
		}},
		{name: "evidence source kind without source ref", mutate: func(record *VerificationRecord) {
			record.Verification.Evidence[0].SourceKind = "artifact"
			record.Verification.Evidence[0].SourceRef = " "
		}},
		{name: "evidence source ref without source kind", mutate: func(record *VerificationRecord) {
			record.Verification.Evidence[0].SourceKind = " "
			record.Verification.Evidence[0].SourceRef = "out/model.step"
		}},
		{name: "duplicate normalized evidence", mutate: func(record *VerificationRecord) {
			record.Verification.Evidence = append(record.Verification.Evidence, VerificationEvidence{
				SourceKind:   " artifact ",
				SourceRef:    " out/model.step ",
				DigestSHA256: " " + digestB() + " ",
			})
		}},
		{name: "identity mismatch", mutate: func(record *VerificationRecord) { record.Verification.Message = "changed message" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := cloneVerificationRecord(valid)
			tc.mutate(&record)

			if err := ValidateVerificationRecord(record); !errors.Is(err, ErrInvalidVerificationRecord) {
				t.Fatalf("ValidateVerificationRecord error = %v, want ErrInvalidVerificationRecord", err)
			}
		})
	}
}

func TestValidateVerificationRecordUsesInvalidVerificationRecordSentinel(t *testing.T) {
	record := mustBuildVerificationRecord(t, validVerificationRecordInput())
	record.Verification.Evidence[0].DigestSHA256 = "abc123"

	err := ValidateVerificationRecord(record)
	if !errors.Is(err, ErrInvalidVerificationRecord) {
		t.Fatalf("ValidateVerificationRecord error = %v, want ErrInvalidVerificationRecord", err)
	}
}

func TestVerificationVocabularyNormalizersTrimWithoutSilentlyAcceptingUnknowns(t *testing.T) {
	if got := NormalizeVerificationOutcome(" pass "); got != VerificationOutcomePass {
		t.Fatalf("NormalizeVerificationOutcome(pass) = %q, want %q", got, VerificationOutcomePass)
	}
	if got := NormalizeVerificationOutcome(" partial "); got != VerificationOutcome("partial") {
		t.Fatalf("NormalizeVerificationOutcome(unknown) = %q, want trimmed unknown", got)
	}
	if got := NormalizeVerificationCategory(" components "); got != VerificationCategoryComponents {
		t.Fatalf("NormalizeVerificationCategory(components) = %q, want %q", got, VerificationCategoryComponents)
	}
	if got := NormalizeVerificationCategory(" geometry "); got != VerificationCategory("geometry") {
		t.Fatalf("NormalizeVerificationCategory(unknown) = %q, want trimmed unknown", got)
	}
	if got := NormalizeVerificationFailureClass(" component_mismatch "); got != VerificationFailureClassComponentMismatch {
		t.Fatalf("NormalizeVerificationFailureClass(component_mismatch) = %q, want %q", got, VerificationFailureClassComponentMismatch)
	}
	if got := NormalizeVerificationFailureClass(" other "); got != VerificationFailureClass("other") {
		t.Fatalf("NormalizeVerificationFailureClass(unknown) = %q, want trimmed unknown", got)
	}
}

func validVerificationRecordInput() VerificationRecordInput {
	return VerificationRecordInput{
		RecordKey: " verification-record-1 ",
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
				StepRef:    " verify ",
			},
			Evidence: []EvidenceReference{
				{Kind: " log ", Ref: " logs/verify.log ", DigestSHA256: digestE()},
				{Kind: " artifact ", Ref: " out/model.step ", DigestSHA256: digestF()},
			},
			Runtime: RuntimeProvenance{
				ToolID:    " parametron ",
				RuntimeID: " runtime-1 ",
				Adapter:   " freecad ",
			},
		},
		Verification: VerificationSummary{
			Outcome:      " fail ",
			Message:      " verification failed ",
			FailureClass: " component_mismatch ",
			Linkage: VerificationLinkage{
				JobID:      " job-a ",
				ProductKey: " product-a ",
				StepRef:    " verify ",
			},
			Categories: []VerificationCategoryResult{
				{
					Category:     " parameters ",
					Enabled:      true,
					Outcome:      " pass ",
					Evidence:     []VerificationEvidence{{SourceKind: " table ", SourceRef: " params.csv ", DigestSHA256: " " + digestE() + " "}},
					FailureClass: " ",
				},
				{
					Category:     " components ",
					Enabled:      true,
					Outcome:      " fail ",
					Message:      " component mismatch ",
					FailureClass: " component_mismatch ",
					Evidence: []VerificationEvidence{
						{SourceKind: " runtime ", SourceRef: " runtime://verification/components ", DigestSHA256: " " + digestF() + " "},
						{SourceKind: " artifact ", SourceRef: " out/model.step ", DigestSHA256: "\t" + digestD() + "\n"},
					},
				},
			},
			Evidence: []VerificationEvidence{
				{SourceKind: " log ", SourceRef: " logs/verify.log ", DigestSHA256: " " + digestC() + " "},
				{SourceKind: " artifact ", SourceRef: " out/model.step ", DigestSHA256: "\t" + digestB() + "\n"},
			},
		},
	}
}

func equivalentVerificationRecordInput() VerificationRecordInput {
	input := validVerificationRecordInput()
	input.RecordKey = "\tverification-record-1\n"
	input.Provenance.Inputs = []ProvenanceInput{
		{Kind: "dsl", Identity: "model-a", DigestSHA256: " " + digestD() + " "},
		{Kind: "parameter-table", Identity: "params-a", DigestSHA256: digestC()},
	}
	input.Provenance.Evidence = []EvidenceReference{
		{Kind: "artifact", Ref: "out/model.step", DigestSHA256: digestF()},
		{Kind: "log", Ref: "logs/verify.log", DigestSHA256: digestE()},
	}
	input.Verification = VerificationSummary{
		Outcome:      "\tfail\n",
		Message:      " verification failed\n",
		FailureClass: "\tcomponent_mismatch ",
		Linkage: VerificationLinkage{
			JobID:      "\tjob-a ",
			ProductKey: " product-a\n",
			StepRef:    "\tverify ",
		},
		Categories: []VerificationCategoryResult{
			{
				Category: "parameters",
				Enabled:  true,
				Outcome:  "pass",
				Evidence: []VerificationEvidence{{SourceKind: "table", SourceRef: "params.csv", DigestSHA256: digestE()}},
			},
			{
				Category:     "\tcomponents ",
				Enabled:      true,
				Outcome:      " fail\n",
				Message:      "\tcomponent mismatch ",
				FailureClass: " component_mismatch\n",
				Evidence: []VerificationEvidence{
					{SourceKind: "artifact", SourceRef: "out/model.step", DigestSHA256: " " + digestD() + " "},
					{SourceKind: "\truntime ", SourceRef: " runtime://verification/components\n", DigestSHA256: digestF()},
				},
			},
		},
		Evidence: []VerificationEvidence{
			{SourceKind: "artifact", SourceRef: "out/model.step", DigestSHA256: " " + digestB() + " "},
			{SourceKind: "\tlog ", SourceRef: " logs/verify.log\n", DigestSHA256: digestC()},
		},
	}
	return input
}

func mustBuildVerificationRecord(t *testing.T, input VerificationRecordInput) VerificationRecord {
	t.Helper()
	record, err := BuildVerificationRecord(input)
	if err != nil {
		t.Fatalf("BuildVerificationRecord(%#v) returned error: %v", input, err)
	}
	return record
}

func cloneVerificationRecordInput(input VerificationRecordInput) VerificationRecordInput {
	input.Provenance.Inputs = append([]ProvenanceInput(nil), input.Provenance.Inputs...)
	input.Provenance.Evidence = append([]EvidenceReference(nil), input.Provenance.Evidence...)
	input.Verification.Categories = cloneVerificationCategoryResults(input.Verification.Categories)
	input.Verification.Evidence = append([]VerificationEvidence(nil), input.Verification.Evidence...)
	return input
}

func cloneVerificationRecord(record VerificationRecord) VerificationRecord {
	record.Provenance.Inputs = append([]ProvenanceInput(nil), record.Provenance.Inputs...)
	record.Provenance.Evidence = append([]EvidenceReference(nil), record.Provenance.Evidence...)
	record.Verification.Categories = cloneVerificationCategoryResults(record.Verification.Categories)
	record.Verification.Evidence = append([]VerificationEvidence(nil), record.Verification.Evidence...)
	return record
}

func cloneVerificationCategoryResults(categories []VerificationCategoryResult) []VerificationCategoryResult {
	out := append([]VerificationCategoryResult(nil), categories...)
	for i := range out {
		out[i].Evidence = append([]VerificationEvidence(nil), out[i].Evidence...)
	}
	return out
}
