// Package shelldecomp parses a shell command into a labeled syntax tree and
// classifies each part: the commands, their read and write path targets, the
// working directory in effect at each point, and any embedded code (heredoc
// bodies, interpreter -c scripts, remote shells) discovered through tree-sitter
// injection and a program-name dispatch table. Anything it cannot parse becomes
// an Opaque node and any path it cannot pin to a literal is Unresolvable, so a
// caller can always treat uncertainty as "allow" rather than a fabricated fact.
package shelldecomp
