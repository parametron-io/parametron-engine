package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"parametron/internal/authoring/planner"
)

// The positive Smoke corpus tables below mirror the canonical in-memory
// tables in internal/testsupport/exampletables.SmokeBreakTables, expressed
// in the JSON file format accepted by the CLI --table loader. They are kept
// as file-format literals because the matrix must exercise the real
// file-backed table loading path, not the in-memory Go representation.
const (
	smokeMatrixFastenersTableJSON = `{
  "schemaVersion": "1.0",
  "name": "fasteners",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true},
    {"name": "label", "type": "string", "required": true},
    {"name": "coated", "type": "boolean", "required": true},
    {"name": "note", "type": "string", "required": false}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8, "label": "Hex bolt", "coated": true, "note": "zinc"},
    {"sku": "M8x30", "diameter": 8, "label": "Hex bolt long", "coated": false}
  ]
}`

	smokeMatrixExactFastenersTableJSON = `{
  "schemaVersion": "1.0",
  "name": "exact_fasteners",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "m8", "diameter": 8},
    {"sku": " M8 ", "diameter": 8}
  ]
}`
)

// TestSmokeMatrix proves that every positive fixture in testdata/dsl/smoke/*.dsl
// passes through the real root CLI planning boundary (--json-plan) and yields
// a valid JSON plan. --json-plan returns from run() before executePlanRun, so
// no scheduler, executor, adapter, runtime, cache, or run-root behavior can
// be reached. Newly added Smoke fixtures enter the matrix automatically.
func TestSmokeMatrix(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	fixtures, err := filepath.Glob(filepath.Join(repoRoot, "testdata", "dsl", "smoke", "*.dsl"))
	if err != nil {
		t.Fatalf("failed to enumerate smoke DSL files: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("no smoke fixtures found under %s", filepath.Join(repoRoot, "testdata", "dsl", "smoke"))
	}
	sort.Strings(fixtures)

	fastenersPath := writeTableFixture(t, smokeMatrixFastenersTableJSON)
	exactFastenersPath := writeTableFixture(t, smokeMatrixExactFastenersTableJSON)

	for _, fixture := range fixtures {
		base := filepath.Base(fixture)
		t.Run(base, func(t *testing.T) {
			stdout, err := executeRootCommand(t, []string{
				"--file", fixture,
				"--json-plan",
				"--table", "fasteners=" + fastenersPath,
				"--table", "exact_fasteners=" + exactFastenersPath,
			})
			if err != nil {
				t.Fatalf("root CLI --json-plan failed for %s: %v", base, err)
			}

			var out struct {
				Plan struct {
					Steps []struct {
						Type string `json:"Type"`
					} `json:"Steps"`
				} `json:"plan"`
				Hash string `json:"hash"`
			}
			if err := json.Unmarshal([]byte(stdout), &out); err != nil {
				t.Fatalf("invalid --json-plan JSON for %s: %v\noutput:\n%s", base, err, stdout)
			}
			if out.Hash == "" {
				t.Fatalf("expected a non-empty plan hash for %s", base)
			}
			if len(out.Plan.Steps) == 0 {
				t.Fatalf("expected at least one plan step for %s", base)
			}

			// The one adapter-present positive fixture must keep planning the
			// aligned normal FreeCAD sequence at the public CLI boundary.
			if base == "smoke_adapter_present.dsl" {
				want := []string{
					string(planner.StepWriteCSV),
					string(planner.StepWriteExportManifest),
					string(planner.StepRunCADRuntime),
				}
				got := make([]string, 0, len(out.Plan.Steps))
				for _, step := range out.Plan.Steps {
					got = append(got, step.Type)
				}
				if len(got) != len(want) {
					t.Fatalf("expected aligned step sequence %v for %s, got %v", want, base, got)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("expected aligned step sequence %v for %s, got %v", want, base, got)
					}
				}
			}
		})
	}
}
