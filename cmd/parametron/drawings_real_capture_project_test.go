package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// #87 real-capture acceptance: the committed real FreeCAD capture projects from
// parametron-freecad #93/#97 are ingested through the normal project-mode CLI
// path without structural repair.

func TestValidateProjectAcceptsRealFreeCADDrawingCaptures(t *testing.T) {
	fixtureRoot := filepath.Join(findRepoRoot(t), "testdata", "projects", "freecad")
	scenarios := []struct {
		name    string
		project string
	}{
		{name: "zero-drawings", project: "rehearsal/cube"},
		{name: "single-sheet", project: "drawings/single-sheet"},
		{name: "multi-sheet", project: "drawings/multi-sheet"},
		{name: "multiple-drawings", project: "drawings/multiple-drawings"},
	}

	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			projectDir := filepath.Join(fixtureRoot, scenario.project)
			for _, entryPath := range projectEntrypoints(projectDir) {
				entryPath := entryPath
				t.Run(filepath.Base(entryPath), func(t *testing.T) {
					for run := 0; run < 2; run++ {
						stdout, err := executeHarnessCommand(t, []string{"validate", "--project", entryPath})
						if err != nil {
							t.Fatalf("validate --project failed on run %d: %v", run, err)
						}
						if !strings.Contains(stdout, "Validation succeeded") {
							t.Fatalf("unexpected validate output on run %d: %q", run, stdout)
						}
					}
				})
			}
		})
	}
}
