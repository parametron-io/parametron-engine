package recordemit_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/metadata"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordmap"
	"parametron/internal/engine/recordpackage"
	"parametron/internal/engine/report"
	"parametron/internal/engine/verification"
)

type emitManifest struct {
	PackageKey  string
	Records     []emitManifestRecord
	RawEvidence []emitManifestRawEvidence `json:"rawEvidence,omitempty"`
}

type emitManifestRecord struct {
	Family       string `json:"family"`
	ContractPath string `json:"contractPath"`
	RecordKey    string `json:"recordKey"`
	IdentityID   string `json:"identityId"`
}

type emitManifestRawEvidence struct {
	ContractPath string `json:"contractPath"`
}

func TestEmitRunPackageIncludesNormalizedFailureRecordFromReportFailure(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "stable-failure-plan"
	rep := syntheticFailedReport(planHash)
	fallbackReportBytes := mustMarshalJSON(t, rep)

	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   rep,
	}); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}

	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
	assertEmitFileExists(t, packageRoot, recordpackage.MustRecordContractPath("execution"))
	assertEmitFileExists(t, packageRoot, recordpackage.MustRecordContractPath("failure"))
	assertEmitFileExists(t, packageRoot, recordpackage.RawReportContractPath())

	manifest := readEmitManifest(t, packageRoot)
	wantPackageKey := "engine-run:" + planHash
	wantFailureKey := wantPackageKey + ":failure"
	if manifest.PackageKey != wantPackageKey {
		t.Fatalf("packageKey = %q, want %q", manifest.PackageKey, wantPackageKey)
	}
	assertEmitManifestRecord(t, manifest, "failure", recordpackage.MustRecordContractPath("failure"), wantFailureKey)
	assertEmitRawEvidence(t, manifest, recordpackage.RawReportContractPath())

	failureBytes := readEmitPackageFile(t, packageRoot, recordpackage.MustRecordContractPath("failure"))
	var failure recordcontract.FailureRecord
	if err := json.Unmarshal(failureBytes, &failure); err != nil {
		t.Fatalf("unmarshal failure record: %v\n%s", err, string(failureBytes))
	}
	if err := recordcontract.ValidateFailureRecord(failure); err != nil {
		t.Fatalf("ValidateFailureRecord(emitted) returned error: %v", err)
	}
	if failure.RecordKey != wantFailureKey {
		t.Fatalf("failure recordKey = %q, want %q", failure.RecordKey, wantFailureKey)
	}
	if failure.Failure.Code == "" {
		t.Fatalf("failure code is empty in %#v", failure.Failure)
	}
	if !emitFailureHasEvidence(failure, "report", recordpackage.RawReportContractPath()) {
		t.Fatalf("failure evidence = %#v, want raw report evidence", failure.Failure.Evidence)
	}

	rawReportBytes := readEmitPackageFile(t, packageRoot, recordpackage.RawReportContractPath())
	if !bytes.Equal(rawReportBytes, fallbackReportBytes) {
		t.Fatalf("raw report fallback bytes differ\nwant:\n%s\ngot:\n%s", string(fallbackReportBytes), string(rawReportBytes))
	}
	if bytes.Equal(rawReportBytes, failureBytes) {
		t.Fatal("raw report evidence should not be the normalized failure record payload")
	}
}

func syntheticFailedReport(planHash string) report.Report {
	started := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	ended := started.Add(1500 * time.Millisecond)
	stepStarted := started.Add(250 * time.Millisecond)
	stepEnded := started.Add(1250 * time.Millisecond)
	stepDuration := int64(1000)

	return report.Report{
		SchemaVersion: report.SchemaVersion,
		Status:        report.StatusFailed,
		PlanHash:      planHash,
		Runtime: report.Runtime{
			StartedAt:          started,
			EndedAt:            ended,
			DurationMs:         1500,
			WorkerCount:        1,
			StepTimeoutSeconds: 30,
			MaxRetries:         2,
		},
		Jobs: []report.JobReport{
			{
				JobID:      "job-widget",
				ProductKey: "widget",
				Steps: []report.StepReport{
					{
						Index:      0,
						Type:       planner.StepRunCADRuntime,
						Status:     report.StepStatusFailed,
						Attempts:   3,
						StartedAt:  &stepStarted,
						EndedAt:    &stepEnded,
						DurationMs: &stepDuration,
					},
				},
			},
		},
		Error: &report.ErrorSummary{
			Message:        "observed parameter mismatch",
			Classification: verification.FailureClassParameterMismatch,
			ProductID:      "widget",
			StepID:         "0:RunCADRuntime",
			RetryCount:     2,
		},
	}
}

