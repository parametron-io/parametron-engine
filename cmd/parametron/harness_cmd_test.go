package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
	"parametron/internal/authoring/planner"
	"parametron/internal/engine/metadata"
	"parametron/internal/engine/projectinput"
	"parametron/internal/engine/report"
	"parametron/internal/shared/projectlock"
	"parametron/internal/shared/projectmap"
)

func TestValidateCommand(t *testing.T) {
	t.Run("succeeds on valid DSL", func(t *testing.T) {
		stdout, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, "Validation succeeded") {
			t.Fatalf("expected success output, got %q", stdout)
		}
	})

	t.Run("fails on parse error", func(t *testing.T) {
		_, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param width: number = 100
`),
		})
		if err == nil {
			t.Fatal("expected parse error")
		}
	})

	t.Run("fails on invalid inputs override key", func(t *testing.T) {
		inputs := writeJSONFixture(t, `{"unknown": 10}`)
		_, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`),
			"--inputs", inputs,
		})
		if err == nil || !strings.Contains(err.Error(), "not defined in DSL") {
			t.Fatalf("expected invalid override error, got %v", err)
		}
	})

	t.Run("succeeds on valid inputs JSON", func(t *testing.T) {
		inputs := writeJSONFixture(t, `{"width": 120}`)
		stdout, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`),
			"--inputs", inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, "with inputs") {
			t.Fatalf("expected inputs success output, got %q", stdout)
		}
	})

	t.Run("succeeds for table_cell when table is loaded from disk", func(t *testing.T) {
		tablePath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`)

		stdout, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`),
			"--table", "fasteners=" + tablePath,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, "Validation succeeded") {
			t.Fatalf("expected success output, got %q", stdout)
		}
	})

	t.Run("fails when required table is not supplied", func(t *testing.T) {
		_, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`),
		})
		if err == nil || !strings.Contains(err.Error(), "references unknown table 'fasteners'") {
			t.Fatalf("expected missing table validation failure, got %v", err)
		}
	})

	t.Run("fails on malformed table flag", func(t *testing.T) {
		_, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`),
			"--table", "fasteners",
		})
		if err == nil || !strings.Contains(err.Error(), "invalid --table value") {
			t.Fatalf("expected malformed --table error, got %v", err)
		}
	})

	t.Run("fails on duplicate table logical id", func(t *testing.T) {
		tablePath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`)

		_, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`),
			"--table", "fasteners=" + tablePath,
			"--table", "fasteners=" + tablePath,
		})
		if err == nil || !strings.Contains(err.Error(), "duplicate --table") {
			t.Fatalf("expected duplicate --table error, got %v", err)
		}
	})

	t.Run("surfaces missing table file errors", func(t *testing.T) {
		_, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`),
			"--table", "fasteners=" + filepath.Join(t.TempDir(), "missing.json"),
		})
		if err == nil || !strings.Contains(err.Error(), "missing.json") || !strings.Contains(err.Error(), "table I/O error") {
			t.Fatalf("expected useful missing file error, got %v", err)
		}
	})

	t.Run("surfaces invalid table file classification", func(t *testing.T) {
		tablePath := writeTableFixture(t, `{"schemaVersion":"1.0"`)

		_, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`),
			"--table", "fasteners=" + tablePath,
		})
		if err == nil || !strings.Contains(err.Error(), "table decode error") {
			t.Fatalf("expected decode-classified error, got %v", err)
		}
	})

	t.Run("loads multiple tables together", func(t *testing.T) {
		fastenersPath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`)
		labelsPath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "label_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "label": "M8 bolt"}
  ]
}`)

		stdout, err := executeHarnessCommand(t, []string{
			"validate",
			"--file", writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
    param label: string = table_cell("labels", sku, "label")
}
`),
			"--table", "fasteners=" + fastenersPath,
			"--table", "labels=" + labelsPath,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(stdout, "Validation succeeded") {
			t.Fatalf("expected success output, got %q", stdout)
		}
	})
}

func TestValidateCommand_ProjectFixtures(t *testing.T) {
	repoRoot := findRepoRoot(t)
	fixtureRoot := filepath.Join(repoRoot, "testdata", "projects", "freecad")

	t.Run("smoke fixtures succeed via both project entry styles", func(t *testing.T) {
		for _, scenario := range []string{
			"smoke/minimal-valid-project",
			"smoke/valid-project-with-table",
			"smoke/multi-adapter-project",
		} {
			scenario := scenario
			t.Run(filepath.Base(scenario), func(t *testing.T) {
				for _, entryPath := range projectFixtureEntrypoints(fixtureRoot, scenario) {
					entryPath := entryPath
					t.Run(filepath.Base(entryPath), func(t *testing.T) {
						stdout, err := executeHarnessCommand(t, []string{
							"validate",
							"--file", entryPath,
						})
						if err != nil {
							t.Fatalf("unexpected error for %s via %s: %v", scenario, entryPath, err)
						}
						if !strings.Contains(stdout, "Validation succeeded") {
							t.Fatalf("expected success output for %s via %s, got %q", scenario, entryPath, stdout)
						}
					})
				}
			})
		}
	})

	t.Run("break fixtures fail at deterministic boundaries for both project entry styles", func(t *testing.T) {
		testCases := []struct {
			name         string
			scenario     string
			wantContains []string
		}{
			{
				name:     "unknown logical model id",
				scenario: "break/unknown-model-id",
				wantContains: []string{
					"unknown model logical ID",
					"missing_box_model",
				},
			},
			{
				name:     "missing mapped model file",
				scenario: "break/missing-model-file",
				wantContains: []string{
					"hash model",
					"missing.FCStd",
				},
			},
			{
				name:     "escaping model path is rejected by project mapping validation",
				scenario: "break/escaping-model-path",
				wantContains: []string{
					"project mapping validation error",
					"must stay within project root",
				},
			},
			{
				name:     "missing dsl file fails during project resolution",
				scenario: "break/missing-dsl-file",
				wantContains: []string{
					"load project DSL",
					"missing.project.dsl",
				},
			},
			{
				name:     "missing mapped table file",
				scenario: "break/missing-table-file",
				wantContains: []string{
					"load table",
					"missing.json",
					"table I/O error",
				},
			},
			{
				name:     "escaping table path is rejected by project mapping validation",
				scenario: "break/escaping-table-path",
				wantContains: []string{
					"project mapping validation error",
					`tables["fasteners"]`,
					"must stay within project root",
				},
			},
			{
				name:     "malformed table json preserves decode classification",
				scenario: "break/malformed-table-json",
				wantContains: []string{
					"load table",
					"table decode error",
				},
			},
			{
				name:     "unknown table logical id fails through project mapping integration",
				scenario: "break/unknown-table-id",
				wantContains: []string{
					"references unknown table 'fasteners'",
				},
			},
			{
				name:     "absolute dsl path is rejected by project mapping validation",
				scenario: "break/absolute-dsl-path",
				wantContains: []string{
					"project mapping validation error",
					"dsl path",
					"must be relative",
				},
			},
			{
				name:     "escaping dsl path is rejected by project mapping validation",
				scenario: "break/escaping-dsl-path",
				wantContains: []string{
					"project mapping validation error",
					"dsl path",
					"must stay within project root",
				},
			},
			{
				name:     "dot dsl path is rejected by project mapping validation",
				scenario: "break/dot-dsl-path",
				wantContains: []string{
					"project mapping validation error",
					`dsl path "." must not resolve to current directory`,
				},
			},
			{
				name:     "case mismatched model id remains unresolved",
				scenario: "break/case-mismatched-model-id",
				wantContains: []string{
					"unknown model logical ID",
					"boxmodel",
				},
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				for _, entryPath := range projectFixtureEntrypoints(fixtureRoot, tc.scenario) {
					entryPath := entryPath
					t.Run(filepath.Base(entryPath), func(t *testing.T) {
						_, err := executeHarnessCommand(t, []string{
							"validate",
							"--file", entryPath,
						})
						if err == nil {
							t.Fatalf("expected error for %s via %s", tc.scenario, entryPath)
						}
						for _, want := range tc.wantContains {
							if !strings.Contains(err.Error(), want) {
								t.Fatalf("expected error for %s via %s to contain %q, got %v", tc.scenario, entryPath, want, err)
							}
						}
					})
				}
			})
		}
	})
}

