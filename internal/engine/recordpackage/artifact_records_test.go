package recordpackage_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

// Issue #1: artifact records are the one family that may appear more than
// once in a package. Each is addressed by its record identity:
//
//	records/artifacts/<identityId>/parametron.artifact-record.json

const artifactRecordFileName = "parametron.artifact-record.json"

func artifactRecordWithKey(t *testing.T, key string) recordcontract.ArtifactRecord {
	t.Helper()
	record, err := recordcontract.BuildArtifactRecord(recordcontract.ArtifactRecordInput{
		RecordKey:  key,
		Provenance: testProvenance("export"),
		Artifact: recordcontract.ArtifactSummary{
			Class:          recordcontract.ArtifactClassExecutionOutput,
			Type:           recordcontract.ArtifactTypeSTEP,
			Path:           "out/" + key + ".step",
			Filename:       key + ".step",
			MimeType:       "model/step",
			SizeBytes:      1024,
			ChecksumSHA256: digestA(),
			Linkage:        recordcontract.ArtifactLinkage{JobID: "job-a", ProductKey: "product-a", StepRef: "export"},
			Evidence:       recordcontract.ArtifactEvidence{SourceKind: "runtime", SourceRef: "runtime://artifact/" + key, DigestSHA256: digestB()},
		},
	})
	if err != nil {
		t.Fatalf("BuildArtifactRecord(%q) returned error: %v", key, err)
	}
	return record
}

func artifactRecordsWithKeys(t *testing.T, keys ...string) []recordcontract.ArtifactRecord {
	t.Helper()
	out := make([]recordcontract.ArtifactRecord, 0, len(keys))
	for _, key := range keys {
		out = append(out, artifactRecordWithKey(t, key))
	}
	return out
}

func artifactIdentityPath(t *testing.T, identityID string) string {
	t.Helper()
	path, err := recordpackage.ArtifactRecordContractPath(identityID)
	if err != nil {
		t.Fatalf("ArtifactRecordContractPath(%q) returned error: %v", identityID, err)
	}
	return path
}

func packageInputWithArtifacts(t *testing.T, root string, artifacts []recordcontract.ArtifactRecord) recordpackage.PackageInput {
	t.Helper()
	in := validPackageInput(t, root)
	for _, record := range artifacts {
		in.Records = append(in.Records, recordpackage.ArtifactRecord(record))
	}
	return in
}

func manifestFamilyPaths(manifest testPackageManifest, family string) []string {
	var paths []string
	for _, entry := range manifest.Records {
		if entry.Family == family {
			paths = append(paths, entry.ContractPath)
		}
	}
	return paths
}

func TestArtifactRecordContractPathIsIdentityAddressed(t *testing.T) {
	t.Parallel()

	got := artifactIdentityPath(t, "abc123")
	if want := "records/artifacts/abc123/parametron.artifact-record.json"; got != want {
		t.Fatalf("ArtifactRecordContractPath = %q, want %q", got, want)
	}
	if got := recordpackage.ArtifactRecordsDirectoryContractPath(); got != "records/artifacts" {
		t.Fatalf("ArtifactRecordsDirectoryContractPath = %q, want records/artifacts", got)
	}
	if !strings.HasPrefix(got, recordpackage.RecordsDirectoryContractPath()+"/") {
		t.Fatalf("artifact record path %q is not under %q", got, recordpackage.RecordsDirectoryContractPath())
	}

	for _, identity := range []string{"", "  ", "../escape", `a\b`, ".", ".."} {
		if _, err := recordpackage.ArtifactRecordContractPath(identity); !errors.Is(err, recordpackage.ErrInvalidLayoutPath) {
			t.Fatalf("ArtifactRecordContractPath(%q) error = %v, want ErrInvalidLayoutPath", identity, err)
		}
	}
}

func TestIsNormalizedRecordContractPathClassifiesIdentityAddressedArtifactRecords(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"records/artifacts/abc123/parametron.artifact-record.json",
		artifactIdentityPath(t, artifactRecordWithKey(t, "k").Identity.ID),
	} {
		if !recordpackage.IsNormalizedRecordContractPath(path) {
			t.Fatalf("IsNormalizedRecordContractPath(%q) = false, want true", path)
		}
	}

	for _, path := range []string{
		"records/artifacts/parametron.artifact-record.json",
		"records/artifacts/abc123/other.json",
		"records/artifacts/abc123/parametron.execution-record.json",
		"records/artifacts/abc123/nested/parametron.artifact-record.json",
		"records/artifacts/../parametron.artifact-record.json",
		"records/artifacts/abc123",
		"records/artifacts",
		"artifacts/files/abc123/parametron.artifact-record.json",
		"raw/artifact-store/manifest.json",
	} {
		if recordpackage.IsNormalizedRecordContractPath(path) {
			t.Fatalf("IsNormalizedRecordContractPath(%q) = true, want false", path)
		}
	}
}

