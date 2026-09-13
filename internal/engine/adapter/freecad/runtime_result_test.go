package freecad

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const alignedSuccessJSON = `{"artifacts":[{"format":"pdf","id":"drawing","path":"exports/drawing.pdf"},{"format":"step","id":"part","path":"/tmp/fake/part.step"},{"format":"csv","id":"report","path":"exports/report.csv"}],"schemaVersion":"1.0","status":"succeeded"}`
const alignedFailureJSON = `{"failure":{"boundary":"execution_entrypoint","category":"execution","code":"runtime_failure","message":"parameter assignment failed","stage":"parameter_assignment"},"schemaVersion":"1.0","status":"failed"}`

func TestParseFreeCADRuntimeResult_AcceptsCurrentAlignedSuccessPayload(t *testing.T) {
	result := mustParseFreeCADRuntimeResult(t, alignedSuccessJSON)
	want := &FreeCADRuntimeResult{
		SchemaVersion: "1.0",
		Status:        FreeCADRuntimeResultStatusSucceeded,
		Artifacts: []FreeCADRuntimeResultArtifact{
			{ID: "drawing", Format: "pdf", Path: "exports/drawing.pdf"},
			{ID: "part", Format: "step", Path: "/tmp/fake/part.step"},
			{ID: "report", Format: "csv", Path: "exports/report.csv"},
		},
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("result mismatch\n got: %#v\nwant: %#v", result, want)
	}
	if result.Artifacts == nil || result.Failure != nil {
		t.Fatalf("normalized success invariants not preserved: %#v", result)
	}
}

func TestParseFreeCADRuntimeResult_AcceptsSucceededWithEmptyArtifacts(t *testing.T) {
	result := mustParseFreeCADRuntimeResult(t, `{"artifacts":[],"schemaVersion":"1.0","status":"succeeded"}`)
	if result.Artifacts == nil || len(result.Artifacts) != 0 || result.Failure != nil {
		t.Fatalf("unexpected normalized result: %#v", result)
	}
	if err := ValidateFreeCADRuntimeResult(result); err != nil {
		t.Fatalf("direct validation failed: %v", err)
	}
}

func TestParseFreeCADRuntimeResult_AcceptsCurrentAlignedFailurePayload(t *testing.T) {
	result := mustParseFreeCADRuntimeResult(t, alignedFailureJSON)
	if result.SchemaVersion != "1.0" || result.Status != FreeCADRuntimeResultStatusFailed {
		t.Fatalf("unexpected envelope: %#v", result)
	}
	if result.Artifacts == nil || len(result.Artifacts) != 0 || result.Failure == nil {
		t.Fatalf("unexpected normalized failure: %#v", result)
	}
	want := FreeCADRuntimeResultFailure{
		Boundary: "execution_entrypoint", Category: "execution", Code: "runtime_failure",
		Message: "parameter assignment failed", Stage: stringPointer("parameter_assignment"),
	}
	if !reflect.DeepEqual(*result.Failure, want) {
		t.Fatalf("failure mismatch: got %#v want %#v", *result.Failure, want)
	}
	if err := ValidateFreeCADRuntimeResult(result); err != nil {
		t.Fatalf("direct validation failed: %v", err)
	}
}

func TestParseFreeCADRuntimeResult_PreservesNullFailureStage(t *testing.T) {
	result := mustParseFreeCADRuntimeResult(t, failedJSON(`"stage":null`))
	if result.Failure.Stage != nil {
		t.Fatalf("stage = %q, want nil", *result.Failure.Stage)
	}
}

func TestParseFreeCADRuntimeResult_PreservesArtifactDeclarationOrder(t *testing.T) {
	result := mustParseFreeCADRuntimeResult(t, `{"artifacts":[{"id":"z-drawing","format":"pdf","path":"z.pdf"},{"id":"a-part","format":"step","path":"a.step"},{"id":"m-report","format":"csv","path":"m.csv"}],"schemaVersion":"1.0","status":"succeeded"}`)
	want := []string{"z-drawing", "a-part", "m-report"}
	for i, id := range want {
		if result.Artifacts[i].ID != id {
			t.Fatalf("artifacts[%d].ID = %q, want %q", i, result.Artifacts[i].ID, id)
		}
	}
}

