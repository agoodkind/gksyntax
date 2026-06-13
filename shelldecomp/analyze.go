package shelldecomp

import (
	"sync"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// AnalyzerInput carries everything a ReadAnalyzer needs to walk a foreign
// program's live tree-sitter tree and resolve its file accesses. Root is the
// parsed program root, still alive when the analyzer runs. Source is the
// program bytes. Argv holds the bare operands after the program slot for
// sys.argv resolution. Cwd and Home resolve relative and tilde paths. Resolver
// reads a nested script off disk, Visited stops a self-referential nest, and
// Depth is the remaining recursion budget for nested commands the program runs.
type AnalyzerInput struct {
	Root     *tree_sitter.Node
	Source   []byte
	Argv     []string
	Cwd      string
	Home     string
	Resolver FileResolver
	Visited  *visitedSet
	Depth    int
}

// ReadAnalyzer inspects a foreign program's tree and returns the files it reads
// and writes. A consumer registers one per language through RegisterAnalyzer; a
// language with no analyzer leaves an embedded region's reads and writes nil,
// exactly as before analyzers existed.
type ReadAnalyzer func(in AnalyzerInput) (reads []ReadTarget, writes []WriteTarget)

// analyzerRegistry holds the per-language ReadAnalyzer table behind a mutex so
// RegisterAnalyzer is safe to call from package init or from a consumer at
// startup, mirroring dispatchRegistry.
type analyzerRegistry struct {
	mutex  sync.RWMutex
	byLang map[Lang]ReadAnalyzer
}

var analyzers = &analyzerRegistry{
	mutex:  sync.RWMutex{},
	byLang: map[Lang]ReadAnalyzer{},
}

// RegisterAnalyzer installs a ReadAnalyzer for a language, replacing any prior
// entry. The built-in analyzers are registered in their files' init; a consumer
// may override or add one after those inits run.
func RegisterAnalyzer(lang Lang, analyzer ReadAnalyzer) {
	analyzers.mutex.Lock()
	defer analyzers.mutex.Unlock()
	analyzers.byLang[lang] = analyzer
}

// lookupAnalyzer returns the ReadAnalyzer for a language and whether one is
// registered.
func lookupAnalyzer(lang Lang) (ReadAnalyzer, bool) {
	analyzers.mutex.RLock()
	defer analyzers.mutex.RUnlock()
	analyzer, found := analyzers.byLang[lang]
	return analyzer, found
}
