package shelldecomp

import "strings"

// sqlArgv0 is the argv0 label recorded on read and write targets a sqlite3
// SQL or dot-command source produces, matching how sedArgv0 is used in
// analyze_sed.go.
const sqlArgv0 = "sqlite3"

// sqlDotImport and sqlDotOutput are the two sqlite3 shell dot commands this
// analyzer resolves. A dot command's name is matched exactly, never by
// prefix, so a real sqlite3 shell abbreviation such as ".imp" for ".import"
// is not recognized: resolving an abbreviation correctly requires knowing
// every dot command to disambiguate it, which this text scan does not do, so
// it drops rather than guesses.
const (
	sqlDotImport = ".import"
	sqlDotOutput = ".output"
)

// sqlOutputStdout and sqlOutputOff are the two special ".output" arguments
// the sqlite3 shell treats as "send to the console" rather than as a file
// name to open. Recording a write target for either would resolve a path the
// shell never touches, so both are matched by exact string and dropped.
const (
	sqlOutputStdout = "stdout"
	sqlOutputOff    = "off"
)

// sqlAttachKeyword, sqlDatabaseKeyword, and sqlAsKeyword are the SQL keywords
// the ATTACH scan matches, compared case-insensitively since SQL keywords are
// themselves case-insensitive.
const (
	sqlAttachKeyword   = "ATTACH"
	sqlDatabaseKeyword = "DATABASE"
	sqlAsKeyword       = "AS"
)

// sqlReadfileFunc and sqlWritefileFunc are the sqlite3 shell's built-in
// file-access scalar functions this analyzer resolves.
const (
	sqlReadfileFunc  = "readfile"
	sqlWritefileFunc = "writefile"
)

// init registers the SQL analyzer so analyzeGrammarlessEmbedding runs it
// over a sqlite3 embedding's source.
func init() {
	RegisterAnalyzer(LangSQL, analyzeSQL)
}

// analyzeSQL scans a sqlite3 command's SQL and dot-command source for the
// file accesses the sqlite3 shell documents: ATTACH [DATABASE] 'path' AS
// name and readfile('path') read a file; writefile('path', ...) and
// .output path write one; .import path table reads one. LangSQL has no
// registered tree-sitter grammar (Lang.grammarName returns "" for it), so
// this runs as a text scan over the region source, in the style of
// analyze_sed.go, rather than a tree walk.
//
// A sqlite3 dot command occupies a whole line by itself and cannot mix with
// SQL text on that line, so the source is split by line first: a line whose
// first non-blank character is '.' is scanned as a dot command, and every
// other line is rejoined for the SQL token scan.
func analyzeSQL(in AnalyzerInput) ([]ReadTarget, []WriteTarget) {
	collector := &sqlCollector{in: in, scope: scope{cwd: in.Cwd, homeDir: in.Home}}
	var sqlLines []string
	for line := range strings.SplitSeq(string(in.Source), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, ".") {
			collector.scanDotCommand(trimmed)
			continue
		}
		sqlLines = append(sqlLines, line)
	}
	collector.scanSQL(strings.Join(sqlLines, "\n"))
	return collector.reads, collector.writes
}

// sqlCollector accumulates the read and write targets found while scanning
// one sqlite3 source, carrying the cwd/home scope used to resolve each
// literal path.
type sqlCollector struct {
	in     AnalyzerInput
	scope  scope
	reads  []ReadTarget
	writes []WriteTarget
}

// sqlDotToken is one whitespace- or quote-delimited argument token found on a
// sqlite3 dot-command line. Value is the token's content with a surrounding
// single or double quote removed; HasBackslash reports whether the token's
// raw source span (including any quotes) carries a backslash.
type sqlDotToken struct {
	Raw          string
	Value        string
	HasBackslash bool
}

// splitDotCommandArgs tokenizes a sqlite3 dot-command line into
// whitespace-separated arguments, treating a single- or double-quoted run as
// one token. This is shell-style word splitting, not SQL string parsing,
// matching how the sqlite3 shell itself reads a dot command's own argument
// line rather than as SQL.
func splitDotCommandArgs(line string) []sqlDotToken {
	var tokens []sqlDotToken
	pos := 0
	length := len(line)
	for pos < length {
		for pos < length && (line[pos] == ' ' || line[pos] == '\t') {
			pos++
		}
		if pos >= length {
			break
		}
		if line[pos] == '\'' || line[pos] == '"' {
			quote := line[pos]
			start := pos
			pos++
			for pos < length && line[pos] != quote {
				pos++
			}
			end := pos
			if pos < length {
				pos++
			}
			raw := line[start:pos]
			tokens = append(tokens, sqlDotToken{
				Raw:          raw,
				Value:        line[start+1 : end],
				HasBackslash: strings.ContainsRune(raw, '\\'),
			})
			continue
		}
		start := pos
		for pos < length && line[pos] != ' ' && line[pos] != '\t' {
			pos++
		}
		raw := line[start:pos]
		tokens = append(tokens, sqlDotToken{Raw: raw, Value: raw, HasBackslash: strings.ContainsRune(raw, '\\')})
	}
	return tokens
}

