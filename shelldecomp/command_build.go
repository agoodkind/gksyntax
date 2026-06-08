package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// walkCommand classifies a bare command node: it builds the Command, applies a
// cd to the scope, records its read targets, and dispatches any embedded code.
// firstStage reports whether the command is the first stage of its pipeline,
// which decides whether a search command with no path operand reads stdin.
func (walk *walker) walkCommand(node *tree_sitter.Node, currentScope *scope, firstStage bool) {
	argv0, args := extractCommand(node, walk.source)
	command := walk.buildCommand(node, argv0, args, currentScope)

	if command.Kind == CommandKindNav && command.Argv0 == "cd" {
		currentScope.applyCd(command.Args)
		walk.result.recordCwd(node.StartByte(), currentScope.cwd)
	}

	walk.collectReadTargets(command, args, currentScope, firstStage)
	walk.collectInlineWrites(command, args, currentScope)
	walk.dispatchEmbedded(node, command, args, currentScope)
}

// buildCommand assembles a Command from the extracted argv0 and raw operands,
// classifying each operand into a Word and recording the command in the result.
func (walk *walker) buildCommand(node *tree_sitter.Node, argv0 string, args []rawArg, currentScope *scope) Command {
	words := make([]Word, 0, len(args))
	for _, arg := range args {
		words = append(words, classifyWord(arg))
	}
	classified := classifyArgv0(argv0)
	commandNode := nodeFromTreeSitter(node, walk.source, LangShell)
	command := Command{
		Argv0: argv0,
		Args:  words,
		Cwd:   currentScope.cwd,
		Kind:  classified,
		Node:  commandNode,
	}
	walk.result.commands = append(walk.result.commands, command)
	return command
}

// classifyWord turns a raw operand into a classified Word, marking a flag, an
// unresolvable token, or a plain literal. Path versus pattern roles are decided
// later by the read and write collectors, which have the command context.
func classifyWord(arg rawArg) Word {
	if !arg.resolvable {
		return Word{
			Text:       arg.text,
			Resolvable: false,
			Value:      arg.text,
			Kind:       WordKindUnresolvable,
		}
	}
	if strings.HasPrefix(arg.text, "-") {
		return Word{
			Text:       arg.text,
			Resolvable: true,
			Value:      arg.value,
			Kind:       WordKindFlag,
		}
	}
	return Word{
		Text:       arg.text,
		Resolvable: true,
		Value:      arg.value,
		Kind:       WordKindLiteral,
	}
}

// nodeFromTreeSitter snapshots a tree-sitter node into a Node value so the
// decomposition keeps a stable record after the tree is closed. It copies the
// span and text but not the children, which the classifiers do not need here.
func nodeFromTreeSitter(node *tree_sitter.Node, source []byte, lang Lang) *Node {
	return &Node{
		Lang:       lang,
		Kind:       node.Kind(),
		StartByte:  node.StartByte(),
		EndByte:    node.EndByte(),
		Text:       node.Utf8Text(source),
		Resolvable: true,
		Value:      "",
		Children:   nil,
	}
}

// valueFlagsByArgv0 lists the value-taking flags for a command so the read
// collector can skip a flag and its separate value operand. A flag joined to
// its value with = (--include=*.go) is one token and needs no entry here.
var valueFlagsByArgv0 = map[string]map[string]bool{
	"grep": {
		"-e": true, "-f": true, "-m": true, "-A": true, "-B": true,
		"-C": true, "-d": true, "--include": true, "--exclude": true,
		"--exclude-dir": true, "--color": true,
	},
	"egrep": {"-e": true, "-f": true, "-m": true},
	"rg": {
		"-e": true, "-f": true, "-m": true, "-A": true, "-B": true,
		"-C": true, "-g": true, "--glob": true, "-t": true, "--type": true,
		"--max-count": true,
	},
	"ag": {"-G": true, "--file-search-regex": true},
}

