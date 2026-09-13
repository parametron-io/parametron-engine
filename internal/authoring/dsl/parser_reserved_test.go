package dsl

import (
	"strings"
	"testing"
)

func TestParser_ReservedIdentifiers(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		errorContains string
	}{
		{"product name reserved", `product if {}`, "reserved keyword"},
		{"param name reserved", `product X { param if: number = 1 }`, "reserved keyword"},
		{"let name reserved", `product X { let if = 1 }`, "reserved keyword"},
		{"type as param name", `product X { param number: number = 1 }`, "reserved keyword"},
		{"boolean as const name", `const true = 1`, "reserved keyword"},
		{"future keyword", `product else {}`, "reserved keyword"},
		{"target as product name", `product target {}`, "reserved keyword"},
		{"target as param name", `product X { param target: number = 1 }`, "reserved keyword"},
		{"target as let name", `product X { let target = 1 }`, "reserved keyword"},
		{"action as product name", `product action {}`, "reserved keyword"},
		{"action as param name", `product X { param action: number = 1 }`, "reserved keyword"},
		{"action as let name", `product X { let action = 1 }`, "reserved keyword"},
		{"target as const name", `const target = 1`, "reserved keyword"},
		{"action as const name", `const action = 1`, "reserved keyword"},
		{"valid product name", `product X {}`, ""},
		{"valid param name", `product X { param width: number = 1 }`, ""},
		{"type usage", `product X { param x: number = 1 }`, ""},
		{"boolean literal", `product X { param x: boolean = true }`, ""},
		{"enum type", `product X { param x: enum { A, B } = A }`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parser{
				input: []rune(tt.input),
				line:  1,
				col:   1,
			}

			_, err := p.parse()
			if tt.errorContains != "" {
				if err == nil {
					t.Fatalf("expected parse error containing %q, got nil", tt.errorContains)
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Fatalf("expected parse error containing %q, got: %v", tt.errorContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
		})
	}
}
