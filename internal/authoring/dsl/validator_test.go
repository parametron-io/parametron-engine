package dsl

import (
	"os"
	"strings"
	"testing"
)

func withDSLVersionHeader(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "dsl v1.0") || strings.HasPrefix(trimmed, "dsl 1.0") {
		return content
	}
	return "dsl v1.0\n" + content
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name          string
		dslContent    string
		expectError   bool
		errorContains string
	}{
		{
			name: "Duplicate Profile Name",
			dslContent: `
profile Build {
    output_dir = "out/a"
}
profile Build {
    output_dir = "out/b"
}`,
			expectError:   true,
			errorContains: "duplicate profile name",
		},
		{
			name: "Duplicate Profile Key",
			dslContent: `
profile Build {
    output_dir = "out/a"
    output_dir = "out/b"
}`,
			expectError:   true,
			errorContains: "duplicate profile setting key",
		},
		{
			name: "Selecting Nonexistent Profile",
			dslContent: `
profile Build {
    output_dir = "out"
}
use profile Prod`,
			expectError:   true,
			errorContains: "selected profile 'Prod' is not defined",
		},
		{
			name: "Multiple Profiles Require Explicit Selection",
			dslContent: `
profile Dev {
    output_dir = "out/dev"
}
profile Prod {
    output_dir = "out/prod"
}`,
			expectError:   true,
			errorContains: "multiple profiles defined; explicit 'use profile <Name>' selection is required",
		},
		{
			name: "Profile Setting Undefined Constant",
			dslContent: `
profile Prod {
    output_dir = UNDEFINED_DIR
}
use profile Prod

product Widget {
    param width: number = 50
}`,
			expectError:   true,
			errorContains: "undefined constant: UNDEFINED_DIR",
		},
		{
			name: "Profile Execution Declaration Rejected",
			dslContent: `
profile Dev {
    output_dir = "out"
    adapter = "freecad"
}
use profile Dev

product Box {
    param length: number = 35
}`,
			expectError:   true,
			errorContains: "execution declarations must be declared inside product blocks",
		},
		{
			name: "Product Adapter SourceModel String Constant",
			dslContent: `
const ADAPTER = "freecad"
const MODEL = "input/box.FCStd"

product Box {
    adapter = ADAPTER
    source_model = MODEL

    param length: number = 35
}`,
			expectError: false,
		},
		{
			name: "Product Adapter Rejects NonString",
			dslContent: `
product Box {
    adapter = 123

    param length: number = 35
}`,
			expectError:   true,
			errorContains: "adapter must be a string",
		},
		{
			name: "Product SourceModel Rejects NonString",
			dslContent: `
product Box {
    adapter = "freecad"
    source_model = true

    param length: number = 35
}`,
			expectError:   true,
			errorContains: "source_model must be a string",
		},
		{
			name: "Product SourceModel RequiresAdapterDeclaration",
			dslContent: `
product Box {
    source_model = "input/box.FCStd"

    param length: number = 35
}`,
			expectError:   true,
			errorContains: "source_model requires adapter to be declared",
		},
		{
			name: "Product Outputs Rejects NonArray",
			dslContent: `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = "step"

    param length: number = 35
}`,
			expectError:   true,
			errorContains: "outputs must be a list of strings",
		},
		{
			name: "Product Outputs RequireAdapterDeclaration",
			dslContent: `
product Box {
    outputs = ["step"]

    param length: number = 35
}`,
			expectError:   true,
			errorContains: "outputs requires adapter to be declared",
		},
		{
			name: "Product Outputs Rejects NonStringItem",
			dslContent: `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", 5]

    param length: number = 35
}`,
			expectError:   true,
			errorContains: "outputs[1]: outputs must be a string",
		},
		{
			name: "Product Outputs Accepts StringList",
			dslContent: `
const PRIMARY = "step"
const SECONDARY = "pdf"

product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = [PRIMARY, SECONDARY]

    param length: number = 35
}`,
			expectError: false,
		},
		{
			name: "Product Outputs Rejects Unsupported Format For Adapter",
			dslContent: `
product Box {
    adapter = "freecad"
    source_model = "input/box.FCStd"
    outputs = ["step", "stl"]

    param length: number = 35
}`,
			expectError:   true,
			errorContains: `outputs[1] has unsupported format "stl" for adapter "freecad"`,
		},
		{
			name: "AdapterNone Accepts EmptyOutputsAndNoSourceModel",
			dslContent: `
product PlanningOnly {
    adapter = "none"
    outputs = []

    param width: number = 50
}`,
			expectError: false,
		},
		{
			name: "AdapterNone AcceptsAbsentOutputsAndNoSourceModel",
			dslContent: `
product PlanningOnly {
    adapter = "none"

    param width: number = 50
}`,
			expectError: false,
		},
		{
			name: "AdapterNone Rejects NonEmptyOutputs",
			dslContent: `
product PlanningOnly {
    adapter = "none"
    outputs = ["step"]

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `outputs must be empty when adapter is "none"`,
		},
		{
			name: "NonNoneAdapterRequiresSourceModel",
			dslContent: `
product Box {
    adapter = "freecad"

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `source_model is required when adapter is "freecad"`,
		},
		{
			name: "UnknownAdapterFailsDeterministically",
			dslContent: `
product Box {
    adapter = "fusion360"
    source_model = "input/box.FCStd"

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `adapter "fusion360" is not supported`,
		},
		{
			name: "Valid DSL",
			dslContent: `
product Desk {
    param width: number = 1200
    param height: number = 800
}`,
			expectError: false,
		},
		{
			name: "Valid Numeric Constant",
			dslContent: `
const BASE = 1200
product Desk {
    param width: number = BASE
}`,
			expectError: false,
		},
		{
			name: "Valid String Constant",
			dslContent: `
const LABEL = "premium"
product Desk {
    param tier: string = LABEL
}`,
			expectError: false,
		},
		{
			name: "Valid Enum Constant",
			dslContent: `
const DEFAULT_FINISH = Oak
product Desk {
    param finish: enum { Oak, Pine } = DEFAULT_FINISH
}`,
			expectError: false,
		},
		{
			name: "Expression Constant",
			dslContent: `
const A = 5 + 3
product Desk {
    param width: number = A
}`,
			expectError: false,
		},
		{
			name: "Duplicate Constant",
			dslContent: `
const A = 5
const A = 6
product Desk {
    param width: number = 1200
}`,
			expectError:   true,
			errorContains: "constant already defined",
		},
		{
			name: "Parameter Name Collides With Constant",
			dslContent: `
const width = 1200
product Desk {
    param width: number = 800
}`,
			expectError:   true,
			errorContains: "cannot override constant",
		},
		{
			name: "Duplicate Product Name",
			dslContent: `
product Desk {
    param width: number = 1200
}
product Desk {
    param height: number = 800
}`,
			expectError:   true,
			errorContains: "duplicate product name",
		},
		{
			name: "Duplicate Parameter Name",
			dslContent: `
product Desk {
    param width: number = 1200
    param width: number = 800
}`,
			expectError:   true,
			errorContains: "duplicate parameter name",
		},
		{
			name: "Undefined Parameter Reference",
			dslContent: `
product Desk {
    param width: number = height
}`,
			expectError:   true,
			errorContains: "undefined binding reference",
		},
		{
			name: "Type Mismatch - String to Number",
			dslContent: `
product Desk {
    param width: number = "too wide"
}`,
			expectError:   true,
			errorContains: "expected number literal, but got type string",
		},
		{
			name: "Type Mismatch - Number to Boolean",
			dslContent: `
product Desk {
    param is_valid: boolean = 123
}`,
			expectError:   true,
			errorContains: "expected boolean literal, but got type float64",
		},
		{
			name: "Unary Not Valid",
			dslContent: `
product Desk {
    param enabled: boolean = true
    param disabled: boolean = !enabled
}`,
			expectError: false,
		},
		{
			name: "Double Unary Not Valid",
			dslContent: `
product Desk {
    param enabled: boolean = true
    param same: boolean = !!enabled
}`,
			expectError: false,
		},
		{
			name: "Unary Not With Boolean Comparison Valid",
			dslContent: `
product Desk {
    param result: boolean = !(1 == 1)
}`,
			expectError: false,
		},
		{
			name: "Unary Not Invalid Number Operand",
			dslContent: `
product Desk {
    param bad: boolean = !1
}`,
			expectError:   true,
			errorContains: "operand of unary '!': expected boolean literal, but got type float64",
		},
		{
			name: "Unary Not Invalid String Operand",
			dslContent: `
product Desk {
    param bad: boolean = !"x"
}`,
			expectError:   true,
			errorContains: "operand of unary '!': expected boolean literal, but got type string",
		},
		{
			name: "Unary Not Invalid Enum Operand",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = Oak
    param bad: boolean = !finish
}`,
			expectError:   true,
			errorContains: "operand of unary '!': binding reference 'finish' has type 'enum', but expected 'boolean'",
		},
		{
			name: "Valid Function Call",
			dslContent: `
product Desk {
    param width: number = max(100, 200)
}`,
			expectError: false,
		},
		{
			name: "Unknown Function",
			dslContent: `
product Desk {
    param width: number = foobar(100)
}`,
			expectError:   true,
			errorContains: "unknown function: 'foobar'",
		},
		{
			name: "Wrong Argument Count",
			dslContent: `
product Desk {
    param width: number = max(100)
}`,
			expectError:   true,
			errorContains: "function 'max' expects 2 arguments, but got 1",
		},
		{
			name: "Wrong Argument Type",
			dslContent: `
product Desk {
    param width: number = max(100, "200")
}`,
			expectError:   true,
			errorContains: "argument 2 of 'max': expected number literal, but got type string",
		},
		{
			name: "Floor Wrong Argument Count",
			dslContent: `
product Desk {
    param width: number = floor(1, 2)
}`,
			expectError:   true,
			errorContains: "function 'floor' expects 1 arguments, but got 2",
		},
		{
			name: "Floor Wrong Argument Type",
			dslContent: `
product Desk {
    param width: number = floor("200")
}`,
			expectError:   true,
			errorContains: "argument of 'floor': expected number literal, but got type string",
		},
		{
			name: "Ceil Wrong Argument Count",
			dslContent: `
product Desk {
    param width: number = ceil(1, 2)
}`,
			expectError:   true,
			errorContains: "function 'ceil' expects 1 arguments, but got 2",
		},
		{
			name: "Ceil Wrong Argument Type",
			dslContent: `
product Desk {
    param width: number = ceil("200")
}`,
			expectError:   true,
			errorContains: "argument of 'ceil': expected number literal, but got type string",
		},
		{
			name: "Clamp Wrong Argument Count",
			dslContent: `
product Desk {
    param width: number = clamp(10, 0)
}`,
			expectError:   true,
			errorContains: "function 'clamp' expects 3 arguments, but got 2",
		},
		{
			name: "Clamp Wrong Argument Type",
			dslContent: `
product Desk {
    param width: number = clamp(10, "0", 5)
}`,
			expectError:   true,
			errorContains: "argument 2 of 'clamp': expected number literal, but got type string",
		},
		{
			name: "Round Up Wrong Argument Count",
			dslContent: `
product Desk {
    param width: number = round_up(10)
}`,
			expectError:   true,
			errorContains: "function 'round_up' expects 2 arguments, but got 1",
		},
		{
			name: "Round Up Wrong Argument Type",
			dslContent: `
product Desk {
    param width: number = round_up(10, "5")
}`,
			expectError:   true,
			errorContains: "argument 2 of 'round_up': expected number literal, but got type string",
		},
		{
			name: "Round Down Wrong Argument Count",
			dslContent: `
product Desk {
    param width: number = round_down(10)
}`,
			expectError:   true,
			errorContains: "function 'round_down' expects 2 arguments, but got 1",
		},
		{
			name: "Round Down Wrong Argument Type",
			dslContent: `
product Desk {
    param width: number = round_down(10, "5")
}`,
			expectError:   true,
			errorContains: "argument 2 of 'round_down': expected number literal, but got type string",
		},
		{
			name: "Clamp Invalid Bounds",
			dslContent: `
product Desk {
    param width: number = clamp(10, 20, 5)
}`,
			expectError:   true,
			errorContains: "clamp requires lo <= hi",
		},
		{
			name: "Round Up Zero Step",
			dslContent: `
product Desk {
    param width: number = round_up(10, 0)
}`,
			expectError:   true,
			errorContains: "round_up requires step > 0",
		},
		{
			name: "Round Up Negative Step",
			dslContent: `
product Desk {
    param width: number = round_up(10, -2)
}`,
			expectError:   true,
			errorContains: "round_up requires step > 0",
		},
		{
			name: "Round Down Zero Step",
			dslContent: `
product Desk {
    param width: number = round_down(10, 0)
}`,
			expectError:   true,
			errorContains: "round_down requires step > 0",
		},
		{
			name: "Round Down Negative Step",
			dslContent: `
product Desk {
    param width: number = round_down(10, -2)
}`,
			expectError:   true,
			errorContains: "round_down requires step > 0",
		},
		{
			name: "Enum Duplicate Values",
			dslContent: `
product Desk {
    param finish: enum { Oak, Oak } = Oak
}`,
			expectError:   true,
			errorContains: "duplicate enum value",
		},
		{
			name: "Enum Valid Default Literal",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = Oak
}`,
			expectError: false,
		},
		{
			name: "Enum Valid Reference Same Type",
			dslContent: `
product Desk {
    param baseFinish: enum { Oak, Pine } = Pine
    param finish: enum { Oak, Pine } = baseFinish
}`,
			expectError: false,
		},
		{
			name: "Enum Valid Ternary",
			dslContent: `
product Desk {
    param use_oak: boolean = true
    param finish: enum { Oak, Pine } = use_oak ? Oak : Pine
}`,
			expectError: false,
		},
		{
			name: "Enum Invalid String Literal Default",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = "Oak"
}`,
			expectError:   true,
			errorContains: "expected enum literal, but got string literal",
		},
		{
			name: "Enum Invalid Number Literal Default",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = 5
}`,
			expectError:   true,
			errorContains: "expected enum literal, but got type float64",
		},
		{
			name: "Enum Invalid Literal",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = Maple
}`,
			expectError:   true,
			errorContains: "invalid enum value 'Maple', allowed values are [Oak Pine]",
		},
		{
			name: "Enum Type Mismatch Between Parameters",
			dslContent: `
product Desk {
    param finishA: enum { Oak, Pine } = Oak
    param finishB: enum { Red, Blue } = finishA
}`,
			expectError:   true,
			errorContains: "enum type mismatch between 'finishA' and 'enum'",
		},
		{
			name: "Enum Used In Arithmetic",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = Oak + Oak
}`,
			expectError:   true,
			errorContains: "enum type does not support operator '+'",
		},
		{
			name: "Enum Used In Logical Expression",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = true && false
}`,
			expectError:   true,
			errorContains: "enum type does not support operator '&&'",
		},
		{
			name: "Enum Comparison Equal Valid",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = Oak
    param is_oak: boolean = finish == Oak
}`,
			expectError: false,
		},
		{
			name: "Enum Comparison Not Equal Valid",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = Oak
    param not_oak: boolean = finish != Oak
}`,
			expectError: false,
		},
		{
			name: "Enum Comparison Type Mismatch",
			dslContent: `
product Desk {
    param finishA: enum { Oak, Pine } = Oak
    param finishB: enum { Red, Blue } = Red
    param result: boolean = finishA == finishB
}`,
			expectError:   true,
			errorContains: "enum type mismatch",
		},
		{
			name: "Enum Comparison Wrong Result Type",
			dslContent: `
product Desk {
    param finish: enum { Oak, Pine } = Oak
    param result: number = finish == Oak
}`,
			expectError:   true,
			errorContains: "comparison expression results in a boolean, but expected type is 'number'",
		},
		{
			name: "String Comparison Equal Valid",
			dslContent: `
product Test {
    param result: boolean = "a" == "a"
}`,
			expectError: false,
		},
		{
			name: "String Comparison Not Equal Valid",
			dslContent: `
product Test {
    param result: boolean = "a" != "b"
}`,
			expectError: false,
		},
		{
			name: "String Comparison Less Than Valid",
			dslContent: `
product Test {
    param result: boolean = "a" < "b"
}`,
			expectError: false,
		},
		{
			name: "String Comparison Less Than Or Equal Valid",
			dslContent: `
product Test {
    param result: boolean = "a" <= "a"
}`,
			expectError: false,
		},
		{
			name: "String Comparison Type Mismatch String And Number",
			dslContent: `
product Test {
    param result: boolean = "a" == 10
}`,
			expectError:   true,
			errorContains: "right side of comparison '=='",
		},
	}

	for _, tt := range tests {
		tt := tt // Capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a temporary file for the DSL content
			tmpFile, err := os.CreateTemp("", "validate_test_*.dsl")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.Write([]byte(withDSLVersionHeader(tt.dslContent))); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			// Parse the DSL using the public Parse function
			ast, err := Parse(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to parse DSL: %v", err)
			}

			// Validate the AST
			err = Validate(ast)

			if tt.expectError {
				if err == nil {
					t.Error("Expected validation error, but got nil")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected validation error: %v", err)
				}
			}
		})
	}
}

