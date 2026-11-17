package recordemit_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"parametron/internal/engine/integrity"
	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordemit"
	"parametron/internal/engine/recordpackage"
)

const (
	emitSourceDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	emitResultDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func emitIntegrityInput(runRoot, planHash string, evidence []byte) recordemit.RunEmitInput {
	return recordemit.RunEmitInput{
		RunRoot:  runRoot,
		PlanHash: planHash,
		Report:   syntheticSuccessReport(planHash),
		SourceIntegrity: &integrity.ProjectIntegrity{
			Lock: integrity.LockStateVerified,
			Resources: []integrity.ResourceSource{{
				LogicalID:        "alpha",
				SourceCoordinate: "models/alpha.FCStd",
				SourceDigest:     integrity.Digest(emitSourceDigest),
				LockState:        integrity.LockStateVerified,
				LockedDigest:     integrity.Digest(emitSourceDigest),
			}},
		},
		CADRuntime: &recordemit.CADRuntimeRunEvidence{
			Integrity: &integrity.OperationIntegrity{Resources: []integrity.ResourceOperation{{
				LogicalID:                 "alpha",
				MaterializedCoordinate:    "source/Alpha.FCStd",
				MaterializedDigest:        integrity.Digest(emitSourceDigest),
				MaterializationStatus:     integrity.StatusVerified,
				RuntimeBaselineDigest:     integrity.Digest(emitSourceDigest),
				RuntimeBaselineStatus:     integrity.StatusVerified,
				PersistedResultCoordinate: "source/Alpha.FCStd",
				PersistedResultDigest:     integrity.Digest(emitResultDigest),
				ResultStatus:              integrity.StatusVerified,
			}}},
			IntegrityEvidence: evidence,
			JobID:             "job-alpha",
			ProductKey:        "widget",
			StepRef:           "2",
		},
	}
}

func TestEmitRunPackagePreservesRawIntegrityEvidenceVerbatim(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "integrity-evidence-plan"
	evidence := []byte("{\n  \"schemaVersion\": \"1.0\",\n  \"resources\": []\n}\n")

	if err := recordemit.EmitRunPackage(emitIntegrityInput(runRoot, planHash, evidence)); err != nil {
		t.Fatalf("EmitRunPackage returned error: %v", err)
	}

	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
	rawPath := recordpackage.RawRuntimeIntegrityEvidenceContractPath()
	if rawPath != "raw/runtime/prm.integrity-evidence.json" {
		t.Fatalf("raw integrity evidence contract path = %q, want raw/runtime/prm.integrity-evidence.json", rawPath)
	}
	raw := readEmitPackageFile(t, packageRoot, rawPath)
	if !bytes.Equal(raw, evidence) {
		t.Fatalf("raw integrity evidence = %q, want verbatim %q", raw, evidence)
	}
	manifest := readEmitManifest(t, packageRoot)
	assertEmitRawEvidence(t, manifest, rawPath)

	executionBytes := readEmitPackageFile(t, packageRoot, recordpackage.MustRecordContractPath("execution"))
	if bytes.Equal(raw, executionBytes) {
		t.Fatal("raw integrity evidence was collapsed into the normalized execution record")
	}
	for _, record := range manifest.Records {
		if record.ContractPath == rawPath {
			t.Fatalf("raw integrity evidence listed as a normalized record: %#v", record)
		}
	}

	var execution recordcontract.ExecutionRecord
	if err := json.Unmarshal(executionBytes, &execution); err != nil {
		t.Fatalf("unmarshal execution record: %v\n%s", err, executionBytes)
	}
	if execution.Provenance.Integrity == nil || len(execution.Provenance.Integrity.Resources) != 1 {
		t.Fatalf("execution record provenance = %#v, want one integrity resource", execution.Provenance.Integrity)
	}
	resource := execution.Provenance.Integrity.Resources[0]
	if resource.SourceDigestSHA256 != emitSourceDigest ||
		resource.BaselineDigestSHA256 != emitSourceDigest ||
		resource.RuntimeBaselineDigestSHA256 != emitSourceDigest ||
		resource.PersistedResultDigestSHA256 != emitResultDigest {
		t.Fatalf("provenance identities = %#v, want source/baseline/result as separate fields", resource)
	}
	if resource.PersistedResultDigestSHA256 == resource.SourceDigestSHA256 {
		t.Fatal("configured-result digest replaced the source identity in the normalized record")
	}
	foundEvidenceReference := false
	for _, evidenceRef := range execution.Provenance.Evidence {
		if evidenceRef.Kind == "integrity-evidence" && evidenceRef.Ref == rawPath {
			foundEvidenceReference = true
		}
	}
	if !foundEvidenceReference {
		t.Fatalf("execution provenance evidence = %#v, want a raw integrity evidence reference", execution.Provenance.Evidence)
	}
}

func TestEmitRunPackageIntegrityRecordsAreByteDeterministic(t *testing.T) {
	evidence := []byte("{\"schemaVersion\":\"1.0\",\"resources\":[]}\n")
	planHash := "integrity-determinism-plan"

	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	if err := recordemit.EmitRunPackage(emitIntegrityInput(firstRoot, planHash, evidence)); err != nil {
		t.Fatal(err)
	}
	if err := recordemit.EmitRunPackage(emitIntegrityInput(secondRoot, planHash, evidence)); err != nil {
		t.Fatal(err)
	}

	firstExecution := readEmitPackageFile(t, filepath.Join(firstRoot, recordpackage.PackageDirectoryName), recordpackage.MustRecordContractPath("execution"))
	secondExecution := readEmitPackageFile(t, filepath.Join(secondRoot, recordpackage.PackageDirectoryName), recordpackage.MustRecordContractPath("execution"))
	if !bytes.Equal(firstExecution, secondExecution) {
		t.Fatalf("equivalent runs produced different execution record bytes\nfirst:\n%s\nsecond:\n%s", firstExecution, secondExecution)
	}

	firstRaw := readEmitPackageFile(t, filepath.Join(firstRoot, recordpackage.PackageDirectoryName), recordpackage.RawRuntimeIntegrityEvidenceContractPath())
	secondRaw := readEmitPackageFile(t, filepath.Join(secondRoot, recordpackage.PackageDirectoryName), recordpackage.RawRuntimeIntegrityEvidenceContractPath())
	if !bytes.Equal(firstRaw, secondRaw) {
		t.Fatal("equivalent runs produced different raw integrity evidence bytes")
	}
}

func TestEmitRunPackageWithoutIntegrityLeavesProvenanceUnchanged(t *testing.T) {
	runRoot := t.TempDir()
	planHash := "no-integrity-plan"
	input := recordemit.RunEmitInput{RunRoot: runRoot, PlanHash: planHash, Report: syntheticSuccessReport(planHash)}
	if err := recordemit.EmitRunPackage(input); err != nil {
		t.Fatal(err)
	}
	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
	executionBytes := readEmitPackageFile(t, packageRoot, recordpackage.MustRecordContractPath("execution"))
	if bytes.Contains(executionBytes, []byte("\"Integrity\"")) {
		t.Fatalf("execution record without declared resources gained integrity provenance:\n%s", executionBytes)
	}
	if _, err := os.Stat(filepath.Join(packageRoot, filepath.FromSlash(recordpackage.RawRuntimeIntegrityEvidenceContractPath()))); !os.IsNotExist(err) {
		t.Fatalf("raw integrity evidence exists without integrity evidence: %v", err)
	}
}
