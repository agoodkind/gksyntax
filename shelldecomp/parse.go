package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	"goodkind.io/gksyntax/treesitter"
)

// DefaultMaxDepth is the default recursion limit for parsing embedded code
// found inside a command (a -c body, a remote shell, a nested heredoc). It
// bounds the recursion so a self-referential nest cannot loop forever.
const DefaultMaxDepth = 5

// Decomposition is the parsed result of one shell command: the commands it
// runs, their read and write targets, the embedded code regions, and the cwd
// model used to resolve paths. It owns no live tree-sitter state, so it stays
// valid after Parse returns.
type Decomposition struct {
	source   []byte
	lang     Lang
	commands []Command
	assigns  []Assignment
	reads    []ReadTarget
	writes   []WriteTarget
	embedded []EmbeddedRegion
	cwdSpans []cwdSpan
	opaque   bool
}

// cwdSpan records the cwd in effect from a byte offset onward within a scope.
// EffectiveCwdAt finds the last span whose start is at or before a query
// offset. Spans are appended in source order as cd commands change the cwd.
type cwdSpan struct {
	startByte uint
	cwd       string
}

// walker carries the parse state threaded through the recursive tree walk: the
// source bytes, the remaining depth budget, the accumulating result, the
// optional resolver that reads an interpreter script off disk, and the visited
// set that stops a self-referential script from looping.
type walker struct {
	source      []byte
	homeDir     string
	depth       int
	result      *Decomposition
	resolver    FileResolver
	visited     *visitedSet
	nextScopeID int
}

// Parse decomposes a shell command starting in baseCwd, using homeDir to
// expand a leading tilde. baseCwd is an absolute directory or "" when the
// starting directory is unknown. Unparseable input becomes a single Opaque
// decomposition; Parse never panics. Parse reads no files off disk; use
// ParseWithOptions with a FileResolver to analyze an interpreter's script file.
func Parse(command string, baseCwd string, homeDir string) *Decomposition {
	return parseShellOpts([]byte(command), baseCwd, homeDir, DefaultMaxDepth, nil, newVisitedSet())
}

// parseShellOpts is the depth-aware shell parser shared by Parse,
// ParseWithOptions, and the recursive embedded-code paths. depth is the
// remaining recursion budget; at zero it returns an Opaque decomposition
// without descending further. resolver, when non-nil, lets an interpreter
// script be read off disk; visited stops a self-referential script from
// looping. Both are threaded onto the walker so nested parses keep the seam.
func parseShellOpts(source []byte, baseCwd string, homeDir string, depth int, resolver FileResolver, visited *visitedSet) *Decomposition {
	result := &Decomposition{
		source:   source,
		lang:     LangShell,
		commands: nil,
		assigns:  nil,
		reads:    nil,
		writes:   nil,
		embedded: nil,
		cwdSpans: nil,
		opaque:   false,
	}
	if depth <= 0 {
		return opaqueDecomposition(source, LangShell)
	}
	if len(strings.TrimSpace(string(source))) == 0 {
		return opaqueDecomposition(source, LangShell)
	}

	grammar, ok := treesitter.GrammarForLanguage("bash")
	if !ok {
		return opaqueDecomposition(source, LangShell)
	}
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(grammar); err != nil {
		return opaqueDecomposition(source, LangShell)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return opaqueDecomposition(source, LangShell)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil || root.HasError() && root.NamedChildCount() == 0 {
		return opaqueDecomposition(source, LangShell)
	}

	currentScope := newScope(baseCwd, homeDir, 0)
	result.recordCwd(0, currentScope.cwd)
	walk := &walker{
		source:      source,
		homeDir:     homeDir,
		depth:       depth,
		result:      result,
		resolver:    resolver,
		visited:     visited,
		nextScopeID: 1,
	}
	walk.walkSequence(root, &currentScope)
	return result
}

// opaqueDecomposition builds a decomposition that holds nothing but a single
// opaque marker for the whole input, used when the source cannot be parsed or
// the depth budget is exhausted.
func opaqueDecomposition(source []byte, lang Lang) *Decomposition {
	return &Decomposition{
		source:   source,
		lang:     lang,
		commands: nil,
		assigns:  nil,
		reads:    nil,
		writes:   nil,
		embedded: nil,
		cwdSpans: nil,
		opaque:   true,
	}
}

// recordCwd appends a cwd span at a byte offset, collapsing a repeat of the
// current cwd so EffectiveCwdAt sees one entry per real change.
func (decomposition *Decomposition) recordCwd(startByte uint, cwd string) {
	count := len(decomposition.cwdSpans)
	if count > 0 && decomposition.cwdSpans[count-1].cwd == cwd {
		return
	}
	decomposition.cwdSpans = append(decomposition.cwdSpans, cwdSpan{startByte: startByte, cwd: cwd})
}

// walkSequence walks a node that holds a run of commands sharing a scope:
// program, list, and subshell bodies. It descends into the structural wrappers
// and dispatches each command in source order. Each command outside a pipeline
// is its own first stage, so it reads from its path operands rather than stdin.
func (walk *walker) walkSequence(node *tree_sitter.Node, currentScope *scope) {
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		walk.walkNode(child, currentScope, true)
	}
}