func assertEmitFileExists(t *testing.T, packageRoot string, contractPath string) {
	t.Helper()

	info, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(contractPath)))
	if err != nil {
		t.Fatalf("expected package file %q: %v", contractPath, err)
	}
	if info.IsDir() {
		t.Fatalf("expected package path %q to be a file", contractPath)
	}
}

func readEmitManifest(t *testing.T, packageRoot string) emitManifest {
	t.Helper()

	data := readEmitPackageFile(t, packageRoot, recordpackage.PackageManifestContractPath())
	var manifest emitManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal package manifest: %v\n%s", err, string(data))
	}
	return manifest
}

func readEmitPackageFile(t *testing.T, packageRoot string, contractPath string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(packageRoot, filepath.FromSlash(contractPath)))
	if err != nil {
		t.Fatalf("read package file %q: %v", contractPath, err)
	}
	return data
}

func assertEmitManifestRecord(t *testing.T, manifest emitManifest, family string, contractPath string, recordKey string) {
	t.Helper()

	for _, record := range manifest.Records {
		if record.Family == family && record.ContractPath == contractPath && record.RecordKey == recordKey && record.IdentityID != "" {
			return
		}
	}
	t.Fatalf("manifest missing record family=%q contractPath=%q recordKey=%q in %#v", family, contractPath, recordKey, manifest.Records)
}

func assertEmitRawEvidence(t *testing.T, manifest emitManifest, contractPath string) {
	t.Helper()

	for _, evidence := range manifest.RawEvidence {
		if evidence.ContractPath == contractPath {
			return
		}
	}
	t.Fatalf("manifest missing raw evidence %q in %#v", contractPath, manifest.RawEvidence)
}

func emitFailureHasEvidence(record recordcontract.FailureRecord, sourceKind string, sourceRef string) bool {
	for _, evidence := range record.Failure.Evidence {
		if evidence.SourceKind == sourceKind && evidence.SourceRef == sourceRef {
			return true
		}
	}
	return false
}

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return append(data, '\n')
}

// --- Reference traversal evidence integration ---

func syntheticSuccessReport(planHash string) report.Report {
	started := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	return report.Report{
		SchemaVersion: report.SchemaVersion,
		Status:        report.StatusSuccess,
		PlanHash:      planHash,
		Runtime:       report.Runtime{StartedAt: started, EndedAt: started, WorkerCount: 1},
	}
}

func syntheticMetadata(planHash string) *metadata.Metadata {
	return &metadata.Metadata{SchemaVersion: "1.0", PlanHash: planHash}
}

// referenceTraversalJSON builds a deliberately non-canonically-formatted
// schema-2 traversal payload with the supplied edges body, so tests can
// prove recordemit hashes/preserves the exact raw bytes rather than a
// re-marshalled form.
func referenceTraversalJSON(edges string) []byte {
	return []byte(fmt.Sprintf(`{
  "schemaVersion":   "2.0",
  "kind": "reference-traversal",
  "boundary": "internal",
  "operation":    "resolve",
  "status": "succeeded",
  "sourceDocument": "Widget.FCStd",
  "nodes": [
    {"sequence": 0, "id": "n1", "kind": "document", "state": "resolved", "documentPath": "Widget.FCStd"},
    {"sequence": 1, "id": "n2", "kind": "object", "state": "resolved", "documentPath": "Widget.FCStd", "objectName": "Body"}
  ],
  "edges": [%s],
  "diagnostics": []
}
`, edges))
}

func validReferenceTraversalEdgeJSON() string {
	return `{"sequence": 0, "source": "n1", "target": "n2", "kind": "document_internal_reference", "state": "resolved"}`
}

