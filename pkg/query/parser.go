package query

import (
	"fmt"
	"strconv"
)

// Parser builds an AST from tokens
type Parser struct {
	tokens []Token
	pos    int
}

// NewParser creates a new parser
func NewParser(tokens []Token) *Parser {
	return &Parser{
		tokens: tokens,
		pos:    0,
	}
}

// Parse parses the tokens into a Query AST
func (p *Parser) Parse() (*Query, error) {
	query := &Query{}

	// Check for EXPLAIN or PROFILE prefix
	if p.peek().Type == TokenExplain {
		p.advance()
		query.Explain = true
	} else if p.peek().Type == TokenProfile {
		p.advance()
		query.Profile = true
	}

	// last is the previous top-level clause. A WHERE belongs to the MATCH
	// before it; the plan filters once, right after every MATCH, so a WHERE
	// anywhere else would run before the clause it was written after.
	last := ""
	for !p.isAtEnd() {
		token := p.peek()

		switch token.Type {
		case TokenOptional:
			p.advance() // consume OPTIONAL
			if p.peek().Type != TokenMatch {
				return nil, fmt.Errorf("expected MATCH after OPTIONAL at line %d", token.Line)
			}
			matchClause, err := p.parseMatch()
			if err != nil {
				return nil, err
			}
			entry := &OptionalMatchClause{Patterns: matchClause.Patterns}
			// WHERE after OPTIONAL MATCH attaches to this optional match
			if p.peek().Type == TokenWhere {
				where, err := p.parseWhere()
				if err != nil {
					return nil, err
				}
				entry.Where = where
			}
			query.OptionalMatches = append(query.OptionalMatches, entry)

		case TokenMatch:
			// The plan runs every MATCH before any write, so a MATCH written
			// after one would silently run before it.
			if w := writeClauseSeen(query); w != "" {
				return nil, fmt.Errorf("MATCH after %s is not supported at line %d: graphdb runs every MATCH before any write; put the MATCH first or split the query with WITH", w, token.Line)
			}
			if len(query.OptionalMatches) > 0 {
				return nil, fmt.Errorf("MATCH after OPTIONAL MATCH is not supported at line %d: graphdb runs every MATCH before OPTIONAL MATCH; put the MATCH first or split the query with WITH", token.Line)
			}
			matchClause, err := p.parseMatch()
			if err != nil {
				return nil, err
			}
			// Consecutive MATCH clauses are one join: for inner matches,
			// MATCH a MATCH b is MATCH a, b.
			if query.Match != nil {
				query.Match.Patterns = append(query.Match.Patterns, matchClause.Patterns...)
			} else {
				query.Match = matchClause
			}

		case TokenCall:
			if query.Call != nil {
				return nil, repeatedClauseError("CALL", token.Line)
			}
			callClause, err := p.parseCall()
			if err != nil {
				return nil, err
			}
			query.Call = callClause

		case TokenWhere:
			if last != "MATCH" {
				return nil, fmt.Errorf("WHERE after %s is not supported at line %d: graphdb filters right after MATCH; filter with WITH ... WHERE instead", clauseName(last), token.Line)
			}
			whereClause, err := p.parseWhere()
			if err != nil {
				return nil, err
			}
			// A WHERE for each of several MATCH clauses: the join keeps the
			// rows that satisfy all of them.
			query.Where = andWhere(query.Where, whereClause)

		case TokenReturn:
			if query.Return != nil {
				return nil, repeatedClauseError("RETURN", token.Line)
			}
			returnClause, err := p.parseReturn()
			if err != nil {
				return nil, err
			}
			query.Return = returnClause

		case TokenCreate:
			if query.Create != nil {
				return nil, fmt.Errorf("repeated CREATE is not supported at line %d: put the patterns in one CREATE, separated by commas", token.Line)
			}
			createClause, err := p.parseCreate()
			if err != nil {
				return nil, err
			}
			query.Create = createClause

		case TokenDetach, TokenDelete:
			deleteClause, err := p.parseDelete()
			if err != nil {
				return nil, err
			}
			if query.Delete != nil {
				if query.Delete.Detach != deleteClause.Detach {
					return nil, fmt.Errorf("DELETE and DETACH DELETE in one query are not supported at line %d: use one form for every variable", token.Line)
				}
				query.Delete.Variables = append(query.Delete.Variables, deleteClause.Variables...)
			} else {
				query.Delete = deleteClause
			}

		case TokenWith:
			withClause, err := p.parseWith()
			if err != nil {
				return nil, err
			}
			query.With = withClause

			// Recursively parse the next query segment
			next, err := p.Parse()
			if err != nil {
				return nil, err
			}
			query.Next = next
			return query, nil

		case TokenMerge:
			// The plan runs every MERGE before CREATE, SET, REMOVE and DELETE.
			if w := writeClauseSeen(query); w != "" && w != "MERGE" {
				return nil, fmt.Errorf("MERGE after %s is not supported at line %d: graphdb runs every MERGE before %s; put the MERGE first or split the query with WITH", w, token.Line, w)
			}
			mergeClause, err := p.parseMerge()
			if err != nil {
				return nil, err
			}
			query.Merges = append(query.Merges, mergeClause)

		case TokenUnwind:
			if query.Unwind != nil {
				return nil, fmt.Errorf("repeated UNWIND is not supported at line %d: split the query with WITH", token.Line)
			}
			unwindClause, err := p.parseUnwind()
			if err != nil {
				return nil, err
			}
			query.Unwind = unwindClause

		case TokenSet:
			setClause, err := p.parseSet()
			if err != nil {
				return nil, err
			}
			// SET a.x = 1 SET b.y = 2 is SET a.x = 1, b.y = 2: assignments
			// run in text order either way.
			if query.Set != nil {
				query.Set.Assignments = append(query.Set.Assignments, setClause.Assignments...)
			} else {
				query.Set = setClause
			}

		case TokenRemove:
			removeClause, err := p.parseRemove()
			if err != nil {
				return nil, err
			}
			if query.Remove != nil {
				query.Remove.Items = append(query.Remove.Items, removeClause.Items...)
			} else {
				query.Remove = removeClause
			}

		case TokenLimit:
			p.advance() // consume LIMIT
			limitToken, err := p.expect(TokenNumber)
			if err != nil {
				return nil, err
			}
			if limit, err := strconv.Atoi(limitToken.Value); err == nil {
				query.Limit = limit
			} else {
				return nil, fmt.Errorf("invalid LIMIT value: %s", limitToken.Value)
			}

		case TokenSkip:
			p.advance() // consume SKIP
			skipToken, err := p.expect(TokenNumber)
			if err != nil {
				return nil, err
			}
			if skip, err := strconv.Atoi(skipToken.Value); err == nil {
				query.Skip = skip
			} else {
				return nil, fmt.Errorf("invalid SKIP value: %s", skipToken.Value)
			}

		case TokenUnion:
			p.advance() // consume UNION
			all := false
			if p.peek().Type == TokenAll {
				p.advance() // consume ALL
				all = true
			}
			query.Union = &UnionClause{All: all}
			next, err := p.Parse()
			if err != nil {
				return nil, err
			}
			query.UnionNext = next
			return query, nil

		case TokenSemicolon:
			p.advance()

		case TokenEOF:
			return query, nil

		default:
			return nil, fmt.Errorf("unexpected token: %s at line %d", token.Type, token.Line)
		}
		last = clauseTokenName(token.Type)
	}

	return query, nil
}

