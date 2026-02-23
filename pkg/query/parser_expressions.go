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
	case TokenIs:
		p.advance() // consume IS
		if p.peek().Type == TokenNot {
			p.advance() // consume NOT
			if p.peek().Type != TokenNull {
				return nil, fmt.Errorf("expected NULL after IS NOT")
			}
			p.advance() // consume NULL
			return &BinaryExpression{
				Left:     left,
				Operator: "IS NOT NULL",
				Right:    &LiteralExpression{Value: nil},
			}, nil
		}
		if p.peek().Type != TokenNull {
			return nil, fmt.Errorf("expected NULL after IS")
		}
		p.advance() // consume NULL
		return &BinaryExpression{
			Left:     left,
			Operator: "IS NULL",
			Right:    &LiteralExpression{Value: nil},
		}, nil
	case TokenIn:
		p.advance() // consume IN
		right, err := p.parsePrimaryExpression()
		if err != nil {
			return nil, err
		}
		return &BinaryExpression{
			Left:     left,
			Operator: "IN",
			Right:    right,
		}, nil
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
		// Could be: function call, variable.property, or just variable
		variable := p.advance().Value

		// Check for function call: identifier followed by (
		if p.peek().Type == TokenLeftParen {
			return p.parseFunctionCall(variable)
		}

		if p.peek().Type == TokenDot {
			p.advance()
			propertyToken, err := p.expect(TokenIdentifier)
			if err != nil {
				return nil, err
			}
			// Namespaced function call: namespace.function(args)
			if p.peek().Type == TokenLeftParen {
				return p.parseFunctionCall(variable + "." + propertyToken.Value)
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

	case TokenParameter:
		tok := p.advance()
		return &ParameterExpression{Name: tok.Value}, nil

	case TokenCase:
		return p.parseCaseExpression()

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

// parseCaseExpression parses CASE [operand] WHEN ... THEN ... [ELSE ...] END
func (p *Parser) parseCaseExpression() (Expression, error) {
	p.advance() // consume CASE

	caseExpr := &CaseExpression{}

	// Simple form if next token is not WHEN
	if p.peek().Type != TokenWhen {
		operand, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		caseExpr.Operand = operand
	}

	// Parse WHEN clauses
	for p.peek().Type == TokenWhen {
		p.advance() // consume WHEN
		condition, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenThen); err != nil {
			return nil, fmt.Errorf("expected THEN after WHEN condition")
		}
		result, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		caseExpr.WhenClauses = append(caseExpr.WhenClauses, CaseWhen{
			Condition: condition,
			Result:    result,
		})
	}

	if len(caseExpr.WhenClauses) == 0 {
		return nil, fmt.Errorf("CASE requires at least one WHEN clause")
	}

	// Optional ELSE
	if p.peek().Type == TokenElse {
		p.advance() // consume ELSE
		elseResult, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		caseExpr.ElseResult = elseResult
	}

	if _, err := p.expect(TokenEnd); err != nil {
		return nil, fmt.Errorf("expected END to close CASE expression")
	}

	return caseExpr, nil
}
