package recordemit_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/executor"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
)

// Issue #1: a valid, correlated CAD runtime-native failure replaces the
// report-derived failure record. A package holds at most one failure record.

const failedPlanHash = "runtime-failure-plan"

// nonCanonicalFailedResult is a valid failed prm.result.json whose formatting
// and key order differ from any re-serialization of the same content.
func nonCanonicalFailedResult() []byte {
	return []byte("{\n  \"status\":   \"failed\",\n  \"schemaVersion\": \"1.0\",\n  \"failure\": {\"stage\":\"artifact_export\",  \"message\":\"native export message\",\"code\":\"export_failed\",\"category\":\"runtime\",\"boundary\":\"freecad\"}\n}\n")
}

func runtimeNativeFailure() *executor.CADRuntimeFailureOutcome {
	return &executor.CADRuntimeFailureOutcome{
		RuntimeNative: true, Class: "export", SemanticStage: "export",
		Boundary: "freecad", Category: "runtime", Code: "export_failed", Stage: "artifact_export",
		Message: "native export message",
	}
}

func emitFailedRun(t *testing.T, evidence *recordemit.CADRuntimeRunEvidence) (map[string][]byte, recordcontract.FailureRecord) {
	t.Helper()
	_, files := emitAndReadPackage(t, recordemit.RunEmitInput{
		PlanHash:   failedPlanHash,
		Report:     syntheticFailedReport(failedPlanHash),
		Metadata:   syntheticMetadata(failedPlanHash),
		CADRuntime: evidence,
	})
	return files, decodeSingleFailure(t, files)
}

// decodeSingleFailure requires exactly one failure record in both the
// manifest and the package files and returns it.
func decodeSingleFailure(t *testing.T, files map[string][]byte) recordcontract.FailureRecord {
	t.Helper()
	if entries := manifestRecordsOf(t, files, "failure"); len(entries) != 1 {
		t.Fatalf("manifest failure records = %#v, want exactly one", entries)
	}
	var failureFiles []string
	for path := range files {
		if strings.HasPrefix(path, "records/") && strings.Contains(path, "failure") {
			failureFiles = append(failureFiles, path)
		}
	}
	if len(failureFiles) != 1 || failureFiles[0] != recordpackage.MustRecordContractPath("failure") {
		t.Fatalf("failure record files = %v, want only %q", failureFiles, recordpackage.MustRecordContractPath("failure"))
	}
	var record recordcontract.FailureRecord
	if err := json.Unmarshal(files[failureFiles[0]], &record); err != nil {
		t.Fatalf("decode failure record: %v", err)
	}
	if err := recordcontract.ValidateFailureRecord(record); err != nil {
		t.Fatalf("failure record invalid: %v", err)
	}
	return record
}

func nativeFailureEvidence(result []byte) *recordemit.CADRuntimeRunEvidence {
	return &recordemit.CADRuntimeRunEvidence{
		Result: result, Failure: runtimeNativeFailure(),
		JobID: "job-widget", ProductKey: "widget", StepRef: "0:RunCADRuntime",
	}
}