func TestEmitRunPackage_ReferenceTraversal_ProducesValidatedRecordAndRawEvidence(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "traversal-plan"
	raw := referenceTraversalJSON(validReferenceTraversalEdgeJSON())

	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		Metadata: syntheticMetadata(planHash),
		ReferenceTraversal: &recordemit.ReferenceTraversalRunEvidence{
			Content: raw, JobID: "job-1", ProductKey: "widget", StepRef: "2",
		},
	}); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}

	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)

	// Existing normal package surfaces remain present: traversal integration is additive.
	assertEmitFileExists(t, packageRoot, recordpackage.MustRecordContractPath("execution"))
	assertEmitFileExists(t, packageRoot, recordpackage.RawReportContractPath())

	// Canonical raw traversal evidence path, byte-identical to the input.
	assertEmitFileExists(t, packageRoot, recordpackage.RawRuntimeReferenceTraversalContractPath())
	rawBytes := readEmitPackageFile(t, packageRoot, recordpackage.RawRuntimeReferenceTraversalContractPath())
	if !bytes.Equal(rawBytes, raw) {
		t.Fatalf("raw traversal evidence bytes differ\nwant:\n%s\ngot:\n%s", raw, rawBytes)
	}

	manifest := readEmitManifest(t, packageRoot)
	assertEmitRawEvidence(t, manifest, recordpackage.RawRuntimeReferenceTraversalContractPath())

	wantRecordKey := "engine-run:" + planHash + ":reference"
	assertEmitManifestRecord(t, manifest, "reference", recordpackage.MustRecordContractPath("reference"), wantRecordKey)

	referenceBytes := readEmitPackageFile(t, packageRoot, recordpackage.MustRecordContractPath("reference"))
	var referenceRecord recordcontract.ReferenceRecord
	if err := json.Unmarshal(referenceBytes, &referenceRecord); err != nil {
		t.Fatalf("unmarshal reference record: %v\n%s", err, string(referenceBytes))
	}
	if err := recordcontract.ValidateReferenceRecord(referenceRecord); err != nil {
		t.Fatalf("ValidateReferenceRecord(emitted) returned error: %v", err)
	}
	if referenceRecord.RecordKey != wantRecordKey {
		t.Fatalf("reference recordKey = %q, want %q", referenceRecord.RecordKey, wantRecordKey)
	}
	if len(referenceRecord.Reference.Edges) != 1 {
		t.Fatalf("expected exactly one normalized edge, got %#v", referenceRecord.Reference.Edges)
	}
	edge := referenceRecord.Reference.Edges[0]
	if edge.Linkage.JobID != "job-1" || edge.Linkage.ProductKey != "widget" || edge.Linkage.StepRef != "2" {
		t.Fatalf("edge linkage = %#v, want Engine-owned job/product/step", edge.Linkage)
	}
	wantDigest := fmt.Sprintf("%x", sha256.Sum256(raw))
	if edge.Evidence.DigestSHA256 != wantDigest {
		t.Fatalf("edge evidence digest = %q, want sha256(rawBytes) = %q", edge.Evidence.DigestSHA256, wantDigest)
	}
	if edge.Evidence.SourceRef != recordpackage.RawRuntimeReferenceTraversalContractPath() {
		t.Fatalf("edge evidence sourceRef = %q, want canonical raw evidence path", edge.Evidence.SourceRef)
	}

	// Base (metadata-derived) provenance is retained: plan hash linkage survives.
	if referenceRecord.Provenance.Plan.PlanHash != planHash {
		t.Fatalf("provenance.Plan.PlanHash = %q, want %q", referenceRecord.Provenance.Plan.PlanHash, planHash)
	}
	foundTraversalEvidence := false
	for _, ev := range referenceRecord.Provenance.Evidence {
		if ev.Kind == "reference-traversal" && ev.Ref == recordpackage.RawRuntimeReferenceTraversalContractPath() {
			foundTraversalEvidence = true
			if ev.DigestSHA256 != wantDigest {
				t.Fatalf("provenance evidence digest = %q, want %q", ev.DigestSHA256, wantDigest)
			}
		}
	}
	if !foundTraversalEvidence {
		t.Fatalf("provenance evidence missing traversal reference: %#v", referenceRecord.Provenance.Evidence)
	}

	// Normalized identity carries the mapped document path/object name (by
	// design), but never the raw traversal node IDs or attempt/runtime paths.
	blob, err := json.Marshal(referenceRecord)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"\"n1\"", "\"n2\"", "_working", runRoot} {
		if bytes.Contains(blob, []byte(forbidden)) {
			t.Fatalf("normalized reference record leaks raw traversal/runtime identity material %q:\n%s", forbidden, blob)
		}
	}
}

