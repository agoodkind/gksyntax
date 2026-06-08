package shelldecomp

import (
	"slices"
	"strings"
	"sync"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Embedding identifies one span of embedded code a Dispatcher located inside a
// command: the byte range that holds the code, the language to parse it as, and
// whether it runs in a fresh cwd scope. AsCommand marks a shell embedding whose
// span is itself a command line (xargs CMD, find -exec CMD) rather than a free
// script, so the recursion parses it as a command in the shared or child scope.
type Embedding struct {
	StartByte uint
	EndByte   uint
	Text      string
	Lang      Lang
	NewScope  bool
	AsCommand bool
}

// Dispatcher inspects a classified command and returns the embedded code it
// carries. A consumer registers one per embedder argv0 through Register; the
// built-in table covers interpreters, shell wrappers, and mini-languages.
type Dispatcher func(cmd Command, source []byte) []Embedding

// dispatchRegistry holds the argv0 to Dispatcher table behind a mutex so
// Register is safe to call from package init or from a consumer at startup.
type dispatchRegistry struct {
	mutex   sync.RWMutex
	byArgv0 map[string]Dispatcher
}

var registry = &dispatchRegistry{
	mutex:   sync.RWMutex{},
	byArgv0: map[string]Dispatcher{},
}

// Register installs a Dispatcher for an argv0, replacing any prior entry. A
// consumer calls it to teach shelldecomp about a new embedder; the built-in
// entries are registered in this package's init.
func Register(argv0 string, dispatcher Dispatcher) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.byArgv0[argv0] = dispatcher
}

// lookupDispatcher returns the Dispatcher for an argv0 and whether one is
// registered.
func lookupDispatcher(argv0 string) (Dispatcher, bool) {
	registry.mutex.RLock()
	defer registry.mutex.RUnlock()
	dispatcher, found := registry.byArgv0[argv0]
	return dispatcher, found
}

// dispatchEmbedded finds embedded code in a command through the dispatch table
// and turns each embedding into an EmbeddedRegion, recursing into a child parse
// when the embedded language has a grammar and the depth budget allows it.
func (walk *walker) dispatchEmbedded(node *tree_sitter.Node, command Command, args []rawArg, currentScope *scope) {
	_ = node
	_ = args
	dispatcher, found := lookupDispatcher(command.Argv0)
	if !found {
		return
	}
	embeddings := dispatcher(command, walk.source)
	for _, embedding := range embeddings {
		region := walk.buildEmbeddedRegion(embedding, currentScope)
		walk.result.embedded = append(walk.result.embedded, region)
	}
}

// buildEmbeddedRegion turns one Embedding into an EmbeddedRegion, parsing its
// body when a grammar exists and the depth budget remains. A language with no
// grammar, or an exhausted budget, yields a located region with a nil parse so
// a consumer still sees that foreign code lives there.
func (walk *walker) buildEmbeddedRegion(embedding Embedding, currentScope *scope) EmbeddedRegion {
	region := EmbeddedRegion{
		Lang:      embedding.Lang,
		StartByte: embedding.StartByte,
		EndByte:   embedding.EndByte,
		Text:      embedding.Text,
		Parsed:    nil,
		NewScope:  embedding.NewScope,
	}
	region.Parsed = walk.parseEmbedding(embedding, currentScope)
	return region
}

// parseEmbedding recursively parses an embedding's body. Shell embeddings parse
// with the bash grammar in either a child or the shared scope; a foreign
// language parses with its own grammar when one is registered; a language with
// no grammar returns nil so the region stays located but unparsed.
func (walk *walker) parseEmbedding(embedding Embedding, currentScope *scope) *Decomposition {
	if walk.depth <= 1 {
		return nil
	}
	if embedding.Lang == LangShell {
		return walk.parseShellEmbedding(embedding, currentScope)
	}
	grammarName := embedding.Lang.grammarName()
	if grammarName == "" {
		return nil
	}
	return parseForeign(grammarName, []byte(embedding.Text), embedding.Lang, walk.depth-1)
}

// parseShellEmbedding parses a shell embedding's body. An AsCommand embedding is
// a command line parsed in the shared or a child scope; a free script embedding
// is parsed as a whole shell program in a child scope when NewScope is set.
func (walk *walker) parseShellEmbedding(embedding Embedding, currentScope *scope) *Decomposition {
	baseCwd := currentScope.cwd
	if embedding.NewScope {
		baseCwd = currentScope.child().cwd
	}
	return parseShell([]byte(embedding.Text), baseCwd, walk.homeDir, walk.depth-1)
}

// flagValueEmbedding extracts the value of a flag (for example -c or -e) as a
// new-scope embedding, returning no embedding when the flag or a literal value
// is absent. An interpreter flag script always runs in a fresh scope, so the
// embedding is marked NewScope.
func flagValueEmbedding(cmd Command, flag string, lang Lang) []Embedding {
	for index := range cmd.Args {
		if cmd.Args[index].Text != flag {
			continue
		}
		if index+1 >= len(cmd.Args) {
			return nil
		}
		value := cmd.Args[index+1]
		if !value.Resolvable {
			return nil
		}
		return []Embedding{{
			StartByte: 0,
			EndByte:   0,
			Text:      value.Value,
			Lang:      lang,
			NewScope:  true,
			AsCommand: false,
		}}
	}
	return nil
}

// trailingEmbedding takes the last literal operand as embedded code, used for
// ssh host '<cmd>' and similar shapes where the script is the final argument.
func trailingEmbedding(cmd Command, lang Lang, newScope bool, asCommand bool) []Embedding {
	for _, arg := range slices.Backward(cmd.Args) {
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
			Lang:      lang,
			NewScope:  newScope,
			AsCommand: asCommand,
		}}
	}
	return nil
}

// firstPositionalEmbedding takes the first literal non-flag operand as embedded
// code, used for xargs <cmd> and parallel <cmd> where the command follows the
// wrapper's own flags.
func firstPositionalEmbedding(cmd Command, lang Lang, newScope bool, asCommand bool) []Embedding {
	for index := range cmd.Args {
		arg := cmd.Args[index]
		if strings.HasPrefix(arg.Text, "-") {
			continue
		}
		if !arg.Resolvable {
			return nil
		}
		rest := joinFrom(cmd.Args, index)
		return []Embedding{{
			StartByte: 0,
			EndByte:   0,
			Text:      rest,
			Lang:      lang,
			NewScope:  newScope,
			AsCommand: asCommand,
		}}
	}
	return nil
}

// joinFrom joins the literal text of operands from start onward into a single
// command line, used to reconstruct a wrapped command from its tokens.
func joinFrom(args []Word, start int) string {
	parts := make([]string, 0, len(args)-start)
	for index := start; index < len(args); index++ {
		if !args[index].Resolvable {
			return strings.Join(parts, " ")
		}
		parts = append(parts, args[index].Value)
	}
	return strings.Join(parts, " ")
}
