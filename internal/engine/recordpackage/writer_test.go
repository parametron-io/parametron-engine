package recordpackage_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"parametron/internal/engine/recordcontract"
	"parametron/internal/engine/recordpackage"
)

type testPackageManifest struct {
	SchemaVersion string `json:"schemaVersion"`
	PackageKey    string `json:"packageKey"`
	LayoutVersion string `json:"layoutVersion"`
	Ownership     struct {
		Producer            string `json:"producer"`
		LocalOutputEmitter  string `json:"localOutputEmitter"`
		IngestionTarget     string `json:"ingestionTarget"`
		DurableStorageOwner string `json:"durableStorageOwner"`
	} `json:"ownership"`
	Records []struct {
		Family       string `json:"family"`
		ContractPath string `json:"contractPath"`
		RecordKey    string `json:"recordKey"`
		IdentityID   string `json:"identityId"`
	} `json:"records"`
	Artifacts []struct {
		ContractPath string `json:"contractPath"`
	} `json:"artifacts,omitempty"`
	RawEvidence []struct {
		ContractPath string `json:"contractPath"`
	} `json:"rawEvidence,omitempty"`
}

func TestWritePackageRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	validRoot := func(t *testing.T) string {
		t.Helper()
		return filepath.Join(t.TempDir(), "package")
	}

	execution := mustBuildExecutionRecord(t)

	tests := []struct {
		name string
		in   func(t *testing.T) recordpackage.PackageInput
		want error
	}{
		{
			name: "missing package root",
			in: func(t *testing.T) recordpackage.PackageInput {
				return validPackageInput(t, "")
			},
			want: recordpackage.ErrInvalidPackageInput,
		},
		{
			name: "missing package key",
			in: func(t *testing.T) recordpackage.PackageInput {
				in := validPackageInput(t, validRoot(t))
				in.PackageKey = " \t "
				return in
			},
			want: recordpackage.ErrInvalidPackageInput,
		},
		{
			name: "no records",
			in: func(t *testing.T) recordpackage.PackageInput {
				in := validPackageInput(t, validRoot(t))
				in.Records = nil
				return in
			},
			want: recordpackage.ErrInvalidPackageInput,
		},
		{
			name: "duplicate record family",
			in: func(t *testing.T) recordpackage.PackageInput {
				in := validPackageInput(t, validRoot(t))
				in.Records = []recordpackage.Record{
					recordpackage.ExecutionRecord(execution),
					recordpackage.ExecutionRecord(execution),
				}
				return in
			},
			want: recordpackage.ErrInvalidRecord,
		},
		{
			name: "missing record payload",
			in: func(t *testing.T) recordpackage.PackageInput {
				in := validPackageInput(t, validRoot(t))
				in.Records = []recordpackage.Record{{}}
				return in
			},
			want: recordpackage.ErrInvalidRecord,
		},
		{
			name: "multiple record payload fields",
			in: func(t *testing.T) recordpackage.PackageInput {
				artifact := mustBuildArtifactRecord(t)
				in := validPackageInput(t, validRoot(t))
				in.Records = []recordpackage.Record{{Execution: &execution, Artifact: &artifact}}
				return in
			},
			want: recordpackage.ErrInvalidRecord,
		},
		{
			name: "invalid record payload",
			in: func(t *testing.T) recordpackage.PackageInput {
				invalid := execution
				invalid.RecordKey = ""
				in := validPackageInput(t, validRoot(t))
				in.Records = []recordpackage.Record{recordpackage.ExecutionRecord(invalid)}
				return in
			},
			want: recordpackage.ErrInvalidRecord,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := recordpackage.WritePackage(tt.in(t))
			if !errors.Is(err, tt.want) {
				t.Fatalf("WritePackage error = %v, want errors.Is(..., %v)", err, tt.want)
			}
		})
	}
}

func TestWritePackageMaterializesLayoutRecordsAndManifest(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "record-package")
	records := allTestRecords(t)
	input := validPackageInput(t, root)
	input.Records = records

	if err := recordpackage.WritePackage(input); err != nil {
		t.Fatalf("WritePackage(valid) returned error: %v", err)
	}

	if !dirExists(t, root) {
		t.Fatalf("package root %q was not created", root)
	}
	for _, contractPath := range []string{
		recordpackage.RecordsDirectoryContractPath(),
		recordpackage.ArtifactsDirectoryContractPath(),
		recordpackage.ArtifactFilesDirectoryContractPath(),
		recordpackage.RawEvidenceDirectoryContractPath(),
	} {
		if !dirExists(t, filepath.Join(root, filepath.FromSlash(contractPath))) {
			t.Fatalf("layout directory %q was not created", contractPath)
		}
	}
	for _, entry := range recordpackage.RawEvidenceEntries() {
		if entry.Kind != recordpackage.EntryKindDirectory {
			continue
		}
		if !dirExists(t, filepath.Join(root, filepath.FromSlash(entry.ContractPath))) {
			t.Fatalf("raw evidence directory %q was not created", entry.ContractPath)
		}
	}

	manifestPath := filepath.Join(root, filepath.FromSlash(recordpackage.PackageManifestContractPath()))
	assertJSONFileWithTrailingNewline(t, manifestPath)
	manifest := readManifest(t, root)
	if manifest.SchemaVersion != "1.0" {
		t.Fatalf("manifest schemaVersion = %q, want 1.0", manifest.SchemaVersion)
	}
	if manifest.PackageKey != input.PackageKey {
		t.Fatalf("manifest packageKey = %q, want %q", manifest.PackageKey, input.PackageKey)
	}
	if manifest.LayoutVersion != recordcontract.CurrentVersion {
		t.Fatalf("manifest layoutVersion = %q, want %q", manifest.LayoutVersion, recordcontract.CurrentVersion)
	}

	ownership := recordcontract.EngineProducedOwnership()
	if manifest.Ownership.Producer != string(ownership.Producer) ||
		manifest.Ownership.LocalOutputEmitter != string(ownership.LocalOutputEmitter) ||
		manifest.Ownership.IngestionTarget != string(ownership.IngestionTarget) ||
		manifest.Ownership.DurableStorageOwner != string(ownership.DurableStorageOwner) {
		t.Fatalf("manifest ownership = %#v, want %#v", manifest.Ownership, ownership)
	}

	wantEntries := expectedRecordManifestEntries(t)
	if !reflect.DeepEqual(manifest.Records, wantEntries) {
		t.Fatalf("manifest records = %#v, want %#v", manifest.Records, wantEntries)
	}

	for _, entry := range manifest.Records {
		recordPath := filepath.Join(root, filepath.FromSlash(entry.ContractPath))
		assertJSONFileWithTrailingNewline(t, recordPath)

		var payload struct {
			Family    string `json:"family"`
			Version   string `json:"version"`
			RecordKey string `json:"recordKey"`
			Identity  struct {
				ID string `json:"id"`
			} `json:"identity"`
		}
		readJSONFile(t, recordPath, &payload)
		if payload.Family != entry.Family || payload.RecordKey != entry.RecordKey || payload.Identity.ID != entry.IdentityID {
			t.Fatalf("record %q payload identity fields = %#v, manifest entry = %#v", entry.ContractPath, payload, entry)
		}
		if payload.Version != recordcontract.CurrentVersion {
			t.Fatalf("record %q version = %q, want %q", entry.ContractPath, payload.Version, recordcontract.CurrentVersion)
		}
	}
}

