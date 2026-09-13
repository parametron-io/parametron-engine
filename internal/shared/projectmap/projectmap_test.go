package projectmap

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRead_MinimalValidMapping(t *testing.T) {
	project, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "logical_model_id": "relative/path/to/model.FCStd"
    }
  }
}`))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if project.Version != Version {
		t.Fatalf("unexpected version: %q", project.Version)
	}
	if project.ProjectID != "example-project" {
		t.Fatalf("unexpected project ID: %q", project.ProjectID)
	}
	if project.DSL != "project.dsl" {
		t.Fatalf("unexpected DSL path: %q", project.DSL)
	}
	if got := project.Resources.Models["logical_model_id"]; got != "relative/path/to/model.FCStd" {
		t.Fatalf("unexpected model path: %q", got)
	}
	if project.Tables == nil {
		t.Fatal("expected empty tables map, got nil")
	}
	if len(project.Tables) != 0 {
		t.Fatalf("expected no tables, got %d", len(project.Tables))
	}
}

func TestRead_ValidMappingWithMultipleModelsAndTables(t *testing.T) {
	project, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "airframe-family",
  "dsl": "dsl/../airframe.dsl",
  "resources": {
    "models": {
      "surface_master": "models/catia/surface_master.CATPart",
      "mech_assembly": "./models/solidworks/mech_assembly.SLDASM"
    }
  },
  "tables": {
    "fasteners": "tables/fasteners.json",
    "materials": "tables/catalogs/../materials.json"
  }
}`))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if project.DSL != "airframe.dsl" {
		t.Fatalf("expected cleaned DSL path, got %q", project.DSL)
	}
	if got := project.Resources.Models["mech_assembly"]; got != "models/solidworks/mech_assembly.SLDASM" {
		t.Fatalf("expected cleaned model path, got %q", got)
	}
	if got := project.Tables["materials"]; got != "tables/materials.json" {
		t.Fatalf("expected cleaned table path, got %q", got)
	}
}

func TestRead_DeterministicAcrossRepeatedCalls(t *testing.T) {
	data := []byte(`{
  "version": "1.0",
  "projectId": "stable-project",
  "dsl": "./project.dsl",
  "resources": {
    "models": {
      "Alpha": "models/alpha.FCStd",
      "alpha": "./models/nested/../alpha_lower.FCStd"
    }
  },
  "tables": {
    "fasteners": "tables/fasteners.json"
  }
}`)

	first, err := Read(data)
	if err != nil {
		t.Fatalf("first Read returned error: %v", err)
	}
	second, err := Read(data)
	if err != nil {
		t.Fatalf("second Read returned error: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected repeated reads to match\nfirst: %#v\nsecond: %#v", first, second)
	}
	if err := Validate(first); err != nil {
		t.Fatalf("Validate(first) returned error: %v", err)
	}
	if err := Validate(second); err != nil {
		t.Fatalf("Validate(second) returned error: %v", err)
	}
}

func TestRead_NormalizesEquivalentRelativeSpellingsDeterministically(t *testing.T) {
	base := []byte(`{
  "version": "1.0",
  "projectId": "equivalent-project",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "box_model": "models/box.FCStd"
    }
  },
  "tables": {
    "fasteners": "tables/fasteners.json"
  }
}`)

	withDots := []byte(`{
  "version": "1.0",
  "projectId": "equivalent-project",
  "dsl": "./project.dsl",
  "resources": {
    "models": {
      "box_model": "./models/box.FCStd"
    }
  },
  "tables": {
    "fasteners": "./tables/fasteners.json"
  }
}`)

	withNestedParents := []byte(`{
  "version": "1.0",
  "projectId": "equivalent-project",
  "dsl": "dsl/../project.dsl",
  "resources": {
    "models": {
      "box_model": "models/nested/../box.FCStd"
    }
  },
  "tables": {
    "fasteners": "tables/catalog/../fasteners.json"
  }
}`)

	want, err := Read(base)
	if err != nil {
		t.Fatalf("Read(base) returned error: %v", err)
	}

	tests := []struct {
		name string
		data []byte
	}{
		{name: "dot_prefixed", data: withDots},
		{name: "nested_parent_segments", data: withNestedParents},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Read(tt.data)
			if err != nil {
				t.Fatalf("Read(%s) returned error: %v", tt.name, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("expected normalized project to match base\nwant: %#v\ngot:  %#v", want, got)
			}
		})
	}
}

