package query

import (
	"fmt"
)

// parseExpression parses a boolean expression
func (p *Parser) parseExpression() (Expression, error) {
	return p.parseOrExpression()
}

// parseOrExpression parses OR expressions
func (p *Parser) parseOrExpression() (Expression, error) {
	left, err := p.parseAndExpression()
	if err != nil {
		return nil, err
	}

	for p.peek().Type == TokenOr {
		p.advance()
		right, err := p.parseAndExpression()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpression{
			Left:     left,
			Operator: "OR",
			Right:    right,
		}
	}

	return left, nil
}

// parseAndExpression parses AND expressions
func (p *Parser) parseAndExpression() (Expression, error) {
	left, err := p.parseComparisonExpression()
	if err != nil {
		return nil, err
	}

	for p.peek().Type == TokenAnd {
		p.advance()
		right, err := p.parseComparisonExpression()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpression{
			Left:     left,
			Operator: "AND",
			Right:    right,
		}
	}

	return left, nil
}

// parseComparisonExpression parses comparison expressions
func (p *Parser) parseComparisonExpression() (Expression, error) {
	left, err := p.parsePrimaryExpression()
	if err != nil {
		return nil, err
	}

	token := p.peek()
	var operator string

	switch token.Type {
	case TokenEquals:
		operator = "="
	case TokenNotEquals:
		operator = "!="
	case TokenLessThan:
		operator = "<"
	case TokenGreaterThan:
		operator = ">"
	case TokenLessEquals:
		operator = "<="
	case TokenGreaterEquals:
		operator = ">="
	default:
		return left, nil // No comparison operator
	}

	p.advance()

	right, err := p.parsePrimaryExpression()
	if err != nil {
		return nil, err
	}

	return &BinaryExpression{
		Left:     left,
		Operator: operator,
		Right:    right,
	}, nil
}

// parsePrimaryExpression parses primary expressions
func (p *Parser) parsePrimaryExpression() (Expression, error) {
	token := p.peek()

	switch token.Type {
	case TokenIdentifier:
		// Could be: variable.property or just variable
		variable := p.advance().Value
		if p.peek().Type == TokenDot {
			p.advance()
			propertyToken, err := p.expect(TokenIdentifier)
			if err != nil {
				return nil, err
			}
			return &PropertyExpression{
				Variable: variable,
				Property: propertyToken.Value,
			}, nil
		}
		// Just a variable reference (treat as property expression with empty property)
		return &PropertyExpression{
			Variable: variable,
			Property: "",
		}, nil

	case TokenString, TokenNumber, TokenTrue, TokenFalse, TokenNull:
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return &LiteralExpression{Value: value}, nil

	case TokenLeftParen:
		p.advance()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.expect(TokenRightParen)
		return expr, nil

	default:
		return nil, fmt.Errorf("unexpected token in expression: %s", token.Type)
	}
}
