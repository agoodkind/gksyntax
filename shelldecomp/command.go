package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Word is one classified token of a command line. Resolvable reports whether
// Value is a pinned literal; for an unresolvable token Value is the raw text
// and Kind is WordKindUnresolvable.
type Word struct {
	Text       string
	Resolvable bool
	Value      string
	Kind       string
}

// Command is one simple command extracted from the shell source in source
// order. Argv0 is the program name after wrapper stripping (env/sudo), Args are
// its classified operands, Cwd is the absolute directory in effect when the
// command runs or Unresolvable, Kind is its CommandKind, and Node points at the
// classified syntax node the command came from.
type Command struct {
	Argv0 string
	Args  []Word
	Cwd   string
	Kind  string
	Node  *Node
}

// ReadTarget is a file a command reads. Path is an absolute path when
// Resolvable is true, and Unresolvable or the raw token otherwise. Argv0 is the
// reading command (for example "grep" or "git grep"), Cwd is the directory the
// path was resolved against, and Raw is the original token text.
type ReadTarget struct {
	Path       string
	Resolvable bool
	Argv0      string
	Cwd        string
	Raw        string
}

// WriteTarget is a file a command writes. Path is an absolute path when
// Resolvable is true, and Unresolvable or the raw token otherwise. Argv0 is the
// writing command, Cwd is the directory the path was resolved against, and Raw
// is the original token text.
type WriteTarget struct {
	Path       string
	Resolvable bool
	Argv0      string
	Cwd        string
	Raw        string
}

// EmbeddedRegion is a span of foreign code found inside a command: a heredoc
// body, an interpreter -c script, a remote shell, or a mini-language program.
// Lang names the embedded language, Parsed holds the recursive decomposition
// when a grammar exists for that language and the depth budget allowed it (nil
// otherwise), and NewScope reports whether the region runs in a fresh cwd scope
// that does not leak back to the parent.
type EmbeddedRegion struct {
	Lang      Lang
	StartByte uint
	EndByte   uint
	Text      string
	Parsed    *Decomposition
	NewScope  bool
}

// commandKindByArgv0 maps a known program name to its CommandKind. An argv0
// absent from this map classifies as CommandKindUnknown.
var commandKindByArgv0 = map[string]string{
	"cd":     CommandKindNav,
	"pushd":  CommandKindNav,
	"popd":   CommandKindNav,
	"grep":   CommandKindSearch,
	"egrep":  CommandKindSearch,
	"fgrep":  CommandKindSearch,
	"rg":     CommandKindSearch,
	"ag":     CommandKindSearch,
	"ack":    CommandKindSearch,
	"cat":    CommandKindRead,
	"less":   CommandKindRead,
	"more":   CommandKindRead,
	"head":   CommandKindRead,
	"tail":   CommandKindRead,
	"cut":    CommandKindRead,
	"sort":   CommandKindRead,
	"uniq":   CommandKindRead,
	"wc":     CommandKindRead,
	"nl":     CommandKindRead,
	"find":   CommandKindRead,
	"tee":    CommandKindWrite,
	"dd":     CommandKindWrite,
	"cp":     CommandKindWrite,
	"mv":     CommandKindWrite,
	"rm":     CommandKindWrite,
	"touch":  CommandKindWrite,
	"mkdir":  CommandKindWrite,
	"patch":  CommandKindWrite,
	"git":    CommandKindVCS,
	"ssh":    CommandKindNetwork,
	"scp":    CommandKindNetwork,
	"rsync":  CommandKindNetwork,
	"curl":   CommandKindNetwork,
	"wget":   CommandKindNetwork,
	"python": CommandKindInterpreter,

	"python3":   CommandKindInterpreter,
	"perl":      CommandKindInterpreter,
	"node":      CommandKindInterpreter,
	"ruby":      CommandKindInterpreter,
	"osascript": CommandKindInterpreter,
	"sqlite3":   CommandKindInterpreter,
	"bash":      CommandKindInterpreter,
	"sh":        CommandKindInterpreter,
	"zsh":       CommandKindInterpreter,
	"awk":       CommandKindRead,
	"gawk":      CommandKindRead,
	"sed":       CommandKindRead,
	"jq":        CommandKindRead,
	"make":      CommandKindBuild,
	"cargo":     CommandKindBuild,
	"go":        CommandKindBuild,
	"apt":       CommandKindPkg,
	"apt-get":   CommandKindPkg,
	"brew":      CommandKindPkg,
	"npm":       CommandKindPkg,
	"pip":       CommandKindPkg,
	"pip3":      CommandKindPkg,
	"env":       CommandKindWrapper,
	"sudo":      CommandKindWrapper,
	"nice":      CommandKindWrapper,
	"timeout":   CommandKindWrapper,
	"xargs":     CommandKindWrapper,
	"nohup":     CommandKindWrapper,
	"parallel":  CommandKindWrapper,
}

