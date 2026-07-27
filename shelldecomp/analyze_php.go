package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// phpArgv0 is the argv0 label recorded on read and write targets an analyzed
// php program produces, matching how rubyArgv0 is used in analyze_ruby.go.
const phpArgv0 = "php"

// phpWriteModePrefixWrite and phpWriteModePrefixAppend are the leading
// characters of an fopen mode argument that request writing rather than
// reading, matching the brief's "a w or a mode" rule and rubyIsWriteMode's
// narrower "begins w or a" reading in analyze_ruby.go.
const (
	phpWriteModePrefixWrite  = "w"
	phpWriteModePrefixAppend = "a"
)

// Grammar node kinds and field names the php_only dialect of tree-sitter-php
// produces for a function call, named here as declared constants rather than
// bare literals in the walk below.
const (
	phpKindFunctionCall   = "function_call_expression"
	phpKindName           = "name"
	phpKindArguments      = "arguments"
	phpKindArgument       = "argument"
	phpKindString         = "string"
	phpKindEncapsedString = "encapsed_string"
	phpKindStringContent  = "string_content"
)

// phpFuncFopen, phpFuncGlob, and phpFuncFilePutContents are the three brief
// functions whose read/write classification needs more than the bare
// function name: fopen depends on its mode argument, glob resolves to its
// literal prefix directory rather than the pattern itself, and
// file_put_contents is unconditionally a write.
const (
	phpFuncFopen           = "fopen"
	phpFuncGlob            = "glob"
	phpFuncFilePutContents = "file_put_contents"
)

// phpGlobWildcards are the pattern metacharacters PHP's glob() recognizes by
// default: *, ?, and a [...] character class. Brace alternation ({a,b}) is
// not a default PHP glob wildcard the way it is for Ruby's Dir.glob; PHP only
// expands { as GLOB_BRACE when the caller passes that flag explicitly, which
// the brief's glob(pattern) single-argument form never does, so { is left out
// of this set.
const phpGlobWildcards = "*?["

// init registers the php read analyzer so parseForeign runs it over a php
// embedding's live tree.
func init() {
	RegisterAnalyzer(LangPHP, analyzePHP)
}

// phpAlwaysReadFunctions is the set of brief-listed php functions that always
// read the file or directory named by their first argument, independent of
// any other argument. fopen, glob, and file_put_contents are handled
// separately: fopen's classification depends on its mode argument, glob
// resolves to a directory rather than its literal pattern text, and
// file_put_contents is always a write, never a read.
var phpAlwaysReadFunctions = map[string]bool{
	"file_get_contents": true,
	"file":              true,
	"readfile":          true,
	"scandir":           true,
}

// analyzePHP walks a php program's tree and returns the files it reads and
// writes. tree-sitter-php's function_call_expression node models a call's
// function and arguments as explicit fields, so this follows the
// grammar-backed tree walk of analyze_ruby.go and analyze_javascript.go
// rather than the grammarless text scan of analyze_sed.go. A path argument
// resolves only when it is a plain string literal with no interpolation and
// no escape of any kind; a variable, an interpolated double-quoted string, a
// heredoc, or any other expression the walk cannot pin to a literal is
// dropped rather than guessed, since a fabricated path would displace the
// real read target the consumer's security gate depends on.
func analyzePHP(in AnalyzerInput) ([]ReadTarget, []WriteTarget) {
	collector := &phpCollector{in: in, scope: scope{cwd: in.Cwd, homeDir: in.Home}}
	collector.walk(in.Root)
	return collector.reads, collector.writes
}

// phpCollector accumulates the read and write targets found while walking one
// php program tree, carrying the analyzer input and the cwd/home scope used
// to resolve each literal path.
type phpCollector struct {
	in     AnalyzerInput
	scope  scope
	reads  []ReadTarget
	writes []WriteTarget
}

// walk descends a node and its children in pre-order, classifying every
// function_call_expression node it meets. A call's arguments are themselves
// walked, so a call nested inside another call's arguments is still seen.
func (collector *phpCollector) walk(node *tree_sitter.Node) {
	if node == nil {
		return
	}
	if node.Kind() == phpKindFunctionCall {
		collector.classifyCall(node)
	}
	for index := range node.ChildCount() {
		collector.walk(node.Child(index))
	}
}