// scanDotCommand tokenizes one already-trimmed dot-command line and records
// its file access when the command is .import or .output. Any other dot
// command, and a recognized command with no path argument, records nothing.
func (collector *sqlCollector) scanDotCommand(line string) {
	tokens := splitDotCommandArgs(line)
	if len(tokens) < 2 {
		return
	}
	switch tokens[0].Value {
	case sqlDotImport:
		collector.addDotRead(tokens[1])
	case sqlDotOutput:
		collector.addDotOutputWrite(tokens[1])
	}
}

// addDotRead records a .import FILE argument as a read, unless its raw span
// carries a backslash.
func (collector *sqlCollector) addDotRead(token sqlDotToken) {
	if token.HasBackslash {
		return
	}
	collector.addRead(token.Value, token.Raw)
}

// addDotOutputWrite records a .output FILE argument as a write, unless its
// raw span carries a backslash or the argument is one of the sqlite3 shell's
// two magic values that mean "the console", not a file: stdout and off.
func (collector *sqlCollector) addDotOutputWrite(token sqlDotToken) {
	if token.HasBackslash {
		return
	}
	if token.Value == sqlOutputStdout || token.Value == sqlOutputOff {
		return
	}
	collector.addWrite(token.Value, token.Raw)
}

// sqlTokenKind distinguishes a quoted string literal from an identifier or
// keyword from any other single lexical unit while scanning SQL text.
type sqlTokenKind int

const (
	sqlTokenOther sqlTokenKind = iota
	sqlTokenIdent
	sqlTokenString
)

// sqlToken is one lexical unit found while scanning SQL statement text for
// the ATTACH, readfile, and writefile shapes. Value and HasBackslash are only
// meaningful when Kind is sqlTokenString.
type sqlToken struct {
	Kind         sqlTokenKind
	Text         string
	Value        string
	HasBackslash bool
}

// tokenizeSQL splits SQL statement text into identifiers, single-quoted
// string literals, and single-character tokens, skipping whitespace and
// -- and /* */ comments. It is not a full SQL lexer: a number, an operator,
// and a double-quoted identifier all fold into single-character
// sqlTokenOther tokens, since this analyzer only needs to recognize a
// keyword or function-call shape, never to evaluate an expression.
func tokenizeSQL(source string) []sqlToken {
	var tokens []sqlToken
	pos := 0
	length := len(source)
	for pos < length {
		char := source[pos]
		switch {
		case char == ' ' || char == '\t' || char == '\n' || char == '\r':
			pos++
		case char == '-' && pos+1 < length && source[pos+1] == '-':
			for pos < length && source[pos] != '\n' {
				pos++
			}
		case char == '/' && pos+1 < length && source[pos+1] == '*':
			pos += 2
			for pos+1 < length && (source[pos] != '*' || source[pos+1] != '/') {
				pos++
			}
			pos += 2
			if pos > length {
				pos = length
			}
		case char == '\'':
			start := pos
			pos = scanSQLStringLiteral(source, pos)
			raw := source[start:pos]
			tokens = append(tokens, sqlToken{
				Kind:         sqlTokenString,
				Text:         raw,
				Value:        sqlUnquoteLiteral(raw),
				HasBackslash: strings.ContainsRune(raw, '\\'),
			})
		case isSQLIdentStart(char):
			start := pos
			pos++
			for pos < length && isSQLIdentPart(source[pos]) {
				pos++
			}
			tokens = append(tokens, sqlToken{Kind: sqlTokenIdent, Text: source[start:pos]})
		default:
			tokens = append(tokens, sqlToken{Kind: sqlTokenOther, Text: string(char)})
			pos++
		}
	}
	return tokens
}

// scanSQLStringLiteral returns the index just past a single-quoted SQL
// string literal starting at pos (which must hold the opening '). SQL
// escapes an embedded quote by doubling it (”), so a ” pair does not end
// the literal; an unterminated literal runs to the end of the source.
func scanSQLStringLiteral(source string, pos int) int {
	length := len(source)
	pos++
	for pos < length {
		if source[pos] == '\'' {
			if pos+1 < length && source[pos+1] == '\'' {
				pos += 2
				continue
			}
			return pos + 1
		}
		pos++
	}
	return pos
}

