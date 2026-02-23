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
			matchClause, err := p.parseMatch()
			if err != nil {
				return nil, err
			}
			query.Match = matchClause

		case TokenWhere:
			whereClause, err := p.parseWhere()
			if err != nil {
				return nil, err
			}
			query.Where = whereClause

		case TokenReturn:
			returnClause, err := p.parseReturn()
			if err != nil {
				return nil, err
			}
			query.Return = returnClause

		case TokenCreate:
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
			query.Delete = deleteClause

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
			mergeClause, err := p.parseMerge()
			if err != nil {
				return nil, err
			}
			query.Merge = mergeClause

		case TokenUnwind:
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
			query.Set = setClause

		case TokenRemove:
			removeClause, err := p.parseRemove()
			if err != nil {
				return nil, err
			}
			query.Remove = removeClause

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
