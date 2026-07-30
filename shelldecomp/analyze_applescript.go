package shelldecomp

import "strings"

// appleScriptArgv0 is the argv0 label recorded on read and write targets an
// analyzed AppleScript source produces, matching how sedArgv0 is used in
// analyze_sed.go.
const appleScriptArgv0 = "osascript"

// asKeywordRead, asKeywordWrite, asKeywordTo, asKeywordFile, asKeywordAlias,
// and asKeywordPOSIX are the AppleScript keywords this analyzer recognizes.
// AppleScript's own compiler treats these case-insensitively (it re-cases
// hand-typed source to the canonical form when it compiles a script), so
// every comparison against these constants uses [strings.EqualFold] rather
// than an exact-case match; the comparison is still against this exact set,
// never a prefix or substring, since a token must equal one of these words
// whole.
const (
	asKeywordRead  = "read"
	asKeywordWrite = "write"
	asKeywordTo    = "to"
	asKeywordFile  = "file"
	asKeywordAlias = "alias"
	asKeywordPOSIX = "POSIX"
)

// init registers the AppleScript analyzer so analyzeGrammarlessEmbedding runs
// it over an osascript -e embedding's source.
func init() {
	RegisterAnalyzer(LangAppleScript, analyzeAppleScript)
}

// analyzeAppleScript scans an osascript -e script's source for the file
// accesses this analyzer resolves: "read POSIX file <literal>", "read file
// <literal>", and "read alias <literal>" each read a file; "write ... to file
// <literal>" and "write ... to POSIX file <literal>" each write one. LangAppleScript
// has no registered tree-sitter grammar (Lang.grammarName returns "" for it),
// so this runs as a text scan over the region source, in the style of
// analyze_sed.go, rather than a tree walk.
//
// "do shell script <literal>" is deliberately out of scope: its argument is a
// shell command line, which conceptually belongs to shelldecomp's embedded
// shell layer rather than to this AppleScript analyzer. No dispatcher in this
// package currently recognizes "do shell script" (dispatch is keyed by a
// command's own argv0, and AppleScript has no grammar for the walker to find
// one inside), so its shell body is not resolved by any layer today; this is
// a drop, not a fabrication, and a future dispatcher for "do shell script"
// belongs beside the other shell embedders in dispatch_entries.go, not in
// this text scan.
//
// A path written in the classic Mac HFS style, with colons as separators such
// as "Macintosh HD:Users:x", is not a POSIX path. This analyzer only resolves
// POSIX absolute and relative paths, and converting an HFS path to POSIX would
// require knowing which POSIX mount point the named volume maps to,
// information a text scan over the script source cannot see. An HFS-style
// literal is therefore dropped rather than guessed at; see isHFSStylePath.
//
// A path built from a variable reference or a concatenation with & is not a
// literal this analyzer can pin, so the whole read or write call is dropped: a
// present but non-literal argument is not the same as an absent one. A string
// literal whose raw source span carries a backslash is dropped rather than
// decoded, matching the same rule analyze_ruby.go and analyze_sql.go apply,
// since decoding an escape could resolve a path the runtime never builds.
//
// The source is scanned one line at a time, since a real read or write
// statement from an osascript -e body is written on a single line; a
// statement split across lines by AppleScript's ¬ continuation character is
// not recognized and is dropped, not fabricated. A -- line comment and a (* *)
// block comment (including one spanning multiple lines) are skipped so that
// text inside a comment is never mistaken for a live read or write statement.
func analyzeAppleScript(in AnalyzerInput) ([]ReadTarget, []WriteTarget) {
	collector := &appleScriptCollector{in: in, scope: scope{cwd: in.Cwd, homeDir: in.Home}}
	inBlockComment := false
	for line := range strings.SplitSeq(string(in.Source), "\n") {
		var tokens []asToken
		tokens, inBlockComment = tokenizeAppleScriptLine(line, inBlockComment)
		collector.scanTokens(tokens)
	}
	return collector.reads, collector.writes
}

// appleScriptCollector accumulates the read and write targets found while
// scanning one AppleScript source, carrying the cwd/home scope used to
// resolve each literal path.
type appleScriptCollector struct {
	in     AnalyzerInput
	scope  scope
	reads  []ReadTarget
	writes []WriteTarget
}

// asTokenKind distinguishes a double-quoted string literal from an identifier
// or keyword from any other single lexical unit while scanning an AppleScript
// line.
type asTokenKind int

const (
	asTokenOther asTokenKind = iota
	asTokenIdent
	asTokenString
)

