package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeterministicID(t *testing.T) {
	id1 := DeterministicID("job-a", "p1", "s1", ArtifactClassExecutionOutput, ArtifactTypeSTEP, "model.step", "")
	id2 := DeterministicID("job-a", "p1", "s1", ArtifactClassExecutionOutput, ArtifactTypeSTEP, "model.step", "")
	if id1 != id2 {
		t.Fatalf("expected stable id, got %q != %q", id1, id2)
	}

	idChecksumA := DeterministicID("job-a", "p1", "s1", ArtifactClassExecutionOutput, ArtifactTypeSTEP, "model.step", "checksum-a")
	idChecksumB := DeterministicID("job-a", "p1", "s1", ArtifactClassExecutionOutput, ArtifactTypeSTEP, "model.step", "checksum-b")
	if idChecksumA == idChecksumB {
		t.Fatalf("expected checksum-driven ids to differ, got %q", idChecksumA)
	}

	idOtherJob := DeterministicID("job-b", "p1", "s1", ArtifactClassExecutionOutput, ArtifactTypeSTEP, "model.step", "checksum-a")
	if idChecksumA == idOtherJob {
		t.Fatalf("expected job-scoped ids to differ, got %q", idChecksumA)
	}

	idVerified := DeterministicID("job-a", "p1", "s1", ArtifactClassVerified, ArtifactTypeSTEP, "model.step", "checksum-a")
	if idChecksumA == idVerified {
		t.Fatalf("expected artifact classes to produce distinct ids, got %q", idChecksumA)
	}
	if got := DeterministicID("job-a", "p1", "s1", "", ArtifactTypeSTEP, "model.step", "checksum-a"); got != idChecksumA {
		t.Fatalf("empty class should default to execution_output id: got %q want %q", got, idChecksumA)
	}
}

func TestArtifactClassHelpers(t *testing.T) {
	if got := DefaultArtifactClass(""); got != ArtifactClassExecutionOutput {
		t.Fatalf("empty class default = %q, want %q", got, ArtifactClassExecutionOutput)
	}
	if !ValidArtifactClass(ArtifactClassExecutionOutput) {
		t.Fatal("execution_output should be valid")
	}
	if !ValidArtifactClass(ArtifactClassVerified) {
		t.Fatal("verified_artifact should be valid")
	}
	if !ValidArtifactClass("  execution_output  ") {
		t.Fatal("surrounding whitespace should normalize before validation")
	}
	if ValidArtifactClass("other") {
		t.Fatal("unknown class should be invalid")
	}
}

func TestPutCopiesFileAndSetsMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "store")
	store := NewFileSystemStore(baseDir)

	srcPath := filepath.Join(tmpDir, "input.csv")
	content := []byte("a,b\n1,2\n")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	createdAt := time.Date(2026, 2, 25, 12, 0, 0, 0, time.UTC)
	meta, err := store.Put(context.Background(), srcPath, Artifact{
		Type:      ArtifactTypeCSV,
		ProductID: "desk",
		StepID:    "WriteCSV",
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("Put returned error: %v", err)
	}

	if meta.Type != ArtifactTypeCSV {
		t.Fatalf("unexpected type: %s", meta.Type)
	}
	if meta.Class != ArtifactClassExecutionOutput {
		t.Fatalf("unexpected class: got %q want %q", meta.Class, ArtifactClassExecutionOutput)
	}
	if meta.Filename != "input.csv" {
		t.Fatalf("unexpected filename: %s", meta.Filename)
	}
	if meta.Path == "" || !strings.HasPrefix(meta.Path, "files/") {
		t.Fatalf("expected store-relative path under files/, got %q", meta.Path)
	}
	if meta.SizeBytes != int64(len(content)) {
		t.Fatalf("unexpected size: got %d want %d", meta.SizeBytes, len(content))
	}
	if meta.ProductID != "desk" || meta.StepID != "WriteCSV" {
		t.Fatalf("unexpected identity metadata: product=%q step=%q", meta.ProductID, meta.StepID)
	}
	if !meta.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected createdAt preserved, got %s", meta.CreatedAt)
	}

	sum := sha256.Sum256(content)
	expectedChecksum := hex.EncodeToString(sum[:])
	if meta.ChecksumSHA256 != expectedChecksum {
		t.Fatalf("unexpected checksum: got %q want %q", meta.ChecksumSHA256, expectedChecksum)
	}
	expectedID := DeterministicID("", "desk", "WriteCSV", ArtifactClassExecutionOutput, ArtifactTypeCSV, "input.csv", expectedChecksum)
	if meta.ID != expectedID {
		t.Fatalf("unexpected id: got %q want %q", meta.ID, expectedID)
	}

	storedPath, ok := store.GetPath(meta.ID)
	if !ok {
		t.Fatalf("expected artifact path for id %s", meta.ID)
	}
	data, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatalf("failed to read stored artifact: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("stored artifact content mismatch: got %q want %q", data, content)
	}
}

