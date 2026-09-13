package dsl

import (
	"os"
	"strings"
	"testing"
)

func TestValidate_ProductBindingNamespaceAndGraph(t *testing.T) {
	tests := []struct {
		name             string
		dslContent       string
		errorContainsAll []string
	}{
		{
			name: "duplicate let",
			dslContent: `
product Widget {
    let width = 10
    let width = 20
}`,
			errorContainsAll: []string{"duplicate let name", "width"},
		},
		{
			name: "duplicate param",
			dslContent: `
product Widget {
    param width: number = 10
    param width: number = 20
}`,
			errorContainsAll: []string{"duplicate parameter name", "width"},
		},
		{
			name: "let param same name collision",
			dslContent: `
product Widget {
    let width = 10
    param width: number = 20
}`,
			errorContainsAll: []string{"duplicate product binding name", "width"},
		},
		{
			name: "const let collision",
			dslContent: `
const width = 100
product Widget {
    let width = 10
    param height: number = 20
}`,
			errorContainsAll: []string{"cannot override constant", "width"},
		},
		{
			name: "const param collision",
			dslContent: `
const width = 100
product Widget {
    param width: number = 10
}`,
			errorContainsAll: []string{"cannot override constant", "width"},
		},
		{
			name: "valid let to let",
			dslContent: `
product Widget {
    let base = 10
    let width = base + 5
    param result: number = width
}`,
		},
		{
			name: "valid let to param",
			dslContent: `
product Widget {
    param base: number = 10
    let width = base + 5
    param result: number = width
}`,
		},
		{
			name: "valid param to let",
			dslContent: `
product Widget {
    let base = 10
    param width: number = base + 5
}`,
		},
		{
			name: "valid mixed graph with const",
			dslContent: `
const BASE = 10
product Widget {
    let seed = BASE + 1
    param width: number = seed + 2
    let total = width + 3
    param result: number = total + BASE
}`,
		},
		{
			name: "valid larger mixed graph with two consts",
			dslContent: `
const BASE = 10
const STEP = 3
product Widget {
    param width: number = BASE
    let half = width / 2
    let offset = half + STEP
    param final: number = offset + 1
}`,
		},
		{
			name: "let let cycle",
			dslContent: `
product Widget {
    let a = b + 1
    let b = a + 1
    param result: number = 1
}`,
			errorContainsAll: []string{"circular dependency detected in product 'Widget'", "a", "b"},
		},
		{
			name: "let param cycle",
			dslContent: `
product Widget {
    let a = b + 1
    param b: number = a + 1
}`,
			errorContainsAll: []string{"circular dependency detected in product 'Widget'", "a", "b"},
		},
		{
			name: "param let cycle",
			dslContent: `
product Widget {
    param a: number = b + 1
    let b = a + 1
}`,
			errorContainsAll: []string{"circular dependency detected in product 'Widget'", "a", "b"},
		},
		{
			name: "larger mixed cycle with const seed",
			dslContent: `
const BASE = 10
product Widget {
    let a = BASE + b
    param b: number = c + 1
    let c = d + 1
    param d: number = a + 1
}`,
			errorContainsAll: []string{"circular dependency detected in product 'Widget'", "a", "b", "c", "d"},
		},
		{
			name: "unresolved name in let",
			dslContent: `
product Widget {
    let a = missing + 1
    param result: number = 1
}`,
			errorContainsAll: []string{"undefined binding reference", "missing"},
		},
		{
			name: "unresolved name in param",
			dslContent: `
product Widget {
    param a: number = missing + 1
}`,
			errorContainsAll: []string{"undefined binding reference", "missing"},
		},
		{
			name: "unresolved name in mixed graph",
			dslContent: `
product Widget {
    let base = 10
    param width: number = base + 5
    let total = width + missing
    param result: number = total
}`,
			errorContainsAll: []string{"undefined binding reference", "missing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "validate_binding_graph_*.dsl")
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
			if len(tt.errorContainsAll) == 0 {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tt.errorContainsAll)
			}

			for _, want := range tt.errorContainsAll {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("expected error containing %q, got: %v", want, err)
				}
			}
		})
	}
}
