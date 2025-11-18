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

	for !p.isAtEnd() {
		token := p.peek()

		switch token.Type {
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

		case TokenSet:
			setClause, err := p.parseSet()
			if err != nil {
				return nil, err
			}
			query.Set = setClause

		case TokenLimit:
			p.advance() // consume LIMIT
			limitToken := p.expect(TokenNumber)
			if limit, err := strconv.Atoi(limitToken.Value); err == nil {
				query.Limit = limit
			} else {
				return nil, fmt.Errorf("invalid LIMIT value: %s", limitToken.Value)
			}

		case TokenSkip:
			p.advance() // consume SKIP
			skipToken := p.expect(TokenNumber)
			if skip, err := strconv.Atoi(skipToken.Value); err == nil {
				query.Skip = skip
			} else {
				return nil, fmt.Errorf("invalid SKIP value: %s", skipToken.Value)
			}

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

// parseMatch parses a MATCH clause
func (p *Parser) parseMatch() (*MatchClause, error) {
	p.expect(TokenMatch)

	patterns := make([]*Pattern, 0)

	// Parse comma-separated patterns
	for {
		pattern, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pattern)

		if p.peek().Type != TokenComma {
			break
		}
		p.advance() // consume comma
	}

	return &MatchClause{Patterns: patterns}, nil
}

// parseWhere parses a WHERE clause
func (p *Parser) parseWhere() (*WhereClause, error) {
	p.expect(TokenWhere)

	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	return &WhereClause{Expression: expr}, nil
}

// parseReturn parses a RETURN clause
func (p *Parser) parseReturn() (*ReturnClause, error) {
	p.expect(TokenReturn)

	returnClause := &ReturnClause{
		Items: make([]*ReturnItem, 0),
	}

	// DISTINCT (optional)
	if p.peek().Type == TokenDistinct {
		p.advance()
		returnClause.Distinct = true
	}

	// Parse return items
	for {
		item, err := p.parseReturnItem()
		if err != nil {
			return nil, err
		}
		returnClause.Items = append(returnClause.Items, item)

		if p.peek().Type != TokenComma {
			break
		}
		p.advance()
	}

	// GROUP BY (optional)
	if p.peek().Type == TokenGroup {
		p.advance() // consume GROUP
		if p.peek().Type != TokenOrderBy { // TokenOrderBy is used for BY keyword
			return nil, fmt.Errorf("expected BY after GROUP")
		}
		p.advance() // consume BY

		// Parse group by expressions
		for {
			expr, err := p.parsePrimaryExpression()
			if err != nil {
				return nil, err
			}
			if propExpr, ok := expr.(*PropertyExpression); ok {
				returnClause.GroupBy = append(returnClause.GroupBy, propExpr)
			}

			if p.peek().Type != TokenComma {
				break
			}
			p.advance()
		}
	}

	return returnClause, nil
}

// parseReturnItem parses a single return item
func (p *Parser) parseReturnItem() (*ReturnItem, error) {
	item := &ReturnItem{}

	// Check for aggregation functions
	if p.peek().Type == TokenIdentifier {
		nextToken := p.peekAhead(1)
		if nextToken.Type == TokenLeftParen {
			// Aggregation function
			funcName := p.advance().Value
			p.advance() // consume (

			// Parse argument
			expr, err := p.parsePrimaryExpression()
			if err != nil {
				return nil, err
			}
			if propExpr, ok := expr.(*PropertyExpression); ok {
				item.Expression = propExpr
			}

			p.expect(TokenRightParen)

			item.Aggregate = funcName
		} else {
			// Regular property expression
			expr, err := p.parsePrimaryExpression()
			if err != nil {
				return nil, err
			}
			if propExpr, ok := expr.(*PropertyExpression); ok {
				item.Expression = propExpr
			}
		}
	}

	// AS alias (optional)
	if p.peek().Type == TokenAs {
		p.advance()
		aliasToken := p.expect(TokenIdentifier)
		item.Alias = aliasToken.Value
	}

	return item, nil
}

// parseCreate parses a CREATE clause
func (p *Parser) parseCreate() (*CreateClause, error) {
	p.expect(TokenCreate)

	patterns := make([]*Pattern, 0)

	for {
		pattern, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pattern)

		if p.peek().Type != TokenComma {
			break
		}
		p.advance()
	}

	return &CreateClause{Patterns: patterns}, nil
}

// parseDelete parses a DELETE clause
func (p *Parser) parseDelete() (*DeleteClause, error) {
	deleteClause := &DeleteClause{
		Variables: make([]string, 0),
	}

	// DETACH DELETE (optional)
	if p.peek().Type == TokenDetach {
		p.advance()
		deleteClause.Detach = true
	}

	p.expect(TokenDelete)

	// Parse variables
	for {
		varToken := p.expect(TokenIdentifier)
		deleteClause.Variables = append(deleteClause.Variables, varToken.Value)

		if p.peek().Type != TokenComma {
			break
		}
		p.advance()
	}

	return deleteClause, nil
}

// parseSet parses a SET clause
func (p *Parser) parseSet() (*SetClause, error) {
	p.expect(TokenSet)

	setClause := &SetClause{
		Assignments: make([]*Assignment, 0),
	}

	for {
		// variable.property = value
		varToken := p.expect(TokenIdentifier)
		p.expect(TokenDot)
		propToken := p.expect(TokenIdentifier)
		p.expect(TokenEquals)

		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}

		setClause.Assignments = append(setClause.Assignments, &Assignment{
			Variable: varToken.Value,
			Property: propToken.Value,
			Value:    value,
		})

		if p.peek().Type != TokenComma {
			break
		}
		p.advance()
	}

	return setClause, nil
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

func (p *Parser) expect(tokenType TokenType) Token {
	token := p.peek()
	if token.Type != tokenType {
		panic(fmt.Sprintf("expected %s, got %s at line %d", tokenType, token.Type, token.Line))
	}
	return p.advance()
}

func (p *Parser) isAtEnd() bool {
	return p.peek().Type == TokenEOF
}
