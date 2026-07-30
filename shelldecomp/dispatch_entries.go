package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	"goodkind.io/gksyntax/treesitter"
)

// init registers the built-in dispatchers for every embedder shelldecomp
// recognizes. A consumer may override any entry, or add new ones, with
// Register after this init runs.
func init() {
	registerShellEmbedders()
	registerLanguageEmbedders()
	registerMiniLanguageEmbedders()
}

// registerShellEmbedders installs dispatchers for commands that run shell code:
// the -c interpreters, ssh, xargs, find, docker, and parallel.
func registerShellEmbedders() {
	Register("bash", dispatchShellDashC)
	Register("sh", dispatchShellDashC)
	Register("zsh", dispatchShellDashC)
	Register("ssh", dispatchSSH)
	Register("xargs", dispatchXargs)
	Register("find", dispatchFindExec)
	Register("docker", dispatchDocker)
	Register("parallel", dispatchParallel)
}

// registerLanguageEmbedders installs dispatchers for interpreters that take a
// foreign-language script through a flag. python and python3 are absent: they
// flow through the walker's program handler (program_python.go), which emits an
// embedding for both a -c body and a script file read off disk, with the trailing
// operands carried as Argv so the analyzer can resolve sys.argv references.
func registerLanguageEmbedders() {
	Register("perl", dispatchPerlDashE)
	Register("node", dispatchNodeDashE)
	Register("ruby", dispatchRubyDashE)
	Register("php", dispatchPHPDashR)
	Register("osascript", dispatchOsascriptDashE)
	Register("sqlite3", dispatchSqlite3)
}

// registerMiniLanguageEmbedders installs dispatchers for the mini-languages
// whose program is an operand: awk, jq, and sed.
func registerMiniLanguageEmbedders() {
	Register("awk", dispatchAwk)
	Register("gawk", dispatchAwk)
	Register("jq", dispatchJQ)
	Register("sed", dispatchSed)
}

// dispatchShellDashC extracts a -c shell script as a new-scope shell embedding.
func dispatchShellDashC(cmd Command, source []byte) []Embedding {
	_ = source
	return flagValueEmbedding(cmd, "-c", LangShell)
}

// dispatchSSH extracts the trailing remote command of ssh as a new-scope shell
// embedding, since the remote shell runs in its own working directory.
func dispatchSSH(cmd Command, source []byte) []Embedding {
	_ = source
	return trailingEmbedding(cmd, LangShell, true, false)
}

// dispatchXargs extracts the wrapped command of xargs as a shared-scope shell
// command embedding, since xargs runs the command in the same directory.
func dispatchXargs(cmd Command, source []byte) []Embedding {
	_ = source
	return firstPositionalEmbedding(cmd, LangShell, false, true)
}

// dispatchParallel extracts the wrapped command of parallel as a shared-scope
// shell command embedding.
func dispatchParallel(cmd Command, source []byte) []Embedding {
	_ = source
	return firstPositionalEmbedding(cmd, LangShell, false, true)
}

// dispatchPerlDashE extracts a perl -e script as a Perl embedding, which has no
// registered grammar and so stays an opaque located region.
func dispatchPerlDashE(cmd Command, source []byte) []Embedding {
	_ = source
	return flagValueEmbedding(cmd, "-e", LangPerl)
}

// dispatchNodeDashE extracts a node -e or -p script as a JavaScript embedding.
func dispatchNodeDashE(cmd Command, source []byte) []Embedding {
	_ = source
	embedding := flagValueEmbedding(cmd, "-e", LangJavaScript)
	if embedding != nil {
		return embedding
	}
	return flagValueEmbedding(cmd, "-p", LangJavaScript)
}

// dispatchRubyDashE extracts a ruby -e script as a Ruby embedding.
func dispatchRubyDashE(cmd Command, source []byte) []Embedding {
	_ = source
	return flagValueEmbedding(cmd, "-e", LangRuby)
}

// dispatchPHPDashR extracts a php -r script as a PHP embedding.
func dispatchPHPDashR(cmd Command, source []byte) []Embedding {
	_ = source
	return flagValueEmbedding(cmd, "-r", LangPHP)
}

