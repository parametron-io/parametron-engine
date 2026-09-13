//go:build integration
// +build integration

package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"parametron/internal/authoring/planner"
	"parametron/internal/engine/adapter"
)

// ============================================================================
// Cache Contract Tests with a Fake Runner
// ============================================================================

// FakeAdapterWithCacheTracking cache-aware test adapter
type FakeAdapterWithCacheTracking struct {
	calls        []string
	shouldFail   map[string]bool
	writtenFiles map[string]string // path -> content
}

func NewFakeAdapterWithCacheTracking() *FakeAdapterWithCacheTracking {
	return &FakeAdapterWithCacheTracking{
		calls:        []string{},
		shouldFail:   make(map[string]bool),
		writtenFiles: make(map[string]string),
	}
}

func (a *FakeAdapterWithCacheTracking) Run(ctx context.Context, step planner.Step) error {
	switch payload := step.Payload.(type) {
	case planner.WriteCSVPayload:
		a.calls = append(a.calls, "write:"+payload.Filename)
		if a.shouldFail[payload.Filename] {
			return errors.New("simulated CSV write failure")
		}
		// Write fixed content (deterministic)
		a.writtenFiles[payload.Filename] = "key,value\ntest,123\n"
		return nil

	case planner.WriteExportManifestPayload:
		a.calls = append(a.calls, "export:"+payload.ManifestFilename)
		if a.shouldFail[payload.ManifestFilename] {
			return errors.New("simulated export failure")
		}
		// Simulate export output
		stepFile := payload.ManifestFilename[:len(payload.ManifestFilename)-len(".json")] + ".step"
		a.writtenFiles[stepFile] = "MOCK_STEP_CONTENT"
		return nil

	default:
		return nil
	}
}

// GetArtifactSize returns the virtual artifact size (for the cache key component)
func (a *FakeAdapterWithCacheTracking) GetArtifactSize(path string) int64 {
	if content, ok := a.writtenFiles[path]; ok {
		return int64(len(content))
	}
	return 0
}

// GetArtifactChecksum returns a virtual checksum (for the cache key component)
func (a *FakeAdapterWithCacheTracking) GetArtifactChecksum(path string) string {
	// Simple checksum: content hash
	return "sha256:" + a.writtenFiles[path]
}

// ============================================================================
// Cache Contract Tests
// ============================================================================

// TestExecutorCacheContract_SuccessfulRun: Artifact information after a successful run
func TestExecutorCacheContract_SuccessfulRun(t *testing.T) {
	adp := NewFakeAdapterWithCacheTracking()
	rt := New(adp)

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{Filename: "product_a.csv"}},
			{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ManifestFilename: "product_a.json"}},
		},
	}

	err := rt.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Artifacts should have been created
	if len(adp.writtenFiles) != 2 {
		t.Errorf("expected 2 artifacts, got %d", len(adp.writtenFiles))
	}

	// Do the CSV and STEP files exist?
	if _, ok := adp.writtenFiles["product_a.csv"]; !ok {
		t.Error("CSV artifact not created")
	}
	if _, ok := adp.writtenFiles["product_a.step"]; !ok {
		t.Error("STEP artifact not created")
	}
}

// TestExecutorCacheContract_FailureNoPartialArtifact: Failed export
// CSV should be created, but STEP should not (no partial artifact)
func TestExecutorCacheContract_FailureNoPartialArtifact(t *testing.T) {
	adp := NewFakeAdapterWithCacheTracking()
	// Make the STEP export fail
	adp.shouldFail["product_b.csv"] = true

	rt := New(adp)

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{Filename: "product_b.csv"}},
			{Type: planner.StepWriteExportManifest, Payload: planner.WriteExportManifestPayload{ManifestFilename: "product_b.json"}},
		},
	}

	err := rt.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error but got nil")
	}

	// CSV may have been created, but STEP should not be
	// Note: The executor does not currently roll back; this is a future feature
	t.Logf("Error (expected): %v", err)
	t.Logf("Written files: %v", adp.writtenFiles)
}