// Helper functions

func (p *Parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peekAhead(n int) Token {
	pos := p.pos + n
	if pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[pos]
}

func (p *Parser) advance() Token {
	token := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return token
}

func (p *Parser) expect(tokenType TokenType) (Token, error) {
	token := p.peek()
	if token.Type != tokenType {
		return Token{}, fmt.Errorf("expected %s, got %s at line %d", tokenType, token.Type, token.Line)
	}
	return p.advance(), nil
}

func (p *Parser) isAtEnd() bool {
	return p.peek().Type == TokenEOF
}

// writeClauseSeen names a write clause the query already has, or returns "".
func writeClauseSeen(q *Query) string {
	switch {
	case q.Create != nil:
		return "CREATE"
	case len(q.Merges) > 0:
		return "MERGE"
	case q.Set != nil:
		return "SET"
	case q.Remove != nil:
		return "REMOVE"
	case q.Delete != nil:
		return "DELETE"
	}
	return ""
}

// repeatedClauseError refuses a second clause of a kind the query can hold
// once. Keeping only one of them would drop the other without a word.
func repeatedClauseError(clause string, line int) error {
	return fmt.Errorf("repeated %s is not supported at line %d", clause, line)
}

// clauseTokenName names the clause a top-level token starts, for the parser's
// record of the previous clause.
func clauseTokenName(t TokenType) string {
	switch t {
	case TokenMatch:
		return "MATCH"
	case TokenOptional:
		return "OPTIONAL MATCH"
	case TokenWhere:
		return "WHERE"
	case TokenCall:
		return "CALL"
	case TokenUnwind:
		return "UNWIND"
	case TokenMerge:
		return "MERGE"
	case TokenCreate:
		return "CREATE"
	case TokenSet:
		return "SET"
	case TokenRemove:
		return "REMOVE"
	case TokenDetach, TokenDelete:
		return "DELETE"
	case TokenReturn:
		return "RETURN"
	case TokenLimit:
		return "LIMIT"
	case TokenSkip:
		return "SKIP"
	}
	return ""
}

func clauseName(last string) string {
	if last == "" {
		return "the start of the query"
	}
	return last
}

// andWhere joins a second WHERE to the first with AND.
func andWhere(existing, next *WhereClause) *WhereClause {
	if existing == nil {
		return next
	}
	return &WhereClause{Expression: &BinaryExpression{Left: existing.Expression, Operator: "AND", Right: next.Expression}}
}
