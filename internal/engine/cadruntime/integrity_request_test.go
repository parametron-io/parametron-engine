package cadruntime

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/engine/adapter/freecad"
	"parametron/internal/engine/integrity"
	"parametron/internal/engine/resourcebinding"
)

const (
	requestDigestAlpha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	requestDigestZeta  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func requestBaseline(logicalID, coordinate, digest string) freecad.FreeCADRuntimeMaterializedBaseline {
	return freecad.FreeCADRuntimeMaterializedBaseline{
		Binding: resourcebinding.Binding{
			ProjectID: "p", Kind: resourcebinding.ResourceKindModel, LogicalID: logicalID,
		},
		Coordinate: coordinate,
		Digest:     integrity.Digest(digest),
	}
}

func TestComposeFreeCADIntegrityRequestComposition(t *testing.T) {
	baselines := []freecad.FreeCADRuntimeMaterializedBaseline{
		requestBaseline("zeta", "resources/zeta/Zeta.FCStd", requestDigestZeta),
		requestBaseline("unused", "resources/unused/Unused.FCStd", requestDigestZeta),
		requestBaseline("alpha", "source/Alpha.FCStd", requestDigestAlpha),
	}

	request, data, err := ComposeFreeCADIntegrityRequest(baselines, "source/Alpha.FCStd")
	if err != nil {
		t.Fatalf("ComposeFreeCADIntegrityRequest returned error: %v", err)
	}
	if request.SchemaVersion != "1.0" {
		t.Fatalf("schema version = %q, want 1.0", request.SchemaVersion)
	}
	if got := []string{request.Resources[0].LogicalID, request.Resources[1].LogicalID, request.Resources[2].LogicalID}; !reflect.DeepEqual(got, []string{"alpha", "unused", "zeta"}) {
		t.Fatalf("resource order = %v, want logical ID order", got)
	}
	persisted := 0
	for _, resource := range request.Resources {
		if resource.Kind != string(resourcebinding.ResourceKindModel) {
			t.Fatalf("resource %q kind = %q, want model", resource.LogicalID, resource.Kind)
		}
		if resource.ExpectsPersistedResult {
			persisted++
			if resource.LogicalID != "alpha" {
				t.Fatalf("resource %q unexpectedly expects a persisted result", resource.LogicalID)
			}
		}
		if strings.HasPrefix(resource.MaterializedCoordinate, "/") || strings.Contains(resource.MaterializedCoordinate, "\\") {
			t.Fatalf("resource %q coordinate leaked host syntax: %q", resource.LogicalID, resource.MaterializedCoordinate)
		}
	}
	if persisted != 1 {
		t.Fatalf("expectsPersistedResult count = %d, want exactly one", persisted)
	}
	// The zero-reference resource participates in the request.
	if request.Resources[1].LogicalID != "unused" {
		t.Fatalf("zero-reference resource dropped from the request: %#v", request.Resources)
	}

	parsed, err := ParseFreeCADIntegrityRequest(data)
	if err != nil {
		t.Fatalf("ParseFreeCADIntegrityRequest(composed) returned error: %v\n%s", err, data)
	}
	if parsed == nil || !reflect.DeepEqual(*parsed, request) {
		t.Fatalf("composed request is not round-trippable:\nwant: %#v\ngot:  %#v", request, parsed)
	}
}

func TestComposeFreeCADIntegrityRequestIsByteDeterministic(t *testing.T) {
	baselines := []freecad.FreeCADRuntimeMaterializedBaseline{
		requestBaseline("zeta", "resources/zeta/Zeta.FCStd", requestDigestZeta),
		requestBaseline("alpha", "source/Alpha.FCStd", requestDigestAlpha),
	}
	reversed := []freecad.FreeCADRuntimeMaterializedBaseline{baselines[1], baselines[0]}

	_, first, err := ComposeFreeCADIntegrityRequest(baselines, "source/Alpha.FCStd")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := ComposeFreeCADIntegrityRequest(reversed, "source/Alpha.FCStd")
	if err != nil {
		t.Fatal(err)
	}
	_, third, err := ComposeFreeCADIntegrityRequest(baselines, "source/Alpha.FCStd")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || !bytes.Equal(first, third) {
		t.Fatalf("equivalent inputs produced different bytes:\nfirst:  %s\nsecond: %s\nthird:  %s", first, second, third)
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Fatal("composed request does not end with a newline")
	}
	if bytes.Contains(first, []byte(os.TempDir())) {
		t.Fatal("composed request contains an absolute host path")
	}
}

func TestComposeFreeCADIntegrityRequestRejectsInvalidBaselines(t *testing.T) {
	tests := []struct {
		name      string
		baselines []freecad.FreeCADRuntimeMaterializedBaseline
	}{
		{name: "missing logical ID", baselines: []freecad.FreeCADRuntimeMaterializedBaseline{requestBaseline("", "source/A.FCStd", requestDigestAlpha)}},
		{name: "duplicate logical ID", baselines: []freecad.FreeCADRuntimeMaterializedBaseline{
			requestBaseline("alpha", "source/A.FCStd", requestDigestAlpha),
			requestBaseline("alpha", "source/B.FCStd", requestDigestZeta),
		}},
		{name: "primary coordinate not requested", baselines: []freecad.FreeCADRuntimeMaterializedBaseline{
			requestBaseline("alpha", "source/A.FCStd", requestDigestAlpha),
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			primary := "source/A.FCStd"
			if tc.name == "primary coordinate not requested" {
				primary = "source/Other.FCStd"
			}
			if _, _, err := ComposeFreeCADIntegrityRequest(tc.baselines, primary); err == nil {
				t.Fatal("ComposeFreeCADIntegrityRequest accepted invalid input")
			}
		})
	}
}

const (
	validIntegrityRequestResources = `{
    "logicalId": "alpha",
    "kind": "model",
    "materializedCoordinate": "source/Alpha.FCStd",
    "baselineSha256": "` + requestDigestAlpha + `",
    "expectsPersistedResult": true
  },
  {
    "logicalId": "zeta",
    "kind": "model",
    "materializedCoordinate": "resources/zeta/Zeta.FCStd",
    "baselineSha256": "` + requestDigestZeta + `",
    "expectsPersistedResult": false
  }`
)

func validIntegrityRequestJSON(resources string) string {
	return `{"schemaVersion": "1.0", "resources": [` + resources + `]}`
}

func TestParseFreeCADIntegrityRequestStrictDecoding(t *testing.T) {
	valid := []byte(validIntegrityRequestJSON(validIntegrityRequestResources))
	if _, err := ParseFreeCADIntegrityRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	tests := []struct {
		name string
		data []byte
		code string
	}{
		{
			name: "unsupported schema version",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), `"schemaVersion": "1.0"`, `"schemaVersion": "2.0"`, 1)),
			code: integrity.CodeEvidenceSchemaUnsupported,
		},
		{
			name: "unknown field",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), `"kind": "model"`, `"kind": "model", "unexpected": true`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "trailing content",
			data: append(append([]byte(nil), valid...), []byte("\n{}")...),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "duplicate logical ID",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), `"zeta"`, `"alpha"`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "unordered logical IDs",
			data: []byte(`{"schemaVersion": "1.0", "resources": [
  {"logicalId": "zeta", "kind": "model", "materializedCoordinate": "resources/zeta/Zeta.FCStd", "baselineSha256": "` + requestDigestZeta + `", "expectsPersistedResult": false},
  {"logicalId": "alpha", "kind": "model", "materializedCoordinate": "source/Alpha.FCStd", "baselineSha256": "` + requestDigestAlpha + `", "expectsPersistedResult": true}
]}`),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "empty logical ID",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), `"logicalId": "alpha"`, `"logicalId": ""`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "whitespace logical ID",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), `"logicalId": "alpha"`, `"logicalId": " alpha"`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "unsupported kind",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), `"kind": "model"`, `"kind": "dsl"`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "non-canonical materialized coordinate",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), `"materializedCoordinate": "source/Alpha.FCStd"`, `"materializedCoordinate": "/source/Alpha.FCStd"`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "non-canonical baseline digest",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), requestDigestAlpha, strings.ToUpper(requestDigestAlpha), 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "no resources",
			data: []byte(`{"schemaVersion": "1.0", "resources": []}`),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "no persisted result expected",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), `"expectsPersistedResult": true`, `"expectsPersistedResult": false`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
		{
			name: "two persisted results expected",
			data: []byte(strings.Replace(validIntegrityRequestJSON(validIntegrityRequestResources), `"expectsPersistedResult": false`, `"expectsPersistedResult": true`, 1)),
			code: integrity.CodeEvidenceInvalid,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFreeCADIntegrityRequest(tc.data)
			failure := integrity.AsFailure(err)
			if failure == nil || failure.Code != tc.code {
				t.Fatalf("error = %v, want integrity code %q", err, tc.code)
			}
		})
	}
}

