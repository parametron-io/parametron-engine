package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/artifact"
	"parametron/internal/engine/executor"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/report"
	"parametron/internal/engine/scheduler"
)

// Issue #1, runtime-native failure path through the normal run:
//
//	fake external runtime -> aligned result parsing -> executor consumption ->
//	generic failure semantics -> recordemit -> normalized FailureRecord
//
// The runtime is the controlled aligned runtime (installControlledAlignedRuntime);
// no FreeCAD is required.

// nonCanonicalFailedResult is a valid aligned failed result whose whitespace
// and key order would change if it were re-serialized.
func nonCanonicalFailedResult(stage, code, message string) string {
	stageField := ""
	if stage != "" {
		stageField = `"stage":   "` + stage + `",` + "\n    "
	}
	return "{\n  \"status\":  \"failed\",\n  \"schemaVersion\": \"1.0\",\n  \"failure\": {\n    " + stageField +
		`"message": "` + message + `",` + "\n    \"code\": \"" + code + "\",\n    \"category\": \"runtime\",\n    \"boundary\": \"freecad\"\n  }\n}\n"
}

// runFailedProjectWithRawResult runs the project with a controlled runtime that
// exits non-zero after writing raw as its prm.result.json (no file when
// rawSet is false).
func runFailedProjectWithRawResult(t *testing.T, projectDir, outDir string, raw string, rawSet bool) failedCLIProjectExecution {
	t.Helper()
	installControlledAlignedRuntime(t, "failure_raw")
	if rawSet {
		t.Setenv("PARAMETRON_TASK13_RUNTIME_RAW_RESULT", raw)
	}
	return executeFailedProject(t, projectDir, outDir)
}

func runFailedProjectWithMode(t *testing.T, projectDir, outDir, mode string) failedCLIProjectExecution {
	t.Helper()
	installControlledAlignedRuntime(t, mode)
	return executeFailedProject(t, projectDir, outDir)
}

func executeFailedProject(t *testing.T, projectDir, outDir string) failedCLIProjectExecution {
	t.Helper()
	resetGlobals()
	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun: %v", err)
	}
	result, err := executePlanRun(executionOptions{Planned: planned, OutputDir: outDir, UseCache: true})
	if result == nil {
		t.Fatalf("executePlanRun failed without a result: %v", err)
	}
	if err == nil {
		t.Fatal("expected the run to fail")
	}
	return failedCLIProjectExecution{result: result, planned: planned, err: err}
}

// failedCADOutcome returns the one CAD outcome of the failed job.
func failedCADOutcome(t *testing.T, execution scheduler.ExecutionResult) *executor.CADRuntimeOutcome {
	t.Helper()
	var found *executor.CADRuntimeOutcome
	for _, job := range execution.Jobs {
		if job.CADRuntimeOutcome != nil {
			if found != nil {
				t.Fatal("expected exactly one CAD runtime outcome")
			}
			if job.Err == nil {
				t.Fatal("failed run has a CAD outcome for a job without an error")
			}
			found = job.CADRuntimeOutcome
		}
	}
	if found == nil || found.Failure == nil {
		t.Fatalf("no CAD failure outcome: %#v", found)
	}
	return found
}

// singleFailureRecord requires exactly one failure record in the manifest and
// in the package files and returns it.
func singleFailureRecord(t *testing.T, run failedCLIProjectExecution) (recordcontract.FailureRecord, map[string][]byte) {
	t.Helper()
	packageRoot := recordPackageRoot(run.result.RunRoot)
	files := readRecordPackageFiles(t, packageRoot)
	manifest := readCLIRecordPackageManifest(t, packageRoot)
	families := manifestFamilies(manifest)
	if len(families["failure"]) != 1 || len(families["execution"]) != 1 {
		t.Fatalf("manifest families = %#v, want one execution and exactly one failure record", families)
	}
	var failureFiles []string
	for path := range files {
		if strings.HasPrefix(path, "records/") && strings.Contains(path, "failure") {
			failureFiles = append(failureFiles, path)
		}
	}
	if !reflect.DeepEqual(failureFiles, []string{recordpackage.MustRecordContractPath("failure")}) {
		t.Fatalf("failure record files = %v", failureFiles)
	}
	for _, family := range []string{"observation", "verification", "reference"} {
		if len(families[family]) != 0 {
			t.Fatalf("%s record emitted for a failed run: %#v", family, families[family])
		}
	}
	failure := readCLIFailureRecord(t, packageRoot)
	if err := recordcontract.ValidateFailureRecord(failure); err != nil {
		t.Fatalf("failure record invalid: %v", err)
	}
	return failure, files
}

