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

	sumVal := ac.sum(values)
	count := float64(len(values))

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
	if len(values) == 0 {
		return nil
	}

	minVal := values[0]

	for i := 1; i < len(values); i++ {
		if ac.compare(values[i], minVal) < 0 {
			minVal = values[i]
		}
	}

	return minVal
}

// max finds the maximum value
func (ac *AggregationComputer) max(values []any) any {
	if len(values) == 0 {
		return nil
	}

	maxVal := values[0]

	for i := 1; i < len(values); i++ {
		if ac.compare(values[i], maxVal) > 0 {
			maxVal = values[i]
		}
	}

	return maxVal
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
