package dsl

import "testing"

func TestParseStringLiteral(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  string
		expectErr bool
	}{
		{"simple string", `"hello"`, "hello", false},
		{"escaped newline", `"line1\nline2"`, "line1\nline2", false},
		{"escaped tab", `"col1\tcol2"`, "col1\tcol2", false},
		{"escaped quote", `"say \"hello\""`, `say "hello"`, false},
		{"escaped backslash", `"path\\file"`, `path\file`, false},
		{"escaped left brace", `"literal \{"`, `literal {`, false},
		{"escaped right brace", `"literal \}"`, `literal }`, false},
		{"invalid escape", `"invalid\x"`, "", true},
		{"incomplete escape", `"abc\`, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStringLiteral(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestParseStringLiteralAndInterpolation_EscapedBracesDoNotInterpolate(t *testing.T) {
	raw := `"hello {param:name} \{param:name\}"`
	value, segments, hasInterpolation, err := parseStringLiteralAndInterpolation(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "hello {param:name} {param:name}" {
		t.Fatalf("unexpected parsed value: %q", value)
	}
	if !hasInterpolation {
		t.Fatalf("expected interpolation to be detected")
	}
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	if segments[0].Text != "hello " {
		t.Fatalf("unexpected first segment: %+v", segments[0])
	}
	if segments[1].ParamName != "name" {
		t.Fatalf("unexpected interpolation segment: %+v", segments[1])
	}
	if segments[2].Text != " {param:name}" {
		t.Fatalf("unexpected last segment: %+v", segments[2])
	}
}

func TestParseStringLiteralAndInterpolation_OnlyEscapedPlaceholderIsLiteral(t *testing.T) {
	raw := `"\{param:name\}"`
	value, segments, hasInterpolation, err := parseStringLiteralAndInterpolation(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "{param:name}" {
		t.Fatalf("unexpected parsed value: %q", value)
	}
	if hasInterpolation {
		t.Fatalf("did not expect interpolation")
	}
	if len(segments) != 1 || segments[0].Text != "{param:name}" {
		t.Fatalf("unexpected segments: %+v", segments)
	}
}

func TestParser_StringConcatenationIsLeftAssociative(t *testing.T) {
	p := &Parser{
		input: []rune(`product P {
    param x: string = "a" + "b" + "c"
}`),
		line: 1,
		col:  1,
	}

	ast, err := p.parse()
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	expr := ast.Products[0].Parameters[0].DefaultValue
	outer, ok := expr.(*BinaryExpression)
	if !ok {
		t.Fatalf("expected outer expression to be binary, got %T", expr)
	}
	if outer.Operator != "+" {
		t.Fatalf("expected outer operator '+', got %q", outer.Operator)
	}
	if _, ok := outer.Right.(*LiteralExpression); !ok {
		t.Fatalf("expected outer right operand to be literal, got %T", outer.Right)
	}

	left, ok := outer.Left.(*BinaryExpression)
	if !ok {
		t.Fatalf("expected outer left operand to be binary, got %T", outer.Left)
	}
	if left.Operator != "+" {
		t.Fatalf("expected left operator '+', got %q", left.Operator)
	}
}

func TestParser_StringConcatenationKeepsAdditivePrecedence(t *testing.T) {
	p := &Parser{
		input: []rune(`product P {
    param x: string = "x" + "y" == "xy"
}`),
		line: 1,
		col:  1,
	}

	ast, err := p.parse()
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	expr := ast.Products[0].Parameters[0].DefaultValue
	outer, ok := expr.(*BinaryExpression)
	if !ok {
		t.Fatalf("expected outer expression to be binary, got %T", expr)
	}
	if outer.Operator != "==" {
		t.Fatalf("expected outer operator '==', got %q", outer.Operator)
	}

	left, ok := outer.Left.(*BinaryExpression)
	if !ok {
		t.Fatalf("expected left side of comparison to be binary, got %T", outer.Left)
	}
	if left.Operator != "+" {
		t.Fatalf("expected left operator '+', got %q", left.Operator)
	}
}

func TestParseInterpolationSegments(t *testing.T) {
	tests := []struct {
		name             string
		in               string
		wantHasInterp    bool
		wantSegmentCount int
		wantErr          bool
	}{
		{name: "plain string", in: "hello", wantHasInterp: false, wantSegmentCount: 1},
		{name: "single placeholder", in: "hello {param:name}", wantHasInterp: true, wantSegmentCount: 2},
		{name: "multiple placeholders", in: "{param:first}-{param:last}", wantHasInterp: true, wantSegmentCount: 3},
		{name: "placeholder start", in: "{param:x} suffix", wantHasInterp: true, wantSegmentCount: 2},
		{name: "placeholder end", in: "prefix {param:x}", wantHasInterp: true, wantSegmentCount: 2},
		{name: "placeholder middle", in: "A{param:x}B{param:y}C", wantHasInterp: true, wantSegmentCount: 5},
		{name: "malformed missing close", in: "{param:name", wantErr: true},
		{name: "wrong prefix", in: "{parm:name}", wantErr: true},
		{name: "empty name", in: "{param:}", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segments, hasInterpolation, err := parseInterpolationSegments(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hasInterpolation != tt.wantHasInterp {
				t.Fatalf("hasInterpolation mismatch: got %v want %v", hasInterpolation, tt.wantHasInterp)
			}
			if len(segments) != tt.wantSegmentCount {
				t.Fatalf("segment count mismatch: got %d want %d", len(segments), tt.wantSegmentCount)
			}
		})
	}
}

func TestParser_InterpolatedStringExpressionShape(t *testing.T) {
	p := &Parser{
		input: []rune(`product P {
    param first: string = "A"
    param last: string = "B"
    param full: string = "{param:first}-{param:last}"
}`),
		line: 1,
		col:  1,
	}

	ast, err := p.parse()
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	expr := ast.Products[0].Parameters[2].DefaultValue
	interp, ok := expr.(*InterpolatedStringExpression)
	if !ok {
		t.Fatalf("expected interpolated string expression, got %T", expr)
	}
	if len(interp.Segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(interp.Segments))
	}
	if interp.Segments[0].ParamName != "first" || interp.Segments[1].Text != "-" || interp.Segments[2].ParamName != "last" {
		t.Fatalf("unexpected segments: %+v", interp.Segments)
	}
}