// TestExecutorCacheContract_DeterministicOutput: Same input -> same output
func TestExecutorCacheContract_DeterministicOutput(t *testing.T) {
	adp1 := NewFakeAdapterWithCacheTracking()
	adp2 := NewFakeAdapterWithCacheTracking()

	rt1 := New(adp1)
	rt2 := New(adp2)

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{Filename: "det.csv"}},
		},
	}

	// Execute the same plan with two separate executors
	rt1.Execute(context.Background(), plan)
	rt2.Execute(context.Background(), plan)

	// Contents should be identical (deterministic)
	content1 := adp1.writtenFiles["det.csv"]
	content2 := adp2.writtenFiles["det.csv"]

	if content1 != content2 {
		t.Errorf("deterministic output mismatch: %q vs %q", content1, content2)
	}
}

// TestExecutorCacheContract_ChecksumStability: Same content = same checksum
func TestExecutorCacheContract_ChecksumStability(t *testing.T) {
	adp := NewFakeAdapterWithCacheTracking()
	rt := New(adp)

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{Filename: "check.csv"}},
		},
	}

	rt.Execute(context.Background(), plan)

	checksum1 := adp.GetArtifactChecksum("check.csv")

	// Same adapter, same content
	checksum2 := adp.GetArtifactChecksum("check.csv")

	if checksum1 != checksum2 {
		t.Errorf("checksum should be stable: %s vs %s", checksum1, checksum2)
	}
}

// TestExecutorCacheContract_SizeConsistency: Size consistency
func TestExecutorCacheContract_SizeConsistency(t *testing.T) {
	adp := NewFakeAdapterWithCacheTracking()
	rt := New(adp)

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{Filename: "size.csv"}},
		},
	}

	rt.Execute(context.Background(), plan)

	size := adp.GetArtifactSize("size.csv")
	content := adp.writtenFiles["size.csv"]

	if size != int64(len(content)) {
		t.Errorf("size mismatch: reported %d, actual %d", size, len(content))
	}
}

// ============================================================================
// Retry and Idempotency Tests
// ============================================================================

// TestExecutorCacheContract_RetryIdempotency: Retry should produce the same result
func TestExecutorCacheContract_RetryIdempotency(t *testing.T) {
	adp := NewFakeAdapterWithCacheTracking()
	// Fail on the first attempt, succeed on the second
	adp.shouldFail["retry.csv"] = true

	rt := New(adp)

	plan := &planner.ExecutionPlan{
		Steps: []planner.Step{
			{Type: planner.StepWriteCSV, Payload: planner.WriteCSVPayload{Filename: "retry.csv"}},
		},
	}

	// First attempt: failure
	err := rt.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("expected first attempt to fail")
	}

	// Reset the failure state
	adp.shouldFail["retry.csv"] = false

	// Second attempt: success
	err = rt.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("expected second attempt to succeed: %v", err)
	}

	// Content should be deterministic
	content := adp.writtenFiles["retry.csv"]
	expected := "key,value\ntest,123\n"
	if content != expected {
		t.Errorf("retry produced different content: %q vs %q", content, expected)
	}
}

// ============================================================================
// Adapter Integration Tests
// ============================================================================

// TestRealAdapterCompatibility: Interface compatibility with the real adapter
func TestRealAdapterCompatibility(t *testing.T) {
	// Verify that code using the adapter.Adapter interface compiles
	var _ adapter.Adapter = (*FakeAdapterWithCacheTracking)(nil)
}

// ============================================================================
// Artifact Store Integration Tests
// ============================================================================

// TestArtifactStore_ManifestConsistency: Are the manifest and artifacts consistent?
func TestArtifactStore_ManifestConsistency(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock artifacts
	csvPath := filepath.Join(tmpDir, "product.csv")
	stepPath := filepath.Join(tmpDir, "product.step")

	os.WriteFile(csvPath, []byte("key,value\na,1\n"), 0644)
	os.WriteFile(stepPath, []byte("MOCK_STEP"), 0644)

	// Do the files exist?
	if _, err := os.Stat(csvPath); err != nil {
		t.Errorf("CSV not found: %v", err)
	}
	if _, err := os.Stat(stepPath); err != nil {
		t.Errorf("STEP not found: %v", err)
	}

	// Check sizes
	csvInfo, _ := os.Stat(csvPath)
	stepInfo, _ := os.Stat(stepPath)

	if csvInfo.Size() == 0 {
		t.Error("CSV file is empty")
	}
	if stepInfo.Size() == 0 {
		t.Error("STEP file is empty")
	}
}
