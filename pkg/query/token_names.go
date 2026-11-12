package query

import "fmt"

// tokenNames names each token type for an error message such as "expected
// IDENTIFIER". TestTokenTypeStringNamesEveryConstant fails when a new
// constant has no entry here.
var tokenNames = map[TokenType]string{
	TokenEOF:   "EOF",
	TokenError: "ERROR",

	TokenMatch:    "MATCH",
	TokenWhere:    "WHERE",
	TokenReturn:   "RETURN",
	TokenCreate:   "CREATE",
	TokenDelete:   "DELETE",
	TokenDetach:   "DETACH",
	TokenSet:      "SET",
	TokenWith:     "WITH",
	TokenLimit:    "LIMIT",
	TokenSkip:     "SKIP",
	TokenOrder:    "ORDER",
	TokenOrderBy:  "BY",
	TokenAsc:      "ASC",
	TokenDesc:     "DESC",
	TokenDistinct: "DISTINCT",
	TokenAs:       "AS",
	TokenAnd:      "AND",
	TokenOr:       "OR",
	TokenNot:      "NOT",
	TokenGroup:    "GROUP",
	TokenBy:       "BY",
	TokenExplain:  "EXPLAIN",
	TokenProfile:  "PROFILE",
	TokenUnwind:   "UNWIND",
	TokenMerge:    "MERGE",
	TokenOn:       "ON",
	TokenOptional: "OPTIONAL",
	TokenCase:     "CASE",
	TokenWhen:     "WHEN",
	TokenThen:     "THEN",
	TokenElse:     "ELSE",
	TokenEnd:      "END",
	TokenUnion:    "UNION",
	TokenAll:      "ALL",
	TokenIs:       "IS",
	TokenIn:       "IN",
	TokenRemove:   "REMOVE",
	TokenStarts:   "STARTS",
	TokenEnds:     "ENDS",
	TokenContains: "CONTAINS",
	TokenCall:     "CALL",
	TokenYield:    "YIELD",

	TokenParameter:  "PARAMETER",
	TokenIdentifier: "IDENTIFIER",
	TokenString:     "STRING",
	TokenNumber:     "NUMBER",
	TokenTrue:       "TRUE",
	TokenFalse:      "FALSE",
	TokenNull:       "NULL",

	TokenEquals:        "'='",
	TokenNotEquals:     "'<>'",
	TokenLessThan:      "'<'",
	TokenGreaterThan:   "'>'",
	TokenLessEquals:    "'<='",
	TokenGreaterEquals: "'>='",
	TokenPlus:          "'+'",
	TokenMinus:         "'-'",
	TokenStar:          "'*'",
	TokenSlash:         "'/'",
	TokenPercent:       "'%'",
	TokenDot:           "'.'",
	TokenColon:         "':'",
	TokenComma:         "','",
	TokenSemicolon:     "';'",

	TokenLeftParen:    "'('",
	TokenRightParen:   "')'",
	TokenLeftBracket:  "'['",
	TokenRightBracket: "']'",
	TokenLeftBrace:    "'{'",
	TokenRightBrace:   "'}'",

	TokenArrowLeft:  "'<-'",
	TokenArrowRight: "'->'",
	TokenArrowBoth:  "'-'",
}

// String names the token type. A value outside the defined range keeps the
// Token(n) form, because there is no better name for it.
func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("Token(%d)", int(t))
}