// dispatchOsascriptDashE extracts an osascript -e script as an AppleScript
// embedding, which has no grammar and stays opaque.
func dispatchOsascriptDashE(cmd Command, source []byte) []Embedding {
	_ = source
	return flagValueEmbedding(cmd, "-e", LangAppleScript)
}

// dispatchSqlite3 extracts the SQL operand of sqlite3 (the argument after the
// database path) as an SQL embedding. LangSQL has no tree-sitter grammar, but
// analyze_sql.go registers a text-scan analyzer for it, so the resulting
// region is parsed (Parsed != nil), not opaque.
func dispatchSqlite3(cmd Command, source []byte) []Embedding {
	_ = source
	positionals := make([]Word, 0, len(cmd.Args))
	for index := range cmd.Args {
		if strings.HasPrefix(cmd.Args[index].Text, "-") {
			continue
		}
		positionals = append(positionals, cmd.Args[index])
	}
	if len(positionals) < 2 {
		return nil
	}
	sql := positionals[1]
	if !sql.Resolvable {
		return nil
	}
	return []Embedding{{
		StartByte: 0,
		EndByte:   0,
		Text:      sql.Value,
		Lang:      LangSQL,
		NewScope:  true,
		AsCommand: false,
	}}
}

// dispatchAwk extracts the awk program (the first bare operand, unless it is
// supplied with -f) as an Awk embedding, which has no grammar and stays opaque.
func dispatchAwk(cmd Command, source []byte) []Embedding {
	_ = source
	index := 0
	for index < len(cmd.Args) {
		arg := cmd.Args[index]
		if !strings.HasPrefix(arg.Text, "-") {
			break
		}
		if arg.Text == "-i" && index+1 < len(cmd.Args) && cmd.Args[index+1].Text == "inplace" {
			index += 2
			continue
		}
		if arg.Text == "-f" {
			return nil
		}
		index++
	}
	if index >= len(cmd.Args) {
		return nil
	}
	program := cmd.Args[index]
	if !program.Resolvable {
		return nil
	}
	return []Embedding{{
		StartByte: 0,
		EndByte:   0,
		Text:      program.Value,
		Lang:      LangAwk,
		NewScope:  true,
		AsCommand: false,
	}}
}

// dispatchJQ extracts the jq filter (the first bare operand) as a JQ embedding,
// which has no grammar and stays opaque.
func dispatchJQ(cmd Command, source []byte) []Embedding {
	_ = source
	for index := range cmd.Args {
		arg := cmd.Args[index]
		if strings.HasPrefix(arg.Text, "-") {
			continue
		}
		if !arg.Resolvable {
			return nil
		}
		return []Embedding{{
			StartByte: 0,
			EndByte:   0,
			Text:      arg.Value,
			Lang:      LangJQ,
			NewScope:  true,
			AsCommand: false,
		}}
	}
	return nil
}

// dispatchSed extracts the sed script (the first bare operand, unless supplied
// with -e/-f) as a Sed embedding, which has no grammar and stays opaque.
func dispatchSed(cmd Command, source []byte) []Embedding {
	_ = source
	index := 0
	for index < len(cmd.Args) {
		arg := cmd.Args[index]
		if !strings.HasPrefix(arg.Text, "-") {
			break
		}
		if arg.Text == "-e" || arg.Text == "-f" {
			return nil
		}
		index++
	}
	if index >= len(cmd.Args) {
		return nil
	}
	program := cmd.Args[index]
	if !program.Resolvable {
		return nil
	}
	return []Embedding{{
		StartByte: 0,
		EndByte:   0,
		Text:      program.Value,
		Lang:      LangSed,
		NewScope:  true,
		AsCommand: false,
	}}
}