func TestValidateCommand_ProjectFlagAcceptsProjectEntrypoints(t *testing.T) {
	repoRoot := findRepoRoot(t)
	projectDir := filepath.Join(repoRoot, "testdata", "projects", "freecad", "smoke", "minimal-valid-project")

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			stdout, err := executeHarnessCommand(t, []string{
				"validate",
				"--project", entryPath,
			})
			if err != nil {
				t.Fatalf("unexpected validate error via --project for %s: %v", entryPath, err)
			}
			if !strings.Contains(stdout, "Validation succeeded") {
				t.Fatalf("expected success output via --project, got %q", stdout)
			}
		})
	}
}

func TestValidateCommand_ProjectFixtureSymlinkedModelIsAccepted(t *testing.T) {
	repoRoot := findRepoRoot(t)
	fixtureRoot := filepath.Join(repoRoot, "testdata", "projects", "freecad")
	projectDir := filepath.Join(fixtureRoot, "smoke", "symlinked-model-project")

	projectInput, err := resolveProjectInput(projectDir)
	if err != nil {
		t.Fatalf("resolveProjectInput returned error: %v", err)
	}

	modelPath, ok := projectInput.ModelPaths["box_model"]
	if !ok {
		t.Fatalf("expected box_model in resolved model paths, got %#v", projectInput.ModelPaths)
	}

	info, err := os.Lstat(modelPath)
	if err != nil {
		t.Fatalf("Lstat(%q) returned error: %v", modelPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %q to be a symlink fixture, mode=%v", modelPath, info.Mode())
	}

	for _, entryPath := range projectFixtureEntrypoints(fixtureRoot, "smoke/symlinked-model-project") {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			stdout, err := executeHarnessCommand(t, []string{
				"validate",
				"--file", entryPath,
			})
			if err != nil {
				t.Fatalf("unexpected error for symlink-backed fixture via %s: %v", entryPath, err)
			}
			if !strings.Contains(stdout, "Validation succeeded") {
				t.Fatalf("expected success output via %s, got %q", entryPath, stdout)
			}
		})
	}
}

func TestValidateCommand_ProjectAndFlagTableConflictFailsDeterministically(t *testing.T) {
	projectDir := t.TempDir()

	projectDSL := filepath.Join(projectDir, "project.dsl")
	if err := os.WriteFile(projectDSL, []byte(`
dsl v1.0

product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	projectTable := filepath.Join(projectDir, "project-fasteners.json")
	if err := os.WriteFile(projectTable, []byte(`{
  "schemaVersion": "1.0",
  "name": "project_fasteners",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
	}`), 0o644); err != nil {
		t.Fatalf("failed to write project table: %v", err)
	}

	modelDir := filepath.Join(projectDir, "input")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	modelPath := filepath.Join(modelDir, "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("failed to write model fixture: %v", err)
	}

	projectMap := filepath.Join(projectDir, "parametron.project.json")
	if err := os.WriteFile(projectMap, []byte(`{
  "version": "1.0",
  "projectId": "table-conflict",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "box_model": "input/box.FCStd"
    }
  },
  "tables": {
    "fasteners": "project-fasteners.json"
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	flagTable := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "flag_fasteners",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 10}
  ]
}`)

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			_, err := executeHarnessCommand(t, []string{
				"validate",
				"--file", entryPath,
				"--table", "fasteners=" + flagTable,
			})
			if err == nil {
				t.Fatal("expected duplicate table conflict to fail")
			}
			if !strings.Contains(err.Error(), `duplicate table logical ID "fasteners" across project mapping and --table`) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateCommand_ProjectTableLoadFailurePrecedesModelMapping(t *testing.T) {
	projectDir := t.TempDir()

	projectDSL := filepath.Join(projectDir, "project.dsl")
	if err := os.WriteFile(projectDSL, []byte(`
dsl v1.0

profile Dev {
}

use profile Dev

product Widget {
    adapter = "freecad"
    source_model = "missing_box_model"
    outputs = ["step"]

    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	modelDir := filepath.Join(projectDir, "input")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	modelPath := filepath.Join(modelDir, "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("failed to write model fixture: %v", err)
	}

	projectMap := filepath.Join(projectDir, "parametron.project.json")
	if err := os.WriteFile(projectMap, []byte(`{
  "version": "1.0",
  "projectId": "table-before-model",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "box_model": "input/box.FCStd"
    }
  },
  "tables": {
    "fasteners": "missing.json"
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			_, err := executeHarnessCommand(t, []string{
				"validate",
				"--file", entryPath,
			})
			if err == nil {
				t.Fatal("expected missing project table to fail")
			}
			if !strings.Contains(err.Error(), `load table "fasteners"`) {
				t.Fatalf("expected table load failure, got %v", err)
			}
			if !strings.Contains(err.Error(), "table I/O error") {
				t.Fatalf("expected table I/O classification, got %v", err)
			}
			if strings.Contains(err.Error(), "unknown model logical ID") {
				t.Fatalf("expected table load failure before model mapping, got %v", err)
			}
		})
	}
}

func TestLoadPlannedRun_ProjectModeUnknownSourceModelFailsAfterDSLValidationBeforePlanGeneration(t *testing.T) {
	projectDir := t.TempDir()

	projectDSL := filepath.Join(projectDir, "project.dsl")
	if err := os.WriteFile(projectDSL, []byte(`
dsl v1.0

profile Dev {
}

use profile Dev

product Widget {
    adapter = "freecad"
    source_model = "missing_box_model"
    outputs = ["step"]

    param width: number = 100
}
`), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	modelDir := filepath.Join(projectDir, "input")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	modelPath := filepath.Join(modelDir, "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("failed to write model fixture: %v", err)
	}

	projectMap := filepath.Join(projectDir, "parametron.project.json")
	if err := os.WriteFile(projectMap, []byte(`{
  "version": "1.0",
  "projectId": "unknown-model-id-boundary",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "box_model": "input/box.FCStd"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			projectInput, err := resolveProjectInput(entryPath)
			if err != nil {
				t.Fatalf("resolveProjectInput returned error: %v", err)
			}

			ast, err := dsl.Parse(projectInput.DSLPath)
			if err != nil {
				t.Fatalf("dsl.Parse returned error: %v", err)
			}
			if err := dsl.Validate(ast); err != nil {
				t.Fatalf("expected DSL validation to succeed before project model mapping, got %v", err)
			}

			planned, err := loadPlannedRun(entryPath, map[string]string{}, nil)
			if err == nil {
				t.Fatal("expected unknown model logical ID failure")
			}
			if planned != nil {
				t.Fatalf("expected no planned run on unknown model logical ID, got %#v", planned)
			}
			if !strings.Contains(err.Error(), "unknown model logical ID") {
				t.Fatalf("expected unknown model logical ID error, got %v", err)
			}
			if !strings.Contains(err.Error(), "missing_box_model") {
				t.Fatalf("expected unresolved logical ID in error, got %v", err)
			}
		})
	}
}