func assertOriginalOperationalError(t *testing.T, run failedCLIProjectExecution, wantSubstring string) {
	t.Helper()
	if run.result.ExecutionError == nil || run.err != run.result.ExecutionError {
		t.Fatalf("returned error %v is not the run's execution error %v", run.err, run.result.ExecutionError)
	}
	if strings.Contains(run.err.Error(), "emit record package") {
		t.Fatalf("emission wrapper replaced the execution error: %v", run.err)
	}
	var execErr *executor.ExecutionError
	var consumptionErr *executor.CADRuntimeConsumptionError
	if !errors.As(run.err, &execErr) || !errors.As(run.err, &consumptionErr) ||
		consumptionErr.Stage != executor.CADRuntimeConsumptionStageRuntimeFailure {
		t.Fatalf("execution error %v does not carry the runtime-failure consumption error", run.err)
	}
	if !strings.Contains(run.err.Error(), wantSubstring) {
		t.Fatalf("execution error %v does not mention %q", run.err, wantSubstring)
	}
}

func TestCLIProjectRun_RuntimeNativeFailureIsNormalizedThroughGenericMapper(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	raw := nonCanonicalFailedResult("artifact_export", "export_failed", "STEP export refused the document")
	run := runFailedProjectWithRawResult(t, projectDir, filepath.Join(t.TempDir(), "out"), raw, true)

	assertOriginalOperationalError(t, run, "export_failed")
	failure, files := singleFailureRecord(t, run)
	outcome := failedCADOutcome(t, run.result.Execution)
	if !outcome.Failure.RuntimeNative || outcome.Failure.Class != "export" || outcome.Failure.SemanticStage != "export" {
		t.Fatalf("consumed outcome failure = %#v", outcome.Failure)
	}

	wantKey := "engine-run:" + run.planned.PlanHash + ":failure"
	f := failure.Failure
	if failure.RecordKey != wantKey ||
		f.Class != recordcontract.FailureClassExport || f.Stage != recordcontract.FailureStageExport ||
		f.Severity != recordcontract.FailureSeverityError ||
		f.Code != "export_failed" || f.Message != "STEP export refused the document" {
		t.Fatalf("failure = %#v", failure)
	}
	// Engine-owned operational context comes from the report, not the runtime.
	if reportErr := run.result.Report.Error; reportErr == nil || reportErr.RetryCount <= 0 || f.RetryCount != reportErr.RetryCount {
		t.Fatalf("failure retryCount = %d, report error = %#v, want the report's positive retry count", f.RetryCount, reportErr)
	}
	if failure.Provenance.Plan.PlanHash != run.planned.PlanHash {
		t.Fatalf("failure plan hash = %q, want %q", failure.Provenance.Plan.PlanHash, run.planned.PlanHash)
	}
	// Linkage names the authoritative failed job / product / step.
	if f.Linkage.JobID != outcome.JobID || f.Linkage.ProductKey != outcome.ProductKey || f.Linkage.StepRef != outcome.StepID ||
		f.Linkage.JobID == "" || f.Linkage.ProductKey == "" || f.Linkage.StepRef == "" {
		t.Fatalf("failure linkage = %+v, outcome = %s/%s/%s", f.Linkage, outcome.JobID, outcome.ProductKey, outcome.StepID)
	}
	if run.result.Report.Error == nil || run.result.Report.Error.ProductID != outcome.ProductKey || run.result.Report.Error.StepID != outcome.StepID {
		t.Fatalf("report error %+v does not name the outcome's product/step", run.result.Report.Error)
	}

	// Exact raw bytes: what the runtime wrote, at the canonical package path.
	original, err := os.ReadFile(outcome.ResultPath)
	if err != nil || string(original) != raw {
		t.Fatalf("attempt result differs from what the runtime wrote (err=%v)", err)
	}
	preserved := files[recordpackage.RawRuntimeResultContractPath()]
	if !bytes.Equal(preserved, original) {
		t.Fatalf("raw runtime result not preserved byte-for-byte:\nwant %q\ngot  %q", original, preserved)
	}
	wantDigest := sha256HexOf(original)
	if reserialized := strings.Join(strings.Fields(raw), ""); sha256HexOf([]byte(reserialized)) == wantDigest {
		t.Fatal("fixture is not distinguishable from a reserialization")
	}
	if !reflect.DeepEqual(f.Evidence, []recordcontract.FailureEvidence{{
		SourceKind: "runtime-result", SourceRef: recordpackage.RawRuntimeResultContractPath(), DigestSHA256: wantDigest,
	}}) {
		t.Fatalf("failure evidence = %#v, want only the runtime result at digest %s", f.Evidence, wantDigest)
	}
	if got, ok := evidenceRefDigest(failure.Provenance.Evidence, "runtime-result", recordpackage.RawRuntimeResultContractPath()); !ok || got != wantDigest {
		t.Fatalf("provenance runtime-result digest = %q (present=%t), want %s", got, ok, wantDigest)
	}
	// Native boundary/category/stage stay in raw evidence only.
	for _, native := range []string{"freecad", "artifact_export"} {
		if bytes.Contains(files[recordpackage.MustRecordContractPath("failure")], []byte(native)) {
			t.Fatalf("normalized failure record carries native detail %q", native)
		}
	}
	if !bytes.Contains(preserved, []byte("artifact_export")) || !bytes.Contains(preserved, []byte("freecad")) {
		t.Fatal("raw runtime result lost native boundary/stage details")
	}

	// The raw report is preserved and the execution record is still report-derived.
	reportOnDisk, err := os.ReadFile(filepath.Join(run.result.RunRoot, report.FileName))
	if err != nil || !bytes.Equal(files[recordpackage.RawReportContractPath()], reportOnDisk) {
		t.Fatalf("raw report not preserved (err=%v)", err)
	}
	manifest := readCLIRecordPackageManifest(t, recordPackageRoot(run.result.RunRoot))
	assertManifestRawEvidencePaths(t, manifest, []string{recordpackage.RawReportContractPath(), recordpackage.RawRuntimeResultContractPath()})
	var execution recordcontract.ExecutionRecord
	decodePackageRecord(t, files, recordpackage.MustRecordContractPath("execution"), &execution)
	if execution.RecordKey != "engine-run:"+run.planned.PlanHash || execution.Execution.Outcome != recordcontract.ExecutionOutcomeFailed {
		t.Fatalf("execution record = %#v", execution)
	}
}

