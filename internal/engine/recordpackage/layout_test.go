package recordpackage_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

func TestCanonicalConstantsAndTopLevelLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"package directory", recordpackage.PackageDirectoryName, "parametron-record-package"},
		{"package manifest", recordpackage.PackageManifestContractPath(), "parametron.record-package.json"},
		{"records directory", recordpackage.RecordsDirectoryContractPath(), "records"},
		{"artifacts directory", recordpackage.ArtifactsDirectoryContractPath(), "artifacts"},
		{"artifact files directory", recordpackage.ArtifactFilesDirectoryContractPath(), "artifacts/files"},
		{"raw evidence directory", recordpackage.RawEvidenceDirectoryContractPath(), "raw"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestRecordContractPathsDeriveFromRegistry(t *testing.T) {
	t.Parallel()

	definitions := recordcontract.Definitions()
	seen := make(map[recordcontract.Family]bool, len(definitions))

	for _, def := range definitions {
		got, ok := recordpackage.RecordContractPath(def.Family)
		if !ok {
			t.Fatalf("RecordContractPath(%q) ok = false, want true", def.Family)
		}

		fileName, ok := recordcontract.FileNameForFamily(def.Family)
		if !ok {
			t.Fatalf("recordcontract.FileNameForFamily(%q) ok = false, want true", def.Family)
		}
		want := "records/" + fileName
		if got != want {
			t.Fatalf("RecordContractPath(%q) = %q, want %q", def.Family, got, want)
		}
		seen[def.Family] = true
	}

	for _, family := range expectedRecordFamilies() {
		if !seen[family] {
			t.Fatalf("recordcontract.Definitions() missing expected family %q", family)
		}
	}
}

func TestUnknownRecordFamilyBehavior(t *testing.T) {
	t.Parallel()

	if got, ok := recordpackage.RecordContractPath(recordcontract.Family("unknown")); ok {
		t.Fatalf("RecordContractPath(unknown) = %q, true; want false", got)
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("MustRecordContractPath(unknown) did not panic")
		}
	}()
	_ = recordpackage.MustRecordContractPath(recordcontract.Family("unknown"))
}

func TestRecordEntriesFollowRegistryOrder(t *testing.T) {
	t.Parallel()

	definitions := recordcontract.Definitions()
	entries := recordpackage.RecordEntries()
	if len(entries) != len(definitions) {
		t.Fatalf("len(RecordEntries()) = %d, want %d", len(entries), len(definitions))
	}

	for i, entry := range entries {
		def := definitions[i]
		wantPath, ok := recordpackage.RecordContractPath(def.Family)
		if !ok {
			t.Fatalf("RecordContractPath(%q) ok = false, want true", def.Family)
		}
		if entry.Role != recordpackage.EntryRoleNormalizedRecord {
			t.Fatalf("entry[%d].Role = %q, want %q", i, entry.Role, recordpackage.EntryRoleNormalizedRecord)
		}
		if entry.Kind != recordpackage.EntryKindFile {
			t.Fatalf("entry[%d].Kind = %q, want %q", i, entry.Kind, recordpackage.EntryKindFile)
		}
		if entry.Family != def.Family {
			t.Fatalf("entry[%d].Family = %q, want %q", i, entry.Family, def.Family)
		}
		if entry.ContractPath != wantPath {
			t.Fatalf("entry[%d].ContractPath = %q, want %q", i, entry.ContractPath, wantPath)
		}
	}
}

