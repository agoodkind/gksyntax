package shelldecomp

import "strings"

// sedArgv0 is the argv0 label recorded on read and write targets a sed script's
// in-program file commands produce.
const sedArgv0 = "sed"

// init registers the sed analyzer. sed has no adequate tree-sitter grammar (the
// available one does not model the r/w file commands), so it runs over the
// script text rather than a parse tree, through the grammarless-analyzer path.
func init() {
	RegisterAnalyzer(LangSed, analyzeSed)
}

// analyzeSed scans a sed script for the file commands that read or write a file:
// r and R read a file, w and W write a file, and the w flag of an s///w
// substitution writes a file. sed is line-oriented and a file command's
// filename runs to the end of its line, so the script is scanned by line and by
// command position rather than parsed by a grammar. A filename resolves against
// the cwd; a command whose form is not understood is skipped, never guessed.
func analyzeSed(in AnalyzerInput) ([]ReadTarget, []WriteTarget) {
	scanner := &sedScanner{in: in, scope: scope{cwd: in.Cwd, homeDir: in.Home}}
	for line := range strings.SplitSeq(string(in.Source), "\n") {
		scanner.scanLine(line)
	}
	return scanner.reads, scanner.writes
}

// sedScanner accumulates the read and write targets found while scanning a sed
// script, carrying the cwd scope used to resolve each filename.
type sedScanner struct {
	in     AnalyzerInput
	scope  scope
	reads  []ReadTarget
	writes []WriteTarget
}

// scanLine processes one line of a sed script, walking commands left to right.
// It skips an optional address before each command, takes the rest of the line
// as the filename for an r, R, w, or W command (which consumes to end of line),
// reads the w flag of an s/// substitution, and skips commands it does not model.
func (scanner *sedScanner) scanLine(line string) {
	pos := 0
	length := len(line)
	for pos < length {
		for pos < length && (line[pos] == ';' || line[pos] == ' ' || line[pos] == '\t') {
			pos++
		}
		if pos >= length {
			return
		}
		pos = skipSedAddress(line, pos)
		for pos < length && (line[pos] == ' ' || line[pos] == '\t' || line[pos] == '!') {
			pos++
		}
		if pos >= length {
			return
		}
		command := line[pos]
		switch command {
		case '#':
			return
		case 'r', 'R':
			scanner.addRead(strings.TrimSpace(line[pos+1:]))
			return
		case 'w', 'W':
			scanner.addWrite(strings.TrimSpace(line[pos+1:]))
			return
		case 's', 'y':
			next, writeName, consumedLine := scanSedSubstitution(line, pos)
			if writeName != "" {
				scanner.addWrite(writeName)
			}
			if consumedLine {
				return
			}
			pos = next
		case 'a', 'i', 'c', 'e':
			return
		case '{', '}':
			pos++
		default:
			pos++
			for pos < length && line[pos] != ';' {
				pos++
			}
		}
	}
}

// skipSedAddress skips an optional address or address range before a command:
// one address, then an optional comma and a second address. It returns the
// index of the first byte after the address.
func skipSedAddress(line string, pos int) int {
	pos = skipOneSedAddress(line, pos)
	if pos < len(line) && line[pos] == ',' {
		pos = skipOneSedAddress(line, pos+1)
	}
	return pos
}

// skipOneSedAddress skips a single sed address: a line number with an optional
// ~step, the last-line $, a /regex/ match, or a \cregexc match with a custom
// delimiter. A position that does not begin an address is returned unchanged.
func skipOneSedAddress(line string, pos int) int {
	length := len(line)
	if pos >= length {
		return pos
	}
	char := line[pos]
	switch {
	case char == '$':
		return pos + 1
	case char >= '0' && char <= '9':
		for pos < length && line[pos] >= '0' && line[pos] <= '9' {
			pos++
		}
		if pos < length && (line[pos] == '~' || line[pos] == '+') {
			pos++
			for pos < length && line[pos] >= '0' && line[pos] <= '9' {
				pos++
			}
		}
		return pos
	case char == '/':
		return skipSedDelimited(line, pos+1, '/')
	case char == '\\':
		if pos+1 >= length {
			return pos + 1
		}
		return skipSedDelimited(line, pos+2, line[pos+1])
	default:
		return pos
	}
}

// skipSedDelimited returns the index after the closing delimiter of a delimited
// run (a regex address or an s/// section), honoring a backslash escape so an
// escaped delimiter does not end the run. An unterminated run returns the line
// length.
func skipSedDelimited(line string, pos int, delim byte) int {
	length := len(line)
	for pos < length {
		if line[pos] == '\\' {
			pos += 2
			continue
		}
		if line[pos] == delim {
			return pos + 1
		}
		pos++
	}
	return pos
}

// scanSedSubstitution scans an s/// or y/// command starting at the command
// letter. It skips the pattern and replacement sections, then reads the flags;
// a w flag takes the rest of the line as a write filename. It returns the index
// after the command, that filename when present, and whether the command
// consumed the rest of the line (a w flag does).
func scanSedSubstitution(line string, pos int) (int, string, bool) {
	length := len(line)
	pos++
	if pos >= length {
		return pos, "", true
	}
	delim := line[pos]
	pos = skipSedDelimited(line, pos+1, delim)
	pos = skipSedDelimited(line, pos, delim)
	for pos < length {
		char := line[pos]
		if char == ';' || char == ' ' || char == '\t' {
			return pos, "", false
		}
		if char == 'w' {
			return length, strings.TrimSpace(line[pos+1:]), true
		}
		pos++
	}
	return pos, "", true
}

// addRead resolves a sed read-command filename against the cwd and records it. An
// empty filename records nothing.
func (scanner *sedScanner) addRead(name string) {
	if name == "" {
		return
	}
	resolved := scanner.scope.resolvePath(name)
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	scanner.reads = append(scanner.reads, ReadTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      sedArgv0,
		Cwd:        scanner.in.Cwd,
		Raw:        name,
	})
}

// addWrite resolves a sed write-command filename against the cwd and records it.
func (scanner *sedScanner) addWrite(name string) {
	if name == "" {
		return
	}
	resolved := scanner.scope.resolvePath(name)
	resolvable := resolved != Unresolvable && resolved != ""
	path := resolved
	if !resolvable {
		path = Unresolvable
	}
	scanner.writes = append(scanner.writes, WriteTarget{
		Path:       path,
		Resolvable: resolvable,
		Argv0:      sedArgv0,
		Cwd:        scanner.in.Cwd,
		Raw:        name,
	})
}