func TestValidateCommand_ProjectModeRejectsUnsafePathSpellings(t *testing.T) {
	tests := []struct {
		name          string
		modelPathJSON string
		tablePathJSON string
		wantContains  []string
	}{
		{
			name:          "backslash_prefixed_model_path",
			modelPathJSON: `\\models\\box.FCStd`,
			tablePathJSON: "tables/fasteners.json",
			wantContains: []string{
				"project mapping validation error",
				`resources.models["box_model"]`,
				"must be relative",
			},
		},
		{
			name:          "unc_table_path",
			modelPathJSON: "input/box.FCStd",
			tablePathJSON: `\\\\server\\share\\fasteners.json`,
			wantContains: []string{
				"project mapping validation error",
				`tables["fasteners"]`,
				"must be relative",
			},
		},
		{
			name:          "mixed_separator_table_path_resolves_to_current_directory",
			modelPathJSON: "input/box.FCStd",
			tablePathJSON: `dir\\..\\`,
			wantContains: []string{
				"project mapping validation error",
				`tables["fasteners"]`,
				"must not resolve to current directory",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			projectDir := t.TempDir()

			projectDSL := filepath.Join(projectDir, "project.dsl")
			if err := os.WriteFile(projectDSL, []byte(`
dsl v1.0

product Widget {
    param width: number = 100
}
`), 0o644); err != nil {
				t.Fatalf("failed to write project DSL: %v", err)
			}

			modelDir := filepath.Join(projectDir, "input")
			if err := os.MkdirAll(modelDir, 0o755); err != nil {
				t.Fatalf("failed to create model dir: %v", err)
			}
			modelFixture := filepath.Join(modelDir, "box.FCStd")
			if err := os.WriteFile(modelFixture, []byte("fixture"), 0o644); err != nil {
				t.Fatalf("failed to write model fixture: %v", err)
			}

			tableDir := filepath.Join(projectDir, "tables")
			if err := os.MkdirAll(tableDir, 0o755); err != nil {
				t.Fatalf("failed to create table dir: %v", err)
			}
			tableFixture := filepath.Join(tableDir, "fasteners.json")
			if err := os.WriteFile(tableFixture, []byte(`{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20"}
  ]
}`), 0o644); err != nil {
				t.Fatalf("failed to write table fixture: %v", err)
			}

			projectMap := filepath.Join(projectDir, "parametron.project.json")
			if err := os.WriteFile(projectMap, []byte(`{
  "version": "1.0",
  "projectId": "unsafe-project-paths",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "box_model": "`+tt.modelPathJSON+`"
    }
  },
  "tables": {
    "fasteners": "`+tt.tablePathJSON+`"
  }
}`), 0o644); err != nil {
				t.Fatalf("failed to write project map: %v", err)
			}

			for _, entryPath := range projectEntrypoints(projectDir) {
				entryPath := entryPath
				t.Run(filepath.Base(entryPath), func(t *testing.T) {
					_, err := executeHarnessCommand(t, []string{
						"validate",
						"--file", entryPath,
					})
					if err == nil {
						t.Fatal("expected unsafe project mapping path to fail")
					}
					for _, want := range tt.wantContains {
						if !strings.Contains(err.Error(), want) {
							t.Fatalf("expected error to contain %q, got %v", want, err)
						}
					}
				})
			}
		})
	}
}

func TestLoadPlannedRun_ProjectModeManifestUsesResolvedSourceModelPath(t *testing.T) {
	repoRoot := findRepoRoot(t)
	projectDir := filepath.Join(repoRoot, "testdata", "projects", "freecad", "smoke", "minimal-valid-project")

	projectInput, err := resolveProjectInput(projectDir)
	if err != nil {
		t.Fatalf("resolveProjectInput returned error: %v", err)
	}
	expectedSourceModel, ok := projectInput.ModelPaths["box_model"]
	if !ok {
		t.Fatalf("expected box_model in resolved model paths, got %#v", projectInput.ModelPaths)
	}

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			planned, err := loadPlannedRun(entryPath, map[string]string{}, nil)
			if err != nil {
				t.Fatalf("loadPlannedRun returned error: %v", err)
			}

			manifestPayload := firstManifestPayloadFromPlan(t, planned.Plan)
			if got := manifestPayload.Inputs.SourceModel; got != expectedSourceModel {
				t.Fatalf("expected manifest sourceModel %q, got %q", expectedSourceModel, got)
			}
			if manifestPayload.Inputs.SourceModel == "box_model" {
				t.Fatalf("expected manifest sourceModel to be resolved path, got logical ID %q", manifestPayload.Inputs.SourceModel)
			}
			if strings.Contains(manifestPayload.Inputs.SourceModel, "box_model") {
				t.Fatalf("expected manifest sourceModel to avoid unresolved logical token, got %q", manifestPayload.Inputs.SourceModel)
			}
		})
	}
}

func TestLoadPlannedRun_ProjectModeRelativeEntrypointsDoNotDuplicateProjectRootInSourceModel(t *testing.T) {
	repoRoot := findRepoRoot(t)
	projectDir := filepath.Join(repoRoot, "testdata", "projects", "freecad", "integration", "real-table-driven-project")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	relativeProjectDir, err := filepath.Rel(cwd, projectDir)
	if err != nil {
		t.Fatalf("filepath.Rel(projectDir) returned error: %v", err)
	}

	expectedSourceModel := filepath.Join(projectDir, "input", "Coupling.FCStd")
	duplicatedSourceModel := filepath.Join(projectDir, relativeProjectDir, "input", "Coupling.FCStd")

	for _, entryPath := range []string{relativeProjectDir, filepath.Join(relativeProjectDir, "parametron.project.json")} {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			planned, err := loadPlannedRun(entryPath, map[string]string{}, nil)
			if err != nil {
				t.Fatalf("loadPlannedRun returned error: %v", err)
			}

			manifestPayload := firstManifestPayloadFromPlan(t, planned.Plan)
			if got := manifestPayload.Inputs.SourceModel; got != expectedSourceModel {
				t.Fatalf("expected manifest sourceModel %q, got %q", expectedSourceModel, got)
			}
			if manifestPayload.Inputs.SourceModel == duplicatedSourceModel {
				t.Fatalf("expected sourceModel to avoid duplicated project root, got %q", manifestPayload.Inputs.SourceModel)
			}
		})
	}
}

func TestLoadPlannedRun_ProjectModeHashUsesResolvedSourceModelPathDeterministically(t *testing.T) {
	repoRoot := findRepoRoot(t)
	projectDir := filepath.Join(repoRoot, "testdata", "projects", "freecad", "smoke", "minimal-valid-project")

	projectInput, err := resolveProjectInput(projectDir)
	if err != nil {
		t.Fatalf("resolveProjectInput returned error: %v", err)
	}
	expectedSourceModel, ok := projectInput.ModelPaths["box_model"]
	if !ok {
		t.Fatalf("expected box_model in resolved model paths, got %#v", projectInput.ModelPaths)
	}

	type plannedSignature struct {
		planHash string
	}
	results := make(map[string]plannedSignature, len(projectEntrypoints(projectDir)))

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			planned, err := loadPlannedRun(entryPath, map[string]string{}, nil)
			if err != nil {
				t.Fatalf("loadPlannedRun returned error: %v", err)
			}
			if len(planned.ProfileSettings) != 2 {
				t.Fatalf("expected only profile policy settings after migration, got %#v", planned.ProfileSettings)
			}
			if _, exists := planned.ProfileSettings["source_model"]; exists {
				t.Fatalf("expected source_model to be absent from resolved profile settings, got %#v", planned.ProfileSettings)
			}
			if manifestPayload := firstManifestPayloadFromPlan(t, planned.Plan); manifestPayload.Inputs.SourceModel != expectedSourceModel {
				t.Fatalf("expected manifest sourceModel %q, got %q", expectedSourceModel, manifestPayload.Inputs.SourceModel)
			}
			results[entryPath] = plannedSignature{
				planHash: planned.PlanHash,
			}
		})
	}

	entrypoints := projectEntrypoints(projectDir)
	first := results[entrypoints[0]]
	second := results[entrypoints[1]]
	if first.planHash != second.planHash {
		t.Fatalf("expected identical plan hashes across project entrypoints, got %q and %q", first.planHash, second.planHash)
	}
}