func TestRawEvidenceEntriesAreSeparateFromNormalizedRecords(t *testing.T) {
	t.Parallel()

	want := []struct {
		path string
		kind recordpackage.EntryKind
	}{
		{"raw/prm.report.json", recordpackage.EntryKindFile},
		{"raw/prm.metadata.json", recordpackage.EntryKindFile},
		{"raw/artifact-store/manifest.json", recordpackage.EntryKindFile},
		{"raw/handoff", recordpackage.EntryKindDirectory},
		{"raw/observed/parametron.observed.json", recordpackage.EntryKindFile},
		{"raw/verification/parametron.verification.json", recordpackage.EntryKindFile},
		{"raw/runtime/result.json", recordpackage.EntryKindFile},
		// Traversal evidence follows runtime result immediately; both are raw runtime evidence.
		{"raw/runtime/parametron.reference-traversal.json", recordpackage.EntryKindFile},
	}

	entries := recordpackage.RawEvidenceEntries()
	if len(entries) != len(want) {
		t.Fatalf("len(RawEvidenceEntries()) = %d, want %d", len(entries), len(want))
	}

	for i, entry := range entries {
		if entry.Role != recordpackage.EntryRoleRawEvidence {
			t.Fatalf("entry[%d].Role = %q, want %q", i, entry.Role, recordpackage.EntryRoleRawEvidence)
		}
		if entry.Kind != want[i].kind {
			t.Fatalf("entry[%d].Kind = %q, want %q", i, entry.Kind, want[i].kind)
		}
		if entry.ContractPath != want[i].path {
			t.Fatalf("entry[%d].ContractPath = %q, want %q", i, entry.ContractPath, want[i].path)
		}
		if entry.Family != "" {
			t.Fatalf("entry[%d].Family = %q, want empty (raw evidence entries have no normalized record family)", i, entry.Family)
		}
		if strings.HasPrefix(entry.ContractPath, "records/") {
			t.Fatalf("raw evidence entry[%d] is under records/: %q", i, entry.ContractPath)
		}
	}

	// Prove traversal is placed immediately after the runtime result entry.
	resultIdx, traversalIdx := -1, -1
	for i, entry := range entries {
		switch entry.ContractPath {
		case recordpackage.RawRuntimeResultContractPath():
			resultIdx = i
		case recordpackage.RawRuntimeReferenceTraversalContractPath():
			traversalIdx = i
		}
	}
	if resultIdx < 0 {
		t.Fatal("RawEvidenceEntries() missing runtime result entry")
	}
	if traversalIdx < 0 {
		t.Fatal("RawEvidenceEntries() missing reference traversal entry")
	}
	if traversalIdx != resultIdx+1 {
		t.Fatalf("traversal entry at index %d, want immediately after runtime result entry at index %d", traversalIdx, resultIdx)
	}

	for i, entry := range recordpackage.RecordEntries() {
		if strings.HasPrefix(entry.ContractPath, "raw/") {
			t.Fatalf("record entry[%d] is under raw/: %q", i, entry.ContractPath)
		}
	}
}