func TestLoad_MissingFilePreservesIOClassification(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected Load to fail")
	}
	if !errors.Is(err, ErrIO) {
		t.Fatalf("expected ErrIO, got %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestLoad_InvalidJSONPreservesDecodeClassification(t *testing.T) {
	path := writeProjectMapFile(t, `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected Load to fail")
	}
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}
	if errors.Is(err, ErrValidation) {
		t.Fatalf("expected decode classification, got validation error: %v", err)
	}
}

func TestLoad_InvalidSchemaPreservesValidationClassification(t *testing.T) {
	path := writeProjectMapFile(t, `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": ".",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected Load to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if errors.Is(err, ErrDecode) {
		t.Fatalf("expected validation classification, got decode error: %v", err)
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	want := []string{`dsl path "." must not resolve to current directory`}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func TestRead_JSONDecodeFailurePreservesClassification(t *testing.T) {
	_, err := Read([]byte(`{"version":"1.0"`))
	if err == nil {
		t.Fatal("expected Read to fail")
	}
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}
	if errors.Is(err, ErrValidation) {
		t.Fatalf("expected decode error, got validation error: %v", err)
	}
}

func TestRead_TrailingJSONPreservesDecodeClassification(t *testing.T) {
	_, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
} {}`))
	if err == nil {
		t.Fatal("expected Read to fail")
	}
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}
	if errors.Is(err, ErrValidation) {
		t.Fatalf("expected decode classification, got validation error: %v", err)
	}
}

func TestRead_NonObjectRootPreservesValidationClassification(t *testing.T) {
	_, err := Read([]byte(`[]`))
	if err == nil {
		t.Fatal("expected Read to fail")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if errors.Is(err, ErrDecode) {
		t.Fatalf("expected validation classification, got decode error: %v", err)
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	want := []string{"root must be a JSON object"}
	if !reflect.DeepEqual(validationErr.Messages(), want) {
		t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", want, validationErr.Messages())
	}
}

func TestLoad_TrailingJSONPreservesDecodeClassification(t *testing.T) {
	path := writeProjectMapFile(t, `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
} {}`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected Load to fail")
	}
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("expected ErrDecode, got %v", err)
	}
	if errors.Is(err, ErrValidation) {
		t.Fatalf("expected decode classification, got validation error: %v", err)
	}
}

func TestRead_ValidationFailures(t *testing.T) {
	tests := []struct {
		name         string
		json         string
		wantMessages []string
	}{
		{
			name: "missing_version",
			json: `{
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`,
			wantMessages: []string{"version is required"},
		},
		{
			name: "unsupported_version",
			json: `{
  "version": "2.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`,
			wantMessages: []string{`unsupported project mapping major version "2.0"`},
		},
		{
			name: "missing_project_id",
			json: `{
  "version": "1.0",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`,
			wantMessages: []string{"projectId is required"},
		},
		{
			name: "missing_dsl",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`,
			wantMessages: []string{"dsl is required"},
		},
		{
			name: "missing_resources",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl"
}`,
			wantMessages: []string{"resources is required"},
		},
		{
			name: "missing_resources_models",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {}
}`,
			wantMessages: []string{"models is required"},
		},
		{
			name: "empty_model_id",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"": "models/a.FCStd"}}
}`,
			wantMessages: []string{"resources.models logical ID must not be empty"},
		},
		{
			name: "whitespace_only_model_id",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"   ": "models/a.FCStd"}}
}`,
			wantMessages: []string{"resources.models logical ID must not be empty"},
		},
		{
			name: "empty_table_id",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}},
  "tables": {"": "tables/a.json"}
}`,
			wantMessages: []string{"tables logical ID must not be empty"},
		},
		{
			name: "empty_path",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": ""}}
}`,
			wantMessages: []string{`resources.models["model"] path must not be empty`},
		},
		{
			name: "whitespace_only_path",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "   "}}
}`,
			wantMessages: []string{`resources.models["model"] path must not be empty`},
		},
		{
			name: "absolute_model_path",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "/models/a.FCStd"}}
}`,
			wantMessages: []string{`resources.models["model"] path "/models/a.FCStd" must be relative`},
		},
		{
			name: "absolute_table_path",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}},
  "tables": {"fasteners": "/tables/a.json"}
}`,
			wantMessages: []string{`tables["fasteners"] path "/tables/a.json" must be relative`},
		},
		{
			name: "traversal_model_path",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "../models/a.FCStd"}}
}`,
			wantMessages: []string{`resources.models["model"] path "../models/a.FCStd" must stay within project root`},
		},
		{
			name: "traversal_table_path",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}},
  "tables": {"fasteners": "./../tables/a.json"}
}`,
			wantMessages: []string{`tables["fasteners"] path "./../tables/a.json" must stay within project root`},
		},
		{
			name: "unknown_top_level_field",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}},
  "unexpected": true
}`,
			wantMessages: []string{`root has unknown field "unexpected"`},
		},
		{
			name: "unknown_resources_field",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {
    "models": {"model": "models/a.FCStd"},
    "tables": {}
  }
}`,
			wantMessages: []string{`resources has unknown field "tables"`},
		},
		{
			name: "wrong_place_resources_tables_with_empty_models",
			json: `{
  "version": "1.0",
  "projectId": "example",
  "dsl": "project.dsl",
  "resources": {
    "models": {},
    "tables": {}
  }
}`,
			wantMessages: []string{`resources has unknown field "tables"`},
		},
		{
			name: "wrong_type_resources_models",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": []}
}`,
			wantMessages: []string{"models must be an object"},
		},
		{
			name: "wrong_type_tables",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}},
  "tables": []
}`,
			wantMessages: []string{"tables must be an object"},
		},
		{
			name: "wrong_type_dsl",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": true,
  "resources": {"models": {"model": "models/a.FCStd"}}
}`,
			wantMessages: []string{"dsl must be a string"},
		},
		{
			name: "wrong_type_project_id",
			json: `{
  "version": "1.0",
  "projectId": true,
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`,
			wantMessages: []string{"projectId must be a string"},
		},
		{
			name: "wrong_type_version",
			json: `{
  "version": true,
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`,
			wantMessages: []string{"version must be a string"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read([]byte(tt.json))
			if err == nil {
				t.Fatal("expected Read to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if !reflect.DeepEqual(validationErr.Messages(), tt.wantMessages) {
				t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", tt.wantMessages, validationErr.Messages())
			}
		})
	}
}

func TestRead_ValidationOutputOrderingIsStable(t *testing.T) {
	data := []byte(`{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "zeta": "../escape-z.FCStd",
      "alpha": "/abs/a.FCStd",
      "  ": "models/blank.FCStd"
    }
  },
  "tables": {
    "beta": "",
    "Alpha": "./../tables/a.json"
  }
}`)

	first := readValidationMessages(t, data)
	second := readValidationMessages(t, data)

	want := []string{
		"resources.models logical ID must not be empty",
		`resources.models["alpha"] path "/abs/a.FCStd" must be relative`,
		`resources.models["zeta"] path "../escape-z.FCStd" must stay within project root`,
		`tables["Alpha"] path "./../tables/a.json" must stay within project root`,
		`tables["beta"] path must not be empty`,
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, first)
	}
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, second)
	}
}

func TestRead_ValidationOutputOrderingIsStableAcrossMixedProblems(t *testing.T) {
	data := []byte(`{
  "version": "1.0",
  "projectId": " example-project ",
  "dsl": ".",
  "resources": {
    "models": {
      "zzz": "C:/models/zzz.FCStd",
      "": "models/blank.FCStd",
      "aaa": "./../escape.FCStd",
      "mid": "."
    }
  },
  "tables": {
    "beta": "C:\\tables\\beta.json",
    "": "",
    "alpha": "dir/../"
  }
}`)

	first := readValidationMessages(t, data)
	second := readValidationMessages(t, data)

	want := []string{
		"projectId must not have leading or trailing whitespace",
		`dsl path "." must not resolve to current directory`,
		"resources.models logical ID must not be empty",
		`resources.models["aaa"] path "./../escape.FCStd" must stay within project root`,
		`resources.models["mid"] path "." must not resolve to current directory`,
		`resources.models["zzz"] path "C:/models/zzz.FCStd" must be relative`,
		"tables logical ID must not be empty",
		`tables["alpha"] path "dir/../" must not resolve to current directory`,
		`tables["beta"] path "C:\\tables\\beta.json" must be relative`,
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, first)
	}
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, second)
	}
}

func TestRead_ProjectMappingVersionSyntaxErrorOrderingIsStable(t *testing.T) {
	data := []byte(`{
  "version": "1.0.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}},
  "unexpected": true
}`)

	first := readValidationMessages(t, data)
	second := readValidationMessages(t, data)
	want := []string{
		`root has unknown field "unexpected"`,
		projectMappingVersionSyntaxMessage,
	}

	if !reflect.DeepEqual(first, want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, first)
	}
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, second)
	}
}

func TestRead_ProjectMappingVersionContract(t *testing.T) {
	tests := []struct {
		name         string
		versionValue string
		wantMessages []string
	}{
		{
			name:         "accepted_supported_version",
			versionValue: `"1.0"`,
			wantMessages: nil,
		},
		{
			name:         "rejected_empty_string",
			versionValue: `""`,
			wantMessages: []string{projectMappingVersionSyntaxMessage},
		},
		{
			name:         "rejected_whitespace_only_string",
			versionValue: `"   "`,
			wantMessages: []string{projectMappingVersionSyntaxMessage},
		},
		{
			name:         "rejected_missing_minor",
			versionValue: `"1"`,
			wantMessages: []string{projectMappingVersionSyntaxMessage},
		},
		{
			name:         "rejected_prefixed_version",
			versionValue: `"v1.0"`,
			wantMessages: []string{projectMappingVersionSyntaxMessage},
		},
		{
			name:         "rejected_patch_version",
			versionValue: `"1.0.0"`,
			wantMessages: []string{projectMappingVersionSyntaxMessage},
		},
		{
			name:         "rejected_wildcard_minor",
			versionValue: `"1.x"`,
			wantMessages: []string{projectMappingVersionSyntaxMessage},
		},
		{
			name:         "rejected_leading_zero_major",
			versionValue: `"01.0"`,
			wantMessages: []string{projectMappingVersionSyntaxMessage},
		},
		{
			name:         "rejected_trailing_whitespace",
			versionValue: `"1.0 "`,
			wantMessages: []string{projectMappingVersionSyntaxMessage},
		},
		{
			name:         "rejected_leading_whitespace",
			versionValue: `" 1.0"`,
			wantMessages: []string{projectMappingVersionSyntaxMessage},
		},
		{
			name:         "rejected_unsupported_major_zero",
			versionValue: `"0.9"`,
			wantMessages: []string{`unsupported project mapping major version "0.9"`},
		},
		{
			name:         "rejected_unsupported_newer_minor_one",
			versionValue: `"1.1"`,
			wantMessages: []string{`unsupported newer project mapping version "1.1"`},
		},
		{
			name:         "rejected_unsupported_newer_minor_nine",
			versionValue: `"1.9"`,
			wantMessages: []string{`unsupported newer project mapping version "1.9"`},
		},
		{
			name:         "rejected_unsupported_major_two_zero",
			versionValue: `"2.0"`,
			wantMessages: []string{`unsupported project mapping major version "2.0"`},
		},
		{
			name:         "rejected_unsupported_major_two_one",
			versionValue: `"2.1"`,
			wantMessages: []string{`unsupported project mapping major version "2.1"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(`{
  "version": ` + tt.versionValue + `,
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`)

			project, err := Read(data)
			if tt.wantMessages == nil {
				if err != nil {
					t.Fatalf("Read returned error: %v", err)
				}
				if project.Version != Version {
					t.Fatalf("unexpected version: %q", project.Version)
				}
				return
			}

			if err == nil {
				t.Fatal("expected Read to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if !reflect.DeepEqual(validationErr.Messages(), tt.wantMessages) {
				t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", tt.wantMessages, validationErr.Messages())
			}

			repeatedMessages := readValidationMessages(t, data)
			if !reflect.DeepEqual(repeatedMessages, tt.wantMessages) {
				t.Fatalf("unexpected repeated validation messages\nwant: %#v\ngot:  %#v", tt.wantMessages, repeatedMessages)
			}
		})
	}
}

