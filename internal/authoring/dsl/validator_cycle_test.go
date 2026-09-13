package dsl

import (
	"os"
	"strings"
	"testing"
)

func TestValidate_CircularParameterDependencies(t *testing.T) {
	tests := []struct {
		name          string
		dslContent    string
		errorContains string
	}{
		{
			name: "simple cycle",
			dslContent: `
product X {
    param a: number = b
    param b: number = a
}
`,
			errorContains: "circular dependency detected in product 'X': a → b → a",
		},
		{
			name: "3-node cycle",
			dslContent: `
product X {
    param a: number = b + 1
    param b: number = c + 1
    param c: number = a + 1
}
`,
			errorContains: "circular dependency detected in product 'X': a → b → c → a",
		},
		{
			name: "self-reference",
			dslContent: `
product X {
    param a: number = a + 1
}
`,
			errorContains: "circular dependency detected in product 'X': a → a",
		},
		{
			name: "valid chain",
			dslContent: `
product X {
    param a: number = 1
    param b: number = a + 1
}
`,
		},
		{
			name: "valid tree",
			dslContent: `
product X {
    param a: number = 1
    param b: number = a + 1
    param c: number = a + b
}
`,
		},
		{
			name: "constants are not treated as parameter edges",
			dslContent: `
const BASE = 10
product X {
    param a: number = BASE + 1
    param b: number = a + 1
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "validate_cycle_*.dsl")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.Write([]byte(withDSLVersionHeader(tt.dslContent))); err != nil {
				t.Fatalf("failed to write temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("failed to close temp file: %v", err)
			}

			ast, err := Parse(tmpFile.Name())
			if err != nil {
				t.Fatalf("failed to parse DSL: %v", err)
			}

			err = Validate(ast)
			if tt.errorContains == "" {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tt.errorContains)
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got: %v", tt.errorContains, err)
			}
		})
	}
}
