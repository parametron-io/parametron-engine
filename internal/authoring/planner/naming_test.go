package planner

import (
	"math"
	"strings"
	"testing"
)

func TestResolveFilePattern_ValidInterpolation(t *testing.T) {
	out, err := ResolveFilePattern(
		"{product}_{profile}_{plan_hash}_{param:width}_{param:enabled}_{param:finish}_{param:label}_{const:SUFFIX}",
		NamingContext{
			ProductName: "Desk",
			ProfileName: "Prod",
			PlanHash:    "abc123",
			ResolvedParams: map[string]any{
				"width":   120.5,
				"enabled": true,
				"finish":  "Oak",
				"label":   "v1",
			},
			ResolvedConsts: map[string]ResolvedConst{
				"SUFFIX": {TypeName: "string", Value: "r2"},
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Desk_Prod_abc123_120.5_true_Oak_v1_r2"
	if out != want {
		t.Fatalf("unexpected output: got %q want %q", out, want)
	}
}

func TestResolveFilePattern_InvalidPlaceholder(t *testing.T) {
	_, err := ResolveFilePattern("{unknown}", NamingContext{})
	if err == nil {
		t.Fatal("expected error for unknown placeholder")
	}
}

func TestResolveFilePattern_MissingParam(t *testing.T) {
	_, err := ResolveFilePattern("{param:missing}", NamingContext{
		ResolvedParams: map[string]any{
			"width": 42.0,
		},
	})
	if err == nil {
		t.Fatal("expected error for missing param")
	}
}

func TestResolveFilePattern_DeterministicRepeatedRuns(t *testing.T) {
	ctx := NamingContext{
		ProductName: "Desk",
		ProfileName: "Prod",
		PlanHash:    "abc123",
		ResolvedParams: map[string]any{
			"width": 120.5,
			"name":  "x",
		},
		ResolvedConsts: map[string]ResolvedConst{
			"SUFFIX": {TypeName: "string", Value: "stable"},
		},
	}
	pattern := "{profile}_{product}_{param:width}_{param:name}_{plan_hash}_{const:SUFFIX}"

	first, err := ResolveFilePattern(pattern, ctx)
	if err != nil {
		t.Fatalf("unexpected error on first run: %v", err)
	}

	for i := 0; i < 100; i++ {
		got, err := ResolveFilePattern(pattern, ctx)
		if err != nil {
			t.Fatalf("unexpected error on run %d: %v", i, err)
		}
		if got != first {
			t.Fatalf("non-deterministic result on run %d: got %q want %q", i, got, first)
		}
	}
}

func TestResolveFilePattern_MissingConst(t *testing.T) {
	_, err := ResolveFilePattern("{const:MISSING}", NamingContext{})
	if err == nil {
		t.Fatal("expected error for missing const")
	}
	if !strings.Contains(err.Error(), "unknown const 'MISSING'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveFilePattern_NonStringConst(t *testing.T) {
	_, err := ResolveFilePattern("{const:COUNT}", NamingContext{
		ResolvedConsts: map[string]ResolvedConst{
			"COUNT": {TypeName: "number", Value: 3.0},
		},
	})
	if err == nil {
		t.Fatal("expected error for non-string const")
	}
	if !strings.Contains(err.Error(), "must resolve to string") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveFilePattern_MalformedConstPlaceholder(t *testing.T) {
	tests := []string{
		"{const:}",
		"{const:NAME:EXTRA}",
	}
	for _, pattern := range tests {
		t.Run(pattern, func(t *testing.T) {
			_, err := ResolveFilePattern(pattern, NamingContext{})
			if err == nil {
				t.Fatal("expected malformed const placeholder error")
			}
			if !strings.Contains(err.Error(), "invalid placeholder") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResolveFilePattern_ConstLookupCaseSensitive(t *testing.T) {
	_, err := ResolveFilePattern("{const:file_prefix}", NamingContext{
		ResolvedConsts: map[string]ResolvedConst{
			"FILE_PREFIX": {TypeName: "string", Value: "x"},
		},
	})
	if err == nil {
		t.Fatal("expected case-sensitive missing const error")
	}
	if !strings.Contains(err.Error(), "unknown const 'file_prefix'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveFilePattern_MultiplePlaceholdersWithConst(t *testing.T) {
	out, err := ResolveFilePattern(
		"{profile}_{product}_{const:TAG}_{param:x}",
		NamingContext{
			ProductName: "Desk",
			ProfileName: "Prod",
			ResolvedParams: map[string]any{
				"x": 17.0,
			},
			ResolvedConsts: map[string]ResolvedConst{
				"TAG": {TypeName: "string", Value: "vA"},
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "Prod_Desk_vA_17" {
		t.Fatalf("unexpected output: got %q", out)
	}
}

func TestResolveFilePattern_ParamFloatFormattingStable(t *testing.T) {
	out, err := ResolveFilePattern(
		"{param:a}_{param:b}_{param:tiny}_{param:big}_{param:neg}",
		NamingContext{
			ResolvedParams: map[string]any{
				"a":    1.0,
				"b":    1.0,
				"tiny": 0.000001,
				"big":  1000000.0,
				"neg":  -3.5,
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "1_1_0.000001_1000000_-3.5"
	if out != want {
		t.Fatalf("unexpected float formatting: got %q want %q", out, want)
	}
}

func TestResolveFilePattern_RejectsNonFiniteNumbers(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{name: "nan", value: math.NaN()},
		{name: "inf", value: math.Inf(1)},
		{name: "neg_inf", value: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveFilePattern("{param:x}", NamingContext{
				ResolvedParams: map[string]any{"x": tt.value},
			})
			if err == nil {
				t.Fatal("expected non-finite number error")
			}
			if !strings.Contains(err.Error(), "non-finite number") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
