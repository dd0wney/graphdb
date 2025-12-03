package graphql

// FilterCondition represents a single filter condition
type FilterCondition struct {
	Field    string
	Operator string
	Value    any
}

// FilterExpression represents a filter that can be simple or logical
type FilterExpression struct {
	// For simple conditions
	Conditions []FilterCondition

	// For logical operators
	AND []FilterExpression
	OR  []FilterExpression
	NOT *FilterExpression
}

// parseWhere parses the where argument into a filter expression
func parseWhere(args map[string]any) *FilterExpression {
	whereArg, ok := args["where"]
	if !ok {
		return nil
	}

	whereMap, ok := whereArg.(map[string]any)
	if !ok {
		return nil
	}

	return parseFilterExpression(whereMap)
}

// parseFilterExpression recursively parses a filter expression
func parseFilterExpression(whereMap map[string]any) *FilterExpression {
	expr := &FilterExpression{}

	for key, value := range whereMap {
		switch key {
		case "AND":
			// Parse AND array
			andArray, ok := value.([]any)
			if !ok {
				continue
			}
			for _, item := range andArray {
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				expr.AND = append(expr.AND, *parseFilterExpression(itemMap))
			}

		case "OR":
			// Parse OR array
			orArray, ok := value.([]any)
			if !ok {
				continue
			}
			for _, item := range orArray {
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				expr.OR = append(expr.OR, *parseFilterExpression(itemMap))
			}

		case "NOT":
			// Parse NOT object
			notMap, ok := value.(map[string]any)
			if !ok {
				continue
			}
			notExpr := parseFilterExpression(notMap)
			expr.NOT = notExpr

		default:
			// Regular field condition
			conditionMap, ok := value.(map[string]any)
			if !ok {
				continue
			}

			// Parse operators for this field
			for operator, opValue := range conditionMap {
				expr.Conditions = append(expr.Conditions, FilterCondition{
					Field:    key,
					Operator: operator,
					Value:    opValue,
				})
			}
		}
	}

	return expr
}
