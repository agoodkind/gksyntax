package shelldecomp

// Commands returns the simple commands found in the source in order. An opaque
// decomposition (unparseable input) returns an empty slice.
func (decomposition *Decomposition) Commands() []Command {
	return decomposition.commands
}

// ReadTargets returns the files the commands read, resolved against the cwd in
// effect at each command. A target that cannot be pinned to a literal has
// Resolvable false and Path Unresolvable.
func (decomposition *Decomposition) ReadTargets() []ReadTarget {
	return decomposition.reads
}

// WriteTargets returns the files the commands write, resolved against the cwd
// in effect at each command. A target that cannot be pinned to a literal has
// Resolvable false and Path Unresolvable.
func (decomposition *Decomposition) WriteTargets() []WriteTarget {
	return decomposition.writes
}

// EmbeddedRegions returns the embedded code regions found in the source: the
// heredoc bodies, interpreter scripts, remote shells, and mini-language
// programs. A region whose language has a grammar carries a recursive Parsed
// decomposition; one without a grammar carries a nil Parsed.
func (decomposition *Decomposition) EmbeddedRegions() []EmbeddedRegion {
	return decomposition.embedded
}

// IsOpaque reports whether the source could not be parsed and the whole input
// is carried as a single opaque region rather than classified commands.
func (decomposition *Decomposition) IsOpaque() bool {
	return decomposition.opaque
}

// EffectiveCwdAt returns the working directory in effect at a byte offset in
// the source: the cwd of the last cd at or before that offset within the top
// scope. It returns Unresolvable once an unpinnable cd has poisoned the cwd,
// and "" when the starting directory was unknown and no cd has run yet.
func (decomposition *Decomposition) EffectiveCwdAt(byteOffset uint) string {
	cwd := ""
	for index := range decomposition.cwdSpans {
		span := decomposition.cwdSpans[index]
		if span.startByte > byteOffset {
			break
		}
		cwd = span.cwd
	}
	return cwd
}
