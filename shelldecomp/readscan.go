package shelldecomp

import "strings"

// readScan walks a search command's operands to separate flags, the pattern
// operand, and the path operands. It encodes the per-tool flag rules so a
// grep pattern is not mistaken for a path and a value-flag's operand is not
// mistaken for a path.
type readScan struct {
	argv0        string
	args         []rawArg
	valueFlags   map[string]bool
	patternTaken bool
	hasExprFlag  bool
}

// newReadScan builds a scan for one command. A grep or rg given -e or -f has
// its pattern supplied by that flag, so the first bare operand is then a path
// rather than the pattern.
func newReadScan(argv0 string, args []rawArg) *readScan {
	valueFlags := valueFlagsByArgv0[argv0]
	if valueFlags == nil {
		valueFlags = map[string]bool{}
	}
	hasExprFlag := false
	for _, arg := range args {
		if arg.text == "-e" || arg.text == "-f" ||
			strings.HasPrefix(arg.text, "--regexp=") ||
			strings.HasPrefix(arg.text, "--file=") {
			hasExprFlag = true
		}
	}
	return &readScan{
		argv0:        argv0,
		args:         args,
		valueFlags:   valueFlags,
		patternTaken: hasExprFlag,
		hasExprFlag:  hasExprFlag,
	}
}

// patternTakingSearchers are the commands whose first bare operand is a search
// pattern rather than a path.
var patternTakingSearchers = map[string]bool{
	"grep":     true,
	"egrep":    true,
	"fgrep":    true,
	"rg":       true,
	"ag":       true,
	"ack":      true,
	"git grep": true,
}

// usesPattern reports whether the command takes a leading pattern operand, as
// the grep family and git grep do. Tools like cat or head read every operand
// as a path.
func (scan *readScan) usesPattern() bool {
	return patternTakingSearchers[scan.argv0]
}

// run returns the path operands and whether a recursive flag was seen. It skips
// flags and their separate values, consumes the first bare operand as the
// pattern for a pattern-taking searcher that has no -e/-f, and treats every
// other bare operand as a path.
func (scan *readScan) run() ([]rawArg, bool) {
	paths := make([]rawArg, 0)
	sawRecursive := false
	index := 0
	for index < len(scan.args) {
		arg := scan.args[index]
		if strings.HasPrefix(arg.text, "-") {
			recursive, advance := scan.handleFlag(index)
			if recursive {
				sawRecursive = true
			}
			index += advance
			continue
		}
		if scan.usesPattern() && !scan.patternTaken {
			scan.patternTaken = true
			index++
			continue
		}
		paths = append(paths, arg)
		index++
	}
	return paths, sawRecursive
}

// handleFlag classifies a flag token at index and returns whether it is a
// recursive flag and how many operands it consumes (one for a lone flag, two
// for a value-flag whose value is a separate operand).
func (scan *readScan) handleFlag(index int) (bool, int) {
	arg := scan.args[index]
	recursive := isRecursiveFlag(arg.text)
	if scan.valueFlags[arg.text] {
		if index+1 < len(scan.args) {
			return recursive, 2
		}
		return recursive, 1
	}
	return recursive, 1
}

// isRecursiveFlag reports whether a flag token requests a recursive search,
// covering the short combined forms like -rn as well as -R and --recursive.
func isRecursiveFlag(token string) bool {
	if token == "--recursive" || token == "-R" {
		return true
	}
	if strings.HasPrefix(token, "-") && !strings.HasPrefix(token, "--") {
		return strings.ContainsRune(token, 'r')
	}
	return false
}