func TestRead_ProjectMappingVersionRejectsNonStringType(t *testing.T) {
	data := []byte(`{
  "version": true,
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`)

	messages := readValidationMessages(t, data)
	want := []string{"version must be a string"}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", want, messages)
	}
}

func TestRead_UnsupportedProjectMappingVersionFailsBeforePathValidation(t *testing.T) {
	data := []byte(`{
  "version": "2.0",
  "projectId": "example-project",
  "dsl": ".",
  "resources": {
    "models": {
      "model": "../models/a.FCStd"
    }
  }
}`)

	first := readValidationMessages(t, data)
	second := readValidationMessages(t, data)
	want := []string{`unsupported project mapping major version "2.0"`}

	if !reflect.DeepEqual(first, want) {
		t.Fatalf("unexpected first validation messages\nwant: %#v\ngot:  %#v", want, first)
	}
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("unexpected second validation messages\nwant: %#v\ngot:  %#v", want, second)
	}
}

func TestRead_RejectsEquivalentEscapePathsDeterministically(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "cleaned_escape", path: "a/../.."},
		{name: "dot_prefix_escape", path: "./../x"},
		{name: "mixed_separator_escape", path: `models\\..\\..\\escape.FCStd`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "model": "` + tt.path + `"
    }
  }
}`))
			if err == nil {
				t.Fatal("expected Read to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
			if !strings.Contains(err.Error(), "must stay within project root") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRead_RejectsCrossPlatformAbsolutePathsDeterministically(t *testing.T) {
	tests := []struct {
		name         string
		modelPath    string
		tablePath    string
		wantMessages []string
	}{
		{
			name:         "unix_absolute_model_path",
			modelPath:    "/abs/path/file.FCStd",
			tablePath:    "tables/fasteners.json",
			wantMessages: []string{`resources.models["model"] path "/abs/path/file.FCStd" must be relative`},
		},
		{
			name:         "windows_backslash_absolute_model_path",
			modelPath:    `C:\\models\\file.FCStd`,
			tablePath:    "tables/fasteners.json",
			wantMessages: []string{`resources.models["model"] path "C:\\models\\file.FCStd" must be relative`},
		},
		{
			name:         "windows_slash_absolute_model_path",
			modelPath:    `C:/models/file.FCStd`,
			tablePath:    "tables/fasteners.json",
			wantMessages: []string{`resources.models["model"] path "C:/models/file.FCStd" must be relative`},
		},
		{
			name:         "unc_absolute_table_path",
			modelPath:    "models/file.FCStd",
			tablePath:    `\\\\server\\share\\file.FCStd`,
			wantMessages: []string{`tables["fasteners"] path "\\\\server\\share\\file.FCStd" must be relative`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "` + tt.modelPath + `"}},
  "tables": {"fasteners": "` + tt.tablePath + `"}
}`))
			if err == nil {
				t.Fatal("expected Read to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if !reflect.DeepEqual(validationErr.Messages(), tt.wantMessages) {
				t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", tt.wantMessages, validationErr.Messages())
			}
		})
	}
}

