package query

// sum computes the sum of numeric values
func (ac *AggregationComputer) sum(values []any) any {
	if len(values) == 0 {
		return 0
	}

	var sumInt int64
	var sumFloat float64
	hasFloat := false

	for _, val := range values {
		switch v := val.(type) {
		case int64:
			sumInt += v
		case float64:
			hasFloat = true
			sumFloat += v
		case int:
			sumInt += int64(v)
		}
	}

	if hasFloat {
		return sumFloat + float64(sumInt)
	}
	return sumInt
}

// avg computes the average of numeric values
func (ac *AggregationComputer) avg(values []any) any {
	if len(values) == 0 {
		return nil
	}

	// Divide by the numeric values only: sum skips the rest, so counting
	// them too (a list, a string) shrank the average.
	numeric := 0
	for _, v := range values {
		if isSummed(v) {
			numeric++
		}
	}
	if numeric == 0 {
		return nil
	}
	sumVal := ac.sum(values)
	count := float64(numeric)

	switch s := sumVal.(type) {
	case int64:
		return float64(s) / count
	case float64:
		return s / count
	default:
		return nil
	}
}

// isSummed reports the kinds that avg counted before isNumericValue grew to
// every Go number kind. It stays narrow so that avg does not change in this
// task. (sum also adds a plain int, which avg does not count: a separate defect.)
func isSummed(v any) bool {
	switch v.(type) {
	case int64, float64:
		return true
	default:
		return false
	}
}

// min finds the minimum value
func (ac *AggregationComputer) min(values []any) any {
	return ac.extreme(values, -1)
}

// max finds the maximum value
func (ac *AggregationComputer) max(values []any) any {
	return ac.extreme(values, 1)
}

// extreme returns the value furthest in the direction of sign (-1 for the
// minimum, 1 for the maximum), or nil when there is none. Null is skipped. Every
// other value, a list and a map included, takes part through compareTotal, so
// the answer does not depend on the order of the rows (TCK Aggregation2 [9] to
// [12]). A NaN is the greatest number, as compareTotal places it; no TCK
// scenario covers NaN in min or max.
func (ac *AggregationComputer) extreme(values []any, sign int) any {
	var best any
	found := false
	for _, v := range values {
		if isNullValue(v) {
			continue
		}
		if !found || sign*compareTotal(v, best) > 0 {
			best = v
			found = true
		}
	}
	return best
}

// collect returns all non-nil values as a slice, preserving order
func (ac *AggregationComputer) collect(values []any) []any {
	result := make([]any, 0, len(values))
	for _, v := range values {
		if v != nil {
			result = append(result, v)
		}
	}
	return result
}
