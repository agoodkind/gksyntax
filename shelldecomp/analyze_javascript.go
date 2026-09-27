package shelldecomp

import (
	"path"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// jsArgv0 is the argv0 label recorded on read and write targets an analyzed
// javascript program produces, matching how rubyArgv0 is used in
// analyze_ruby.go.
const jsArgv0 = "node"

// jsWriteFlagPrefixWrite and jsWriteFlagPrefixAppend are the leading
// characters of an fs.openSync flags argument that request writing rather
// than reading. This mirrors rubyIsWriteMode's narrower "begins w or a"
// rule rather than modeling every node fs flag combination (r+, wx, ax, ...),
// since the brief's own openSync example is exactly a "w" flag.
const (
	jsWriteFlagPrefixWrite  = "w"
	jsWriteFlagPrefixAppend = "a"
)

// jsModuleFs and jsModuleFsPromises are the module specifiers a destructured
// or imported read binding may name. jsModuleFsPromises is both the literal
// `import ... from "fs/promises"` source string and the synthesized module
// name requireBindingModule assigns to `require("fs").promises`, so the two
// spellings of the promise-based fs API bind identically.
const (
	jsModuleFs         = "fs"
	jsModuleFsPromises = "fs/promises"
)

// jsFsModuleNames is the set of module specifiers buildBindings recognizes.
// fs/promises is the modern spelling of fs.promises.readFile, one of the
// brief's three named read forms it says must also resolve "through a
// destructured or renamed import", so both fs and fs/promises are in scope.
var jsFsModuleNames = map[string]bool{
	jsModuleFs:         true,
	jsModuleFsPromises: true,
}

// init registers the javascript read analyzer so parseForeign runs it over a
// javascript embedding's live tree.
func init() {
	RegisterAnalyzer(LangJavaScript, analyzeJavaScript)
}

// jsAlwaysReadMethods maps a known fs receiver text (as it appears in source:
// "fs" or "fs.promises") to the method names on it that always read the file
// or directory their first argument names. fs.openSync is handled separately
// because its read/write classification needs its flags argument, not just
// the receiver and method name. The "fs" entry doubles as the canonical set
// of destructurable read functions: classifyBoundCall consults it directly so
// a bare readFileSync(...) call site, reached through
// `const { readFileSync } = require("fs")`, `import { readFileSync } from
// "fs"`, `const { readFile } = require("fs").promises`, or
// `import { readFile } from "fs/promises"`, is recognized by the same table
// as fs.readFileSync(...).
var jsAlwaysReadMethods = map[string]map[string]bool{
	"fs":          {"readFileSync": true, "readFile": true, "readdirSync": true, "readdir": true, "createReadStream": true},
	"fs.promises": {"readFile": true},
}

// jsWriteMethods maps a known fs receiver text to the method names on it that
// always write the file their first argument names.
var jsWriteMethods = map[string]map[string]bool{
	"fs": {"writeFileSync": true, "writeFile": true, "appendFileSync": true, "createWriteStream": true},
}

// analyzeJavaScript walks a javascript program's tree and returns the files
// it reads and writes. The tree-sitter javascript grammar is already a
// module dependency and models a member call's receiver and method as
// explicit object/property fields, so this follows the grammar-backed tree
// walk of analyze_python.go and analyze_ruby.go rather than the grammarless
// text scan of analyze_sed.go. A path argument resolves only when it is a
// plain string literal, a no-substitution template literal, or a path.join
// call whose every argument is one of those; a variable, a template literal
// with any substitution, or any other expression the walk cannot pin to a
// literal is dropped rather than guessed, since a fabricated path would
// displace the real read target the consumer's security gate depends on.
//
// Before the walk, buildBindings resolves the destructured and renamed
// import forms the brief calls out (`const { readFileSync } = require("fs")`,
// `import { readFileSync } from "fs"`, and their renamed variants), plus the
// fs/promises spelling of fs.promises.readFile
// (`const { readFile } = require("fs").promises`,
// `import { readFile } from "fs/promises"`, and their renamed variants),
// into a local-name-to-canonical-fs-method map, so a bare call site such as
// `readFileSync(path)` is recognized the same way `fs.readFileSync(path)` is.
// This does not verify that the local name "fs" itself came from requiring
// or importing the fs module before a direct fs.readFileSync(...) call is
// recognized, matching how analyze_python.go trusts a bare "os" or "io"
// receiver name without tracing its import.
func analyzeJavaScript(in AnalyzerInput) ([]ReadTarget, []WriteTarget) {
	collector := &jsCollector{
		in:       in,
		scope:    scope{cwd: in.Cwd, homeDir: in.Home},
		bindings: map[string]jsBinding{},
	}
	collector.buildBindings(in.Root)
	collector.walk(in.Root)
	return collector.reads, collector.writes
}

// jsCollector accumulates the read and write targets found while walking one
// javascript program tree, carrying the analyzer input, the cwd/home scope
// used to resolve each literal path, and bindings, the local-name-to-
// canonical-fs-method map buildBindings fills in before the walk starts.
type jsCollector struct {
	in       AnalyzerInput
	scope    scope
	bindings map[string]jsBinding
	reads    []ReadTarget
	writes   []WriteTarget
}

// jsBinding is one local name bound to an fs read function, together with the
// module it came from. The module is what makes the binding checkable: a name
// destructured from fs/promises exists only if fs.promises defines it, and
// checking against the whole fs table records a read at a call site that throws.
type jsBinding struct {
	canonical string
	module    string
}

// jsModuleReadMethods maps a module specifier to the read methods that module
// actually exposes, so a destructured or imported name is checked against its
// own module rather than the union of both.
var jsModuleReadMethods = map[string]map[string]bool{
	jsModuleFs:         jsAlwaysReadMethods["fs"],
	jsModuleFsPromises: jsAlwaysReadMethods["fs.promises"],
}

// buildBindings scans the whole tree for require("fs") and
// require("fs").promises destructuring assignments and `from "fs"` and
// `from "fs/promises"` named imports, recording each local name a read
// method is bound to. It runs as a full pre-pass, before the call-site walk,
// so a binding is known regardless of whether its declaration happens to
// appear before or after the call site in source order.
func (collector *jsCollector) buildBindings(root *tree_sitter.Node) {
	var declarators []*tree_sitter.Node
	collectKinds(root, map[string]bool{"variable_declarator": true}, &declarators)
	for _, declarator := range declarators {
		collector.bindRequireDestructure(declarator)
	}

	var importStatements []*tree_sitter.Node
	collectKinds(root, map[string]bool{"import_statement": true}, &importStatements)
	for _, statement := range importStatements {
		collector.bindImportDestructure(statement)
	}
}

// bindRequireDestructure records the bindings of one
// `const { readFileSync } = require("fs")`,
// `const { readFileSync: rf } = require("fs")`, or
// `const { readFile } = require("fs").promises` declarator. A declarator
// whose name is not an object pattern, or whose value does not resolve to
// the fs or fs/promises module, contributes nothing.
func (collector *jsCollector) bindRequireDestructure(declarator *tree_sitter.Node) {
	nameNode := declarator.ChildByFieldName("name")
	valueNode := declarator.ChildByFieldName("value")
	if nameNode == nil || valueNode == nil || nameNode.Kind() != "object_pattern" {
		return
	}
	moduleName, ok := collector.requireBindingModule(valueNode)
	if !ok || !jsFsModuleNames[moduleName] {
		return
	}
	for index := range nameNode.NamedChildCount() {
		collector.bindObjectPatternEntry(nameNode.NamedChild(index), moduleName)
	}
}

// bindObjectPatternEntry records one object_pattern entry as a binding. A
// shorthand entry (`readFileSync`) binds the name to itself. A pair entry
// (`readFileSync: rf`) binds the renamed local name to the canonical fs
// method name, but only when its value is a plain identifier; a default
// value or a nested pattern is not a plain rename and is dropped rather than
// guessed. Any other entry kind (a rest pattern, a default-value pattern) is
// left alone.
func (collector *jsCollector) bindObjectPatternEntry(entry *tree_sitter.Node, moduleName string) {
	if entry == nil {
		return
	}
	if entry.Kind() == "shorthand_property_identifier_pattern" {
		name := entry.Utf8Text(collector.in.Source)
		collector.bindings[name] = jsBinding{canonical: name, module: moduleName}
		return
	}
	if entry.Kind() != "pair_pattern" {
		return
	}
	keyNode := entry.ChildByFieldName("key")
	valueNode := entry.ChildByFieldName("value")
	if keyNode == nil || valueNode == nil || keyNode.Kind() != "property_identifier" {
		return
	}
	if valueNode.Kind() != "identifier" {
		return
	}
	canonical := keyNode.Utf8Text(collector.in.Source)
	localName := valueNode.Utf8Text(collector.in.Source)
	collector.bindings[localName] = jsBinding{canonical: canonical, module: moduleName}
}

// requireModuleName returns the module name of a require(...) call whose
// sole argument is a plain string literal, and ("", false) for anything
// else: a node that is not a call_expression, a call to something other
// than the bare identifier require, or a call whose argument is not a
// resolvable literal.
func (collector *jsCollector) requireModuleName(node *tree_sitter.Node) (string, bool) {
	if node == nil || node.Kind() != "call_expression" {
		return "", false
	}
	function := node.ChildByFieldName("function")
	if function == nil || function.Kind() != "identifier" || function.Utf8Text(collector.in.Source) != "require" {
		return "", false
	}
	arguments := node.ChildByFieldName("arguments")
	return jsStringLiteralValue(jsPositionalArg(arguments, 0), collector.in.Source)
}

// requireBindingModule resolves the module name a variable_declarator's
// value expression destructures from: a plain require("fs") call resolves
// to "fs", and a require("fs").promises member expression resolves to the
// synthesized name "fs/promises", so
// `const { readFile } = require("fs").promises` binds the same way
// `import { readFile } from "fs/promises"` does. Any other expression
// (a variable, a call to something other than require, a member access
// other than .promises) returns ("", false).
func (collector *jsCollector) requireBindingModule(node *tree_sitter.Node) (string, bool) {
	if moduleName, ok := collector.requireModuleName(node); ok {
		return moduleName, true
	}
	if node == nil || node.Kind() != "member_expression" {
		return "", false
	}
	object := node.ChildByFieldName("object")
	property := node.ChildByFieldName("property")
	if object == nil || property == nil || property.Utf8Text(collector.in.Source) != "promises" {
		return "", false
	}
	moduleName, ok := collector.requireModuleName(object)
	if !ok {
		return "", false
	}
	return moduleName + "/promises", true
}

// bindImportDestructure records the bindings of one
// `import { readFileSync } from "fs"`,
// `import { readFileSync as rf } from "fs"`, or
// `import { readFile } from "fs/promises"` statement. A statement whose
// source is not the literal string "fs" or "fs/promises" contributes
// nothing.
func (collector *jsCollector) bindImportDestructure(statement *tree_sitter.Node) {
	sourceNode := statement.ChildByFieldName("source")
	moduleName, ok := jsStringLiteralValue(sourceNode, collector.in.Source)
	if !ok || !jsFsModuleNames[moduleName] {
		return
	}
	var specifiers []*tree_sitter.Node
	collectKinds(statement, map[string]bool{"import_specifier": true}, &specifiers)
	for _, specifier := range specifiers {
		collector.bindImportSpecifier(specifier, moduleName)
	}
}

// bindImportSpecifier records one import_specifier's binding: the canonical
// name it imports, and the local name it binds, which is the alias when the
// specifier renames with `as` and the canonical name otherwise. A specifier
// whose name is not a plain identifier (the `default` keyword form, or a
// string re-export name) contributes nothing.
func (collector *jsCollector) bindImportSpecifier(specifier *tree_sitter.Node, moduleName string) {
	nameNode := specifier.ChildByFieldName("name")
	if nameNode == nil || nameNode.Kind() != "identifier" {
		return
	}
	canonical := nameNode.Utf8Text(collector.in.Source)
	localName := canonical
	if aliasNode := specifier.ChildByFieldName("alias"); aliasNode != nil {
		localName = aliasNode.Utf8Text(collector.in.Source)
	}
	collector.bindings[localName] = jsBinding{canonical: canonical, module: moduleName}
}

// walk descends a node and its children in pre-order, classifying every
// call_expression node it meets. A call's arguments are themselves walked,
// so a call nested inside another call's arguments (fs.readFileSync(path.
// join(...))) is still seen.
func (collector *jsCollector) walk(node *tree_sitter.Node) {
	if node == nil {
		return
	}
	if node.Kind() == "call_expression" {
		collector.classifyCall(node)
	}
	for index := range node.ChildCount() {
		collector.walk(node.Child(index))
	}
}

// classifyCall inspects one call_expression's function field: a bare
// identifier is checked against the destructured-import bindings, and a
// member expression (fs.readFileSync, fs.promises.readFile, ...) is checked
// against the known fs receiver/method tables.
func (collector *jsCollector) classifyCall(node *tree_sitter.Node) {
	function := node.ChildByFieldName("function")
	if function == nil {
		return
	}
	arguments := node.ChildByFieldName("arguments")
	if function.Kind() == "identifier" {
		collector.classifyBoundCall(function.Utf8Text(collector.in.Source), arguments)
		return
	}
	if function.Kind() == "member_expression" {
		collector.classifyMemberCall(function, arguments)
	}
}

// classifyBoundCall handles a call whose function is a bare identifier,
// checking it against the bindings buildBindings resolved. A name that is
// not a known fs read binding is left alone, since a bare call to an
// unrelated function is not one of the forms the brief lists.
func (collector *jsCollector) classifyBoundCall(name string, arguments *tree_sitter.Node) {
	binding, ok := collector.bindings[name]
	if !ok {
		return
	}
	// Checked against the binding's own module, not the union of both. A sync
	// method destructured from fs/promises does not exist there, so a call to it
	// throws before opening anything and must not be recorded as a read.
	if jsModuleReadMethods[binding.module][binding.canonical] {
		collector.addRead(jsPositionalArg(arguments, 0))
	}
}

// classifyMemberCall handles a call whose function is a member expression,
// routing fs.openSync by its flags argument and every other known fs
// receiver/method pair by the always-read and write tables.
func (collector *jsCollector) classifyMemberCall(function *tree_sitter.Node, arguments *tree_sitter.Node) {
	propertyNode := function.ChildByFieldName("property")
	if propertyNode == nil {
		return
	}
	method := propertyNode.Utf8Text(collector.in.Source)
	objectName := ""
	if object := function.ChildByFieldName("object"); object != nil {
		objectName = object.Utf8Text(collector.in.Source)
	}

	switch {
	case objectName == "fs" && method == "openSync":
		collector.classifyOpenSync(arguments)
	case jsAlwaysReadMethods[objectName][method]:
		collector.addRead(jsPositionalArg(arguments, 0))
	case jsWriteMethods[objectName][method]:
		collector.addWrite(jsPositionalArg(arguments, 0))
	}
}

// classifyOpenSync records fs.openSync(path, flags) as a write when its
// flags argument is a literal beginning w or a, matching the brief's
// fs.openSync(path, "w") example. openSync is not one of the brief's read
// forms, so a default or "r"-family flags argument records nothing rather
// than being resolved as a read. A flags argument that is present but not a
// resolvable literal cannot be classified without guessing, so the call is
// dropped entirely, matching how analyze_ruby.go drops a File.open call
// whose mode argument is a variable.
func (collector *jsCollector) classifyOpenSync(arguments *tree_sitter.Node) {
	pathNode := jsPositionalArg(arguments, 0)
	flagsNode := jsPositionalArg(arguments, 1)
	if flagsNode == nil {
		return
	}
	flags, ok := jsStringLiteralValue(flagsNode, collector.in.Source)
	if !ok {
		return
	}
	if jsIsWriteFlag(flags) {
		collector.addWrite(pathNode)
	}
}

// addRead resolves a path-argument node to a literal path and records it as
// a read target. A node that cannot be resolved records nothing.
func (collector *jsCollector) addRead(node *tree_sitter.Node) {
	value, ok := collector.resolveLiteralPath(node)
	if !ok {
		return
	}
	resolved := collector.scope.resolvePath(value)
	collector.reads = append(collector.reads, collector.readTarget(resolved, node))
}

// addWrite resolves a path-argument node to a literal path and records it as
// a write target.
func (collector *jsCollector) addWrite(node *tree_sitter.Node) {
	value, ok := collector.resolveLiteralPath(node)
	if !ok {
		return
	}
	resolved := collector.scope.resolvePath(value)
	collector.writes = append(collector.writes, collector.writeTarget(resolved, node))
}

// resolveLiteralPath resolves a path-argument node to a literal string
// value: a plain string literal, a template literal with no substitution, or
// a path.join(...) call whose every argument is one of those two forms,
// joined with [path.Join]. Any other expression, including a variable, a
// template literal with a substitution, or a path.join call with a
// non-literal argument, is dropped rather than guessed.
func (collector *jsCollector) resolveLiteralPath(node *tree_sitter.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	if node.Kind() == "string" {
		return jsStringLiteralValue(node, collector.in.Source)
	}
	if node.Kind() == "template_string" {
		return jsTemplateLiteralValue(node, collector.in.Source)
	}
	if node.Kind() == "call_expression" {
		return collector.pathJoinValue(node)
	}
	return "", false
}

// pathJoinValue resolves a path.join(...) call to the joined literal path
// when every argument is a plain string literal or a no-substitution
// template literal. A call on any receiver other than the bare identifier
// path, a call to a method other than join, or a call with any non-literal
// or spread argument, returns ("", false).
func (collector *jsCollector) pathJoinValue(node *tree_sitter.Node) (string, bool) {
	function := node.ChildByFieldName("function")
	if function == nil || function.Kind() != "member_expression" {
		return "", false
	}
	object := function.ChildByFieldName("object")
	property := function.ChildByFieldName("property")
	if object == nil || property == nil {
		return "", false
	}
	if object.Utf8Text(collector.in.Source) != "path" || property.Utf8Text(collector.in.Source) != "join" {
		return "", false
	}
	arguments := node.ChildByFieldName("arguments")
	if arguments == nil || arguments.Kind() != "arguments" {
		return "", false
	}
	parts := make([]string, 0, arguments.NamedChildCount())
	for index := range arguments.NamedChildCount() {
		child := arguments.NamedChild(index)
		value, ok := collector.pathJoinArgValue(child)
		if !ok {
			return "", false
		}
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return "", false
	}
	return path.Join(parts...), true
}

// pathJoinArgValue resolves one path.join argument node to a literal string:
// a plain string literal or a no-substitution template literal. Any other
// node, including a nested path.join call, is not resolved here, keeping
// path.join resolution to the single level the brief describes.
func (collector *jsCollector) pathJoinArgValue(node *tree_sitter.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	if node.Kind() == "string" {
		return jsStringLiteralValue(node, collector.in.Source)
	}
	if node.Kind() == "template_string" {
		return jsTemplateLiteralValue(node, collector.in.Source)
	}
	return "", false
}

// readTarget builds a read target for a resolved path, with the original
// node text as Raw. A path that is empty or Unresolvable is recorded as a
// non-resolvable target rather than a fabricated one.
func (collector *jsCollector) readTarget(resolved string, node *tree_sitter.Node) ReadTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	targetPath := resolved
	if !resolvable {
		targetPath = Unresolvable
	}
	return ReadTarget{
		Path:       targetPath,
		Resolvable: resolvable,
		Argv0:      jsArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        node.Utf8Text(collector.in.Source),
	}
}