// The same run with a result the Engine cannot trust as a native failure is
// the report-derived baseline the native record must differ from.
func TestCLIProjectRun_RuntimeNativeFailureLeavesExecutionAndReportRecordsUnchanged(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	native := runFailedProjectWithRawResult(t, projectDir, filepath.Join(t.TempDir(), "native"),
		nonCanonicalFailedResult("recompute", "controlled_failure", "intentional controlled aligned runtime failure"), true)
	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatal(err)
	}
	// Malformed result: report-derived fallback, otherwise the same failure.
	fallback := runFailedProjectWithRawResult(t, projectDir, filepath.Join(t.TempDir(), "fallback"), "{", true)

	nativeFailure, nativeFiles := singleFailureRecord(t, native)
	fallbackFailure, fallbackFiles := singleFailureRecord(t, fallback)
	// Each record's retry count is its own run report's (a native failure is
	// retried; a malformed result is not), and the fallback keeps exactly the
	// report's value. Timing is stripped from emitted reports for determinism,
	// so neither carries an invented occurrence time.
	for name, tc := range map[string]struct {
		run     failedCLIProjectExecution
		failure recordcontract.FailureRecord
	}{"native": {native, nativeFailure}, "fallback": {fallback, fallbackFailure}} {
		reportErr := tc.run.result.Report.Error
		if reportErr == nil || reportErr.RetryCount <= 0 || tc.failure.Failure.RetryCount != reportErr.RetryCount || tc.failure.Failure.OccurredAt != nil {
			t.Fatalf("%s: retryCount/occurredAt = %d/%v, report error = %#v", name, tc.failure.Failure.RetryCount, tc.failure.Failure.OccurredAt, reportErr)
		}
	}
	if nativeFailure.Failure.Code != "controlled_failure" || fallbackFailure.Failure.Code == "controlled_failure" ||
		nativeFailure.Failure.Message == fallbackFailure.Failure.Message {
		t.Fatalf("native/report semantics mixed: %#v vs %#v", nativeFailure.Failure, fallbackFailure.Failure)
	}
	if nativeFailure.Identity.ID == fallbackFailure.Identity.ID {
		t.Fatal("native and report-derived failure records share an identity")
	}
	executionPath := recordpackage.MustRecordContractPath("execution")
	if string(nativeFiles[executionPath]) == "" || nativeFailure.RecordKey != fallbackFailure.RecordKey {
		t.Fatalf("record keys differ: %q vs %q", nativeFailure.RecordKey, fallbackFailure.RecordKey)
	}
	var nativeExec, fallbackExec recordcontract.ExecutionRecord
	decodePackageRecord(t, nativeFiles, executionPath, &nativeExec)
	decodePackageRecord(t, fallbackFiles, executionPath, &fallbackExec)
	if nativeExec.Execution.Outcome != fallbackExec.Execution.Outcome || nativeExec.RecordKey != fallbackExec.RecordKey ||
		nativeExec.Provenance.Plan != fallbackExec.Provenance.Plan {
		t.Fatalf("execution record depends on the failure source:\n%#v\n%#v", nativeExec, fallbackExec)
	}
}

