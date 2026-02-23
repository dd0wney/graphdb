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
	left, err := p.parseNotExpression()
	if err != nil {
		return nil, err
	}

	for p.peek().Type == TokenAnd {
		p.advance()
		right, err := p.parseNotExpression()
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

// parseNotExpression parses NOT prefix (binds tighter than AND, looser than comparisons)
func (p *Parser) parseNotExpression() (Expression, error) {
	if p.peek().Type == TokenNot {
		p.advance()
		operand, err := p.parseNotExpression() // right-recursive for NOT NOT x
		if err != nil {
			return nil, err
		}
		return &UnaryExpression{Operator: "NOT", Operand: operand}, nil
	}
	return p.parseComparisonExpression()
}

// parseComparisonExpression parses comparison expressions
func (p *Parser) parseComparisonExpression() (Expression, error) {
	left, err := p.parseAddSubExpression()
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
	case TokenNot:
		// NOT IN: expr NOT IN [list]
		if p.peekAhead(1).Type == TokenIn {
			p.advance() // consume NOT
			p.advance() // consume IN
			right, err := p.parseAddSubExpression()
			if err != nil {
				return nil, err
			}
			return &UnaryExpression{
				Operator: "NOT",
				Operand: &BinaryExpression{
					Left:     left,
					Operator: "IN",
					Right:    right,
				},
			}, nil
		}
		return left, nil
	case TokenIn:
		p.advance() // consume IN
		right, err := p.parseAddSubExpression()
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

	right, err := p.parseAddSubExpression()
	if err != nil {
		return nil, err
	}

	return &BinaryExpression{
		Left:     left,
		Operator: operator,
		Right:    right,
	}, nil
}

// parseAddSubExpression parses + and - (left-associative, lower precedence than * / %)
func (p *Parser) parseAddSubExpression() (Expression, error) {
	left, err := p.parseMulDivExpression()
	if err != nil {
		return nil, err
	}

	for p.peek().Type == TokenPlus || p.peek().Type == TokenMinus {
		op := p.advance()
		opStr := "+"
		if op.Type == TokenMinus {
			opStr = "-"
		}
		right, err := p.parseMulDivExpression()
		if err != nil {
			return nil, err
		}
		left = &ArithmeticExpression{
			Left:     left,
			Operator: opStr,
			Right:    right,
		}
	}

	return left, nil
}

// parseMulDivExpression parses *, /, % (left-associative, higher precedence than + -)
func (p *Parser) parseMulDivExpression() (Expression, error) {
	left, err := p.parseUnaryExpression()
	if err != nil {
		return nil, err
	}

	for p.peek().Type == TokenStar || p.peek().Type == TokenSlash || p.peek().Type == TokenPercent {
		op := p.advance()
		var opStr string
		switch op.Type {
		case TokenStar:
			opStr = "*"
		case TokenSlash:
			opStr = "/"
		case TokenPercent:
			opStr = "%"
		}
		right, err := p.parseUnaryExpression()
		if err != nil {
			return nil, err
		}
		left = &ArithmeticExpression{
			Left:     left,
			Operator: opStr,
			Right:    right,
		}
	}

	return left, nil
}

// parseUnaryExpression parses unary minus (tightest binding before primary)
func (p *Parser) parseUnaryExpression() (Expression, error) {
	if p.peek().Type == TokenMinus {
		p.advance()
		operand, err := p.parseUnaryExpression() // right-recursive for --x
		if err != nil {
			return nil, err
		}
		return &UnaryExpression{Operator: "-", Operand: operand}, nil
	}
	return p.parsePrimaryExpression()
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

	case TokenLeftBracket:
		return p.parseListLiteral()

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

// parseListLiteral parses [val1, val2, ...] into a LiteralExpression with []any value
func (p *Parser) parseListLiteral() (Expression, error) {
	if _, err := p.expect(TokenLeftBracket); err != nil {
		return nil, err
	}

	items := make([]any, 0)

	// Handle empty list
	if p.peek().Type == TokenRightBracket {
		p.advance()
		return &LiteralExpression{Value: items}, nil
	}

	for {
		val, err := p.parseValue()
		if err != nil {
			return nil, fmt.Errorf("invalid list element: %w", err)
		}
		items = append(items, val)

		if p.peek().Type != TokenComma {
			break
		}
		p.advance() // consume comma
	}

	if _, err := p.expect(TokenRightBracket); err != nil {
		return nil, fmt.Errorf("expected ] to close list literal")
	}

	return &LiteralExpression{Value: items}, nil
}
