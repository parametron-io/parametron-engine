package projectlock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"parametron/internal/engine/projectinput"
	"parametron/internal/engine/tableloader"
	"parametron/internal/shared/projectmap"
)

func TestRead_MinimalValidLockWithoutTables(t *testing.T) {
	lock, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "demo-project",
  "dsl": {
    "path": "./project.dsl",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "resources": {
    "models": {
      "box_model": {
        "path": "models/box.FCStd",
        "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }
  }
}`))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	want := &LockFile{
		Version:   Version,
		ProjectID: "demo-project",
		DSL: LockedDSLEntry{
			Path:   "project.dsl",
			SHA256: repeatHex('a'),
		},
		Resources: LockedResources{
			Models: map[string]LockedModelEntry{
				"box_model": {
					Path:   "models/box.FCStd",
					SHA256: repeatHex('b'),
				},
			},
		},
	}

	if !reflect.DeepEqual(lock, want) {
		t.Fatalf("unexpected lock\nwant: %#v\ngot:  %#v", want, lock)
	}
}

func TestRead_ValidLockWithMultipleModelsAndTables(t *testing.T) {
	lock, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "airframe-family",
  "dsl": {
    "path": "dsl/../project.dsl",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "resources": {
    "models": {
      "surface_master": {
        "path": "models/catia/surface_master.CATPart",
        "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      },
      "mech_assembly": {
        "path": "./models/solidworks/mech_assembly.SLDASM",
        "sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
      }
    }
  },
  "tables": {
    "fasteners": {
      "path": "tables/fasteners.json",
      "fingerprint": "fasteners-v1"
    },
    "materials": {
      "path": "tables/catalogs/../materials.json",
      "fingerprint": "materials-v1"
    }
  }
}`))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if lock.DSL.Path != "project.dsl" {
		t.Fatalf("expected cleaned DSL path, got %q", lock.DSL.Path)
	}
	if got := lock.Resources.Models["mech_assembly"].Path; got != "models/solidworks/mech_assembly.SLDASM" {
		t.Fatalf("expected cleaned model path, got %q", got)
	}
	if got := lock.Tables["materials"].Path; got != "tables/materials.json" {
		t.Fatalf("expected cleaned table path, got %q", got)
	}
}

