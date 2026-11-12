package query

import (
	"errors"
	"regexp"
	"testing"
)

var (
	rawTokenRe    = regexp.MustCompile(`Token\(\d+\)`)
	rawTokenAllRe = regexp.MustCompile(`^Token\(\d+\)$`)
	positionRe    = regexp.MustCompile(`at line \d+, column \d+$`)
)

// parseQueryText runs the lexer and the parser the way the API does and
// returns the first error.
func parseQueryText(input string) error {
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		return err
	}
	_, err = NewParser(tokens).Parse()
	return err
}

// Every defined token type needs a name, or an error message falls back to
// "Token(n)", which tells a caller nothing.
func TestTokenTypeStringNamesEveryConstant(t *testing.T) {
	for tt := TokenEOF; tt <= TokenArrowBoth; tt++ {
		if s := tt.String(); rawTokenAllRe.MatchString(s) {
			t.Errorf("TokenType %d has no name: String() = %q", int(tt), s)
		}
	}
	if got := TokenType(9999).String(); got != "Token(9999)" {
		t.Errorf("out-of-range String() = %q, want the Token(n) fallback", got)
	}
}

var syntaxFaultQueries = []string{
	// parser.go
	`RETRN 1`,
	`MATCH (p:Person) RETURN p.age > > 1`,
	`MATCH (p:Person) RETURN count(*) AS n`,
	`MATCH (p:Person) RETURN count(DISTINCT p.age) AS n`,
	`UNWIND [1,2,3] AS x RETURN x`,
	`OPTIONAL MATCH (p:Person {id: -1}) RETURN p`,
	`MATCH (p) RETURN p LIMIT x`,
	`MATCH (p) RETURN p SKIP 1.5`,
	`OPTIONAL RETURN 1`,
	"MATCH (p:Person)\nWHERE p.age > 1\nRETURN p.age > > 2\nLIMIT 1",
	// parser_clauses.go
	`MATCH (p:Person) RETURN`,
	`MATCH (p) RETURN p ORDER`,
	`CALL { MATCH (p) RETURN p } RETURN 1`,
	`CALL algo.pageRank( RETURN 1`,
	`MATCH (p) WITH RETURN p`,
	`MATCH (p) SET p.x = RETURN p`,
	`MERGE`,
	`MATCH (p) MERGE (q) ON RETURN p`,
	`UNWIND AS x RETURN x`,
	// parser_expressions.go
	`MATCH (p) WHERE EXISTS { (p)-[:R]->() } RETURN p`,
	`MATCH (p) WHERE p.x IS RETURN p`,
	`MATCH (p) WHERE p.x STARTS RETURN p`,
	`MATCH (p) RETURN CASE WHEN p.x THEN 1`,
	`MATCH (p) RETURN [1, 2`,
	// parser_patterns.go
	`MATCH (p:Person RETURN p`,
	`MATCH (p)-[:R->(q) RETURN p`,
	`MATCH p = (a)-[:R]->(b) RETURN p`,
	`MATCH p RETURN p`,
	// parser_properties.go
	`CREATE (p:Person {name: })`,
	`CREATE (p:Person {age: 1.2.3})`,
	// lexer.go
	`MATCH (p) WHERE p.name = 'abc RETURN p`,
	`MATCH (p) RETURN p # 1`,
	`MATCH (p) WHERE p.x = $ RETURN p`,
}

func TestSyntaxErrorFormOnEveryFault(t *testing.T) {
	for _, q := range syntaxFaultQueries {
		t.Run(q, func(t *testing.T) {
			err := parseQueryText(q)
			if err == nil {
				t.Fatalf("query parsed with no error; replace it with a faulty query: %q", q)
			}
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("error is not a *SyntaxError: %T %q", err, err)
			}
			if rawTokenRe.MatchString(err.Error()) {
				t.Errorf("error text shows a raw token number: %q", err)
			}
			if !positionRe.MatchString(err.Error()) {
				t.Errorf("error text does not end in a position: %q", err)
			}
			if se.Line < 1 || se.Column < 1 {
				t.Errorf("position is not 1-based: line %d, column %d", se.Line, se.Column)
			}
		})
	}
}

// Each expected position is counted by hand from the query text. Columns
// count from 1 at the first character of a line.
func TestSyntaxErrorExactPosition(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		token  string
		line   int
		column int
	}{
		{
			// "MATCH (p:Person) RETURN p.age > > 1"
			// MATCH 1-5, space 6, (p:Person) 7-16, space 17, RETURN 18-23,
			// space 24, p.age 25-29, space 30, first > at 31, space 32,
			// second > at 33. The parser fails on the second >.
			name: "double operator", query: `MATCH (p:Person) RETURN p.age > > 1`,
			token: ">", line: 1, column: 33,
		},
		{
			// "MATCH (p:Person) WHERE p.age > 60 RETURN count(*) AS n"
			// (p:Person) 7-16, WHERE 18-22, p.age 24-28, > at 30, 60 at
			// 32-33, RETURN 35-40, count 42-46, ( at 47, * at 48.
			name: "star in count", query: `MATCH (p:Person) WHERE p.age > 60 RETURN count(*) AS n`,
			token: "*", line: 1, column: 48,
		},
		{
			// "RETRN 1": RETRN is an identifier that starts at column 1.
			name: "misspelt clause", query: `RETRN 1`,
			token: "RETRN", line: 1, column: 1,
		},
		{
			// "MATCH (p:Person) RETURN" is 23 characters long. The end of
			// the query is column 24, and no token has source text there.
			name: "end of query", query: `MATCH (p:Person) RETURN`,
			token: "", line: 1, column: 24,
		},
		{
			// Line 1 "MATCH (p:Person)", line 2 "WHERE p.age > 1",
			// line 3 "RETURN p.age > > 2", line 4 "LIMIT 1".
			// On line 3: RETURN 1-6, space 7, p.age 8-12, space 13,
			// first > at 14, space 15, second > at 16.
			name: "line 3 of 4", query: "MATCH (p:Person)\nWHERE p.age > 1\nRETURN p.age > > 2\nLIMIT 1",
			token: ">", line: 3, column: 16,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parseQueryText(tc.query)
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("want a *SyntaxError, got %T %v", err, err)
			}
			if se.Token != tc.token || se.Line != tc.line || se.Column != tc.column {
				t.Errorf("got token %q at %d:%d, want %q at %d:%d (%v)",
					se.Token, se.Line, se.Column, tc.token, tc.line, tc.column, err)
			}
		})
	}
}
