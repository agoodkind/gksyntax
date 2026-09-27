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
	// twoValueFlags take a name and a value as two separate operands, so
	// skipping only one leaves the value bare and shifts every operand after
	// it by one position.
	twoValueFlags map[string]bool
	// fileValuedFlags are the subset whose second value is a file the command
	// reads, so it is a real read target rather than something to skip.
	fileValuedFlags map[string]bool
}

// exprFlagsByArgv0 lists the flags that supply a command's pattern or inline
// program, so its first bare operand is then a path rather than the program.
//
// The set is per-tool because the same spelling means different things. grep
// and rg take their pattern from -e; jq's -e sets the exit status from the
// output and supplies nothing, so treating it as an expression flag consumed
// jq's real program as a path and dropped the input file behind it.
var exprFlagsByArgv0 = map[string]map[string]bool{
	"grep":     {"-e": true, "-f": true},
	"egrep":    {"-e": true, "-f": true},
	"fgrep":    {"-e": true, "-f": true},
	"rg":       {"-e": true, "-f": true},
	"ag":       {"-e": true, "-f": true},
	"ack":      {"-e": true, "-f": true},
	"git grep": {"-e": true, "-f": true},
	"sed":      {"-e": true, "-f": true},
	"awk":      {"-f": true},
	"gawk":     {"-f": true},
	// jq's program comes from -f or --from-file only. -e is the exit-status
	// flag and -r, -c, -n and the rest supply no program either.
	"jq": {"-f": true, "--from-file": true},
}

// exprFlagPrefixesByArgv0 lists the joined spellings of the same flags, where
// the value is part of the token (--regexp=x) rather than a separate operand.
var exprFlagPrefixesByArgv0 = map[string][]string{
	"grep":     {"--regexp=", "--file="},
	"egrep":    {"--regexp=", "--file="},
	"fgrep":    {"--regexp=", "--file="},
	"rg":       {"--regexp=", "--file="},
	"ag":       {"--regexp=", "--file="},
	"ack":      {"--regexp=", "--file="},
	"git grep": {"--regexp=", "--file="},
	"sed":      {"--expression=", "--file="},
	"awk":      {"--file="},
	"gawk":     {"--file="},
	"jq":       {"--from-file="},
}

// newReadScan builds a scan for one command. A command given one of its own
// expression flags has its pattern or program supplied by that flag, so the
// first bare operand is then a path rather than the program.
func newReadScan(argv0 string, args []rawArg) *readScan {
	valueFlags := valueFlagsByArgv0[argv0]
	if valueFlags == nil {
		valueFlags = map[string]bool{}
	}
	exprFlags := exprFlagsByArgv0[argv0]
	exprPrefixes := exprFlagPrefixesByArgv0[argv0]
	hasExprFlag := false
	for _, arg := range args {
		if exprFlags[arg.text] {
			hasExprFlag = true
			continue
		}
		for _, prefix := range exprPrefixes {
			if strings.HasPrefix(arg.text, prefix) {
				hasExprFlag = true
				break
			}
		}
	}
	twoValueFlags := twoValueFlagsByArgv0[argv0]
	if twoValueFlags == nil {
		twoValueFlags = map[string]bool{}
	}
	fileValuedFlags := fileValuedFlagsByArgv0[argv0]
	if fileValuedFlags == nil {
		fileValuedFlags = map[string]bool{}
	}
	return &readScan{
		argv0:           argv0,
		args:            args,
		valueFlags:      valueFlags,
		patternTaken:    hasExprFlag,
		hasExprFlag:     hasExprFlag,
		twoValueFlags:   twoValueFlags,
		fileValuedFlags: fileValuedFlags,
	}
}

// twoValueFlagsByArgv0 lists flags that take two separate operands. jq's --arg
// binds a name to a value, and --slurpfile and --rawfile bind a name to a file.
var twoValueFlagsByArgv0 = map[string]map[string]bool{
	"jq": {
		"--arg": true, "--argjson": true,
		"--slurpfile": true, "--rawfile": true,
	},
}

// fileValuedFlagsByArgv0 lists the two-value flags whose second operand is a
// file the command reads, so it belongs in the read targets rather than being
// skipped with the name that precedes it.
var fileValuedFlagsByArgv0 = map[string]map[string]bool{
	"jq": {"--slurpfile": true, "--rawfile": true},
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

// programTakingTools are the commands whose first bare operand is an inline
// program rather than a path: awk's pattern-action script and sed's editing
// script. A literal script like '{print $1}' or 's/a/b/' must not be resolved as
// a read path. When the script is supplied through -e or -f instead, the first
// bare operand is a data file, which the -e/-f handling already accounts for.
var programTakingTools = map[string]bool{
	"awk":  true,
	"gawk": true,
	"sed":  true,
	"jq":   true,
}

// usesPattern reports whether the command consumes its first bare operand as a
// pattern or an inline program rather than a path: the grep family and git grep
// take a pattern, and awk and sed take a script. Tools like cat or head read
// every operand as a path.
func (scan *readScan) usesPattern() bool {
	return patternTakingSearchers[scan.argv0] || programTakingTools[scan.argv0]
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
			outcome := scan.handleFlag(index)
			if outcome.recursive {
				sawRecursive = true
			}
			if outcome.dataPath != nil {
				paths = append(paths, *outcome.dataPath)
			}
			index += outcome.advance
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

// flagOutcome is what one flag token contributed: whether it requested a
// recursive search, how many operands it consumed, and the data file it named
// when its final value is one.
type flagOutcome struct {
	recursive bool
	advance   int
	dataPath  *rawArg
}

// handleFlag classifies a flag token at index. A lone flag consumes one
// operand, a value-flag consumes its separate value too, and a two-value flag
// consumes both.
//
// Getting the count wrong shifts every later operand by one: jq's `--arg x 1`
// left `1` bare, which the program-taking rule then consumed as jq's program,
// so the real program `.x` resolved as a path that exists nowhere. A fabricated
// path both hides the real read and hands a phantom target to anything keyed on
// read targets.
func (scan *readScan) handleFlag(index int) flagOutcome {
	arg := scan.args[index]
	recursive := isRecursiveFlag(arg.text)

	if scan.twoValueFlags[arg.text] {
		if index+2 < len(scan.args) {
			outcome := flagOutcome{recursive: recursive, advance: 3, dataPath: nil}
			// The second value of a file-valued flag is data the command reads,
			// unlike the name that precedes it.
			if scan.fileValuedFlags[arg.text] {
				outcome.dataPath = &scan.args[index+2]
			}
			return outcome
		}
		// A truncated invocation names no complete pair, so nothing after it can
		// be classified; consume what is left rather than guessing.
		return flagOutcome{recursive: recursive, advance: len(scan.args) - index, dataPath: nil}
	}

	if scan.valueFlags[arg.text] {
		if index+1 < len(scan.args) {
			return flagOutcome{recursive: recursive, advance: 2, dataPath: nil}
		}
		return flagOutcome{recursive: recursive, advance: 1, dataPath: nil}
	}
	return flagOutcome{recursive: recursive, advance: 1, dataPath: nil}
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