func TestResolveProjectInput_NormalizationEquivalenceAndDeterminism(t *testing.T) {
	projectDir := t.TempDir()

	projectDSL := filepath.Join(projectDir, "project.dsl")
	if err := os.WriteFile(projectDSL, []byte(`
dsl v1.0

profile Dev {
}

use profile Dev

product Widget {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	modelDir := filepath.Join(projectDir, "models")
	if err := os.MkdirAll(filepath.Join(modelDir, "nested"), 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	modelPath := filepath.Join(modelDir, "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("failed to write model fixture: %v", err)
	}

	tableDir := filepath.Join(projectDir, "tables")
	if err := os.MkdirAll(filepath.Join(tableDir, "catalog"), 0o755); err != nil {
		t.Fatalf("failed to create table dir: %v", err)
	}
	tablePath := filepath.Join(tableDir, "fasteners.json")
	if err := os.WriteFile(tablePath, []byte(`{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to write table fixture: %v", err)
	}

	projectMap := filepath.Join(projectDir, "parametron.project.json")
	if err := os.WriteFile(projectMap, []byte(`{
  "version": "1.0",
  "projectId": "normalized-project-mode",
  "dsl": "./dsl/../project.dsl",
  "resources": {
    "models": {
      "box_model": "./models/nested/../box.FCStd"
    }
  },
  "tables": {
    "fasteners": "./tables/catalog/../fasteners.json"
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	expected := &resolvedProjectInput{
		ProjectFile: projectMap,
		ProjectRoot: projectDir,
		DSLPath:     projectDSL,
		ModelPaths: map[string]string{
			"box_model": modelPath,
		},
		TablePaths: map[string]string{
			"fasteners": tablePath,
		},
	}

	results := make(map[string]*resolvedProjectInput, len(projectEntrypoints(projectDir)))
	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run("resolve_"+filepath.Base(entryPath), func(t *testing.T) {
			first, err := resolveProjectInput(entryPath)
			if err != nil {
				t.Fatalf("first resolveProjectInput returned error: %v", err)
			}
			second, err := resolveProjectInput(entryPath)
			if err != nil {
				t.Fatalf("second resolveProjectInput returned error: %v", err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("expected repeated resolveProjectInput calls to match\nfirst: %#v\nsecond: %#v", first, second)
			}
			if !reflect.DeepEqual(first, expected) {
				t.Fatalf("expected normalized resolved input\nwant: %#v\ngot:  %#v", expected, first)
			}
			results[entryPath] = first
		})
	}

	entrypoints := projectEntrypoints(projectDir)
	if !reflect.DeepEqual(results[entrypoints[0]], results[entrypoints[1]]) {
		t.Fatalf("expected equivalent resolved project input across entrypoints\ndir:  %#v\nfile: %#v", results[entrypoints[0]], results[entrypoints[1]])
	}
}

func TestLoadPlannedRun_ProjectModeNormalizesResolvedPathsAcrossEntrypoints(t *testing.T) {
	projectDir := t.TempDir()

	projectDSL := filepath.Join(projectDir, "project.dsl")
	if err := os.WriteFile(projectDSL, []byte(`
dsl v1.0

profile Dev {
}

use profile Dev

product Widget {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param sku: string = "M8x20"
    param fastener_label: string = table_cell("fasteners", sku, "label")
}
`), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	modelDir := filepath.Join(projectDir, "models")
	if err := os.MkdirAll(filepath.Join(modelDir, "nested"), 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	modelPath := filepath.Join(modelDir, "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("failed to write model fixture: %v", err)
	}

	tableDir := filepath.Join(projectDir, "tables")
	if err := os.MkdirAll(filepath.Join(tableDir, "catalog"), 0o755); err != nil {
		t.Fatalf("failed to create table dir: %v", err)
	}
	tablePath := filepath.Join(tableDir, "fasteners.json")
	if err := os.WriteFile(tablePath, []byte(`{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true},
    {"name": "label", "type": "string", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8, "label": "M8 bolt"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("failed to write table fixture: %v", err)
	}

	projectMap := filepath.Join(projectDir, "parametron.project.json")
	if err := os.WriteFile(projectMap, []byte(`{
  "version": "1.0",
  "projectId": "normalized-project-mode",
  "dsl": "./dsl/../project.dsl",
  "resources": {
    "models": {
      "box_model": "./models/nested/../box.FCStd"
    }
  },
  "tables": {
    "fasteners": "./tables/catalog/../fasteners.json"
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	type plannedSignature struct {
		planHash string
		dslFile  string
	}
	results := make(map[string]plannedSignature, len(projectEntrypoints(projectDir)))

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			planned, err := loadPlannedRun(entryPath, map[string]string{}, nil)
			if err != nil {
				t.Fatalf("loadPlannedRun returned error: %v", err)
			}

			if planned.DSLFile != projectDSL {
				t.Fatalf("expected normalized DSL file %q, got %q", projectDSL, planned.DSLFile)
			}

			manifestPayload := firstManifestPayloadFromPlan(t, planned.Plan)
			if got := manifestPayload.Inputs.SourceModel; got != modelPath {
				t.Fatalf("expected manifest sourceModel %q, got %q", modelPath, got)
			}
			if _, exists := planned.ProfileSettings["source_model"]; exists {
				t.Fatalf("expected source_model to be absent from profile settings, got %#v", planned.ProfileSettings)
			}
			if strings.Contains(manifestPayload.Inputs.SourceModel, "nested/../") {
				t.Fatalf("expected normalized manifest sourceModel, got %q", manifestPayload.Inputs.SourceModel)
			}
			if strings.Contains(manifestPayload.Inputs.SourceModel, "box_model") {
				t.Fatalf("expected resolved sourceModel path, got %q", manifestPayload.Inputs.SourceModel)
			}

			foundTable := false
			for _, input := range planned.TableInputs {
				if input.LogicalID == "fasteners" {
					foundTable = true
				}
			}
			if !foundTable {
				t.Fatal("expected fasteners table metadata")
			}

			results[entryPath] = plannedSignature{
				planHash: planned.PlanHash,
				dslFile:  planned.DSLFile,
			}
		})
	}

	entrypoints := projectEntrypoints(projectDir)
	first := results[entrypoints[0]]
	second := results[entrypoints[1]]
	if first.dslFile != second.dslFile {
		t.Fatalf("expected identical normalized DSL path across entrypoints, got %q and %q", first.dslFile, second.dslFile)
	}
	if first.planHash != second.planHash {
		t.Fatalf("expected identical plan hashes across entrypoints, got %q and %q", first.planHash, second.planHash)
	}
}

func TestValidateCommand_ProjectModeMixedAdaptersSucceedsAcrossEntrypoints(t *testing.T) {
	projectDir, modelPath := writeMixedAdapterProjectFixture(t)

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			stdout, err := executeHarnessCommand(t, []string{
				"validate",
				"--file", entryPath,
			})
			if err != nil {
				t.Fatalf("unexpected error validating mixed-adapter project via %s: %v", entryPath, err)
			}
			if !strings.Contains(stdout, "Validation succeeded") {
				t.Fatalf("expected success output via %s, got %q", entryPath, stdout)
			}

			planned, err := loadPlannedRun(entryPath, map[string]string{}, nil)
			if err != nil {
				t.Fatalf("loadPlannedRun returned error via %s: %v", entryPath, err)
			}

			manifests := manifestPayloadsByProduct(t, planned.Plan)
			freecadManifest, ok := manifests["FreeCADBox"]
			if !ok {
				t.Fatalf("expected FreeCADBox manifest in plan, got keys %v", sortedManifestKeys(manifests))
			}
			noneManifest, ok := manifests["NoAdapterSummary"]
			if !ok {
				t.Fatalf("expected NoAdapterSummary manifest in plan, got keys %v", sortedManifestKeys(manifests))
			}

			if freecadManifest.Adapter != "freecad" {
				t.Fatalf("expected FreeCADBox adapter %q, got %q", "freecad", freecadManifest.Adapter)
			}
			if freecadManifest.Inputs.SourceModel != modelPath {
				t.Fatalf("expected FreeCADBox sourceModel %q, got %q", modelPath, freecadManifest.Inputs.SourceModel)
			}
			if len(freecadManifest.Outputs) != 1 || freecadManifest.Outputs[0].Type != "step" {
				t.Fatalf("expected FreeCADBox step output, got %+v", freecadManifest.Outputs)
			}
			if freecadManifest.Outputs[0].Object == "" {
				t.Fatalf("expected FreeCADBox step output object to be populated, got %+v", freecadManifest.Outputs[0])
			}

			if noneManifest.Adapter != "none" {
				t.Fatalf("expected NoAdapterSummary adapter %q, got %q", "none", noneManifest.Adapter)
			}
			if noneManifest.Inputs.SourceModel != "" {
				t.Fatalf("expected NoAdapterSummary sourceModel to be empty, got %q", noneManifest.Inputs.SourceModel)
			}
			if len(noneManifest.Outputs) != 0 {
				t.Fatalf("expected NoAdapterSummary outputs to be empty, got %+v", noneManifest.Outputs)
			}

			runCADRuntimeProducts := runCADRuntimeProductKeys(planned.Plan)
			if !reflect.DeepEqual(runCADRuntimeProducts, []string{"FreeCADBox"}) {
				t.Fatalf("expected only FreeCADBox to schedule RunCADRuntime, got %#v", runCADRuntimeProducts)
			}
		})
	}
}

func TestLoadPlannedRun_ProjectModeMixedAdaptersDeterministicAcrossEntrypoints(t *testing.T) {
	projectDir, modelPath := writeMixedAdapterProjectFixture(t)

	type plannedSignature struct {
		planHash string
		jsonPlan string
	}
	results := make(map[string]plannedSignature, len(projectEntrypoints(projectDir)))

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			first, err := loadPlannedRun(entryPath, map[string]string{}, nil)
			if err != nil {
				t.Fatalf("first loadPlannedRun returned error: %v", err)
			}
			second, err := loadPlannedRun(entryPath, map[string]string{}, nil)
			if err != nil {
				t.Fatalf("second loadPlannedRun returned error: %v", err)
			}
			if first.PlanHash != second.PlanHash {
				t.Fatalf("expected repeated plan hashes to match, got %q and %q", first.PlanHash, second.PlanHash)
			}

			manifests := manifestPayloadsByProduct(t, first.Plan)
			if got := manifests["FreeCADBox"].Inputs.SourceModel; got != modelPath {
				t.Fatalf("expected resolved FreeCADBox sourceModel %q, got %q", modelPath, got)
			}
			if got := manifests["NoAdapterSummary"].Inputs.SourceModel; got != "" {
				t.Fatalf("expected empty NoAdapterSummary sourceModel, got %q", got)
			}

			jsonStdout, err := executeHarnessCommand(t, []string{
				"--file", entryPath,
				"--json-plan",
			})
			if err != nil {
				t.Fatalf("unexpected --json-plan error via %s: %v", entryPath, err)
			}

			results[entryPath] = plannedSignature{
				planHash: first.PlanHash,
				jsonPlan: jsonStdout,
			}
		})
	}

	entrypoints := projectEntrypoints(projectDir)
	first := results[entrypoints[0]]
	second := results[entrypoints[1]]
	if first.planHash != second.planHash {
		t.Fatalf("expected identical mixed-adapter plan hashes across entrypoints, got %q and %q", first.planHash, second.planHash)
	}
	if first.jsonPlan != second.jsonPlan {
		t.Fatalf("expected identical mixed-adapter json-plan output across entrypoints\nfirst: %s\nsecond: %s", first.jsonPlan, second.jsonPlan)
	}
}

func projectFixtureEntrypoints(fixtureRoot string, scenario string) []string {
	return projectEntrypoints(filepath.Join(fixtureRoot, scenario))
}

func projectEntrypoints(projectDir string) []string {
	return []string{
		projectDir,
		filepath.Join(projectDir, "parametron.project.json"),
	}
}

func TestSyncCommand_ProjectLockLifecycle(t *testing.T) {
	repoRoot := findRepoRoot(t)

	t.Run("writes expected lock via project directory and project file entrypoints", func(t *testing.T) {
		projectDir := copyFixtureTree(t, filepath.Join(repoRoot, "testdata", "projects", "freecad", "smoke", "valid-project-with-table"))
		lockPath := filepath.Join(projectDir, projectlock.FileName)

		stdoutDir, err := executeHarnessCommand(t, []string{
			"sync",
			"--file", projectDir,
		})
		if err != nil {
			t.Fatalf("unexpected sync error via project dir: %v", err)
		}
		if want := "Lock file written: " + lockPath + "\n"; stdoutDir != want {
			t.Fatalf("unexpected stdout via project dir\nwant: %q\ngot:  %q", want, stdoutDir)
		}

		lockBytesDir, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("failed to read lock file: %v", err)
		}

		loadedLock, err := projectlock.Load(lockPath)
		if err != nil {
			t.Fatalf("projectlock.Load returned error: %v", err)
		}

		expectedLock, expectedBytes := buildExpectedProjectLockBytes(t, projectDir)
		if !reflect.DeepEqual(loadedLock, expectedLock) {
			t.Fatalf("unexpected lock contents\nwant: %#v\ngot:  %#v", expectedLock, loadedLock)
		}
		if !bytes.Equal(lockBytesDir, expectedBytes) {
			t.Fatalf("unexpected lock file bytes via project dir\nwant: %s\ngot:  %s", string(expectedBytes), string(lockBytesDir))
		}

		stdoutFile, err := executeHarnessCommand(t, []string{
			"sync",
			"--file", filepath.Join(projectDir, projectmap.FileName),
		})
		if err != nil {
			t.Fatalf("unexpected sync error via project file: %v", err)
		}
		if stdoutFile != stdoutDir {
			t.Fatalf("expected identical stdout across entrypoints, got %q and %q", stdoutDir, stdoutFile)
		}

		lockBytesFile, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("failed to reread lock file: %v", err)
		}
		if !bytes.Equal(lockBytesDir, lockBytesFile) {
			t.Fatalf("expected identical lock bytes across entrypoints\nfirst:  %s\nsecond: %s", string(lockBytesDir), string(lockBytesFile))
		}
	})

	t.Run("rerun on unchanged project is byte-identical", func(t *testing.T) {
		projectDir := copyFixtureTree(t, filepath.Join(repoRoot, "testdata", "projects", "freecad", "smoke", "valid-project-with-table"))
		lockPath := filepath.Join(projectDir, projectlock.FileName)

		if _, err := executeHarnessCommand(t, []string{"sync", "--file", projectDir}); err != nil {
			t.Fatalf("unexpected first sync error: %v", err)
		}
		first, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("failed to read first lock file: %v", err)
		}

		if _, err := executeHarnessCommand(t, []string{"sync", "--file", projectDir}); err != nil {
			t.Fatalf("unexpected second sync error: %v", err)
		}
		second, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("failed to read second lock file: %v", err)
		}

		if !bytes.Equal(first, second) {
			t.Fatalf("expected byte-identical lock contents across unchanged reruns\nfirst:  %s\nsecond: %s", string(first), string(second))
		}
	})

	t.Run("resource changes refresh hashes and fingerprints deterministically", func(t *testing.T) {
		projectDir := copyFixtureTree(t, filepath.Join(repoRoot, "testdata", "projects", "freecad", "smoke", "valid-project-with-table"))
		lockPath := filepath.Join(projectDir, projectlock.FileName)

		if _, err := executeHarnessCommand(t, []string{"sync", "--file", projectDir}); err != nil {
			t.Fatalf("unexpected initial sync error: %v", err)
		}

		firstBytes, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("failed to read initial lock file: %v", err)
		}
		firstLock, err := projectlock.Load(lockPath)
		if err != nil {
			t.Fatalf("failed to load initial lock file: %v", err)
		}

		modelPath := filepath.Join(projectDir, "input", "box.FCStd")
		if err := os.WriteFile(modelPath, []byte("updated-model-fixture"), 0o644); err != nil {
			t.Fatalf("failed to update model fixture: %v", err)
		}

		tablePath := filepath.Join(projectDir, "tables", "fasteners.json")
		if err := os.WriteFile(tablePath, []byte(`{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true},
    {"name": "label", "type": "string", "required": true},
    {"name": "coated", "type": "boolean", "required": true},
    {"name": "note", "type": "string", "required": false}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 10, "label": "Hex bolt revised", "coated": true, "note": "zinc"},
    {"sku": "M8x30", "diameter": 8, "label": "Hex bolt long", "coated": false}
  ]
}`), 0o644); err != nil {
			t.Fatalf("failed to update table fixture: %v", err)
		}

		if _, err := executeHarnessCommand(t, []string{"sync", "--file", projectDir}); err != nil {
			t.Fatalf("unexpected refresh sync error: %v", err)
		}

		secondBytes, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("failed to read refreshed lock file: %v", err)
		}
		secondLock, err := projectlock.Load(lockPath)
		if err != nil {
			t.Fatalf("failed to load refreshed lock file: %v", err)
		}

		if bytes.Equal(firstBytes, secondBytes) {
			t.Fatal("expected lock file bytes to change after resource updates")
		}
		if firstLock.Resources.Models["box_model"].SHA256 == secondLock.Resources.Models["box_model"].SHA256 {
			t.Fatal("expected box_model sha256 to change after model update")
		}
		if firstLock.Tables["fasteners"].Fingerprint == secondLock.Tables["fasteners"].Fingerprint {
			t.Fatal("expected fasteners fingerprint to change after table update")
		}

		expectedLock, expectedBytes := buildExpectedProjectLockBytes(t, projectDir)
		if !reflect.DeepEqual(secondLock, expectedLock) {
			t.Fatalf("unexpected refreshed lock contents\nwant: %#v\ngot:  %#v", expectedLock, secondLock)
		}
		if !bytes.Equal(secondBytes, expectedBytes) {
			t.Fatalf("unexpected refreshed lock file bytes\nwant: %s\ngot:  %s", string(expectedBytes), string(secondBytes))
		}
	})
}

func TestSyncCommand_RejectsStandaloneDSLInput(t *testing.T) {
	dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`)

	_, err := executeHarnessCommand(t, []string{
		"sync",
		"--file", dslPath,
	})
	if err == nil {
		t.Fatal("expected standalone DSL sync to fail")
	}
	if !strings.Contains(err.Error(), "project directory or "+projectmap.FileName) {
		t.Fatalf("expected project entrypoint guidance, got %v", err)
	}
	if !strings.Contains(err.Error(), "standalone DSL files are not supported") {
		t.Fatalf("expected standalone DSL rejection, got %v", err)
	}
}

func TestSyncCommand_ProjectFlagAcceptsProjectEntrypoints(t *testing.T) {
	repoRoot := findRepoRoot(t)
	projectDir := copyFixtureTree(t, filepath.Join(repoRoot, "testdata", "projects", "freecad", "smoke", "valid-project-with-table"))

	for _, entryPath := range projectEntrypoints(projectDir) {
		entryPath := entryPath
		t.Run(filepath.Base(entryPath), func(t *testing.T) {
			stdout, err := executeHarnessCommand(t, []string{
				"sync",
				"--project", entryPath,
			})
			if err != nil {
				t.Fatalf("unexpected sync error via --project for %s: %v", entryPath, err)
			}
			if !strings.Contains(stdout, "Lock file written:") {
				t.Fatalf("expected sync success output via --project, got %q", stdout)
			}
		})
	}
}

func TestSyncCommand_PropagatesProjectFailures(t *testing.T) {
	repoRoot := findRepoRoot(t)
	fixtureRoot := filepath.Join(repoRoot, "testdata", "projects", "freecad")

	testCases := []struct {
		name         string
		scenario     string
		wantContains []string
	}{
		{
			name:     "missing dsl file",
			scenario: "break/missing-dsl-file",
			wantContains: []string{
				"load project DSL",
				"missing.project.dsl",
			},
		},
		{
			name:     "missing mapped model file",
			scenario: "break/missing-model-file",
			wantContains: []string{
				"hash model",
				"missing.FCStd",
			},
		},
		{
			name:     "malformed table json",
			scenario: "break/malformed-table-json",
			wantContains: []string{
				"load table",
				"table decode error",
			},
		},
		{
			name:     "missing mapped table file",
			scenario: "break/missing-table-file",
			wantContains: []string{
				"load table",
				"missing.json",
				"table I/O error",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, entryPath := range projectFixtureEntrypoints(fixtureRoot, tc.scenario) {
				entryPath := entryPath
				t.Run(filepath.Base(entryPath), func(t *testing.T) {
					_, err := executeHarnessCommand(t, []string{
						"sync",
						"--file", entryPath,
					})
					if err == nil {
						t.Fatalf("expected sync failure for %s via %s", tc.scenario, entryPath)
					}
					for _, want := range tc.wantContains {
						if !strings.Contains(err.Error(), want) {
							t.Fatalf("expected error for %s via %s to contain %q, got %v", tc.scenario, entryPath, want, err)
						}
					}
				})
			}
		})
	}
}

func TestSimulateCommand(t *testing.T) {
	dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 100
    param material: string = "Steel"
}
`)
	casesPath := writeJSONFixture(t, `[
  {"width": 120, "material": "Steel"},
  {"width": 140, "material": "Aluminum"}
]`)
	outDir := filepath.Join(t.TempDir(), "simulate")

	stdout, err := executeHarnessCommand(t, []string{
		"simulate",
		"--file", dslPath,
		"--inputs", casesPath,
		"--out", outDir,
	})
	if err != nil {
		t.Fatalf("unexpected simulate error: %v", err)
	}
	if !strings.Contains(stdout, "total cases: 2") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}

	reportBytes, err := os.ReadFile(filepath.Join(outDir, "simulate_report.json"))
	if err != nil {
		t.Fatalf("failed to read simulate report: %v", err)
	}

	var report simulateReport
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatalf("failed to decode simulate report: %v", err)
	}
	if report.TotalCases != 2 || report.PassedCases != 2 || report.FailedCases != 0 {
		t.Fatalf("unexpected summary: %+v", report)
	}
	if report.Cases[0].Index != 0 || report.Cases[1].Index != 1 {
		t.Fatalf("expected input order to be preserved: %+v", report.Cases)
	}
	if !strings.Contains(report.Cases[0].RunRoot, "case-000") || !strings.Contains(report.Cases[1].RunRoot, "case-001") {
		t.Fatalf("unexpected run roots: %+v", report.Cases)
	}

	stdoutAgain, err := executeHarnessCommand(t, []string{
		"simulate",
		"--file", dslPath,
		"--inputs", casesPath,
		"--out", outDir,
	})
	if err != nil {
		t.Fatalf("unexpected simulate rerun error: %v", err)
	}
	if stdout != stdoutAgain {
		t.Fatalf("expected deterministic stdout, got %q then %q", stdout, stdoutAgain)
	}
	reportBytesAgain, err := os.ReadFile(filepath.Join(outDir, "simulate_report.json"))
	if err != nil {
		t.Fatalf("failed to read simulate report on rerun: %v", err)
	}
	if !bytes.Equal(reportBytes, reportBytesAgain) {
		t.Fatal("simulate_report.json changed between identical runs")
	}
}

func TestSimulateCommand_WithLoadedTables(t *testing.T) {
	tablePath := writeTableFixture(t, `{
  "schemaVersion": "1.0",
  "name": "fastener_catalog",
  "keyColumn": "sku",
  "columns": [
    {"name": "sku", "type": "string", "required": true},
    {"name": "diameter", "type": "number", "required": true}
  ],
  "rows": [
    {"sku": "M8x20", "diameter": 8}
  ]
}`)
	dslPath := writeDSLFixture(t, `
product Widget {
    param sku: string = "M8x20"
    param diameter: number = table_cell("fasteners", sku, "diameter")
}
`)
	casesPath := writeJSONFixture(t, `[
  {"sku": "M8x20"}
]`)
	outDir := filepath.Join(t.TempDir(), "simulate")

	stdout, err := executeHarnessCommand(t, []string{
		"simulate",
		"--file", dslPath,
		"--inputs", casesPath,
		"--out", outDir,
		"--table", "fasteners=" + tablePath,
	})
	if err != nil {
		t.Fatalf("unexpected simulate error: %v", err)
	}
	if !strings.Contains(stdout, "passed: 1") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}

	var report simulateReport
	readJSONFile(t, filepath.Join(outDir, "simulate_report.json"), &report)
	if report.PassedCases != 1 || report.FailedCases != 0 {
		t.Fatalf("unexpected summary: %+v", report)
	}
}

func TestSimulateCommand_FailFastAndMaxErrors(t *testing.T) {
	dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`)
	casesPath := writeJSONFixture(t, `[
  {"width": 120},
  {"width": "bad"},
  {"width": 140}
]`)

	t.Run("records failures and stops on fail-fast", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "simulate-fail-fast")
		_, err := executeHarnessCommand(t, []string{
			"simulate",
			"--file", dslPath,
			"--inputs", casesPath,
			"--out", outDir,
			"--fail-fast",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var report simulateReport
		readJSONFile(t, filepath.Join(outDir, "simulate_report.json"), &report)
		if report.FailedCases != 1 || report.PassedCases != 1 {
			t.Fatalf("unexpected fail-fast summary: %+v", report)
		}
		if report.Cases[2].Status != "skipped" {
			t.Fatalf("expected trailing case to be skipped, got %+v", report.Cases[2])
		}
	})

	t.Run("respects max-errors", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "simulate-max-errors")
		_, err := executeHarnessCommand(t, []string{
			"simulate",
			"--file", dslPath,
			"--inputs", writeJSONFixture(t, `[
  {"width": "bad"},
  {"width": "still-bad"},
  {"width": 140}
]`),
			"--out", outDir,
			"--max-errors", "1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var report simulateReport
		readJSONFile(t, filepath.Join(outDir, "simulate_report.json"), &report)
		if report.FailedCases != 1 || report.Cases[1].Status != "skipped" || report.Cases[2].Status != "skipped" {
			t.Fatalf("unexpected max-errors summary: %+v", report)
		}
	})
}

