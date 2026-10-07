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
		if isNumericValue(v) {
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

// min finds the minimum value
func (ac *AggregationComputer) min(values []any) any {
	return ac.extreme(values, -1)
}

// max finds the maximum value
func (ac *AggregationComputer) max(values []any) any {
	return ac.extreme(values, 1)
}

// extreme returns the value furthest in the direction of sign (-1 for the
// minimum, 1 for the maximum), or nil when there is none. Lists and maps have
// no order against scalars (compare reports 0, so one in first place was never
// displaced); they are skipped as nulls are.
func (ac *AggregationComputer) extreme(values []any, sign int) any {
	var best any
	for _, v := range values {
		if isCollection(v) {
			continue
		}
		if best == nil || sign*ac.compare(v, best) > 0 {
			best = v
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

// compare compares two values (returns -1, 0, or 1)
// Now delegates to the unified compareValues function
func (ac *AggregationComputer) compare(a, b any) int {
	return compareValues(a, b)
}
