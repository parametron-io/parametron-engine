package recordmap_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/observed"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/verification"
)

// Issue #18: target-state verification normalized through the existing
// verification record family. Results come from the real Engine verifier so
// category failure classes are authentic rather than hand-assembled.

const verificationObservedRef = "raw/observed/prm.observed.json"

func tsEvidenceBool(destination, object string, value bool) observed.BooleanTargetEvidence {
	return observed.BooleanTargetEvidence{Destination: destination, Object: object, Status: observed.BooleanEvidenceStatusObserved, Value: &value}
}

func tsEvidenceStatus(destination, object, status string) observed.BooleanTargetEvidence {
	return observed.BooleanTargetEvidence{Destination: destination, Object: object, Status: status}
}

func tsMapObservedValue(t *testing.T, raw string) observed.Value {
	t.Helper()
	value, err := observed.NewValue([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// realTargetStateVerification derives an Engine contract for a single
// suppression(true) intent and verifies it against the given evidence.
func realTargetStateVerification(t *testing.T, targetState *observed.TargetStateObservation, tweak func(*observed.Observed)) verification.Result {
	t.Helper()
	workingCopy := filepath.Join(t.TempDir(), "attempt", "source", "widget.FCStd")
	if err := os.MkdirAll(filepath.Dir(workingCopy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workingCopy, []byte("working copy bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	contract, err := verification.DeriveFromManifestAndWorkingCopy(planner.WriteExportManifestPayload{
		SchemaVersion: planner.ExportManifestSchemaVersion,
		AssemblyMutations: &planner.ExportManifestMutationCollection{
			Suppression: []planner.ExportManifestSuppressionMutation{{Object: "Feature", Suppressed: true}},
		},
	}, workingCopy)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	meta, ref := contract.Expected.Metadata[0], contract.Expected.References[0]
	obs := &observed.Observed{
		SchemaVersion: observed.SchemaVersion,
		WorkingCopy:   observed.WorkingCopy{Path: ref.Name, SHA256: meta.Value},
		Observation: observed.Observation{
			Parameters:  []observed.Parameter{},
			Metadata:    []observed.Metadata{{ID: "working-copy:sha256", Key: meta.Key, Value: tsMapObservedValue(t, `"`+meta.Value+`"`), ValueKind: "string"}},
			References:  []observed.Reference{{Kind: ref.Kind, Name: ref.Name}},
			Components:  []observed.Component{},
			TargetState: targetState,
		},
	}
	if tweak != nil {
		tweak(obs)
	}
	result, _ := verification.Verify(contract, obs)
	if result == nil {
		t.Fatal("Verify returned no result")
	}
	return *result
}

func tsSuppressionEvidence(entries ...observed.BooleanTargetEvidence) *observed.TargetStateObservation {
	if entries == nil {
		entries = []observed.BooleanTargetEvidence{}
	}
	return &observed.TargetStateObservation{
		Suppression: entries, Visibility: []observed.BooleanTargetEvidence{}, Existence: []observed.ExistenceTargetEvidence{},
	}
}

func targetStateVerificationInput(result verification.Result) recordmap.VerificationMappingInput {
	input := validVerificationMappingInput(result)
	input.EvidenceDigestSHA256 = digestA()
	input.ObservedEvidenceDigestSHA256 = digestB()
	return input
}

func TestMapVerificationTargetStateOutcomesUseExistingVerificationFamily(t *testing.T) {
	tests := []struct {
		name      string
		evidence  *observed.TargetStateObservation
		wantClass recordcontract.VerificationFailureClass
	}{
		{"pass", tsSuppressionEvidence(tsEvidenceBool("assembly", "Feature", true)), ""},
		{"target_state_mismatch", tsSuppressionEvidence(tsEvidenceBool("assembly", "Feature", false)), recordcontract.VerificationFailureClassTargetStateMismatch},
		{"target_missing", tsSuppressionEvidence(tsEvidenceStatus("assembly", "Feature", observed.BooleanEvidenceStatusTargetMissing)), recordcontract.VerificationFailureClassTargetMissing},
		{"native_evidence_unavailable", tsSuppressionEvidence(tsEvidenceStatus("assembly", "Feature", observed.BooleanEvidenceStatusUnavailable)), recordcontract.VerificationFailureClassNativeEvidenceUnavailable},
		{"required_observation_missing", tsSuppressionEvidence(), recordcontract.VerificationFailureClassRequiredObservationMissing},
		{"entire targetState omitted", nil, recordcontract.VerificationFailureClassRequiredObservationMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := realTargetStateVerification(t, tt.evidence, nil)
			got, err := recordmap.MapVerification(targetStateVerificationInput(result))
			if err != nil {
				t.Fatalf("MapVerification: %v", err)
			}
			record := got.VerificationRecord
			if err := recordcontract.ValidateVerificationRecord(record); err != nil {
				t.Fatalf("record invalid: %v", err)
			}
			if record.Family != recordcontract.FamilyVerification {
				t.Fatalf("family=%q", record.Family)
			}

			category := requireVerificationCategory(t, record.Verification.Categories, recordcontract.VerificationCategoryTargetState)
			if !category.Enabled {
				t.Fatalf("category=%+v", category)
			}
			if tt.wantClass == "" {
				if record.Verification.Outcome != recordcontract.VerificationOutcomePass || record.Verification.FailureClass != "" ||
					category.Outcome != recordcontract.VerificationOutcomePass || category.FailureClass != "" {
					t.Fatalf("record=%+v category=%+v", record.Verification, category)
				}
			} else if record.Verification.Outcome != recordcontract.VerificationOutcomeFail || record.Verification.FailureClass != tt.wantClass ||
				category.Outcome != recordcontract.VerificationOutcomeFail || category.FailureClass != tt.wantClass || category.Message == "" {
				t.Fatalf("record=%+v category=%+v, want class %s", record.Verification, category, tt.wantClass)
			}

			// Existing categories are unchanged by the addition.
			for _, name := range []recordcontract.VerificationCategory{recordcontract.VerificationCategoryMetadata, recordcontract.VerificationCategoryReferences} {
				c := requireVerificationCategory(t, record.Verification.Categories, name)
				if !c.Enabled || c.Outcome != recordcontract.VerificationOutcomePass || c.FailureClass != "" {
					t.Fatalf("%s category changed: %+v", name, c)
				}
			}
			for _, name := range []recordcontract.VerificationCategory{recordcontract.VerificationCategoryComponents, recordcontract.VerificationCategoryParameters} {
				if c := requireVerificationCategory(t, record.Verification.Categories, name); c.Enabled || c.Outcome != recordcontract.VerificationOutcomeSkipped {
					t.Fatalf("%s category changed: %+v", name, c)
				}
			}
		})
	}
}

func TestMapVerificationTargetStateCategoryOrderFollowsRecordContract(t *testing.T) {
	result := realTargetStateVerification(t, tsSuppressionEvidence(tsEvidenceBool("assembly", "Feature", true)), nil)
	first, err := recordmap.MapVerificationToVerificationRecord(targetStateVerificationInput(result))
	if err != nil {
		t.Fatal(err)
	}
	var got []recordcontract.VerificationCategory
	for _, category := range first.Verification.Categories {
		got = append(got, category.Category)
	}
	// The record contract sorts categories lexically by vocabulary value.
	want := []recordcontract.VerificationCategory{
		recordcontract.VerificationCategoryComponents, recordcontract.VerificationCategoryMetadata, recordcontract.VerificationCategoryParameters,
		recordcontract.VerificationCategoryReferences, recordcontract.VerificationCategoryTargetState,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("categories=%v want %v", got, want)
	}
	for i := 0; i < 5; i++ {
		again, err := recordmap.MapVerificationToVerificationRecord(targetStateVerificationInput(result))
		if err != nil || !reflect.DeepEqual(first, again) {
			t.Fatalf("repeat %d differs (err=%v)", i, err)
		}
	}
}

func TestMapVerificationTargetStateFailureDoesNotDisplaceEarlierCategory(t *testing.T) {
	result := realTargetStateVerification(t, tsSuppressionEvidence(tsEvidenceBool("assembly", "Feature", false)), func(obs *observed.Observed) {
		obs.Observation.References[0].Name = "/tmp/other.FCStd"
	})
	if result.Failure != verification.FailureClassReferenceMismatch || result.Categories.TargetState.Status != verification.CategoryStatusFail {
		t.Fatalf("precondition: %+v", result)
	}
	record, err := recordmap.MapVerificationToVerificationRecord(targetStateVerificationInput(result))
	if err != nil {
		t.Fatal(err)
	}
	if record.Verification.FailureClass != recordcontract.VerificationFailureClassReferenceMismatch {
		t.Fatalf("top-level class=%q", record.Verification.FailureClass)
	}
	refs := requireVerificationCategory(t, record.Verification.Categories, recordcontract.VerificationCategoryReferences)
	target := requireVerificationCategory(t, record.Verification.Categories, recordcontract.VerificationCategoryTargetState)
	if refs.FailureClass != recordcontract.VerificationFailureClassReferenceMismatch || target.FailureClass != recordcontract.VerificationFailureClassTargetStateMismatch {
		t.Fatalf("refs=%+v target=%+v", refs, target)
	}
}

func TestMapVerificationTargetStateUnsupportedFailureClassStillRejected(t *testing.T) {
	result := failVerificationResult(verification.FailureClass("target_state_bogus"), "bogus")
	_, err := recordmap.MapVerification(validVerificationMappingInput(result))
	assertInvalidVerificationMapping(t, err)
}

func TestMapVerificationTargetStateDisabledKeepsExistingRecordShape(t *testing.T) {
	withoutTarget := passVerificationResult()
	input := validVerificationMappingInput(withoutTarget)
	input.EvidenceDigestSHA256 = digestA()
	baseline, err := recordmap.MapVerificationToVerificationRecord(input)
	if err != nil {
		t.Fatal(err)
	}

	// Supplying an observed digest must not make unrelated observed provenance appear.
	input.ObservedEvidenceDigestSHA256 = digestB()
	withDigest, err := recordmap.MapVerificationToVerificationRecord(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline, withDigest) {
		t.Fatalf("observed digest changed a target-state-disabled record\nbase: %+v\ngot:  %+v", baseline, withDigest)
	}
	if len(baseline.Verification.Categories) != 4 {
		t.Fatalf("categories=%d, want the existing four", len(baseline.Verification.Categories))
	}
	for _, category := range baseline.Verification.Categories {
		if category.Category == recordcontract.VerificationCategoryTargetState {
			t.Fatalf("disabled target-state category emitted: %+v", category)
		}
		if hasVerificationEvidence(category.Evidence, "observed", verificationObservedRef, "") || hasVerificationEvidence(category.Evidence, "observed", verificationObservedRef, digestB()) {
			t.Fatalf("observed evidence leaked into %s", category.Category)
		}
	}
	if countEvidence(withDigest.Provenance.Evidence, "observed", verificationObservedRef) != 0 {
		t.Fatalf("observed provenance appeared: %+v", withDigest.Provenance.Evidence)
	}

	// A real disabled-state Verify result (Categories.TargetState skipped, disabled) behaves identically.
	realDisabled := verification.Result{Status: verification.StatusPass, Message: "verification passed", Categories: withoutTarget.Categories}
	realDisabled.Categories.TargetState = verification.CategoryResult{Status: verification.CategoryStatusSkipped, Message: "target-state verification is disabled"}
	got, err := recordmap.MapVerificationToVerificationRecord(validVerificationMappingInputWithDigests(realDisabled))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Verification.Categories) != 4 {
		t.Fatalf("disabled skipped target-state category emitted: %+v", got.Verification.Categories)
	}
}

func validVerificationMappingInputWithDigests(result verification.Result) recordmap.VerificationMappingInput {
	input := validVerificationMappingInput(result)
	input.EvidenceDigestSHA256 = digestA()
	input.ObservedEvidenceDigestSHA256 = digestB()
	return input
}

func TestMapVerificationTargetStateDualProvenance(t *testing.T) {
	passing := realTargetStateVerification(t, tsSuppressionEvidence(tsEvidenceBool("assembly", "Feature", true)), nil)
	failing := realTargetStateVerification(t, tsSuppressionEvidence(tsEvidenceBool("assembly", "Feature", false)), nil)

	t.Run("both digests present", func(t *testing.T) {
		for _, result := range []verification.Result{passing, failing} {
			record, err := recordmap.MapVerificationToVerificationRecord(targetStateVerificationInput(result))
			if err != nil {
				t.Fatal(err)
			}
			for _, evidence := range []struct{ kind, ref, digest string }{
				{"verification", verificationEvidenceRef, digestA()},
				{"observed", verificationObservedRef, digestB()},
			} {
				if !hasVerificationEvidence(record.Verification.Evidence, evidence.kind, evidence.ref, evidence.digest) {
					t.Fatalf("record evidence lacks %+v: %+v", evidence, record.Verification.Evidence)
				}
				if !hasEvidence(record.Provenance.Evidence, evidence.kind, evidence.ref, evidence.digest) {
					t.Fatalf("provenance lacks %+v: %+v", evidence, record.Provenance.Evidence)
				}
			}
			target := requireVerificationCategory(t, record.Verification.Categories, recordcontract.VerificationCategoryTargetState)
			if !hasVerificationEvidence(target.Evidence, "verification", verificationEvidenceRef, digestA()) ||
				!hasVerificationEvidence(target.Evidence, "observed", verificationObservedRef, digestB()) {
				t.Fatalf("target-state category evidence=%+v", target.Evidence)
			}
			// Only the target-state category cites observed evidence.
			for _, category := range record.Verification.Categories {
				if category.Category != recordcontract.VerificationCategoryTargetState && hasVerificationEvidence(category.Evidence, "observed", verificationObservedRef, digestB()) {
					t.Fatalf("%s cites observed evidence", category.Category)
				}
			}
			if hasInputKind(record.Provenance.Inputs, "observed") {
				t.Fatalf("observed evidence embedded as provenance input: %+v", record.Provenance.Inputs)
			}
		}
	})

	t.Run("digests omitted still reference canonical raw paths", func(t *testing.T) {
		input := validVerificationMappingInput(passing)
		record, err := recordmap.MapVerificationToVerificationRecord(input)
		if err != nil {
			t.Fatal(err)
		}
		if !hasEvidence(record.Provenance.Evidence, "verification", verificationEvidenceRef, "") || !hasEvidence(record.Provenance.Evidence, "observed", verificationObservedRef, "") {
			t.Fatalf("provenance=%+v", record.Provenance.Evidence)
		}
		if !hasVerificationEvidence(record.Verification.Evidence, "observed", verificationObservedRef, "") {
			t.Fatalf("evidence=%+v", record.Verification.Evidence)
		}
	})

	t.Run("only observed digest omitted", func(t *testing.T) {
		input := validVerificationMappingInput(passing)
		input.EvidenceDigestSHA256 = digestA()
		record, err := recordmap.MapVerificationToVerificationRecord(input)
		if err != nil {
			t.Fatal(err)
		}
		if !hasEvidence(record.Provenance.Evidence, "verification", verificationEvidenceRef, digestA()) || !hasEvidence(record.Provenance.Evidence, "observed", verificationObservedRef, "") {
			t.Fatalf("provenance=%+v", record.Provenance.Evidence)
		}
	})

	t.Run("matching existing entries are not duplicated", func(t *testing.T) {
		input := targetStateVerificationInput(passing)
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "verification", Ref: verificationEvidenceRef, DigestSHA256: digestA()},
			{Kind: "observed", Ref: verificationObservedRef, DigestSHA256: digestB()},
		}
		record, err := recordmap.MapVerificationToVerificationRecord(input)
		if err != nil {
			t.Fatal(err)
		}
		if countEvidence(record.Provenance.Evidence, "verification", verificationEvidenceRef) != 1 || countEvidence(record.Provenance.Evidence, "observed", verificationObservedRef) != 1 {
			t.Fatalf("duplicated: %+v", record.Provenance.Evidence)
		}
	})

	t.Run("existing entries without digest are upgraded", func(t *testing.T) {
		input := targetStateVerificationInput(passing)
		input.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "verification", Ref: verificationEvidenceRef},
			{Kind: "observed", Ref: verificationObservedRef},
		}
		record, err := recordmap.MapVerificationToVerificationRecord(input)
		if err != nil {
			t.Fatal(err)
		}
		if !hasEvidence(record.Provenance.Evidence, "verification", verificationEvidenceRef, digestA()) || !hasEvidence(record.Provenance.Evidence, "observed", verificationObservedRef, digestB()) ||
			countEvidence(record.Provenance.Evidence, "observed", verificationObservedRef) != 1 {
			t.Fatalf("not upgraded: %+v", record.Provenance.Evidence)
		}
	})

	t.Run("existing digest is preserved when none is supplied", func(t *testing.T) {
		input := validVerificationMappingInput(passing)
		input.Provenance.Evidence = []recordcontract.EvidenceReference{{Kind: "observed", Ref: verificationObservedRef, DigestSHA256: digestC()}}
		record, err := recordmap.MapVerificationToVerificationRecord(input)
		if err != nil {
			t.Fatal(err)
		}
		if !hasEvidence(record.Provenance.Evidence, "observed", verificationObservedRef, digestC()) || countEvidence(record.Provenance.Evidence, "observed", verificationObservedRef) != 1 {
			t.Fatalf("provenance=%+v", record.Provenance.Evidence)
		}
	})

	t.Run("conflicting existing digests fail deterministically", func(t *testing.T) {
		for _, conflict := range []recordcontract.EvidenceReference{
			{Kind: "observed", Ref: verificationObservedRef, DigestSHA256: digestC()},
			{Kind: "verification", Ref: verificationEvidenceRef, DigestSHA256: digestC()},
		} {
			input := targetStateVerificationInput(passing)
			input.Provenance.Evidence = []recordcontract.EvidenceReference{conflict}
			for i := 0; i < 3; i++ {
				_, err := recordmap.MapVerificationToVerificationRecord(input)
				assertInvalidVerificationMapping(t, err)
				if !strings.Contains(err.Error(), "conflicting digest") {
					t.Fatalf("err=%v", err)
				}
			}
		}
	})

	t.Run("invalid observed digest is rejected", func(t *testing.T) {
		for _, bad := range []string{"A" + digestB()[1:], "g" + digestB()[1:], digestB()[:63], digestB() + "b"} {
			input := targetStateVerificationInput(passing)
			input.ObservedEvidenceDigestSHA256 = bad
			_, err := recordmap.MapVerificationToVerificationRecord(input)
			assertInvalidVerificationMapping(t, err)
		}
	})

	t.Run("caller provenance and result are not mutated", func(t *testing.T) {
		input := targetStateVerificationInput(passing)
		input.Provenance.Evidence = []recordcontract.EvidenceReference{{Kind: "observed", Ref: verificationObservedRef}}
		before := append([]recordcontract.EvidenceReference(nil), input.Provenance.Evidence...)
		resultBefore := input.Result
		if _, err := recordmap.MapVerification(input); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, input.Provenance.Evidence) || !reflect.DeepEqual(resultBefore, input.Result) {
			t.Fatal("mapping mutated its input")
		}
	})

	t.Run("caller evidence ordering does not change identity", func(t *testing.T) {
		a := targetStateVerificationInput(passing)
		a.Provenance.Evidence = []recordcontract.EvidenceReference{
			{Kind: "log", Ref: "raw/log.txt", DigestSHA256: digestC()},
			{Kind: "observed", Ref: verificationObservedRef},
			{Kind: "verification", Ref: verificationEvidenceRef},
		}
		b := targetStateVerificationInput(passing)
		b.Provenance.Evidence = []recordcontract.EvidenceReference{a.Provenance.Evidence[2], a.Provenance.Evidence[0], a.Provenance.Evidence[1]}
		ra, err := recordmap.MapVerificationToVerificationRecord(a)
		if err != nil {
			t.Fatal(err)
		}
		rb, err := recordmap.MapVerificationToVerificationRecord(b)
		if err != nil || !reflect.DeepEqual(ra, rb) || ra.Identity != rb.Identity {
			t.Fatalf("evidence order leaked (err=%v)", err)
		}
	})
}