func TestWritePackageSingleArtifactUsesIdentityAddressedPath(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "pkg")
	record := artifactRecordWithKey(t, "only-artifact")
	if err := recordpackage.WritePackage(packageInputWithArtifacts(t, root, []recordcontract.ArtifactRecord{record})); err != nil {
		t.Fatalf("WritePackage returned error: %v", err)
	}

	wantPath := "records/artifacts/" + record.Identity.ID + "/" + artifactRecordFileName
	if got := artifactIdentityPath(t, record.Identity.ID); got != wantPath {
		t.Fatalf("ArtifactRecordContractPath = %q, want %q", got, wantPath)
	}
	if !fileExists(t, filepath.Join(root, filepath.FromSlash(wantPath))) {
		t.Fatalf("identity-addressed artifact record %q was not written", wantPath)
	}
	// A single artifact must not fall back to the superseded singleton path.
	legacy := filepath.Join(root, "records", artifactRecordFileName)
	if fileExists(t, legacy) {
		t.Fatalf("superseded singleton artifact record %q was written", legacy)
	}

	var payload struct {
		Family    string `json:"family"`
		RecordKey string `json:"recordKey"`
		Identity  struct {
			ID string `json:"id"`
		} `json:"identity"`
	}
	readJSONFile(t, filepath.Join(root, filepath.FromSlash(wantPath)), &payload)
	if payload.Family != "artifact" || payload.RecordKey != record.RecordKey || payload.Identity.ID != record.Identity.ID {
		t.Fatalf("artifact record payload = %#v, want key %q identity %q", payload, record.RecordKey, record.Identity.ID)
	}
	assertJSONFileWithTrailingNewline(t, filepath.Join(root, filepath.FromSlash(wantPath)))

	manifest := readManifest(t, root)
	if got := manifestFamilyPaths(manifest, "artifact"); !reflect.DeepEqual(got, []string{wantPath}) {
		t.Fatalf("manifest artifact record paths = %#v, want %#v", got, []string{wantPath})
	}
}

func TestWritePackageMultipleArtifactRecordsAreAllWrittenAndIndexed(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "pkg")
	records := artifactRecordsWithKeys(t, "artifact-c", "artifact-a", "artifact-b")
	if err := recordpackage.WritePackage(packageInputWithArtifacts(t, root, records)); err != nil {
		t.Fatalf("WritePackage returned error: %v", err)
	}

	files := readPackageFiles(t, root)
	manifest := readManifest(t, root)

	wantByIdentity := make(map[string]string, len(records))
	for _, record := range records {
		path := artifactIdentityPath(t, record.Identity.ID)
		wantByIdentity[record.Identity.ID] = path
		if _, ok := files[path]; !ok {
			t.Fatalf("artifact record %q (%s) was not written; files = %v", record.RecordKey, path, sortedKeys(files))
		}
	}

	var gotEntries []string
	for _, entry := range manifest.Records {
		if entry.Family != "artifact" {
			continue
		}
		if want := wantByIdentity[entry.IdentityID]; entry.ContractPath != want {
			t.Fatalf("manifest artifact entry %#v path != identity-addressed %q", entry, want)
		}
		gotEntries = append(gotEntries, entry.IdentityID)
	}
	if len(gotEntries) != len(records) {
		t.Fatalf("manifest indexes %d artifact records, want %d", len(gotEntries), len(records))
	}
	if !sort.StringsAreSorted(gotEntries) {
		t.Fatalf("manifest artifact records are not ordered by identity: %v", gotEntries)
	}
	if legacy := filepath.Join(root, "records", artifactRecordFileName); fileExists(t, legacy) {
		t.Fatalf("superseded singleton artifact record %q was written", legacy)
	}
}