func TestEntriesAreCompleteDeterministicUniqueAndValid(t *testing.T) {
	t.Parallel()

	first := recordpackage.Entries()
	second := recordpackage.Entries()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Entries() is not deterministic:\nfirst:  %#v\nsecond: %#v", first, second)
	}

	paths := make(map[string]recordpackage.Entry)
	var hasManifest, hasRecordsDir, hasArtifactsDir, hasArtifactFilesDir, hasRawDir bool
	recordPaths := contractPathSet(recordpackage.RecordEntries())
	rawPaths := contractPathSet(recordpackage.RawEvidenceEntries())

	for _, entry := range first {
		if err := recordpackage.ValidateContractPath(entry.ContractPath); err != nil {
			t.Fatalf("ValidateContractPath(%q) returned error: %v", entry.ContractPath, err)
		}
		if existing, ok := paths[entry.ContractPath]; ok {
			t.Fatalf("duplicate contract path %q: %#v and %#v", entry.ContractPath, existing, entry)
		}
		paths[entry.ContractPath] = entry

		switch entry.ContractPath {
		case recordpackage.PackageManifestContractPath():
			hasManifest = entry.Role == recordpackage.EntryRolePackageManifest && entry.Kind == recordpackage.EntryKindFile
		case recordpackage.RecordsDirectoryContractPath():
			hasRecordsDir = entry.Role == recordpackage.EntryRoleNormalizedRecord && entry.Kind == recordpackage.EntryKindDirectory
		case recordpackage.ArtifactsDirectoryContractPath():
			hasArtifactsDir = entry.Role == recordpackage.EntryRoleArtifactPayload && entry.Kind == recordpackage.EntryKindDirectory
		case recordpackage.ArtifactFilesDirectoryContractPath():
			hasArtifactFilesDir = entry.Role == recordpackage.EntryRoleArtifactPayload && entry.Kind == recordpackage.EntryKindDirectory
		case recordpackage.RawEvidenceDirectoryContractPath():
			hasRawDir = entry.Role == recordpackage.EntryRoleRawEvidence && entry.Kind == recordpackage.EntryKindDirectory
		}
	}

	for path := range recordPaths {
		if _, ok := paths[path]; !ok {
			t.Fatalf("Entries() missing normalized record path %q", path)
		}
	}
	for path := range rawPaths {
		if _, ok := paths[path]; !ok {
			t.Fatalf("Entries() missing raw evidence path %q", path)
		}
	}

	for name, ok := range map[string]bool{
		"manifest":       hasManifest,
		"records dir":    hasRecordsDir,
		"artifacts dir":  hasArtifactsDir,
		"artifact files": hasArtifactFilesDir,
		"raw dir":        hasRawDir,
	} {
		if !ok {
			t.Fatalf("Entries() missing valid %s entry", name)
		}
	}
}

func TestReturnedEntrySlicesAreCopySafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func() []recordpackage.Entry
	}{
		{"RecordEntries", recordpackage.RecordEntries},
		{"RawEvidenceEntries", recordpackage.RawEvidenceEntries},
		{"Entries", recordpackage.Entries},
	}

	for _, tt := range tests {
		before := tt.fn()
		if len(before) == 0 {
			t.Fatalf("%s() returned no entries", tt.name)
		}
		before[0] = recordpackage.Entry{
			ContractPath: "mutated",
			Kind:         recordpackage.EntryKindDirectory,
			Role:         recordpackage.EntryRoleRawEvidence,
			Family:       recordcontract.Family("mutated"),
		}

		after := tt.fn()
		if after[0].ContractPath == "mutated" || after[0].Family == recordcontract.Family("mutated") {
			t.Fatalf("%s() returned state mutated through earlier result: %#v", tt.name, after[0])
		}
	}
}

func TestValidateContractPathAcceptsSafeAndRejectsUnsafePaths(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"records/parametron.execution-record.json",
		"raw/report.json",
		"artifacts/files",
		"parametron.record-package.json",
	} {
		if err := recordpackage.ValidateContractPath(path); err != nil {
			t.Fatalf("ValidateContractPath(%q) returned error: %v", path, err)
		}
	}

	for _, path := range []string{
		"",
		"/absolute/path",
		"../escape",
		"records/../escape",
		"records//file.json",
		`records\file.json`,
		".",
		"./records",
		"records/.",
		"records/..",
	} {
		err := recordpackage.ValidateContractPath(path)
		if err == nil {
			t.Fatalf("ValidateContractPath(%q) returned nil, want error", path)
		}
		if !errors.Is(err, recordpackage.ErrInvalidLayoutPath) {
			t.Fatalf("ValidateContractPath(%q) error = %v, want ErrInvalidLayoutPath", path, err)
		}
	}
}