// classifyCall inspects one function_call_expression's function field. Only a
// call whose function is a bare name node (file_get_contents, fopen, ...) is
// classified; a call through a variable, a namespaced or qualified name, or
// any other callable expression is not one of the brief's exact forms, so it
// is left alone rather than matched by a prefix or substring.
func (collector *phpCollector) classifyCall(node *tree_sitter.Node) {
	functionNode := node.ChildByFieldName("function")
	if functionNode == nil || functionNode.Kind() != phpKindName {
		return
	}
	name := functionNode.Utf8Text(collector.in.Source)
	arguments := node.ChildByFieldName("arguments")

	switch {
	case name == phpFuncFopen:
		collector.classifyFopen(arguments)
	case name == phpFuncGlob:
		collector.addGlobRead(phpArgumentValue(phpPositionalArg(arguments, 0)))
	case name == phpFuncFilePutContents:
		collector.addWrite(phpArgumentValue(phpPositionalArg(arguments, 0)))
	case phpAlwaysReadFunctions[name]:
		collector.addRead(phpArgumentValue(phpPositionalArg(arguments, 0)))
	}
}

// classifyFopen records an fopen call as a read or a write by its mode
// argument. The first positional argument is the path. Unlike Ruby's
// File.open, PHP's fopen has no default mode: the parameter is mandatory, so
// a call with no second argument is not a genuine fopen call and is dropped
// rather than assumed to be a read. A mode that is a plain string literal is
// a write when it begins w or a, and a read otherwise (a literal beginning r,
// or any other literal). A mode argument that is present but not a
// resolvable literal, such as a variable, cannot be classified as read or
// write without guessing, so the call is dropped entirely.
func (collector *phpCollector) classifyFopen(arguments *tree_sitter.Node) {
	pathNode := phpArgumentValue(phpPositionalArg(arguments, 0))
	modeArgument := phpPositionalArg(arguments, 1)
	if modeArgument == nil {
		return
	}
	modeNode := phpArgumentValue(modeArgument)
	mode, ok := phpStringLiteralValue(modeNode, collector.in.Source)
	if !ok {
		return
	}
	if phpIsWriteMode(mode) {
		collector.addWrite(pathNode)
		return
	}
	collector.addRead(pathNode)
}

// addRead resolves a path-argument node as a plain string literal and
// records it as a read target. A node that is nil, not a string, or a string
// with any interpolation or escape records nothing.
func (collector *phpCollector) addRead(node *tree_sitter.Node) {
	value, ok := phpStringLiteralValue(node, collector.in.Source)
	if !ok {
		return
	}
	resolved := collector.scope.resolvePath(value)
	collector.reads = append(collector.reads, collector.readTarget(resolved, node))
}

// addWrite resolves a path-argument node as a plain string literal and
// records it as a write target.
func (collector *phpCollector) addWrite(node *tree_sitter.Node) {
	value, ok := phpStringLiteralValue(node, collector.in.Source)
	if !ok {
		return
	}
	resolved := collector.scope.resolvePath(value)
	collector.writes = append(collector.writes, collector.writeTarget(resolved, node))
}

// addGlobRead resolves a glob-pattern argument node as a plain string literal
// and records a read of the directory named by its literal prefix: the
// pattern text before the first wildcard character, taken to its last path
// separator. This mirrors rubyGlobPrefixDir's shape in analyze_ruby.go, since
// a program that globs an indexed directory and reads what it finds must
// still surface that directory as a read.
func (collector *phpCollector) addGlobRead(node *tree_sitter.Node) {
	value, ok := phpStringLiteralValue(node, collector.in.Source)
	if !ok {
		return
	}
	dir := phpGlobPrefixDir(value)
	resolved := collector.scope.resolvePath(dir)
	collector.reads = append(collector.reads, collector.readTarget(resolved, node))
}

// readTarget builds a read target for a resolved path, with the original
// node text as Raw. A path that is empty or Unresolvable is recorded as a
// non-resolvable target rather than a fabricated one.
func (collector *phpCollector) readTarget(resolved string, node *tree_sitter.Node) ReadTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	targetPath := resolved
	if !resolvable {
		targetPath = Unresolvable
	}
	return ReadTarget{
		Path:       targetPath,
		Resolvable: resolvable,
		Argv0:      phpArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        node.Utf8Text(collector.in.Source),
	}
}

// writeTarget builds a write target for a resolved path, with the original
// node text as Raw.
func (collector *phpCollector) writeTarget(resolved string, node *tree_sitter.Node) WriteTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	targetPath := resolved
	if !resolvable {
		targetPath = Unresolvable
	}
	return WriteTarget{
		Path:       targetPath,
		Resolvable: resolvable,
		Argv0:      phpArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        node.Utf8Text(collector.in.Source),
	}
}