func TestEmitRunPackage_ReferenceTraversal_ZeroEdgesOmitsRecordButKeepsRawEvidence(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "traversal-zero-edges"
	raw := referenceTraversalJSON("")

	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		ReferenceTraversal: &recordemit.ReferenceTraversalRunEvidence{
			Content: raw, JobID: "job-1", ProductKey: "widget", StepRef: "2",
		},
	}); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}

	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
	assertEmitFileExists(t, packageRoot, recordpackage.RawRuntimeReferenceTraversalContractPath())
	if _, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(recordpackage.MustRecordContractPath("reference")))); !os.IsNotExist(err) {
		t.Fatalf("expected no reference record file for zero-edge traversal, stat err=%v", err)
	}

	manifest := readEmitManifest(t, packageRoot)
	assertEmitRawEvidence(t, manifest, recordpackage.RawRuntimeReferenceTraversalContractPath())
	for _, record := range manifest.Records {
		if record.Family == "reference" {
			t.Fatalf("manifest indexes a reference record for zero-edge traversal: %#v", record)
		}
	}
}

func TestEmitRunPackage_ReferenceTraversal_MalformedJSONFailsClosed(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "traversal-malformed"

	err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		ReferenceTraversal: &recordemit.ReferenceTraversalRunEvidence{
			Content: []byte(`{not valid json`), JobID: "job-1", ProductKey: "widget", StepRef: "2",
		},
	})
	if err == nil {
		t.Fatal("expected an error for malformed traversal JSON")
	}
	if _, statErr := os.Stat(filepath.Join(runRoot, recordpackage.PackageDirectoryName)); !os.IsNotExist(statErr) {
		t.Fatalf("expected no package to be written on malformed traversal failure, stat err=%v", statErr)
	}
}

func TestEmitRunPackage_ReferenceTraversal_UnknownFieldRejected(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "traversal-unknown-field"
	raw := []byte(`{"schemaVersion":"2.0","kind":"reference-traversal","boundary":"internal","operation":"resolve","status":"succeeded","sourceDocument":"Widget.FCStd","nodes":[],"edges":[],"diagnostics":[],"unexpectedField":true}`)

	err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		ReferenceTraversal: &recordemit.ReferenceTraversalRunEvidence{
			Content: raw, JobID: "job-1", ProductKey: "widget", StepRef: "2",
		},
	})
	if err == nil {
		t.Fatal("expected an error for a traversal payload with an unknown field")
	}
}

func TestEmitRunPackage_ReferenceTraversal_TrailingJSONRejected(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "traversal-trailing"
	valid := `{"schemaVersion":"2.0","kind":"reference-traversal","boundary":"internal","operation":"resolve","status":"succeeded","sourceDocument":"Widget.FCStd","nodes":[],"edges":[],"diagnostics":[]}`
	raw := []byte(valid + "\ntrue\n")

	err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		ReferenceTraversal: &recordemit.ReferenceTraversalRunEvidence{
			Content: raw, JobID: "job-1", ProductKey: "widget", StepRef: "2",
		},
	})
	if err == nil {
		t.Fatal("expected an error for a traversal payload with a trailing JSON value")
	}
}

