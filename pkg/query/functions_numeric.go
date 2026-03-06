package query

import (
	"fmt"
	"math"
)

func init() {
	RegisterFunction("abs", fnAbs)
	RegisterFunction("ceil", fnCeil)
	RegisterFunction("floor", fnFloor)
	RegisterFunction("round", fnRound)
	RegisterFunction("toInteger", fnToInteger)
	RegisterFunction("toFloat", fnToFloat)
}

func toFloat64(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	default:
		return 0, false
	}
}

func fnAbs(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("abs requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	// Preserve type: int64 in → int64 out, float64 in → float64 out
	switch v := args[0].(type) {
	case int64:
		if v < 0 {
			return -v, nil
		}
		return v, nil
	case float64:
		return math.Abs(v), nil
	default:
		f, ok := toFloat64(args[0])
		if !ok {
			return nil, fmt.Errorf("abs: argument must be numeric")
		}
		return math.Abs(f), nil
	}
}

func fnCeil(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("ceil requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	f, ok := toFloat64(args[0])
	if !ok {
		return nil, fmt.Errorf("ceil: argument must be numeric")
	}
	return math.Ceil(f), nil
}

func fnFloor(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("floor requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	f, ok := toFloat64(args[0])
	if !ok {
		return nil, fmt.Errorf("floor: argument must be numeric")
	}
	return math.Floor(f), nil
}

func fnRound(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("round requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	f, ok := toFloat64(args[0])
	if !ok {
		return nil, fmt.Errorf("round: argument must be numeric")
	}
	return math.Round(f), nil
}

func fnToInteger(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("toInteger requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	switch v := args[0].(type) {
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	case int:
		return int64(v), nil
	case string:
		// Try parsing
		var i int64
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			return i, nil
		}
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			return int64(f), nil
		}
		return nil, fmt.Errorf("toInteger: cannot convert %q to integer", v)
	default:
		return nil, fmt.Errorf("toInteger: unsupported type %T", args[0])
	}
}

func fnToFloat(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("toFloat requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	switch v := args[0].(type) {
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	case int:
		return float64(v), nil
	case string:
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			return f, nil
		}
		return nil, fmt.Errorf("toFloat: cannot convert %q to float", v)
	default:
		return nil, fmt.Errorf("toFloat: unsupported type %T", args[0])
	}
}
