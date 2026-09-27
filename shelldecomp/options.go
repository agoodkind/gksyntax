package shelldecomp

// FileResolver reads the bytes of a program file named by an absolute path. It
// returns the content and true on success, or nil and false when the path is
// missing, unreadable, too large, or otherwise refused by the caller. A nil
// FileResolver means off-disk reads are disabled: a script path is located but
// its body is never read.
type FileResolver func(absPath string) (content []byte, ok bool)

// Options configures a parse. Home expands a leading tilde in resolved paths.
// FileResolver, when non-nil, lets the parser read an interpreter's script file
// off disk so the program inside it can be analyzed; a nil resolver leaves
// those scripts located but unparsed.
type Options struct {
	Home         string
	FileResolver FileResolver
}

// visitedSet tracks the absolute script paths already read during one parse so a
// self-referential program (a script that re-invokes itself) terminates rather
// than looping. It is threaded through the recursive parse alongside the depth
// budget; the depth budget bounds nesting and the visited set bounds revisiting.
type visitedSet struct {
	seen map[string]struct{}
}

// newVisitedSet returns an empty visited set ready to record script paths.
func newVisitedSet() *visitedSet {
	return &visitedSet{seen: map[string]struct{}{}}
}

// add records a path and reports whether it was newly added. It returns false
// when the path was already present, so a caller skips re-reading a script it
// has already descended into. A nil set or an empty path is never recorded and
// always reports false, so a caller treats both as "do not descend".
func (set *visitedSet) add(path string) bool {
	if set == nil || path == "" {
		return false
	}
	if _, found := set.seen[path]; found {
		return false
	}
	set.seen[path] = struct{}{}
	return true
}

// ParseWithOptions decomposes a shell command starting in baseCwd using opts to
// expand a tilde and, when opts.FileResolver is set, to read an interpreter's
// script file off disk. It mirrors Parse but adds the resolver seam; a nil
// resolver yields the same result Parse would. ParseWithOptions never panics.
func ParseWithOptions(command string, baseCwd string, opts Options) *Decomposition {
	return parseShellOpts([]byte(command), baseCwd, opts.Home, DefaultMaxDepth, opts.FileResolver, newVisitedSet())
}