func TestRead_RejectsBackslashPrefixedAbsolutePathsDeterministically(t *testing.T) {
	tests := []struct {
		name         string
		dslPath      string
		modelPath    string
		tablePath    string
		wantMessages []string
	}{
		{
			name:         "backslash_prefixed_dsl_path",
			dslPath:      `\project.dsl`,
			modelPath:    "models/file.FCStd",
			tablePath:    "tables/fasteners.json",
			wantMessages: []string{`dsl path "\\project.dsl" must be relative`},
		},
		{
			name:         "backslash_prefixed_model_path",
			dslPath:      "project.dsl",
			modelPath:    `\models\file.FCStd`,
			tablePath:    "tables/fasteners.json",
			wantMessages: []string{`resources.models["model"] path "\\models\\file.FCStd" must be relative`},
		},
		{
			name:         "backslash_prefixed_table_path",
			dslPath:      "project.dsl",
			modelPath:    "models/file.FCStd",
			tablePath:    `\tables\fasteners.json`,
			wantMessages: []string{`tables["fasteners"] path "\\tables\\fasteners.json" must be relative`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&Project{
				Version:   "1.0",
				ProjectID: "example-project",
				DSL:       tt.dslPath,
				Resources: Resources{
					Models: map[string]string{"model": tt.modelPath},
				},
				Tables: map[string]string{"fasteners": tt.tablePath},
			})
			if err == nil {
				t.Fatal("expected Validate to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if !reflect.DeepEqual(validationErr.Messages(), tt.wantMessages) {
				t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", tt.wantMessages, validationErr.Messages())
			}
		})
	}
}