func TestWritePackageDeterministicOrderingAndBytes(t *testing.T) {
	t.Parallel()

	rootA := filepath.Join(t.TempDir(), "a")
	rootB := filepath.Join(t.TempDir(), "b")

	inputA := validPackageInput(t, rootA)
	inputA.Records = shuffledTestRecords(t)
	inputA.ArtifactFiles = shuffledArtifactFiles()
	inputA.RawEvidenceFiles = shuffledRawEvidenceFilesWithHandoff(t)

	inputB := validPackageInput(t, rootB)
	inputB.Records = allTestRecords(t)
	inputB.ArtifactFiles = differentlyOrderedArtifactFiles()
	inputB.RawEvidenceFiles = differentlyOrderedRawEvidenceFilesWithHandoff(t)

	if err := recordpackage.WritePackage(inputA); err != nil {
		t.Fatalf("WritePackage(first) returned error: %v", err)
	}
	if err := recordpackage.WritePackage(inputB); err != nil {
		t.Fatalf("WritePackage(second) returned error: %v", err)
	}

	manifest := readManifest(t, rootA)
	gotFamilies := make([]recordcontract.Family, len(manifest.Records))
	for i, record := range manifest.Records {
		gotFamilies[i] = recordcontract.Family(record.Family)
	}
	if want := definitionFamilies(); !reflect.DeepEqual(gotFamilies, want) {
		t.Fatalf("manifest family order = %#v, want %#v", gotFamilies, want)
	}

	gotArtifactPaths := artifactManifestPaths(manifest)
	if want := []string{"artifacts/files/a.txt", "artifacts/files/nested/b.txt", "artifacts/files/z.txt"}; !reflect.DeepEqual(gotArtifactPaths, want) {
		t.Fatalf("manifest artifact paths = %#v, want %#v", gotArtifactPaths, want)
	}

	gotRawPaths := rawManifestPaths(manifest)
	if want := rawEvidenceFilePathsWithHandoffInLayoutOrder(); !reflect.DeepEqual(gotRawPaths, want) {
		t.Fatalf("manifest raw evidence paths = %#v, want %#v", gotRawPaths, want)
	}

	filesA := readPackageFiles(t, rootA)
	filesB := readPackageFiles(t, rootB)
	if !reflect.DeepEqual(filesA, filesB) {
		t.Fatalf("package outputs differ for equivalent inputs")
	}
	if got, want := filesA[recordpackage.PackageManifestContractPath()], filesB[recordpackage.PackageManifestContractPath()]; !bytes.Equal(got, want) {
		t.Fatalf("manifest bytes differ for equivalent inputs")
	}
	for _, entry := range manifest.Records {
		got := filesA[entry.ContractPath]
		want := filesB[entry.ContractPath]
		if !bytes.Equal(got, want) {
			t.Fatalf("record bytes differ for equivalent inputs at %q", entry.ContractPath)
		}
	}
	assertPackageManifestAndRecordsHaveTrailingNewline(t, rootA, manifest)
	assertPackageManifestAndRecordsHaveTrailingNewline(t, rootB, readManifest(t, rootB))
	assertPackageFilesDoNotContain(t, filesA, rootA, rootB)
	assertPackageFilesDoNotContain(t, filesB, rootA, rootB)
}

func TestWritePackageCanonicalDirectoryName(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	if err := recordpackage.WritePackage(recordpackage.PackageInput{
		PackageRoot:               parent,
		UseCanonicalDirectoryName: true,
		PackageKey:                "package-1",
		Records:                   []recordpackage.Record{recordpackage.ExecutionRecord(mustBuildExecutionRecord(t))},
	}); err != nil {
		t.Fatalf("WritePackage(canonical directory) returned error: %v", err)
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir(parent) returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != recordpackage.PackageDirectoryName || !entries[0].IsDir() {
		t.Fatalf("parent entries = %#v, want only canonical package directory", entries)
	}

	canonicalRoot := filepath.Join(parent, recordpackage.PackageDirectoryName)
	if !fileExists(t, filepath.Join(canonicalRoot, filepath.FromSlash(recordpackage.PackageManifestContractPath()))) {
		t.Fatalf("manifest was not written under canonical root %q", canonicalRoot)
	}
	if fileExists(t, filepath.Join(parent, filepath.FromSlash(recordpackage.PackageManifestContractPath()))) {
		t.Fatalf("manifest was written directly under parent %q", parent)
	}
}

func TestWritePackageDestinationAndOverwriteBehavior(t *testing.T) {
	t.Parallel()

	t.Run("non-empty root without overwrite", func(t *testing.T) {
		root := t.TempDir()
		unrelated := filepath.Join(root, "unrelated.txt")
		writeTestFile(t, unrelated, []byte("keep"))

		err := recordpackage.WritePackage(validPackageInput(t, root))
		if !errors.Is(err, recordpackage.ErrPackageDestinationExists) {
			t.Fatalf("WritePackage error = %v, want ErrPackageDestinationExists", err)
		}
		if got := readFile(t, unrelated); string(got) != "keep" {
			t.Fatalf("unrelated file content = %q, want keep", got)
		}
		if fileExists(t, filepath.Join(root, filepath.FromSlash(recordpackage.PackageManifestContractPath()))) {
			t.Fatal("manifest was produced after destination protection failure")
		}
	})

	t.Run("existing manifest without overwrite", func(t *testing.T) {
		root := t.TempDir()
		manifestPath := filepath.Join(root, filepath.FromSlash(recordpackage.PackageManifestContractPath()))
		writeTestFile(t, manifestPath, []byte("existing manifest"))

		err := recordpackage.WritePackage(validPackageInput(t, root))
		if !errors.Is(err, recordpackage.ErrPackageManifestExists) {
			t.Fatalf("WritePackage error = %v, want ErrPackageManifestExists", err)
		}
		if got := readFile(t, manifestPath); string(got) != "existing manifest" {
			t.Fatalf("manifest content = %q, want existing manifest", got)
		}
	})

	t.Run("overwrite is idempotent and preserves unrelated files", func(t *testing.T) {
		root := t.TempDir()
		unrelated := filepath.Join(root, "unrelated.txt")
		writeTestFile(t, unrelated, []byte("keep"))

		input := validPackageInput(t, root)
		input.OverwriteExisting = true
		if err := recordpackage.WritePackage(input); err != nil {
			t.Fatalf("WritePackage(overwrite first) returned error: %v", err)
		}
		first := readPackageFiles(t, root)
		if got := readFile(t, unrelated); string(got) != "keep" {
			t.Fatalf("unrelated file content = %q, want keep", got)
		}

		if err := recordpackage.WritePackage(input); err != nil {
			t.Fatalf("WritePackage(overwrite second) returned error: %v", err)
		}
		second := readPackageFiles(t, root)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("overwrite with same input changed package bytes")
		}
		if got := readFile(t, unrelated); string(got) != "keep" {
			t.Fatalf("unrelated file content after overwrite = %q, want keep", got)
		}
	})
}

func TestWritePackageArtifactFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "package")
	input := validPackageInput(t, root)
	input.ArtifactFiles = shuffledArtifactFiles()
	if err := recordpackage.WritePackage(input); err != nil {
		t.Fatalf("WritePackage(artifacts) returned error: %v", err)
	}

	for _, artifact := range input.ArtifactFiles {
		got := readFile(t, filepath.Join(root, filepath.FromSlash(artifact.ContractPath)))
		if !bytes.Equal(got, artifact.Content) {
			t.Fatalf("artifact %q bytes = %q, want %q", artifact.ContractPath, got, artifact.Content)
		}
	}
	manifest := readManifest(t, root)
	gotPaths := artifactManifestPaths(manifest)
	if want := []string{"artifacts/files/a.txt", "artifacts/files/nested/b.txt", "artifacts/files/z.txt"}; !reflect.DeepEqual(gotPaths, want) {
		t.Fatalf("manifest artifact paths = %#v, want %#v", gotPaths, want)
	}

	for _, contractPath := range []string{
		"",
		"../outside",
		"artifacts/files/../outside",
		"/tmp/outside",
		"records/not-artifact.txt",
		"artifacts/other.txt",
		`artifacts\files\outside.txt`,
	} {
		t.Run("reject "+contractPath, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "package")
			input := validPackageInput(t, root)
			input.ArtifactFiles = []recordpackage.ArtifactFile{{ContractPath: contractPath, Content: []byte("x")}}
			err := recordpackage.WritePackage(input)
			if !errors.Is(err, recordpackage.ErrInvalidLayoutPath) {
				t.Fatalf("WritePackage artifact path %q error = %v, want ErrInvalidLayoutPath", contractPath, err)
			}
			assertNoOutsideFile(t, root)
		})
	}
}

func TestWritePackageCopiesCallerOwnedPayloadBytes(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "package")
	artifactContent := []byte("original artifact bytes")
	rawContent := []byte("original raw bytes")
	input := validPackageInput(t, root)
	input.ArtifactFiles = []recordpackage.ArtifactFile{{
		ContractPath: "artifacts/files/copy-safe.txt",
		Content:      artifactContent,
	}}
	input.RawEvidenceFiles = []recordpackage.RawEvidenceFile{{
		ContractPath: recordpackage.RawRuntimeResultContractPath(),
		Content:      rawContent,
	}}

	if err := recordpackage.WritePackage(input); err != nil {
		t.Fatalf("WritePackage(copy safety) returned error: %v", err)
	}

	artifactContent[0] = 'X'
	rawContent[0] = 'X'

	artifactPath := filepath.Join(root, filepath.FromSlash(input.ArtifactFiles[0].ContractPath))
	if got, want := readFile(t, artifactPath), []byte("original artifact bytes"); !bytes.Equal(got, want) {
		t.Fatalf("artifact bytes after caller mutation = %q, want %q", got, want)
	}

	rawPath := filepath.Join(root, filepath.FromSlash(input.RawEvidenceFiles[0].ContractPath))
	if got, want := readFile(t, rawPath), []byte("original raw bytes"); !bytes.Equal(got, want) {
		t.Fatalf("raw evidence bytes after caller mutation = %q, want %q", got, want)
	}
}

func TestWritePackageRawEvidenceFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "package")
	input := validPackageInput(t, root)
	input.RawEvidenceFiles = shuffledRawEvidenceFiles(t)
	if err := recordpackage.WritePackage(input); err != nil {
		t.Fatalf("WritePackage(raw evidence) returned error: %v", err)
	}

	for _, evidence := range input.RawEvidenceFiles {
		got := readFile(t, filepath.Join(root, filepath.FromSlash(evidence.ContractPath)))
		if !bytes.Equal(got, evidence.Content) {
			t.Fatalf("raw evidence %q bytes = %q, want %q", evidence.ContractPath, got, evidence.Content)
		}
	}
	manifest := readManifest(t, root)
	if want := rawEvidenceFilePathsInLayoutOrder(); !reflect.DeepEqual(rawManifestPaths(manifest), want) {
		t.Fatalf("manifest raw evidence paths = %#v, want %#v", rawManifestPaths(manifest), want)
	}

	t.Run("canonical runtime result remains raw evidence", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "package")
		input := validPackageInput(t, root)
		input.RawEvidenceFiles = []recordpackage.RawEvidenceFile{{
			ContractPath: recordpackage.RawRuntimeResultContractPath(),
			Content:      []byte(`{"status":"failure"}`),
		}}

		if err := recordpackage.WritePackage(input); err != nil {
			t.Fatalf("WritePackage(raw runtime result) returned error: %v", err)
		}

		got := readFile(t, filepath.Join(root, filepath.FromSlash(recordpackage.RawRuntimeResultContractPath())))
		if want := []byte(`{"status":"failure"}`); !bytes.Equal(got, want) {
			t.Fatalf("raw runtime result bytes = %q, want %q", got, want)
		}

		manifest := readManifest(t, root)
		if !reflect.DeepEqual(rawManifestPaths(manifest), []string{recordpackage.RawRuntimeResultContractPath()}) {
			t.Fatalf("manifest raw evidence paths = %#v, want runtime result only", rawManifestPaths(manifest))
		}
		for _, record := range manifest.Records {
			if record.ContractPath == recordpackage.RawRuntimeResultContractPath() {
				t.Fatalf("runtime result path listed as normalized record: %#v", manifest.Records)
			}
		}
	})

	t.Run("canonical traversal remains raw evidence", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "package")
		input := validPackageInput(t, root)
		traversalContent := []byte(`{"nodes":[],"edges":[]}`)
		input.RawEvidenceFiles = []recordpackage.RawEvidenceFile{{
			ContractPath: recordpackage.RawRuntimeReferenceTraversalContractPath(),
			Content:      traversalContent,
		}}

		if err := recordpackage.WritePackage(input); err != nil {
			t.Fatalf("WritePackage(traversal raw evidence) returned error: %v", err)
		}

		traversalPath := filepath.Join(root, filepath.FromSlash(recordpackage.RawRuntimeReferenceTraversalContractPath()))
		if got := readFile(t, traversalPath); !bytes.Equal(got, traversalContent) {
			t.Fatalf("traversal bytes = %q, want %q", got, traversalContent)
		}

		manifest := readManifest(t, root)
		if !reflect.DeepEqual(rawManifestPaths(manifest), []string{recordpackage.RawRuntimeReferenceTraversalContractPath()}) {
			t.Fatalf("manifest raw evidence paths = %#v, want traversal only", rawManifestPaths(manifest))
		}
		for _, record := range manifest.Records {
			if record.ContractPath == recordpackage.RawRuntimeReferenceTraversalContractPath() {
				t.Fatalf("traversal path listed as normalized record: %#v", manifest.Records)
			}
		}
		if got := artifactManifestPaths(manifest); len(got) != 0 {
			t.Fatalf("manifest artifact paths = %#v, want none for traversal-only package", got)
		}
	})

	tests := []struct {
		name         string
		files        []recordpackage.RawEvidenceFile
		wantSentinel error
	}{
		{
			name:         "empty path",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "normalized record path",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "records/parametron.failure-record.json", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "artifact payload path",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "artifacts/files/example.step", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "raw handoff directory path",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: recordpackage.RawHandoffDirectoryContractPath(), Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "handoff parent traversal",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "raw/handoff/../package.json", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "handoff dot segment",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "raw/handoff/./package.json", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "handoff repeated separator",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "raw//handoff/package.json", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "handoff backslash",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: `raw\handoff\package.json`, Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "handoff absolute",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "/raw/handoff/package.json", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "handoff traversal",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "../raw/handoff/package.json", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "unknown raw path",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "raw/runtime/other.json", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "traversal",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "../outside", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "nested traversal",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "raw/../outside", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "absolute",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "/tmp/outside", Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "backslash",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: `raw\report.json`, Content: []byte("x")}},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name: "duplicate canonical path",
			files: []recordpackage.RawEvidenceFile{
				{ContractPath: recordpackage.RawRuntimeResultContractPath(), Content: []byte("x")},
				{ContractPath: recordpackage.RawRuntimeResultContractPath(), Content: []byte("y")},
			},
			wantSentinel: recordpackage.ErrInvalidLayoutPath,
		},
		{
			name:         "empty content",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: recordpackage.RawRuntimeResultContractPath()}},
			wantSentinel: recordpackage.ErrInvalidPackageInput,
		},
		{
			name:         "handoff nil content",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "raw/handoff/package.json", Content: nil}},
			wantSentinel: recordpackage.ErrInvalidPackageInput,
		},
		{
			name:         "handoff empty content",
			files:        []recordpackage.RawEvidenceFile{{ContractPath: "raw/handoff/package.json", Content: []byte{}}},
			wantSentinel: recordpackage.ErrInvalidPackageInput,
		},
	}

	for _, tt := range tests {
		t.Run("reject "+tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "package")
			input := validPackageInput(t, root)
			input.RawEvidenceFiles = tt.files
			err := recordpackage.WritePackage(input)
			if !errors.Is(err, tt.wantSentinel) {
				t.Fatalf("WritePackage raw evidence files %#v error = %v, want %v", tt.files, err, tt.wantSentinel)
			}
			assertNoOutsideFile(t, root)
		})
	}
}

func TestWritePackageHandoffRawEvidenceFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "package")
	input := validPackageInput(t, root)
	input.RawEvidenceFiles = []recordpackage.RawEvidenceFile{
		{ContractPath: "raw/handoff/package.json", Content: []byte(`{"package":"root"}`)},
		{ContractPath: "raw/handoff/product-a/manifest.json", Content: []byte(`{"product":"a"}`)},
	}

	if err := recordpackage.WritePackage(input); err != nil {
		t.Fatalf("WritePackage(handoff raw evidence) returned error: %v", err)
	}

	for _, evidence := range input.RawEvidenceFiles {
		got := readFile(t, filepath.Join(root, filepath.FromSlash(evidence.ContractPath)))
		if !bytes.Equal(got, evidence.Content) {
			t.Fatalf("handoff raw evidence %q bytes = %q, want %q", evidence.ContractPath, got, evidence.Content)
		}
	}

	manifest := readManifest(t, root)
	wantRaw := []string{
		"raw/handoff/package.json",
		"raw/handoff/product-a/manifest.json",
	}
	if !reflect.DeepEqual(rawManifestPaths(manifest), wantRaw) {
		t.Fatalf("manifest raw evidence paths = %#v, want %#v", rawManifestPaths(manifest), wantRaw)
	}
	if got := artifactManifestPaths(manifest); len(got) != 0 {
		t.Fatalf("manifest artifact paths = %#v, want none", got)
	}
	for _, record := range manifest.Records {
		for _, handoffPath := range wantRaw {
			if record.ContractPath == handoffPath {
				t.Fatalf("handoff path %q listed as normalized record: %#v", handoffPath, manifest.Records)
			}
		}
	}
}