func TestRead_NormalizesEquivalentRelativePathsDeterministically(t *testing.T) {
	base := []byte(`{
  "version": "1.0",
  "projectId": "stable-project",
  "dsl": {
    "path": "project.dsl",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "resources": {
    "models": {
      "box_model": {
        "path": "models/box.FCStd",
        "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }
  },
  "tables": {
    "fasteners": {
      "path": "tables/fasteners.json",
      "fingerprint": "fasteners-v1"
    }
  }
}`)

	equivalent := []byte(`{
  "version": "1.0",
  "projectId": "stable-project",
  "dsl": {
    "path": "./dsl/../project.dsl",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "resources": {
    "models": {
      "box_model": {
        "path": "./models/nested/../box.FCStd",
        "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }
  },
  "tables": {
    "fasteners": {
      "path": "./tables/catalog/../fasteners.json",
      "fingerprint": "fasteners-v1"
    }
  }
}`)

	want, err := Read(base)
	if err != nil {
		t.Fatalf("Read(base) returned error: %v", err)
	}
	got, err := Read(equivalent)
	if err != nil {
		t.Fatalf("Read(equivalent) returned error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected normalized locks to match\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestRead_NonObjectRoot(t *testing.T) {
	_, err := Read([]byte(`[]`))
	assertValidationMessages(t, err, "root must be a JSON object")
}

func TestRead_TrailingJSON(t *testing.T) {
	_, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "demo-project",
  "dsl": {"path": "project.dsl", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "resources": {"models": {}}
} {}`))
	if err == nil {
		t.Fatal("expected Read to fail")
	}
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}
}

func TestRead_UnknownFieldsRejected(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		message string
	}{
		{
			name: "root",
			json: `{
  "version": "1.0",
  "projectId": "demo-project",
  "dsl": {"path": "project.dsl", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "resources": {"models": {}},
  "unexpected": true
}`,
			message: `root has unknown field "unexpected"`,
		},
		{
			name: "dsl",
			json: `{
  "version": "1.0",
  "projectId": "demo-project",
  "dsl": {
    "path": "project.dsl",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "extra": true
  },
  "resources": {"models": {}}
}`,
			message: `dsl has unknown field "extra"`,
		},
		{
			name: "model_entry",
			json: `{
  "version": "1.0",
  "projectId": "demo-project",
  "dsl": {"path": "project.dsl", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "resources": {
    "models": {
      "box_model": {
        "path": "models/box.FCStd",
        "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        "extra": true
      }
    }
  }
}`,
			message: `resources.models["box_model"] has unknown field "extra"`,
		},
		{
			name: "table_entry",
			json: `{
  "version": "1.0",
  "projectId": "demo-project",
  "dsl": {"path": "project.dsl", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "resources": {"models": {}},
  "tables": {
    "fasteners": {
      "path": "tables/fasteners.json",
      "fingerprint": "fp-v1",
      "extra": true
    }
  }
}`,
			message: `tables["fasteners"] has unknown field "extra"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read([]byte(tt.json))
			assertValidationMessages(t, err, tt.message)
		})
	}
}

func TestRead_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		message string
	}{
		{
			name: "version",
			json: `{
  "projectId": "demo-project",
  "dsl": {"path": "project.dsl", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "resources": {"models": {}}
}`,
			message: "version is required",
		},
		{
			name: "projectId",
			json: `{
  "version": "1.0",
  "dsl": {"path": "project.dsl", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "resources": {"models": {}}
}`,
			message: "projectId is required",
		},
		{
			name: "dsl",
			json: `{
  "version": "1.0",
  "projectId": "demo-project",
  "resources": {"models": {}}
}`,
			message: "dsl is required",
		},
		{
			name: "resources",
			json: `{
  "version": "1.0",
  "projectId": "demo-project",
  "dsl": {"path": "project.dsl", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
}`,
			message: "resources is required",
		},
		{
			name: "resources.models",
			json: `{
  "version": "1.0",
  "projectId": "demo-project",
  "dsl": {"path": "project.dsl", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "resources": {}
}`,
			message: "models is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read([]byte(tt.json))
			assertValidationMessages(t, err, tt.message)
		})
	}
}

func TestValidate_InvalidForms(t *testing.T) {
	base := func() *LockFile {
		return &LockFile{
			Version:   Version,
			ProjectID: "demo-project",
			DSL: LockedDSLEntry{
				Path:   "project.dsl",
				SHA256: repeatHex('a'),
			},
			Resources: LockedResources{
				Models: map[string]LockedModelEntry{
					"box_model": {
						Path:   "models/box.FCStd",
						SHA256: repeatHex('b'),
					},
				},
			},
			Tables: map[string]LockedTableEntry{
				"fasteners": {
					Path:        "tables/fasteners.json",
					Fingerprint: "fp-v1",
				},
			},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*LockFile)
		message string
	}{
		{name: "invalid_version", mutate: func(lock *LockFile) { lock.Version = "1.1" }, message: `version must be "1.0"`},
		{name: "empty_project_id", mutate: func(lock *LockFile) { lock.ProjectID = "" }, message: "projectId is required"},
		{name: "spaced_project_id", mutate: func(lock *LockFile) { lock.ProjectID = " demo-project " }, message: "projectId must not have leading or trailing whitespace"},
		{name: "spaced_model_id", mutate: func(lock *LockFile) {
			lock.Resources.Models[" box_model "] = lock.Resources.Models["box_model"]
			delete(lock.Resources.Models, "box_model")
		}, message: `resources.models logical ID " box_model " must not have leading or trailing whitespace`},
		{name: "spaced_table_id", mutate: func(lock *LockFile) {
			lock.Tables[" fasteners "] = lock.Tables["fasteners"]
			delete(lock.Tables, "fasteners")
		}, message: `tables logical ID " fasteners " must not have leading or trailing whitespace`},
		{name: "empty_dsl_hash", mutate: func(lock *LockFile) { lock.DSL.SHA256 = "" }, message: "dsl.sha256 is required"},
		{name: "uppercase_dsl_hash", mutate: func(lock *LockFile) { lock.DSL.SHA256 = "ABCDEF" }, message: "dsl.sha256 must be lowercase hexadecimal"},
		{name: "empty_model_hash", mutate: func(lock *LockFile) {
			entry := lock.Resources.Models["box_model"]
			entry.SHA256 = ""
			lock.Resources.Models["box_model"] = entry
		}, message: `resources.models["box_model"].sha256 is required`},
		{name: "empty_table_fingerprint", mutate: func(lock *LockFile) {
			entry := lock.Tables["fasteners"]
			entry.Fingerprint = ""
			lock.Tables["fasteners"] = entry
		}, message: `tables["fasteners"].fingerprint is required`},
		{name: "empty_dsl_path", mutate: func(lock *LockFile) { lock.DSL.Path = "" }, message: "dsl.path must not be empty"},
		{name: "absolute_model_path", mutate: func(lock *LockFile) {
			entry := lock.Resources.Models["box_model"]
			entry.Path = "/models/box.FCStd"
			lock.Resources.Models["box_model"] = entry
		}, message: `resources.models["box_model"].path "/models/box.FCStd" must be relative`},
		{name: "traversal_table_path", mutate: func(lock *LockFile) {
			entry := lock.Tables["fasteners"]
			entry.Path = "../tables/fasteners.json"
			lock.Tables["fasteners"] = entry
		}, message: `tables["fasteners"].path "../tables/fasteners.json" must stay within project root`},
		{name: "dot_dsl_path", mutate: func(lock *LockFile) { lock.DSL.Path = "." }, message: `dsl.path "." must not resolve to current directory`},
		{name: "spaced_dsl_path", mutate: func(lock *LockFile) { lock.DSL.Path = " project.dsl " }, message: `dsl.path " project.dsl " must not have leading or trailing whitespace`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := base()
			tt.mutate(lock)
			err := Validate(lock)
			assertValidationMessages(t, err, tt.message)
		})
	}
}

