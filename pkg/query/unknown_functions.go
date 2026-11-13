package query

import (
	"fmt"
	"math"
)

// checkUnknownFunctions refuses a call to a function that GetFunction does not
// know, and a call to range() that is wrong whatever the data is.
//
// It uses GetFunction, the lookup that evaluation uses, so the case rule is the
// same: toupper is not toUpper. A function that SetVectorSearch or
// SetSearchIndex registers is unknown until the executor is wired, and the
// refusal is then right: the call gave null before.
//
// An aggregate (count, sum, collect and so on) is a ReturnItem.Aggregate, not a
// FunctionCallExpression, so a whole RETURN or WITH item is not seen here. An
// aggregate nested in an expression is a function call to a name that is not
// registered, and it gave null; it is refused with a message that says so.
func checkUnknownFunctions(q *Query) error {
	var found error
	walkQueryExpressions(q, func(_ string, expr Expression) {
		if found != nil {
			return
		}
		call, ok := expr.(*FunctionCallExpression)
		if !ok {
			return
		}
		if _, err := GetFunction(call.Name); err != nil {
			found = unknownFunctionError(call.Name)
			return
		}
		if call.Name == "range" {
			// The text names only the query's own literals and the limit.
			if err := checkRangeLiterals(call.Args); err != nil {
				found = &RefusalError{Message: err.Error()}
			}
		}
	})
	return found
}

func unknownFunctionError(name string) error {
	if isAggregateFunction(name) {
		return refusef("unknown function: %s() (an aggregate works only as a whole RETURN or WITH item, not inside an expression)", name)
	}
	return refusef("unknown function: %s()", name)
}

// checkRangeLiterals refuses what range() would refuse for any data, so the
// refusal does not wait for a row. The run-time error of a function is
// discarded by extractValue, which turns it into null; a literal fault must not
// reach that path.
func checkRangeLiterals(args []Expression) error {
	if len(args) != 2 && len(args) != 3 {
		return fmt.Errorf("range() takes 2 or 3 arguments (start, end[, step]); got %d", len(args))
	}
	values := make([]int64, len(args))
	allLiteral := true
	for i, arg := range args {
		v, isLiteral, err := literalRangeArg(arg)
		if err != nil {
			return fmt.Errorf("range(): %w", err)
		}
		values[i] = v
		allLiteral = allLiteral && isLiteral
	}
	if len(args) == 3 {
		if _, isLiteral, _ := literalRangeArg(args[2]); isLiteral && values[2] == 0 {
			return errRangeZeroStep
		}
	}
	if !allLiteral {
		return nil
	}
	step := int64(1)
	if len(args) == 3 {
		step = values[2]
	}
	_, err := rangeLength(values[0], values[1], step)
	return err
}

// literalRangeArg reads a literal integer argument, including a negative one,
// which the parser builds as unary minus over a literal. isLiteral is false
// for an argument whose value is known only at run time.
func literalRangeArg(arg Expression) (value int64, isLiteral bool, err error) {
	switch a := arg.(type) {
	case *LiteralExpression:
		v, convErr := rangeInteger(a.Value)
		if convErr != nil {
			return 0, true, convErr
		}
		return v, true, nil
	case *UnaryExpression:
		if a.Operator != "-" {
			return 0, false, nil
		}
		lit, ok := a.Operand.(*LiteralExpression)
		if !ok {
			return 0, false, nil
		}
		v, convErr := rangeInteger(lit.Value)
		if convErr != nil {
			return 0, true, convErr
		}
		if v == math.MinInt64 {
			return 0, true, fmt.Errorf("argument out of range")
		}
		return -v, true, nil
	default:
		return 0, false, nil
	}
}
