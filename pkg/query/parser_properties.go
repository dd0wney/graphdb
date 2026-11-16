package query

import (
	"strconv"
)

// parseProperties parses property map: {key: value, ...}
func (p *Parser) parseProperties() (map[string]any, error) {
	if _, err := p.expect(TokenLeftBrace); err != nil {
		return nil, err
	}

	props := make(map[string]any)

	for p.peek().Type != TokenRightBrace {
		// Property key
		keyToken, err := p.expect(TokenIdentifier)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenColon); err != nil {
			return nil, err
		}

		value, err := p.parsePropertyValue()
		if err != nil {
			return nil, err
		}

		props[keyToken.Value] = value

		if p.peek().Type == TokenComma {
			p.advance()
		}
	}

	if _, err := p.expect(TokenRightBrace); err != nil {
		return nil, err
	}

	return props, nil
}

// parsePropertyValue parses the value of one entry of a property map. A bare
// literal or parameter keeps its plain form, which the executor resolves once
// per query. Anything longer is an expression, and it may read a row variable,
// so it is kept as a *RowExprRef for the executor to calculate for each row.
func (p *Parser) parsePropertyValue() (any, error) {
	if isPlainValueToken(p.peek().Type) {
		if next := p.peekAhead(1).Type; next == TokenComma || next == TokenRightBrace {
			return p.parseValue()
		}
	}
	// A negative number is a constant too. As a row expression it would take
	// the per-row MERGE path, and -9223372036854775808 would overflow to a
	// float, because the expression negates the positive literal.
	if p.peek().Type == TokenMinus && p.peekAhead(1).Type == TokenNumber {
		if next := p.peekAhead(2).Type; next == TokenComma || next == TokenRightBrace {
			p.advance()
			return p.parseNegativeNumber()
		}
	}
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	return &RowExprRef{Expr: expr}, nil
}

func isPlainValueToken(t TokenType) bool {
	switch t {
	case TokenString, TokenNumber, TokenTrue, TokenFalse, TokenNull, TokenParameter:
		return true
	}
	return false
}

// parseNegativeNumber parses the number after a minus sign as one literal, so
// that the int64 minimum, whose magnitude has no positive int64, stays an int.
func (p *Parser) parseNegativeNumber() (any, error) {
	token := p.advance()
	text := "-" + token.Value
	if val, err := strconv.ParseInt(text, 10, 64); err == nil {
		return val, nil
	}
	if val, err := strconv.ParseFloat(text, 64); err == nil {
		return val, nil
	}
	return nil, syntaxErrorAt(token, "invalid number: %s", text)
}

// parseValue parses a literal value
func (p *Parser) parseValue() (any, error) {
	token := p.peek()

	switch token.Type {
	case TokenString:
		p.advance()
		return token.Value, nil
	case TokenNumber:
		p.advance()
		if val, err := strconv.ParseInt(token.Value, 10, 64); err == nil {
			return val, nil
		}
		if val, err := strconv.ParseFloat(token.Value, 64); err == nil {
			return val, nil
		}
		return nil, syntaxErrorAt(token, "invalid number: %s", token.Value)
	case TokenTrue:
		p.advance()
		return true, nil
	case TokenFalse:
		p.advance()
		return false, nil
	case TokenNull:
		p.advance()
		return nil, nil
	case TokenParameter:
		tok := p.advance()
		return &ParameterRef{Name: tok.Value}, nil
	default:
		return nil, syntaxErrorAt(token, "expected value, got %s", describeToken(token))
	}
}
