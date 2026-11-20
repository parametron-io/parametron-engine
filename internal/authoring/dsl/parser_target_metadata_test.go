package dsl

import (
	"strings"
	"testing"
)

func TestParser_TargetMetadata_Canonical(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    target Pad:
        metadata material = "S235JR"
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	prod := ast.Products[0]
	if prod.TargetActions != nil {
		t.Fatalf("expected metadata-only target to produce no target actions, got %#v", prod.TargetActions)
	}
	if len(prod.TargetMetadata) != 1 {
		t.Fatalf("expected 1 target metadata declaration, got %d", len(prod.TargetMetadata))
	}

	decl := prod.TargetMetadata[0]
	if decl.SemanticTarget != "Pad" {
		t.Fatalf("expected SemanticTarget 'Pad', got %q", decl.SemanticTarget)
	}
	if decl.Key != "material" {
		t.Fatalf("expected metadata key 'material', got %q", decl.Key)
	}
	lit, ok := decl.Value.(*LiteralExpression)
	if !ok {
		t.Fatalf("expected *LiteralExpression value, got %T", decl.Value)
	}
	if lit.Value != "S235JR" {
		t.Fatalf("expected metadata value 'S235JR', got %#v", lit.Value)
	}
	if decl.Settings != nil {
		t.Fatalf("expected omitted settings block to produce nil settings, got %#v", decl.Settings)
	}
}

func TestParser_TargetMetadata_WithAction(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "action then metadata",
			input: `product Demo {
    target Pad:
        action = suppress
        metadata material = "S235JR"
}`,
		},
		{
			name: "metadata then action",
			input: `product Demo {
    target Pad:
        metadata material = "S235JR"
        action = suppress
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parseTargetActionSnippet(t, tt.input)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			prod := ast.Products[0]
			if len(prod.TargetActions) != 1 {
				t.Fatalf("expected 1 target action, got %d", len(prod.TargetActions))
			}
			if prod.TargetActions[0].SemanticTarget != "Pad" {
				t.Fatalf("expected target action for 'Pad', got %q", prod.TargetActions[0].SemanticTarget)
			}
			if len(prod.TargetMetadata) != 1 {
				t.Fatalf("expected 1 target metadata declaration, got %d", len(prod.TargetMetadata))
			}
			if prod.TargetMetadata[0].Key != "material" {
				t.Fatalf("expected metadata key 'material', got %q", prod.TargetMetadata[0].Key)
			}
		})
	}
}

func TestParser_TargetMetadata_SourceOrderPreserved(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    target Chamfer:
        metadata coating = "black"
        metadata material = "S235JR"
    target Cover:
        metadata material = "AlMg3"
    target Body:
        metadata supplier = "ACME"
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	prod := ast.Products[0]
	if prod.TargetActions != nil {
		t.Fatalf("expected no target actions, got %#v", prod.TargetActions)
	}
	if len(prod.TargetMetadata) != 4 {
		t.Fatalf("expected 4 target metadata declarations, got %d", len(prod.TargetMetadata))
	}

	want := []struct {
		target string
		key    string
	}{
		{"Chamfer", "coating"},
		{"Chamfer", "material"},
		{"Cover", "material"},
		{"Body", "supplier"},
	}
	for i, expected := range want {
		got := prod.TargetMetadata[i]
		if got.SemanticTarget != expected.target || got.Key != expected.key {
			t.Fatalf("index %d: expected %s.%s, got %s.%s", i, expected.target, expected.key, got.SemanticTarget, got.Key)
		}
	}
}

func TestParser_TargetMetadata_EmptySettingsNormalizesToNil(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "omitted block",
			input: `product Demo {
    target Pad: metadata material = "S235JR"
}`,
		},
		{
			name: "empty block",
			input: `product Demo {
    target Pad: metadata material = "S235JR" { }
}`,
		},
		{
			name: "whitespace-only block",
			input: `product Demo {
    target Pad: metadata material = "S235JR" {
    }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parseTargetActionSnippet(t, tt.input)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			decl := ast.Products[0].TargetMetadata[0]
			if decl.Settings != nil {
				t.Fatalf("expected empty settings block to normalize to nil, got %#v", decl.Settings)
			}
		})
	}
}

func TestParser_TargetMetadata_NonEmptySettingsParsedStructurally(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    target Pad:
        metadata material = "S235JR" {
            source = "supplier"
            verified = true
        }
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	decl := ast.Products[0].TargetMetadata[0]
	if len(decl.Settings) != 2 {
		t.Fatalf("expected 2 structurally parsed settings, got %d", len(decl.Settings))
	}
	if decl.Settings[0].Key != "source" || decl.Settings[1].Key != "verified" {
		t.Fatalf("unexpected settings order: %#v", decl.Settings)
	}
	if lit, ok := decl.Settings[0].Value.(*LiteralExpression); !ok || lit.Value != "supplier" {
		t.Fatalf("unexpected first setting value: %#v", decl.Settings[0].Value)
	}
	if lit, ok := decl.Settings[1].Value.(*LiteralExpression); !ok || lit.Value != true {
		t.Fatalf("unexpected second setting value: %#v", decl.Settings[1].Value)
	}
}

