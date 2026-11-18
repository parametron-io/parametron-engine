package recordpackage_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

func outcomeScopedObservation(t *testing.T, recordKey string) recordcontract.ObservationRecord {
	t.Helper()
	record, err := recordcontract.BuildObservationRecord(recordcontract.ObservationRecordInput{
		RecordKey: recordKey,
		Observation: recordcontract.ObservationSummary{Facts: []recordcontract.ObservationFact{{
			Kind:    recordcontract.ObservationKindMetadata,
			Subject: recordcontract.ObservationSubject{Name: "subject"},
			Key:     "key",
			Value:   recordcontract.ObservationValue{Kind: "string", Raw: `"value"`},
		}}},
	})
	if err != nil {
		t.Fatalf("BuildObservationRecord(%q): %v", recordKey, err)
	}
	return record
}

func scopedObservationRecord(record recordcontract.ObservationRecord, outcomeKey string) recordpackage.Record {
	wrapped := recordpackage.ObservationRecord(record)
	wrapped.OutcomeKey = outcomeKey
	return wrapped
}

func TestOutcomeScopedRecordsUseDeterministicPathsAndAllowPluralFamilies(t *testing.T) {
	first := scopedObservationRecord(outcomeScopedObservation(t, "observation-a"), "outcome-a")
	second := scopedObservationRecord(outcomeScopedObservation(t, "observation-b"), "outcome-b")
	runRoot := t.TempDir()

	if err := recordpackage.WritePackage(recordpackage.PackageInput{
		PackageRoot: runRoot, UseCanonicalDirectoryName: true, PackageKey: "outcome-package", Records: []recordpackage.Record{second, first},
	}); err != nil {
		t.Fatalf("WritePackage returned error: %v", err)
	}
	packageRoot := filepath.Join(runRoot, recordpackage.PackageDirectoryName)
	for _, outcomeKey := range []string{"outcome-a", "outcome-b"} {
		path := filepath.Join(packageRoot, "records", "outcomes", outcomeKey, "prm.observation-record.json")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("outcome-scoped record %q missing: %v", path, err)
		}
		entry, ok := recordpackage.EntryForContractPath("records/outcomes/" + outcomeKey + "/prm.observation-record.json")
		if !ok || entry.Role != recordpackage.EntryRoleNormalizedRecord || entry.Family != recordcontract.FamilyObservation {
			t.Fatalf("scoped observation classification = %#v, %v", entry, ok)
		}
		if !recordpackage.IsNormalizedRecordContractPath(entry.ContractPath) {
			t.Fatalf("%q is not classified as a normalized record path", entry.ContractPath)
		}
	}

	manifestPath := filepath.Join(packageRoot, recordpackage.PackageManifestContractPath())
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Records []struct {
			Family       string `json:"family"`
			ContractPath string `json:"contractPath"`
		} `json:"records"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Records) != 2 ||
		manifest.Records[0].ContractPath != "records/outcomes/outcome-a/prm.observation-record.json" ||
		manifest.Records[1].ContractPath != "records/outcomes/outcome-b/prm.observation-record.json" {
		t.Fatalf("outcome-scoped manifest ordering = %#v", manifest.Records)
	}
}

func TestOutcomeScopedRecordsRejectDuplicateScopeAndUnsafeKeys(t *testing.T) {
	first := scopedObservationRecord(outcomeScopedObservation(t, "observation-a"), "outcome-a")
	second := scopedObservationRecord(outcomeScopedObservation(t, "observation-b"), "outcome-a")

	err := recordpackage.WritePackage(recordpackage.PackageInput{
		PackageRoot: t.TempDir(), PackageKey: "duplicate-scope", Records: []recordpackage.Record{first, second},
	})
	if !errors.Is(err, recordpackage.ErrInvalidRecord) || !strings.Contains(err.Error(), "duplicate family") {
		t.Fatalf("WritePackage error = %v, want duplicate family in outcome scope", err)
	}

	for _, outcomeKey := range []string{"../escape", "with/slash", ".", ".."} {
		record := scopedObservationRecord(outcomeScopedObservation(t, "observation-unsafe"), outcomeKey)
		err := recordpackage.WritePackage(recordpackage.PackageInput{
			PackageRoot: t.TempDir(), PackageKey: "unsafe-scope", Records: []recordpackage.Record{record},
		})
		if !errors.Is(err, recordpackage.ErrInvalidRecord) {
			t.Fatalf("outcome key %q error = %v, want ErrInvalidRecord", outcomeKey, err)
		}
	}
}

func TestOutcomeScopedRawEvidenceIsClassifiedAndOrdered(t *testing.T) {
	canonical := recordpackage.RawRuntimeResultContractPath()
	firstPath, err := recordpackage.OutcomeRawEvidenceContractPath("outcome-a", canonical)
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := recordpackage.OutcomeRawEvidenceContractPath("outcome-b", canonical)
	if err != nil {
		t.Fatal(err)
	}
	if firstPath != "raw/outcomes/outcome-a/runtime/prm.result.json" || secondPath != "raw/outcomes/outcome-b/runtime/prm.result.json" {
		t.Fatalf("scoped raw paths = %q, %q", firstPath, secondPath)
	}
	for _, path := range []string{firstPath, secondPath} {
		if !recordpackage.IsRawEvidenceFileContractPath(path) {
			t.Fatalf("%q is not classified as raw evidence", path)
		}
		if !recordpackage.IsRawRuntimeEvidenceContractPath(path) {
			t.Fatalf("%q is not classified as raw runtime evidence", path)
		}
		entry, ok := recordpackage.EntryForContractPath(path)
		if !ok || entry.Role != recordpackage.EntryRoleRawEvidence || entry.Kind != recordpackage.EntryKindFile {
			t.Fatalf("raw classification = %#v, %v", entry, ok)
		}
	}
	if _, err := recordpackage.OutcomeRawEvidenceContractPath("outcome-a", recordpackage.RawReportContractPath()); err == nil {
		t.Fatal("run-level report evidence accepted an outcome scope")
	}

	runRoot := t.TempDir()
	if err := recordpackage.WritePackage(recordpackage.PackageInput{
		PackageRoot: runRoot, UseCanonicalDirectoryName: true, PackageKey: "outcome-raw",
		Records: []recordpackage.Record{scopedObservationRecord(outcomeScopedObservation(t, "observation-raw"), "outcome-a")},
		RawEvidenceFiles: []recordpackage.RawEvidenceFile{
			{ContractPath: secondPath, Content: []byte("second")},
			{ContractPath: canonical, Content: []byte("canonical")},
			{ContractPath: firstPath, Content: []byte("first")},
		},
	}); err != nil {
		t.Fatalf("WritePackage returned error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(runRoot, recordpackage.PackageDirectoryName, recordpackage.PackageManifestContractPath()))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		RawEvidence []struct {
			ContractPath string `json:"contractPath"`
		} `json:"rawEvidence"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(manifest.RawEvidence))
	for _, entry := range manifest.RawEvidence {
		got = append(got, entry.ContractPath)
	}
	want := []string{canonical, firstPath, secondPath}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("raw evidence ordering = %v, want %v", got, want)
	}
}
