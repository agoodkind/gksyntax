package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// perlAnalyzerArgv0 is the argv0 label recorded on read and write targets an
// analyzed perl program produces, so a consumer can tell a perl-derived target
// from a cat or grep target. It matches perlArgv0 in program_perl.go so an
// in-script open and an -n/-p operand read share the "perl" label.
const perlAnalyzerArgv0 = perlArgv0

// init registers the perl read analyzer so parseForeign runs it over a perl
// embedding's live tree.
func init() {
	RegisterAnalyzer(LangPerl, analyzePerl)
}

// analyzePerl walks a perl program's tree and returns the files it reads and
// writes through in-script open() calls. It handles the 3-arg form
// open(FH, MODE, EXPR), where MODE is a string literal selecting read ("<"),
// write (">", ">>", "+>"), or a dup ("<&", ">&") that names a descriptor rather
// than a file and is skipped, and the 2-arg form open(FH, "<file") where the
// leading mode characters are parsed off the path literal. Only a plain string
// literal path resolves to a target; a scalar variable, an interpolated string,
// or any non-literal expression is dropped rather than turned into a fabricated
// path. perl's own -n/-p file operands are surfaced separately by the command
// scan (program_perl.go), so this analyzer covers only I/O inside the program.
func analyzePerl(in AnalyzerInput) ([]ReadTarget, []WriteTarget) {
	collector := &perlCollector{in: in, reads: nil, writes: nil}
	collector.walk(in.Root)
	return collector.reads, collector.writes
}

// perlCollector accumulates the read and write targets found while walking one
// perl program tree, carrying the analyzer input needed to resolve paths.
type perlCollector struct {
	in     AnalyzerInput
	reads  []ReadTarget
	writes []WriteTarget
}

// walk descends a node and its children in pre-order, classifying each open()
// call it meets. Both the parenthesized function_call_expression and the
// paren-less ambiguous_function_call_expression forms are inspected.
func (collector *perlCollector) walk(node *tree_sitter.Node) {
	if node == nil {
		return
	}
	kind := node.Kind()
	if kind == "function_call_expression" || kind == "ambiguous_function_call_expression" {
		collector.classifyCall(node)
	}
	for index := range node.ChildCount() {
		collector.walk(node.Child(index))
	}
}

// classifyCall records the file access of an open() call. A call whose function
// is not open, or whose argument list does not hold at least a filehandle and
// one more operand, contributes nothing.
func (collector *perlCollector) classifyCall(node *tree_sitter.Node) {
	function := node.ChildByFieldName("function")
	if function == nil || function.Utf8Text(collector.in.Source) != "open" {
		return
	}
	arguments := node.ChildByFieldName("arguments")
	if arguments == nil {
		return
	}
	operands := perlListOperands(arguments)
	if len(operands) < 2 {
		return
	}
	if len(operands) >= 3 {
		collector.classifyThreeArg(operands[1], operands[2])
		return
	}
	collector.classifyTwoArg(operands[1])
}

// classifyThreeArg records open(FH, MODE, EXPR). MODE must be a string literal:
// a dup mode ("<&", ">&") names a descriptor and is skipped, a write mode opens
// a write target, and any other mode opens a read target. EXPR must be a plain
// string literal to resolve, otherwise it is dropped.
func (collector *perlCollector) classifyThreeArg(modeNode *tree_sitter.Node, pathNode *tree_sitter.Node) {
	mode, ok := perlStringLiteralValue(modeNode, collector.in.Source)
	if !ok {
		return
	}
	if isPerlDupMode(mode) {
		return
	}
	value, ok := perlStringLiteralValue(pathNode, collector.in.Source)
	if !ok {
		return
	}
	collector.record(value, pathNode, isPerlWriteMode(mode))
}

// classifyTwoArg records open(FH, "MODEPATH") where the mode characters lead the
// single path literal. It splits the leading mode prefix from the path, skips a
// dup open, and resolves the remaining path text against the cwd. A non-literal
// second operand is dropped.
func (collector *perlCollector) classifyTwoArg(pathNode *tree_sitter.Node) {
	value, ok := perlStringLiteralValue(pathNode, collector.in.Source)
	if !ok {
		return
	}
	mode, rest := splitPerlTwoArgMode(value)
	if isPerlDupMode(mode) {
		return
	}
	if rest == "" {
		return
	}
	collector.record(rest, pathNode, isPerlWriteMode(mode))
}