func TestSweepCommand(t *testing.T) {
	dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 100
    param material: string = "Steel"
}
`)
	outDir := filepath.Join(t.TempDir(), "sweep")

	stdout, err := executeHarnessCommand(t, []string{
		"sweep",
		"--file", dslPath,
		"--out", outDir,
		"--product", "Widget",
		"--param", "width=60..100:20",
		"--param", "material=[Steel,Aluminum]",
	})
	if err != nil {
		t.Fatalf("unexpected sweep error: %v", err)
	}
	if !strings.Contains(stdout, "total generated cases: 6") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}

	reportPath := filepath.Join(outDir, "sweep_report.json")
	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed to read sweep report: %v", err)
	}

	var report sweepReport
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatalf("failed to decode sweep report: %v", err)
	}
	if report.TotalCases != 6 || report.PassedCases != 6 || report.FailedCases != 0 {
		t.Fatalf("unexpected sweep summary: %+v", report)
	}

	if _, err := executeHarnessCommand(t, []string{
		"sweep",
		"--file", dslPath,
		"--out", outDir,
		"--product", "Widget",
		"--param", "width=60..100:20",
		"--param", "material=[Steel,Aluminum]",
	}); err != nil {
		t.Fatalf("unexpected sweep rerun error: %v", err)
	}
	reportBytesAgain, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed to reread sweep report: %v", err)
	}
	if !bytes.Equal(reportBytes, reportBytesAgain) {
		t.Fatal("sweep_report.json changed between identical runs")
	}
}

func TestSweepCommand_ValidationErrors(t *testing.T) {
	dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 1
}
`)

	testCases := []struct {
		name string
		args []string
		out  string
	}{
		{
			name: "rejects malformed syntax",
			args: []string{"sweep", "--file", dslPath, "--product", "Widget", "--param", "width=oops"},
		},
		{
			name: "rejects zero step",
			args: []string{"sweep", "--file", dslPath, "--product", "Widget", "--param", "width=0..2:0"},
		},
		{
			name: "captures planning failure",
			out:  filepath.Join(t.TempDir(), "sweep-failure"),
			args: []string{"sweep", "--file", writeDSLFixture(t, `
product Widget {
    param width: number = 1
    param scaled: number = 10 / width
}
`), "--product", "Widget", "--param", "width=0..1:1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			outDir := filepath.Join(t.TempDir(), "out")
			args := append([]string(nil), tc.args...)
			if tc.out != "" {
				outDir = tc.out
			}
			hasOut := false
			for _, arg := range args {
				if arg == "--out" {
					hasOut = true
					break
				}
			}
			if !hasOut {
				args = append(args, "--out", outDir)
			}

			_, err := executeHarnessCommand(t, args)
			if tc.name == "captures planning failure" {
				if err != nil {
					t.Fatalf("unexpected command error: %v", err)
				}
				var report sweepReport
				readJSONFile(t, filepath.Join(outDir, "sweep_report.json"), &report)
				if report.FailedCases != 1 || report.PassedCases != 1 {
					t.Fatalf("unexpected sweep failure summary: %+v", report)
				}
				return
			}
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSnapshotCommand(t *testing.T) {
	dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 100
    param material: string = "Steel"
}
`)
	inputsPath := writeJSONFixture(t, `{"width": 120, "material": "Aluminum"}`)
	outDir := filepath.Join(t.TempDir(), "snapshot")

	stdout, err := executeHarnessCommand(t, []string{
		"snapshot",
		"--file", dslPath,
		"--inputs", inputsPath,
		"--out", outDir,
	})
	if err != nil {
		t.Fatalf("unexpected snapshot error: %v", err)
	}
	if !strings.Contains(stdout, "Snapshot created") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}

	for _, name := range []string{"snapshot.json", "plan.json", "inputs.json", report.FileName, metadata.FileName, "manifest.json"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	var descriptor snapshotDescriptor
	readJSONFile(t, filepath.Join(outDir, "snapshot.json"), &descriptor)
	if descriptor.PlanHash == "" || descriptor.DSLHash == "" {
		t.Fatalf("expected hashes in snapshot descriptor: %+v", descriptor)
	}
	if descriptor.Inputs["material"] != "Aluminum" {
		t.Fatalf("unexpected snapshot inputs: %+v", descriptor.Inputs)
	}
	if !sortStrings(descriptor.GeneratedFiles) {
		t.Fatalf("expected generated files to be sorted: %+v", descriptor.GeneratedFiles)
	}

	firstSnapshot, err := os.ReadFile(filepath.Join(outDir, "snapshot.json"))
	if err != nil {
		t.Fatalf("failed to read first snapshot: %v", err)
	}
	if err := os.RemoveAll(outDir); err != nil {
		t.Fatalf("failed to clean snapshot dir: %v", err)
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatalf("failed to recreate snapshot dir: %v", err)
	}
	if _, err := executeHarnessCommand(t, []string{
		"snapshot",
		"--file", dslPath,
		"--inputs", inputsPath,
		"--out", outDir,
	}); err != nil {
		t.Fatalf("unexpected snapshot rerun error: %v", err)
	}
	secondSnapshot, err := os.ReadFile(filepath.Join(outDir, "snapshot.json"))
	if err != nil {
		t.Fatalf("failed to read second snapshot: %v", err)
	}
	if !bytes.Equal(firstSnapshot, secondSnapshot) {
		t.Fatal("snapshot.json changed between identical runs")
	}
}

func TestDiffCommand(t *testing.T) {
	t.Run("identical plans compare equal", func(t *testing.T) {
		planPath := writeJSONFixture(t, `{"plan":{"steps":[{"type":"WriteCSV"}]},"hash":"abc"}`)
		stdout, err := executeHarnessCommand(t, []string{"diff", "--plan", planPath, "--plan", planPath})
		if err != nil {
			t.Fatalf("unexpected diff error: %v", err)
		}
		if !strings.Contains(stdout, "plan: equal") {
			t.Fatalf("unexpected stdout: %q", stdout)
		}
	})

	t.Run("different plans compare different and support json", func(t *testing.T) {
		planA := writeJSONFixture(t, `{"plan":{"steps":[{"type":"WriteCSV"}]},"hash":"abc"}`)
		planB := writeJSONFixture(t, `{"plan":{"steps":[{"type":"WriteCSV"},{"type":"RunCADRuntime"}]},"hash":"def"}`)
		stdout, err := executeHarnessCommand(t, []string{"diff", "--plan", planA, "--plan", planB})
		if err != nil {
			t.Fatalf("unexpected diff error: %v", err)
		}
		if !strings.Contains(stdout, "plan: different") {
			t.Fatalf("unexpected stdout: %q", stdout)
		}

		jsonStdout, err := executeHarnessCommand(t, []string{"diff", "--plan", planA, "--plan", planB, "--json"})
		if err != nil {
			t.Fatalf("unexpected json diff error: %v", err)
		}
		var summary diffSummary
		if err := json.Unmarshal([]byte(jsonStdout), &summary); err != nil {
			t.Fatalf("failed to decode diff json: %v", err)
		}
		if summary.Equal {
			t.Fatalf("expected plan diff to report difference: %+v", summary)
		}
	})

	t.Run("identical and different snapshots compare correctly", func(t *testing.T) {
		dslPath := writeDSLFixture(t, `
product Widget {
    param width: number = 100
}
`)
		inputsA := writeJSONFixture(t, `{"width": 120}`)
		inputsB := writeJSONFixture(t, `{"width": 140}`)
		snapshotA := filepath.Join(t.TempDir(), "snap-a")
		snapshotB := filepath.Join(t.TempDir(), "snap-b")
		snapshotC := filepath.Join(t.TempDir(), "snap-c")

		if _, err := executeHarnessCommand(t, []string{"snapshot", "--file", dslPath, "--inputs", inputsA, "--out", snapshotA}); err != nil {
			t.Fatalf("unexpected snapshot A error: %v", err)
		}
		if _, err := executeHarnessCommand(t, []string{"snapshot", "--file", dslPath, "--inputs", inputsA, "--out", snapshotB}); err != nil {
			t.Fatalf("unexpected snapshot B error: %v", err)
		}
		if _, err := executeHarnessCommand(t, []string{"snapshot", "--file", dslPath, "--inputs", inputsB, "--out", snapshotC}); err != nil {
			t.Fatalf("unexpected snapshot C error: %v", err)
		}

		stdout, err := executeHarnessCommand(t, []string{"diff", "--snapshot", snapshotA, "--snapshot", snapshotB})
		if err != nil {
			t.Fatalf("unexpected snapshot diff error: %v", err)
		}
		if !strings.Contains(stdout, "snapshot: equal") {
			t.Fatalf("unexpected equal snapshot stdout: %q", stdout)
		}

		stdout, err = executeHarnessCommand(t, []string{"diff", "--snapshot", snapshotA, "--snapshot", snapshotC})
		if err != nil {
			t.Fatalf("unexpected snapshot diff error: %v", err)
		}
		if !strings.Contains(stdout, "snapshot: different") {
			t.Fatalf("unexpected different snapshot stdout: %q", stdout)
		}
	})
}

func buildExpectedProjectLockBytes(t *testing.T, entryPath string) (*projectlock.LockFile, []byte) {
	t.Helper()

	resolved, err := projectinput.ResolveProject(entryPath)
	if err != nil {
		t.Fatalf("ResolveProject returned error: %v", err)
	}
	if resolved == nil {
		t.Fatalf("expected project entrypoint, got nil resolution for %q", entryPath)
	}

	project, err := projectmap.Load(resolved.ProjectFile)
	if err != nil {
		t.Fatalf("projectmap.Load returned error: %v", err)
	}
	captured, err := projectinput.CaptureResources(resolved)
	if err != nil {
		t.Fatalf("CaptureResources returned error: %v", err)
	}
	lock, err := projectlock.Build(project, resolved.ProjectFile, captured)
	if err != nil {
		t.Fatalf("projectlock.Build returned error: %v", err)
	}

	path := filepath.Join(t.TempDir(), projectlock.FileName)
	if err := projectlock.Write(path, lock); err != nil {
		t.Fatalf("projectlock.Write returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read expected lock bytes: %v", err)
	}

	return lock, data
}

func executeHarnessCommand(t *testing.T, args []string) (string, error) {
	t.Helper()
	resetGlobals()

	cmd := newHarnessRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String(), err
}

func writeDSLFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.dsl")
	if err := os.WriteFile(path, []byte(withVersionHeader(content)), 0644); err != nil {
		t.Fatalf("failed to write DSL fixture: %v", err)
	}
	return path
}

func writeJSONFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write JSON fixture: %v", err)
	}
	return path
}

func writeTableFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.table.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write table fixture: %v", err)
	}
	return path
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("failed to decode %s: %v", path, err)
	}
}

func firstManifestPayloadFromPlan(t *testing.T, plan *planner.ExecutionPlan) planner.WriteExportManifestPayload {
	t.Helper()

	if plan == nil {
		t.Fatal("expected non-nil execution plan")
	}
	for _, step := range plan.Steps {
		if step.Type != planner.StepWriteExportManifest {
			continue
		}
		payload, ok := step.Payload.(planner.WriteExportManifestPayload)
		if !ok {
			t.Fatalf("expected WriteExportManifestPayload, got %T", step.Payload)
		}
		return payload
	}

	t.Fatal("expected execution plan to contain a WriteExportManifest step")
	return planner.WriteExportManifestPayload{}
}

func manifestPayloadsByProduct(t *testing.T, plan *planner.ExecutionPlan) map[string]planner.WriteExportManifestPayload {
	t.Helper()

	if plan == nil {
		t.Fatal("expected non-nil execution plan")
	}

	manifests := make(map[string]planner.WriteExportManifestPayload)
	for _, step := range plan.Steps {
		if step.Type != planner.StepWriteExportManifest {
			continue
		}
		payload, ok := step.Payload.(planner.WriteExportManifestPayload)
		if !ok {
			t.Fatalf("expected WriteExportManifestPayload, got %T", step.Payload)
		}
		manifests[payload.Product.ID] = payload
	}
	if len(manifests) == 0 {
		t.Fatal("expected execution plan to contain WriteExportManifest steps")
	}
	return manifests
}

func runCADRuntimeProductKeys(plan *planner.ExecutionPlan) []string {
	var products []string
	if plan == nil {
		return products
	}
	for _, step := range plan.Steps {
		if step.Type != planner.StepRunCADRuntime {
			continue
		}
		payload, ok := step.Payload.(planner.RunCADRuntimePayload)
		if ok {
			products = append(products, payload.ProductKey)
		}
	}
	return products
}

func sortedManifestKeys(manifests map[string]planner.WriteExportManifestPayload) []string {
	keys := make([]string, 0, len(manifests))
	for key := range manifests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeMixedAdapterProjectFixture(t *testing.T) (string, string) {
	t.Helper()

	projectDir := t.TempDir()

	projectDSL := filepath.Join(projectDir, "project.dsl")
	if err := os.WriteFile(projectDSL, []byte(withVersionHeader(`
product FreeCADBox {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param label: string = "box"
}

product NoAdapterSummary {
    adapter = "none"

    param label: string = "summary"
}
`)), 0o644); err != nil {
		t.Fatalf("failed to write project DSL: %v", err)
	}

	modelDir := filepath.Join(projectDir, "input")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	modelPath := filepath.Join(modelDir, "box.FCStd")
	if err := os.WriteFile(modelPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("failed to write model fixture: %v", err)
	}

	projectMap := filepath.Join(projectDir, "parametron.project.json")
	if err := os.WriteFile(projectMap, []byte(`{
  "version": "1.0",
  "projectId": "mixed-adapter-project",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "box_model": "input/box.FCStd"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("failed to write project map: %v", err)
	}

	return projectDir, modelPath
}

func sortStrings(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1] > values[i] {
			return false
		}
	}
	return true
}

func copyFixtureTree(t *testing.T, src string) string {
	t.Helper()

	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("failed to create fixture copy root: %v", err)
	}

	if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	}); err != nil {
		t.Fatalf("failed to copy fixture tree from %s: %v", src, err)
	}

	return dst
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repository root")
		}
		dir = parent
	}
}
