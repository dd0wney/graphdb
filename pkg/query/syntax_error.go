package query

import "fmt"

// SyntaxError is the error the lexer and the parser return for a fault at a
// known place in the query text. A caller reads the position with errors.As.
//
// Line and Column are 1-based. They point at the first character of the
// token at the fault. A column counts bytes, not runes, because the lexer
// reads the input byte by byte. For a fault at the end of the query they
// point just after the last character.
type SyntaxError struct {
	Message string // the text without the position
	Token   string // the source text of the token at the fault, "" at the end of the query
	Line    int    // 1-based
	Column  int    // 1-based
}

// Error returns the message followed by the position.
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s at line %d, column %d", e.Message, e.Line, e.Column)
}

// syntaxErrorAt builds the error for a fault at tok. Every parser error for
// a known token goes through it, so that no error leaves out the position.
func syntaxErrorAt(tok Token, format string, args ...any) error {
	return &SyntaxError{
		Message: fmt.Sprintf(format, args...),
		Token:   tok.Value,
		Line:    tok.Line,
		Column:  tok.Column,
	}
}

// describeToken names tok for an error message: its source text in single
// quotes, or "end of query" at the end of the input. It never prints the
// numeric token type.
func describeToken(tok Token) string {
	if tok.Type == TokenEOF {
		return "end of query"
	}
	return "'" + tok.Value + "'"
}