// writeTarget builds a write target for a resolved path, with the original
// node text as Raw.
func (collector *jsCollector) writeTarget(resolved string, node *tree_sitter.Node) WriteTarget {
	resolvable := resolved != Unresolvable && resolved != ""
	targetPath := resolved
	if !resolvable {
		targetPath = Unresolvable
	}
	return WriteTarget{
		Path:       targetPath,
		Resolvable: resolvable,
		Argv0:      jsArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        node.Utf8Text(collector.in.Source),
	}
}

// jsPositionalArg returns the nth positional argument of a call's arguments
// node, skipping a spread_element rather than assigning it a positional
// index, since a spread could contribute zero or more arguments at runtime
// and counting it as one would misalign every later index. It returns nil
// when arguments is nil, is not an arguments node (a tagged template call's
// argument is a template_string, not an arguments node), or has no nth
// positional.
func jsPositionalArg(arguments *tree_sitter.Node, n int) *tree_sitter.Node {
	if arguments == nil || arguments.Kind() != "arguments" {
		return nil
	}
	count := 0
	for index := range arguments.NamedChildCount() {
		child := arguments.NamedChild(index)
		if child == nil || child.Kind() == "spread_element" {
			continue
		}
		if count == n {
			return child
		}
		count++
	}
	return nil
}

