package runtime

import (
	"fmt"
	"math"
)

const roundingEpsilon = 1e-12

// BuiltinFunc defines the signature for a built-in DSL function.
type BuiltinFunc func(args []interface{}) (interface{}, error)

// Builtins is a registry of all built-in functions available in the DSL.
var Builtins = map[string]BuiltinFunc{
	"min":        minFunc,
	"max":        maxFunc,
	"abs":        absFunc,
	"round":      roundFunc,
	"floor":      floorFunc,
	"ceil":       ceilFunc,
	"clamp":      clampFunc,
	"round_up":   roundUpFunc,
	"round_down": roundDownFunc,
}

func minFunc(args []interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("min expects 2 arguments, but got %d", len(args))
	}
	a, okA := args[0].(float64)
	b, okB := args[1].(float64)
	if !okA || !okB {
		return nil, fmt.Errorf("min requires numeric arguments")
	}
	return math.Min(a, b), nil
}

func maxFunc(args []interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("max expects 2 arguments, but got %d", len(args))
	}
	a, okA := args[0].(float64)
	b, okB := args[1].(float64)
	if !okA || !okB {
		return nil, fmt.Errorf("max requires numeric arguments")
	}
	return math.Max(a, b), nil
}

func absFunc(args []interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("abs expects 1 argument, but got %d", len(args))
	}
	x, ok := args[0].(float64)
	if !ok {
		return nil, fmt.Errorf("abs requires a numeric argument")
	}
	return math.Abs(x), nil
}

func roundFunc(args []interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("round expects 1 argument, but got %d", len(args))
	}
	x, ok := args[0].(float64)
	if !ok {
		return nil, fmt.Errorf("round requires a numeric argument")
	}
	return math.Round(x), nil
}

func floorFunc(args []interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("floor expects 1 argument, but got %d", len(args))
	}
	x, ok := args[0].(float64)
	if !ok {
		return nil, fmt.Errorf("floor requires a numeric argument")
	}
	return math.Floor(x), nil
}

func ceilFunc(args []interface{}) (interface{}, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("ceil expects 1 argument, but got %d", len(args))
	}
	x, ok := args[0].(float64)
	if !ok {
		return nil, fmt.Errorf("ceil requires a numeric argument")
	}
	return math.Ceil(x), nil
}

func clampFunc(args []interface{}) (interface{}, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("clamp expects 3 arguments, but got %d", len(args))
	}
	x, okX := args[0].(float64)
	lo, okLo := args[1].(float64)
	hi, okHi := args[2].(float64)
	if !okX || !okLo || !okHi {
		return nil, fmt.Errorf("clamp requires numeric arguments")
	}
	if lo > hi {
		return nil, fmt.Errorf("clamp requires lo <= hi, but got lo=%v and hi=%v", lo, hi)
	}
	return math.Max(lo, math.Min(x, hi)), nil
}

func roundUpFunc(args []interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("round_up expects 2 arguments, but got %d", len(args))
	}
	x, okX := args[0].(float64)
	step, okStep := args[1].(float64)
	if !okX || !okStep {
		return nil, fmt.Errorf("round_up requires numeric arguments")
	}
	if step <= 0 {
		return nil, fmt.Errorf("round_up requires step > 0, but got %v", step)
	}
	q := x / step
	return math.Ceil(q-roundingEpsilon) * step, nil
}

func roundDownFunc(args []interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("round_down expects 2 arguments, but got %d", len(args))
	}
	x, okX := args[0].(float64)
	step, okStep := args[1].(float64)
	if !okX || !okStep {
		return nil, fmt.Errorf("round_down requires numeric arguments")
	}
	if step <= 0 {
		return nil, fmt.Errorf("round_down requires step > 0, but got %v", step)
	}
	q := x / step
	return math.Floor(q+roundingEpsilon) * step, nil
}
