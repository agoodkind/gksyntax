package shelldecomp

import (
	"strconv"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonArgv0 is the argv0 label recorded on read and write targets an analyzed
// python program produces, so a consumer can tell a python-derived target from
// a grep or cat target.
const pythonArgv0 = "python"

// init registers the python read analyzer so parseForeign runs it over a python
// embedding's live tree.
func init() {
	RegisterAnalyzer(LangPython, analyzePython)
}

// pythonReadMethods are the methods that read the file their receiver names. The
// pathlib forms (read_text, read_bytes) take the path from the receiver's
// Path(...) constructor; the file-object forms (read, readline, readlines) take
// it from a receiver that is itself a path expression, which matters when that
// receiver is a variable bound to an enumerated path.
var pythonReadMethods = map[string]bool{
	"read_text":  true,
	"read_bytes": true,
	"read":       true,
	"readline":   true,
	"readlines":  true,
}

// pythonWriteMethods are the pathlib.Path methods that write the file the Path
// names.
var pythonWriteMethods = map[string]bool{
	"write_text":  true,
	"write_bytes": true,
}

// pythonSubprocessFuncs are the subprocess functions whose first argument is a
// command run as a child process. A list of string literals is reconstructed
// into a command line and recursed through the shell parser.
var pythonSubprocessFuncs = map[string]bool{
	"run":          true,
	"call":         true,
	"check_call":   true,
	"check_output": true,
	"Popen":        true,
}

// analyzePython walks a python program's tree and returns the files it reads and
// writes. It descends every call node, classifying open/io.open and pathlib
// reads and writes, fileinput reads, and subprocess/os.system child commands it
// recurses into. It resolves a path argument that is a string literal, a
// sys.argv index, or a sys.argv slice; any other argument is dropped rather than
// turned into a fabricated path. Recursion stops once the depth budget is spent.
//
// Before the walk it builds two def-use maps so the gate decision is the
// principle "does this program read the contents of files it discovered by
// walking the filesystem", not a fixed idiom list: pathVars maps a name assigned
// a string literal or a Path(...) constructor to the underlying path node, and
// enumTaint maps a name bound to a directory-enumeration result to the walked
// directory. A content-read sink whose path traces back to an enumeration then
// resolves the walked directory as the read target.
func analyzePython(in AnalyzerInput) ([]ReadTarget, []WriteTarget) {
	collector := &pythonCollector{
		in:        in,
		pathVars:  map[string]*tree_sitter.Node{},
		enumTaint: map[string][]string{},
		seenRead:  map[string]bool{},
		seenWrite: map[string]bool{},
	}
	collector.buildPathVars(in.Root)
	collector.buildEnumTaint(in.Root)
	collector.walk(in.Root)
	return collector.reads, collector.writes
}

// pythonCollector accumulates the read and write targets found while walking one
// python program tree, carrying the analyzer input needed to resolve paths and
// recurse into child commands. pathVars and enumTaint are the def-use maps the
// pre-passes build; seenRead and seenWrite dedupe targets so two sinks naming the
// same resolved path record it once.
type pythonCollector struct {
	in        AnalyzerInput
	reads     []ReadTarget
	writes    []WriteTarget
	pathVars  map[string]*tree_sitter.Node
	enumTaint map[string][]string
	seenRead  map[string]bool
	seenWrite map[string]bool
}

// walk descends a node and its children, classifying each call it meets. It is a
// plain pre-order traversal: a call's arguments are themselves walked, so a call
// nested inside another call's arguments is still seen.
func (collector *pythonCollector) walk(node *tree_sitter.Node) {
	if node == nil {
		return
	}
	if node.Kind() == "call" {
		collector.classifyCall(node)
	}
	for index := range node.ChildCount() {
		collector.walk(node.Child(index))
	}
}

// classifyCall inspects one call node and records the file access it represents.
// A bare-name call (open) is matched by its identifier; an attribute call
// (io.open, subprocess.run, Path(...).read_text) is matched by its method name
// and, where it matters, by its receiver shape.
func (collector *pythonCollector) classifyCall(node *tree_sitter.Node) {
	function := node.ChildByFieldName("function")
	arguments := node.ChildByFieldName("arguments")
	if function == nil {
		return
	}
	if function.Kind() == "identifier" {
		collector.classifyNamedCall(function.Utf8Text(collector.in.Source), arguments)
		return
	}
	if function.Kind() == "attribute" {
		collector.classifyAttributeCall(function, arguments)
	}
}

// classifyNamedCall handles a call whose function is a bare identifier: open(),
// the only built-in path opener of interest, classified by its mode argument.
func (collector *pythonCollector) classifyNamedCall(name string, arguments *tree_sitter.Node) {
	if name == "open" {
		collector.classifyOpen(arguments)
	}
}

// classifyAttributeCall handles a call whose function is an attribute access. It
// routes io.open, fileinput.input, os.system, the subprocess functions, and the
// pathlib read and write methods by the attribute name and receiver shape.
func (collector *pythonCollector) classifyAttributeCall(function *tree_sitter.Node, arguments *tree_sitter.Node) {
	object := function.ChildByFieldName("object")
	attributeNode := function.ChildByFieldName("attribute")
	if attributeNode == nil {
		return
	}
	method := attributeNode.Utf8Text(collector.in.Source)
	objectName := ""
	if object != nil {
		objectName = object.Utf8Text(collector.in.Source)
	}

	if method == "open" && objectName == "io" {
		collector.classifyOpen(arguments)
		return
	}
	if method == "input" && objectName == "fileinput" {
		collector.classifyFileinput(arguments)
		return
	}
	if method == "system" && objectName == "os" {
		collector.recurseOsSystem(arguments)
		return
	}
	if objectName == "subprocess" && pythonSubprocessFuncs[method] {
		collector.recurseSubprocess(arguments)
		return
	}
	if pythonReadMethods[method] {
		collector.classifyPathMethod(object, true)
		return
	}
	if pythonWriteMethods[method] {
		collector.classifyPathMethod(object, false)
		return
	}
	if method == "open" {
		collector.classifyPathOpen(object, arguments)
	}
}

// classifyOpen records open(p, mode) as a read or a write by its mode argument.
// The first positional argument is the path; the mode is the second positional
// or a mode= keyword. No mode means read; a mode containing w, a, x, or + is a
// write; any other mode (an r form) is a read.
func (collector *pythonCollector) classifyOpen(arguments *tree_sitter.Node) {
	if arguments == nil {
		return
	}
	pathNode := positionalArg(arguments, 0)
	if pathNode == nil {
		return
	}
	mode := openMode(arguments, collector.in.Source)
	if isWriteMode(mode) {
		collector.addWrites(pathNode)
		return
	}
	collector.addReads(pathNode)
}

// classifyPathMethod records a pathlib Path(...).read_text or .write_text style
// access. The receiver object is the Path(...) call; its constructor's first
// argument names the file. When the receiver is not a Path(...) call (a file
// object from open(), or a variable bound to an enumerated path), a read still
// resolves through the receiver expression so a `p.read_text()` whose `p` came
// from a directory walk surfaces the walked directory.
func (collector *pythonCollector) classifyPathMethod(object *tree_sitter.Node, isRead bool) {
	pathNode := pathConstructorArg(object, collector.in.Source)
	if pathNode == nil {
		if isRead {
			collector.addReads(object)
		}
		return
	}
	if isRead {
		collector.addReads(pathNode)
		return
	}
	collector.addWrites(pathNode)
}

// classifyPathOpen records a Path(...).open(mode) access, reading by default and
// writing when the mode argument requests it. When the receiver is not an inline
// Path(...) call, a read still resolves through the receiver expression, so a
// `p.open()` whose `p` is a variable bound to a Path or an enumerated walk value
// surfaces its target; a plain object.open() that resolves to nothing is ignored.
func (collector *pythonCollector) classifyPathOpen(object *tree_sitter.Node, arguments *tree_sitter.Node) {
	mode := firstStringArg(arguments, collector.in.Source)
	pathNode := pathConstructorArg(object, collector.in.Source)
	if pathNode == nil {
		if !isWriteMode(mode) {
			collector.addReads(object)
		}
		return
	}
	if isWriteMode(mode) {
		collector.addWrites(pathNode)
		return
	}
	collector.addReads(pathNode)
}

// classifyFileinput records fileinput.input() reads. With a list argument, each
// list entry is a path; with no argument, the program reads the files named in
// its argv, so every argv entry becomes a read target.
func (collector *pythonCollector) classifyFileinput(arguments *tree_sitter.Node) {
	listNode := firstPositionalList(arguments)
	if listNode == nil {
		collector.addArgvReads()
		return
	}
	for index := range listNode.NamedChildCount() {
		entry := listNode.NamedChild(index)
		if entry == nil {
			continue
		}
		collector.addReads(entry)
	}
}

// recurseOsSystem reconstructs the command line of os.system("...") from its
// string-literal argument and recurses through the shell parser, folding the
// child decomposition's reads and writes into this program's results.
func (collector *pythonCollector) recurseOsSystem(arguments *tree_sitter.Node) {
	commandLine := firstStringArg(arguments, collector.in.Source)
	if commandLine == "" {
		return
	}
	collector.recurseCommandLine(commandLine)
}

// recurseSubprocess reconstructs the command line of a subprocess call from its
// list-of-string-literals first argument and recurses through the shell parser.
// A first argument that is not a list of literals (a shell=True string, a
// variable) is dropped rather than guessed.
func (collector *pythonCollector) recurseSubprocess(arguments *tree_sitter.Node) {
	listNode := firstPositionalList(arguments)
	if listNode == nil {
		return
	}
	parts := make([]string, 0, listNode.NamedChildCount())
	for index := range listNode.NamedChildCount() {
		entry := listNode.NamedChild(index)
		if entry == nil {
			continue
		}
		value, ok := stringLiteralValue(entry, collector.in.Source)
		if !ok {
			return
		}
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return
	}
	collector.recurseCommandLine(strings.Join(parts, " "))
}

// recurseCommandLine parses a reconstructed child command line through the shell
// parser at the reduced depth, carrying the same cwd, home, resolver, and
// visited set, then folds its read and write targets into this program's
// results. The depth guard stops a runaway nest.
func (collector *pythonCollector) recurseCommandLine(commandLine string) {
	if collector.in.Depth-1 <= 0 {
		return
	}
	child := parseShellOpts(
		[]byte(commandLine),
		collector.in.Cwd,
		collector.in.Home,
		collector.in.Depth-1,
		collector.in.Resolver,
		collector.in.Visited,
	)
	collector.reads = append(collector.reads, child.ReadTargets()...)
	collector.writes = append(collector.writes, child.WriteTargets()...)
}

// addReads resolves a read-sink's path-expression node into one or more read
// targets and records them, deduped by resolved path. Resolution tries, in
// order: a literal or sys.argv path; a variable bound to a literal path; an
// f-string's literal directory prefix; and finally the directory of an
// enumeration the node's value traces back to. A node that resolves to nothing
// (an opaque expression, an unknown variable) records nothing rather than a
// fabricated path.
func (collector *pythonCollector) addReads(node *tree_sitter.Node) {
	for _, resolved := range collector.resolveReadNodes(node) {
		if collector.seenRead[resolved] {
			continue
		}
		collector.seenRead[resolved] = true
		collector.reads = append(collector.reads, collector.readTarget(resolved, node))
	}
}

// addWrites resolves a path-argument node into one or more write targets and
// records them, deduped by resolved path. Writes use literal/argv resolution
// only: a discovered file being written is an edit, not a content search the
// semantic index could answer.
func (collector *pythonCollector) addWrites(node *tree_sitter.Node) {
	for _, resolved := range collector.resolvePathArg(node) {
		if collector.seenWrite[resolved] {
			continue
		}
		collector.seenWrite[resolved] = true
		collector.writes = append(collector.writes, collector.writeTarget(resolved, node))
	}
}

// resolveReadNodes resolves a read sink's path expression into zero or more
// absolute paths, widening the literal-only resolvePathArg with the def-use maps:
// a variable bound to a literal path, an f-string's literal directory prefix, and
// the directory of an enumeration the value traces back to. The order is
// most-specific first, so a concrete file path wins over a walked directory.
func (collector *pythonCollector) resolveReadNodes(node *tree_sitter.Node) []string {
	if node == nil {
		return nil
	}
	if literal := collector.resolvePathArg(node); len(literal) > 0 {
		return literal
	}
	if node.Kind() == "identifier" {
		if bound, ok := collector.pathVars[node.Utf8Text(collector.in.Source)]; ok {
			if resolved := collector.resolvePathArg(bound); len(resolved) > 0 {
				return resolved
			}
		}
	}
	if prefix := collector.fstringPrefixDir(node); len(prefix) > 0 {
		return prefix
	}
	return collector.taintDirs(node)
}

// addArgvReads records every argv entry as a read target, used by a bare
// fileinput.input() that reads the files named on the program's command line.
func (collector *pythonCollector) addArgvReads() {
	scope := scope{cwd: collector.in.Cwd, homeDir: collector.in.Home}
	for _, value := range collector.in.Argv {
		resolved := scope.resolvePath(value)
		collector.reads = append(collector.reads, collector.readTargetRaw(resolved, value))
	}
}

// resolvePathArg resolves one path-argument node into zero or more absolute
// paths. A string literal resolves to one path; sys.argv[N] or argv[N] resolves
// to the matching argv entry; a sys.argv[1:] slice resolves to every argv entry;
// anything else resolves to nothing so no path is fabricated.
func (collector *pythonCollector) resolvePathArg(node *tree_sitter.Node) []string {
	if node == nil {
		return nil
	}
	scope := scope{cwd: collector.in.Cwd, homeDir: collector.in.Home}
	if value, ok := stringLiteralValue(node, collector.in.Source); ok {
		return []string{scope.resolvePath(value)}
	}
	if node.Kind() == "subscript" {
		return collector.resolveArgvSubscript(node, scope)
	}
	return nil
}

// resolveArgvSubscript resolves a subscript of sys.argv or argv. An integer
// index N maps to argv[N-1], skipping argv[0] which is the script name; a slice
// maps to every argv entry. A subscript of anything but sys.argv or argv, or an
// out-of-range index, resolves to nothing.
func (collector *pythonCollector) resolveArgvSubscript(node *tree_sitter.Node, scope scope) []string {
	value := node.ChildByFieldName("value")
	if value == nil || !isArgvExpression(value, collector.in.Source) {
		return nil
	}
	subscript := node.ChildByFieldName("subscript")
	if subscript == nil {
		return nil
	}
	if subscript.Kind() == "slice" {
		resolved := make([]string, 0, len(collector.in.Argv))
		for _, entry := range collector.in.Argv {
			resolved = append(resolved, scope.resolvePath(entry))
		}
		return resolved
	}
	if subscript.Kind() == "integer" {
		index, err := strconv.Atoi(subscript.Utf8Text(collector.in.Source))
		if err != nil || index < 1 || index > len(collector.in.Argv) {
			return nil
		}
		return []string{scope.resolvePath(collector.in.Argv[index-1])}
	}
	return nil
}

// readTarget builds a read target for a resolved path, with the original node
// text as Raw. A path that is empty or Unresolvable is recorded as a
// non-resolvable target rather than a fabricated one.
func (collector *pythonCollector) readTarget(resolved string, node *tree_sitter.Node) ReadTarget {
	return collector.readTargetRaw(resolved, node.Utf8Text(collector.in.Source))
}

// readTargetRaw builds a read target for a resolved path with explicit raw text.
func (collector *pythonCollector) readTargetRaw(resolved string, raw string) ReadTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	return ReadTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      pythonArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        raw,
	}
}

