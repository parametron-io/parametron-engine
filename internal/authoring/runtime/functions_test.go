package runtime

import (
	"math"
	"strings"
	"testing"
)

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

func TestMinFunc(t *testing.T) {
	got, err := minFunc([]interface{}{2.0, 5.0})
	if err != nil {
		t.Fatalf("minFunc returned error: %v", err)
	}

	v, ok := got.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", got)
	}
	if !almostEqual(v, 2.0, 1e-9) {
		t.Fatalf("expected 2.0, got %v", v)
	}
}

func TestMaxFunc(t *testing.T) {
	got, err := maxFunc([]interface{}{2.0, 5.0})
	if err != nil {
		t.Fatalf("maxFunc returned error: %v", err)
	}

	v, ok := got.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", got)
	}
	if !almostEqual(v, 5.0, 1e-9) {
		t.Fatalf("expected 5.0, got %v", v)
	}
}

func TestAbsFunc(t *testing.T) {
	got, err := absFunc([]interface{}{-7.5})
	if err != nil {
		t.Fatalf("absFunc returned error: %v", err)
	}

	v, ok := got.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", got)
	}
	if !almostEqual(v, 7.5, 1e-9) {
		t.Fatalf("expected 7.5, got %v", v)
	}
}

func TestRoundFunc(t *testing.T) {
	got, err := roundFunc([]interface{}{3.6})
	if err != nil {
		t.Fatalf("roundFunc returned error: %v", err)
	}

	v, ok := got.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", got)
	}
	if !almostEqual(v, 4.0, 1e-9) {
		t.Fatalf("expected 4.0, got %v", v)
	}
}

func TestFloorFunc(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{name: "positive decimal", in: 12.9, want: 12.0},
		{name: "negative decimal", in: -12.1, want: -13.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := floorFunc([]interface{}{tt.in})
			if err != nil {
				t.Fatalf("floorFunc returned error: %v", err)
			}
			v, ok := got.(float64)
			if !ok {
				t.Fatalf("expected float64, got %T", got)
			}
			if !almostEqual(v, tt.want, 1e-9) {
				t.Fatalf("expected %v, got %v", tt.want, v)
			}
		})
	}
}

func TestCeilFunc(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{name: "positive decimal", in: 12.1, want: 13.0},
		{name: "negative decimal", in: -12.9, want: -12.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ceilFunc([]interface{}{tt.in})
			if err != nil {
				t.Fatalf("ceilFunc returned error: %v", err)
			}
			v, ok := got.(float64)
			if !ok {
				t.Fatalf("expected float64, got %T", got)
			}
			if !almostEqual(v, tt.want, 1e-9) {
				t.Fatalf("expected %v, got %v", tt.want, v)
			}
		})
	}
}

func TestClampFunc(t *testing.T) {
	tests := []struct {
		name       string
		x, lo, hi  float64
		want       float64
		expectErr  bool
		errSnippet string
	}{
		{name: "inside range", x: 5, lo: 0, hi: 10, want: 5},
		{name: "below range", x: -1, lo: 0, hi: 10, want: 0},
		{name: "above range", x: 15, lo: 0, hi: 10, want: 10},
		{name: "invalid bounds", x: 10, lo: 20, hi: 5, expectErr: true, errSnippet: "lo <= hi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := clampFunc([]interface{}{tt.x, tt.lo, tt.hi})
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSnippet != "" && !strings.Contains(err.Error(), tt.errSnippet) {
					t.Fatalf("expected error containing %q, got %q", tt.errSnippet, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("clampFunc returned error: %v", err)
			}
			v, ok := got.(float64)
			if !ok {
				t.Fatalf("expected float64, got %T", got)
			}
			if !almostEqual(v, tt.want, 1e-9) {
				t.Fatalf("expected %v, got %v", tt.want, v)
			}
		})
	}
}