func TestWritePackageArtifactRecordOrderIsInputOrderIndependent(t *testing.T) {
	t.Parallel()

	keys := []string{"artifact-1", "artifact-2", "artifact-3", "artifact-4"}
	forward := artifactRecordsWithKeys(t, keys...)
	reversed := artifactRecordsWithKeys(t, keys...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	rootA := filepath.Join(t.TempDir(), "a")
	rootB := filepath.Join(t.TempDir(), "b")
	if err := recordpackage.WritePackage(packageInputWithArtifacts(t, rootA, forward)); err != nil {
		t.Fatalf("WritePackage(forward) returned error: %v", err)
	}
	if err := recordpackage.WritePackage(packageInputWithArtifacts(t, rootB, reversed)); err != nil {
		t.Fatalf("WritePackage(reversed) returned error: %v", err)
	}

	filesA, filesB := readPackageFiles(t, rootA), readPackageFiles(t, rootB)
	if !reflect.DeepEqual(sortedKeys(filesA), sortedKeys(filesB)) {
		t.Fatalf("package file sets differ: %v vs %v", sortedKeys(filesA), sortedKeys(filesB))
	}
	for path, content := range filesA {
		if !bytes.Equal(content, filesB[path]) {
			t.Fatalf("package file %q bytes differ by input order", path)
		}
	}

	var identities []string
	for _, entry := range readManifest(t, rootA).Records {
		if entry.Family == "artifact" {
			identities = append(identities, entry.IdentityID)
		}
	}
	if len(identities) != len(keys) || !sort.StringsAreSorted(identities) {
		t.Fatalf("artifact manifest identities = %v, want %d sorted by identity", identities, len(keys))
	}
}

func TestWritePackageArtifactRecordsSortWithinFamilyOrder(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "pkg")
	in := validPackageInput(t, root)
	in.Records = shuffledTestRecords(t)
	for _, record := range artifactRecordsWithKeys(t, "extra-2", "extra-1") {
		in.Records = append(in.Records, recordpackage.ArtifactRecord(record))
	}
	if err := recordpackage.WritePackage(in); err != nil {
		t.Fatalf("WritePackage returned error: %v", err)
	}

	var families []string
	for _, entry := range readManifest(t, root).Records {
		if n := len(families); n == 0 || families[n-1] != entry.Family {
			families = append(families, entry.Family)
		}
	}
	var want []string
	for _, family := range definitionFamilies() {
		want = append(want, string(family))
	}
	if !reflect.DeepEqual(families, want) {
		t.Fatalf("manifest family groups = %v, want registry order %v (artifact records contiguous)", families, want)
	}
	if got := len(manifestFamilyPaths(readManifest(t, root), "artifact")); got != 3 {
		t.Fatalf("manifest artifact record count = %d, want 3", got)
	}
}

func TestWritePackageAllowsZeroArtifactRecords(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "pkg")
	if err := recordpackage.WritePackage(validPackageInput(t, root)); err != nil {
		t.Fatalf("WritePackage returned error: %v", err)
	}
	manifest := readManifest(t, root)
	if got := manifestFamilyPaths(manifest, "artifact"); len(got) != 0 {
		t.Fatalf("manifest artifact record paths = %v, want none", got)
	}
	for path := range readPackageFiles(t, root) {
		if strings.HasSuffix(path, artifactRecordFileName) {
			t.Fatalf("unexpected artifact record file %q for a package without artifact records", path)
		}
	}
}

func TestWritePackageRejectsDuplicateArtifactIdentity(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "pkg")
	record := artifactRecordWithKey(t, "same-artifact")
	err := recordpackage.WritePackage(packageInputWithArtifacts(t, root, []recordcontract.ArtifactRecord{
		artifactRecordWithKey(t, "other-artifact"), record, record,
	}))
	if !errors.Is(err, recordpackage.ErrInvalidRecord) {
		t.Fatalf("WritePackage error = %v, want ErrInvalidRecord", err)
	}
	if !strings.Contains(err.Error(), "duplicate artifact identity") {
		t.Fatalf("WritePackage error = %q, want duplicate artifact identity", err)
	}
	if _, statErr := os.Stat(root); !os.IsNotExist(statErr) {
		t.Fatalf("rejected package must not be materialized, stat err = %v", statErr)
	}
}

