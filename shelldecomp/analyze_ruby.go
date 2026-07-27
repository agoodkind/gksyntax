package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// rubyArgv0 is the argv0 label recorded on read and write targets an analyzed
// ruby program produces, matching how sedArgv0 is used in analyze_sed.go.
const rubyArgv0 = "ruby"

// rubyWriteModePrefixWrite and rubyWriteModePrefixAppend are the leading
// characters of a File.open mode argument that request writing rather than
// reading, matching the brief's "a mode argument beginning w or a".
const (
	rubyWriteModePrefixWrite  = "w"
	rubyWriteModePrefixAppend = "a"
)

// init registers the ruby read analyzer so parseForeign runs it over a ruby
// embedding's live tree.
func init() {
	RegisterAnalyzer(LangRuby, analyzeRuby)
}

// rubyAlwaysReadMethods maps a known receiver constant to the method names on
// it that always read the file (or directory) their first argument names,
// independent of any other argument. File.open and Dir.glob are handled
// separately because their read/write classification, or their target, needs
// more than the receiver and method name.
var rubyAlwaysReadMethods = map[string]map[string]bool{
	"File": {"read": true, "readlines": true, "foreach": true},
	"IO":   {"read": true},
	"Dir":  {"entries": true, "each_child": true},
}

// analyzeRuby walks a ruby program's tree and returns the files it reads and
// writes. The tree-sitter ruby grammar is already a module dependency and
// models receiver.method calls with explicit receiver/method/arguments
// fields, so this follows the grammar-backed tree walk of analyze_python.go
// rather than the grammarless text scan of analyze_sed.go. A path argument
// resolves only when it is a plain string literal with no interpolation; a
// variable, an interpolated string, or any other expression the walk cannot
// pin to a literal is dropped rather than guessed, since a fabricated path
// would displace the real read target the consumer's security gate depends
// on.
func analyzeRuby(in AnalyzerInput) ([]ReadTarget, []WriteTarget) {
	collector := &rubyCollector{in: in, scope: scope{cwd: in.Cwd, homeDir: in.Home}}
	collector.walk(in.Root)
	return collector.reads, collector.writes
}

// rubyCollector accumulates the read and write targets found while walking
// one ruby program tree, carrying the analyzer input and the cwd/home scope
// used to resolve each literal path.
type rubyCollector struct {
	in     AnalyzerInput
	scope  scope
	reads  []ReadTarget
	writes []WriteTarget
}

// walk descends a node and its children in pre-order, classifying every call
// and element_reference node it meets. A call's arguments are themselves
// walked, so a call nested inside another call's arguments or a block body is
// still seen.
func (collector *rubyCollector) walk(node *tree_sitter.Node) {
	if node == nil {
		return
	}
	if node.Kind() == "call" {
		collector.classifyCall(node)
	}
	if node.Kind() == "element_reference" {
		collector.classifyElementReference(node)
	}
	for index := range node.ChildCount() {
		collector.walk(node.Child(index))
	}
}

// classifyCall inspects one call node's receiver constant and method name.
// Only a call whose receiver is a known constant (File, IO, Dir) is
// classified; a bare-name call or a call on a variable receiver is not one of
// the forms the brief lists, so it is left alone.
func (collector *rubyCollector) classifyCall(node *tree_sitter.Node) {
	receiverName, ok := collector.receiverConstantName(node)
	if !ok {
		return
	}
	methodNode := node.ChildByFieldName("method")
	if methodNode == nil {
		return
	}
	method := methodNode.Utf8Text(collector.in.Source)
	arguments := node.ChildByFieldName("arguments")

	switch {
	case receiverName == "File" && method == "open":
		collector.classifyFileOpen(arguments)
	case receiverName == "File" && method == "write":
		collector.addWrite(rubyPositionalArg(arguments, 0))
	case receiverName == "Dir" && method == "glob":
		collector.addGlobRead(rubyPositionalArg(arguments, 0))
	case rubyAlwaysReadMethods[receiverName][method]:
		collector.addRead(rubyPositionalArg(arguments, 0))
	}
}

// classifyElementReference handles Dir[pattern], the indexing form of
// Dir.glob. The receiver must be the Dir constant; the pattern is the second
// named child, since an element_reference's first named child is always its
// object field.
func (collector *rubyCollector) classifyElementReference(node *tree_sitter.Node) {
	object := node.ChildByFieldName("object")
	if object == nil || object.Kind() != "constant" || object.Utf8Text(collector.in.Source) != "Dir" {
		return
	}
	collector.addGlobRead(node.NamedChild(1))
}

// classifyFileOpen records a File.open call as a read or a write by its mode
// argument. The first positional argument is the path; the second, when a
// plain string literal beginning w or a, requests writing. A missing mode, a
// mode beginning r, or a mode that is not a resolvable literal all default to
// a read, matching File.open's own default mode.
func (collector *rubyCollector) classifyFileOpen(arguments *tree_sitter.Node) {
	pathNode := rubyPositionalArg(arguments, 0)
	modeNode := rubyPositionalArg(arguments, 1)
	mode, ok := rubyStringLiteralValue(modeNode, collector.in.Source)
	if ok && rubyIsWriteMode(mode) {
		collector.addWrite(pathNode)
		return
	}
	collector.addRead(pathNode)
}

