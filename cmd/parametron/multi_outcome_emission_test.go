package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parametron/internal/engine/report"
)

// writeMultiProductExecutionFixture declares two products that share one source
// resource. Both can therefore accept the same traversal evidence, while each
// attempt still produces its own outcome-scoped observation, verification,
// reference, and raw evidence material.
func writeMultiProductExecutionFixture(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "models", "box.FCStd"), []byte("model-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.dsl"), []byte(withVersionHeader(`
const PRIMARY_MODEL = "box_model"

product Widget {
    adapter = "freecad"
    source_model = PRIMARY_MODEL
    outputs = ["step"]
}

product Gadget {
    adapter = "freecad"
    source_model = PRIMARY_MODEL
    outputs = ["step"]
}
`)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "prm.project.json"), []byte(`{
  "version": "1.0",
  "projectId": "multi-product-fixture",
  "dsl": "./project.dsl",
  "resources": {
    "models": {
      "box_model": "./models/box.FCStd"
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return projectDir
}

func TestCLIProjectRun_MultiProductEmitsEveryOutcomeScopedEngineeringFamily(t *testing.T) {
	resetGlobals()
	chdirToTemp(t)
	setPathWithoutFreeCAD(t)

	projectDir := writeMultiProductExecutionFixture(t)
	traversal := validReferenceTraversalJSONFixture()
	t.Setenv("PARAMETRON_TASK13_RUNTIME_TRAVERSAL_JSON", string(traversal))

	run := runCLIExecutionForProject(t, projectDir, filepath.Join(t.TempDir(), "out"))
	if run.report.Status != report.StatusSuccess {
		t.Fatalf("report status = %q, want success", run.report.Status)
	}
	packageRoot := recordPackageRoot(run.result.RunRoot)
	files := readRecordPackageFiles(t, packageRoot)
	manifest := readCLIRecordPackageManifest(t, packageRoot)

	counts := map[string]int{}
	var observationPaths, referencePaths []string
	for _, record := range manifest.Records {
		counts[record.Family]++
		if record.Family == "observation" {
			observationPaths = append(observationPaths, record.ContractPath)
		}
		if record.Family == "reference" {
			referencePaths = append(referencePaths, record.ContractPath)
		}
		if strings.HasPrefix(record.ContractPath, "records/outcomes/") && record.IdentityID == "" {
			t.Fatalf("outcome-scoped record lacks identity: %#v", record)
		}
	}
	if counts["observation"] != 2 || counts["reference"] != 2 || counts["verification"] != 2 {
		t.Fatalf("family counts = %#v, want two observation/reference/verification records", counts)
	}
	if counts["execution"] != 1 || counts["failure"] != 0 {
		t.Fatalf("run-level family counts = %#v, want one execution and no failure", counts)
	}
	for _, path := range append(append([]string{}, observationPaths...), referencePaths...) {
		if !strings.HasPrefix(path, "records/outcomes/") {
			t.Fatalf("multi-outcome record path %q is not outcome-scoped", path)
		}
	}

	products := map[string]bool{}
	for _, path := range observationPaths {
		var record struct {
			Provenance struct {
				Linkage struct {
					JobID      string `json:"jobId"`
					ProductKey string `json:"productKey"`
					StepRef    string `json:"stepRef"`
				} `json:"linkage"`
			} `json:"provenance"`
		}
		if err := json.Unmarshal(files[path], &record); err != nil {
			t.Fatalf("decode observation %q: %v", path, err)
		}
		if record.Provenance.Linkage.JobID == "" || record.Provenance.Linkage.ProductKey == "" || record.Provenance.Linkage.StepRef == "" {
			t.Fatalf("observation %q lacks outcome linkage", path)
		}
		products[record.Provenance.Linkage.ProductKey] = true
	}
	if !products["Widget"] || !products["Gadget"] {
		t.Fatalf("outcome-scoped observation products = %#v, want Widget and Gadget", products)
	}

	rawCounts := map[string]int{}
	for path := range files {
		if !strings.HasPrefix(path, "raw/outcomes/") {
			continue
		}
		parts := strings.Split(path, "/")
		if len(parts) < 4 {
			t.Fatalf("unexpected scoped raw path %q", path)
		}
		rawCounts[strings.Join(parts[3:], "/")]++
	}
	for _, family := range []string{
		"observed/prm.observed.json",
		"verification/prm.verification.json",
		"runtime/prm.result.json",
		"runtime/prm.reference-traversal.json",
	} {
		if rawCounts[family] != 2 {
			t.Fatalf("scoped raw family %q count = %d, want 2: %#v", family, rawCounts[family], rawCounts)
		}
	}

	for _, canonical := range []string{
		"raw/observed/prm.observed.json",
		"raw/verification/prm.verification.json",
		"raw/runtime/prm.result.json",
		"raw/runtime/prm.reference-traversal.json",
	} {
		if _, exists := files[canonical]; exists {
			t.Fatalf("multi-outcome package wrote singular raw evidence at %q", canonical)
		}
	}

	traversalFiles := 0
	for path, content := range files {
		if strings.HasPrefix(path, "raw/outcomes/") && strings.HasSuffix(path, "/runtime/prm.reference-traversal.json") {
			traversalFiles++
			if !bytes.Equal(content, traversal) {
				t.Fatalf("scoped traversal raw bytes at %q do not preserve exact adapter output", path)
			}
		}
	}
	if traversalFiles != 2 {
		t.Fatalf("scoped traversal raw files = %d, want 2", traversalFiles)
	}
}