// TestValidate_OutputsNoneExclusivityMatrix proves the authoring-only
// exclusivity contract for outputs=["none"]: "none" must be the sole
// declared output, is mutually exclusive with every real output token in
// both list positions, is idempotent-invalid when repeated, and does not
// alter validation for existing supported output combinations. It also
// proves the DSL layer normalizes "none" the same way it normalizes every
// other output token (lowercase + trim), and that an explicit empty
// outputs=[] list (distinct from omission and from ["none"]) is not
// rejected at the DSL layer for adapter="freecad" -- rejection for that
// case is a planner-level invariant, proven separately in the planner
// package.
func TestValidate_OutputsNoneExclusivityMatrix(t *testing.T) {
	tests := []struct {
		name          string
		dslContent    string
		expectError   bool
		errorContains string
	}{
		{
			name: "NoneAlone Valid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["none"]

    param width: number = 50
}`,
			expectError: false,
		},
		{
			name: "NoneThenStep Invalid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["none", "step"]

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `output format "none" must be the only declared output`,
		},
		{
			name: "StepThenNone Invalid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["step", "none"]

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `output format "none" must be the only declared output`,
		},
		{
			name: "NoneThenPdf Invalid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["none", "pdf"]

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `output format "none" must be the only declared output`,
		},
		{
			name: "PdfThenNone Invalid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["pdf", "none"]

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `output format "none" must be the only declared output`,
		},
		{
			name: "NoneThenCsv Invalid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["none", "csv"]

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `output format "none" must be the only declared output`,
		},
		{
			name: "CsvThenNone Invalid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["csv", "none"]

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `output format "none" must be the only declared output`,
		},
		{
			name: "NoneRepeated Invalid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["none", "none"]

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `output format "none" must be the only declared output`,
		},
		{
			name: "NoneCaseAndWhitespaceNormalized Valid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["  NoNe  "]

    param width: number = 50
}`,
			expectError: false,
		},
		{
			name: "ExistingCsvAloneUnaffected Valid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["csv"]

    param width: number = 50
}`,
			expectError: false,
		},
		{
			name: "ExistingPdfAloneUnaffected Valid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["pdf"]

    param width: number = 50
}`,
			expectError: false,
		},
		{
			name: "ExistingStepPdfCsvComboUnaffected Valid",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = ["step", "pdf", "csv"]

    param width: number = 50
}`,
			expectError: false,
		},
		{
			name: "OmittedOutputsRemainValidAndDistinctFromNone",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"

    param width: number = 50
}`,
			expectError: false,
		},
		{
			name: "ExplicitEmptyOutputsListPassesDSLLayer",
			dslContent: `
product Widget {
    adapter = "freecad"
    source_model = "input/widget.FCStd"
    outputs = []

    param width: number = 50
}`,
			// Explicit outputs=[] is distinct from both omission and
			// explicit ["none"]. The DSL layer's per-element loops are
			// vacuously satisfied for an empty list (pre-existing,
			// unrelated to the "none" feature), so this passes here.
			// The planner rejects it deterministically because it is not
			// the canonical native-only request; see
			// TestCreatePlan_ExplicitEmptyOutputsListIsRejected in the
			// planner package.
			expectError: false,
		},
		{
			name: "AdapterNoneWithOutputsNoneIsRejectedAsNonEmpty",
			dslContent: `
product PlanningOnly {
    adapter = "none"
    outputs = ["none"]

    param width: number = 50
}`,
			expectError:   true,
			errorContains: `outputs must be empty when adapter is "none"`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpFile, err := os.CreateTemp("", "validate_none_test_*.dsl")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.Write([]byte(withDSLVersionHeader(tt.dslContent))); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			ast, err := Parse(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to parse DSL: %v", err)
			}

			err = Validate(ast)

			if tt.expectError {
				if err == nil {
					t.Error("Expected validation error, but got nil")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else if err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidate_ProfileAutoSelectionSingleProfile(t *testing.T) {
	t.Parallel()

	content := `
profile Build {
    output_dir = "out/build"
}`

	tmpFile, err := os.CreateTemp("", "validate_profile_autoselect_*.dsl")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(withDSLVersionHeader(content))); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	ast, err := Parse(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse DSL: %v", err)
	}

	if err := Validate(ast); err != nil {
		t.Fatalf("Unexpected validation error: %v", err)
	}

	if ast.ActiveProfileName == nil {
		t.Fatalf("expected auto-selected active profile, got nil")
	}
	if *ast.ActiveProfileName != "Build" {
		t.Fatalf("expected active profile 'Build', got '%s'", *ast.ActiveProfileName)
	}
}

func TestValidate_DSLVersion(t *testing.T) {
	tests := []struct {
		name          string
		ast           *AST
		expectErr     bool
		errorContains string
	}{
		{
			name:          "missing version directive",
			ast:           &AST{},
			expectErr:     true,
			errorContains: "missing version directive",
		},
		{
			name:          "unsupported DSL version",
			ast:           &AST{DSLVersion: "2.0"},
			expectErr:     true,
			errorContains: "unsupported DSL version",
		},
		{
			name: "supported DSL version",
			ast: &AST{
				DSLVersion: SupportedDSLVersion,
				Products: []*ProductNode{
					{
						Name: "X",
						Parameters: []*ParameterNode{
							{
								Name:         "a",
								Type:         ParameterType{Kind: ParamTypeNumber},
								DefaultValue: &LiteralExpression{Value: 1.0},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.ast)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected validation error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected error containing %q, got: %v", tt.errorContains, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
