package dsl

import (
	"os"
	"strings"
	"testing"
)

func TestValidate_EqualityTypeMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		dslContent    string
		expectError   bool
		errorContains string
	}{
		{
			name: "valid boolean equality",
			dslContent: `
product Test {
    param a: boolean = true
    param b: boolean = false
    param result: boolean = a == b
}`,
		},
		{
			name: "valid number equality",
			dslContent: `
product Test {
    param x: number = 10
    param y: number = 10
    param result: boolean = x != y
}`,
		},
		{
			name: "valid string equality",
			dslContent: `
product Test {
    param x: string = "a"
    param y: string = "b"
    param result: boolean = x == y
}`,
		},
		{
			name: "valid enum equality same enum type",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
    param result: boolean = finish == Pine
}`,
		},
		{
			name: "valid enum equality with same value set across different params",
			dslContent: `
product Test {
    param e1: enum { A, B } = A
    param e2: enum { A, B } = B
    param result: boolean = e1 == e2
}`,
		},
		{
			name: "invalid enum equality with same members but different order",
			dslContent: `
product Test {
    param e1: enum { A, B } = A
    param e2: enum { B, A } = B
    param result: boolean = e1 == e2
}`,
			expectError:   true,
			errorContains: "enum type mismatch",
		},
		{
			name: "valid enum literal compared with enum param",
			dslContent: `
product Test {
    param color: enum { Red, Blue } = Red
    param result: boolean = Red == color
}`,
		},
		{
			name: "valid enum constant compared with enum param",
			dslContent: `
const PRIMARY = Red
product Test {
    param color: enum { Red, Blue } = Blue
    param result: boolean = PRIMARY != color
}`,
		},
		{
			name: "invalid untyped enum constants compared without enum definition context",
			dslContent: `
const A = Red
const B = Blue
product Test {
    param result: boolean = A == B
}`,
			expectError:   true,
			errorContains: "left side of comparison '=='",
		},
		{
			name: "invalid boolean equals number",
			dslContent: `
product Test {
    param b: boolean = true
    param result: boolean = b == 1
}`,
			expectError:   true,
			errorContains: "right side of comparison '=='",
		},
		{
			name: "invalid boolean equals string",
			dslContent: `
product Test {
    param b: boolean = true
    param result: boolean = b == "true"
}`,
			expectError:   true,
			errorContains: "left side of comparison '=='",
		},
		{
			name: "invalid boolean equals enum",
			dslContent: `
product Test {
    param b: boolean = true
    param finish: enum { Oak, Pine } = Oak
    param result: boolean = b == finish
}`,
			expectError:   true,
			errorContains: "left side of comparison '=='",
		},
		{
			name: "invalid string equals number",
			dslContent: `
product Test {
    param s: string = "x"
    param result: boolean = s == 1
}`,
			expectError:   true,
			errorContains: "right side of comparison '=='",
		},
		{
			name: "invalid enum equals string",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
    param result: boolean = finish == "Oak"
}`,
			expectError:   true,
			errorContains: "right side of comparison '=='",
		},
		{
			name: "invalid enum equals number",
			dslContent: `
product Test {
    param finish: enum { Oak, Pine } = Oak
    param result: boolean = finish == 1
}`,
			expectError:   true,
			errorContains: "right side of comparison '=='",
		},
		{
			name: "invalid enum equals different enum type",
			dslContent: `
product Test {
    param finishA: enum { Oak, Pine } = Oak
    param finishB: enum { Red, Blue } = Red
    param result: boolean = finishA == finishB
}`,
			expectError:   true,
			errorContains: "enum type mismatch",
		},
		{
			name: "invalid enum literal compared with different enum set param",
			dslContent: `
product Test {
    param other_color: enum { Cyan, Magenta } = Cyan
    param result: boolean = Red == other_color
}`,
			expectError:   true,
			errorContains: "left side of comparison '=='",
		},
		{
			name: "valid boolean const to param ref chain equality",
			dslContent: `
const A = true
product Test {
    param b: boolean = A
    param c: boolean = b == true
}`,
		},
		{
			name: "side specific error true equals one reports right",
			dslContent: `
product Test {
    param result: boolean = true == 1
}`,
			expectError:   true,
			errorContains: "right side of comparison '=='",
		},
		{
			name: "side specific error one equals true reports left",
			dslContent: `
product Test {
    param result: boolean = 1 == true
}`,
			expectError:   true,
			errorContains: "left side of comparison '=='",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpFile, err := os.CreateTemp("", "validate_equality_*.dsl")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.Write([]byte(withDSLVersionHeader(tt.dslContent))); err != nil {
				t.Fatalf("failed to write temp DSL: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("failed to close temp DSL: %v", err)
			}

			ast, err := Parse(tmpFile.Name())
			if err != nil {
				t.Fatalf("failed to parse DSL: %v", err)
			}

			err = Validate(ast)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected validation error, got nil")
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected error containing %q, got %v", tt.errorContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