// phpPositionalArg returns the nth argument node (by source position) of a
// call's arguments node. It returns nil when arguments is nil, is not an
// arguments node, or has no nth argument child; the sole non-argument shape
// arguments can hold, a variadic_placeholder for a bare f(...) spread call,
// is skipped rather than counted, since it names no positional value.
func phpPositionalArg(arguments *tree_sitter.Node, n int) *tree_sitter.Node {
	if arguments == nil || arguments.Kind() != phpKindArguments {
		return nil
	}
	count := 0
	for index := range arguments.NamedChildCount() {
		child := arguments.NamedChild(index)
		if child == nil || child.Kind() != phpKindArgument {
			continue
		}
		if count == n {
			return child
		}
		count++
	}
	return nil
}

// phpArgumentValue returns the value expression wrapped by one argument
// node: its last named child. An argument's grammar is an optional named-
// argument name, an optional reference modifier, and then the value itself,
// in that order, so the value is always the last named child regardless of
// which optional prefixes are present. It returns nil for a nil argument or
// one with no named children at all.
func phpArgumentValue(argument *tree_sitter.Node) *tree_sitter.Node {
	if argument == nil {
		return nil
	}
	count := argument.NamedChildCount()
	if count == 0 {
		return nil
	}
	return argument.NamedChild(count - 1)
}

// phpStringLiteralValue returns the content of a php string literal and true
// when the node is a plain single- or double-quoted literal with no
// interpolation and no escape of any kind. Any other node kind, including
// heredoc and nowdoc, returns ("", false): tree-sitter-php gives those their
// own "heredoc"/"nowdoc" node kinds, so they are already excluded by the kind
// check without needing a separate rule. A raw backslash anywhere in the
// string's source text also returns ("", false) rather than decoding it,
// matching rubyStringLiteralValue's rule in analyze_ruby.go: single-quoted
// PHP recognizes only \\ and \' as escapes, and double-quoted recognizes a
// wider set (\n, \t, \", \$, \xFF, ...), and both surface as an
// escape_sequence child whose php-decoded value differs from its raw source
// text. Resolving the un-decoded raw text would record a path PHP never
// actually opens, which is worse than dropping it, so any backslash drops
// the whole string. The surrounding quote token (', ", or the vestigial b'/b"
// byte-string prefix the grammar still accepts) is an anonymous node and is
// skipped without checking its kind; a named child that is not string_content
// is a variable or `{...}` interpolation and also drops the string.
func phpStringLiteralValue(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	kind := node.Kind()
	if kind != phpKindString && kind != phpKindEncapsedString {
		return "", false
	}
	if strings.ContainsRune(node.Utf8Text(source), '\\') {
		return "", false
	}
	var content strings.Builder
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil || !child.IsNamed() {
			continue
		}
		if child.Kind() != phpKindStringContent {
			return "", false
		}
		content.WriteString(child.Utf8Text(source))
	}
	return content.String(), true
}

// phpIsWriteMode reports whether an fopen mode string requests writing: the
// brief's rule is a mode beginning w or a, matching rubyIsWriteMode's
// narrower "begins w or a" rule in analyze_ruby.go rather than modeling
// every php fopen mode combination (r+, w+, x, c, ...).
func phpIsWriteMode(mode string) bool {
	return strings.HasPrefix(mode, phpWriteModePrefixWrite) || strings.HasPrefix(mode, phpWriteModePrefixAppend)
}

// phpGlobPrefixDir returns the directory a glob pattern searches: the
// literal text before the first wildcard character, taken up to its last
// path separator. A pattern with no separator before its wildcard resolves
// to ".", the cwd itself. This mirrors rubyGlobPrefixDir's logic in
// analyze_ruby.go, minus the brace-alternation wildcard: PHP's glob() only
// expands { under an explicit GLOB_BRACE flag the brief's single-argument
// glob(pattern) form never passes, so { is an ordinary literal character
// here rather than a wildcard boundary.
func phpGlobPrefixDir(pattern string) string {
	prefix := pattern
	if wildcard := strings.IndexAny(pattern, phpGlobWildcards); wildcard >= 0 {
		prefix = pattern[:wildcard]
	}
	dir := "."
	if slash := strings.LastIndex(prefix, "/"); slash >= 0 {
		dir = prefix[:slash]
		if dir == "" {
			dir = "/"
		}
	}
	return dir
}