// isSQLIdentStart reports whether a byte can begin a SQL identifier or
// keyword: a letter or underscore.
func isSQLIdentStart(char byte) bool {
	return char == '_' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

// isSQLIdentPart reports whether a byte can continue a SQL identifier or
// keyword: everything isSQLIdentStart allows, plus a digit.
func isSQLIdentPart(char byte) bool {
	return isSQLIdentStart(char) || (char >= '0' && char <= '9')
}

// sqlUnquoteLiteral strips the surrounding quotes from a raw single-quoted
// SQL string literal span and un-doubles SQL's ” escaped-quote sequence
// into a single '. A raw span shorter than two bytes is not a well-formed
// literal and unquotes to empty.
func sqlUnquoteLiteral(raw string) string {
	if len(raw) < 2 {
		return ""
	}
	inner := raw[1 : len(raw)-1]
	return strings.ReplaceAll(inner, "''", "'")
}

// scanSQL walks the tokenized SQL text once, recognizing an ATTACH statement
// and a readfile or writefile call wherever they occur in the token stream.
func (collector *sqlCollector) scanSQL(source string) {
	tokens := tokenizeSQL(source)
	for index, token := range tokens {
		if token.Kind != sqlTokenIdent {
			continue
		}
		switch {
		case strings.EqualFold(token.Text, sqlAttachKeyword):
			collector.scanAttach(tokens, index)
		case strings.EqualFold(token.Text, sqlReadfileFunc):
			collector.scanFunctionCall(tokens, index, collector.addRead)
		case strings.EqualFold(token.Text, sqlWritefileFunc):
			collector.scanFunctionCall(tokens, index, collector.addWrite)
		}
	}
}

// scanAttach inspects the tokens after an ATTACH keyword at index for the
// [DATABASE] 'literal' AS name shape and records a read when the literal is
// a backslash-free single-quoted string immediately followed by AS.
// ATTACH's own grammar makes the AS clause mandatory, so requiring it here
// does not narrow real coverage; it only rejects a shape that is not
// actually an ATTACH statement. A bind parameter (?, ?1, :name, @name,
// $name) or a column reference in the expression position is not a
// sqlTokenString, so it never matches and the call is dropped, never
// guessed.
func (collector *sqlCollector) scanAttach(tokens []sqlToken, index int) {
	next := index + 1
	if next < len(tokens) && tokens[next].Kind == sqlTokenIdent && strings.EqualFold(tokens[next].Text, sqlDatabaseKeyword) {
		next++
	}
	if next >= len(tokens) || tokens[next].Kind != sqlTokenString {
		return
	}
	literal := tokens[next]
	asIndex := next + 1
	if asIndex >= len(tokens) || tokens[asIndex].Kind != sqlTokenIdent || !strings.EqualFold(tokens[asIndex].Text, sqlAsKeyword) {
		return
	}
	if literal.HasBackslash {
		return
	}
	collector.addRead(literal.Value, literal.Text)
}

// scanFunctionCall inspects the tokens after a readfile or writefile
// identifier at index for a '(' immediately followed by a single-quoted
// string literal, and records it through record when found. A missing
// paren, a missing argument, or an argument that is not a plain string
// literal (a bind parameter, a column reference, or any other expression)
// leaves the whole call unrecorded: a present but non-literal argument is
// not the same as an absent one.
func (collector *sqlCollector) scanFunctionCall(tokens []sqlToken, index int, record func(value string, raw string)) {
	openIndex := index + 1
	if openIndex >= len(tokens) || tokens[openIndex].Kind != sqlTokenOther || tokens[openIndex].Text != "(" {
		return
	}
	argIndex := openIndex + 1
	if argIndex >= len(tokens) || tokens[argIndex].Kind != sqlTokenString {
		return
	}
	arg := tokens[argIndex]
	if arg.HasBackslash {
		return
	}
	record(arg.Value, arg.Text)
}

// addRead resolves a path value against the cwd and records it as a read
// target, with raw as the original source text.
func (collector *sqlCollector) addRead(value string, raw string) {
	resolved := collector.scope.resolvePath(value)
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	collector.reads = append(collector.reads, ReadTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      sqlArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        raw,
	})
}

// addWrite resolves a path value against the cwd and records it as a write
// target, with raw as the original source text.
func (collector *sqlCollector) addWrite(value string, raw string) {
	resolved := collector.scope.resolvePath(value)
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	collector.writes = append(collector.writes, WriteTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      sqlArgv0,
		Cwd:        collector.in.Cwd,
		Raw:        raw,
	})
}
