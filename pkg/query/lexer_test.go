package query

import (
	"testing"
)

// TestLexerBasicKeywords tests that keywords are tokenized correctly
func TestLexerBasicKeywords(t *testing.T) {
	input := "MATCH WHERE RETURN CREATE DELETE"
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	expected := []TokenType{
		TokenMatch,
		TokenWhere,
		TokenReturn,
		TokenCreate,
		TokenDelete,
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("Expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, token := range tokens {
		if token.Type != expected[i] {
			t.Errorf("Token %d: expected type %v, got %v", i, expected[i], token.Type)
		}
	}
}

// TestLexerIdentifiers tests identifier tokenization
func TestLexerIdentifiers(t *testing.T) {
	input := "person name age"
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	if len(tokens) != 4 { // 3 identifiers + EOF
		t.Fatalf("Expected 4 tokens, got %d", len(tokens))
	}

	expectedValues := []string{"person", "name", "age"}
	for i := 0; i < 3; i++ {
		if tokens[i].Type != TokenIdentifier {
			t.Errorf("Token %d: expected TokenIdentifier, got %v", i, tokens[i].Type)
		}
		if tokens[i].Value != expectedValues[i] {
			t.Errorf("Token %d: expected value %s, got %s", i, expectedValues[i], tokens[i].Value)
		}
	}
}

// TestLexerStrings tests string literal tokenization
func TestLexerStrings(t *testing.T) {
	input := `"hello" 'world' "multi word string"`
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	expectedValues := []string{"hello", "world", "multi word string"}

	for i := 0; i < len(expectedValues); i++ {
		if tokens[i].Type != TokenString {
			t.Errorf("Token %d: expected TokenString, got %v", i, tokens[i].Type)
		}
		if tokens[i].Value != expectedValues[i] {
			t.Errorf("Token %d: expected value %q, got %q", i, expectedValues[i], tokens[i].Value)
		}
	}
}

// TestLexerNumbers tests number tokenization
func TestLexerNumbers(t *testing.T) {
	input := "123 456.789 0.5 -42"
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	expectedValues := []string{"123", "456.789", "0.5", "-", "42"}
	expectedTypes := []TokenType{TokenNumber, TokenNumber, TokenNumber, TokenMinus, TokenNumber}

	for i := 0; i < len(expectedValues); i++ {
		if tokens[i].Type != expectedTypes[i] {
			t.Errorf("Token %d: expected type %v, got %v", i, expectedTypes[i], tokens[i].Type)
		}
		if tokens[i].Value != expectedValues[i] {
			t.Errorf("Token %d: expected value %s, got %s", i, expectedValues[i], tokens[i].Value)
		}
	}
}

// TestLexerOperators tests operator tokenization
func TestLexerOperators(t *testing.T) {
	input := "= != < > <= >= + - * / %"
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	expected := []TokenType{
		TokenEquals,
		TokenNotEquals,
		TokenLessThan,
		TokenGreaterThan,
		TokenLessEquals,
		TokenGreaterEquals,
		TokenPlus,
		TokenMinus,
		TokenStar,
		TokenSlash,
		TokenPercent,
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("Expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, token := range tokens {
		if token.Type != expected[i] {
			t.Errorf("Token %d: expected type %v, got %v", i, expected[i], token.Type)
		}
	}
}

// TestLexerDelimiters tests delimiter tokenization
func TestLexerDelimiters(t *testing.T) {
	input := "( ) [ ] { } , ; : ."
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	expected := []TokenType{
		TokenLeftParen,
		TokenRightParen,
		TokenLeftBracket,
		TokenRightBracket,
		TokenLeftBrace,
		TokenRightBrace,
		TokenComma,
		TokenSemicolon,
		TokenColon,
		TokenDot,
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("Expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, token := range tokens {
		if token.Type != expected[i] {
			t.Errorf("Token %d: expected type %v, got %v", i, expected[i], token.Type)
		}
	}
}

// TestLexerRelationshipArrows tests relationship arrow tokenization
func TestLexerRelationshipArrows(t *testing.T) {
	input := "-> <-"
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	expected := []TokenType{
		TokenArrowRight,
		TokenArrowLeft,
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("Expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, token := range tokens {
		if token.Type != expected[i] {
			t.Errorf("Token %d: expected type %v, got %v", i, expected[i], token.Type)
		}
	}
}

// TestLexerMinusVsRelationship tests that standalone '-' is tokenized as TokenMinus
// The parser will determine from context if it's part of a relationship pattern
func TestLexerMinusVsRelationship(t *testing.T) {
	input := "- 5 -"
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// All standalone '-' should be TokenMinus at lexer level
	// Parser handles context (arithmetic vs relationship)
	expected := []TokenType{
		TokenMinus,
		TokenNumber,
		TokenMinus,
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("Expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, token := range tokens {
		if token.Type != expected[i] {
			t.Errorf("Token %d: expected type %v, got %v", i, expected[i], token.Type)
		}
	}
}

// TestLexerComplexQuery tests a complete Cypher-like query
func TestLexerComplexQuery(t *testing.T) {
	input := `MATCH (p:Person {name: "Alice"})-[:KNOWS]->(f:Person) WHERE f.age > 30 RETURN f.name`
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// Verify we got tokens without error
	if len(tokens) == 0 {
		t.Fatal("Expected non-empty token list")
	}

	// First token should be MATCH
	if tokens[0].Type != TokenMatch {
		t.Errorf("Expected first token to be MATCH, got %v", tokens[0].Type)
	}

	// Should contain WHERE
	hasWhere := false
	for _, tok := range tokens {
		if tok.Type == TokenWhere {
			hasWhere = true
			break
		}
	}
	if !hasWhere {
		t.Error("Expected query to contain WHERE token")
	}

	// Should contain RETURN
	hasReturn := false
	for _, tok := range tokens {
		if tok.Type == TokenReturn {
			hasReturn = true
			break
		}
	}
	if !hasReturn {
		t.Error("Expected query to contain RETURN token")
	}
}

// TestLexerComments tests that comments are skipped
func TestLexerComments(t *testing.T) {
	input := `MATCH // this is a comment
	(p:Person) RETURN p`
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// Comments should be skipped
	for _, tok := range tokens {
		if tok.Value == "this" || tok.Value == "comment" {
			t.Error("Comment text should not appear in tokens")
		}
	}
}

// TestLexerLineAndColumnNumbers tests position tracking
func TestLexerLineAndColumnNumbers(t *testing.T) {
	input := "MATCH\n(p:Person)\nRETURN p"
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// First token (MATCH) should be on line 1
	if tokens[0].Line != 1 {
		t.Errorf("First token: expected line 1, got %d", tokens[0].Line)
	}

	// RETURN should be on line 3
	for _, tok := range tokens {
		if tok.Type == TokenReturn && tok.Line != 3 {
			t.Errorf("RETURN token: expected line 3, got %d", tok.Line)
		}
	}
}

// TestLexerEmptyInput tests empty string input
func TestLexerEmptyInput(t *testing.T) {
	input := ""
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// Should only have EOF
	if len(tokens) != 1 {
		t.Errorf("Expected 1 token (EOF), got %d", len(tokens))
	}

	if tokens[0].Type != TokenEOF {
		t.Errorf("Expected EOF token, got %v", tokens[0].Type)
	}
}

// TestLexerWhitespaceOnly tests whitespace-only input
func TestLexerWhitespaceOnly(t *testing.T) {
	input := "   \n\t  \r\n  "
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// Should only have EOF
	if len(tokens) != 1 {
		t.Errorf("Expected 1 token (EOF), got %d", len(tokens))
	}

	if tokens[0].Type != TokenEOF {
		t.Errorf("Expected EOF token, got %v", tokens[0].Type)
	}
}

// TestLexerUnterminatedString tests error handling for unterminated strings
func TestLexerUnterminatedString(t *testing.T) {
	input := `"unterminated string`
	lexer := NewLexer(input)

	_, err := lexer.Tokenize()
	if err == nil {
		t.Error("Expected error for unterminated string, got nil")
	}
}

// TestLexerCaseInsensitiveKeywords tests that keywords are case-insensitive
func TestLexerCaseInsensitiveKeywords(t *testing.T) {
	input := "match Match MATCH MaTcH"
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	// All should be tokenized as TokenMatch
	for i := 0; i < 4; i++ {
		if tokens[i].Type != TokenMatch {
			t.Errorf("Token %d: expected TokenMatch, got %v", i, tokens[i].Type)
		}
	}
}

// TestLexerBooleanLiterals tests TRUE, FALSE, and NULL
func TestLexerBooleanLiterals(t *testing.T) {
	input := "TRUE FALSE NULL true false null"
	lexer := NewLexer(input)

	tokens, err := lexer.Tokenize()
	if err != nil {
		t.Fatalf("Tokenize failed: %v", err)
	}

	expected := []TokenType{
		TokenTrue, TokenFalse, TokenNull,
		TokenTrue, TokenFalse, TokenNull,
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("Expected %d tokens, got %d", len(expected), len(tokens))
	}

	for i, token := range tokens {
		if token.Type != expected[i] {
			t.Errorf("Token %d: expected type %v, got %v", i, expected[i], token.Type)
		}
	}
}
