package query

import (
	"fmt"
	"strconv"
)

// parseMatch parses a MATCH clause
func (p *Parser) parseMatch() (*MatchClause, error) {
	if _, err := p.expect(TokenMatch); err != nil {
		return nil, err
	}

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
	if _, err := p.expect(TokenWhere); err != nil {
		return nil, err
	}

	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	return &WhereClause{Expression: expr}, nil
}

// parseReturn parses a RETURN clause
func (p *Parser) parseReturn() (*ReturnClause, error) {
	if _, err := p.expect(TokenReturn); err != nil {
		return nil, err
	}

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

			if _, err := p.expect(TokenRightParen); err != nil {
				return nil, err
			}

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
		aliasToken, err := p.expect(TokenIdentifier)
		if err != nil {
			return nil, err
		}
		item.Alias = aliasToken.Value
	}

	return item, nil
}

// parseCreate parses a CREATE clause
func (p *Parser) parseCreate() (*CreateClause, error) {
	if _, err := p.expect(TokenCreate); err != nil {
		return nil, err
	}

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

	if _, err := p.expect(TokenDelete); err != nil {
		return nil, err
	}

	// Parse variables
	for {
		varToken, err := p.expect(TokenIdentifier)
		if err != nil {
			return nil, err
		}
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
	if _, err := p.expect(TokenSet); err != nil {
		return nil, err
	}

	setClause := &SetClause{
		Assignments: make([]*Assignment, 0),
	}

	for {
		// variable.property = value
		varToken, err := p.expect(TokenIdentifier)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenDot); err != nil {
			return nil, err
		}
		propToken, err := p.expect(TokenIdentifier)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenEquals); err != nil {
			return nil, err
		}

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

// parseLimitSkip parses LIMIT and SKIP values
func (p *Parser) parseLimitSkip(query *Query) error {
	token := p.peek()

	if token.Type == TokenLimit {
		p.advance() // consume LIMIT
		limitToken, err := p.expect(TokenNumber)
		if err != nil {
			return err
		}
		if limit, err := strconv.Atoi(limitToken.Value); err == nil {
			query.Limit = limit
		} else {
			return fmt.Errorf("invalid LIMIT value: %s", limitToken.Value)
		}
	}

	if p.peek().Type == TokenSkip {
		p.advance() // consume SKIP
		skipToken, err := p.expect(TokenNumber)
		if err != nil {
			return err
		}
		if skip, err := strconv.Atoi(skipToken.Value); err == nil {
			query.Skip = skip
		} else {
			return fmt.Errorf("invalid SKIP value: %s", skipToken.Value)
		}
	}

	return nil
}
