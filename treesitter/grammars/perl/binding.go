// Package perl exposes the tree-sitter Perl grammar to the splitter. The
// upstream repository commits the grammar definition (src/grammar.json) and the
// external scanner (src/scanner.c) but not the generated parser, so the parser
// is produced from the pinned upstream/ git submodule by the tree-sitter CLI
// (the `grammars` Makefile target). The generated parser.c and the scanner.c
// are each compiled as their own translation unit through the grammar_parser.c
// and grammar_scanner.c shims in this directory, which keeps their macros from
// colliding. No generated file is stored in this repository.
package perl

// #cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/upstream/src
// typedef struct TSLanguage TSLanguage;
// const TSLanguage *tree_sitter_perl(void);
import "C"

import "unsafe"

// Language returns the tree-sitter Language pointer for Perl.
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_perl())
}
