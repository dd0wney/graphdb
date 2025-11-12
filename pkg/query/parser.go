package query

import (
	"fmt"
	"strconv"
	"strings"
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
			limit, _ := strconv.Atoi(limitToken.Value)
			query.Limit = limit

		case TokenSkip:
			p.advance() // consume SKIP
			skipToken := p.expect(TokenNumber)
			skip, _ := strconv.Atoi(skipToken.Value)
			query.Skip = skip

		case TokenSemicolon:
			p.advance()
			break

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
		Properties: make(map[string]interface{}),
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
		Properties: make(map[string]interface{}),
		MinHops:    1,
		MaxHops:    1,
	}

	// Determine initial direction from leading token
	leadingToken := p.peek().Type
	hasDetails := false

	if leadingToken == TokenArrowLeft {
		// <-[...]- pattern
		p.advance()
		rel.Direction = DirectionIncoming
	} else if leadingToken == TokenArrowRight {
		// ->[...] pattern (uncommon but possible)
		p.advance()
		rel.Direction = DirectionOutgoing
	} else if leadingToken == TokenMinus {
		// -[...]- or -[...]-> pattern
		p.advance()
		rel.Direction = DirectionBoth // May be updated after details
	} else {
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
			typeToken := p.expect(TokenIdentifier)
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
						min, _ := strconv.Atoi(parts[0])
						rel.MinHops = min
						if parts[1] != "" {
							max, _ := strconv.Atoi(parts[1])
							rel.MaxHops = max
						} else {
							rel.MaxHops = -1 // unlimited
						}
					}
				} else {
					// Just a single number, use it as both min and max
					val, _ := strconv.Atoi(numToken.Value)
					rel.MinHops = val
					rel.MaxHops = val
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

// parseProperties parses property map: {key: value, ...}
func (p *Parser) parseProperties() (map[string]interface{}, error) {
	p.expect(TokenLeftBrace)

	props := make(map[string]interface{})

	for p.peek().Type != TokenRightBrace {
		// Property key
		keyToken := p.expect(TokenIdentifier)
		p.expect(TokenColon)

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

	p.expect(TokenRightBrace)

	return props, nil
}

// parseValue parses a literal value
func (p *Parser) parseValue() (interface{}, error) {
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

// parseWhere parses a WHERE clause
func (p *Parser) parseWhere() (*WhereClause, error) {
	p.expect(TokenWhere)

	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	return &WhereClause{Expression: expr}, nil
}

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
			propertyToken := p.expect(TokenIdentifier)
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