func TestCLIProjectRun_RuntimeNativeFailureClassificationReachesPackage(t *testing.T) {
	for _, tc := range []struct {
		name, stage string
		wantClass   recordcontract.FailureClass
		wantStage   recordcontract.FailureStage
	}{
		{"validation", "manifest_validation", recordcontract.FailureClassValidation, recordcontract.FailureStageValidation},
		{"adapter", "document_open", recordcontract.FailureClassAdapter, recordcontract.FailureStageAdapter},
		{"observation", "observation_output", recordcontract.FailureClassAdapter, recordcontract.FailureStageAdapter},
		{"export", "artifact_export", recordcontract.FailureClassExport, recordcontract.FailureStageExport},
		{"runtime", "recompute", recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime},
		{"unknown future stage", "future_stage_v9", recordcontract.FailureClassRuntime, recordcontract.FailureStageRuntime},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobals()
			chdirToTemp(t)
			setPathWithoutFreeCAD(t)
			run := runFailedProjectWithRawResult(t, writeProjectExecutionFixture(t), filepath.Join(t.TempDir(), "out"),
				nonCanonicalFailedResult(tc.stage, "native_code", "native message"), true)
			failure, _ := singleFailureRecord(t, run)
			if failure.Failure.Class != tc.wantClass || failure.Failure.Stage != tc.wantStage ||
				failure.Failure.Code != "native_code" || failure.Failure.Message != "native message" {
				t.Fatalf("failure = %#v, want %s/%s", failure.Failure, tc.wantClass, tc.wantStage)
			}
		})
	}
}

func TestCLIProjectRun_RuntimeNativeFailureIsDeterministicAcrossOutputRoots(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	raw := nonCanonicalFailedResult("artifact_export", "export_failed", "STEP export refused the document")
	first := runFailedProjectWithRawResult(t, projectDir, filepath.Join(t.TempDir(), "out-a"), raw, true)
	if err := os.RemoveAll(".cache"); err != nil {
		t.Fatal(err)
	}
	second := runFailedProjectWithRawResult(t, projectDir, filepath.Join(t.TempDir(), "out-b"), raw, true)

	filesA := readRecordPackageFiles(t, recordPackageRoot(first.result.RunRoot))
	filesB := readRecordPackageFiles(t, recordPackageRoot(second.result.RunRoot))
	assertSamePackageFileSet(t, filesA, filesB)
	for _, path := range []string{
		recordpackage.PackageManifestContractPath(),
		recordpackage.MustRecordContractPath("execution"),
		recordpackage.MustRecordContractPath("failure"),
		recordpackage.RawRuntimeResultContractPath(),
	} {
		assertPackageFileBytesEqual(t, path, filesA[path], filesB[path])
	}
	failureA, failureB := readCLIFailureRecord(t, recordPackageRoot(first.result.RunRoot)), readCLIFailureRecord(t, recordPackageRoot(second.result.RunRoot))
	if failureA.Identity != failureB.Identity {
		t.Fatalf("failure identities differ: %#v vs %#v", failureA.Identity, failureB.Identity)
	}
	if !reflect.DeepEqual(readCLIRecordPackageManifest(t, recordPackageRoot(first.result.RunRoot)).Records, readCLIRecordPackageManifest(t, recordPackageRoot(second.result.RunRoot)).Records) {
		t.Fatal("manifest record entries differ")
	}
}