// writeTarget builds a write target for a resolved path, with the original node
// text as Raw.
func (collector *pythonCollector) writeTarget(resolved string, node *tree_sitter.Node) WriteTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	return WriteTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      pythonArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        node.Utf8Text(collector.in.Source),
	}
}

// positionalArg returns the nth positional argument of an argument_list,
// counting only positional operands and skipping keyword arguments and the
// surrounding punctuation. It returns nil when there is no nth positional.
func positionalArg(arguments *tree_sitter.Node, n int) *tree_sitter.Node {
	count := 0
	for index := range arguments.NamedChildCount() {
		child := arguments.NamedChild(index)
		if child == nil || child.Kind() == "keyword_argument" {
			continue
		}
		if count == n {
			return child
		}
		count++
	}
	return nil
}

// openMode returns the mode string of an open call: the second positional
// argument or a mode= keyword argument, both as string literals. An absent or
// non-literal mode returns the empty string, which classifies as a read.
func openMode(arguments *tree_sitter.Node, source []byte) string {
	if mode := positionalArg(arguments, 1); mode != nil {
		if value, ok := stringLiteralValue(mode, source); ok {
			return value
		}
	}
	for index := range arguments.NamedChildCount() {
		child := arguments.NamedChild(index)
		if child == nil || child.Kind() != "keyword_argument" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		valueNode := child.ChildByFieldName("value")
		if nameNode == nil || valueNode == nil {
			continue
		}
		if nameNode.Utf8Text(source) != "mode" {
			continue
		}
		if value, ok := stringLiteralValue(valueNode, source); ok {
			return value
		}
	}
	return ""
}

