package recordmap_test

import (
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/verification"
)

func TestMapReportClassifiesIntegrityFailuresDistinctly(t *testing.T) {
	rep := failedReportWithError()
	rep.Error.Category = "integrity"
	rep.Error.Boundary = "engine"
	rep.Error.Code = "lock_source_mismatch"
	rep.Error.Classification = verification.FailureClass("lock_source_mismatch")
	rep.Error.Stage = "integrity"

	mapped, err := recordmap.MapReport(testMappingInput(rep))
	if err != nil {
		t.Fatalf("MapReport returned error: %v", err)
	}
	if mapped.FailureRecord == nil {
		t.Fatal("integrity failure produced no normalized failure record")
	}
	failure := mapped.FailureRecord.Failure
	if failure.Class != recordcontract.FailureClassIntegrity {
		t.Fatalf("failure class = %q, want %q", failure.Class, recordcontract.FailureClassIntegrity)
	}
	if failure.Stage != recordcontract.FailureStageIntegrity {
		t.Fatalf("failure stage = %q, want %q", failure.Stage, recordcontract.FailureStageIntegrity)
	}
	if failure.Code != "lock_source_mismatch" {
		t.Fatalf("failure code = %q, want the specific integrity code", failure.Code)
	}
	for _, collapsed := range []recordcontract.FailureClass{
		recordcontract.FailureClassExecution,
		recordcontract.FailureClassRuntime,
		recordcontract.FailureClassVerification,
	} {
		if failure.Class == collapsed {
			t.Fatalf("integrity failure collapsed into %q", collapsed)
		}
	}
}

func TestMapReportIntegrityClassificationIsDeterministicAcrossCodes(t *testing.T) {
	codes := []string{
		"lock_digest_malformed",
		"lock_resource_missing",
		"materialization_content_mismatch",
		"runtime_baseline_mismatch",
		"persisted_result_content_mismatch",
	}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			var previous recordcontract.FailureRecord
			for run := 0; run < 2; run++ {
				rep := failedReportWithError()
				rep.Error.Category = "integrity"
				rep.Error.Code = code
				rep.Error.Classification = verification.FailureClass(code)
				mapped, err := recordmap.MapReport(testMappingInput(rep))
				if err != nil {
					t.Fatal(err)
				}
				if mapped.FailureRecord == nil {
					t.Fatal("no failure record")
				}
				if mapped.FailureRecord.Failure.Class != recordcontract.FailureClassIntegrity ||
					mapped.FailureRecord.Failure.Stage != recordcontract.FailureStageIntegrity ||
					mapped.FailureRecord.Failure.Code != code {
					t.Fatalf("mapped failure = %#v, want integrity/%s", mapped.FailureRecord.Failure, code)
				}
				if run == 1 && mapped.FailureRecord.Identity.ID != previous.Identity.ID {
					t.Fatalf("equivalent integrity failures derived different identities: %q / %q",
						previous.Identity.ID, mapped.FailureRecord.Identity.ID)
				}
				previous = *mapped.FailureRecord
			}
		})
	}
}

func TestMapReportNonIntegrityFailureRemainsExecutionClass(t *testing.T) {
	// A generic execution failure must not be reclassified as integrity.
	rep := failedReportWithError()
	rep.Error.Category = ""
	rep.Error.Code = ""
	rep.Error.Classification = verification.FailureClassNone
	mapped, err := recordmap.MapReport(testMappingInput(rep))
	if err != nil {
		t.Fatal(err)
	}
	if mapped.FailureRecord == nil {
		t.Fatal("failure produced no record")
	}
	if got := mapped.FailureRecord.Failure.Class; got != recordcontract.FailureClassExecution {
		t.Fatalf("failure class = %q, want execution for a non-integrity failure", got)
	}
}
