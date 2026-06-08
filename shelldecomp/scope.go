package shelldecomp

import (
	"path"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// scope holds the current working directory for a run of commands that share
// it. A list or pipeline mutates the enclosing scope; a subshell, a -c body, a
// remote shell, a container exec, and a heredoc-fed interpreter each get a
// fresh child scope so a cd inside does not leak to the parent. cwd is an
// absolute path, "" when unknown (no baseCwd and no cd yet), or Unresolvable
// once an unpinnable cd poisons it.
type scope struct {
	cwd     string
	homeDir string
}

// newScope returns a scope rooted at baseCwd with the given home directory for
// tilde expansion. An empty baseCwd means the starting directory is unknown.
func newScope(baseCwd string, homeDir string) scope {
	return scope{cwd: baseCwd, homeDir: homeDir}
}

// child returns a fresh scope that inherits this scope's cwd and home but whose
// later cd changes do not affect the parent.
func (currentScope *scope) child() scope {
	return scope{cwd: currentScope.cwd, homeDir: currentScope.homeDir}
}

// applyCd updates the scope cwd for a cd command. An unresolvable argument, or
// a relative argument against an unknown or already-unresolvable cwd, sets the
// cwd to Unresolvable for the rest of the scope. A cd with no path argument
// targets the home directory, matching the shell.
func (currentScope *scope) applyCd(args []Word) {
	target, found := firstNonFlag(args)
	if !found {
		currentScope.cwd = currentScope.homeDir
		return
	}
	if !target.Resolvable {
		currentScope.cwd = Unresolvable
		return
	}
	currentScope.cwd = currentScope.resolvePath(target.Value)
}

// resolvePath resolves a literal path token against the scope cwd. An absolute
// path replaces the cwd, a tilde path expands against the home directory, and a
// relative path joins onto the cwd. A relative path against an unknown or
// unresolvable cwd yields Unresolvable, since no real directory can be named.
func (currentScope *scope) resolvePath(token string) string {
	expanded := expandTilde(token, currentScope.homeDir)
	if path.IsAbs(expanded) {
		return path.Clean(expanded)
	}
	if currentScope.cwd == "" || currentScope.cwd == Unresolvable {
		return Unresolvable
	}
	return path.Clean(path.Join(currentScope.cwd, expanded))
}

// expandTilde expands a leading ~ or ~/ to the home directory. A bare ~
// becomes the home directory and ~/rest joins rest onto it. A ~user form is
// left untouched, since the target user's home is unknown here.
func expandTilde(token string, homeDir string) string {
	if token == "~" {
		if homeDir == "" {
			return token
		}
		return homeDir
	}
	if strings.HasPrefix(token, "~/") {
		if homeDir == "" {
			return token
		}
		return path.Join(homeDir, token[2:])
	}
	return token
}

// firstNonFlag returns the first operand that is not an option token, used to
// find cd's target. It reports false when every operand is a flag.
func firstNonFlag(args []Word) (Word, bool) {
	for _, word := range args {
		if !strings.HasPrefix(word.Text, "-") {
			return word, true
		}
	}
	var zero Word
	return zero, false
}

// resolveArgNode classifies a command argument node into a rawArg, deciding
// whether its value is a pinned literal. A plain word or a single-quoted
// raw_string is literal; a double-quoted string is literal only when it has no
// expansion children; any node carrying $, backtick, or a substitution is
// unresolvable.
func resolveArgNode(node *tree_sitter.Node, source []byte) rawArg {
	text := node.Utf8Text(source)
	startByte := node.StartByte()
	endByte := node.EndByte()
	value, resolvable := literalValue(node, source)
	return rawArg{
		text:       text,
		value:      value,
		resolvable: resolvable,
		startByte:  startByte,
		endByte:    endByte,
	}
}

// literalValue returns the resolved literal text of an argument node and
// whether it is fully literal. It returns ("", false) for any node that
// contains an expansion or command substitution.
func literalValue(node *tree_sitter.Node, source []byte) (string, bool) {
	text := node.Utf8Text(source)
	kind := nodeKind(node.Kind())
	if kind == kindRawString {
		return stripQuotes(text), true
	}
	if kind == kindString {
		return literalStringValue(node, source)
	}
	if isNonLiteralKind(kind) {
		return "", false
	}
	if containsExpansion(text) {
		return "", false
	}
	return text, true
}

// nonLiteralKinds are the node kinds that always carry an unresolved expansion
// or substitution, so an argument of one of these kinds is never a literal.
var nonLiteralKinds = map[nodeKind]bool{
	kindConcatenation:       true,
	kindSimpleExpansion:     true,
	kindExpansion:           true,
	kindCommandSubstitution: true,
	kindProcessSubstitution: true,
}

// isNonLiteralKind reports whether a node kind is inherently non-literal.
func isNonLiteralKind(kind nodeKind) bool {
	return nonLiteralKinds[kind]
}

// literalStringValue resolves a double-quoted string node. It is literal only
// when its sole inner content is string_content with no expansion siblings.
func literalStringValue(node *tree_sitter.Node, source []byte) (string, bool) {
	var content strings.Builder
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		kind := nodeKind(child.Kind())
		if kind == kindDoubleQuote {
			continue
		}
		if kind == kindStringContent {
			content.WriteString(child.Utf8Text(source))
			continue
		}
		return "", false
	}
	return content.String(), true
}

// stripQuotes removes a single pair of surrounding single or double quotes from
// a raw_string token, leaving the inner literal text.
func stripQuotes(text string) string {
	if len(text) >= 2 {
		first := text[0]
		last := text[len(text)-1]
		if first == last && (first == '\'' || first == '"') {
			return text[1 : len(text)-1]
		}
	}
	return text
}

// containsExpansion reports whether a token text carries an unresolved shell
// expansion: a dollar sign or a backtick command substitution. An unquoted
// word such as $dir/x or `cmd` is not a pinnable literal even though the
// grammar may represent it as a single word.
func containsExpansion(text string) bool {
	return strings.ContainsRune(text, '$') || strings.ContainsRune(text, '`')
}