func TestWriteManifestDeterministicOrdering(t *testing.T) {
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "store")
	store := NewFileSystemStore(baseDir)

	srcA := filepath.Join(tmpDir, "b.step")
	srcB := filepath.Join(tmpDir, "a.json")
	if err := os.WriteFile(srcA, []byte("step"), 0644); err != nil {
		t.Fatalf("failed to write srcA: %v", err)
	}
	if err := os.WriteFile(srcB, []byte(`{"ok":true}`), 0644); err != nil {
		t.Fatalf("failed to write srcB: %v", err)
	}

	// Insert in reverse semantic order; manifest output must still be deterministic.
	if _, err := store.Put(context.Background(), srcA, Artifact{
		Type:      ArtifactTypeSTEP,
		ProductID: "p2",
		StepID:    "RunCADRuntime",
		CreatedAt: time.Date(2026, 2, 25, 12, 0, 2, 0, time.UTC),
	}); err != nil {
		t.Fatalf("failed to put srcA: %v", err)
	}
	if _, err := store.Put(context.Background(), srcB, Artifact{
		Type:      ArtifactTypeJSON,
		ProductID: "p1",
		StepID:    "WriteJSON",
		CreatedAt: time.Date(2026, 2, 25, 12, 0, 1, 0, time.UTC),
	}); err != nil {
		t.Fatalf("failed to put srcB: %v", err)
	}

	if err := store.WriteManifest(); err != nil {
		t.Fatalf("WriteManifest returned error: %v", err)
	}
	manifestPath := filepath.Join(baseDir, "manifest.json")
	first, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	if err := store.WriteManifest(); err != nil {
		t.Fatalf("second WriteManifest returned error: %v", err)
	}
	second, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest after second write: %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("manifest output is not deterministic across repeated writes")
	}
	if !strings.Contains(string(first), `"class": "execution_output"`) {
		t.Fatalf("manifest must include explicit execution_output class, got:\n%s", string(first))
	}

	idxA := strings.Index(string(first), "\"productId\": \"p1\"")
	idxB := strings.Index(string(first), "\"productId\": \"p2\"")
	if idxA < 0 || idxB < 0 {
		t.Fatalf("expected both artifacts in manifest, got:\n%s", string(first))
	}
	if idxA > idxB {
		t.Fatalf("expected deterministic sorted ordering in manifest")
	}
}