// Report fallback: with no valid, aligned native failure the established
// report-derived failure record stays authoritative and is never replaced by a
// synthesized native one.
func TestCLIProjectRun_ReportDerivedFailureRemainsWithoutApplicableNativeFailure(t *testing.T) {
	type runner func(t *testing.T, projectDir, outDir string) failedCLIProjectExecution
	withRaw := func(raw string, set bool) runner {
		return func(t *testing.T, projectDir, outDir string) failedCLIProjectExecution {
			return runFailedProjectWithRawResult(t, projectDir, outDir, raw, set)
		}
	}
	withMode := func(mode string) runner {
		return func(t *testing.T, projectDir, outDir string) failedCLIProjectExecution {
			return runFailedProjectWithMode(t, projectDir, outDir, mode)
		}
	}
	for _, tc := range []struct {
		name string
		run  runner
	}{
		{"malformed result", withRaw("{", true)},
		{"empty result", withRaw("", true)},
		{"missing result", withRaw("", false)},
		{"failure without the required stage", withRaw(nonCanonicalFailedResult("", "native_code", "native message"), true)},
		{"failed status without failure body", withRaw(`{"schemaVersion":"1.0","status":"failed"}`, true)},
		{"unsupported schema version", withRaw(`{"schemaVersion":"9.9","status":"failed","failure":{"boundary":"freecad","category":"runtime","code":"c","message":"m"}}`, true)},
		{"unknown result field", withRaw(`{"schemaVersion":"1.0","status":"failed","surprise":1,"failure":{"boundary":"freecad","category":"runtime","code":"c","message":"m"}}`, true)},
		{"failure without message", withRaw(`{"schemaVersion":"1.0","status":"failed","failure":{"boundary":"freecad","category":"runtime","code":"c","message":""}}`, true)},
		{"invalid observed evidence", withMode("observed_mismatch")},
		{"succeeded status reported with failing exit", withRaw(`{"schemaVersion":"1.0","status":"succeeded","artifacts":[]}`, true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobals()
			chdirToTemp(t)
			setPathWithoutFreeCAD(t)
			run := tc.run(t, writeProjectExecutionFixture(t), filepath.Join(t.TempDir(), "out"))

			if outcome := lastCADOutcome(run.result.Execution); outcome != nil && outcome.Failure != nil && outcome.Failure.RuntimeNative {
				t.Fatalf("invalid evidence was consumed as a native failure: %#v", outcome.Failure)
			}
			failure, files := singleFailureRecord(t, run)
			assertReportDerivedFailure(t, run, failure, files)
		})
	}
}

// lastCADOutcome returns the CAD outcome of the (single) job, if any.
func lastCADOutcome(execution scheduler.ExecutionResult) *executor.CADRuntimeOutcome {
	var out *executor.CADRuntimeOutcome
	for _, job := range execution.Jobs {
		if job.CADRuntimeOutcome != nil {
			out = job.CADRuntimeOutcome
		}
	}
	return out
}