func TestBuild_FromProjectAndCapturedResources(t *testing.T) {
	projectDir, project, captured := writeProjectLockFixture(t)

	lock, err := Build(project, filepath.Join(projectDir, projectmap.FileName), captured)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if lock.Version != Version {
		t.Fatalf("unexpected version: %q", lock.Version)
	}
	if lock.ProjectID != "capture-project" {
		t.Fatalf("unexpected project ID: %q", lock.ProjectID)
	}
	if lock.DSL.Path != "project.dsl" {
		t.Fatalf("expected relative normalized DSL path, got %q", lock.DSL.Path)
	}
	if lock.DSL.Path == captured.DSL.ResolvedPath {
		t.Fatalf("expected project-relative DSL path, got resolved path %q", lock.DSL.Path)
	}
	if got := lock.Resources.Models["box_model"].Path; got != "models/box.FCStd" {
		t.Fatalf("unexpected model path: %q", got)
	}
	if got := lock.Tables["labels"].Path; got != "tables/labels.json" {
		t.Fatalf("unexpected table path: %q", got)
	}
}

func TestBuild_DeterministicMarshalOutput(t *testing.T) {
	projectDir, project, captured := writeProjectLockFixture(t)
	projectFile := filepath.Join(projectDir, projectmap.FileName)

	first, err := Build(project, projectFile, captured)
	if err != nil {
		t.Fatalf("first Build returned error: %v", err)
	}
	second, err := Build(project, projectFile, captured)
	if err != nil {
		t.Fatalf("second Build returned error: %v", err)
	}

	firstJSON := mustMarshalLock(t, first)
	secondJSON := mustMarshalLock(t, second)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("expected identical JSON\nfirst:\n%s\nsecond:\n%s", firstJSON, secondJSON)
	}

	if got, want := sortedModelKeys(first), []string{"box_model", "cover_model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected model ordering via keys: got %v want %v", got, want)
	}
	if got, want := sortedTableKeys(first), []string{"fasteners", "labels"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected table ordering via keys: got %v want %v", got, want)
	}
}