// firstStringArg returns the value of the first positional argument when it is a
// string literal, used for os.system("...") and Path(...).open("r"). A missing
// or non-literal first argument returns the empty string.
func firstStringArg(arguments *tree_sitter.Node, source []byte) string {
	if arguments == nil {
		return ""
	}
	first := positionalArg(arguments, 0)
	if first == nil {
		return ""
	}
	value, ok := stringLiteralValue(first, source)
	if !ok {
		return ""
	}
	return value
}

// firstPositionalList returns the first positional argument when it is a list
// literal, used for fileinput.input([...]) and subprocess.run([...]). A missing
// or non-list first argument returns nil.
func firstPositionalList(arguments *tree_sitter.Node) *tree_sitter.Node {
	if arguments == nil {
		return nil
	}
	first := positionalArg(arguments, 0)
	if first == nil || first.Kind() != "list" {
		return nil
	}
	return first
}

// pathConstructorArg returns the first argument of a Path(...) or pathlib.Path(...)
// constructor call when object is that call, used to find the file a pathlib
// method reads or writes. A receiver that is not a Path constructor call returns
// nil.
func pathConstructorArg(object *tree_sitter.Node, source []byte) *tree_sitter.Node {
	if object == nil || object.Kind() != "call" {
		return nil
	}
	function := object.ChildByFieldName("function")
	if function == nil || !isPathConstructor(function, source) {
		return nil
	}
	arguments := object.ChildByFieldName("arguments")
	if arguments == nil {
		return nil
	}
	return positionalArg(arguments, 0)
}

