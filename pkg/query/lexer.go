package query

import (
	"fmt"
	"strings"
	"unicode"
)

// Token represents a lexical token
type Token struct {
	Type    TokenType
	Value   string
	Pos     int
	Line    int
	Column  int
}

// TokenType represents the type of a token
type TokenType int

const (
	// Special tokens
	TokenEOF TokenType = iota
	TokenError

	// Keywords
	TokenMatch
	TokenWhere
	TokenReturn
	TokenCreate
	TokenDelete
	TokenDetach
	TokenSet
	TokenWith
	TokenLimit
	TokenSkip
	TokenOrderBy
	TokenAsc
	TokenDesc
	TokenDistinct
	TokenAs
	TokenAnd
	TokenOr
	TokenNot

	// Identifiers and literals
	TokenIdentifier
	TokenString
	TokenNumber
	TokenTrue
	TokenFalse
	TokenNull

	// Operators
	TokenEquals      // =
	TokenNotEquals   // !=, <>
	TokenLessThan    // <
	TokenGreaterThan // >
	TokenLessEquals  // <=
	TokenGreaterEquals // >=
	TokenPlus        // +
	TokenMinus       // -
	TokenStar        // *
	TokenSlash       // /
	TokenPercent     // %
	TokenDot         // .
	TokenColon       // :
	TokenComma       // ,
	TokenSemicolon   // ;

	// Delimiters
	TokenLeftParen   // (
	TokenRightParen  // )
	TokenLeftBracket // [
	TokenRightBracket // ]
	TokenLeftBrace   // {
	TokenRightBrace  // }

	// Relationship arrows
	TokenArrowLeft   // <-
	TokenArrowRight  // ->
	TokenArrowBoth   // -
)

var keywords = map[string]TokenType{
	"MATCH":    TokenMatch,
	"WHERE":    TokenWhere,
	"RETURN":   TokenReturn,
	"CREATE":   TokenCreate,
	"DELETE":   TokenDelete,
	"DETACH":   TokenDetach,
	"SET":      TokenSet,
	"WITH":     TokenWith,
	"LIMIT":    TokenLimit,
	"SKIP":     TokenSkip,
	"ORDER":    TokenOrderBy,
	"BY":       TokenOrderBy,
	"ASC":      TokenAsc,
	"DESC":     TokenDesc,
	"DISTINCT": TokenDistinct,
	"AS":       TokenAs,
	"AND":      TokenAnd,
	"OR":       TokenOr,
	"NOT":      TokenNot,
	"TRUE":     TokenTrue,
	"FALSE":    TokenFalse,
	"NULL":     TokenNull,
}

// Lexer tokenizes a query string
type Lexer struct {
	input   string
	pos     int
	line    int
	column  int
	tokens  []Token
}

// NewLexer creates a new lexer
func NewLexer(input string) *Lexer {
	return &Lexer{
		input:  input,
		pos:    0,
		line:   1,
		column: 1,
		tokens: make([]Token, 0),
	}
}

// Tokenize converts the input string into tokens
func (l *Lexer) Tokenize() ([]Token, error) {
	for l.pos < len(l.input) {
		// Skip whitespace
		if unicode.IsSpace(rune(l.input[l.pos])) {
			l.skipWhitespace()
			continue
		}

		// Skip comments
		if l.peek() == '/' && l.peekAhead(1) == '/' {
			l.skipLineComment()
			continue
		}

		// Try to read token
		token, err := l.nextToken()
		if err != nil {
			return nil, err
		}

		if token.Type != TokenEOF {
			l.tokens = append(l.tokens, token)
		}
	}

	// Add EOF token
	l.tokens = append(l.tokens, Token{
		Type:   TokenEOF,
		Value:  "",
		Pos:    l.pos,
		Line:   l.line,
		Column: l.column,
	})

	return l.tokens, nil
}

// nextToken reads the next token
func (l *Lexer) nextToken() (Token, error) {
	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Pos: l.pos, Line: l.line, Column: l.column}, nil
	}

	ch := l.peek()

	// Operators and delimiters
	switch ch {
	case '(':
		return l.makeToken(TokenLeftParen, string(l.advance())), nil
	case ')':
		return l.makeToken(TokenRightParen, string(l.advance())), nil
	case '[':
		return l.makeToken(TokenLeftBracket, string(l.advance())), nil
	case ']':
		return l.makeToken(TokenRightBracket, string(l.advance())), nil
	case '{':
		return l.makeToken(TokenLeftBrace, string(l.advance())), nil
	case '}':
		return l.makeToken(TokenRightBrace, string(l.advance())), nil
	case ',':
		return l.makeToken(TokenComma, string(l.advance())), nil
	case ';':
		return l.makeToken(TokenSemicolon, string(l.advance())), nil
	case '.':
		return l.makeToken(TokenDot, string(l.advance())), nil
	case ':':
		return l.makeToken(TokenColon, string(l.advance())), nil
	case '+':
		return l.makeToken(TokenPlus, string(l.advance())), nil
	case '*':
		return l.makeToken(TokenStar, string(l.advance())), nil
	case '/':
		return l.makeToken(TokenSlash, string(l.advance())), nil
	case '%':
		return l.makeToken(TokenPercent, string(l.advance())), nil
	case '=':
		l.advance()
		return l.makeToken(TokenEquals, "="), nil
	case '!':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TokenNotEquals, "!="), nil
		}
		return l.makeToken(TokenNot, "!"), nil
	case '<':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TokenLessEquals, "<="), nil
		} else if l.peek() == '>' {
			l.advance()
			return l.makeToken(TokenNotEquals, "<>"), nil
		} else if l.peek() == '-' {
			l.advance()
			return l.makeToken(TokenArrowLeft, "<-"), nil
		}
		return l.makeToken(TokenLessThan, "<"), nil
	case '>':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TokenGreaterEquals, ">="), nil
		}
		return l.makeToken(TokenGreaterThan, ">"), nil
	case '-':
		l.advance()
		if l.peek() == '>' {
			l.advance()
			return l.makeToken(TokenArrowRight, "->"), nil
		}
		return l.makeToken(TokenMinus, "-"), nil
	case '\'', '"':
		return l.readString()
	}

	// Numbers
	if unicode.IsDigit(rune(ch)) {
		return l.readNumber()
	}

	// Identifiers and keywords
	if unicode.IsLetter(rune(ch)) || ch == '_' {
		return l.readIdentifier()
	}

	return Token{}, fmt.Errorf("unexpected character '%c' at line %d, column %d", ch, l.line, l.column)
}

