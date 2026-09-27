package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonPathTraversalMethods are the pathlib.Path methods that enumerate a
// directory tree (or its entries). The walked directory is the receiver Path's
// path; whether it is a content search is decided by whether a read sink later
// consumes what the traversal yields.
var pythonPathTraversalMethods = map[string]bool{
	"rglob":   true,
	"glob":    true,
	"iterdir": true,
	"walk":    true,
}

// pythonOsWalkFuncs are the os module functions that enumerate a directory; their
// first positional argument names the directory.
var pythonOsWalkFuncs = map[string]bool{
	"walk":    true,
	"fwalk":   true,
	"scandir": true,
	"listdir": true,
}

// pythonGlobModuleFuncs are the glob module functions that expand a path pattern.
var pythonGlobModuleFuncs = map[string]bool{
	"glob":  true,
	"iglob": true,
}

// pythonTaintWrapperFuncs are built-ins that wrap an iterable without changing
// which directory its elements came from, so taint flows through them unchanged.
var pythonTaintWrapperFuncs = map[string]bool{
	"sorted":    true,
	"list":      true,
	"reversed":  true,
	"set":       true,
	"frozenset": true,
	"tuple":     true,
	"iter":      true,
	"enumerate": true,
}

// pythonBinderKinds are the node kinds that bind a value to a name or pattern, so
// the taint fixpoint only revisits those rather than the whole tree each pass.
var pythonBinderKinds = map[string]bool{
	"assignment":    true,
	"for_statement": true,
	"for_in_clause": true,
}

// pythonTaintUnionKinds are the expression kinds whose taint is the union of
// their named children's taint: a parenthesized expression, a literal
// list/set/tuple, an expression list, and a binary operator (a path join with +
// or a Path division with /).
var pythonTaintUnionKinds = map[string]bool{
	"parenthesized_expression": true,
	"list":                     true,
	"set":                      true,
	"tuple":                    true,
	"expression_list":          true,
	"binary_operator":          true,
}

// buildPathVars records, for each `name = <path>` assignment, the underlying path
// node so a later use of name resolves to that path. It captures a string literal
// directly and a Path(...) constructor by its first argument, which lets
// `root = Path(".")` then `root.rglob(...)` resolve the directory.
func (collector *pythonCollector) buildPathVars(root *tree_sitter.Node) {
	var assignments []*tree_sitter.Node
	collectKinds(root, map[string]bool{"assignment": true}, &assignments)
	for _, assignment := range assignments {
		left := assignment.ChildByFieldName("left")
		right := assignment.ChildByFieldName("right")
		if left == nil || right == nil || left.Kind() != "identifier" {
			continue
		}
		name := left.Utf8Text(collector.in.Source)
		// Last write wins, and a reassignment to a non-path expression clears the
		// binding so a later read does not resolve to a stale path.
		if pathNode := collector.pathBindingNode(right); pathNode != nil {
			collector.pathVars[name] = pathNode
		} else {
			delete(collector.pathVars, name)
		}
	}
}

// pathBindingNode returns the node naming the path a name is bound to when an
// assignment's right-hand side is a string literal or a Path(...) constructor,
// or nil for any other expression so the caller clears a stale binding.
func (collector *pythonCollector) pathBindingNode(right *tree_sitter.Node) *tree_sitter.Node {
	if _, ok := stringLiteralValue(right, collector.in.Source); ok {
		return right
	}
	if right.Kind() != "call" {
		return nil
	}
	function := right.ChildByFieldName("function")
	if function == nil || !isPathConstructor(function, collector.in.Source) {
		return nil
	}
	return positionalArg(right.ChildByFieldName("arguments"), 0)
}

