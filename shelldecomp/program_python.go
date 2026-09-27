package shelldecomp

import "strings"

// pythonValueFlags are the python interpreter flags that consume a following
// operand as their value rather than naming a program or a script argument. -c
// and -m select the program and are handled before this set is consulted; the
// rest (-W, -X, -I) take a value that is neither the program nor a script
// operand, so the value operand is skipped.
var pythonValueFlags = map[string]bool{
	"-W": true,
	"-X": true,
}

// pythonProgramSlot identifies how a python command supplies its program: a -c
// inline body, a -m module resolved to a local file, or a script path operand.
// argvStart is the index in cmd.Args of the first operand that follows the
// program slot, so the remaining bare operands become the program's sys.argv.
type pythonProgramSlot struct {
	inlineCode string
	hasInline  bool
	scriptPath string
	hasScript  bool
	argvStart  int
}

// pythonEmbeddings handles python and python3 as the walker's program embedder,
// returning the embeddings and true when the command is python, or nil and
// false otherwise so dispatchEmbedded falls back to the generic registry. A -c
// body becomes an inline Python embedding; a script path (or a -m local module)
// is resolved against the scope and read off disk through the resolver, with a
// cycle skipped through the visited set and a resolver miss yielding a located
// region with an empty body. Both shapes carry the trailing operands as Argv.
func (walk *walker) pythonEmbeddings(command Command, currentScope *scope) ([]Embedding, bool) {
	if command.Argv0 != "python" && command.Argv0 != "python3" {
		return nil, false
	}
	slot, found := walk.pythonProgramSlot(command, currentScope)
	if !found {
		return nil, true
	}
	argv := pythonArgv(command.Args, slot.argvStart)
	if slot.hasInline {
		return []Embedding{pythonEmbedding(slot.inlineCode, argv)}, true
	}
	body, _ := walk.readScript(slot.scriptPath)
	return []Embedding{pythonEmbedding(string(body), argv)}, true
}

// pythonProgramSlot locates the program a python command runs: the -c inline
// body, the -m module file resolved under the cwd, or the first bare resolvable
// operand as a script path. It reports false when no program can be pinned (a
// bare REPL, an unresolvable program operand, or a -m module that does not
// resolve to a local file).
func (walk *walker) pythonProgramSlot(command Command, currentScope *scope) (pythonProgramSlot, bool) {
	index := 0
	for index < len(command.Args) {
		arg := command.Args[index]
		if !strings.HasPrefix(arg.Text, "-") || arg.Text == "-" {
			break
		}
		if arg.Text == "-c" {
			return walk.inlineSlot(command.Args, index)
		}
		if arg.Text == "-m" {
			return walk.moduleSlot(command.Args, index, currentScope)
		}
		if pythonValueFlags[arg.Text] {
			index += 2
			continue
		}
		index++
	}
	return walk.scriptSlot(command.Args, index, currentScope)
}

// inlineSlot builds the program slot for python -c CODE. The body is the
// resolvable operand after -c; an absent or unresolvable value yields no slot.
func (walk *walker) inlineSlot(args []Word, flagIndex int) (pythonProgramSlot, bool) {
	valueIndex := flagIndex + 1
	if valueIndex >= len(args) || !args[valueIndex].Resolvable {
		return pythonProgramSlot{}, false
	}
	return pythonProgramSlot{
		inlineCode: args[valueIndex].Value,
		hasInline:  true,
		argvStart:  valueIndex + 1,
	}, true
}

// moduleSlot builds the program slot for python -m MODULE. A module resolves to
// a program only when its dotted name maps to a local file under the cwd that
// the resolver can read; a non-local module (the stdlib, an installed package)
// reads no project file and yields no slot, so the command is located without a
// fabricated path.
func (walk *walker) moduleSlot(args []Word, flagIndex int, currentScope *scope) (pythonProgramSlot, bool) {
	valueIndex := flagIndex + 1
	if valueIndex >= len(args) || !args[valueIndex].Resolvable {
		return pythonProgramSlot{}, false
	}
	relative := strings.ReplaceAll(args[valueIndex].Value, ".", "/") + ".py"
	resolved := currentScope.resolvePath(relative)
	if resolved == Unresolvable || resolved == "" {
		return pythonProgramSlot{}, false
	}
	if walk.resolver == nil {
		return pythonProgramSlot{}, false
	}
	if _, ok := walk.resolver(resolved); !ok {
		return pythonProgramSlot{}, false
	}
	return pythonProgramSlot{
		scriptPath: resolved,
		hasScript:  true,
		argvStart:  valueIndex + 1,
	}, true
}

// scriptSlot builds the program slot for python SCRIPT.py: the first bare
// resolvable operand at or after start, resolved against the cwd. An absent or
// unresolvable operand yields no slot rather than a fabricated path.
func (walk *walker) scriptSlot(args []Word, start int, currentScope *scope) (pythonProgramSlot, bool) {
	if start >= len(args) || !args[start].Resolvable {
		return pythonProgramSlot{}, false
	}
	resolved := currentScope.resolvePath(args[start].Value)
	if resolved == Unresolvable || resolved == "" {
		return pythonProgramSlot{}, false
	}
	return pythonProgramSlot{
		scriptPath: resolved,
		hasScript:  true,
		argvStart:  start + 1,
	}, true
}

// readScript reads a resolved script path off disk through the walker's
// resolver, recording the path in the visited set first so a self-referential
// script (one that re-invokes itself) is read once and then skipped. It returns
// nil bytes and false on a cycle, a nil resolver, or a resolver miss, leaving
// the embedding body empty so the region stays located but unparsed.
func (walk *walker) readScript(absPath string) ([]byte, bool) {
	if !walk.visited.add(absPath) {
		return nil, false
	}
	if walk.resolver == nil {
		return nil, false
	}
	content, ok := walk.resolver(absPath)
	if !ok {
		return nil, false
	}
	return content, true
}

// pythonArgv collects the bare resolvable operands at or after start as the
// program's sys.argv values. A flag or an unresolvable operand is skipped, since
// it is not a positional argument the program indexes by position.
func pythonArgv(args []Word, start int) []string {
	argv := make([]string, 0, len(args))
	for index := start; index < len(args); index++ {
		arg := args[index]
		if strings.HasPrefix(arg.Text, "-") {
			continue
		}
		if !arg.Resolvable {
			continue
		}
		argv = append(argv, arg.Value)
	}
	return argv
}

// pythonEmbedding builds a new-scope Python embedding for a program body and its
// argv. The byte span is zero because the body is reconstructed text (an inline
// -c value or off-disk script bytes), not a span of the outer command source.
func pythonEmbedding(text string, argv []string) Embedding {
	return Embedding{
		StartByte: 0,
		EndByte:   0,
		Text:      text,
		Lang:      LangPython,
		NewScope:  true,
		AsCommand: false,
		Argv:      argv,
	}
}