// asToken is one lexical unit found while scanning an AppleScript line for
// the read and write shapes this analyzer resolves. Value and HasBackslash
// are only meaningful when Kind is asTokenString.
type asToken struct {
	Kind         asTokenKind
	Text         string
	Value        string
	HasBackslash bool
}

// tokenizeAppleScriptLine splits one line of AppleScript source into
// identifiers, double-quoted string literals, and single-character tokens,
// skipping whitespace, a -- line comment, and a (* *) block comment. inBlock
// reports whether the line starts already inside a block comment opened on an
// earlier line; the second return reports whether the line ends still inside
// one, so the caller threads the flag across lines. This is not a full
// AppleScript lexer: a number, an operator, and every other punctuation token
// fold into single-character asTokenOther tokens, since this analyzer only
// needs to recognize the read/write keyword shapes, never to evaluate an
// expression.
func tokenizeAppleScriptLine(line string, inBlock bool) ([]asToken, bool) {
	var tokens []asToken
	pos := 0
	length := len(line)
	for pos < length {
		if inBlock {
			end := strings.Index(line[pos:], "*)")
			if end < 0 {
				return tokens, true
			}
			pos += end + 2
			inBlock = false
			continue
		}
		char := line[pos]
		switch {
		case char == ' ' || char == '\t' || char == '\r':
			pos++
		case char == '-' && pos+1 < length && line[pos+1] == '-':
			pos = length
		case char == '(' && pos+1 < length && line[pos+1] == '*':
			pos += 2
			inBlock = true
		case char == '"':
			start := pos
			pos = scanAppleScriptStringLiteral(line, pos)
			raw := line[start:pos]
			tokens = append(tokens, asToken{
				Kind:         asTokenString,
				Text:         raw,
				Value:        asUnquoteLiteral(raw),
				HasBackslash: strings.ContainsRune(raw, '\\'),
			})
		case isASIdentStart(char):
			start := pos
			pos++
			for pos < length && isASIdentPart(line[pos]) {
				pos++
			}
			tokens = append(tokens, asToken{Kind: asTokenIdent, Text: line[start:pos]})
		default:
			tokens = append(tokens, asToken{Kind: asTokenOther, Text: string(char)})
			pos++
		}
	}
	return tokens, inBlock
}

// scanAppleScriptStringLiteral returns the index just past a double-quoted
// AppleScript string literal starting at pos (which must hold the opening
// "). AppleScript escapes an embedded quote or backslash with a leading
// backslash, so a \" or \\ pair does not end the literal; an unterminated
// literal runs to the end of the line.
func scanAppleScriptStringLiteral(line string, pos int) int {
	length := len(line)
	pos++
	for pos < length {
		if line[pos] == '\\' {
			pos += 2
			continue
		}
		if line[pos] == '"' {
			return pos + 1
		}
		pos++
	}
	return pos
}

