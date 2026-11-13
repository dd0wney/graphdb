package query

import (
	"errors"
	"fmt"
	"math"
)

// maxRangeElements caps the list range() builds. Without a cap, range(0, 1e18)
// asks for memory until the process dies.
const maxRangeElements = 1_000_000

var errRangeZeroStep = errors.New("range(): step must not be 0")

func init() {
	RegisterFunction("range", fnRange)
}

// fnRange is range(start, end) and range(start, end, step): the integers from
// start to end, both included, stepping by step, which is 1 when absent. A
// negative step counts down. A range that cannot reach end is the empty list.
// The list is a []any of int64, as a list literal is.
func fnRange(args []any) (any, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, fmt.Errorf("range() takes 2 or 3 arguments (start, end[, step]); got %d", len(args))
	}
	ints := make([]int64, len(args))
	for i, arg := range args {
		v, err := rangeInteger(arg)
		if err != nil {
			return nil, fmt.Errorf("range(): %w", err)
		}
		ints[i] = v
	}
	step := int64(1)
	if len(ints) == 3 {
		step = ints[2]
	}
	n, err := rangeLength(ints[0], ints[1], step)
	if err != nil {
		return nil, err
	}

	out := make([]any, 0, n)
	value := ints[0]
	for i := int64(0); i < n; i++ {
		out = append(out, value)
		value += step
	}
	return out, nil
}

// rangeInteger reads an integer argument. A float with no fractional part is
// accepted, because a JSON number in a query parameter arrives as float64.
func rangeInteger(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case float64:
		// 2^63 is not representable as int64, so the upper bound is exclusive.
		if n == math.Trunc(n) && n >= -(1<<63) && n < 1<<63 {
			return int64(n), nil
		}
		return 0, fmt.Errorf("arguments must be integers; got %v", n)
	case nil:
		return 0, errors.New("arguments must be integers; got null")
	default:
		return 0, fmt.Errorf("arguments must be integers; got %T", v)
	}
}

// rangeLength is the number of elements of range(start, end, step), or an error
// when step is 0 or the count passes maxRangeElements. It counts in uint64 so
// that end-start cannot overflow.
func rangeLength(start, end, step int64) (int64, error) {
	if step == 0 {
		return 0, errRangeZeroStep
	}
	var distance, size uint64
	if step > 0 {
		if start > end {
			return 0, nil
		}
		distance = uint64(end) - uint64(start)
		size = uint64(step)
	} else {
		if start < end {
			return 0, nil
		}
		distance = uint64(start) - uint64(end)
		size = uint64(^step) + 1 // |step|, safe for math.MinInt64
	}
	// Compare before adding 1: distance/size can be 2^64-1, and +1 wraps to 0.
	steps := distance / size
	if steps >= maxRangeElements {
		return 0, fmt.Errorf("range(): the list would have more than %d elements, which is the limit", maxRangeElements)
	}
	return int64(steps) + 1, nil
}
