package dsl

import (
	"strings"
	"testing"
)

func TestValidate_StringControlCharacters(t *testing.T) {
	tests := []struct {
		name          string
		dslContent    string
		expectError   bool
		errorContains string
	}{
		{
			name: "null byte in string",
			dslContent: `product X {
    param x: string = "test` + "\x00" + `test"
}`,
			expectError:   true,
			errorContains: "invalid control character (0x00)",
		},
		{
			name: "newline in string",
			dslContent: `product X {
    param x: string = "test\ntest"
}`,
			expectError:   true,
			errorContains: "invalid control character (0x0A)",
		},
		{
			name: "raw control char",
			dslContent: `product X {
    param x: string = "test` + "\x01" + `test"
}`,
			expectError:   true,
			errorContains: "invalid control character (0x01)",
		},
		{
			name: "path traversal in path-like parameter",
			dslContent: `product X {
    param path: string = "../../etc/passwd"
}`,
			expectError:   true,
			errorContains: "is not a safe path",
		},
		{
			name: "safe relative path in path-like parameter",
			dslContent: `product X {
    param output_path: string = "safe/subdir/file.txt"
}`,
			expectError: false,
		},
		{
			name: "path traversal via string constant in path-like parameter",
			dslContent: `const BAD = "../../etc/passwd"
product X {
    param file_path: string = BAD
}`,
			expectError:   true,
			errorContains: "is not a safe path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parser{
				input: []rune(tt.dslContent),
				line:  1,
				col:   1,
			}

			ast, err := p.parse()
			if err != nil {
				t.Fatalf("failed to parse DSL: %v", err)
			}
			ast.DSLVersion = SupportedDSLVersion

			err = Validate(ast)
			if tt.expectError {
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

func TestValidate_StringConcatenationSuccess(t *testing.T) {
	dslContent := `const PREFIX = "pre"
product P {
    param left: string = "L"
    param right: string = "R"
    param direct: string = "a" + "b"
    param refs: string = left + right
    param chain: string = "a" + "b" + "c"
    param ternary_concat: string = true ? left + right : "x" + "y"
    param const_concat: string = PREFIX + "_fix"
}`

	p := &Parser{
		input: []rune(dslContent),
		line:  1,
		col:   1,
	}

	ast, err := p.parse()
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	ast.DSLVersion = SupportedDSLVersion

	if err := Validate(ast); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidate_StringConcatenationInvalidMixedTypes(t *testing.T) {
	tests := []struct {
		name          string
		paramType     string
		expr          string
		errorContains string
	}{
		{name: "string + number", paramType: "string", expr: `"a" + 1`, errorContains: "expected string literal"},
		{name: "number + string", paramType: "number", expr: `1 + "a"`, errorContains: "expected number literal"},
		{name: "string + boolean", paramType: "string", expr: `"a" + true`, errorContains: "expected string literal"},
		{name: "boolean + string", paramType: "number", expr: `true + "a"`, errorContains: "expected number literal"},
		{name: "string + enum ref", paramType: "string", expr: `"a" + finish`, errorContains: "has type 'enum"},
		{name: "enum ref + string", paramType: "string", expr: `finish + "a"`, errorContains: "has type 'enum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dslContent := `product P {
    param finish: enum { Oak, Pine } = Oak
    param bad: ` + tt.paramType + ` = ` + tt.expr + `
}`

			p := &Parser{
				input: []rune(dslContent),
				line:  1,
				col:   1,
			}

			ast, err := p.parse()
			if err != nil {
				t.Fatalf("failed to parse DSL: %v", err)
			}
			ast.DSLVersion = SupportedDSLVersion

			err = Validate(ast)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got: %v", tt.errorContains, err)
			}
		})
	}
}

func TestValidate_StringInterpolationSuccess(t *testing.T) {
	dslContent := `product P {
    param first: string = "A"
    param last: string = "B"
    param full: string = "{param:first}-{param:last}"
    param chained: string = "X-{param:full}"
    param escaped: string = "\{param:missing\}"
}`

	p := &Parser{
		input: []rune(dslContent),
		line:  1,
		col:   1,
	}
	ast, err := p.parse()
	if err != nil {
		t.Fatalf("failed to parse DSL: %v", err)
	}
	ast.DSLVersion = SupportedDSLVersion

	if err := Validate(ast); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidate_StringInterpolationFailures(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		errorContains string
	}{
		{
			name: "unknown parameter",
			content: `product P {
    param x: string = "{param:missing}"
}`,
			errorContains: "undefined binding reference in interpolation",
		},
		{
			name: "empty parameter name",
			content: `product P {
    param x: string = "{param:}"
}`,
			errorContains: "empty parameter name",
		},
		{
			name: "wrong prefix",
			content: `product P {
    param x: string = "{parm:name}"
}`,
			errorContains: "unsupported interpolation placeholder",
		},
		{
			name: "number parameter interpolation",
			content: `product P {
    param length: number = 5
    param x: string = "{param:length}"
}`,
			errorContains: "has type 'number'",
		},
		{
			name: "boolean parameter interpolation",
			content: `product P {
    param enabled: boolean = true
    param x: string = "{param:enabled}"
}`,
			errorContains: "has type 'boolean'",
		},
		{
			name: "enum parameter interpolation",
			content: `product P {
    param finish: enum { Oak, Pine } = Oak
    param x: string = "{param:finish}"
}`,
			errorContains: "has type 'enum'",
		},
		{
			name: "malformed missing close",
			content: `product P {
    param x: string = "{param:name"
}`,
			errorContains: "missing closing '}'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parser{
				input: []rune(tt.content),
				line:  1,
				col:   1,
			}
			ast, err := p.parse()
			if err != nil {
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected parse error containing %q, got: %v", tt.errorContains, err)
				}
				return
			}
			ast.DSLVersion = SupportedDSLVersion

			err = Validate(ast)
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got: %v", tt.errorContains, err)
			}
		})
	}
}

func TestValidate_StringExpressionStrictTypeRules(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectError   bool
		errorContains string
	}{
		{
			name: "string literal to string param valid",
			content: `product P {
    param name: string = "abc"
}`,
			expectError: false,
		},
		{
			name: "string interpolation to string param valid",
			content: `product P {
    param first: string = "A"
    param label: string = "prefix-{param:first}"
}`,
			expectError: false,
		},
		{
			name: "string concatenation to string param valid",
			content: `product P {
    param code: string = "A" + "B"
}`,
			expectError: false,
		},
		{
			name: "string literal to number invalid",
			content: `product P {
    param length: number = "10"
}`,
			expectError:   true,
			errorContains: "expected number literal, but got type string",
		},
		{
			name: "string literal to boolean invalid",
			content: `product P {
    param flag: boolean = "true"
}`,
			expectError:   true,
			errorContains: "expected boolean literal, but got type string",
		},
		{
			name: "string literal to enum invalid",
			content: `product P {
    param finish: enum { Painted, Raw } = "PAINTED"
}`,
			expectError:   true,
			errorContains: "expected enum literal, but got string literal",
		},
		{
			name: "string interpolation references number invalid",
			content: `product P {
    param count: number = 5
    param label: string = "count-{param:count}"
}`,
			expectError:   true,
			errorContains: "has type 'number', but expected 'string'",
		},
		{
			name: "string interpolation references boolean invalid",
			content: `product P {
    param enabled: boolean = true
    param label: string = "flag-{param:enabled}"
}`,
			expectError:   true,
			errorContains: "has type 'boolean', but expected 'string'",
		},
		{
			name: "string concatenation assigned to number invalid",
			content: `product P {
    param bad: number = "A" + "B"
}`,
			expectError:   true,
			errorContains: "left side of operator '+': expected number literal, but got type string",
		},
		{
			name: "string interpolation assigned to number invalid",
			content: `product P {
    param first: string = "A"
    param bad: number = "x-{param:first}"
}`,
			expectError:   true,
			errorContains: "interpolated string expression results in a string, but expected type is 'number'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parser{
				input: []rune(tt.content),
				line:  1,
				col:   1,
			}

			ast, err := p.parse()
			if err != nil {
				t.Fatalf("failed to parse DSL: %v", err)
			}
			ast.DSLVersion = SupportedDSLVersion

			err = Validate(ast)
			if tt.expectError {
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