func TestBuild_MismatchErrors(t *testing.T) {
	projectDir, project, captured := writeProjectLockFixture(t)
	projectFile := filepath.Join(projectDir, projectmap.FileName)

	tests := []struct {
		name    string
		mutate  func(*projectinput.CapturedResources)
		message string
	}{
		{
			name: "missing_captured_model",
			mutate: func(c *projectinput.CapturedResources) {
				c.Models = c.Models[1:]
			},
			message: `captured resources missing model logical ID "box_model"`,
		},
		{
			name: "extra_captured_model",
			mutate: func(c *projectinput.CapturedResources) {
				c.Models = append(c.Models, projectinput.CapturedResource{
					LogicalID:    "extra_model",
					ResolvedPath: "/tmp/extra.FCStd",
					Signature:    repeatHex('e'),
					Kind:         projectinput.ResourceKindModel,
				})
			},
			message: `captured resources have extra model logical IDs: "extra_model"`,
		},
		{
			name: "duplicate_captured_model",
			mutate: func(c *projectinput.CapturedResources) {
				c.Models = append(c.Models, c.Models[0])
			},
			message: `captured resources contain duplicate model logical ID "box_model"`,
		},
		{
			name: "missing_captured_table",
			mutate: func(c *projectinput.CapturedResources) {
				c.Tables = c.Tables[1:]
			},
			message: `captured resources missing table logical ID "fasteners"`,
		},
		{
			name: "extra_captured_table",
			mutate: func(c *projectinput.CapturedResources) {
				c.Tables = append(c.Tables, projectinput.CapturedResource{
					LogicalID:    "extra_table",
					ResolvedPath: "/tmp/extra.json",
					Signature:    "extra-fingerprint",
					Kind:         projectinput.ResourceKindTable,
				})
			},
			message: `captured resources have extra table logical IDs: "extra_table"`,
		},
		{
			name: "duplicate_captured_table",
			mutate: func(c *projectinput.CapturedResources) {
				c.Tables = append(c.Tables, c.Tables[0])
			},
			message: `captured resources contain duplicate table logical ID "fasteners"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutated := projectinput.CloneCapturedResources(captured)
			tt.mutate(mutated)
			_, err := Build(project, projectFile, mutated)
			assertValidationMessages(t, err, tt.message)
		})
	}
}

func TestBuild_NilInputs(t *testing.T) {
	_, err := Build(nil, "", nil)
	assertValidationMessages(t, err, "project mapping must not be nil", "captured resources must not be nil")
}

func TestWriteAndLoad_RoundTripAndDeterministicBytes(t *testing.T) {
	projectDir, project, captured := writeProjectLockFixture(t)
	lock, err := Build(project, filepath.Join(projectDir, projectmap.FileName), captured)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	path := filepath.Join(t.TempDir(), "nested", FileName)
	if err := Write(path, lock); err != nil {
		t.Fatalf("first Write returned error: %v", err)
	}
	firstBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read first write: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !reflect.DeepEqual(loaded, lock) {
		t.Fatalf("expected round-trip lock equality\nwant: %#v\ngot:  %#v", lock, loaded)
	}

	if err := Write(path, lock); err != nil {
		t.Fatalf("second Write returned error: %v", err)
	}
	secondBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read second write: %v", err)
	}

	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("expected repeated writes to match\nfirst:\n%s\nsecond:\n%s", firstBytes, secondBytes)
	}
	if len(secondBytes) == 0 || secondBytes[len(secondBytes)-1] != '\n' {
		t.Fatalf("expected trailing newline, got %q", secondBytes)
	}
}

func TestWrite_CreatesParentDirectory(t *testing.T) {
	lock := &LockFile{
		Version:   Version,
		ProjectID: "demo-project",
		DSL: LockedDSLEntry{
			Path:   "project.dsl",
			SHA256: repeatHex('a'),
		},
		Resources: LockedResources{Models: map[string]LockedModelEntry{}},
	}

	path := filepath.Join(t.TempDir(), "missing", "dirs", FileName)
	if err := Write(path, lock); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist after Write: %v", err)
	}
}

func assertValidationMessages(t *testing.T, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func writeProjectLockFixture(t *testing.T) (string, *projectmap.Project, *projectinput.CapturedResources) {
	t.Helper()

	projectDir := t.TempDir()
	modelDir := filepath.Join(projectDir, "models")
	tableDir := filepath.Join(projectDir, "tables")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.MkdirAll(tableDir, 0o755); err != nil {
		t.Fatalf("failed to create table dir: %v", err)
	}

	dslPath := filepath.Join(projectDir, "project.dsl")
	if err := os.WriteFile(dslPath, []byte("dsl v1.0\nproduct Widget {}\n"), 0o644); err != nil {
		t.Fatalf("failed to write DSL: %v", err)
	}

	boxModelPath := filepath.Join(modelDir, "box.FCStd")
	if err := os.WriteFile(boxModelPath, []byte("box-model"), 0o644); err != nil {
		t.Fatalf("failed to write box model: %v", err)
	}
	coverModelPath := filepath.Join(modelDir, "cover.FCStd")
	if err := os.WriteFile(coverModelPath, []byte("cover-model"), 0o644); err != nil {
		t.Fatalf("failed to write cover model: %v", err)
	}

	fastenersPath := filepath.Join(tableDir, "fasteners.json")
	if err := os.WriteFile(fastenersPath, []byte(`{
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
		t.Fatalf("failed to write fasteners table: %v", err)
	}
	labelsPath := filepath.Join(tableDir, "labels.json")
	if err := os.WriteFile(labelsPath, []byte(`{
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
}`), 0o644); err != nil {
		t.Fatalf("failed to write labels table: %v", err)
	}

	project := &projectmap.Project{
		Version:   projectmap.Version,
		ProjectID: "capture-project",
		DSL:       "./dsl/../project.dsl",
		Resources: projectmap.Resources{
			Models: map[string]string{
				"cover_model": ".\\models\\cover.FCStd",
				"box_model":   "./models/box.FCStd",
			},
		},
		Tables: map[string]string{
			"labels":    "./tables/labels.json",
			"fasteners": "./tables/fasteners.json",
		},
	}

	captured := &projectinput.CapturedResources{
		DSL: projectinput.CapturedResource{
			ResolvedPath: dslPath,
			Signature:    mustFileSHA256(t, dslPath),
			Kind:         projectinput.ResourceKindDSL,
		},
		Models: []projectinput.CapturedResource{
			{
				LogicalID:    "box_model",
				ResolvedPath: boxModelPath,
				Signature:    mustFileSHA256(t, boxModelPath),
				Kind:         projectinput.ResourceKindModel,
			},
			{
				LogicalID:    "cover_model",
				ResolvedPath: coverModelPath,
				Signature:    mustFileSHA256(t, coverModelPath),
				Kind:         projectinput.ResourceKindModel,
			},
		},
		Tables: []projectinput.CapturedResource{
			{
				LogicalID:    "fasteners",
				ResolvedPath: fastenersPath,
				Signature:    mustTableFingerprint(t, fastenersPath),
				Kind:         projectinput.ResourceKindTable,
			},
			{
				LogicalID:    "labels",
				ResolvedPath: labelsPath,
				Signature:    mustTableFingerprint(t, labelsPath),
				Kind:         projectinput.ResourceKindTable,
			},
		},
	}

	return projectDir, project, captured
}

func mustMarshalLock(t *testing.T, lock *LockFile) []byte {
	t.Helper()
	data, err := marshal(lock)
	if err != nil {
		t.Fatalf("marshal returned error: %v", err)
	}
	return data
}

func repeatHex(ch byte) string {
	return string(bytes.Repeat([]byte{ch}, 64))
}

func mustFileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func mustTableFingerprint(t *testing.T, path string) string {
	t.Helper()
	loaded, err := tableloader.LoadFiles(map[string]string{"table": path})
	if err != nil {
		t.Fatalf("LoadFiles(%s) failed: %v", path, err)
	}
	fingerprint, err := loaded["table"].Fingerprint()
	if err != nil {
		t.Fatalf("Fingerprint(%s) failed: %v", path, err)
	}
	return fingerprint
}

func sortedModelKeys(lock *LockFile) []string {
	keys := make([]string, 0, len(lock.Resources.Models))
	for key := range lock.Resources.Models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedTableKeys(lock *LockFile) []string {
	keys := make([]string, 0, len(lock.Tables))
	for key := range lock.Tables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
