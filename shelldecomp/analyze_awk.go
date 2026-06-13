package shelldecomp

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// awkArgv0 is the argv0 label recorded on read and write targets an analyzed
// awk program produces, so a consumer can tell an awk-derived target from a
// grep or cat target.
const awkArgv0 = "awk"

// init registers the awk read analyzer so parseForeign runs it over an awk
// embedding's live tree.
func init() {
	RegisterAnalyzer(LangAwk, analyzeAwk)
}

// analyzeAwk walks an awk program's tree and returns the files it reads and
// writes through in-script I/O. A getline_file node (getline < FILE or
// getline VAR < FILE) reads its filename operand; a redirected_io_statement
// node (print/printf > FILE or >> FILE) writes its filename operand. Only a
// string-literal filename resolves to a path; a variable, an ARGV[i] index, a
// concatenation, or an escaped string is dropped rather than turned into a
// fabricated path. A "cmd" | getline pipe names a command, not a file, and is
// dropped. The awk program's file operands are surfaced separately by the
// command read scan, so this analyzer only covers I/O inside the program text.
func analyzeAwk(in AnalyzerInput) ([]ReadTarget, []WriteTarget) {
	collector := &awkCollector{in: in, reads: nil, writes: nil}
	collector.walk(in.Root)
	return collector.reads, collector.writes
}

// awkCollector accumulates the read and write targets found while walking one
// awk program tree, carrying the analyzer input needed to resolve paths.
type awkCollector struct {
	in     AnalyzerInput
	reads  []ReadTarget
	writes []WriteTarget
}

// walk descends a node and its children in pre-order, classifying each getline
// file read and each redirected print or printf write it meets.
func (collector *awkCollector) walk(node *tree_sitter.Node) {
	if node == nil {
		return
	}
	kind := node.Kind()
	if kind == "getline_file" {
		collector.classifyGetlineFile(node)
	}
	if kind == "redirected_io_statement" {
		collector.classifyRedirectedIO(node)
	}
	for index := range node.ChildCount() {
		collector.walk(node.Child(index))
	}
}

// classifyGetlineFile records the read target of a getline_file node, whose
// filename field names the file read. A getline_input node ("cmd" | getline) is
// a different kind and is never reached here, so a command pipe is not mistaken
// for a file read.
func (collector *awkCollector) classifyGetlineFile(node *tree_sitter.Node) {
	filename := node.ChildByFieldName("filename")
	collector.addRead(filename)
}

// classifyRedirectedIO records the write target of a redirected_io_statement
// node, whose filename field names the file written by the inner print or
// printf statement through > or >>.
func (collector *awkCollector) classifyRedirectedIO(node *tree_sitter.Node) {
	filename := node.ChildByFieldName("filename")
	collector.addWrite(filename)
}

// addRead resolves a filename node to a read target when it is a string
// literal, recording nothing for a variable, an ARGV[i] index, a concatenation,
// or an escaped string so no path is fabricated.
func (collector *awkCollector) addRead(node *tree_sitter.Node) {
	value, ok := awkStringLiteralValue(node, collector.in.Source)
	if !ok {
		return
	}
	scope := scope{cwd: collector.in.Cwd, homeDir: collector.in.Home}
	resolved := scope.resolvePath(value)
	collector.reads = append(collector.reads, collector.readTarget(resolved, node))
}

// addWrite resolves a filename node to a write target when it is a string
// literal, recording nothing for a non-literal target.
func (collector *awkCollector) addWrite(node *tree_sitter.Node) {
	value, ok := awkStringLiteralValue(node, collector.in.Source)
	if !ok {
		return
	}
	scope := scope{cwd: collector.in.Cwd, homeDir: collector.in.Home}
	resolved := scope.resolvePath(value)
	collector.writes = append(collector.writes, collector.writeTarget(resolved, node))
}

// readTarget builds a read target for a resolved path, with the original node
// text as Raw. A path that is empty or Unresolvable is recorded as a
// non-resolvable target rather than a fabricated one.
func (collector *awkCollector) readTarget(resolved string, node *tree_sitter.Node) ReadTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	return ReadTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      awkArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        node.Utf8Text(collector.in.Source),
	}
}

// writeTarget builds a write target for a resolved path, with the original node
// text as Raw.
func (collector *awkCollector) writeTarget(resolved string, node *tree_sitter.Node) WriteTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	return WriteTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      awkArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        node.Utf8Text(collector.in.Source),
	}
}

// awkStringLiteralValue returns the inner text of an awk string literal and
// true when the node is a plain double-quoted string: a string node whose only
// children are its two quote delimiters, with no escape_sequence and no other
// content nodes. A string carrying an escape (whose resolved bytes are
// ambiguous here), a string_concat, an array_ref, an identifier, or any other
// node returns ("", false) so a non-literal target is dropped.
func awkStringLiteralValue(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil || node.Kind() != "string" {
		return "", false
	}
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		if child.Kind() == "\"" {
			continue
		}
		return "", false
	}
	text := node.Utf8Text(source)
	if len(text) < 2 || text[0] != '"' || text[len(text)-1] != '"' {
		return "", false
	}
	return text[1 : len(text)-1], true
}