func TestRoundUpFunc(t *testing.T) {
	tests := []struct {
		name       string
		x, step    float64
		want       float64
		expectErr  bool
		errSnippet string
	}{
		{name: "exact multiple", x: 10, step: 5, want: 10},
		{name: "non exact positive", x: 13, step: 5, want: 15},
		{name: "non exact negative", x: -13, step: 5, want: -10},
		{name: "floating step", x: 0.3, step: 0.1, want: 0.3},
		{name: "floating non exact above", x: 0.31, step: 0.1, want: 0.4},
		{name: "floating non exact below", x: 0.29, step: 0.1, want: 0.3},
		{name: "zero step", x: 10, step: 0, expectErr: true, errSnippet: "step > 0"},
		{name: "negative step", x: 10, step: -2, expectErr: true, errSnippet: "step > 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := roundUpFunc([]interface{}{tt.x, tt.step})
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSnippet != "" && !strings.Contains(err.Error(), tt.errSnippet) {
					t.Fatalf("expected error containing %q, got %q", tt.errSnippet, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("roundUpFunc returned error: %v", err)
			}
			v, ok := got.(float64)
			if !ok {
				t.Fatalf("expected float64, got %T", got)
			}
			if !almostEqual(v, tt.want, 1e-9) {
				t.Fatalf("expected %v, got %v", tt.want, v)
			}
		})
	}
}

func TestRoundDownFunc(t *testing.T) {
	tests := []struct {
		name       string
		x, step    float64
		want       float64
		expectErr  bool
		errSnippet string
	}{
		{name: "exact multiple", x: 10, step: 5, want: 10},
		{name: "non exact positive", x: 13, step: 5, want: 10},
		{name: "non exact negative", x: -13, step: 5, want: -15},
		{name: "floating step", x: 0.3, step: 0.1, want: 0.3},
		{name: "floating non exact above", x: 0.31, step: 0.1, want: 0.3},
		{name: "floating non exact below", x: 0.29, step: 0.1, want: 0.2},
		{name: "zero step", x: 10, step: 0, expectErr: true, errSnippet: "step > 0"},
		{name: "negative step", x: 10, step: -2, expectErr: true, errSnippet: "step > 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := roundDownFunc([]interface{}{tt.x, tt.step})
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSnippet != "" && !strings.Contains(err.Error(), tt.errSnippet) {
					t.Fatalf("expected error containing %q, got %q", tt.errSnippet, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("roundDownFunc returned error: %v", err)
			}
			v, ok := got.(float64)
			if !ok {
				t.Fatalf("expected float64, got %T", got)
			}
			if !almostEqual(v, tt.want, 1e-9) {
				t.Fatalf("expected %v, got %v", tt.want, v)
			}
		})
	}
}

func TestBuiltinFunctions_InvalidArgs(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]interface{}) (interface{}, error)
		args []interface{}
	}{
		{
			name: "min wrong arity",
			fn:   minFunc,
			args: []interface{}{1.0},
		},
		{
			name: "max wrong type",
			fn:   maxFunc,
			args: []interface{}{"2", 5.0},
		},
		{
			name: "abs wrong arity",
			fn:   absFunc,
			args: []interface{}{1.0, 2.0},
		},
		{
			name: "round wrong type",
			fn:   roundFunc,
			args: []interface{}{"3.6"},
		},
		{
			name: "floor wrong arity",
			fn:   floorFunc,
			args: []interface{}{1.0, 2.0},
		},
		{
			name: "ceil wrong type",
			fn:   ceilFunc,
			args: []interface{}{"3.6"},
		},
		{
			name: "clamp wrong arity",
			fn:   clampFunc,
			args: []interface{}{1.0, 2.0},
		},
		{
			name: "round_up wrong type",
			fn:   roundUpFunc,
			args: []interface{}{10.0, "5"},
		},
		{
			name: "round_down wrong type",
			fn:   roundDownFunc,
			args: []interface{}{10.0, "5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.fn(tt.args); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