func TestParser_TargetMetadata_DuplicateMetadataKeyFails(t *testing.T) {
	_, err := parseTargetActionSnippet(t, `product Demo {
    target Pad:
        metadata material = "S235JR"
        metadata material = "S355"
}`)
	if err == nil {
		t.Fatal("expected duplicate metadata key parse error")
	}
	want := "duplicate metadata declaration 'material' for semantic target 'Pad' in product 'Demo'"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %v", want, err)
	}
}

func TestParser_TargetMetadata_DuplicateTargetDeclarationFails(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "action then metadata",
			input: `product Demo {
    target Pad: action = suppress
    target Pad: metadata material = "S235JR"
}`,
		},
		{
			name: "metadata then action",
			input: `product Demo {
    target Pad: metadata material = "S235JR"
    target Pad: action = suppress
}`,
		},
		{
			name: "metadata then metadata",
			input: `product Demo {
    target Pad: metadata material = "S235JR"
    target Pad: metadata supplier = "ACME"
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseTargetActionSnippet(t, tt.input)
			if err == nil {
				t.Fatal("expected duplicate target declaration parse error")
			}
			want := "duplicate target declaration for semantic target 'Pad' in product 'Demo'"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error containing %q, got %v", want, err)
			}
		})
	}
}

func TestParser_TargetMetadata_MalformedGrammar(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		errorContains string
	}{
		{
			name: "no entries",
			input: `product Demo {
    target Pad:
}`,
			errorContains: "expected 'action' or 'metadata' after target 'Pad:', got '}'",
		},
		{
			name: "missing metadata key",
			input: `product Demo {
    target Pad: metadata = "S235JR"
}`,
			errorContains: "invalid identifier '='",
		},
		{
			name: "missing equals",
			input: `product Demo {
    target Pad: metadata material "S235JR"
}`,
			errorContains: "expected '=', got '\"S235JR\"'",
		},
		{
			name: "missing value",
			input: `product Demo {
    target Pad: metadata material =
}`,
			errorContains: "unexpected token in expression: '}'",
		},
		{
			name: "unclosed settings block",
			input: `product Demo {
    target Pad: metadata material = "S235JR" {
        source = "supplier"`,
			errorContains: "unexpected EOF in metadata settings block",
		},
		{
			name: "missing setting equals",
			input: `product Demo {
    target Pad: metadata material = "S235JR" {
        source
    }
}`,
			errorContains: "expected '=', got '}'",
		},
		{
			name: "duplicate action",
			input: `product Demo {
    target Pad:
        action = suppress
        action = hide
}`,
			errorContains: "duplicate action declaration for semantic target 'Pad' in product 'Demo'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseTargetActionSnippet(t, tt.input)
			if err == nil {
				t.Fatalf("expected parse error containing %q, got nil", tt.errorContains)
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("expected error containing %q, got %v", tt.errorContains, err)
			}
		})
	}
}

func TestParser_TargetMetadata_MixedDeclarationsCompatibility(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    let base = "S235JR"
    param width: number = 10
    adapter = "freecad"
    source_model = "input/box.FCStd"
    target Pad:
        action = suppress
        metadata material = base
    outputs = ["step"]
}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	prod := ast.Products[0]
	if len(prod.Lets) != 1 || prod.Lets[0].Name != "base" {
		t.Fatalf("expected 1 let 'base', got %#v", prod.Lets)
	}
	if len(prod.Parameters) != 1 || prod.Parameters[0].Name != "width" {
		t.Fatalf("expected 1 param 'width', got %#v", prod.Parameters)
	}
	if lit, ok := prod.Adapter.(*LiteralExpression); !ok || lit.Value != "freecad" {
		t.Fatalf("expected adapter literal 'freecad', got %#v", prod.Adapter)
	}
	if lit, ok := prod.SourceModel.(*LiteralExpression); !ok || lit.Value != "input/box.FCStd" {
		t.Fatalf("expected source_model literal 'input/box.FCStd', got %#v", prod.SourceModel)
	}
	if arr, ok := prod.Outputs.(*ArrayExpression); !ok || len(arr.Elements) != 1 {
		t.Fatalf("expected outputs array with 1 element, got %#v", prod.Outputs)
	}
	if len(prod.TargetActions) != 1 || len(prod.TargetMetadata) != 1 {
		t.Fatalf("expected 1 target action and 1 metadata declaration, got %d and %d", len(prod.TargetActions), len(prod.TargetMetadata))
	}
	if ident, ok := prod.TargetMetadata[0].Value.(*IdentifierExpression); !ok || ident.Name != "base" {
		t.Fatalf("expected metadata value identifier 'base', got %#v", prod.TargetMetadata[0].Value)
	}
}

// TestParser_TargetMetadata_MetadataNotGloballyReserved permanently protects
// the contextual-keyword boundary: metadata declarations are recognized only
// inside a target declaration block, so existing identifiers named "metadata"
// remain valid.
func TestParser_TargetMetadata_MetadataNotGloballyReserved(t *testing.T) {
	ast, err := parseTargetActionSnippet(t, `product Demo {
    let metadata = "ok"
    param label: string = metadata
}`)
	if err != nil {
		t.Fatalf("unexpected parse error using 'metadata' as a let name: %v", err)
	}

	prod := ast.Products[0]
	if len(prod.Lets) != 1 || prod.Lets[0].Name != "metadata" {
		t.Fatalf("expected let named 'metadata', got %#v", prod.Lets)
	}
	if len(prod.TargetMetadata) != 0 {
		t.Fatalf("expected no target metadata declarations, got %#v", prod.TargetMetadata)
	}
}