// isPathConstructor reports whether a call's function names the pathlib Path
// constructor, as the bare Path or the attribute pathlib.Path.
func isPathConstructor(function *tree_sitter.Node, source []byte) bool {
	if function.Kind() == "identifier" {
		return function.Utf8Text(source) == "Path"
	}
	if function.Kind() == "attribute" {
		attributeNode := function.ChildByFieldName("attribute")
		if attributeNode == nil {
			return false
		}
		return attributeNode.Utf8Text(source) == "Path"
	}
	return false
}

// isArgvExpression reports whether a node is the sys.argv attribute or a bare
// argv identifier, the receiver of an argv subscript.
func isArgvExpression(node *tree_sitter.Node, source []byte) bool {
	if node.Kind() == "identifier" {
		return node.Utf8Text(source) == "argv"
	}
	if node.Kind() == "attribute" {
		object := node.ChildByFieldName("object")
		attributeNode := node.ChildByFieldName("attribute")
		if object == nil || attributeNode == nil {
			return false
		}
		return object.Utf8Text(source) == "sys" && attributeNode.Utf8Text(source) == "argv"
	}
	return false
}

// stringLiteralValue returns the content of a python string literal and true
// when the node is a plain literal: a string whose only children are its
// delimiters and string_content, with no interpolation. An f-string with an
// interpolation child, or any non-string node, returns ("", false).
func stringLiteralValue(node *tree_sitter.Node, source []byte) (string, bool) {
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
		if kind == "string_start" || kind == "string_end" {
			continue
		}
		if kind == "string_content" {
			content.WriteString(child.Utf8Text(source))
			continue
		}
		return "", false
	}
	return content.String(), true
}

// isWriteMode reports whether an open mode string requests writing: a mode that
// contains w, a, or x opens for writing, and a + adds writing to a read mode.
func isWriteMode(mode string) bool {
	return strings.ContainsAny(mode, "wax+")
}
