package query

import "fmt"

// Token represents a lexical token
type Token struct {
	Type   TokenType
	Value  string
	Pos    int
	Line   int
	Column int
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
	TokenGroup
	TokenBy

	// Identifiers and literals
	TokenIdentifier
	TokenString
	TokenNumber
	TokenTrue
	TokenFalse
	TokenNull

	// Operators
	TokenEquals        // =
	TokenNotEquals     // !=, <>
	TokenLessThan      // <
	TokenGreaterThan   // >
	TokenLessEquals    // <=
	TokenGreaterEquals // >=
	TokenPlus          // +
	TokenMinus         // -
	TokenStar          // *
	TokenSlash         // /
	TokenPercent       // %
	TokenDot           // .
	TokenColon         // :
	TokenComma         // ,
	TokenSemicolon     // ;

	// Delimiters
	TokenLeftParen    // (
	TokenRightParen   // )
	TokenLeftBracket  // [
	TokenRightBracket // ]
	TokenLeftBrace    // {
	TokenRightBrace   // }

	// Relationship arrows
	TokenArrowLeft  // <-
	TokenArrowRight // ->
	TokenArrowBoth  // -
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
	"GROUP":    TokenGroup,
	"TRUE":     TokenTrue,
	"FALSE":    TokenFalse,
	"NULL":     TokenNull,
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
