package cadruntime

import (
	"os"
	"path/filepath"
	"testing"

	"parametron/internal/engine/adapter"
	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/recordcontract"
)

// Issue #51: the runtime intake boundary reads only the canonical confined
// evidence file. An absent file is explicit unavailability, never an empty
// observation or an inferred fact.

func factLoadRunFixture(t *testing.T) (FreeCADRuntimeRun, string) {
	t.Helper()
	workingCopy := t.TempDir()
	outputDir := filepath.Join(workingCopy, "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := FreeCADRuntimeRun{ObservationRequest: FreeCADRuntimeObservationRequestMaterialization{
		Manifest: freecad.FreeCADRuntimeManifestMaterialization{
			Attempt: freecad.FreeCADRuntimeAttempt{
				Resources: factTestInventory(t),
				Layout: freecad.FreeCADRuntimeWorkingCopyLayout{
					WorkingCopyDir: workingCopy,
					OutputDir:      outputDir,
					SourceDocument: "source/Widget.FCStd",
				},
			},
		},
	}}
	return run, outputDir
}

func TestLoadFactRunAbsentEvidenceIsExplicitUnavailability(t *testing.T) {
	request := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
	run, outputDir := factLoadRunFixture(t)
	factRun, err := LoadFactRun(adapter.CADRuntimeOrchestrationRequest{FactObservation: request, Resources: nil}, run)
	if err != nil {
		t.Fatalf("LoadFactRun returned error for an absent optional file: %v", err)
	}
	if factRun == nil || factRun.Content != nil {
		t.Fatalf("absent evidence run = %#v, want nil content", factRun)
	}
	if factRun.Path != filepath.Join(outputDir, FactEvidenceFilename) {
		t.Fatalf("evidence path = %q, want the canonical confined path", factRun.Path)
	}
	facts, err := MapFactEvidence(FactMappingInput{Request: factRun.Request, Resources: factTestInventory(t), Content: factRun.Content})
	if err != nil {
		t.Fatalf("mapping absent evidence returned error: %v", err)
	}
	if len(facts) != 1 || facts[0].Availability != recordcontract.FactAvailabilityUnavailable || facts[0].Value != (recordcontract.ObservationValue{}) {
		t.Fatalf("absent evidence facts = %#v, want explicit unavailability", facts)
	}
}

func TestLoadFactRunPresentEvidencePreservesExactBytes(t *testing.T) {
	request := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))
	run, outputDir := factLoadRunFixture(t)
	content := factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")))
	if err := os.WriteFile(filepath.Join(outputDir, FactEvidenceFilename), content, 0o600); err != nil {
		t.Fatal(err)
	}
	factRun, err := LoadFactRun(adapter.CADRuntimeOrchestrationRequest{FactObservation: request}, run)
	if err != nil {
		t.Fatalf("LoadFactRun returned error: %v", err)
	}
	if factRun == nil || string(factRun.Content) != string(content) {
		t.Fatalf("LoadFactRun content = %#v, want the exact evidence bytes", factRun)
	}
	facts, err := MapFactEvidence(FactMappingInput{Request: factRun.Request, Resources: factTestInventory(t), Content: factRun.Content})
	if err != nil {
		t.Fatalf("mapping present evidence returned error: %v", err)
	}
	if len(facts) != 1 || facts[0].Value.Raw != "42" || facts[0].Availability != recordcontract.FactAvailabilityObserved {
		t.Fatalf("present evidence facts = %#v, want observed 42", facts)
	}
}

func TestLoadFactRunRejectsUnconfinedOrEmptyEvidence(t *testing.T) {
	request := factTestRequest(factTestBinding("widget", "Body", "Length", recordcontract.FactValueKindNumber, "mm"))

	t.Run("empty file", func(t *testing.T) {
		run, outputDir := factLoadRunFixture(t)
		if err := os.WriteFile(filepath.Join(outputDir, FactEvidenceFilename), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFactRun(adapter.CADRuntimeOrchestrationRequest{FactObservation: request}, run); err == nil {
			t.Fatal("empty evidence file was accepted")
		}
	})

	t.Run("symlink escape", func(t *testing.T) {
		run, outputDir := factLoadRunFixture(t)
		outside := filepath.Join(t.TempDir(), "evidence.json")
		content := factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")))
		if err := os.WriteFile(outside, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(outputDir, FactEvidenceFilename)); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFactRun(adapter.CADRuntimeOrchestrationRequest{FactObservation: request}, run); err == nil {
			t.Fatal("symlinked evidence file was accepted")
		}
	})

	t.Run("no intent ignores unrelated file", func(t *testing.T) {
		run, outputDir := factLoadRunFixture(t)
		content := factTestEvidenceBytes(t, factTestEvidence(factTestObserved("widget", "Body", "source/Widget.FCStd", "Length", recordcontract.FactValueKindNumber, "42", "mm")))
		if err := os.WriteFile(filepath.Join(outputDir, FactEvidenceFilename), content, 0o600); err != nil {
			t.Fatal(err)
		}
		factRun, err := LoadFactRun(adapter.CADRuntimeOrchestrationRequest{}, run)
		if err != nil || factRun != nil {
			t.Fatalf("no intent loaded evidence: %#v %v", factRun, err)
		}
	})
}