func TestEmitRunPackage_ReferenceTraversal_MapperInvalidPayloadFailsClosed(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "traversal-mapper-invalid"
	// A syntactically valid, strictly-decodable payload with an unsupported
	// schema version violates the mapper contract, not the JSON decoder.
	raw := []byte(`{"schemaVersion":"1.0","kind":"reference-traversal","boundary":"internal","operation":"resolve","status":"succeeded","sourceDocument":"Widget.FCStd","nodes":[],"edges":[],"diagnostics":[]}`)

	err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		ReferenceTraversal: &recordemit.ReferenceTraversalRunEvidence{
			Content: raw, JobID: "job-1", ProductKey: "widget", StepRef: "2",
		},
	})
	if err == nil {
		t.Fatal("expected an error for a mapper-invalid traversal payload")
	}
	if !errors.Is(err, recordmap.ErrInvalidReferenceTraversalMapping) {
		t.Fatalf("error = %v, want errors.Is(..., recordmap.ErrInvalidReferenceTraversalMapping)", err)
	}
	if _, statErr := os.Stat(filepath.Join(runRoot, recordpackage.PackageDirectoryName)); !os.IsNotExist(statErr) {
		t.Fatalf("expected no package to be written on mapper-invalid traversal failure, stat err=%v", statErr)
	}
}

// --- CAD runtime evidence (result / verification-request / observed) integration ---

// nonCanonicalCADEvidenceJSON builds deliberately non-canonically formatted
// JSON (whitespace, indentation, key ordering) so tests can prove recordemit
// preserves the exact raw attempt-evidence bytes rather than re-serializing.
func nonCanonicalCADEvidenceJSON(marker string) []byte {
	return []byte(fmt.Sprintf("{\n  \"marker\":   %q,\n \"schemaVersion\": \"1.0\"\n}\n", marker))
}

func TestEmitRunPackage_CADRuntime_CapturesResultVerificationObservedWithExactBytes(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "cad-runtime-plan"
	result := nonCanonicalCADEvidenceJSON("result")
	verification := nonCanonicalCADEvidenceJSON("verification")
	observed := nonCanonicalCADEvidenceJSON("observed")

	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		Metadata: syntheticMetadata(planHash),
		CADRuntime: &recordemit.CADRuntimeRunEvidence{
			Result: result, Verification: verification, Observed: observed,
		},
	}); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}

	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)

	for contractPath, want := range map[string][]byte{
		recordpackage.RawRuntimeResultContractPath(): result,
		recordpackage.RawVerificationContractPath():  verification,
		recordpackage.RawObservedContractPath():      observed,
	} {
		assertEmitFileExists(t, packageRoot, contractPath)
		got := readEmitPackageFile(t, packageRoot, contractPath)
		if !bytes.Equal(got, want) {
			t.Fatalf("raw evidence %q bytes differ\nwant:\n%s\ngot:\n%s", contractPath, want, got)
		}
	}

	manifest := readEmitManifest(t, packageRoot)
	assertEmitRawEvidence(t, manifest, recordpackage.RawRuntimeResultContractPath())
	assertEmitRawEvidence(t, manifest, recordpackage.RawVerificationContractPath())
	assertEmitRawEvidence(t, manifest, recordpackage.RawObservedContractPath())

	// Existing normal package surfaces remain present: CAD evidence capture is additive.
	assertEmitFileExists(t, packageRoot, recordpackage.MustRecordContractPath("execution"))
	assertEmitFileExists(t, packageRoot, recordpackage.RawReportContractPath())
}

func TestEmitRunPackage_CADRuntime_NilCADRuntimeOmitsAllThreeFamilies(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "cad-runtime-nil"

	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		Metadata: syntheticMetadata(planHash),
	}); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}

	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
	for _, contractPath := range []string{
		recordpackage.RawRuntimeResultContractPath(),
		recordpackage.RawVerificationContractPath(),
		recordpackage.RawObservedContractPath(),
	} {
		if _, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(contractPath))); !os.IsNotExist(err) {
			t.Fatalf("expected no %q file when CADRuntime evidence is absent, stat err=%v", contractPath, err)
		}
	}
}