// assertReportDerivedFailure checks failure is exactly the record the report
// mapper derives: report evidence only and the report's classification.
func assertReportDerivedFailure(t *testing.T, run failedCLIProjectExecution, failure recordcontract.FailureRecord, files map[string][]byte) {
	t.Helper()
	if run.result.Report.Error == nil {
		t.Fatal("failed run has no report error")
	}
	rep := run.result.Report.Error
	f := failure.Failure
	if failure.RecordKey != "engine-run:"+run.planned.PlanHash+":failure" {
		t.Fatalf("failure recordKey = %q", failure.RecordKey)
	}
	if len(f.Evidence) != 1 || f.Evidence[0].SourceKind != "report" || f.Evidence[0].SourceRef != recordpackage.RawReportContractPath() || f.Evidence[0].DigestSHA256 != "" {
		t.Fatalf("failure evidence = %#v, want only the raw report", f.Evidence)
	}
	if _, ok := evidenceRefDigest(failure.Provenance.Evidence, "runtime-result", recordpackage.RawRuntimeResultContractPath()); ok {
		t.Fatalf("report-derived failure provenance cites a runtime result: %#v", failure.Provenance.Evidence)
	}
	if f.Message != rep.Message || f.Linkage.ProductKey != rep.ProductID || f.Linkage.StepRef != rep.StepID || f.RetryCount != rep.RetryCount {
		t.Fatalf("failure %#v does not carry report error %#v", f, rep)
	}
	if _, ok := files[recordpackage.RawReportContractPath()]; !ok {
		t.Fatal("raw report missing")
	}
}

func TestCLIProjectRun_VerificationMismatchIsNotRuntimeNativeFailure(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	// The controlled runtime reports suppression targets as not suppressed
	// while the Engine-derived expectation is suppressed.
	run := runFailedProjectWithMode(t, writeTask13TargetMutationHandoffFixture(t), filepath.Join(t.TempDir(), "out"), "verification_mismatch")
	var consumptionErr *executor.CADRuntimeConsumptionError
	if !errors.As(run.err, &consumptionErr) || consumptionErr.Stage != executor.CADRuntimeConsumptionStageVerificationFailure {
		t.Fatalf("error = %v, want verification_failure consumption error", run.err)
	}

	outcome := failedCADOutcome(t, run.result.Execution)
	// The runtime itself succeeded; Engine verification failed.
	result, err := os.ReadFile(outcome.ResultPath)
	if err != nil || !bytes.Contains(result, []byte(`"status":"succeeded"`)) {
		t.Fatalf("runtime result = %q (err=%v), want a succeeded status", result, err)
	}
	if outcome.Verification != artifact.VerificationOutcomeFailed || outcome.Failure.RuntimeNative ||
		outcome.Failure.Class != "" || outcome.Failure.SemanticStage != "" || outcome.Failure.Category != "verification" {
		t.Fatalf("outcome = verification %q failure %#v", outcome.Verification, outcome.Failure)
	}

	failure, files := singleFailureRecord(t, run)
	assertReportDerivedFailure(t, run, failure, files)
	// The established report-derived semantics: the code is the report's
	// verification classification, not a native runtime failure code.
	classification := string(run.result.Report.Error.Classification)
	if classification == "" || failure.Failure.Code != classification || outcome.VerificationClass == "" ||
		string(outcome.VerificationClass) != classification {
		t.Fatalf("failure code = %q, report classification = %q, outcome class = %q", failure.Failure.Code, classification, outcome.VerificationClass)
	}
	// The generic runtime-failure record was not used: its code and evidence are absent.
	if hasFailureRecordEvidence(failure, "runtime-result", recordpackage.RawRuntimeResultContractPath()) {
		t.Fatal("verification failure cites runtime-result evidence")
	}
}

// --- candidate correlation (cadRuntimeFailureRunEvidence) ---

type failureCandidate struct {
	jobID, product, step string
	result               string
	err                  error
	native               bool
	noResultFile         bool
}

