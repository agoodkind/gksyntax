package shelldecomp

// Lang identifies the language of a decomposed command or an embedded code
// region. LangUnknown is the zero value and means no language was determined;
// LangOpaque marks a region whose language is known but has no registered
// grammar, so its body is carried as text rather than parsed.
type Lang int

// Language identifiers for commands and embedded regions. LangShell covers
// bash, sh, and zsh, which share the bash grammar here. LangOpaque is the
// terminal "located but not parsed" tag for languages with no grammar.
const (
	LangUnknown Lang = iota
	LangShell
	LangPython
	LangJavaScript
	LangRuby
	LangPerl
	LangAppleScript
	LangSQL
	LangAwk
	LangJQ
	LangSed
	LangRegex
	LangOpaque
	LangPHP
)

// String returns the lower-case name of the language, or "unknown" for an
// unrecognized value.
func (lang Lang) String() string {
	switch lang {
	case LangUnknown:
		return "unknown"
	case LangShell:
		return "shell"
	case LangPython:
		return "python"
	case LangJavaScript:
		return "javascript"
	case LangRuby:
		return "ruby"
	case LangPerl:
		return "perl"
	case LangAppleScript:
		return "applescript"
	case LangSQL:
		return "sql"
	case LangAwk:
		return "awk"
	case LangJQ:
		return "jq"
	case LangSed:
		return "sed"
	case LangRegex:
		return "regex"
	case LangOpaque:
		return "opaque"
	case LangPHP:
		return "php"
	default:
		return "unknown"
	}
}

// grammarName returns the treesitter grammar id for a language, or "" when no
// grammar is registered for it. Languages that return "" are carried as
// LangOpaque embedded regions with a nil parse. LangPHP resolves to "php_only"
// rather than the registry's "php" id: that id backs the tag-aware document
// grammar tree-sitter-php ships for whole .php files, whose program rule folds
// any input with no leading <?php tag into a single opaque text node. A
// `php -r` body is bare statements with no tag, so treesitter.GrammarForLanguage
// registers a second grammar id, "php_only", for tree-sitter-php's untagged
// dialect, which parses the same body as real statement and expression nodes.
func (lang Lang) grammarName() string {
	switch lang {
	case LangShell:
		return "bash"
	case LangPython:
		return "python"
	case LangJavaScript:
		return "javascript"
	case LangRuby:
		return "ruby"
	case LangAwk:
		return "awk"
	case LangPerl:
		return "perl"
	case LangPHP:
		return "php_only"
	case LangUnknown, LangAppleScript, LangSQL, LangJQ,
		LangSed, LangRegex, LangOpaque:
		return ""
	default:
		return ""
	}
}

// Command kind constants classify an argv0 by what it does to the filesystem
// or process tree. They are plain strings so a consumer can match them without
// importing an enum type.
const (
	// CommandKindNav is a directory-change command (cd, pushd, popd).
	CommandKindNav = "nav"
	// CommandKindRead reads files without modifying them (cat, grep, head).
	CommandKindRead = "read"
	// CommandKindWrite modifies the filesystem (tee, cp, mv, redirections).
	CommandKindWrite = "write"
	// CommandKindSearch searches file contents (grep, rg, ag); overlaps read.
	CommandKindSearch = "search"
	// CommandKindVCS is a version-control command (git).
	CommandKindVCS = "vcs"
	// CommandKindNetwork moves data across hosts (ssh, scp, curl, wget).
	CommandKindNetwork = "network"
	// CommandKindBuild runs a build tool (make, cargo, go).
	CommandKindBuild = "build"
	// CommandKindPkg manages packages (apt, brew, npm, pip).
	CommandKindPkg = "pkg"
	// CommandKindInterpreter runs code in another language (python, node, ruby).
	CommandKindInterpreter = "interpreter"
	// CommandKindWrapper prefixes another command (env, sudo, timeout, xargs).
	CommandKindWrapper = "wrapper"
	// CommandKindUnknown is the fallback for an unrecognized argv0.
	CommandKindUnknown = "unknown"
)

// Word kind constants classify a single command token by the role it plays in
// the command line.
const (
	// WordKindFlag is an option token such as -r or --include.
	WordKindFlag = "flag"
	// WordKindFlagValue is the value operand of a value-taking flag.
	WordKindFlagValue = "flagValue"
	// WordKindReadPath is a path operand the command reads.
	WordKindReadPath = "readPath"
	// WordKindWritePath is a path operand the command writes.
	WordKindWritePath = "writePath"
	// WordKindPattern is a search pattern operand (grep PATTERN).
	WordKindPattern = "pattern"
	// WordKindEmbeddedCode is a token carrying code in another language.
	WordKindEmbeddedCode = "embeddedCode"
	// WordKindLiteral is a plain resolvable literal token.
	WordKindLiteral = "literal"
	// WordKindUnresolvable is a token whose value cannot be pinned to a literal.
	WordKindUnresolvable = "unresolvable"
)

// Unresolvable is the sentinel value for a cwd or path that cannot be pinned to
// a literal, because an expansion, command substitution, or an unknown base
// directory feeds it. A consumer treats this as "do not assume a real path"
// rather than a fabricated location.
const Unresolvable = "\x00UNRESOLVABLE"

// Node is a language-tagged span of the parsed source. It mirrors the parts of
// a tree-sitter node shelldecomp needs without holding a live tree, so a
// Decomposition stays valid after its parse trees are closed. Resolvable
// reports whether Value is a pinned literal; Value is the resolved literal text
// when Resolvable is true and is empty or Unresolvable otherwise.
type Node struct {
	Lang       Lang
	Kind       string
	StartByte  uint
	EndByte    uint
	Text       string
	Resolvable bool
	Value      string
	Children   []Node
}
