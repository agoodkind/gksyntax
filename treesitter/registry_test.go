package treesitter

import "testing"

func TestGrammarForLanguageReturnsRegisteredGrammars(t *testing.T) {
	languages := []string{"bash", "python", "go", "swift", "dart", "javascript", "ruby"}
	for _, language := range languages {
		grammar, ok := GrammarForLanguage(language)
		if !ok {
			t.Errorf("GrammarForLanguage(%q) reported unsupported, want supported", language)
			continue
		}
		if grammar == nil {
			t.Errorf("GrammarForLanguage(%q) returned nil grammar", language)
		}
	}
}

func TestGrammarForLanguageRejectsUnknown(t *testing.T) {
	grammar, ok := GrammarForLanguage("not-a-language")
	if ok {
		t.Errorf("GrammarForLanguage(unknown) reported supported, want unsupported")
	}
	if grammar != nil {
		t.Errorf("GrammarForLanguage(unknown) returned non-nil grammar")
	}
}

func TestLanguageForPathMapsExtensions(t *testing.T) {
	cases := map[string]string{
		"a.go":      "go",
		"b.py":      "python",
		"c.sh":      "bash",
		"d.swift":   "swift",
		"e.unknown": "text",
	}
	for path, want := range cases {
		got := LanguageForPath(path)
		if got != want {
			t.Errorf("LanguageForPath(%q) = %q, want %q", path, got, want)
		}
	}
}