// receiverConstantName returns the text of a call node's receiver and true
// when the receiver is present and is a bare constant such as File, IO, or
// Dir. A call with no receiver, or a receiver that is a variable or another
// call, returns ("", false).
func (collector *rubyCollector) receiverConstantName(node *tree_sitter.Node) (string, bool) {
	receiver := node.ChildByFieldName("receiver")
	if receiver == nil || receiver.Kind() != "constant" {
		return "", false
	}
	return receiver.Utf8Text(collector.in.Source), true
}

// addRead resolves a path-argument node as a plain string literal and records
// it as a read target. A node that is nil, not a string, or a string with any
// interpolation records nothing.
func (collector *rubyCollector) addRead(node *tree_sitter.Node) {
	value, ok := rubyStringLiteralValue(node, collector.in.Source)
	if !ok {
		return
	}
	resolved := collector.scope.resolvePath(value)
	collector.reads = append(collector.reads, collector.readTarget(resolved, node))
}

// addWrite resolves a path-argument node as a plain string literal and
// records it as a write target.
func (collector *rubyCollector) addWrite(node *tree_sitter.Node) {
	value, ok := rubyStringLiteralValue(node, collector.in.Source)
	if !ok {
		return
	}
	resolved := collector.scope.resolvePath(value)
	collector.writes = append(collector.writes, collector.writeTarget(resolved, node))
}

// addGlobRead resolves a glob-pattern argument node as a plain string literal
// and records a read of the directory named by its literal prefix: the
// pattern text before the first wildcard character, taken to its last path
// separator. This is the shape that matters most to the consumer, since a
// program that globs an indexed directory and reads what it finds must still
// surface that directory as a read.
func (collector *rubyCollector) addGlobRead(node *tree_sitter.Node) {
	value, ok := rubyStringLiteralValue(node, collector.in.Source)
	if !ok {
		return
	}
	dir := rubyGlobPrefixDir(value)
	resolved := collector.scope.resolvePath(dir)
	collector.reads = append(collector.reads, collector.readTarget(resolved, node))
}

// readTarget builds a read target for a resolved path, with the original
// node text as Raw. A path that is empty or Unresolvable is recorded as a
// non-resolvable target rather than a fabricated one.
func (collector *rubyCollector) readTarget(resolved string, node *tree_sitter.Node) ReadTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	return ReadTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      rubyArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        node.Utf8Text(collector.in.Source),
	}
}

// writeTarget builds a write target for a resolved path, with the original
// node text as Raw.
func (collector *rubyCollector) writeTarget(resolved string, node *tree_sitter.Node) WriteTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	return WriteTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      rubyArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        node.Utf8Text(collector.in.Source),
	}
}

// rubyPositionalArg returns the nth positional argument of a call's
// arguments node, counting only positional operands and skipping an implicit
// keyword hash entry (a pair node, such as encoding: "utf-8"). It returns nil
// when arguments is nil or there is no nth positional, which covers both a
// parenthesized argument_list and Ruby's parens-optional call form, since the
// grammar wraps both the same way.
func rubyPositionalArg(arguments *tree_sitter.Node, n int) *tree_sitter.Node {
	if arguments == nil {
		return nil
	}
	count := 0
	for index := range arguments.NamedChildCount() {
		child := arguments.NamedChild(index)
		if child == nil || child.Kind() == "pair" {
			continue
		}
		if count == n {
			return child
		}
		count++
	}
	return nil
}

// rubyStringLiteralValue returns the content of a ruby string literal and
// true when the node is a plain literal: a string whose content children are
// only string_content and escape_sequence, with no interpolation. A string
// with an interpolation child, or any non-string node, returns ("", false).
// The surrounding quote tokens are anonymous nodes and are skipped without
// checking their kind, since the ruby grammar does not distinguish a single
// from a double quote by kind the way analyze_python.go's string_start and
// string_end do.
func rubyStringLiteralValue(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil || node.Kind() != "string" {
		return "", false
	}
	var content strings.Builder
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		kind := child.Kind()
		if kind == "interpolation" {
			return "", false
		}
		if kind == "string_content" || kind == "escape_sequence" {
			content.WriteString(child.Utf8Text(source))
		}
	}
	return content.String(), true
}

// rubyIsWriteMode reports whether a File.open mode string requests writing:
// the brief's rule is a mode beginning w or a, narrower than a language whose
// modes are also matched by a trailing +.
func rubyIsWriteMode(mode string) bool {
	return strings.HasPrefix(mode, rubyWriteModePrefixWrite) || strings.HasPrefix(mode, rubyWriteModePrefixAppend)
}

// rubyGlobPrefixDir returns the directory a glob pattern searches: the
// literal text before the first wildcard character, taken up to its last
// path separator. A pattern with no separator before its wildcard resolves
// to ".", the cwd itself.
func rubyGlobPrefixDir(pattern string) string {
	prefix := pattern
	if wildcard := strings.IndexAny(pattern, "*?["); wildcard >= 0 {
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
