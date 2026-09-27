// Package treesitter holds the tree-sitter grammar registry, the file
// extension to language map, and the vendored grammars. It is the shared
// foundation that the chunk and shelldecomp packages build on. Code that
// cannot be matched to a grammar is the caller's concern; this package only
// answers which grammar a language or path maps to.
package treesitter

import (
	"path/filepath"
	"strings"

	tree_sitter_kotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	tree_sitter_objc "github.com/tree-sitter-grammars/tree-sitter-objc/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tree_sitter_csharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_css "github.com/tree-sitter/tree-sitter-css/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_html "github.com/tree-sitter/tree-sitter-html/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_json "github.com/tree-sitter/tree-sitter-json/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	tree_sitter_awk "goodkind.io/gksyntax/treesitter/grammars/awk"
	tree_sitter_dart "goodkind.io/gksyntax/treesitter/grammars/dart"
	tree_sitter_perl "goodkind.io/gksyntax/treesitter/grammars/perl"
	tree_sitter_swift "goodkind.io/gksyntax/treesitter/grammars/swift"
)

type grammarKey string

const (
	grammarJavaScript grammarKey = "javascript"
	grammarJS         grammarKey = "js"
	grammarTypeScript grammarKey = "typescript"
	grammarTS         grammarKey = "ts"
	grammarPython     grammarKey = "python"
	grammarPy         grammarKey = "py"
	grammarJava       grammarKey = "java"
	grammarCPP        grammarKey = "cpp"
	grammarCXX        grammarKey = "c++"
	grammarC          grammarKey = "c"
	grammarGo         grammarKey = "go"
	grammarRust       grammarKey = "rust"
	grammarRS         grammarKey = "rs"
	grammarScala      grammarKey = "scala"
	grammarCSharp     grammarKey = "csharp"
	grammarCS         grammarKey = "cs"
	grammarPHP        grammarKey = "php"
	grammarPHPOnly    grammarKey = "php_only"
	grammarRuby       grammarKey = "ruby"
	grammarBash       grammarKey = "bash"
	grammarJSON       grammarKey = "json"
	grammarHTML       grammarKey = "html"
	grammarCSS        grammarKey = "css"
	grammarKotlin     grammarKey = "kotlin"
	grammarObjectiveC grammarKey = "objective-c"
	grammarDart       grammarKey = "dart"
	grammarSwift      grammarKey = "swift"
	grammarAwk        grammarKey = "awk"
	grammarPerl       grammarKey = "perl"
)

// extensionLanguages maps a file extension to a language id understood by
// GrammarForLanguage. Extensions without a grammar still appear here when a
// language name is useful to callers; GrammarForLanguage reports the miss.
var extensionLanguages = map[string]string{
	".ts":       "typescript",
	".tsx":      "typescript",
	".js":       "javascript",
	".jsx":      "javascript",
	".py":       "python",
	".java":     "java",
	".cpp":      "cpp",
	".c":        "c",
	".h":        "c",
	".hpp":      "cpp",
	".cs":       "csharp",
	".go":       "go",
	".rs":       "rust",
	".php":      "php",
	".rb":       "ruby",
	".swift":    "swift",
	".kt":       "kotlin",
	".scala":    "scala",
	".m":        "objective-c",
	".mm":       "objective-c",
	".dart":     "dart",
	".sol":      "solidity",
	".ipynb":    "jupyter",
	".md":       "markdown",
	".markdown": "markdown",
	".tex":      "latex",
	".sh":       "bash",
	".bash":     "bash",
	".json":     "json",
	".awk":      "awk",
	".pl":       "perl",
	".pm":       "perl",
	".css":      "css",
	".html":     "html",
	".htm":      "html",
	".kts":      "kotlin",
}

// GrammarForLanguage returns the tree-sitter language for a language id and
// whether a grammar is registered for it. A false second return is a normal
// result: the caller treats the input as unparseable rather than an error.
// "php" and "php_only" are two dialects of the same tree-sitter-php module:
// "php" is the tag-aware document grammar for a whole .php file, whose program
// rule treats any input before a leading <?php tag as opaque text, and
// "php_only" is the untagged dialect that parses a bare sequence of statements
// with no tag at all, the shape of a `php -r` script body.
func GrammarForLanguage(language string) (*tree_sitter.Language, bool) {
	switch grammarKey(strings.ToLower(language)) {
	case grammarJavaScript, grammarJS:
		return tree_sitter.NewLanguage(tree_sitter_javascript.Language()), true
	case grammarTypeScript, grammarTS:
		return tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()), true
	case grammarPython, grammarPy:
		return tree_sitter.NewLanguage(tree_sitter_python.Language()), true
	case grammarJava:
		return tree_sitter.NewLanguage(tree_sitter_java.Language()), true
	case grammarCPP, grammarCXX:
		return tree_sitter.NewLanguage(tree_sitter_cpp.Language()), true
	case grammarC:
		return tree_sitter.NewLanguage(tree_sitter_c.Language()), true
	case grammarGo:
		return tree_sitter.NewLanguage(tree_sitter_go.Language()), true
	case grammarRust, grammarRS:
		return tree_sitter.NewLanguage(tree_sitter_rust.Language()), true
	case grammarScala:
		return tree_sitter.NewLanguage(tree_sitter_scala.Language()), true
	case grammarCSharp, grammarCS:
		return tree_sitter.NewLanguage(tree_sitter_csharp.Language()), true
	case grammarPHP:
		return tree_sitter.NewLanguage(tree_sitter_php.LanguagePHP()), true
	case grammarPHPOnly:
		return tree_sitter.NewLanguage(tree_sitter_php.LanguagePHPOnly()), true
	case grammarRuby:
		return tree_sitter.NewLanguage(tree_sitter_ruby.Language()), true
	case grammarBash:
		return tree_sitter.NewLanguage(tree_sitter_bash.Language()), true
	case grammarJSON:
		return tree_sitter.NewLanguage(tree_sitter_json.Language()), true
	case grammarHTML:
		return tree_sitter.NewLanguage(tree_sitter_html.Language()), true
	case grammarCSS:
		return tree_sitter.NewLanguage(tree_sitter_css.Language()), true
	case grammarKotlin:
		return tree_sitter.NewLanguage(tree_sitter_kotlin.Language()), true
	case grammarObjectiveC:
		return tree_sitter.NewLanguage(tree_sitter_objc.Language()), true
	case grammarDart:
		return tree_sitter.NewLanguage(tree_sitter_dart.Language()), true
	case grammarSwift:
		return tree_sitter.NewLanguage(tree_sitter_swift.Language()), true
	case grammarAwk:
		return tree_sitter.NewLanguage(tree_sitter_awk.Language()), true
	case grammarPerl:
		return tree_sitter.NewLanguage(tree_sitter_perl.Language()), true
	default:
		return nil, false
	}
}

// LanguageForPath returns the language id for a file path based on its
// extension, or "text" when the extension is unknown.
func LanguageForPath(path string) string {
	extension := filepath.Ext(path)
	language, found := extensionLanguages[extension]
	if !found {
		return "text"
	}
	return language
}