func TestEmitRunPackage_RuntimeNativeFailureReplacesReportDerivedFailure(t *testing.T) {
	result := nonCanonicalFailedResult()
	baselineFiles, baseline := emitFailedRun(t, nil)
	files, failure := emitFailedRun(t, nativeFailureEvidence(result))

	if failure.RecordKey != "engine-run:"+failedPlanHash+":failure" {
		t.Fatalf("RecordKey = %q", failure.RecordKey)
	}
	f := failure.Failure
	if f.Class != recordcontract.FailureClassExport || f.Stage != recordcontract.FailureStageExport ||
		f.Severity != recordcontract.FailureSeverityError || f.Code != "export_failed" || f.Message != "native export message" {
		t.Fatalf("failure summary = %#v", f)
	}
	if want := (recordcontract.FailureLinkage{JobID: "job-widget", ProductKey: "widget", StepRef: "0:RunCADRuntime"}); f.Linkage != want {
		t.Fatalf("linkage = %+v, want %+v", f.Linkage, want)
	}

	// It is the record the generic mapper builds from the same semantics, not
	// a report-derived one: identical bytes to a direct mapping.
	wantDigest := sha256Hex(result)
	if len(f.Evidence) != 1 || f.Evidence[0].SourceKind != "runtime-result" ||
		f.Evidence[0].SourceRef != recordpackage.RawRuntimeResultContractPath() || f.Evidence[0].DigestSHA256 != wantDigest {
		t.Fatalf("failure evidence = %#v, want the exact-byte digest %s", f.Evidence, wantDigest)
	}
	if got, ok := evidenceDigest(failure.Provenance.Evidence, "runtime-result", recordpackage.RawRuntimeResultContractPath()); !ok || got != wantDigest {
		t.Fatalf("provenance runtime-result digest = %q (present=%t), want %s", got, ok, wantDigest)
	}
	direct, err := recordmap.MapCADRuntimeFailure(recordmap.CADRuntimeFailureMappingInput{
		Failure: *runtimeNativeFailure(), RecordKey: "engine-run:" + failedPlanHash,
		Provenance: failure.Provenance, Linkage: f.Linkage, EvidenceDigestSHA256: wantDigest,
		RetryCount: baseline.Failure.RetryCount, OccurredAt: baseline.Failure.OccurredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(direct, failure) {
		t.Fatalf("emitted failure is not the generic mapper's record: %#v vs %#v", failure, direct)
	}
	// The record names its plan exactly as the report-derived record does,
	// even though a failed run has no metadata provenance.
	if failure.Provenance.Plan.PlanHash != failedPlanHash || failure.Provenance.Plan != baseline.Provenance.Plan {
		t.Fatalf("provenance plan = %#v, want plan hash %q (baseline %#v)", failure.Provenance.Plan, failedPlanHash, baseline.Provenance.Plan)
	}
	// Report-owned operational context is retained, while the report's own
	// class/stage/code/message do not leak in. (The emitter strips runtime
	// timestamps for deterministic packages, so the report-derived OccurredAt is
	// carried as-is: equal to the fallback's.)
	if baseline.Failure.RetryCount != 2 || f.RetryCount != baseline.Failure.RetryCount ||
		!reflect.DeepEqual(f.OccurredAt, baseline.Failure.OccurredAt) {
		t.Fatalf("retryCount/occurredAt = %d/%v, report-derived = %d/%v", f.RetryCount, f.OccurredAt, baseline.Failure.RetryCount, baseline.Failure.OccurredAt)
	}
	if f.Class == baseline.Failure.Class || f.Stage == baseline.Failure.Stage || f.Code == baseline.Failure.Code || f.Message == baseline.Failure.Message {
		t.Fatalf("report semantics contaminated the native failure: native %#v, report %#v", f, baseline.Failure)
	}
	// The report-derived record differs in identity and evidence; it is not
	// also present.
	if baseline.Identity.ID == failure.Identity.ID || baseline.Failure.Evidence[0].SourceKind != "report" {
		t.Fatalf("baseline failure = %#v", baseline)
	}
	for _, evidence := range f.Evidence {
		if evidence.SourceKind == "report" {
			t.Fatalf("native failure record also cites report evidence: %#v", f.Evidence)
		}
	}

	// Raw evidence: exact bytes at the canonical path, report preserved.
	if !bytes.Equal(files[recordpackage.RawRuntimeResultContractPath()], result) {
		t.Fatalf("raw runtime result was not preserved byte-for-byte:\n%s", files[recordpackage.RawRuntimeResultContractPath()])
	}
	if !bytes.Equal(files[recordpackage.RawReportContractPath()], baselineFiles[recordpackage.RawReportContractPath()]) {
		t.Fatal("raw report changed when native failure evidence is present")
	}
	// The execution record is still report-derived and unaffected.
	execPath := recordpackage.MustRecordContractPath("execution")
	if !bytes.Equal(files[execPath], baselineFiles[execPath]) {
		t.Fatalf("execution record changed:\n%s\nvs\n%s", files[execPath], baselineFiles[execPath])
	}
	// Only failure-related package content differs.
	failurePath := recordpackage.MustRecordContractPath("failure")
	for path, content := range baselineFiles {
		if path == failurePath || path == recordpackage.PackageManifestContractPath() || path == recordpackage.RawRuntimeResultContractPath() {
			continue
		}
		if !bytes.Equal(content, files[path]) {
			t.Fatalf("unrelated package file %q changed", path)
		}
	}
}

func TestEmitRunPackage_RuntimeNativeFailureIsDeterministic(t *testing.T) {
	result := nonCanonicalFailedResult()
	firstFiles, first := emitFailedRun(t, nativeFailureEvidence(result))
	secondFiles, second := emitFailedRun(t, nativeFailureEvidence(result))
	if first.Identity != second.Identity {
		t.Fatalf("identity differs: %#v vs %#v", first.Identity, second.Identity)
	}
	if len(firstFiles) != len(secondFiles) {
		t.Fatalf("package file sets differ: %d vs %d", len(firstFiles), len(secondFiles))
	}
	for path, content := range firstFiles {
		if !bytes.Equal(content, secondFiles[path]) {
			t.Fatalf("package file %q differs between identical emissions", path)
		}
	}
}

func TestEmitRunPackage_AbsentNativeFailureKeepsReportDerivedFailure(t *testing.T) {
	baselineFiles, baseline := emitFailedRun(t, nil)
	failurePath := recordpackage.MustRecordContractPath("failure")

	for name, evidence := range map[string]*recordemit.CADRuntimeRunEvidence{
		"no cad runtime evidence": nil,
		"result bytes without native failure outcome": {
			Result: nonCanonicalFailedResult(), JobID: "job-widget", ProductKey: "widget", StepRef: "0:RunCADRuntime",
		},
	} {
		t.Run(name, func(t *testing.T) {
			files, failure := emitFailedRun(t, evidence)
			if !bytes.Equal(files[failurePath], baselineFiles[failurePath]) {
				t.Fatalf("report-derived failure changed:\n%s\nvs\n%s", files[failurePath], baselineFiles[failurePath])
			}
			if failure.Identity != baseline.Identity || !emitFailureHasEvidence(failure, "report", recordpackage.RawReportContractPath()) {
				t.Fatalf("failure = %#v", failure)
			}
			for _, evidence := range failure.Failure.Evidence {
				if evidence.SourceKind == "runtime-result" {
					t.Fatalf("report fallback cites runtime result: %#v", failure.Failure.Evidence)
				}
			}
		})
	}
}

func TestEmitRunPackage_InvalidNativeFailureFailsClosedWithoutSynthesizingRecord(t *testing.T) {
	for name, mutate := range map[string]func(*executor.CADRuntimeFailureOutcome){
		"not runtime native":    func(f *executor.CADRuntimeFailureOutcome) { f.RuntimeNative = false },
		"missing semantic":      func(f *executor.CADRuntimeFailureOutcome) { f.Class, f.SemanticStage = "", "" },
		"missing code":          func(f *executor.CADRuntimeFailureOutcome) { f.Code = "" },
		"missing message":       func(f *executor.CADRuntimeFailureOutcome) { f.Message = "" },
		"unknown semantic pair": func(f *executor.CADRuntimeFailureOutcome) { f.Class = "bogus" },
	} {
		t.Run(name, func(t *testing.T) {
			evidence := nativeFailureEvidence(nonCanonicalFailedResult())
			mutate(evidence.Failure)
			runRoot := t.TempDir()
			err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
				RunRoot: runRoot, PlanHash: failedPlanHash, Report: syntheticFailedReport(failedPlanHash),
				Metadata: syntheticMetadata(failedPlanHash), CADRuntime: evidence,
			})
			if !errors.Is(err, recordmap.ErrInvalidCADRuntimeFailureMapping) {
				t.Fatalf("EmitRunPackage error = %v, want ErrInvalidCADRuntimeFailureMapping", err)
			}
		})
	}
}