func TestFileSystemStore_CrossClassManifestDeterminism(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	productDir := filepath.Join(dir, "products", "widget")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatal(err)
	}

	srcPath := filepath.Join(productDir, "release.step")
	content := []byte("ISO-10303-21;")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	createdAt := time.Date(2026, 2, 25, 12, 0, 0, 0, time.UTC)
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	executionID1 := DeterministicID("job-cross-class", "widget", "verify", ArtifactClassExecutionOutput, ArtifactTypeSTEP, "release.step", checksum)
	executionID2 := DeterministicID("job-cross-class", "widget", "verify", ArtifactClassExecutionOutput, ArtifactTypeSTEP, "release.step", checksum)
	verifiedID1 := DeterministicID("job-cross-class", "widget", "verify", ArtifactClassVerified, ArtifactTypeSTEP, "release.step", checksum)
	verifiedID2 := DeterministicID("job-cross-class", "widget", "verify", ArtifactClassVerified, ArtifactTypeSTEP, "release.step", checksum)

	if executionID1 != executionID2 {
		t.Fatalf("execution_output deterministic ID must be stable, got %q != %q", executionID1, executionID2)
	}
	if verifiedID1 != verifiedID2 {
		t.Fatalf("verified_artifact deterministic ID must be stable, got %q != %q", verifiedID1, verifiedID2)
	}
	if executionID1 == verifiedID1 {
		t.Fatalf("artifact class must affect deterministic ID, got shared id %q", executionID1)
	}

	executionArtifact, err := store.RecordExisting(context.Background(), srcPath, Artifact{
		JobID:     "job-cross-class",
		ProductID: "widget",
		StepID:    "verify",
		Class:     ArtifactClassExecutionOutput,
		Type:      ArtifactTypeSTEP,
		Filename:  "release.step",
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("failed to record execution artifact: %v", err)
	}

	verifiedArtifact, err := store.RecordExisting(context.Background(), srcPath, Artifact{
		JobID:     "job-cross-class",
		ProductID: "widget",
		StepID:    "verify",
		Class:     ArtifactClassVerified,
		Type:      ArtifactTypeSTEP,
		Filename:  "release.step",
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("failed to record verified artifact: %v", err)
	}

	if executionArtifact.ID != executionID1 {
		t.Fatalf("execution artifact ID = %q, want %q", executionArtifact.ID, executionID1)
	}
	if verifiedArtifact.ID != verifiedID1 {
		t.Fatalf("verified artifact ID = %q, want %q", verifiedArtifact.ID, verifiedID1)
	}

	list1, err := store.List()
	if err != nil {
		t.Fatalf("first List returned error: %v", err)
	}
	list2, err := store.List()
	if err != nil {
		t.Fatalf("second List returned error: %v", err)
	}

	if len(list1) != 2 || len(list2) != 2 {
		t.Fatalf("expected two cross-class artifacts, got len(list1)=%d len(list2)=%d", len(list1), len(list2))
	}
	for i := range list1 {
		if list1[i].ID != list2[i].ID {
			t.Fatalf("List ordering must be stable across calls: list1[%d]=%q list2[%d]=%q", i, list1[i].ID, i, list2[i].ID)
		}
	}

	if list1[0].Class != ArtifactClassExecutionOutput || list1[1].Class != ArtifactClassVerified {
		t.Fatalf("expected deterministic class ordering execution_output before verified_artifact, got %+v", list1)
	}

	if err := store.WriteManifest(); err != nil {
		t.Fatalf("first WriteManifest returned error: %v", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	first, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read first manifest: %v", err)
	}

	if err := store.WriteManifest(); err != nil {
		t.Fatalf("second WriteManifest returned error: %v", err)
	}
	second, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read second manifest: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("manifest must be byte-stable across repeated writes")
	}

	manifest := string(first)
	if !strings.Contains(manifest, `"class": "execution_output"`) {
		t.Fatalf("manifest must include explicit execution_output class, got:\n%s", manifest)
	}
	if !strings.Contains(manifest, `"class": "verified_artifact"`) {
		t.Fatalf("manifest must include explicit verified_artifact class, got:\n%s", manifest)
	}

	executionObject := "{\n      \"id\": \"" + executionArtifact.ID + "\",\n      \"class\": \"execution_output\",\n      \"type\": \"step\""
	verifiedObject := "{\n      \"id\": \"" + verifiedArtifact.ID + "\",\n      \"class\": \"verified_artifact\",\n      \"type\": \"step\""
	if !strings.Contains(manifest, executionObject) {
		t.Fatalf("manifest must serialize execution artifact with stable field order, got:\n%s", manifest)
	}
	if !strings.Contains(manifest, verifiedObject) {
		t.Fatalf("manifest must serialize verified artifact with stable field order, got:\n%s", manifest)
	}

	executionIndex := strings.Index(manifest, `"class": "execution_output"`)
	verifiedIndex := strings.Index(manifest, `"class": "verified_artifact"`)
	if executionIndex < 0 || verifiedIndex < 0 {
		t.Fatalf("expected both artifact classes in manifest, got:\n%s", manifest)
	}
	if executionIndex > verifiedIndex {
		t.Fatalf("manifest class ordering must be deterministic across class tie-breaker, got:\n%s", manifest)
	}
}

func TestSortArtifacts_ProductStepPathOrdering(t *testing.T) {
	artifacts := []Artifact{
		{ID: "3", ProductID: "B", StepID: "1", Path: "products/B/out.step"},
		{ID: "1", ProductID: "A", StepID: "1", Path: "products/A/z.step"},
		{ID: "2", ProductID: "A", StepID: "1", Path: "products/A/a.csv"},
		{ID: "4", ProductID: "A", StepID: "0", Path: "products/A/in.csv"},
	}

	sortArtifacts(artifacts)

	got := []string{
		artifacts[0].Path,
		artifacts[1].Path,
		artifacts[2].Path,
		artifacts[3].Path,
	}
	want := []string{
		"products/A/in.csv", // step 0 first
		"products/A/a.csv",  // then step 1 sorted by path
		"products/A/z.step",
		"products/B/out.step",
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected sorted order at %d: got=%v want=%v", i, got, want)
		}
	}
}

