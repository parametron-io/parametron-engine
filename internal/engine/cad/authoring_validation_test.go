package cad

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"parametron/internal/authoring/dsl"
)

func TestValidateAuthoringReferences_ParameterResolution(t *testing.T) {
	contract := loadFixture(t, "authoring", "parameter-by-name")
	ast := parseAuthoringFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param length: number = 35
}
`)

	if err := ValidateAuthoringReferences(ast, contract); err != nil {
		t.Fatalf("ValidateAuthoringReferences returned error: %v", err)
	}
}

func TestValidateAuthoringReferences_UnknownParameterFailsDeterministically(t *testing.T) {
	contract := loadFixture(t, "authoring", "parameter-by-name")
	ast := parseAuthoringFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param width: number = 35
}
`)

	err := ValidateAuthoringReferences(ast, contract)
	if err == nil {
		t.Fatal("expected ValidateAuthoringReferences to fail")
	}
	if !errors.Is(err, ErrAuthoring) {
		t.Fatalf("expected ErrAuthoring, got %v", err)
	}
	var authoringErr *AuthoringValidationError
	if !errors.As(err, &authoringErr) {
		t.Fatalf("expected AuthoringValidationError, got %T", err)
	}
	wantDiagnostics := []AuthoringDiagnostic{{
		Category: AuthoringDiagnosticCategoryInvalidDSLReference,
		Code:     AuthoringDiagnosticCodeCapturedParameterReferenceNotFound,
		Message:  `product "Box" parameter "width" does not resolve to a captured parameter by exact name`,
	}}
	if !reflect.DeepEqual(authoringErr.Diagnostics(), wantDiagnostics) {
		t.Fatalf("unexpected diagnostics\nwant: %#v\ngot:  %#v", wantDiagnostics, authoringErr.Diagnostics())
	}
	if !strings.Contains(err.Error(), wantDiagnostics[0].Message) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAuthoringReferences_DuplicateParameterNamesFailDeterministically(t *testing.T) {
	contract := loadFixture(t, "authoring", "duplicate-parameter-name")
	ast := parseAuthoringFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param length: number = 35
}
`)

	err := ValidateAuthoringReferences(ast, contract)
	if err == nil {
		t.Fatal("expected ValidateAuthoringReferences to fail")
	}
	var authoringErr *AuthoringValidationError
	if !errors.As(err, &authoringErr) {
		t.Fatalf("expected AuthoringValidationError, got %T", err)
	}
	wantDiagnostics := []AuthoringDiagnostic{{
		Category: AuthoringDiagnosticCategoryAmbiguousDSLReference,
		Code:     AuthoringDiagnosticCodeCapturedParameterReferenceAmbiguous,
		Message:  `product "Box" parameter "length" is ambiguous across captured parameters ["par.root.length.a" "par.root.length.b"]`,
	}}
	if !reflect.DeepEqual(authoringErr.Diagnostics(), wantDiagnostics) {
		t.Fatalf("unexpected diagnostics\nwant: %#v\ngot:  %#v", wantDiagnostics, authoringErr.Diagnostics())
	}
	if !strings.Contains(err.Error(), wantDiagnostics[0].Message) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAuthoringReferences_MatchingIsCaseSensitive(t *testing.T) {
	contract := loadFixture(t, "authoring", "parameter-by-name")
	ast := parseAuthoringFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param Length: number = 35
}
`)

	err := ValidateAuthoringReferences(ast, contract)
	if err == nil {
		t.Fatal("expected ValidateAuthoringReferences to fail")
	}
	if !strings.Contains(err.Error(), `product "Box" parameter "Length" does not resolve to a captured parameter by exact name`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAuthoringReferences_DisplayNameIsNotIdentitySubstitute(t *testing.T) {
	contract := loadFixture(t, "authoring", "display-name-only")
	ast := parseAuthoringFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param length: number = 35
}
`)

	err := ValidateAuthoringReferences(ast, contract)
	if err == nil {
		t.Fatal("expected ValidateAuthoringReferences to fail")
	}
	if !strings.Contains(err.Error(), `product "Box" parameter "length" does not resolve to a captured parameter by exact name`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAuthoringReferences_SourceModelMustMatchCaptureLogicalID(t *testing.T) {
	contract := loadFixture(t, "authoring", "parameter-by-name")
	ast := parseAuthoringFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "other_model"
    outputs = ["step"]

    param length: number = 35
}
`)

	err := ValidateAuthoringReferences(ast, contract)
	if err == nil {
		t.Fatal("expected ValidateAuthoringReferences to fail")
	}
	var authoringErr *AuthoringValidationError
	if !errors.As(err, &authoringErr) {
		t.Fatalf("expected AuthoringValidationError, got %T", err)
	}
	wantDiagnostics := []AuthoringDiagnostic{{
		Category: AuthoringDiagnosticCategorySourceModelReferenceMismatch,
		Code:     AuthoringDiagnosticCodeSourceModelLogicalIDMismatch,
		Message:  `product "Box" source_model "other_model" does not match capture sourceDocument.logicalId "box_model"`,
	}}
	if !reflect.DeepEqual(authoringErr.Diagnostics(), wantDiagnostics) {
		t.Fatalf("unexpected diagnostics\nwant: %#v\ngot:  %#v", wantDiagnostics, authoringErr.Diagnostics())
	}
	if !strings.Contains(err.Error(), wantDiagnostics[0].Message) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAuthoringReferences_DiagnosticsAreStableAcrossRuns(t *testing.T) {
	contract := loadFixture(t, "authoring", "duplicate-parameter-name")
	ast := parseAuthoringFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "wrong_model"
    outputs = ["step"]

    param length: number = 35
    param Width: number = 12
}
`)

	first := ValidateAuthoringReferences(ast, contract)
	second := ValidateAuthoringReferences(ast, contract)
	if first == nil || second == nil {
		t.Fatal("expected repeated validation failures")
	}
	if first.Error() != second.Error() {
		t.Fatalf("expected deterministic diagnostics\nfirst:  %q\nsecond: %q", first.Error(), second.Error())
	}

	var firstAuthoringErr *AuthoringValidationError
	if !errors.As(first, &firstAuthoringErr) {
		t.Fatalf("expected first AuthoringValidationError, got %T", first)
	}
	var secondAuthoringErr *AuthoringValidationError
	if !errors.As(second, &secondAuthoringErr) {
		t.Fatalf("expected second AuthoringValidationError, got %T", second)
	}
	if !reflect.DeepEqual(firstAuthoringErr.Diagnostics(), secondAuthoringErr.Diagnostics()) {
		t.Fatalf("expected identical diagnostic payloads across runs\nfirst:  %#v\nsecond: %#v", firstAuthoringErr.Diagnostics(), secondAuthoringErr.Diagnostics())
	}

	want := []AuthoringDiagnostic{
		{
			Category: AuthoringDiagnosticCategorySourceModelReferenceMismatch,
			Code:     AuthoringDiagnosticCodeSourceModelLogicalIDMismatch,
			Message:  `product "Box" source_model "wrong_model" does not match capture sourceDocument.logicalId "box_model"`,
		},
		{
			Category: AuthoringDiagnosticCategoryAmbiguousDSLReference,
			Code:     AuthoringDiagnosticCodeCapturedParameterReferenceAmbiguous,
			Message:  `product "Box" parameter "length" is ambiguous across captured parameters ["par.root.length.a" "par.root.length.b"]`,
		},
		{
			Category: AuthoringDiagnosticCategoryInvalidDSLReference,
			Code:     AuthoringDiagnosticCodeCapturedParameterReferenceNotFound,
			Message:  `product "Box" parameter "Width" does not resolve to a captured parameter by exact name`,
		},
	}
	if !reflect.DeepEqual(firstAuthoringErr.Diagnostics(), want) {
		t.Fatalf("unexpected diagnostic ordering\nwant: %#v\ngot:  %#v", want, firstAuthoringErr.Diagnostics())
	}

	gotCategoryCodes := make([]string, 0, len(firstAuthoringErr.Diagnostics()))
	for _, diagnostic := range firstAuthoringErr.Diagnostics() {
		gotCategoryCodes = append(gotCategoryCodes, diagnostic.Category+":"+diagnostic.Code)
	}
	wantCategoryCodes := []string{
		AuthoringDiagnosticCategorySourceModelReferenceMismatch + ":" + AuthoringDiagnosticCodeSourceModelLogicalIDMismatch,
		AuthoringDiagnosticCategoryAmbiguousDSLReference + ":" + AuthoringDiagnosticCodeCapturedParameterReferenceAmbiguous,
		AuthoringDiagnosticCategoryInvalidDSLReference + ":" + AuthoringDiagnosticCodeCapturedParameterReferenceNotFound,
	}
	if !reflect.DeepEqual(gotCategoryCodes, wantCategoryCodes) {
		t.Fatalf("unexpected diagnostic category/code ordering\nwant: %#v\ngot:  %#v", wantCategoryCodes, gotCategoryCodes)
	}
}

func TestValidateAuthoringReferences_NoCaptureIsNoOp(t *testing.T) {
	ast := parseAuthoringFixtureDSL(t, `
product Box {
    adapter = "freecad"
    source_model = "box_model"
    outputs = ["step"]

    param length: number = 35
}
`)

	if err := ValidateAuthoringReferences(ast, nil); err != nil {
		t.Fatalf("expected nil capture to be a no-op, got %v", err)
	}
}

func parseAuthoringFixtureDSL(t *testing.T, content string) *dsl.AST {
	t.Helper()

	path := writeAuthoringDSLFixture(t, content)
	ast, err := dsl.Parse(path)
	if err != nil {
		t.Fatalf("dsl.Parse returned error: %v", err)
	}
	if err := dsl.Validate(ast); err != nil {
		t.Fatalf("dsl.Validate returned error: %v", err)
	}
	return ast
}

func writeAuthoringDSLFixture(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fixture.dsl")
	if err := os.WriteFile(path, []byte(withDSLVersionHeader(content)), 0o644); err != nil {
		t.Fatalf("failed to write DSL fixture: %v", err)
	}
	return path
}

func withDSLVersionHeader(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "dsl v1.0") || strings.HasPrefix(trimmed, "dsl 1.0") {
		return content
	}
	return "dsl v1.0\n" + content
}