// classifyArgv0 returns the CommandKind for a program name, or
// CommandKindUnknown when the name is not in the table.
func classifyArgv0(argv0 string) string {
	kind, found := commandKindByArgv0[argv0]
	if !found {
		return CommandKindUnknown
	}
	return kind
}

// rawArg is a command operand before role classification: its literal value
// when the token is a pinned literal, plus the original text and the byte span.
type rawArg struct {
	text       string
	value      string
	resolvable bool
	startByte  uint
	endByte    uint
}

// extractCommand reads a (command) node into argv0 plus raw operands, then
// strips env and sudo wrappers so argv0 is the real program. The returned
// argv0 is "" when the command name is not a literal word (for example a bare
// command substitution as the whole command).
func extractCommand(node *tree_sitter.Node, source []byte) (string, []rawArg) {
	nameNode := node.ChildByFieldName("name")
	args := make([]rawArg, 0)
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil {
			continue
		}
		if node.FieldNameForChild(uint32(index)) != "argument" {
			continue
		}
		args = append(args, resolveArgNode(child, source))
	}

	argv0 := ""
	if nameNode != nil {
		argv0 = literalArgv0(nameNode, source)
	}
	return stripWrappers(argv0, args)
}

// literalArgv0 returns the program name when the command_name holds a plain
// literal word, or "" when it holds an expansion or command substitution.
func literalArgv0(nameNode *tree_sitter.Node, source []byte) string {
	text := nameNode.Utf8Text(source)
	if containsExpansion(text) {
		return ""
	}
	for index := range nameNode.ChildCount() {
		child := nameNode.Child(index)
		if child == nil {
			continue
		}
		kind := child.Kind()
		if kind == "command_substitution" || kind == "simple_expansion" || kind == "expansion" {
			return ""
		}
	}
	return text
}

// stripWrappers removes env VAR=value prefixes and sudo flags, repeatedly, so
// the returned argv0 is the wrapped program. It stops when argv0 is no longer a
// wrapper or no operands remain.
func stripWrappers(argv0 string, args []rawArg) (string, []rawArg) {
	for {
		if argv0 == "env" {
			next := dropEnvAssignments(args)
			if next == nil {
				return argv0, args
			}
			argv0, args = promoteFirstArg(next)
			continue
		}
		if argv0 == "sudo" {
			next := dropSudoFlags(args)
			if next == nil {
				return argv0, args
			}
			argv0, args = promoteFirstArg(next)
			continue
		}
		return argv0, args
	}
}

// dropEnvAssignments returns the operands of env after its leading VAR=value
// tokens, or nil when no real program follows.
func dropEnvAssignments(args []rawArg) []rawArg {
	index := 0
	for index < len(args) && isAssignment(args[index].text) {
		index++
	}
	if index >= len(args) {
		return nil
	}
	return args[index:]
}

// dropSudoFlags returns the operands of sudo after its own flags, consuming the
// argument of -u, or nil when no real program follows.
func dropSudoFlags(args []rawArg) []rawArg {
	index := 0
	for index < len(args) {
		token := args[index].text
		if !strings.HasPrefix(token, "-") {
			break
		}
		if token == "-u" || token == "--user" {
			index += 2
			continue
		}
		index++
	}
	if index >= len(args) {
		return nil
	}
	return args[index:]
}

// promoteFirstArg treats the first operand as the new argv0 and the rest as its
// operands. The first operand becomes "" when it is not a literal.
func promoteFirstArg(args []rawArg) (string, []rawArg) {
	first := args[0]
	rest := args[1:]
	if !first.resolvable {
		return "", rest
	}
	return first.value, rest
}

// isAssignment reports whether a token is a VAR=value env assignment.
func isAssignment(token string) bool {
	equals := strings.IndexByte(token, '=')
	if equals <= 0 {
		return false
	}
	name := token[:equals]
	for _, char := range name {
		isLetter := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
		isDigit := char >= '0' && char <= '9'
		if !isLetter && !isDigit && char != '_' {
			return false
		}
	}
	return true
}