func TestParseFreeCADRuntimeResult_RejectsUnknownArtifactFields(t *testing.T) {
	for _, tc := range []struct{ name, field string }{{"unknown", `"size":123`}, {"legacy_type", `"type":"step"`}, {"legacy_filename", `"filename":"part.step"`}} {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"artifacts":[{"id":"part","format":"step","path":"part.step",` + tc.field + `}],"schemaVersion":"1.0","status":"succeeded"}`
			assertValidationErrorContains(t, parseError(payload), strings.Split(tc.field, `"`)[1])
		})
	}
}

func TestParseFreeCADRuntimeResult_RejectsMissingArtifactFields(t *testing.T) {
	for _, field := range []string{"id", "format", "path"} {
		t.Run(field, func(t *testing.T) {
			artifact := map[string]any{"id": "part", "format": "step", "path": "part.step"}
			delete(artifact, field)
			assertValidationErrorContains(t, parseError(successWithArtifact(t, artifact)), field+" is required")
		})
	}
}

func TestParseFreeCADRuntimeResult_RejectsInvalidArtifactFieldTypes(t *testing.T) {
	invalid := []struct {
		name  string
		value any
	}{{"null", nil}, {"number", 1}, {"boolean", true}, {"object", map[string]any{}}, {"array", []any{}}}
	for _, field := range []string{"id", "format", "path"} {
		for _, value := range invalid {
			t.Run(field+"_"+value.name, func(t *testing.T) {
				artifact := map[string]any{"id": "part", "format": "step", "path": "part.step"}
				artifact[field] = value.value
				assertValidationErrorContains(t, parseError(successWithArtifact(t, artifact)), field+" must be a string")
			})
		}
	}
}

func TestParseFreeCADRuntimeResult_RejectsEmptyArtifactFields(t *testing.T) {
	for _, field := range []string{"id", "format", "path"} {
		t.Run(field, func(t *testing.T) {
			artifact := map[string]any{"id": "part", "format": "step", "path": "part.step"}
			artifact[field] = ""
			assertValidationErrorContains(t, parseError(successWithArtifact(t, artifact)), field+" must be a non-empty string")
		})
	}
}

func TestParseFreeCADRuntimeResult_ValidatesCanonicalArtifactFormats(t *testing.T) {
	for _, format := range []string{"step", "csv", "pdf"} {
		t.Run("accept_"+format, func(t *testing.T) {
			mustParseFreeCADRuntimeResult(t, successWithArtifact(t, map[string]any{"id": "x", "format": format, "path": "x"}))
		})
	}
	for _, format := range []string{"STEP", "Csv", " pdf ", "json", ""} {
		t.Run("reject_"+format, func(t *testing.T) {
			assertValidationErrorContains(t, parseError(successWithArtifact(t, map[string]any{"id": "x", "format": format, "path": "x"})), "format")
		})
	}
}

func TestParseFreeCADRuntimeResult_PreservesDuplicateArtifactDeclarations(t *testing.T) {
	result := mustParseFreeCADRuntimeResult(t, `{"artifacts":[{"id":"part","format":"step","path":"same.step"},{"id":"part","format":"step","path":"same.step"}],"schemaVersion":"1.0","status":"succeeded"}`)
	if len(result.Artifacts) != 2 || result.Artifacts[0] != result.Artifacts[1] {
		t.Fatalf("duplicates were changed: %#v", result.Artifacts)
	}
}

func TestParseFreeCADRuntimeResult_RejectsUnknownFailureFields(t *testing.T) {
	for _, field := range []string{`"classification":"recompute_error"`, `"detail":"x"`} {
		err := parseError(`{"failure":{"boundary":"b","category":"c","code":"x","message":"m","stage":null,` + field + `},"schemaVersion":"1.0","status":"failed"}`)
		assertValidationErrorContains(t, err, strings.Split(field, `"`)[1])
	}
}

