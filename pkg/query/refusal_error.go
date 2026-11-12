package query

import "fmt"

// RefusalError is an error for a query that the executor refuses because of
// the query text alone: an undefined variable, an unknown function or
// procedure, a WITH item that cannot be bound. The data does not matter and
// the caller can fix it, so a server answers it as a client error.
//
// Message holds only text that came from the caller's query (a variable, a
// function, a procedure or a column name) or fixed text. It must never hold a
// value from the graph or an internal path, because a server shows it to the
// caller. A procedure's own argument error, a storage error, a timeout and a
// truncation are not refusals and do not use this type.
type RefusalError struct {
	Message string
}

// Error returns Message unchanged, so that a refusal reads as it did before it
// had a type.
func (e *RefusalError) Error() string { return e.Message }

// refusef builds a RefusalError from a format and its arguments.
func refusef(format string, args ...any) error {
	return &RefusalError{Message: fmt.Sprintf(format, args...)}
}