func TestJoinContractPathUsesForwardSlashesAndRejectsUnsafeSegments(t *testing.T) {
	t.Parallel()

	got, err := recordpackage.JoinContractPath("records", "parametron.execution-record.json")
	if err != nil {
		t.Fatalf("JoinContractPath(safe parts) returned error: %v", err)
	}
	if got != "records/parametron.execution-record.json" {
		t.Fatalf("JoinContractPath(safe parts) = %q, want %q", got, "records/parametron.execution-record.json")
	}
	if strings.Contains(got, string(os.PathSeparator)) && os.PathSeparator != '/' {
		t.Fatalf("JoinContractPath returned OS-specific separator in %q", got)
	}
	if err := recordpackage.ValidateContractPath(got); err != nil {
		t.Fatalf("joined path did not validate: %v", err)
	}

	for _, parts := range [][]string{
		{"records", ""},
		{"records", ".."},
		{"records", "."},
		{"records", "/absolute"},
		{"records", `name\with\backslash`},
	} {
		joined, err := recordpackage.JoinContractPath(parts...)
		if err == nil {
			t.Fatalf("JoinContractPath(%q) = %q, nil; want error", parts, joined)
		}
		if !errors.Is(err, recordpackage.ErrInvalidLayoutPath) {
			t.Fatalf("JoinContractPath(%q) error = %v, want ErrInvalidLayoutPath", parts, err)
		}
	}
}

func TestLayoutResolverIsSafeAndWriteFree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	layout, err := recordpackage.NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout(temp root) returned error: %v", err)
	}
	if layout.Root != filepath.Clean(root) {
		t.Fatalf("layout.Root = %q, want %q", layout.Root, filepath.Clean(root))
	}

	recordPath := "records/parametron.execution-record.json"
	fsPath, err := layout.FSPath(recordPath)
	if err != nil {
		t.Fatalf("FSPath(%q) returned error: %v", recordPath, err)
	}
	if !pathIsUnderRoot(t, root, fsPath) {
		t.Fatalf("FSPath(%q) = %q, want path under %q", recordPath, fsPath, root)
	}
	if filepath.ToSlash(fsPath) == recordPath {
		t.Fatalf("FSPath(%q) returned contract path instead of filesystem path", recordPath)
	}

	if escaped, err := layout.FSPath("records/../escape"); err == nil {
		t.Fatalf("FSPath(unsafe path) = %q, nil; want error", escaped)
	} else if !errors.Is(err, recordpackage.ErrInvalidLayoutPath) {
		t.Fatalf("FSPath(unsafe path) error = %v, want ErrInvalidLayoutPath", err)
	}

	expectedContractPath, ok := recordpackage.RecordContractPath(recordcontract.FamilyExecution)
	if !ok {
		t.Fatal("RecordContractPath(execution) ok = false, want true")
	}
	recordFSPath, ok, err := layout.RecordFSPath(recordcontract.FamilyExecution)
	if err != nil {
		t.Fatalf("RecordFSPath(execution) returned error: %v", err)
	}
	if !ok {
		t.Fatal("RecordFSPath(execution) ok = false, want true")
	}
	expectedFSPath, err := layout.FSPath(expectedContractPath)
	if err != nil {
		t.Fatalf("FSPath(%q) returned error: %v", expectedContractPath, err)
	}
	if recordFSPath != expectedFSPath {
		t.Fatalf("RecordFSPath(execution) = %q, want %q", recordFSPath, expectedFSPath)
	}

	if got, ok, err := layout.RecordFSPath(recordcontract.Family("unknown")); err != nil || ok || got != "" {
		t.Fatalf("RecordFSPath(unknown) = %q, %v, %v; want empty, false, nil", got, ok, err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir(temp root) returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("layout resolver created filesystem entries under root: %#v", entries)
	}
}

func expectedRecordFamilies() []recordcontract.Family {
	return []recordcontract.Family{
		recordcontract.FamilyExecution,
		recordcontract.FamilyArtifact,
		recordcontract.FamilyObservation,
		recordcontract.FamilyReference,
		recordcontract.FamilyFailure,
		recordcontract.FamilyVerification,
	}
}

func contractPathSet(entries []recordpackage.Entry) map[string]struct{} {
	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out[entry.ContractPath] = struct{}{}
	}
	return out
}

func pathIsUnderRoot(t *testing.T, root, path string) bool {
	t.Helper()

	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q) returned error: %v", root, path, err)
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
