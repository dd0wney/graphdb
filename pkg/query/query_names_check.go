package query

// checkQueryNames refuses a query that names something that does not exist,
// before any segment runs. The check reads the parsed query only, so the
// refusal does not depend on the data: a MATCH that finds zero rows is refused
// as a MATCH that finds a million.
//
// The executor evaluated such a name to null, so a misspelling gave a column
// of nulls or a write that changed nothing, and no error.
func checkQueryNames(q *Query) error {
	if q == nil {
		return nil
	}
	if err := checkUndefinedVariables(q); err != nil {
		return err
	}
	return checkUnknownFunctions(q)
}
