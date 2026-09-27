// Compiles the vendored AWK external scanner in src/ as its own translation
// unit. A separate unit keeps the scanner's macros from colliding with the
// parser's macros.
#include "src/scanner.c"