func TestWritePackageRawEvidenceManifestOrderingWithHandoffFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "package")
	input := validPackageInput(t, root)
	input.RawEvidenceFiles = []recordpackage.RawEvidenceFile{
		{ContractPath: "raw/runtime/parametron.reference-traversal.json", Content: []byte("traversal")},
		{ContractPath: "raw/runtime/result.json", Content: []byte("runtime")},
		{ContractPath: "raw/handoff/zeta/package.json", Content: []byte("zeta")},
		{ContractPath: "raw/report.json", Content: []byte("report")},
		{ContractPath: "raw/handoff/alpha/package.json", Content: []byte("alpha")},
		{ContractPath: "raw/metadata.json", Content: []byte("metadata")},
		{ContractPath: "raw/observed/parametron.observed.json", Content: []byte("observed")},
		{ContractPath: "raw/verification/parametron.verification.json", Content: []byte("verification")},
		{ContractPath: "raw/artifact-store/manifest.json", Content: []byte("artifact-store")},
	}

	if err := recordpackage.WritePackage(input); err != nil {
		t.Fatalf("WritePackage(raw evidence ordering with handoff) returned error: %v", err)
	}

	manifest := readManifest(t, root)
	// Order must follow RawEvidenceEntries(): traversal immediately after runtime result.
	want := []string{
		"raw/report.json",
		"raw/metadata.json",
		"raw/artifact-store/manifest.json",
		"raw/handoff/alpha/package.json",
		"raw/handoff/zeta/package.json",
		"raw/observed/parametron.observed.json",
		"raw/verification/parametron.verification.json",
		"raw/runtime/result.json",
		"raw/runtime/parametron.reference-traversal.json",
	}
	if !reflect.DeepEqual(rawManifestPaths(manifest), want) {
		t.Fatalf("manifest raw evidence paths = %#v, want %#v", rawManifestPaths(manifest), want)
	}
}

func TestWritePackagePathConfinement(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name         string
		artifact     string
		raw          string
		wantSentinel error
	}{
		{name: "artifact traversal", artifact: "../outside", wantSentinel: recordpackage.ErrInvalidLayoutPath},
		{name: "artifact nested traversal", artifact: "records/../../outside", wantSentinel: recordpackage.ErrInvalidLayoutPath},
		{name: "artifact absolute", artifact: "/tmp/outside", wantSentinel: recordpackage.ErrInvalidLayoutPath},
		{name: "artifact backslash", artifact: `artifacts\files\outside`, wantSentinel: recordpackage.ErrInvalidLayoutPath},
		{name: "raw traversal", raw: "../outside", wantSentinel: recordpackage.ErrInvalidLayoutPath},
		{name: "raw nested traversal", raw: "records/../../outside", wantSentinel: recordpackage.ErrInvalidLayoutPath},
		{name: "raw absolute", raw: "/tmp/outside", wantSentinel: recordpackage.ErrInvalidLayoutPath},
		{name: "raw backslash", raw: `raw\report.json`, wantSentinel: recordpackage.ErrInvalidLayoutPath},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "package")
			outside := filepath.Join(parent, "outside")

			input := validPackageInput(t, root)
			if tt.artifact != "" {
				input.ArtifactFiles = []recordpackage.ArtifactFile{{ContractPath: tt.artifact, Content: []byte("x")}}
			}
			if tt.raw != "" {
				input.RawEvidenceFiles = []recordpackage.RawEvidenceFile{{ContractPath: tt.raw, Content: []byte("x")}}
			}

			err := recordpackage.WritePackage(input)
			if !errors.Is(err, tt.wantSentinel) {
				t.Fatalf("WritePackage error = %v, want %v", err, tt.wantSentinel)
			}
			if fileExists(t, outside) {
				t.Fatalf("path confinement failed: outside file %q exists", outside)
			}
		})
	}
}

func TestWritePackageFailureBehavior(t *testing.T) {
	t.Parallel()

	t.Run("package root is file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "package")
		writeTestFile(t, root, []byte("not a directory"))

		err := recordpackage.WritePackage(validPackageInput(t, root))
		if !errors.Is(err, recordpackage.ErrPackageDestinationExists) {
			t.Fatalf("WritePackage error = %v, want ErrPackageDestinationExists", err)
		}
		if got := readFile(t, root); string(got) != "not a directory" {
			t.Fatalf("root file content = %q, want unchanged", got)
		}
	})

	t.Run("required layout directory blocked by file", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(recordpackage.RecordsDirectoryContractPath())), []byte("blocked"))

		input := validPackageInput(t, root)
		input.OverwriteExisting = true
		err := recordpackage.WritePackage(input)
		if !errors.Is(err, recordpackage.ErrPackageDestinationExists) {
			t.Fatalf("WritePackage error = %v, want ErrPackageDestinationExists", err)
		}
		if fileExists(t, filepath.Join(root, filepath.FromSlash(recordpackage.PackageManifestContractPath()))) {
			t.Fatal("manifest was produced after blocked layout path failure")
		}
		assertNoTempFiles(t, root)
	})
}

func validPackageInput(t *testing.T, root string) recordpackage.PackageInput {
	t.Helper()
	return recordpackage.PackageInput{
		PackageRoot: root,
		PackageKey:  "package-1",
		Records:     []recordpackage.Record{recordpackage.ExecutionRecord(mustBuildExecutionRecord(t))},
	}
}

func allTestRecords(t *testing.T) []recordpackage.Record {
	t.Helper()
	return []recordpackage.Record{
		recordpackage.ExecutionRecord(mustBuildExecutionRecord(t)),
		recordpackage.ArtifactRecord(mustBuildArtifactRecord(t)),
		recordpackage.ObservationRecord(mustBuildObservationRecord(t)),
		recordpackage.ReferenceRecord(mustBuildReferenceRecord(t)),
		recordpackage.FailureRecord(mustBuildFailureRecord(t)),
		recordpackage.VerificationRecord(mustBuildVerificationRecord(t)),
	}
}

func shuffledTestRecords(t *testing.T) []recordpackage.Record {
	t.Helper()
	return []recordpackage.Record{
		recordpackage.VerificationRecord(mustBuildVerificationRecord(t)),
		recordpackage.ReferenceRecord(mustBuildReferenceRecord(t)),
		recordpackage.ExecutionRecord(mustBuildExecutionRecord(t)),
		recordpackage.FailureRecord(mustBuildFailureRecord(t)),
		recordpackage.ObservationRecord(mustBuildObservationRecord(t)),
		recordpackage.ArtifactRecord(mustBuildArtifactRecord(t)),
	}
}

