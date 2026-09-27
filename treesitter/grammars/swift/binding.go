// Package swift exposes the tree-sitter Swift grammar to the splitter. The
// grammar's C sources are committed under src/, and a build needs neither a git
// submodule nor the tree-sitter CLI. The upstream repository commits the
// grammar definition and the external scanner but not the generated parser.
// scripts/vendor-grammars.sh generates src/parser.c with the upstream commit,
// tree-sitter CLI release, and ABI that upstream.conf pins. The
// grammar_parser.c and grammar_scanner.c shims in this directory compile
// src/parser.c and src/scanner.c as separate translation units. Separate units
// prevent collisions between the parser's macros and the scanner's macros.
package swift

// #cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/src
// typedef struct TSLanguage TSLanguage;
// const TSLanguage *tree_sitter_swift(void);
import "C"

import "unsafe"

// Language returns the tree-sitter Language pointer for Swift.
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_swift())
}
