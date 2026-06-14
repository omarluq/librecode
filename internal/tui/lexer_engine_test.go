package tui_test

import (
	"testing"

	"github.com/omarluq/librecode/internal/tui"
)

func TestLexerEngineCachesAnalysisResult(t *testing.T) {
	t.Parallel()

	engine := tui.NewLexerEngine()

	// Go code without a language tag — should detect the Go lexer.
	text := "func main() {\n\tfmt.Println(\"hello\")\n}"

	// First call runs the full analysis (cache miss).
	iter1, found := engine.IteratorFor(text)
	if !found {
		t.Fatal("expected lexer detection to succeed on first call")
	}

	tokens1 := iter1.Tokens()

	// Second call should return the cached lexer (cache hit).
	iter2, found := engine.IteratorFor(text)
	if !found {
		t.Fatal("expected lexer detection to succeed on second call")
	}

	tokens2 := iter2.Tokens()

	if len(tokens1) != len(tokens2) {
		t.Fatalf("token count mismatch: first=%d second=%d", len(tokens1), len(tokens2))
	}

	for index := range tokens1 {
		if tokens1[index].Type != tokens2[index].Type || tokens1[index].Value != tokens2[index].Value {
			t.Fatalf("token %d mismatch: first=%v second=%v", index, tokens1[index], tokens2[index])
		}
	}
}

func TestLexerEngineReturnsFalseForNoMatch(t *testing.T) {
	t.Parallel()

	engine := tui.NewLexerEngine()

	// Plain text with no distinguishing features.
	_, found := engine.IteratorFor("just some plain words")
	if found {
		t.Fatal("expected no lexer match for plain text")
	}
}

func TestLexerEngineDetectsGoCode(t *testing.T) {
	t.Parallel()

	engine := tui.NewLexerEngine()
	text := "package main\n\nfunc main() {}"

	iter, found := engine.IteratorFor(text)
	if !found {
		t.Fatal("expected engine to detect a lexer for Go code")
	}

	tokens := iter.Tokens()
	if len(tokens) == 0 {
		t.Fatal("expected non-empty token stream")
	}

	foundPackage := false

	for _, token := range tokens {
		if token.Value == "package" {
			foundPackage = true

			break
		}
	}

	if !foundPackage {
		t.Fatalf("expected 'package' keyword in tokens: %#v", tokens)
	}
}