func TestValidate_PreservesValidationClassification(t *testing.T) {
	tests := []struct {
		name         string
		project      *Project
		wantMessages []string
	}{
		{
			name:         "nil_project",
			project:      nil,
			wantMessages: []string{"project mapping must not be nil"},
		},
		{
			name: "unsupported_version",
			project: &Project{
				Version:   "2.0",
				ProjectID: "example-project",
				DSL:       "project.dsl",
				Resources: Resources{
					Models: map[string]string{"model": "models/a.FCStd"},
				},
			},
			wantMessages: []string{`unsupported project mapping major version "2.0"`},
		},
		{
			name: "table_path_traversal",
			project: &Project{
				Version:   "1.0",
				ProjectID: "example-project",
				DSL:       "project.dsl",
				Resources: Resources{
					Models: map[string]string{"model": "models/a.FCStd"},
				},
				Tables: map[string]string{"fasteners": "./../tables/a.json"},
			},
			wantMessages: []string{`tables["fasteners"] path "./../tables/a.json" must stay within project root`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.project)
			if err == nil {
				t.Fatal("expected Validate to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
			if errors.Is(err, ErrDecode) {
				t.Fatalf("expected validation classification, got decode error: %v", err)
			}
			if errors.Is(err, ErrIO) {
				t.Fatalf("expected validation classification, got I/O error: %v", err)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if !reflect.DeepEqual(validationErr.Messages(), tt.wantMessages) {
				t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", tt.wantMessages, validationErr.Messages())
			}
		})
	}
}

func TestRead_RejectsPathsThatNormalizeToCurrentDirectory(t *testing.T) {
	tests := []struct {
		name         string
		modelPath    string
		tablePath    string
		wantMessages []string
	}{
		{
			name:         "dot_model_path",
			modelPath:    ".",
			tablePath:    "tables/fasteners.json",
			wantMessages: []string{`resources.models["model"] path "." must not resolve to current directory`},
		},
		{
			name:         "collapsed_model_path",
			modelPath:    "a/..",
			tablePath:    "tables/fasteners.json",
			wantMessages: []string{`resources.models["model"] path "a/.." must not resolve to current directory`},
		},
		{
			name:         "collapsed_table_path",
			modelPath:    "models/file.FCStd",
			tablePath:    "dir/../",
			wantMessages: []string{`tables["fasteners"] path "dir/../" must not resolve to current directory`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "` + tt.modelPath + `"}},
  "tables": {"fasteners": "` + tt.tablePath + `"}
}`))
			if err == nil {
				t.Fatal("expected Read to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if !reflect.DeepEqual(validationErr.Messages(), tt.wantMessages) {
				t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", tt.wantMessages, validationErr.Messages())
			}
		})
	}
}

func TestRead_RejectsMixedSeparatorPathsThatNormalizeToCurrentDirectoryDeterministically(t *testing.T) {
	tests := []struct {
		name         string
		dslPath      string
		modelPath    string
		tablePath    string
		wantMessages []string
	}{
		{
			name:         "mixed_separator_dsl_path",
			dslPath:      `dir\..\`,
			modelPath:    "models/file.FCStd",
			tablePath:    "tables/fasteners.json",
			wantMessages: []string{`dsl path "dir\\..\\" must not resolve to current directory`},
		},
		{
			name:         "mixed_separator_model_path",
			dslPath:      "project.dsl",
			modelPath:    `dir\..\`,
			tablePath:    "tables/fasteners.json",
			wantMessages: []string{`resources.models["model"] path "dir\\..\\" must not resolve to current directory`},
		},
		{
			name:         "mixed_separator_table_path",
			dslPath:      "project.dsl",
			modelPath:    "models/file.FCStd",
			tablePath:    `dir\..\`,
			wantMessages: []string{`tables["fasteners"] path "dir\\..\\" must not resolve to current directory`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&Project{
				Version:   "1.0",
				ProjectID: "example-project",
				DSL:       tt.dslPath,
				Resources: Resources{
					Models: map[string]string{"model": tt.modelPath},
				},
				Tables: map[string]string{"fasteners": tt.tablePath},
			})
			if err == nil {
				t.Fatal("expected Validate to fail")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T", err)
			}
			if !reflect.DeepEqual(validationErr.Messages(), tt.wantMessages) {
				t.Fatalf("unexpected validation messages\nwant: %#v\ngot:  %#v", tt.wantMessages, validationErr.Messages())
			}
		})
	}
}

func TestRead_PreservesCaseSensitivityForResourceIDs(t *testing.T) {
	project, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {
    "models": {
      "Model": "models/upper.FCStd",
      "model": "models/lower.FCStd"
    }
  }
}`))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if len(project.Resources.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(project.Resources.Models))
	}
	if project.Resources.Models["Model"] != "models/upper.FCStd" {
		t.Fatalf("unexpected value for Model: %q", project.Resources.Models["Model"])
	}
	if project.Resources.Models["model"] != "models/lower.FCStd" {
		t.Fatalf("unexpected value for model: %q", project.Resources.Models["model"])
	}
}