// walkNode dispatches one node by kind. Structural sequence nodes recurse in
// the same scope; a subshell starts a child scope; a redirected_statement is
// handled as its body command plus its redirects; a command is classified.
func (walk *walker) walkNode(node *tree_sitter.Node, currentScope *scope, firstStage bool) {
	kind := nodeKind(node.Kind())
	if kind == kindPipeline {
		walk.walkPipeline(node, currentScope)
		return
	}
	if kind == kindSubshell {
		childScope := currentScope.child(walk.allocateScopeID())
		walk.walkSequence(node, &childScope)
		return
	}
	if kind == kindRedirectedStatement {
		walk.walkRedirected(node, currentScope, firstStage)
		return
	}
	if kind == kindCommand {
		walk.walkCommand(node, currentScope, firstStage)
		return
	}
	if kind == kindVariableAssignment {
		walk.walkAssignment(node, currentScope)
		return
	}
	walk.walkSequence(node, currentScope)
}

// allocateScopeID returns a parse-local identifier for a child shell scope.
func (walk *walker) allocateScopeID() int {
	id := walk.nextScopeID
	walk.nextScopeID++
	return id
}

// walkPipeline walks the stages of a pipeline in the shared scope, marking
// every stage after the first as a non-first stage so a stdin-reading grep is
// recognized as having no path target.
func (walk *walker) walkPipeline(node *tree_sitter.Node, currentScope *scope) {
	stageIndex := 0
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil || !child.IsNamed() {
			continue
		}
		firstStage := stageIndex == 0
		walk.walkNode(child, currentScope, firstStage)
		stageIndex++
	}
}