func TestEmitRunPackage_CADRuntime_PartialEvidenceOmitsOnlyMissingFamilies(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "cad-runtime-partial"
	result := nonCanonicalCADEvidenceJSON("result-only")

	if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		Metadata: syntheticMetadata(planHash),
		CADRuntime: &recordemit.CADRuntimeRunEvidence{
			Result: result,
			// Verification and Observed intentionally absent, mirroring an
			// attempt whose optional evidence was never produced.
		},
	}); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}

	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
	assertEmitFileExists(t, packageRoot, recordpackage.RawRuntimeResultContractPath())
	for _, contractPath := range []string{
		recordpackage.RawVerificationContractPath(),
		recordpackage.RawObservedContractPath(),
	} {
		if _, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(contractPath))); !os.IsNotExist(err) {
			t.Fatalf("expected no %q file for absent optional evidence, stat err=%v", contractPath, err)
		}
	}
}

// TestEmitRunPackage_CADRuntime_DoesNotFallBackToRunRootGuess is the key
// regression proof for this fix: collectRawEvidence must never reconstruct
// prm.result.json / prm.verification.json / prm.observed.json by guessing a
// path directly under the run root. It plants files with those exact names
// and different bytes at the run root (the old, broken lookup location) and
// confirms the package instead contains the real attempt evidence supplied
// through CADRuntimeRunEvidence -- and nothing at all when no such evidence
// is supplied, even though the stale run-root files exist and are non-empty.
func TestEmitRunPackage_CADRuntime_DoesNotFallBackToRunRootGuess(t *testing.T) {
	staleResult := []byte(`{"marker":"stale-run-root-result"}`)
	staleVerification := []byte(`{"marker":"stale-run-root-verification"}`)
	staleObserved := []byte(`{"marker":"stale-run-root-observed"}`)
	writeStaleRunRootFiles := func(t *testing.T, runRoot string) {
		t.Helper()
		for name, content := range map[string][]byte{
			"prm.result.json":       staleResult,
			"prm.verification.json": staleVerification,
			"prm.observed.json":     staleObserved,
		} {
			if err := os.WriteFile(filepath.Join(runRoot, name), content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("real attempt evidence wins over stale run-root files", func(t *testing.T) {
		runRoot := t.TempDir()
		writeStaleRunRootFiles(t, runRoot)
		planHash := "cad-runtime-stale-vs-real"
		realResult := nonCanonicalCADEvidenceJSON("real-result")
		realVerification := nonCanonicalCADEvidenceJSON("real-verification")
		realObserved := nonCanonicalCADEvidenceJSON("real-observed")

		if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
			RunRoot:  runRoot,
			PlanHash: planHash,
			Report:   syntheticSuccessReport(planHash),
			Metadata: syntheticMetadata(planHash),
			CADRuntime: &recordemit.CADRuntimeRunEvidence{
				Result: realResult, Verification: realVerification, Observed: realObserved,
			},
		}); err != nil {
			t.Fatalf("EmitRunPackage returned error: %v", err)
		}

		packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
		for contractPath, want := range map[string][]byte{
			recordpackage.RawRuntimeResultContractPath(): realResult,
			recordpackage.RawVerificationContractPath():  realVerification,
			recordpackage.RawObservedContractPath():      realObserved,
		} {
			got := readEmitPackageFile(t, packageRoot, contractPath)
			if !bytes.Equal(got, want) {
				t.Fatalf("raw evidence %q = %q, want the real attempt evidence %q (not the stale run-root guess)", contractPath, got, want)
			}
		}
	})

	t.Run("no CADRuntime evidence means no capture even with stale run-root files present", func(t *testing.T) {
		runRoot := t.TempDir()
		writeStaleRunRootFiles(t, runRoot)
		planHash := "cad-runtime-stale-only"

		if err := recordemit.EmitRunPackage(recordemit.RunEmitInput{
			RunRoot:  runRoot,
			PlanHash: planHash,
			Report:   syntheticSuccessReport(planHash),
			Metadata: syntheticMetadata(planHash),
		}); err != nil {
			t.Fatalf("EmitRunPackage returned error: %v", err)
		}

		packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
		for _, contractPath := range []string{
			recordpackage.RawRuntimeResultContractPath(),
			recordpackage.RawVerificationContractPath(),
			recordpackage.RawObservedContractPath(),
		} {
			if _, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(contractPath))); !os.IsNotExist(err) {
				t.Fatalf("expected no %q file: the old runRoot-relative lookup must not have been used, stat err=%v", contractPath, err)
			}
		}
	})
}