// dispatchFindExec extracts the command run by find -exec or -execdir as a
// shared-scope shell command embedding. The command spans the tokens after
// -exec up to the terminator ; or +; the {} placeholder is dropped.
func dispatchFindExec(cmd Command, source []byte) []Embedding {
	_ = source
	start := -1
	for index := range cmd.Args {
		if cmd.Args[index].Text == "-exec" || cmd.Args[index].Text == "-execdir" {
			start = index + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	parts := make([]string, 0)
	for index := start; index < len(cmd.Args); index++ {
		token := cmd.Args[index].Text
		if token == ";" || token == "\\;" || token == "+" {
			break
		}
		if token == "{}" {
			continue
		}
		if !cmd.Args[index].Resolvable {
			return nil
		}
		parts = append(parts, cmd.Args[index].Value)
	}
	if len(parts) == 0 {
		return nil
	}
	return []Embedding{{
		StartByte: 0,
		EndByte:   0,
		Text:      strings.Join(parts, " "),
		Lang:      LangShell,
		NewScope:  false,
		AsCommand: true,
	}}
}

// dispatchDocker extracts the command run inside a container by docker run or
// docker exec as a new-scope shell command embedding. The container command
// begins after the image (run) or container (exec) operand, skipping docker's
// own flags.
func dispatchDocker(cmd Command, source []byte) []Embedding {
	_ = source
	if len(cmd.Args) == 0 || !cmd.Args[0].Resolvable {
		return nil
	}
	subcommand := cmd.Args[0].Value
	if subcommand != "run" && subcommand != "exec" {
		return nil
	}
	index := skipDockerFlags(cmd.Args, 1)
	if index >= len(cmd.Args) {
		return nil
	}
	commandStart := index + 1
	if commandStart >= len(cmd.Args) {
		return nil
	}
	parts := make([]string, 0)
	for argIndex := commandStart; argIndex < len(cmd.Args); argIndex++ {
		if !cmd.Args[argIndex].Resolvable {
			return nil
		}
		parts = append(parts, cmd.Args[argIndex].Value)
	}
	if len(parts) == 0 {
		return nil
	}
	return []Embedding{{
		StartByte: 0,
		EndByte:   0,
		Text:      strings.Join(parts, " "),
		Lang:      LangShell,
		NewScope:  true,
		AsCommand: true,
	}}
}

// skipDockerFlags returns the index of the image or container operand of a
// docker run or exec command, skipping docker's own flags and their values.
func skipDockerFlags(args []Word, start int) int {
	index := start
	for index < len(args) {
		token := args[index].Text
		if !strings.HasPrefix(token, "-") {
			return index
		}
		if dockerValueFlag(token) && index+1 < len(args) {
			index += 2
			continue
		}
		index++
	}
	return index
}

// dockerValueFlags are the docker run and exec flags that take a separate value
// operand, so the image or container operand follows the flag's value.
var dockerValueFlags = map[string]bool{
	"-e": true, "-v": true, "-w": true, "-u": true, "-p": true,
	"--env": true, "--volume": true, "--workdir": true, "--user": true, "--name": true,
}

// dockerValueFlag reports whether a docker flag takes a separate value operand,
// covering the common -e, -v, -w, -u, and --env forms.
func dockerValueFlag(token string) bool {
	return dockerValueFlags[token]
}

// parseForeign parses an embedded foreign-language script with its own grammar
// into a decomposition that holds the language tag, the parse outcome, and, when
// an analyzer is registered for the language, the reads and writes the analyzer
// derives from the live tree. A nil grammar or a nil tree yields an opaque
// decomposition, never a panic. argv carries the program's trailing operands,
// cwd and homeDir resolve its paths, resolver reads a nested script off disk,
// and visited stops a self-referential nest.
func parseForeign(
	grammarName string,
	source []byte,
	lang Lang,
	depth int,
	argv []string,
	cwd string,
	homeDir string,
	resolver FileResolver,
	visited *visitedSet,
) *Decomposition {
	if depth <= 0 {
		return opaqueDecomposition(source, lang)
	}
	grammar, ok := treesitter.GrammarForLanguage(grammarName)
	if !ok {
		return opaqueDecomposition(source, lang)
	}
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(grammar); err != nil {
		return opaqueDecomposition(source, lang)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return opaqueDecomposition(source, lang)
	}
	defer tree.Close()
	result := &Decomposition{
		source:   source,
		lang:     lang,
		commands: nil,
		reads:    nil,
		writes:   nil,
		embedded: nil,
		cwdSpans: nil,
		opaque:   false,
	}
	if analyzer, found := lookupAnalyzer(lang); found {
		input := AnalyzerInput{
			Root:     tree.RootNode(),
			Source:   source,
			Argv:     argv,
			Cwd:      cwd,
			Home:     homeDir,
			Resolver: resolver,
			Visited:  visited,
			Depth:    depth,
		}
		result.reads, result.writes = analyzer(input)
	}
	return result
}