func writeCandidateOutcome(t *testing.T, c failureCandidate) scheduler.JobExecution {
	t.Helper()
	working := t.TempDir()
	resultPath := filepath.Join(working, "prm.result.json")
	if !c.noResultFile {
		if err := os.WriteFile(resultPath, []byte(c.result), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outcome := &executor.CADRuntimeOutcome{
		JobID: c.jobID, ProductKey: c.product, StepID: c.step, WorkingCopyDir: working, ResultPath: resultPath,
		Failure: &executor.CADRuntimeFailureOutcome{
			RuntimeNative: c.native, Class: "runtime", SemanticStage: "runtime", Code: "code-" + c.jobID, Message: "message-" + c.jobID,
		},
	}
	if !c.native {
		outcome.Failure.Class, outcome.Failure.SemanticStage = "", ""
	}
	return scheduler.JobExecution{Err: c.err, CADRuntimeOutcome: outcome}
}

func failedReportFor(product, step string, jobs ...report.JobReport) report.Report {
	return report.Report{
		Status: report.StatusFailed, Jobs: jobs,
		Error: &report.ErrorSummary{Message: "boom", ProductID: product, StepID: step},
	}
}

func candidateJobs(t *testing.T, candidates ...failureCandidate) []scheduler.JobExecution {
	t.Helper()
	out := make([]scheduler.JobExecution, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, writeCandidateOutcome(t, c))
	}
	return out
}

func reverseJobs(in []scheduler.JobExecution) []scheduler.JobExecution {
	out := make([]scheduler.JobExecution, len(in))
	for i, job := range in {
		out[len(in)-1-i] = job
	}
	return out
}

func TestCADRuntimeFailureRunEvidence_CandidateCorrelation(t *testing.T) {
	opErr := errors.New("execution failed")
	native := func(job, product, step string) failureCandidate {
		return failureCandidate{jobID: job, product: product, step: step, result: "raw-" + job + "\n", err: opErr, native: true}
	}
	for _, tc := range []struct {
		name       string
		candidates []failureCandidate
		report     report.Report
		wantJob    string // "" means report fallback (no evidence)
	}{
		{
			name:       "exactly one matching failure",
			candidates: []failureCandidate{native("job-a", "widget", "2"), native("job-b", "gadget", "2")},
			report:     failedReportFor("widget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}, report.JobReport{JobID: "job-b", ProductKey: "gadget"}),
			wantJob:    "job-a",
		},
		{
			name:       "no candidate matches the reported product",
			candidates: []failureCandidate{native("job-a", "widget", "2")},
			report:     failedReportFor("gadget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}),
		},
		{
			name:       "no candidate matches the reported step",
			candidates: []failureCandidate{native("job-a", "widget", "2")},
			report:     failedReportFor("widget", "5", report.JobReport{JobID: "job-a", ProductKey: "widget"}),
		},
		{
			name:       "no candidate at all",
			candidates: nil,
			report:     failedReportFor("widget", "2"),
		},
		{
			name:       "two matching failures for the same product and step are ambiguous",
			candidates: []failureCandidate{native("job-a", "widget", "2"), native("job-b", "widget", "2")},
			report:     failedReportFor("widget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}, report.JobReport{JobID: "job-b", ProductKey: "widget"}),
		},
		{
			name:       "report job id disambiguates candidates of one product",
			candidates: []failureCandidate{native("job-a", "widget", "2"), native("job-b", "widget", "2")},
			report:     failedReportFor("widget", "2", report.JobReport{JobID: "job-b", ProductKey: "widget"}),
			wantJob:    "job-b",
		},
		{
			name: "candidate without an execution error is ignored",
			candidates: []failureCandidate{
				{jobID: "job-a", product: "widget", step: "2", result: "raw-job-a\n", native: true},
			},
			report: failedReportFor("widget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}),
		},
		{
			name: "non-native failure is never a candidate",
			candidates: []failureCandidate{
				{jobID: "job-a", product: "widget", step: "2", result: "raw-job-a\n", err: opErr, native: false},
			},
			report: failedReportFor("widget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}),
		},
		{
			name: "non-native failure does not make a native match ambiguous",
			candidates: []failureCandidate{
				native("job-a", "widget", "2"),
				{jobID: "job-b", product: "widget", step: "2", result: "raw-job-b\n", err: opErr, native: false},
			},
			report:  failedReportFor("widget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}, report.JobReport{JobID: "job-b", ProductKey: "widget"}),
			wantJob: "job-a",
		},
		{
			name: "missing result file falls back",
			candidates: []failureCandidate{
				{jobID: "job-a", product: "widget", step: "2", err: opErr, native: true, noResultFile: true},
			},
			report: failedReportFor("widget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}),
		},
		{
			name: "empty result file falls back",
			candidates: []failureCandidate{
				{jobID: "job-a", product: "widget", step: "2", err: opErr, native: true, result: ""},
			},
			report: failedReportFor("widget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}),
		},
		{
			name:       "successful report never yields native evidence",
			candidates: []failureCandidate{native("job-a", "widget", "2")},
			report:     report.Report{Status: report.StatusSuccess},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobs := candidateJobs(t, tc.candidates...)
			for _, order := range [][]scheduler.JobExecution{jobs, reverseJobs(jobs)} {
				got, err := cadRuntimeFailureRunEvidence(scheduler.ExecutionResult{Jobs: order}, tc.report)
				if err != nil {
					t.Fatalf("cadRuntimeFailureRunEvidence: %v", err)
				}
				if tc.wantJob == "" {
					if got != nil {
						t.Fatalf("evidence = %#v, want report fallback (nil)", got)
					}
					continue
				}
				if got == nil || got.Failure == nil || got.JobID != tc.wantJob || got.Failure.Code != "code-"+tc.wantJob ||
					string(got.Result) != "raw-"+tc.wantJob+"\n" || !got.Failure.RuntimeNative {
					t.Fatalf("evidence = %#v, want job %q", got, tc.wantJob)
				}
				if got.ProductKey == "" || got.StepRef == "" {
					t.Fatalf("evidence linkage incomplete: %#v", got)
				}
				if got.Observed != nil || got.Verification != nil || got.ObservedValue != nil || got.VerificationResult != nil {
					t.Fatalf("failure evidence carries success-only material: %#v", got)
				}
			}
		})
	}
}

// The evidence's failure is a copy: mutating it cannot change the outcome the
// scheduler retained.
func TestCADRuntimeFailureRunEvidence_FailureIsCopied(t *testing.T) {
	jobs := candidateJobs(t, failureCandidate{jobID: "job-a", product: "widget", step: "2", result: "raw\n", err: errors.New("x"), native: true})
	got, err := cadRuntimeFailureRunEvidence(scheduler.ExecutionResult{Jobs: jobs},
		failedReportFor("widget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}))
	if err != nil || got == nil {
		t.Fatalf("evidence=%#v err=%v", got, err)
	}
	got.Failure.Code = "mutated"
	if jobs[0].CADRuntimeOutcome.Failure.Code != "code-job-a" {
		t.Fatal("evidence aliases the scheduler outcome failure")
	}
}

// Symlinked or otherwise unsafe result paths are refused rather than read.
func TestCADRuntimeFailureRunEvidence_UnsafeResultPathIsAnError(t *testing.T) {
	working := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(working, "prm.result.json")
	if err := os.Symlink(target, resultPath); err != nil {
		t.Fatal(err)
	}
	jobs := []scheduler.JobExecution{{Err: errors.New("x"), CADRuntimeOutcome: &executor.CADRuntimeOutcome{
		JobID: "job-a", ProductKey: "widget", StepID: "2", WorkingCopyDir: working, ResultPath: resultPath,
		Failure: &executor.CADRuntimeFailureOutcome{RuntimeNative: true, Class: "runtime", SemanticStage: "runtime", Code: "c", Message: "m"},
	}}}
	got, err := cadRuntimeFailureRunEvidence(scheduler.ExecutionResult{Jobs: jobs},
		failedReportFor("widget", "2", report.JobReport{JobID: "job-a", ProductKey: "widget"}))
	if err == nil || got != nil {
		t.Fatalf("evidence=%#v err=%v, want a read error and no evidence", got, err)
	}
}

// A package-emission problem on an already-failed run is logged, never
// substituted for the operational execution error.
func TestCLIProjectRun_EmissionFailureDoesNotReplaceOriginalExecutionError(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeProjectExecutionFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	planned, err := loadPlannedRun(projectDir, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("loadPlannedRun: %v", err)
	}
	// A regular file where the package directory belongs makes emission fail.
	runRoot := planner.BuildRunRoot(outDir, planned.PlanHash)
	if err := os.MkdirAll(runRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	installControlledAlignedRuntime(t, "failure_raw")
	t.Setenv("PARAMETRON_TASK13_RUNTIME_RAW_RESULT", nonCanonicalFailedResult("artifact_export", "export_failed", "STEP export refused the document"))
	resetGlobals()
	result, err := executePlanRun(executionOptions{Planned: planned, OutputDir: outDir, UseCache: true})
	if result == nil || err == nil {
		t.Fatalf("result=%v err=%v, want a failed run", result, err)
	}
	if info, statErr := os.Stat(blocker); statErr != nil || info.IsDir() {
		t.Fatalf("emission unexpectedly succeeded despite the blocked package path (stat err=%v)", statErr)
	}
	run := failedCLIProjectExecution{result: result, planned: planned, err: err}
	assertOriginalOperationalError(t, run, "export_failed")
}