// Two artifact records can only resolve to one path when they share an
// identity, because the path is derived from the (derivation-validated)
// identity. A forged identity that would alias another record's path, or
// escape the artifact directory, is rejected before anything is written.
func TestWritePackageRejectsForgedArtifactIdentities(t *testing.T) {
	t.Parallel()

	for name, identity := range map[string]string{
		"path traversal": "../outside",
		"nested":         "a/b",
		"blank":          "",
	} {
		t.Run(name, func(t *testing.T) {
			record := artifactRecordWithKey(t, "forged")
			record.Identity.ID = identity
			root := filepath.Join(t.TempDir(), "pkg")
			err := recordpackage.WritePackage(packageInputWithArtifacts(t, root, []recordcontract.ArtifactRecord{record}))
			if !errors.Is(err, recordpackage.ErrInvalidRecord) {
				t.Fatalf("WritePackage error = %v, want ErrInvalidRecord", err)
			}
			assertNoOutsideFile(t, root)
			if _, statErr := os.Stat(root); !os.IsNotExist(statErr) {
				t.Fatalf("rejected package must not be materialized, stat err = %v", statErr)
			}
		})
	}
}

func TestWritePackageNonArtifactFamiliesRemainSingular(t *testing.T) {
	t.Parallel()

	duplicates := map[string]func(t *testing.T) []recordpackage.Record{
		"execution": func(t *testing.T) []recordpackage.Record {
			r := mustBuildExecutionRecord(t)
			return []recordpackage.Record{recordpackage.ExecutionRecord(r), recordpackage.ExecutionRecord(r)}
		},
		"observation": func(t *testing.T) []recordpackage.Record {
			r := mustBuildObservationRecord(t)
			return []recordpackage.Record{recordpackage.ObservationRecord(r), recordpackage.ObservationRecord(r)}
		},
		"reference": func(t *testing.T) []recordpackage.Record {
			r := mustBuildReferenceRecord(t)
			return []recordpackage.Record{recordpackage.ReferenceRecord(r), recordpackage.ReferenceRecord(r)}
		},
		"failure": func(t *testing.T) []recordpackage.Record {
			r := mustBuildFailureRecord(t)
			return []recordpackage.Record{recordpackage.FailureRecord(r), recordpackage.FailureRecord(r)}
		},
		"verification": func(t *testing.T) []recordpackage.Record {
			r := mustBuildVerificationRecord(t)
			return []recordpackage.Record{recordpackage.VerificationRecord(r), recordpackage.VerificationRecord(r)}
		},
	}
	for family, build := range duplicates {
		t.Run(family, func(t *testing.T) {
			in := validPackageInput(t, filepath.Join(t.TempDir(), "pkg"))
			in.Records = build(t)
			err := recordpackage.WritePackage(in)
			if !errors.Is(err, recordpackage.ErrInvalidRecord) || !strings.Contains(err.Error(), "duplicate family") {
				t.Fatalf("WritePackage error = %v, want ErrInvalidRecord duplicate family", err)
			}
		})
	}

	// Distinct records of a non-artifact family are still a duplicate family:
	// plural support is artifact-only and must not be inferred for others.
	t.Run("distinct observation records", func(t *testing.T) {
		first := mustBuildObservationRecord(t)
		second := first
		second.RecordKey = "observation-record-2"
		rebuilt, err := recordcontract.BuildObservationRecord(recordcontract.ObservationRecordInput{
			RecordKey: second.RecordKey, Provenance: second.Provenance, Observation: second.Observation,
		})
		if err != nil {
			t.Fatalf("BuildObservationRecord returned error: %v", err)
		}
		in := validPackageInput(t, filepath.Join(t.TempDir(), "pkg"))
		in.Records = []recordpackage.Record{recordpackage.ObservationRecord(first), recordpackage.ObservationRecord(rebuilt)}
		if err := recordpackage.WritePackage(in); !errors.Is(err, recordpackage.ErrInvalidRecord) {
			t.Fatalf("WritePackage error = %v, want ErrInvalidRecord", err)
		}
	})
}

func TestWritePackageRewritesIdentityAddressedArtifactRecordsIdempotently(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "pkg")
	records := artifactRecordsWithKeys(t, "artifact-a", "artifact-b")
	in := packageInputWithArtifacts(t, root, records)
	in.OverwriteExisting = true
	if err := recordpackage.WritePackage(in); err != nil {
		t.Fatalf("WritePackage(first) returned error: %v", err)
	}
	before := readPackageFiles(t, root)
	if err := recordpackage.WritePackage(in); err != nil {
		t.Fatalf("WritePackage(second) returned error: %v", err)
	}
	after := readPackageFiles(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("overwrite with identical input changed package files")
	}
}

func sortedKeys(files map[string][]byte) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