func TestRead_NoSilentTrimmingOfIDsOrPaths(t *testing.T) {
	tests := []struct {
		name         string
		json         string
		wantContains string
	}{
		{
			name: "project_id_surrounding_whitespace",
			json: `{
  "version": "1.0",
  "projectId": " example-project ",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`,
			wantContains: "projectId must not have leading or trailing whitespace",
		},
		{
			name: "model_id_surrounding_whitespace",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {" model ": "models/a.FCStd"}}
}`,
			wantContains: `resources.models logical ID " model " must not have leading or trailing whitespace`,
		},
		{
			name: "path_surrounding_whitespace",
			json: `{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": " models/a.FCStd "}}
}`,
			wantContains: `resources.models["model"] path " models/a.FCStd " must not have leading or trailing whitespace`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read([]byte(tt.json))
			if err == nil {
				t.Fatal("expected Read to fail")
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("expected error containing %q, got %v", tt.wantContains, err)
			}
		})
	}
}

func TestRead_TablesOmittedAndExplicitEmptyNormalizeEquivalently(t *testing.T) {
	omitted, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}}
}`))
	if err != nil {
		t.Fatalf("Read(omitted) returned error: %v", err)
	}

	explicitEmpty, err := Read([]byte(`{
  "version": "1.0",
  "projectId": "example-project",
  "dsl": "project.dsl",
  "resources": {"models": {"model": "models/a.FCStd"}},
  "tables": {}
}`))
	if err != nil {
		t.Fatalf("Read(explicit empty) returned error: %v", err)
	}

	if omitted.Tables == nil {
		t.Fatal("expected omitted tables to normalize to empty map, got nil")
	}
	if explicitEmpty.Tables == nil {
		t.Fatal("expected explicit empty tables to normalize to empty map, got nil")
	}
	if len(omitted.Tables) != 0 {
		t.Fatalf("expected omitted tables to normalize empty, got %d entries", len(omitted.Tables))
	}
	if len(explicitEmpty.Tables) != 0 {
		t.Fatalf("expected explicit empty tables to normalize empty, got %d entries", len(explicitEmpty.Tables))
	}
	if !reflect.DeepEqual(omitted, explicitEmpty) {
		t.Fatalf("expected equivalent normalized models\nomitted: %#v\nexplicit: %#v", omitted, explicitEmpty)
	}
}

func readValidationMessages(t *testing.T, data []byte) []string {
	t.Helper()

	_, err := Read(data)
	if err == nil {
		t.Fatal("expected validation error")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	return validationErr.Messages()
}

func writeProjectMapFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write project map fixture: %v", err)
	}
	return path
}