// jsStringLiteralValue returns the content of a javascript string literal
// and true when the node is a plain literal with no escape of any kind. A
// raw backslash anywhere in the string's source text returns ("", false)
// rather than decoding it, matching rubyStringLiteralValue's rule in
// analyze_ruby.go: an escape_sequence child's decoded value differs from its
// raw source text, and resolving the un-decoded raw text would record a path
// the program never actually opens, which is worse than dropping it. A
// string containing an html_character_reference child (tree-sitter-
// javascript parses entities such as &amp; inside a plain string) is
// dropped for the same reason: its raw text is not the value the program
// sees. The surrounding quote tokens are anonymous nodes and are skipped
// without checking their kind.
func jsStringLiteralValue(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil || node.Kind() != "string" {
		return "", false
	}
	if strings.ContainsRune(node.Utf8Text(source), '\\') {
		return "", false
	}
	var content strings.Builder
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		kind := child.Kind()
		if kind == "escape_sequence" || kind == "html_character_reference" {
			return "", false
		}
		if kind == "string_fragment" {
			content.WriteString(child.Utf8Text(source))
		}
	}
	return content.String(), true
}

// jsTemplateLiteralValue returns the content of a javascript template
// literal and true only when it has no substitution, matching the brief's
// "a template literal with no substitution is an ordinary literal" rule. A
// template_substitution child, an escape_sequence or html_character_
// reference child, or any raw backslash in the source text, returns
// ("", false) for the same non-decoding reasons as jsStringLiteralValue. The
// surrounding backtick tokens are anonymous nodes and are skipped without
// checking their kind.
func jsTemplateLiteralValue(node *tree_sitter.Node, source []byte) (string, bool) {
	if node == nil || node.Kind() != "template_string" {
		return "", false
	}
	if strings.ContainsRune(node.Utf8Text(source), '\\') {
		return "", false
	}
	var content strings.Builder
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		kind := child.Kind()
		if kind == "template_substitution" || kind == "escape_sequence" || kind == "html_character_reference" {
			return "", false
		}
		if kind == "string_fragment" {
			content.WriteString(child.Utf8Text(source))
		}
	}
	return content.String(), true
}

// jsIsWriteFlag reports whether an fs.openSync flags string requests
// writing: the brief's rule is a flags value beginning w or a.
func jsIsWriteFlag(flags string) bool {
	return strings.HasPrefix(flags, jsWriteFlagPrefixWrite) || strings.HasPrefix(flags, jsWriteFlagPrefixAppend)
}