func TestMapVerificationTargetStateRecordIdentityTracksVerificationMaterial(t *testing.T) {
	build := func(result verification.Result, mutate func(*recordmap.VerificationMappingInput)) recordcontract.VerificationRecord {
		input := targetStateVerificationInput(result)
		if mutate != nil {
			mutate(&input)
		}
		record, err := recordmap.MapVerificationToVerificationRecord(input)
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	pass := realTargetStateVerification(t, tsSuppressionEvidence(tsEvidenceBool("assembly", "Feature", true)), nil)
	mismatch := realTargetStateVerification(t, tsSuppressionEvidence(tsEvidenceBool("assembly", "Feature", false)), nil)
	missing := realTargetStateVerification(t, tsSuppressionEvidence(tsEvidenceStatus("assembly", "Feature", observed.BooleanEvidenceStatusTargetMissing)), nil)
	unavailable := realTargetStateVerification(t, tsSuppressionEvidence(tsEvidenceStatus("assembly", "Feature", observed.BooleanEvidenceStatusUnavailable)), nil)

	base := build(pass, nil)
	if again := build(pass, nil); again.Identity != base.Identity {
		t.Fatal("identity is not stable for equivalent results")
	}
	seen := map[string]string{"pass": base.Identity.ID}
	for name, record := range map[string]recordcontract.VerificationRecord{
		"mismatch":        build(mismatch, nil),
		"missing":         build(missing, nil),
		"unavailable":     build(unavailable, nil),
		"observed digest": build(pass, func(i *recordmap.VerificationMappingInput) { i.ObservedEvidenceDigestSHA256 = digestC() }),
		"request digest":  build(pass, func(i *recordmap.VerificationMappingInput) { i.EvidenceDigestSHA256 = digestC() }),
	} {
		for other, id := range seen {
			if id == record.Identity.ID {
				t.Fatalf("identity for %s equals identity for %s", name, other)
			}
		}
		seen[name] = record.Identity.ID
	}
}

// keep errors imported for assertions that may be extended without churn.
var _ = errors.Is