func expectedRecordManifestEntries(t *testing.T) []struct {
	Family       string `json:"family"`
	ContractPath string `json:"contractPath"`
	RecordKey    string `json:"recordKey"`
	IdentityID   string `json:"identityId"`
} {
	t.Helper()

	recordsByFamily := map[recordcontract.Family]struct {
		recordKey  string
		identityID string
	}{
		recordcontract.FamilyExecution:    {recordKey: mustBuildExecutionRecord(t).RecordKey, identityID: mustBuildExecutionRecord(t).Identity.ID},
		recordcontract.FamilyArtifact:     {recordKey: mustBuildArtifactRecord(t).RecordKey, identityID: mustBuildArtifactRecord(t).Identity.ID},
		recordcontract.FamilyObservation:  {recordKey: mustBuildObservationRecord(t).RecordKey, identityID: mustBuildObservationRecord(t).Identity.ID},
		recordcontract.FamilyReference:    {recordKey: mustBuildReferenceRecord(t).RecordKey, identityID: mustBuildReferenceRecord(t).Identity.ID},
		recordcontract.FamilyFailure:      {recordKey: mustBuildFailureRecord(t).RecordKey, identityID: mustBuildFailureRecord(t).Identity.ID},
		recordcontract.FamilyVerification: {recordKey: mustBuildVerificationRecord(t).RecordKey, identityID: mustBuildVerificationRecord(t).Identity.ID},
	}

	out := make([]struct {
		Family       string `json:"family"`
		ContractPath string `json:"contractPath"`
		RecordKey    string `json:"recordKey"`
		IdentityID   string `json:"identityId"`
	}, 0, len(recordcontract.Definitions()))
	for _, def := range recordcontract.Definitions() {
		path, ok := recordpackage.RecordContractPath(def.Family)
		if !ok {
			t.Fatalf("RecordContractPath(%q) ok = false, want true", def.Family)
		}
		record := recordsByFamily[def.Family]
		out = append(out, struct {
			Family       string `json:"family"`
			ContractPath string `json:"contractPath"`
			RecordKey    string `json:"recordKey"`
			IdentityID   string `json:"identityId"`
		}{
			Family:       string(def.Family),
			ContractPath: path,
			RecordKey:    record.recordKey,
			IdentityID:   record.identityID,
		})
	}
	return out
}

func shuffledArtifactFiles() []recordpackage.ArtifactFile {
	return []recordpackage.ArtifactFile{
		{ContractPath: "artifacts/files/z.txt", Content: []byte("z")},
		{ContractPath: "artifacts/files/nested/b.txt", Content: []byte("b")},
		{ContractPath: "artifacts/files/a.txt", Content: []byte("a")},
	}
}

func differentlyOrderedArtifactFiles() []recordpackage.ArtifactFile {
	return []recordpackage.ArtifactFile{
		{ContractPath: "artifacts/files/a.txt", Content: []byte("a")},
		{ContractPath: "artifacts/files/z.txt", Content: []byte("z")},
		{ContractPath: "artifacts/files/nested/b.txt", Content: []byte("b")},
	}
}

func shuffledRawEvidenceFiles(t *testing.T) []recordpackage.RawEvidenceFile {
	t.Helper()

	paths := rawEvidenceFilePathsInLayoutOrder()
	if len(paths) < 3 {
		t.Fatalf("need at least three raw evidence file paths, got %#v", paths)
	}

	out := make([]recordpackage.RawEvidenceFile, 0, len(paths))
	for i := len(paths) - 1; i >= 0; i-- {
		out = append(out, recordpackage.RawEvidenceFile{
			ContractPath: paths[i],
			Content:      []byte("raw:" + paths[i]),
		})
	}
	return out
}

func shuffledRawEvidenceFilesWithHandoff(t *testing.T) []recordpackage.RawEvidenceFile {
	t.Helper()

	return append(
		[]recordpackage.RawEvidenceFile{
			{ContractPath: "raw/handoff/zeta/package.json", Content: []byte("raw:raw/handoff/zeta/package.json")},
			{ContractPath: "raw/handoff/alpha/package.json", Content: []byte("raw:raw/handoff/alpha/package.json")},
		},
		shuffledRawEvidenceFiles(t)...,
	)
}

func differentlyOrderedRawEvidenceFilesWithHandoff(t *testing.T) []recordpackage.RawEvidenceFile {
	t.Helper()

	paths := rawEvidenceFilePathsInLayoutOrder()
	out := make([]recordpackage.RawEvidenceFile, 0, len(paths)+2)
	for _, path := range paths {
		out = append(out, recordpackage.RawEvidenceFile{
			ContractPath: path,
			Content:      []byte("raw:" + path),
		})
		if path == recordpackage.RawMetadataContractPath() {
			out = append(out,
				recordpackage.RawEvidenceFile{ContractPath: "raw/handoff/alpha/package.json", Content: []byte("raw:raw/handoff/alpha/package.json")},
				recordpackage.RawEvidenceFile{ContractPath: "raw/handoff/zeta/package.json", Content: []byte("raw:raw/handoff/zeta/package.json")},
			)
		}
	}
	return out
}

func rawEvidenceFilePathsInLayoutOrder() []string {
	var paths []string
	for _, entry := range recordpackage.RawEvidenceEntries() {
		if entry.Kind == recordpackage.EntryKindFile {
			paths = append(paths, entry.ContractPath)
		}
	}
	return paths
}

func rawEvidenceFilePathsWithHandoffInLayoutOrder() []string {
	var paths []string
	for _, entry := range recordpackage.RawEvidenceEntries() {
		switch {
		case entry.Kind == recordpackage.EntryKindFile:
			paths = append(paths, entry.ContractPath)
		case entry.ContractPath == recordpackage.RawHandoffDirectoryContractPath():
			paths = append(paths,
				"raw/handoff/alpha/package.json",
				"raw/handoff/zeta/package.json",
			)
		}
	}
	return paths
}

func definitionFamilies() []recordcontract.Family {
	definitions := recordcontract.Definitions()
	out := make([]recordcontract.Family, len(definitions))
	for i, def := range definitions {
		out[i] = def.Family
	}
	return out
}

func readManifest(t *testing.T, root string) testPackageManifest {
	t.Helper()

	var manifest testPackageManifest
	readJSONFile(t, filepath.Join(root, filepath.FromSlash(recordpackage.PackageManifestContractPath())), &manifest)
	return manifest
}

func readJSONFile(t *testing.T, path string, out any) {
	t.Helper()

	data := readFile(t, path)
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("json.Unmarshal(%q) returned error: %v\n%s", path, err, data)
	}
}

