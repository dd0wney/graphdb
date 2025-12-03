package query

import (
	"fmt"
	"strconv"
	"strings"
)

// parsePattern parses a graph pattern
func (p *Parser) parsePattern() (*Pattern, error) {
	pattern := &Pattern{
		Nodes:         make([]*NodePattern, 0),
		Relationships: make([]*RelationshipPattern, 0),
	}

	// Parse first node
	node, err := p.parseNode()
	if err != nil {
		return nil, err
	}
	pattern.Nodes = append(pattern.Nodes, node)

	// Parse relationships and nodes
	for {
		tokenType := p.peek().Type
		if tokenType != TokenMinus && tokenType != TokenArrowLeft && tokenType != TokenArrowRight {
			break
		}
		rel, targetNode, err := p.parseRelationship(node)
		if err != nil {
			return nil, err
		}
		pattern.Relationships = append(pattern.Relationships, rel)
		pattern.Nodes = append(pattern.Nodes, targetNode)
		node = targetNode
	}

	return pattern, nil
}

// parseNode parses a node pattern: (variable:Label {prop: value})
func (p *Parser) parseNode() (*NodePattern, error) {
	if p.peek().Type != TokenLeftParen {
		return nil, fmt.Errorf("expected '(', got %s", p.peek().Type)
	}
	p.advance() // consume (

	node := &NodePattern{
		Labels:     make([]string, 0),
		Properties: make(map[string]any),
	}

	// Variable (optional)
	if p.peek().Type == TokenIdentifier {
		// Check if next token is : (label) or ) or { (properties)
		nextToken := p.peekAhead(1)
		if nextToken.Type == TokenColon || nextToken.Type == TokenRightParen || nextToken.Type == TokenLeftBrace {
			node.Variable = p.advance().Value
		}
	}

	// Labels (optional)
	for p.peek().Type == TokenColon {
		p.advance() // consume :
		if p.peek().Type == TokenIdentifier {
			labelToken := p.advance()
			node.Labels = append(node.Labels, labelToken.Value)
		}
	}

	// Properties (optional)
	if p.peek().Type == TokenLeftBrace {
		props, err := p.parseProperties()
		if err != nil {
			return nil, err
		}
		node.Properties = props
	}

	if p.peek().Type != TokenRightParen {
		return nil, fmt.Errorf("expected ')', got %s at line %d", p.peek().Type, p.peek().Line)
	}
	p.advance() // consume )

	return node, nil
}

// parseRelationship parses a relationship pattern
func (p *Parser) parseRelationship(fromNode *NodePattern) (*RelationshipPattern, *NodePattern, error) {
	rel := &RelationshipPattern{
		From:       fromNode,
		Properties: make(map[string]any),
		MinHops:    1,
		MaxHops:    1,
	}

	// Determine initial direction from leading token
	leadingToken := p.peek().Type
	hasDetails := false

	switch leadingToken {
	case TokenArrowLeft:
		// <-[...]- pattern
		p.advance()
		rel.Direction = DirectionIncoming
	case TokenArrowRight:
		// ->[...] pattern (uncommon but possible)
		p.advance()
		rel.Direction = DirectionOutgoing
	case TokenMinus:
		// -[...]- or -[...]-> pattern
		p.advance()
		rel.Direction = DirectionBoth // May be updated after details
	default:
		return nil, nil, fmt.Errorf("expected relationship pattern, got %v", leadingToken)
	}

	// Parse relationship details
	if p.peek().Type == TokenLeftBracket {
		hasDetails = true
		p.advance() // consume [

		// Variable (optional)
		if p.peek().Type == TokenIdentifier {
			rel.Variable = p.advance().Value
		}

		// Type (optional)
		if p.peek().Type == TokenColon {
			p.advance() // consume :
			typeToken, err := p.expect(TokenIdentifier)
			if err != nil {
				return nil, nil, err
			}
			rel.Type = typeToken.Value
		}

		// Variable-length path (optional): *1..5
		if p.peek().Type == TokenStar {
			p.advance()
			if p.peek().Type == TokenNumber {
				numToken := p.advance()
				// The number might be "1" or "1..3" (lexer includes range in number)
				if strings.Contains(numToken.Value, "..") {
					parts := strings.Split(numToken.Value, "..")
					if len(parts) == 2 {
						if min, err := strconv.Atoi(parts[0]); err == nil {
							rel.MinHops = min
						}
						if parts[1] != "" {
							if max, err := strconv.Atoi(parts[1]); err == nil {
								rel.MaxHops = max
							}
						} else {
							rel.MaxHops = -1 // unlimited
						}
					}
				} else {
					// Just a single number, use it as both min and max
					if val, err := strconv.Atoi(numToken.Value); err == nil {
						rel.MinHops = val
						rel.MaxHops = val
					}
				}
			}
		}

		// Properties (optional)
		if p.peek().Type == TokenLeftBrace {
			props, err := p.parseProperties()
			if err != nil {
				return nil, nil, err
			}
			rel.Properties = props
		}

		p.expect(TokenRightBracket)
	}

	// Check for trailing arrow to determine final direction
	// After -[...], we might see -> or -
	// After <-[...], we might see -
	if hasDetails {
		trailingToken := p.peek().Type
		if leadingToken == TokenMinus {
			if trailingToken == TokenArrowRight {
				p.advance()
				rel.Direction = DirectionOutgoing
			} else if trailingToken == TokenMinus {
				p.advance()
				rel.Direction = DirectionBoth
			}
		} else if leadingToken == TokenArrowLeft {
			if trailingToken == TokenMinus {
				p.advance()
				// Keep DirectionIncoming
			}
		}
	}

	// Target node
	toNode, err := p.parseNode()
	if err != nil {
		return nil, nil, err
	}
	rel.To = toNode

	return rel, toNode, nil
}