// walkRedirected handles a redirected_statement: it classifies the body command
// exactly as a bare command (its cwd effect, read targets, inline writes, and
// embedded code), then records the redirects (output-redirect writes and a
// heredoc body as an embedded region). The body collection must run here too,
// because a redirected reader (grep x f > out) or writer (tee f < in) carries
// its targets on the body command, not on the redirect.
func (walk *walker) walkRedirected(node *tree_sitter.Node, currentScope *scope, firstStage bool) {
	body := node.ChildByFieldName("body")
	if body == nil {
		walk.walkSequence(node, currentScope)
		return
	}

	// tree-sitter-bash attaches a trailing redirect to the whole preceding
	// chain, so `cd X && git Y 2>&1` parses as a redirected_statement whose body
	// is the && list (or a pipeline), not a bare command. Extracting a command
	// from that compound body yields an empty argv0 and drops the inner cd and
	// git, which erases the cwd model. Recurse into a list or pipeline body so its
	// inner commands, cd effects, and targets are classified, then bind the
	// redirects to the last command, which is what the redirect attaches to.
	//
	// A group ({ ...; }) or subshell body is left to the simple-command path
	// below: its redirect applies to the whole group and is set up in the cwd
	// before the group runs, so it must resolve against the unmutated scope. The
	// simple path does not walk the group's inner cd, so the scope stays put.
	var command Command
	if kind := nodeKind(body.Kind()); kind == kindList || kind == kindPipeline {
		before := len(walk.result.commands)
		walk.walkNode(body, currentScope, firstStage)
		// Bind the redirect to the last command this body actually added, not to
		// an unrelated earlier command from the outer walk. A body that adds no
		// command (only assignments, say) leaves command zero so the redirect
		// binds to nothing rather than mis-binding.
		if len(walk.result.commands) > before {
			command = walk.result.commands[len(walk.result.commands)-1]
		}
	} else {
		argv0, args := extractCommand(body, walk.source, currentScope)
		command = walk.buildCommand(body, argv0, args, currentScope)

		if command.Kind == CommandKindNav && command.Argv0 == "cd" {
			currentScope.applyCd(command.Args)
			walk.result.recordCwd(body.StartByte(), currentScope.cwd)
		}
		walk.collectReadTargets(command, args, currentScope, firstStage)
		walk.collectInlineWrites(command, args, currentScope)
		walk.dispatchEmbedded(body, command, args, currentScope)
	}

	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		if node.FieldNameForChild(uint32(index)) != "redirect" {
			continue
		}
		walk.handleRedirect(child, command, currentScope)
	}
}

// handleRedirect classifies one redirect child of a redirected_statement. A
// file_redirect that names a destination file is a write target; a heredoc
// redirect exposes its body as an embedded region consumed by the command.
func (walk *walker) handleRedirect(node *tree_sitter.Node, command Command, currentScope *scope) {
	kind := nodeKind(node.Kind())
	if kind == kindFileRedirect {
		walk.handleFileRedirect(node, command, currentScope)
		return
	}
	if kind == kindHeredocRedirect {
		walk.handleHeredoc(node, command, currentScope)
		// A trailing output redirect (cat <<EOF >> log) parses as a file_redirect
		// nested inside the heredoc_redirect, so descend to record its write.
		for index := range node.ChildCount() {
			child := node.Child(index)
			if child == nil {
				continue
			}
			if nodeKind(child.Kind()) == kindFileRedirect {
				walk.handleFileRedirect(child, command, currentScope)
			}
		}
	}
}

// handleHeredoc exposes a heredoc body as an embedded region whose language is
// chosen by the consuming command: an interpreter (python, bash) parses the
// body as that language, and any other consumer (cat) keeps the body opaque.
// The body text is treated as data, never as live commands of the outer shell.
func (walk *walker) handleHeredoc(node *tree_sitter.Node, command Command, currentScope *scope) {
	body := childByKind(node, "heredoc_body")
	if body == nil {
		return
	}
	lang := heredocLang(command.Argv0)
	embedding := Embedding{
		StartByte: body.StartByte(),
		EndByte:   body.EndByte(),
		Text:      body.Utf8Text(walk.source),
		Lang:      lang,
		NewScope:  true,
		AsCommand: false,
	}
	region := walk.buildEmbeddedRegion(embedding, currentScope)
	walk.result.embedded = append(walk.result.embedded, region)
}

// heredocLangByArgv0 maps a heredoc-consuming command to the language of its
// body. A consumer absent from the map keeps the body opaque.
var heredocLangByArgv0 = map[string]Lang{
	"python":  LangPython,
	"python3": LangPython,
	"bash":    LangShell,
	"sh":      LangShell,
	"zsh":     LangShell,
	"node":    LangJavaScript,
	"ruby":    LangRuby,
	"perl":    LangPerl,
}

// heredocLang returns the language of a heredoc body for its consuming command,
// or LangOpaque when the consumer does not interpret the body as code.
func heredocLang(argv0 string) Lang {
	lang, found := heredocLangByArgv0[argv0]
	if !found {
		return LangOpaque
	}
	return lang
}

// childByKind returns the first child of a node with the given kind, or nil.
func childByKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		if child.Kind() == kind {
			return child
		}
	}
	return nil
}