func assertJSONFileWithTrailingNewline(t *testing.T, path string) {
	t.Helper()

	data := readFile(t, path)
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("%q does not end with trailing newline", path)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("%q is not valid JSON: %v", path, err)
	}
}

func assertPackageManifestAndRecordsHaveTrailingNewline(t *testing.T, root string, manifest testPackageManifest) {
	t.Helper()

	assertJSONFileWithTrailingNewline(t, filepath.Join(root, filepath.FromSlash(recordpackage.PackageManifestContractPath())))
	for _, record := range manifest.Records {
		assertJSONFileWithTrailingNewline(t, filepath.Join(root, filepath.FromSlash(record.ContractPath)))
	}
}

func assertPackageFilesDoNotContain(t *testing.T, files map[string][]byte, disallowed ...string) {
	t.Helper()

	for contractPath, data := range files {
		for _, value := range disallowed {
			if value == "" {
				continue
			}
			if bytes.Contains(data, []byte(value)) {
				t.Fatalf("emitted file %q contains temp root path %q", contractPath, value)
			}
		}
	}
}

func readPackageFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()

	out := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = readFile(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%q) returned error: %v", root, err)
	}
	return out
}

func artifactManifestPaths(manifest testPackageManifest) []string {
	out := make([]string, len(manifest.Artifacts))
	for i, artifact := range manifest.Artifacts {
		out[i] = artifact.ContractPath
	}
	return out
}

func rawManifestPaths(manifest testPackageManifest) []string {
	out := make([]string, len(manifest.RawEvidence))
	for i, item := range manifest.RawEvidence {
		out[i] = item.ContractPath
	}
	return out
}

func assertNoOutsideFile(t *testing.T, root string) {
	t.Helper()

	parent := filepath.Dir(root)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir(%q) returned error: %v", parent, err)
	}
	for _, entry := range entries {
		if entry.Name() == filepath.Base(root) {
			continue
		}
		if entry.Name() == "outside" || strings.Contains(entry.Name(), "outside") {
			t.Fatalf("unexpected outside file under %q: %q", parent, entry.Name())
		}
	}
}

func assertNoTempFiles(t *testing.T, root string) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary file left behind: %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%q) returned error: %v", root, err)
	}
}

func dirExists(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		t.Fatalf("Stat(%q) returned error: %v", path, err)
	}
	return info.IsDir()
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		t.Fatalf("Stat(%q) returned error: %v", path, err)
	}
	return !info.IsDir()
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", path, err)
	}
	return data
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", path, err)
	}
}

