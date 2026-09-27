// Package perl exposes the tree-sitter Perl grammar to the splitter. The
// grammar's C sources are committed under src/, and a build needs neither a git
// submodule nor the tree-sitter CLI. The upstream repository commits the
// grammar definition, the external scanner, and the scanner's headers but not
// the generated parser. scripts/vendor-grammars.sh generates src/parser.c with
// the upstream commit, tree-sitter CLI release, and ABI that upstream.conf
// pins. The grammar_parser.c and grammar_scanner.c shims in this directory
// compile src/parser.c and src/scanner.c as separate translation units.
// Separate units prevent collisions between the parser's macros and the
// scanner's macros.
package perl

// #cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/src
// typedef struct TSLanguage TSLanguage;
// const TSLanguage *tree_sitter_perl(void);
import "C"

import "unsafe"

// Language returns the tree-sitter Language pointer for Perl.
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_perl())
}