// readIdentifier reads an identifier or keyword
func (l *Lexer) readIdentifier() (Token, error) {
	start := l.pos
	startCol := l.column

	for l.pos < len(l.input) && (unicode.IsLetter(rune(l.input[l.pos])) || unicode.IsDigit(rune(l.input[l.pos])) || l.input[l.pos] == '_') {
		l.advance()
	}

	value := l.input[start:l.pos]
	valueUpper := strings.ToUpper(value)

	// Check if it's a keyword
	if tokenType, ok := keywords[valueUpper]; ok {
		return Token{
			Type:   tokenType,
			Value:  value,
			Pos:    start,
			Line:   l.line,
			Column: startCol,
		}, nil
	}

	return Token{
		Type:   TokenIdentifier,
		Value:  value,
		Pos:    start,
		Line:   l.line,
		Column: startCol,
	}, nil
}

// readNumber reads a numeric literal
func (l *Lexer) readNumber() (Token, error) {
	start := l.pos
	startCol := l.column

	for l.pos < len(l.input) && (unicode.IsDigit(rune(l.input[l.pos])) || l.input[l.pos] == '.') {
		l.advance()
	}

	return Token{
		Type:   TokenNumber,
		Value:  l.input[start:l.pos],
		Pos:    start,
		Line:   l.line,
		Column: startCol,
	}, nil
}

// readString reads a string literal
func (l *Lexer) readString() (Token, error) {
	start := l.pos
	startCol := l.column
	quote := l.advance()

	value := ""
	for l.pos < len(l.input) && l.peek() != quote {
		if l.peek() == '\\' {
			l.advance()
			if l.pos >= len(l.input) {
				return Token{}, fmt.Errorf("unterminated string at line %d", l.line)
			}
			// Handle escape sequences
			switch l.peek() {
			case 'n':
				value += "\n"
			case 't':
				value += "\t"
			case 'r':
				value += "\r"
			case '\\':
				value += "\\"
			case quote:
				value += string(quote)
			default:
				value += string(l.peek())
			}
			l.advance()
		} else {
			value += string(l.advance())
		}
	}

	if l.pos >= len(l.input) {
		return Token{}, fmt.Errorf("unterminated string at line %d", l.line)
	}

	l.advance() // Closing quote

	return Token{
		Type:   TokenString,
		Value:  value,
		Pos:    start,
		Line:   l.line,
		Column: startCol,
	}, nil
}

// Helper functions

func (l *Lexer) peek() byte {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *Lexer) peekAhead(n int) byte {
	pos := l.pos + n
	if pos >= len(l.input) {
		return 0
	}
	return l.input[pos]
}

func (l *Lexer) advance() byte {
	if l.pos >= len(l.input) {
		return 0
	}
	ch := l.input[l.pos]
	l.pos++
	l.column++
	if ch == '\n' {
		l.line++
		l.column = 1
	}
	return ch
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(rune(l.input[l.pos])) {
		l.advance()
	}
}

func (l *Lexer) skipLineComment() {
	for l.pos < len(l.input) && l.input[l.pos] != '\n' {
		l.advance()
	}
}

func (l *Lexer) makeToken(tokenType TokenType, value string) Token {
	return Token{
		Type:   tokenType,
		Value:  value,
		Pos:    l.pos - len(value),
		Line:   l.line,
		Column: l.column - len(value),
	}
}

func (t TokenType) String() string {
	switch t {
	case TokenEOF:
		return "EOF"
	case TokenError:
		return "ERROR"
	case TokenMatch:
		return "MATCH"
	case TokenWhere:
		return "WHERE"
	case TokenReturn:
		return "RETURN"
	case TokenCreate:
		return "CREATE"
	case TokenDelete:
		return "DELETE"
	case TokenIdentifier:
		return "IDENTIFIER"
	case TokenString:
		return "STRING"
	case TokenNumber:
		return "NUMBER"
	default:
		return fmt.Sprintf("Token(%d)", t)
	}
}