// record resolves a path string against the program cwd and appends a read or a
// write target labeled "perl". An empty or unresolvable resolution is recorded
// as a non-resolvable target rather than a fabricated path.
func (collector *perlCollector) record(value string, node *tree_sitter.Node, isWrite bool) {
	currentScope := scope{cwd: collector.in.Cwd, homeDir: collector.in.Home}
	resolved := currentScope.resolvePath(value)
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	raw := node.Utf8Text(collector.in.Source)
	if isWrite {
		collector.writes = append(collector.writes, WriteTarget{
			Path:       path,
			Resolvable: resolvable,
			Argv0:      perlAnalyzerArgv0,
			Cwd:        collector.in.Cwd,
			Raw:        raw,
		})
		return
	}
	collector.reads = append(collector.reads, ReadTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      perlAnalyzerArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        raw,
	})
}

// perlListOperands returns the named operand nodes of a list_expression
// argument list, skipping the comma and fat-comma separators so the positional
// index of the filehandle, mode, and path operands is stable.
func perlListOperands(arguments *tree_sitter.Node) []*tree_sitter.Node {
	operands := make([]*tree_sitter.Node, 0, arguments.NamedChildCount())
	for index := range arguments.NamedChildCount() {
		child := arguments.NamedChild(index)
		if child == nil {
			continue
		}
		operands = append(operands, child)
	}
	return operands
}

// perlStringLiteralValue returns the content of a perl string literal and true
// when the node is a plain, non-interpolated double-quoted string: an
// interpolated_string_literal whose content holds only string_content with no
// scalar, array, or escape child. An interpolated string (one whose content
// carries a scalar), a variable, or any other expression returns ("", false) so
// a non-literal target is dropped.
func perlStringLiteralValue(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil {
		return "", false
	}
	if node.Kind() != "interpolated_string_literal" {
		return "", false
	}
	content := node.ChildByFieldName("content")
	if content == nil {
		return "", false
	}
	if content.Kind() != "string_content" {
		return "", false
	}
	if content.NamedChildCount() != 0 {
		return "", false
	}
	return content.Utf8Text(source), true
}

// perlDupModes are the open modes that duplicate an existing file descriptor
// rather than open a named file, so an open with one of these modes reads or
// writes no path and is skipped.
var perlDupModes = map[string]bool{
	"<&":  true,
	">&":  true,
	"+<&": true,
	"+>&": true,
}

// isPerlDupMode reports whether a mode string is a descriptor-dup mode.
func isPerlDupMode(mode string) bool {
	return perlDupModes[mode]
}

// isPerlWriteMode reports whether a perl open mode opens for writing. The write
// modes are ">" (truncate), ">>" (append), "+>" (read/write truncate), and
// "+<" reads and writes an existing file; "+<" is treated as a read since the
// file must already exist to be read. A leading or trailing space is trimmed
// first, matching perl's tolerance of whitespace around the mode.
func isPerlWriteMode(mode string) bool {
	trimmed := strings.TrimSpace(mode)
	return trimmed == ">" || trimmed == ">>" || trimmed == "+>"
}

// splitPerlTwoArgMode splits a 2-arg open path literal into its leading mode
// characters and the remaining path. perl recognizes the mode prefixes <, >,
// >>, +<, +>, +>>, <&, and >& at the start of a 2-arg open string; a string
// with no recognized prefix opens for reading with the whole string as the
// path. Surrounding whitespace around the prefix is trimmed, matching perl.
func splitPerlTwoArgMode(value string) (string, string) {
	trimmed := strings.TrimLeft(value, " ")
	for _, prefix := range perlTwoArgModePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			rest := strings.TrimLeft(trimmed[len(prefix):], " ")
			return prefix, rest
		}
	}
	return "<", trimmed
}

// perlTwoArgModePrefixes lists the 2-arg open mode prefixes in longest-first
// order so a longer prefix (">>") is matched before its shorter prefix (">").
var perlTwoArgModePrefixes = []string{
	"+>>", "+<", "+>", ">>", "<&", ">&", "<", ">",
}
