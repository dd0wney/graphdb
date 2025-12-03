package query

import (
	"fmt"
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

		// Property value
		value, err := p.parseValue()
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
		return nil, fmt.Errorf("invalid number: %s", token.Value)
	case TokenTrue:
		p.advance()
		return true, nil
	case TokenFalse:
		p.advance()
		return false, nil
	case TokenNull:
		p.advance()
		return nil, nil
	default:
		return nil, fmt.Errorf("expected value, got %s", token.Type)
	}
}