func TestSortArtifacts_ClassTieBreaker(t *testing.T) {
	artifacts := []Artifact{
		{ID: "2", ProductID: "A", StepID: "1", Class: ArtifactClassVerified, Path: "same", Filename: "same.step", Type: ArtifactTypeSTEP},
		{ID: "1", ProductID: "A", StepID: "1", Class: ArtifactClassExecutionOutput, Path: "same", Filename: "same.step", Type: ArtifactTypeSTEP},
	}

	sortArtifacts(artifacts)

	if artifacts[0].Class != ArtifactClassExecutionOutput || artifacts[1].Class != ArtifactClassVerified {
		t.Fatalf("expected class tie-breaker ordering, got %+v", artifacts)
	}
}

func TestArtifactStore_Put_CancelDoesNotLeavePartialFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	src := filepath.Join(dir, "big.bin")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(50 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := store.Put(ctx, src, Artifact{
			Type:      ArtifactTypeUnknown,
			Filename:  "big.bin",
			ProductID: "P",
			StepID:    "S",
		})
		done <- err
	}()

	time.Sleep(5 * time.Millisecond)
	cancel()

	err = <-done
	if err == nil {
		t.Fatalf("expected error (context cancel), got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	filesDir := filepath.Join(dir, "files")
	if _, statErr := os.Stat(filesDir); statErr == nil {
		entries, _ := os.ReadDir(filesDir)
		if len(entries) != 0 {
			t.Fatalf("expected no leftover files in store after cancel; got %d", len(entries))
		}
	}
}

func TestManifest_Write_IsIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := store.Put(context.Background(), src, Artifact{
		Type:      ArtifactTypeLog,
		Filename:  "a.txt",
		ProductID: "P",
		StepID:    "S",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.WriteManifest(); err != nil {
		t.Fatal(err)
	}
	b1, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}

	if err := store.WriteManifest(); err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(b1, b2) {
		t.Fatalf("manifest must be identical across repeated writes")
	}
}

func TestArtifactStore_Put_RejectsPathTraversalFilename(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	src := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := store.Put(context.Background(), src, Artifact{
		Type:      ArtifactTypeUnknown,
		Filename:  "../../pwned.bin",
		ProductID: "P",
		StepID:    "S",
	})
	if err == nil {
		t.Fatalf("expected error for path traversal filename")
	}
}

func TestArtifactStore_Put_SameContent_IsIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	src := filepath.Join(dir, "d.bin")
	if err := os.WriteFile(src, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}

	a1, err := store.Put(context.Background(), src, Artifact{
		Type:      ArtifactTypeUnknown,
		Filename:  "d.bin",
		ProductID: "P",
		StepID:    "S",
	})
	if err != nil {
		t.Fatal(err)
	}

	a2, err := store.Put(context.Background(), src, Artifact{
		Type:      ArtifactTypeUnknown,
		Filename:  "d.bin",
		ProductID: "P",
		StepID:    "S",
	})
	if err != nil {
		t.Fatal(err)
	}

	if a1.ID != a2.ID {
		t.Fatalf("expected same deterministic ID for same content; got %s vs %s", a1.ID, a2.ID)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, a := range list {
		if seen[a.ID] {
			t.Fatalf("duplicate artifact in List() for same ID: %s", a.ID)
		}
		seen[a.ID] = true
	}
}

func TestArtifactStore_Put_PreservesExplicitArtifactClass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	src := filepath.Join(dir, "release.step")
	if err := os.WriteFile(src, []byte("ISO-10303-21;"), 0644); err != nil {
		t.Fatal(err)
	}

	a, err := store.Put(context.Background(), src, Artifact{
		Type:      ArtifactTypeSTEP,
		Class:     ArtifactClassVerified,
		Filename:  "release.step",
		ProductID: "P",
		StepID:    "S",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Class != ArtifactClassVerified {
		t.Fatalf("class = %q, want %q", a.Class, ArtifactClassVerified)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Class != ArtifactClassVerified {
		t.Fatalf("stored artifact class not preserved: %+v", list)
	}

	if err := store.WriteManifest(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"class": "verified_artifact"`) {
		t.Fatalf("manifest must include explicit verified_artifact class, got:\n%s", string(data))
	}
}

func TestArtifactStore_Put_RejectsUnknownArtifactClass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	src := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := store.Put(context.Background(), src, Artifact{
		Type:      ArtifactTypeUnknown,
		Class:     "mystery",
		Filename:  "x.bin",
		ProductID: "P",
		StepID:    "S",
	})
	if err == nil {
		t.Fatal("expected invalid artifact class error")
	}
	if got, want := err.Error(), `invalid artifact class: "mystery"`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}

	list, listErr := store.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(list) != 0 {
		t.Fatalf("unknown class must not register anything, got %+v", list)
	}
}

func TestRecordExisting_ComputesChecksumWithoutCopying(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	subDir := filepath.Join(dir, "products", "widget")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := []byte("header\n42\n")
	srcPath := filepath.Join(subDir, "widget.csv")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSystemStore(dir)
	a, err := store.RecordExisting(context.Background(), srcPath, Artifact{
		Type:      ArtifactTypeCSV,
		ProductID: "widget",
		StepID:    "0",
		Filename:  "widget.csv",
	})
	if err != nil {
		t.Fatalf("RecordExisting returned error: %v", err)
	}

	if a.Filename != "widget.csv" {
		t.Fatalf("unexpected filename: %s", a.Filename)
	}
	if a.SizeBytes != int64(len(content)) {
		t.Fatalf("unexpected size: got %d want %d", a.SizeBytes, len(content))
	}
	if a.ChecksumSHA256 == "" {
		t.Fatal("expected checksum to be set")
	}
	if a.Path != "products/widget/widget.csv" {
		t.Fatalf("expected store-relative path, got %q", a.Path)
	}
	if a.ID == "" {
		t.Fatal("expected ID to be set")
	}
	if a.Class != ArtifactClassExecutionOutput {
		t.Fatalf("class = %q, want %q", a.Class, ArtifactClassExecutionOutput)
	}

	// Original file must still exist (not moved or renamed).
	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("original file should still exist: %v", err)
	}

	// files/ sub-directory must NOT be created by RecordExisting.
	if _, err := os.Stat(filepath.Join(dir, "files")); err == nil {
		t.Fatal("RecordExisting must not create the files/ directory")
	}
}

func TestRecordExisting_PreservesExplicitArtifactClass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	subDir := filepath.Join(dir, "products", "widget")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	srcPath := filepath.Join(subDir, "widget.step")
	if err := os.WriteFile(srcPath, []byte("ISO-10303-21;"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSystemStore(dir)
	a, err := store.RecordExisting(context.Background(), srcPath, Artifact{
		Type:      ArtifactTypeSTEP,
		Class:     ArtifactClassVerified,
		ProductID: "widget",
		StepID:    "2",
		Filename:  "widget.step",
	})
	if err != nil {
		t.Fatalf("RecordExisting returned error: %v", err)
	}
	if a.Class != ArtifactClassVerified {
		t.Fatalf("class = %q, want %q", a.Class, ArtifactClassVerified)
	}
}

func TestRecordExisting_RejectsUnknownArtifactClass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	subDir := filepath.Join(dir, "products", "widget")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	srcPath := filepath.Join(subDir, "widget.step")
	if err := os.WriteFile(srcPath, []byte("ISO-10303-21;"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSystemStore(dir)
	_, err := store.RecordExisting(context.Background(), srcPath, Artifact{
		Type:      ArtifactTypeSTEP,
		Class:     "mystery",
		ProductID: "widget",
		StepID:    "2",
		Filename:  "widget.step",
	})
	if err == nil {
		t.Fatal("expected invalid artifact class error")
	}
	if got, want := err.Error(), `invalid artifact class: "mystery"`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}

	list, listErr := store.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(list) != 0 {
		t.Fatalf("unknown class must not register anything, got %+v", list)
	}
}

func TestRecordExisting_RejectsPathOutsideStore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outside := t.TempDir()

	externalFile := filepath.Join(outside, "sneaky.csv")
	if err := os.WriteFile(externalFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSystemStore(dir)
	_, err := store.RecordExisting(context.Background(), externalFile, Artifact{
		Type:      ArtifactTypeCSV,
		ProductID: "p",
		StepID:    "s",
	})
	if err == nil {
		t.Fatal("expected error for path outside store, got nil")
	}
}

func TestRecordExisting_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(sub, "out.csv")
	if err := os.WriteFile(src, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileSystemStore(dir)
	a1, err := store.RecordExisting(context.Background(), src, Artifact{Type: ArtifactTypeCSV, ProductID: "P", StepID: "S"})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.RecordExisting(context.Background(), src, Artifact{Type: ArtifactTypeCSV, ProductID: "P", StepID: "S"})
	if err != nil {
		t.Fatal(err)
	}
	if a1.ID != a2.ID {
		t.Fatalf("expected same ID on repeated RecordExisting; got %s vs %s", a1.ID, a2.ID)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 artifact in store, got %d", len(list))
	}
}

func TestRecordExistingBatch_SuccessCommitsAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	productDir := filepath.Join(dir, "products", "widget")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatal(err)
	}

	csvPath := filepath.Join(productDir, "widget.csv")
	if err := os.WriteFile(csvPath, []byte("value\n42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stepPath := filepath.Join(productDir, "widget.step")
	if err := os.WriteFile(stepPath, []byte("ISO-10303-21;"), 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := store.RecordExistingBatch(context.Background(), []ExistingRecord{
		{
			AbsPath: stepPath,
			Meta: Artifact{
				Type:      ArtifactTypeSTEP,
				ProductID: "widget",
				StepID:    "2",
				Filename:  "widget.step",
			},
		},
		{
			AbsPath: csvPath,
			Meta: Artifact{
				Type:      ArtifactTypeCSV,
				ProductID: "widget",
				StepID:    "0",
				Filename:  "widget.csv",
			},
		},
	})
	if err != nil {
		t.Fatalf("RecordExistingBatch returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("registered artifacts = %d, want 2", len(items))
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("store artifact count = %d, want 2", len(list))
	}
	if list[0].Filename != "widget.csv" || list[1].Filename != "widget.step" {
		t.Fatalf("unexpected deterministic ordering after batch success: %+v", list)
	}

	sum := sha256.Sum256([]byte("value\n42\n"))
	expectedChecksum := hex.EncodeToString(sum[:])
	if items[1].ChecksumSHA256 != expectedChecksum {
		t.Fatalf("csv checksum = %q, want %q", items[1].ChecksumSHA256, expectedChecksum)
	}
	expectedID := DeterministicID("", "widget", "0", ArtifactClassExecutionOutput, ArtifactTypeCSV, "widget.csv", expectedChecksum)
	if items[1].ID != expectedID {
		t.Fatalf("csv ID = %q, want %q", items[1].ID, expectedID)
	}
	if items[1].Path != "products/widget/widget.csv" {
		t.Fatalf("csv path = %q, want %q", items[1].Path, "products/widget/widget.csv")
	}
}

func TestRecordExistingBatch_FailureLeavesNoObservableEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	productDir := filepath.Join(dir, "products", "widget")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatal(err)
	}

	validPath := filepath.Join(productDir, "widget.csv")
	if err := os.WriteFile(validPath, []byte("value\n42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "widget.step")
	if err := os.WriteFile(outsidePath, []byte("ISO-10303-21;"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := store.RecordExistingBatch(context.Background(), []ExistingRecord{
		{
			AbsPath: validPath,
			Meta: Artifact{
				Type:      ArtifactTypeCSV,
				ProductID: "widget",
				StepID:    "0",
				Filename:  "widget.csv",
			},
		},
		{
			AbsPath: outsidePath,
			Meta: Artifact{
				Type:      ArtifactTypeSTEP,
				ProductID: "widget",
				StepID:    "2",
				Filename:  "widget.step",
			},
		},
	})
	if err == nil {
		t.Fatal("expected RecordExistingBatch to fail")
	}

	var batchErr *RecordExistingBatchError
	if !errors.As(err, &batchErr) {
		t.Fatalf("expected RecordExistingBatchError, got %T", err)
	}
	if batchErr.Index != 1 {
		t.Fatalf("batch error index = %d, want 1", batchErr.Index)
	}

	list, listErr := store.List()
	if listErr != nil {
		t.Fatalf("List returned error: %v", listErr)
	}
	if len(list) != 0 {
		t.Fatalf("store artifact count after batch failure = %d, want 0", len(list))
	}
}

func TestRecordExistingBatch_RepeatedFailureLeavesStoreUnchanged(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	productDir := filepath.Join(dir, "products", "widget")
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatal(err)
	}

	validPath := filepath.Join(productDir, "widget.csv")
	if err := os.WriteFile(validPath, []byte("value\n42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "widget.step")
	if err := os.WriteFile(outsidePath, []byte("ISO-10303-21;"), 0o644); err != nil {
		t.Fatal(err)
	}

	records := []ExistingRecord{
		{
			AbsPath: validPath,
			Meta: Artifact{
				Type:      ArtifactTypeCSV,
				ProductID: "widget",
				StepID:    "0",
				Filename:  "widget.csv",
			},
		},
		{
			AbsPath: outsidePath,
			Meta: Artifact{
				Type:      ArtifactTypeSTEP,
				ProductID: "widget",
				StepID:    "2",
				Filename:  "widget.step",
			},
		},
	}

	first, firstErr := store.RecordExistingBatch(context.Background(), records)
	second, secondErr := store.RecordExistingBatch(context.Background(), records)
	if firstErr == nil || secondErr == nil {
		t.Fatal("expected repeated RecordExistingBatch calls to fail")
	}
	if first != nil || second != nil {
		t.Fatalf("failed batch registrations must not return partial items: first=%v second=%v", first, second)
	}

	var firstBatchErr *RecordExistingBatchError
	if !errors.As(firstErr, &firstBatchErr) {
		t.Fatalf("expected first error to be RecordExistingBatchError, got %T", firstErr)
	}
	var secondBatchErr *RecordExistingBatchError
	if !errors.As(secondErr, &secondBatchErr) {
		t.Fatalf("expected second error to be RecordExistingBatchError, got %T", secondErr)
	}
	if firstBatchErr.Index != 1 || secondBatchErr.Index != 1 {
		t.Fatalf("repeated failures must select the same item, got %d and %d", firstBatchErr.Index, secondBatchErr.Index)
	}
	if firstErr.Error() != secondErr.Error() {
		t.Fatalf("repeated failures must keep the same error message, got %q and %q", firstErr.Error(), secondErr.Error())
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("store must remain empty after repeated batch failures, got %+v", list)
	}
}

func TestRecordExistingBatch_SameShapeDifferentJobsRemainDistinct(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	jobADir := filepath.Join(dir, "jobs", "job-a")
	jobBDir := filepath.Join(dir, "jobs", "job-b")
	if err := os.MkdirAll(jobADir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobBDir, 0o755); err != nil {
		t.Fatal(err)
	}

	jobAPath := filepath.Join(jobADir, "widget.step")
	jobBPath := filepath.Join(jobBDir, "widget.step")
	content := []byte("ISO-10303-21; runtime test")
	if err := os.WriteFile(jobAPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobBPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := store.RecordExistingBatch(context.Background(), []ExistingRecord{
		{
			AbsPath: jobAPath,
			Meta: Artifact{
				JobID:     "job-a",
				ProductID: "widget",
				StepID:    "2",
				Type:      ArtifactTypeSTEP,
				Filename:  "widget.step",
			},
		},
		{
			AbsPath: jobBPath,
			Meta: Artifact{
				JobID:     "job-b",
				ProductID: "widget",
				StepID:    "2",
				Type:      ArtifactTypeSTEP,
				Filename:  "widget.step",
			},
		},
	})
	if err != nil {
		t.Fatalf("RecordExistingBatch returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("registered artifacts = %d, want 2", len(items))
	}
	if items[0].ID == items[1].ID {
		t.Fatalf("same-shape artifacts from different jobs must not share an ID: %q", items[0].ID)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("store artifact count = %d, want 2", len(list))
	}
	if list[0].JobID == list[1].JobID {
		t.Fatalf("expected distinct job ownership in store list, got %+v", list)
	}
}

func TestArtifactStore_List_StableOrdering_IndependentOfCreatedAt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewFileSystemStore(dir)

	src1 := filepath.Join(dir, "a.bin")
	src2 := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(src1, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src2, []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	_, err := store.Put(context.Background(), src1, Artifact{
		Type:      ArtifactTypeUnknown,
		Filename:  "a.bin",
		ProductID: "P",
		StepID:    "S",
		CreatedAt: t2, // intentionally later
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Put(context.Background(), src2, Artifact{
		Type:      ArtifactTypeUnknown,
		Filename:  "b.bin",
		ProductID: "P",
		StepID:    "S",
		CreatedAt: t1, // intentionally earlier
	})
	if err != nil {
		t.Fatal(err)
	}

	l1, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	l2, err := store.List()
	if err != nil {
		t.Fatal(err)
	}

	if len(l1) != len(l2) {
		t.Fatalf("list length mismatch")
	}
	for i := range l1 {
		if l1[i].ID != l2[i].ID {
			t.Fatalf("List ordering must be stable across calls")
		}
	}

	got := map[string]time.Time{}
	for _, a := range l1 {
		got[a.Filename] = a.CreatedAt
	}

	if !got["a.bin"].Equal(t2) || !got["b.bin"].Equal(t1) {
		t.Fatalf("CreatedAt must be preserved when provided")
	}
}