func mustBuildExecutionRecord(t *testing.T) recordcontract.ExecutionRecord {
	t.Helper()
	started := time.Date(2026, 3, 4, 13, 0, 0, 0, time.UTC)
	ended := time.Date(2026, 3, 4, 13, 5, 0, 0, time.UTC)
	record, err := recordcontract.BuildExecutionRecord(recordcontract.ExecutionRecordInput{
		RecordKey:  "execution-record-1",
		Provenance: testProvenance("parse"),
		Execution: recordcontract.ExecutionSummary{
			Outcome:   recordcontract.ExecutionOutcomeSucceeded,
			PlanID:    "plan-1",
			PlanHash:  "plan-hash-1",
			StartedAt: &started,
			EndedAt:   &ended,
			Duration:  5 * time.Minute,
		},
		Jobs: []recordcontract.ExecutionJobSummary{{
			JobID:      "job-a",
			ProductKey: "product-a",
			Outcome:    recordcontract.ExecutionOutcomeSucceeded,
			Steps: []recordcontract.ExecutionStepSummary{{
				Index:    1,
				StepRef:  "parse",
				Outcome:  recordcontract.ExecutionStepOutcomeSucceeded,
				Attempts: 1,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("BuildExecutionRecord returned error: %v", err)
	}
	return record
}

func mustBuildArtifactRecord(t *testing.T) recordcontract.ArtifactRecord {
	t.Helper()
	record, err := recordcontract.BuildArtifactRecord(recordcontract.ArtifactRecordInput{
		RecordKey:  "artifact-record-1",
		Provenance: testProvenance("export"),
		Artifact: recordcontract.ArtifactSummary{
			Class:          recordcontract.ArtifactClassExecutionOutput,
			Type:           recordcontract.ArtifactTypeSTEP,
			Path:           "out/model.step",
			Filename:       "model.step",
			MimeType:       "model/step",
			SizeBytes:      1024,
			ChecksumSHA256: digestA(),
			DeclaredOutput: recordcontract.ArtifactDeclaredOutput{OutputID: "model-output", Format: "step"},
			Linkage:        recordcontract.ArtifactLinkage{JobID: "job-a", ProductKey: "product-a", StepRef: "export"},
			Evidence:       recordcontract.ArtifactEvidence{SourceKind: "runtime", SourceRef: "runtime://artifact/model.step", DigestSHA256: digestB()},
		},
	})
	if err != nil {
		t.Fatalf("BuildArtifactRecord returned error: %v", err)
	}
	return record
}

func mustBuildObservationRecord(t *testing.T) recordcontract.ObservationRecord {
	t.Helper()
	record, err := recordcontract.BuildObservationRecord(recordcontract.ObservationRecordInput{
		RecordKey:  "observation-record-1",
		Provenance: testProvenance("observe"),
		Observation: recordcontract.ObservationSummary{
			Facts: []recordcontract.ObservationFact{{
				Kind:    recordcontract.ObservationKindParameter,
				Subject: recordcontract.ObservationSubject{ID: "parameter:width"},
				Key:     "width",
				Value:   recordcontract.ObservationValue{Kind: "number", Raw: "10"},
				Linkage: recordcontract.ObservationLinkage{JobID: "job-a", ProductKey: "product-a", StepRef: "observe"},
				Evidence: recordcontract.ObservationEvidence{
					SourceKind:   "runtime",
					SourceRef:    "runtime://observed/width",
					DigestSHA256: digestC(),
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("BuildObservationRecord returned error: %v", err)
	}
	return record
}

func mustBuildReferenceRecord(t *testing.T) recordcontract.ReferenceRecord {
	t.Helper()
	record, err := recordcontract.BuildReferenceRecord(recordcontract.ReferenceRecordInput{
		RecordKey:  "reference-record-1",
		Provenance: testProvenance("resolve"),
		Reference: recordcontract.ReferenceSummary{
			Edges: []recordcontract.ReferenceEdge{{
				Kind:       recordcontract.ReferenceKindExternal,
				Source:     recordcontract.ReferenceEndpoint{ID: "part:body"},
				Target:     recordcontract.ReferenceEndpoint{Path: "library/bolt.step", DigestSHA256: digestD()},
				Role:       "source-model",
				Resolution: recordcontract.ReferenceResolutionResolved,
				Linkage:    recordcontract.ReferenceLinkage{JobID: "job-a", ProductKey: "product-a", StepRef: "resolve"},
				Evidence:   recordcontract.ReferenceEvidence{SourceKind: "runtime", SourceRef: "runtime://references/bolt", DigestSHA256: digestE()},
			}},
		},
	})
	if err != nil {
		t.Fatalf("BuildReferenceRecord returned error: %v", err)
	}
	return record
}

func mustBuildFailureRecord(t *testing.T) recordcontract.FailureRecord {
	t.Helper()
	occurredAt := time.Date(2026, 3, 4, 13, 6, 0, 0, time.UTC)
	record, err := recordcontract.BuildFailureRecord(recordcontract.FailureRecordInput{
		RecordKey:  "failure-record-1",
		Provenance: testProvenance("solve"),
		Failure: recordcontract.FailureSummary{
			Class:      recordcontract.FailureClassExecution,
			Severity:   recordcontract.FailureSeverityError,
			Stage:      recordcontract.FailureStageExecution,
			Code:       "E-SOLVE",
			Message:    "solver failed",
			RetryCount: 1,
			OccurredAt: &occurredAt,
			Linkage:    recordcontract.FailureLinkage{JobID: "job-a", ProductKey: "product-a", StepRef: "solve"},
			Location:   recordcontract.FailureLocation{Path: "model.root.sketch", Component: "sketcher", Field: "constraint"},
			Evidence: []recordcontract.FailureEvidence{{
				SourceKind:   "log",
				SourceRef:    "logs/run.log",
				DigestSHA256: digestF(),
			}},
		},
	})
	if err != nil {
		t.Fatalf("BuildFailureRecord returned error: %v", err)
	}
	return record
}

func mustBuildVerificationRecord(t *testing.T) recordcontract.VerificationRecord {
	t.Helper()
	record, err := recordcontract.BuildVerificationRecord(recordcontract.VerificationRecordInput{
		RecordKey:  "verification-record-1",
		Provenance: testProvenance("verify"),
		Verification: recordcontract.VerificationSummary{
			Outcome:      recordcontract.VerificationOutcomeFail,
			Message:      "component mismatch",
			FailureClass: recordcontract.VerificationFailureClassComponentMismatch,
			Linkage:      recordcontract.VerificationLinkage{JobID: "job-a", ProductKey: "product-a", StepRef: "verify"},
			Categories: []recordcontract.VerificationCategoryResult{{
				Category:     recordcontract.VerificationCategoryComponents,
				Enabled:      true,
				Outcome:      recordcontract.VerificationOutcomeFail,
				Message:      "component mismatch",
				FailureClass: recordcontract.VerificationFailureClassComponentMismatch,
				Evidence:     []recordcontract.VerificationEvidence{{SourceKind: "runtime", SourceRef: "runtime://verification/components", DigestSHA256: digestD()}},
			}},
			Evidence: []recordcontract.VerificationEvidence{{SourceKind: "log", SourceRef: "logs/verify.log", DigestSHA256: digestE()}},
		},
	})
	if err != nil {
		t.Fatalf("BuildVerificationRecord returned error: %v", err)
	}
	return record
}

func testProvenance(stepRef string) recordcontract.Provenance {
	return recordcontract.Provenance{
		SourceRevision: recordcontract.SourceRevisionProvenance{
			RevisionID:   "rev-1",
			AssetID:      "asset-1",
			DigestSHA256: digestA(),
		},
		Inputs: []recordcontract.ProvenanceInput{
			{Kind: "dsl", Identity: "model-a", DigestSHA256: digestC()},
			{Kind: "parameter-table", Identity: "params-a", DigestSHA256: digestD()},
		},
		Plan:    recordcontract.PlanProvenance{PlanID: "plan-1", PlanHash: "plan-hash-1"},
		Linkage: recordcontract.LinkageProvenance{ProductKey: "product-a", JobID: "job-a", StepRef: stepRef},
		Evidence: []recordcontract.EvidenceReference{
			{Kind: "artifact", Ref: "out/model.step", DigestSHA256: digestE()},
			{Kind: "log", Ref: "logs/run.log", DigestSHA256: digestF()},
		},
		Runtime: recordcontract.RuntimeProvenance{ToolID: "parametron", RuntimeID: "runtime-1", Adapter: "freecad"},
	}
}

func digestA() string { return strings.Repeat("a", 64) }
func digestB() string { return strings.Repeat("b", 64) }
func digestC() string { return strings.Repeat("c", 64) }
func digestD() string { return strings.Repeat("d", 64) }
func digestE() string { return strings.Repeat("e", 64) }
func digestF() string { return strings.Repeat("f", 64) }

func TestWriterTestFixturesStayValid(t *testing.T) {
	t.Parallel()

	for _, record := range allTestRecords(t) {
		switch {
		case record.Execution != nil:
			if err := recordcontract.ValidateExecutionRecord(*record.Execution); err != nil {
				t.Fatalf("execution fixture invalid: %v", err)
			}
		case record.Artifact != nil:
			if err := recordcontract.ValidateArtifactRecord(*record.Artifact); err != nil {
				t.Fatalf("artifact fixture invalid: %v", err)
			}
		case record.Observation != nil:
			if err := recordcontract.ValidateObservationRecord(*record.Observation); err != nil {
				t.Fatalf("observation fixture invalid: %v", err)
			}
		case record.Reference != nil:
			if err := recordcontract.ValidateReferenceRecord(*record.Reference); err != nil {
				t.Fatalf("reference fixture invalid: %v", err)
			}
		case record.Failure != nil:
			if err := recordcontract.ValidateFailureRecord(*record.Failure); err != nil {
				t.Fatalf("failure fixture invalid: %v", err)
			}
		case record.Verification != nil:
			if err := recordcontract.ValidateVerificationRecord(*record.Verification); err != nil {
				t.Fatalf("verification fixture invalid: %v", err)
			}
		default:
			t.Fatal("fixture record missing payload")
		}
	}
}