// isASIdentStart reports whether a byte can begin an AppleScript identifier or
// keyword: a letter or underscore.
func isASIdentStart(char byte) bool {
	return char == '_' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

// isASIdentPart reports whether a byte can continue an AppleScript identifier
// or keyword: everything isASIdentStart allows, plus a digit.
func isASIdentPart(char byte) bool {
	return isASIdentStart(char) || (char >= '0' && char <= '9')
}

// asUnquoteLiteral strips the surrounding quotes from a raw double-quoted
// AppleScript string literal span, without decoding any backslash escape
// inside it. A raw span shorter than two bytes is not a well-formed literal
// and unquotes to empty. Callers only use the unquoted value after confirming
// HasBackslash is false, so no escape ever needs decoding here.
func asUnquoteLiteral(raw string) string {
	if len(raw) < 2 {
		return ""
	}
	return raw[1 : len(raw)-1]
}

// scanTokens walks one line's tokens once, recognizing a read or write
// statement wherever its keyword starts the shape.
func (collector *appleScriptCollector) scanTokens(tokens []asToken) {
	for index, token := range tokens {
		if token.Kind != asTokenIdent {
			continue
		}
		switch {
		case strings.EqualFold(token.Text, asKeywordRead):
			collector.scanRead(tokens, index)
		case strings.EqualFold(token.Text, asKeywordWrite):
			collector.scanWrite(tokens, index)
		}
	}
}

// scanRead inspects the tokens after a read keyword at index for the three
// forms this analyzer resolves: POSIX file <literal>, file <literal>, and
// alias <literal>. Any other word following read, such as a bare variable
// holding a file reference, is not one of these three exact shapes, so
// nothing is recorded.
func (collector *appleScriptCollector) scanRead(tokens []asToken, index int) {
	next := index + 1
	if matchAppleScriptIdent(tokens, next, asKeywordPOSIX) && matchAppleScriptIdent(tokens, next+1, asKeywordFile) {
		collector.addLiteralRead(tokens, next+2)
		return
	}
	if matchAppleScriptIdent(tokens, next, asKeywordFile) {
		collector.addLiteralRead(tokens, next+1)
		return
	}
	if matchAppleScriptIdent(tokens, next, asKeywordAlias) {
		collector.addLiteralRead(tokens, next+1)
	}
}

// scanWrite looks past a write keyword at index for a to clause naming a file
// target: to POSIX file <literal> or to file <literal>. It scans every to
// token on the line rather than stopping at the first one, since to is also
// AppleScript's range operator (character 1 to 5 of x) and can appear in the
// data expression before the real target clause; skipping past a to that is
// not followed by file never risks a fabricated path, so continuing to look
// for the real target clause is safe.
func (collector *appleScriptCollector) scanWrite(tokens []asToken, index int) {
	for cursor := index + 1; cursor < len(tokens); cursor++ {
		if !matchAppleScriptIdent(tokens, cursor, asKeywordTo) {
			continue
		}
		next := cursor + 1
		if matchAppleScriptIdent(tokens, next, asKeywordPOSIX) && matchAppleScriptIdent(tokens, next+1, asKeywordFile) {
			collector.addLiteralWrite(tokens, next+2)
			return
		}
		if matchAppleScriptIdent(tokens, next, asKeywordFile) {
			collector.addLiteralWrite(tokens, next+1)
			return
		}
	}
}

// matchAppleScriptIdent reports whether the token at index is an identifier
// equal to want under case-insensitive comparison, matching AppleScript's own
// case-insensitive keyword handling.
func matchAppleScriptIdent(tokens []asToken, index int, want string) bool {
	if index < 0 || index >= len(tokens) {
		return false
	}
	token := tokens[index]
	return token.Kind == asTokenIdent && strings.EqualFold(token.Text, want)
}

// appleScriptLiteralTarget resolves the token at literalIndex to a usable
// path value. It requires a string literal with no raw backslash, rejects an
// HFS colon-style path, and rejects a literal immediately followed by & (a
// concatenation continuing the expression, so the literal alone is not the
// whole path). Any of these failures returns ("", false), which the caller
// treats as drop-the-call rather than resolve-a-fabricated-path.
func appleScriptLiteralTarget(tokens []asToken, literalIndex int) (string, bool) {
	if literalIndex >= len(tokens) || tokens[literalIndex].Kind != asTokenString {
		return "", false
	}
	literal := tokens[literalIndex]
	if literal.HasBackslash {
		return "", false
	}
	if literalIndex+1 < len(tokens) && tokens[literalIndex+1].Kind == asTokenOther && tokens[literalIndex+1].Text == "&" {
		return "", false
	}
	if isHFSStylePath(literal.Value) {
		return "", false
	}
	return literal.Value, true
}

// isHFSStylePath reports whether a literal path uses the classic Mac HFS
// colon-separated form, such as "Macintosh HD:Users:x", rather than a POSIX
// path. See the analyzeAppleScript doc comment for why this analyzer drops
// rather than converts such a path.
func isHFSStylePath(value string) bool {
	return strings.Contains(value, ":")
}

// addLiteralRead records the literal target at literalIndex as a read, when
// appleScriptLiteralTarget accepts it.
func (collector *appleScriptCollector) addLiteralRead(tokens []asToken, literalIndex int) {
	value, ok := appleScriptLiteralTarget(tokens, literalIndex)
	if !ok {
		return
	}
	collector.addRead(value, tokens[literalIndex].Text)
}

// addLiteralWrite records the literal target at literalIndex as a write, when
// appleScriptLiteralTarget accepts it.
func (collector *appleScriptCollector) addLiteralWrite(tokens []asToken, literalIndex int) {
	value, ok := appleScriptLiteralTarget(tokens, literalIndex)
	if !ok {
		return
	}
	collector.addWrite(value, tokens[literalIndex].Text)
}

// addRead resolves a path value against the cwd and records it as a read
// target, with raw as the original source text.
func (collector *appleScriptCollector) addRead(value string, raw string) {
	resolved := collector.scope.resolvePath(value)
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	collector.reads = append(collector.reads, ReadTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      appleScriptArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        raw,
	})
}

// addWrite resolves a path value against the cwd and records it as a write
// target, with raw as the original source text.
func (collector *appleScriptCollector) addWrite(value string, raw string) {
	resolved := collector.scope.resolvePath(value)
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	collector.writes = append(collector.writes, WriteTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      appleScriptArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        raw,
	})
}
