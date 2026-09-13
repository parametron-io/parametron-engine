package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"parametron/internal/authoring/planner"
)

// Task 14 permanent regression coverage for the aligned smoke submission
// generator's real CLI entrypoint. main_test.go already exercises
// buildSubmission in-process; this file instead builds and runs the actual
// parametron-engine-smoke binary as a subprocess (exactly as
// scripts/cad_runtime_integration_proof.py's aligned_submission helper
// does), proving the real flag-parsing/stdout-encoding contract the proof
// harness depends on, not just the underlying Go functions.

func buildSmokeBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parametron-engine-smoke")
	cmd := exec.Command("go", "build", "-o", path, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build smoke binary: %v\n%s", err, stderr.String())
	}
	return path
}

func runSmokeBinary(t *testing.T, binary string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run smoke binary %v: %v\n%s", args, err, stderr.String())
	}
	return stdout.Bytes()
}

func TestSmokeSubmission_UsesProductionContracts(t *testing.T) {
	binary := buildSmokeBinary(t)
	sourceModel := filepath.Join(t.TempDir(), "box.FCStd")
	out := runSmokeBinary(t, binary, "--source-model", sourceModel, "--product", "box")

	var decoded submissionRequest
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, out)
	}
	h := decoded.Handoff
	if h.CADRuntime == nil || h.CADRuntime.Adapter != "freecad" {
		t.Fatalf("expected a freecad CADRuntime payload, got %#v", h.CADRuntime)
	}
	if !filepath.IsAbs(h.Manifest.Inputs.SourceModel) || h.Manifest.Inputs.SourceModel != sourceModel {
		t.Fatalf("expected the absolute source model to round-trip exactly, got %q", h.Manifest.Inputs.SourceModel)
	}
	if h.Manifest.SourceDocument != "source/box.FCStd" {
		t.Fatalf("expected the native source-document intent to be source/box.FCStd, got %q", h.Manifest.SourceDocument)
	}
	if len(h.Manifest.Outputs) != 1 || h.Manifest.Outputs[0].Object != "Body" {
		t.Fatalf("expected a single STEP output selecting object Body, got %#v", h.Manifest.Outputs)
	}
	if h.JobID == "" || h.Manifest.PlanHash == "" {
		t.Fatal("expected non-empty deterministic job/plan identity")
	}

	stepTypes := make([]planner.StepType, 0, len(h.Steps))
	for _, s := range h.Steps {
		stepTypes = append(stepTypes, s.Type)
	}
	if len(stepTypes) != 3 || stepTypes[0] != planner.StepWriteCSV ||
		stepTypes[1] != planner.StepWriteExportManifest || stepTypes[2] != planner.StepRunCADRuntime {
		t.Fatalf("expected exactly WriteCSV, WriteExportManifest, RunCADRuntime in order, got %v", stepTypes)
	}
}

func TestSmokeSubmission_ExcludesOperationalRuntimeState(t *testing.T) {
	binary := buildSmokeBinary(t)
	sourceModel := filepath.Join(t.TempDir(), "box.FCStd")
	out := runSmokeBinary(t, binary, "--source-model", sourceModel, "--product", "box")

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("decode top-level stdout: %v\n%s", err, out)
	}
	handoffKeys := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw["handoff"], &handoffKeys); err != nil {
		t.Fatalf("decode handoff stdout: %v", err)
	}
	forbidden := []string{
		"executable", "attemptRoot", "resultState", "observedState",
		"verificationState", "acceptedArtifactState", "workingCopy",
	}
	for _, key := range forbidden {
		if _, present := handoffKeys[key]; present {
			t.Fatalf("aligned submission must not carry operational runtime field %q", key)
		}
	}
	if bytes.Contains(out, []byte("attemptID")) || bytes.Contains(out, []byte("parametron.observed")) {
		t.Fatalf("aligned submission leaked operational runtime evidence: %s", out)
	}
}

func TestSmokeSubmission_IsDeterministic(t *testing.T) {
	binary := buildSmokeBinary(t)
	sourceModel := filepath.Join(t.TempDir(), "box.FCStd")

	first := runSmokeBinary(t, binary, "--source-model", sourceModel, "--product", "box")
	second := runSmokeBinary(t, binary, "--source-model", sourceModel, "--product", "box")

	if !bytes.Equal(first, second) {
		t.Fatalf("expected byte-identical submissions for equivalent inputs:\n%s\n---\n%s", first, second)
	}
}