// buildEnumTaint propagates directory taint from enumeration sources to the names
// they bind, iterating to a fixpoint so a value assigned to one variable and then
// iterated by another (`files = root.rglob("*"); for p in files: ...`) still
// links. Each binder taints its target with the directories its right-hand side
// traces back to; the loop ends when a pass adds nothing.
func (collector *pythonCollector) buildEnumTaint(root *tree_sitter.Node) {
	var binders []*tree_sitter.Node
	collectKinds(root, pythonBinderKinds, &binders)
	for iteration := 0; iteration <= len(binders); iteration++ {
		changed := false
		for _, binder := range binders {
			right := binder.ChildByFieldName("right")
			left := binder.ChildByFieldName("left")
			if right == nil || left == nil {
				continue
			}
			dirs := collector.taintDirs(right)
			if len(dirs) == 0 {
				continue
			}
			if collector.taintTargets(left, dirs) {
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

// taintTargets taints a binder's left side with dirs, descending tuple and list
// patterns so `for dirpath, dirnames, filenames in os.walk(d)` taints every
// unpacked name. It reports whether any taint was newly added.
func (collector *pythonCollector) taintTargets(left *tree_sitter.Node, dirs []string) bool {
	if left == nil {
		return false
	}
	if left.Kind() == "identifier" {
		return collector.addTaint(left.Utf8Text(collector.in.Source), dirs)
	}
	changed := false
	for index := range left.NamedChildCount() {
		if collector.taintTargets(left.NamedChild(index), dirs) {
			changed = true
		}
	}
	return changed
}

// addTaint merges dirs into the taint set for name and reports whether the set
// grew, which drives the fixpoint's termination.
func (collector *pythonCollector) addTaint(name string, dirs []string) bool {
	existing := collector.enumTaint[name]
	merged := mergeUnique(existing, dirs)
	if len(merged) == len(existing) {
		return false
	}
	collector.enumTaint[name] = merged
	return true
}

// taintDirs returns the enumeration directories an expression's value traces back
// to: an enumeration source resolves to its walked directory; a taint-preserving
// wrapper, os.path.join, a path join with + or /, an attribute, a subscript, or a
// tainted identifier all forward their operands' taint. Anything else returns
// nothing, so an opaque value under-approximates rather than fabricating a path.
func (collector *pythonCollector) taintDirs(node *tree_sitter.Node) []string {
	if node == nil {
		return nil
	}
	kind := node.Kind()
	if kind == "call" {
		return collector.callTaintDirs(node)
	}
	if kind == "identifier" {
		return collector.enumTaint[node.Utf8Text(collector.in.Source)]
	}
	if kind == "attribute" {
		return collector.taintDirs(node.ChildByFieldName("object"))
	}
	if kind == "subscript" {
		return collector.taintDirs(node.ChildByFieldName("value"))
	}
	if pythonTaintUnionKinds[kind] {
		merged := []string{}
		for index := range node.NamedChildCount() {
			merged = mergeUnique(merged, collector.taintDirs(node.NamedChild(index)))
		}
		return merged
	}
	return nil
}

// callTaintDirs handles the call cases of taintDirs: an enumeration source, a
// taint-preserving wrapper like sorted/list, or os.path.join over tainted parts.
func (collector *pythonCollector) callTaintDirs(node *tree_sitter.Node) []string {
	if dirs, ok := collector.sourceDir(node); ok {
		return dirs
	}
	function := node.ChildByFieldName("function")
	arguments := node.ChildByFieldName("arguments")
	if function == nil {
		return nil
	}
	if function.Kind() == "identifier" && pythonTaintWrapperFuncs[function.Utf8Text(collector.in.Source)] {
		return collector.taintDirs(positionalArg(arguments, 0))
	}
	if function.Kind() == "attribute" {
		object := function.ChildByFieldName("object")
		attribute := function.ChildByFieldName("attribute")
		if object != nil && attribute != nil &&
			attribute.Utf8Text(collector.in.Source) == "join" &&
			object.Utf8Text(collector.in.Source) == "os.path" {
			return collector.unionArgTaint(arguments)
		}
	}
	return nil
}

// unionArgTaint returns the union of the taint of every positional argument of an
// argument list, used for os.path.join(part, ...).
func (collector *pythonCollector) unionArgTaint(arguments *tree_sitter.Node) []string {
	if arguments == nil {
		return nil
	}
	merged := []string{}
	for index := range arguments.NamedChildCount() {
		child := arguments.NamedChild(index)
		if child == nil || child.Kind() == "keyword_argument" {
			continue
		}
		merged = mergeUnique(merged, collector.taintDirs(child))
	}
	return merged
}

// sourceDir reports whether a call is a directory-enumeration source and the
// directory it walks. It covers pathlib Path(...).rglob/glob/iterdir/walk, the
// os.walk family, and glob.glob/iglob. An enumeration whose directory cannot be
// resolved returns ok=false, so it under-approximates rather than guessing.
func (collector *pythonCollector) sourceDir(node *tree_sitter.Node) ([]string, bool) {
	function := node.ChildByFieldName("function")
	arguments := node.ChildByFieldName("arguments")
	if function == nil || function.Kind() != "attribute" {
		return nil, false
	}
	object := function.ChildByFieldName("object")
	attribute := function.ChildByFieldName("attribute")
	if attribute == nil {
		return nil, false
	}
	method := attribute.Utf8Text(collector.in.Source)
	objectName := ""
	if object != nil {
		objectName = object.Utf8Text(collector.in.Source)
	}
	if objectName == "glob" && pythonGlobModuleFuncs[method] {
		dirs := collector.globPatternDir(arguments)
		return dirs, len(dirs) > 0
	}
	if objectName == "os" && pythonOsWalkFuncs[method] {
		dirs := collector.resolveDirArg(positionalArg(arguments, 0))
		return dirs, len(dirs) > 0
	}
	if pythonPathTraversalMethods[method] {
		dirs := collector.resolveReceiverDir(object)
		return dirs, len(dirs) > 0
	}
	return nil, false
}

// resolveReceiverDir resolves the directory a pathlib traversal receiver names:
// an inline Path(...) constructor by its first argument, or a variable bound to a
// Path(...) via pathVars. An unresolvable receiver yields nothing.
func (collector *pythonCollector) resolveReceiverDir(object *tree_sitter.Node) []string {
	if object == nil {
		return nil
	}
	if object.Kind() == "call" {
		if arg := pathConstructorArg(object, collector.in.Source); arg != nil {
			return collector.resolvePathArg(arg)
		}
		return nil
	}
	if object.Kind() == "identifier" {
		if bound, ok := collector.pathVars[object.Utf8Text(collector.in.Source)]; ok {
			return collector.resolvePathArg(bound)
		}
	}
	return nil
}

// resolveDirArg resolves an os.walk-style directory argument: a literal or argv
// path, or a variable bound to one via pathVars.
func (collector *pythonCollector) resolveDirArg(node *tree_sitter.Node) []string {
	if node == nil {
		return nil
	}
	if resolved := collector.resolvePathArg(node); len(resolved) > 0 {
		return resolved
	}
	if node.Kind() == "identifier" {
		if bound, ok := collector.pathVars[node.Utf8Text(collector.in.Source)]; ok {
			return collector.resolvePathArg(bound)
		}
	}
	return nil
}

// globPatternDir resolves the directory a glob pattern searches: the literal text
// before the first wildcard, taken up to its last path separator, resolved
// against cwd. A non-literal pattern yields nothing.
func (collector *pythonCollector) globPatternDir(arguments *tree_sitter.Node) []string {
	pattern, ok := stringLiteralValue(positionalArg(arguments, 0), collector.in.Source)
	if !ok {
		return nil
	}
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
	scope := scope{cwd: collector.in.Cwd, homeDir: collector.in.Home}
	return []string{scope.resolvePath(dir)}
}

// fstringPrefixDir returns the directory named by an f-string's leading literal
// segment when that segment contains a path separator (f"src/{name}" -> src), so
// a read whose path is built from a fixed directory and a dynamic name surfaces
// the directory. An f-string with no literal directory prefix yields nothing.
func (collector *pythonCollector) fstringPrefixDir(node *tree_sitter.Node) []string {
	if node == nil || node.Kind() != "string" {
		return nil
	}
	var prefix strings.Builder
	sawInterpolation := false
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		kind := child.Kind()
		if kind == "interpolation" {
			sawInterpolation = true
			break
		}
		if kind == "string_content" {
			prefix.WriteString(child.Utf8Text(collector.in.Source))
		}
	}
	if !sawInterpolation {
		return nil
	}
	text := prefix.String()
	slash := strings.LastIndex(text, "/")
	if slash < 0 {
		return nil
	}
	dir := text[:slash]
	if dir == "" {
		dir = "/"
	}
	scope := scope{cwd: collector.in.Cwd, homeDir: collector.in.Home}
	return []string{scope.resolvePath(dir)}
}

// collectKinds appends every descendant of node (and node itself) whose kind is
// in kinds to out, in pre-order.
func collectKinds(node *tree_sitter.Node, kinds map[string]bool, out *[]*tree_sitter.Node) {
	if node == nil {
		return
	}
	if kinds[node.Kind()] {
		*out = append(*out, node)
	}
	for index := range node.ChildCount() {
		collectKinds(node.Child(index), kinds, out)
	}
}

// mergeUnique returns the union of two string slices, preserving order and
// dropping duplicates and empties.
func mergeUnique(existing []string, additions []string) []string {
	seen := make(map[string]bool, len(existing))
	merged := make([]string, 0, len(existing)+len(additions))
	for _, value := range existing {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		merged = append(merged, value)
	}
	for _, value := range additions {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		merged = append(merged, value)
	}
	return merged
}
