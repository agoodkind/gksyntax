// Package dart exposes the tree-sitter Dart grammar to the splitter. Dart has
// no maintained Go-module binding against this runtime, and this package
// vendors the grammar's C sources under src/ instead. scripts/vendor-grammars.sh
// copies the generated parser and the external scanner into src/ from the
// upstream commit that upstream.conf pins. The grammar_parser.c and
// grammar_scanner.c shims in this directory compile src/parser.c and
// src/scanner.c as separate translation units. Separate units prevent
// collisions between the parser's macros and the scanner's macros.
package dart

// #cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/src
// typedef struct TSLanguage TSLanguage;
// const TSLanguage *tree_sitter_dart(void);
import "C"

import "unsafe"

// Language returns the tree-sitter Language pointer for Dart.
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_dart())
}