func TestParseFreeCADRuntimeResult_RejectsMissingFailureFields(t *testing.T) {
	for _, field := range []string{"boundary", "category", "code", "message", "stage"} {
		t.Run(field, func(t *testing.T) {
			failure := validFailureMap()
			delete(failure, field)
			assertValidationErrorContains(t, parseError(failurePayload(t, failure)), field+" is required")
		})
	}
}

func TestParseFreeCADRuntimeResult_RejectsInvalidFailureDetailFields(t *testing.T) {
	invalid := []struct {
		name  string
		value any
	}{{"null", nil}, {"number", 1}, {"boolean", true}, {"object", map[string]any{}}, {"array", []any{}}, {"empty", ""}}
	for _, field := range []string{"boundary", "category", "code", "message"} {
		for _, value := range invalid {
			t.Run(field+"_"+value.name, func(t *testing.T) {
				failure := validFailureMap()
				failure[field] = value.value
				assertValidationErrorContains(t, parseError(failurePayload(t, failure)), field)
			})
		}
	}
}

func TestParseFreeCADRuntimeResult_ValidatesNullableFailureStage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    any
		accepted bool
	}{
		{"null", nil, true}, {"string", "future_stage", true}, {"empty", "", false}, {"number", 1, false},
		{"boolean", true, false}, {"object", map[string]any{}, false}, {"array", []any{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := validFailureMap()
			failure["stage"] = tc.value
			result, err := ParseFreeCADRuntimeResult([]byte(failurePayload(t, failure)))
			if tc.accepted {
				if err != nil {
					t.Fatalf("parse failed: %v", err)
				}
				if tc.value == nil && result.Failure.Stage != nil {
					t.Fatal("null stage was not preserved")
				}
				if tc.value != nil && (result.Failure.Stage == nil || *result.Failure.Stage != tc.value) {
					t.Fatalf("stage not preserved: %#v", result.Failure.Stage)
				}
				return
			}
			assertValidationErrorContains(t, err, "stage")
		})
	}
	future := map[string]any{"boundary": "future_boundary", "category": "future_category", "code": "future_code", "message": "future message", "stage": "future_stage"}
	result := mustParseFreeCADRuntimeResult(t, failurePayload(t, future))
	if result.Failure.Boundary != "future_boundary" || result.Failure.Category != "future_category" || result.Failure.Code != "future_code" || *result.Failure.Stage != "future_stage" {
		t.Fatalf("open vocabulary values changed: %#v", result.Failure)
	}
}

