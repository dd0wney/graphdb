package query

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
	TokenOrder
	TokenOrderBy // BY keyword
	TokenAsc
	TokenDesc
	TokenDistinct
	TokenAs
	TokenAnd
	TokenOr
	TokenNot
	TokenGroup
	TokenBy
	TokenExplain
	TokenProfile
	TokenUnwind
	TokenMerge
	TokenOn
	TokenOptional
	TokenCase
	TokenWhen
	TokenThen
	TokenElse
	TokenEnd
	TokenUnion
	TokenAll
	TokenIs
	TokenIn
	TokenRemove
	TokenStarts
	TokenEnds
	TokenContains
	TokenCall
	TokenYield

	// Identifiers and literals
	TokenParameter // $name
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
	"ORDER":    TokenOrder,
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
	"EXPLAIN":  TokenExplain,
	"PROFILE":  TokenProfile,
	"UNWIND":   TokenUnwind,
	"MERGE":    TokenMerge,
	"ON":       TokenOn,
	"OPTIONAL": TokenOptional,
	"CASE":     TokenCase,
	"WHEN":     TokenWhen,
	"THEN":     TokenThen,
	"ELSE":     TokenElse,
	"END":      TokenEnd,
	"UNION":    TokenUnion,
	"ALL":      TokenAll,
	"IS":       TokenIs,
	"IN":       TokenIn,
	"REMOVE":   TokenRemove,
	"STARTS":   TokenStarts,
	"ENDS":     TokenEnds,
	"CONTAINS": TokenContains,
	"CALL":     TokenCall,
	"YIELD":    TokenYield,
}