func TestFreeCADIntegrityRequestPathHandling(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, FreeCADIntegrityRequestDirectoryName, "attempt-000001", FreeCADIntegrityRequestFilename)
	first, err := FreeCADIntegrityRequestPath(root, "attempt-000001")
	if err != nil {
		t.Fatal(err)
	}
	second, err := FreeCADIntegrityRequestPath(root, "attempt-000001")
	if err != nil {
		t.Fatal(err)
	}
	if first != want || second != want {
		t.Fatalf("request path = %q / %q, want deterministic %q", first, second, want)
	}

	for name, attemptID := range map[string]string{
		"empty":             "",
		"whitespace":        " ",
		"dot":               ".",
		"dot dot":           "..",
		"slash":             "attempt/one",
		"backslash":         `attempt\one`,
		"absolute":          "/attempt",
		"null byte":         "attempt\x00",
		"surrounding space": " attempt ",
	} {
		t.Run("attempt "+name, func(t *testing.T) {
			if _, err := FreeCADIntegrityRequestPath(root, attemptID); err == nil {
				t.Fatalf("attempt ID %q was accepted", attemptID)
			}
		})
	}
	for name, invalidRoot := range map[string]string{
		"empty":       "",
		"relative":    "relative/root",
		"unclean":     root + "/.",
		"rooted file": root + "/../root",
	} {
		t.Run("root "+name, func(t *testing.T) {
			if _, err := FreeCADIntegrityRequestPath(invalidRoot, "attempt-000001"); err == nil {
				t.Fatalf("root %q was accepted", invalidRoot)
			}
		})
	}
}

func TestWriteFreeCADIntegrityRequestPublishesExactBytes(t *testing.T) {
	root := t.TempDir()
	data := []byte(validIntegrityRequestJSON(validIntegrityRequestResources))
	path, err := WriteFreeCADIntegrityRequest(root, "attempt-000002", data)
	if err != nil {
		t.Fatalf("WriteFreeCADIntegrityRequest returned error: %v", err)
	}
	want, _ := FreeCADIntegrityRequestPath(root, "attempt-000002")
	if path != want {
		t.Fatalf("published path = %q, want %q", path, want)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, data) {
		t.Fatalf("published bytes = %q, want the exact request bytes", content)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			t.Fatalf("temporary publication file left behind: %q", entry.Name())
		}
	}
}