func TestParseFreeCADRuntimeResult_EnforcesStatusDependentShape(t *testing.T) {
	validFailure := `{"boundary":"b","category":"c","code":"x","message":"m","stage":null}`
	cases := map[string]string{
		"succeeded_without_artifacts":   `{"schemaVersion":"1.0","status":"succeeded"}`,
		"succeeded_null_artifacts":      `{"artifacts":null,"schemaVersion":"1.0","status":"succeeded"}`,
		"succeeded_non_array_artifacts": `{"artifacts":{},"schemaVersion":"1.0","status":"succeeded"}`,
		"succeeded_failure_object":      `{"artifacts":[],"failure":` + validFailure + `,"schemaVersion":"1.0","status":"succeeded"}`,
		"succeeded_failure_null":        `{"artifacts":[],"failure":null,"schemaVersion":"1.0","status":"succeeded"}`,
		"failed_without_failure":        `{"schemaVersion":"1.0","status":"failed"}`,
		"failed_null_failure":           `{"failure":null,"schemaVersion":"1.0","status":"failed"}`,
		"failed_non_object_failure":     `{"failure":[],"schemaVersion":"1.0","status":"failed"}`,
		"failed_empty_artifacts":        `{"artifacts":[],"failure":` + validFailure + `,"schemaVersion":"1.0","status":"failed"}`,
		"failed_null_artifacts":         `{"artifacts":null,"failure":` + validFailure + `,"schemaVersion":"1.0","status":"failed"}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) { assertValidationErrorContains(t, parseError(payload), "") })
	}
}

func TestParseFreeCADRuntimeResult_RejectsInvalidStatus(t *testing.T) {
	cases := map[string]string{"missing": `{"artifacts":[],"schemaVersion":"1.0"}`, "null": `{"artifacts":[],"schemaVersion":"1.0","status":null}`, "number": `{"artifacts":[],"schemaVersion":"1.0","status":1}`}
	for _, value := range []string{"", "success", "failure", "unknown"} {
		cases["value_"+value] = `{"artifacts":[],"schemaVersion":"1.0","status":` + quoteJSON(t, value) + `}`
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) { assertValidationErrorContains(t, parseError(payload), "status") })
	}
}

func TestParseFreeCADRuntimeResult_RejectsInvalidSchemaVersion(t *testing.T) {
	cases := map[string]string{"missing": `{"artifacts":[],"status":"succeeded"}`, "null": `{"artifacts":[],"schemaVersion":null,"status":"succeeded"}`, "number": `{"artifacts":[],"schemaVersion":1,"status":"succeeded"}`}
	for _, value := range []string{"", "0.9", "1.1", "2.0"} {
		cases["value_"+value] = `{"artifacts":[],"schemaVersion":` + quoteJSON(t, value) + `,"status":"succeeded"}`
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) { assertValidationErrorContains(t, parseError(payload), "schemaVersion") })
	}
}

func TestParseFreeCADRuntimeResult_RejectsUnknownTopLevelFields(t *testing.T) {
	for _, field := range []string{"error", "outputs", "workingCopy"} {
		t.Run(field, func(t *testing.T) {
			assertValidationErrorContains(t, parseError(`{"artifacts":[],"schemaVersion":"1.0","status":"succeeded",`+quoteJSON(t, field)+`:true}`), field)
		})
	}
}

func TestParseFreeCADRuntimeResult_RejectsMalformedJSON(t *testing.T) {
	for _, payload := range []string{"", `{`, `{"artifacts":[`} {
		assertDecodeError(t, parseError(payload))
	}
}

func TestParseFreeCADRuntimeResult_RejectsTrailingJSON(t *testing.T) {
	for _, suffix := range []string{` {}`, ` true`} {
		assertDecodeError(t, parseError(`{"artifacts":[],"schemaVersion":"1.0","status":"succeeded"}`+suffix))
	}
}

func TestParseFreeCADRuntimeResult_RequiresObjectRoot(t *testing.T) {
	for _, payload := range []string{`[]`, `"result"`, `null`} {
		assertValidationErrorContains(t, parseError(payload), "root must be a JSON object")
	}
}

func TestValidateFreeCADRuntimeResult_RejectsNil(t *testing.T) {
	assertValidationErrorContains(t, ValidateFreeCADRuntimeResult(nil), "result must not be nil")
}

func TestValidateFreeCADRuntimeResult_EnforcesSucceededInvariants(t *testing.T) {
	valid := func() *FreeCADRuntimeResult {
		return &FreeCADRuntimeResult{SchemaVersion: "1.0", Status: FreeCADRuntimeResultStatusSucceeded, Artifacts: []FreeCADRuntimeResultArtifact{}}
	}
	cases := []struct {
		name     string
		mutate   func(*FreeCADRuntimeResult)
		accepted bool
	}{
		{"nil_artifacts", func(r *FreeCADRuntimeResult) { r.Artifacts = nil }, false},
		{"empty_artifacts", func(r *FreeCADRuntimeResult) {}, true},
		{"failure", func(r *FreeCADRuntimeResult) { r.Failure = &FreeCADRuntimeResultFailure{} }, false},
		{"invalid_artifact", func(r *FreeCADRuntimeResult) {
			r.Artifacts = []FreeCADRuntimeResultArtifact{{ID: "x", Format: "json", Path: "x"}}
		}, false},
		{"wrong_schema", func(r *FreeCADRuntimeResult) { r.SchemaVersion = "2.0" }, false},
		{"wrong_status", func(r *FreeCADRuntimeResult) { r.Status = "success" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := valid()
			tc.mutate(r)
			err := ValidateFreeCADRuntimeResult(r)
			if tc.accepted && err != nil {
				t.Fatalf("validation failed: %v", err)
			}
			if !tc.accepted {
				assertValidationErrorContains(t, err, "")
			}
		})
	}
}

func TestValidateFreeCADRuntimeResult_EnforcesFailedInvariants(t *testing.T) {
	valid := func() *FreeCADRuntimeResult {
		return &FreeCADRuntimeResult{SchemaVersion: "1.0", Status: FreeCADRuntimeResultStatusFailed, Artifacts: []FreeCADRuntimeResultArtifact{}, Failure: &FreeCADRuntimeResultFailure{Boundary: "b", Category: "c", Code: "x", Message: "m"}}
	}
	cases := []struct {
		name     string
		mutate   func(*FreeCADRuntimeResult)
		accepted bool
	}{
		{"nil_artifacts", func(r *FreeCADRuntimeResult) { r.Artifacts = nil }, false}, {"non_empty_artifacts", func(r *FreeCADRuntimeResult) {
			r.Artifacts = []FreeCADRuntimeResultArtifact{{ID: "x", Format: "step", Path: "x"}}
		}, false},
		{"nil_failure", func(r *FreeCADRuntimeResult) { r.Failure = nil }, false}, {"invalid_failure", func(r *FreeCADRuntimeResult) { r.Failure.Message = "" }, false},
		{"nil_stage", func(r *FreeCADRuntimeResult) {}, true}, {"non_empty_stage", func(r *FreeCADRuntimeResult) { r.Failure.Stage = stringPointer("future_stage") }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := valid()
			tc.mutate(r)
			err := ValidateFreeCADRuntimeResult(r)
			if tc.accepted && err != nil {
				t.Fatalf("validation failed: %v", err)
			}
			if !tc.accepted {
				assertValidationErrorContains(t, err, "")
			}
		})
	}
}

func TestValidateFreeCADRuntimeResult_IsIdempotentAndNonMutating(t *testing.T) {
	values := []*FreeCADRuntimeResult{
		{SchemaVersion: "1.0", Status: FreeCADRuntimeResultStatusSucceeded, Artifacts: []FreeCADRuntimeResultArtifact{{ID: "z", Format: "pdf", Path: "z"}, {ID: "a", Format: "step", Path: "a"}}},
		{SchemaVersion: "1.0", Status: FreeCADRuntimeResultStatusFailed, Artifacts: []FreeCADRuntimeResultArtifact{}, Failure: &FreeCADRuntimeResultFailure{Boundary: "b", Category: "c", Code: "x", Message: "m", Stage: stringPointer("future_stage")}},
	}
	for _, value := range values {
		clone := cloneResultForTest(value)
		for i := 0; i < 3; i++ {
			if err := ValidateFreeCADRuntimeResult(value); err != nil {
				t.Fatalf("validation %d failed: %v", i, err)
			}
		}
		if !reflect.DeepEqual(value, clone) {
			t.Fatalf("validation mutated value: got %#v want %#v", value, clone)
		}
	}
}

func TestParseFreeCADRuntimeResult_ReturnsIndependentValues(t *testing.T) {
	first := mustParseFreeCADRuntimeResult(t, alignedSuccessJSON)
	second := mustParseFreeCADRuntimeResult(t, alignedSuccessJSON)
	first.Artifacts[0].ID = "changed"
	if second.Artifacts[0].ID != "drawing" {
		t.Fatalf("artifact slices alias: %#v", second.Artifacts)
	}
	failed1 := mustParseFreeCADRuntimeResult(t, alignedFailureJSON)
	failed2 := mustParseFreeCADRuntimeResult(t, alignedFailureJSON)
	*failed1.Failure.Stage = "changed"
	if *failed2.Failure.Stage != "parameter_assignment" {
		t.Fatalf("stage pointers alias: %#v", failed2.Failure.Stage)
	}
}

func TestParseFreeCADRuntimeResult_DoesNotRetainInputBuffer(t *testing.T) {
	input := []byte(alignedSuccessJSON)
	result, err := ParseFreeCADRuntimeResult(input)
	if err != nil {
		t.Fatal(err)
	}
	for i := range input {
		input[i] = 'x'
	}
	if result.Artifacts[0].ID != "drawing" || result.SchemaVersion != "1.0" {
		t.Fatalf("input buffer retained: %#v", result)
	}
}

func TestLoadFreeCADRuntimeResult_LoadsAlignedResultFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(path, []byte(alignedSuccessJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFreeCADRuntimeResult(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed := mustParseFreeCADRuntimeResult(t, alignedSuccessJSON)
	if !reflect.DeepEqual(loaded, parsed) {
		t.Fatalf("loaded mismatch: %#v != %#v", loaded, parsed)
	}
}

func TestLoadFreeCADRuntimeResult_PreservesFilesystemErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := LoadFreeCADRuntimeResult(path)
	if !errors.Is(err, ErrFreeCADRuntimeResultIO) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected error chain: %v", err)
	}
	var fileErr *FreeCADRuntimeResultFileError
	if !errors.As(err, &fileErr) {
		t.Fatalf("errors.As file error failed: %v", err)
	}
	if fileErr.Path != path {
		t.Fatalf("path=%q want %q", fileErr.Path, path)
	}
}

func TestLoadFreeCADRuntimeResult_PreservesDecodeAndValidationErrors(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		sentinel      error
	}{{"decode", "{", ErrFreeCADRuntimeResultDecode}, {"validation", `{"artifacts":[],"schemaVersion":"1.0","status":"success"}`, ErrFreeCADRuntimeResultValidation}} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "result.json")
			if err := os.WriteFile(path, []byte(tc.payload), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadFreeCADRuntimeResult(path)
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error=%v", err)
			}
			if errors.Is(err, ErrFreeCADRuntimeResultIO) {
				t.Fatalf("incorrect I/O wrapping: %v", err)
			}
		})
	}
}

func TestFreeCADRuntimeResultErrors_AreInspectable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, ioErr := LoadFreeCADRuntimeResult(missing)
	var fileErr *FreeCADRuntimeResultFileError
	if !errors.As(ioErr, &fileErr) || !errors.Is(ioErr, ErrFreeCADRuntimeResultIO) || !errors.Is(ioErr, os.ErrNotExist) {
		t.Fatalf("I/O error not inspectable: %v", ioErr)
	}
	_, decodeErr := ParseFreeCADRuntimeResult([]byte("{"))
	var typedDecode *FreeCADRuntimeResultDecodeError
	if !errors.As(decodeErr, &typedDecode) || !errors.Is(decodeErr, ErrFreeCADRuntimeResultDecode) {
		t.Fatalf("decode error not inspectable: %v", decodeErr)
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(decodeErr, &syntaxErr) && !errors.Is(decodeErr, io.ErrUnexpectedEOF) {
		t.Fatalf("underlying JSON error not inspectable: %v", decodeErr)
	}
	_, validationErr := ParseFreeCADRuntimeResult([]byte(`{"artifacts":[],"schemaVersion":"1.0","status":"success"}`))
	var typedValidation *FreeCADRuntimeResultValidationError
	if !errors.As(validationErr, &typedValidation) || !errors.Is(validationErr, ErrFreeCADRuntimeResultValidation) {
		t.Fatalf("validation error not inspectable: %v", validationErr)
	}
	if errors.Is(validationErr, ErrFreeCADRuntimeResultIO) || errors.Is(validationErr, ErrFreeCADRuntimeResultDecode) {
		t.Fatalf("validation error masquerades as another category: %v", validationErr)
	}
}

func TestParseFreeCADRuntimeResult_AcceptsCurrentParametronFreeCADContract(t *testing.T) {
	// Mirrored from parametron_freecad/execution/result_writer.py and
	// parametron_freecad/runtime/{failure_output_contract,failure_result_writer}.py,
	// together with their focused producer tests.
	t.Run("succeeded", func(t *testing.T) { mustParseFreeCADRuntimeResult(t, alignedSuccessJSON) })
	t.Run("failed", func(t *testing.T) { mustParseFreeCADRuntimeResult(t, alignedFailureJSON) })
}

func TestAlignedFreeCADRuntimeResult_DoesNotAcceptLegacyEngineLocalShape(t *testing.T) {
	legacySuccess := `{"schemaVersion":"1.0","status":"success","artifacts":[{"type":"step","filename":"part.step","path":"part.step"}]}`
	legacyFailure := `{"schemaVersion":"1.0","status":"failure","error":{"classification":"recompute_error","message":"recompute failed"}}`
	for _, payload := range []string{legacySuccess, legacyFailure} {
		assertValidationErrorContains(t, parseError(payload), "")
	}
}

func mustParseFreeCADRuntimeResult(t *testing.T, payload string) *FreeCADRuntimeResult {
	t.Helper()
	result, err := ParseFreeCADRuntimeResult([]byte(payload))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return result
}
func parseError(payload string) error {
	_, err := ParseFreeCADRuntimeResult([]byte(payload))
	return err
}
func assertValidationErrorContains(t *testing.T, err error, fragment string) {
	t.Helper()
	if !errors.Is(err, ErrFreeCADRuntimeResultValidation) {
		t.Fatalf("error=%v, want validation sentinel", err)
	}
	var typed *FreeCADRuntimeResultValidationError
	if !errors.As(err, &typed) {
		t.Fatalf("error=%v, want validation type", err)
	}
	if fragment != "" && !strings.Contains(err.Error(), fragment) {
		t.Fatalf("error %q does not contain %q", err, fragment)
	}
}
func assertDecodeError(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrFreeCADRuntimeResultDecode) {
		t.Fatalf("error=%v, want decode sentinel", err)
	}
	var typed *FreeCADRuntimeResultDecodeError
	if !errors.As(err, &typed) {
		t.Fatalf("error=%v, want decode type", err)
	}
}
func stringPointer(value string) *string { return &value }
func failedJSON(stageField string) string {
	return `{"failure":{"boundary":"execution_entrypoint","category":"execution","code":"runtime_failure","message":"parameter assignment failed",` + stageField + `},"schemaVersion":"1.0","status":"failed"}`
}
func validFailureMap() map[string]any {
	return map[string]any{"boundary": "b", "category": "c", "code": "x", "message": "m", "stage": nil}
}
func failurePayload(t *testing.T, failure map[string]any) string {
	t.Helper()
	return marshalJSON(t, map[string]any{"failure": failure, "schemaVersion": "1.0", "status": "failed"})
}
func successWithArtifact(t *testing.T, entry map[string]any) string {
	t.Helper()
	return marshalJSON(t, map[string]any{"artifacts": []any{entry}, "schemaVersion": "1.0", "status": "succeeded"})
}
func marshalJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func quoteJSON(t *testing.T, value string) string { t.Helper(); return marshalJSON(t, value) }
func cloneResultForTest(value *FreeCADRuntimeResult) *FreeCADRuntimeResult {
	clone := *value
	if value.Artifacts != nil {
		clone.Artifacts = append([]FreeCADRuntimeResultArtifact{}, value.Artifacts...)
	}
	if value.Failure != nil {
		failure := *value.Failure
		if value.Failure.Stage != nil {
			stage := *value.Failure.Stage
			failure.Stage = &stage
		}
		clone.Failure = &failure
	}
	return &clone
}