// collectReadTargets records the read targets of a read, search, or vcs-read
// command. It walks the operands, skips flags and their values, treats the
// first bare operand of a pattern-taking searcher as the pattern, and resolves
// each remaining path operand against the command cwd. A recursive searcher or
// a bare rg/ag with no path operand defaults to the cwd; a searcher that is a
// non-first pipeline stage with no path operand reads stdin and has no target.
func (walk *walker) collectReadTargets(command Command, args []rawArg, currentScope *scope, firstStage bool) {
	if !readsFiles(command) {
		return
	}
	argv0, sawGitGrep := readArgv0(command, args)
	if command.Argv0 == "git" && !sawGitGrep {
		return
	}

	scanArgs := args
	if sawGitGrep {
		scanArgs = args[1:]
	}
	scan := newReadScan(argv0, scanArgs)
	pathTokens, sawRecursive := scan.run()

	if len(pathTokens) == 0 {
		walk.defaultReadTarget(argv0, currentScope, sawRecursive, firstStage)
		return
	}
	for _, token := range pathTokens {
		walk.result.reads = append(walk.result.reads, resolveReadTarget(argv0, token, currentScope))
	}
}

// readingKinds is the set of command kinds whose commands read file paths.
var readingKinds = map[string]bool{
	CommandKindRead:   true,
	CommandKindSearch: true,
	CommandKindVCS:    true,
}

// readsFiles reports whether a command's kind makes it a reader of file paths.
func readsFiles(command Command) bool {
	return readingKinds[command.Kind]
}

// readArgv0 returns the argv0 label for a read target and whether a git
// subcommand is a grep. A git command is only a reader when its subcommand is
// grep, and then it is labeled "git grep" so a consumer can choose to skip it.
func readArgv0(command Command, args []rawArg) (string, bool) {
	if command.Argv0 != "git" {
		return command.Argv0, false
	}
	if len(args) > 0 && args[0].resolvable && args[0].value == "grep" {
		return "git grep", true
	}
	return command.Argv0, false
}

// defaultReadTarget records the implicit read target of a searcher that names
// no path. A recursive searcher or a bare rg/ag/ack targets the cwd; any other
// searcher with no path operand reads stdin and records nothing, whether it is
// a first or later pipeline stage. firstStage is accepted for symmetry with the
// caller and reserved for shapes that read the cwd only as a first stage.
func (walk *walker) defaultReadTarget(argv0 string, currentScope *scope, sawRecursive bool, firstStage bool) {
	_ = firstStage
	defaultsToCwd := sawRecursive || isBareSearcher(argv0)
	if !defaultsToCwd {
		return
	}
	walk.result.reads = append(walk.result.reads, cwdReadTarget(argv0, currentScope))
}

// bareSearchers default their target to the cwd when given no path operand: rg,
// ag, and ack recurse the current directory, and git grep searches the whole
// repository rooted at the cwd.
var bareSearchers = map[string]bool{
	"rg":       true,
	"ag":       true,
	"ack":      true,
	"git grep": true,
}

// isBareSearcher reports whether a searcher defaults its target to the cwd when
// given no path operand.
func isBareSearcher(argv0 string) bool {
	return bareSearchers[argv0]
}

// cwdReadTarget builds a read target pointing at the command cwd, used for a
// recursive or bare searcher that names no path operand.
func cwdReadTarget(argv0 string, currentScope *scope) ReadTarget {
	resolvable := currentScope.cwd != "" && currentScope.cwd != Unresolvable
	path := currentScope.cwd
	if !resolvable {
		path = Unresolvable
	}
	return ReadTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      argv0,
		Cwd:        currentScope.cwd,
		Raw:        ".",
	}
}

// resolveReadTarget resolves one path operand against the command cwd into a
// read target. An unresolvable operand or cwd yields a non-resolvable target
// with Path set to Unresolvable, never a fabricated path.
func resolveReadTarget(argv0 string, token rawArg, currentScope *scope) ReadTarget {
	if !token.resolvable {
		return ReadTarget{
			Path:       Unresolvable,
			Resolvable: false,
			Argv0:      argv0,
			Cwd:        currentScope.cwd,
			Raw:        token.text,
		}
	}
	resolved := currentScope.resolvePath(token.value)
	if resolved == Unresolvable {
		return ReadTarget{
			Path:       Unresolvable,
			Resolvable: false,
			Argv0:      argv0,
			Cwd:        currentScope.cwd,
			Raw:        token.text,
		}
	}
	return ReadTarget{
		Path:       resolved,
		Resolvable: true,
		Argv0:      argv0,
		Cwd:        currentScope.cwd,
		Raw:        token.text,
	}
}
