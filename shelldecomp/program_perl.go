package shelldecomp

import "strings"

// perlArgv0 is the argv0 label recorded on the operand read targets a perl
// -n/-p command produces, so a consumer can tell a perl-derived operand read
// from a cat or grep read.
const perlArgv0 = "perl"

// perlOperandReads records the data-file operands a perl command reads. perl
// reads its trailing file operands only under the -n or -p line loop (the
// implicit while(<>) that opens each operand in turn, documented in perlrun);
// without -n or -p perl runs the script and a trailing operand is just an
// argument the script may or may not open, not a read by perl itself. The reads
// surface at the top level with Argv0 "perl", mirroring how a cat or grep
// operand read surfaces, so a consumer folds them through its top-level read
// loop. A perl command without both an -n/-p flag and an -e/-E script
// contributes no operand read here.
func (walk *walker) perlOperandReads(command Command, currentScope *scope) {
	if command.Argv0 != "perl" {
		return
	}
	if !perlHasLoopFlag(command.Args) {
		return
	}
	operands, hasScript := perlDataOperands(command.Args)
	if !hasScript {
		return
	}
	for _, token := range operands {
		walk.result.reads = append(walk.result.reads, perlReadTarget(token, currentScope))
	}
}

// perlHasLoopFlag reports whether any short-flag cluster on the command carries
// the n or p line-loop letter. perl clusters single-letter flags, so -ne, -pe,
// -lne, and -nE all enable the loop and a bare -n or -p enables it too; a long
// flag (one starting with --) is not a single-letter cluster and is skipped.
func perlHasLoopFlag(args []Word) bool {
	for index := range args {
		token := args[index].Text
		if !isPerlShortFlagCluster(token) {
			continue
		}
		if strings.ContainsAny(token[1:], "np") {
			return true
		}
	}
	return false
}

// perlDataOperands returns the bare operands the -n/-p loop reads as input
// files, and whether the command supplies an -e/-E script. The script can be a
// standalone -e flag whose value is the next operand, or a cluster ending in e
// or E (-ne, -lpE) whose script value is likewise the next operand; in both
// cases that next operand is the script body and is skipped, never a read path.
// A non-script short-flag cluster consumes only itself. Each remaining operand
// is collected whether resolvable or not, so an unresolvable operand still
// records a non-resolvable read rather than being silently dropped.
func perlDataOperands(args []Word) ([]Word, bool) {
	operands := make([]Word, 0, len(args))
	hasScript := false
	index := 0
	for index < len(args) {
		token := args[index].Text
		if isPerlShortFlagCluster(token) {
			if perlClusterTakesScript(token) {
				hasScript = true
				index += 2
				continue
			}
			index++
			continue
		}
		if strings.HasPrefix(token, "--") {
			index++
			continue
		}
		operands = append(operands, args[index])
		index++
	}
	return operands, hasScript
}

// perlClusterTakesScript reports whether a short-flag cluster supplies a script
// whose body is the following operand. perl's -e and -E consume the rest of the
// command line up to the next operand as the script, so a cluster ending in e or
// E (a bare -e/-E or a combined -ne/-lpE) is the script-bearing form. An e or E
// in the middle of a cluster does not occur in valid perl flag clustering, since
// -e and -E must be terminal, so testing the last letter is sufficient.
func perlClusterTakesScript(token string) bool {
	last := token[len(token)-1]
	return last == 'e' || last == 'E'
}

// isPerlShortFlagCluster reports whether a token is a perl single-letter flag
// cluster: it starts with a single dash, has at least one letter, and is not a
// long (--) flag or a bare dash.
func isPerlShortFlagCluster(token string) bool {
	if len(token) < 2 {
		return false
	}
	if token[0] != '-' || token[1] == '-' {
		return false
	}
	return true
}

// perlReadTarget resolves one perl data operand against the command cwd into a
// read target labeled "perl". An unresolvable operand or cwd yields a
// non-resolvable target with Path Unresolvable, never a fabricated path,
// matching resolveReadTarget's contract for the generic read scan.
func perlReadTarget(token Word, currentScope *scope) ReadTarget {
	if !token.Resolvable {
		return ReadTarget{
			Path:       Unresolvable,
			Resolvable: false,
			Argv0:      perlArgv0,
			Cwd:        currentScope.cwd,
			Raw:        token.Text,
		}
	}
	resolved := currentScope.resolvePath(token.Value)
	if resolved == Unresolvable || resolved == "" {
		return ReadTarget{
			Path:       Unresolvable,
			Resolvable: false,
			Argv0:      perlArgv0,
			Cwd:        currentScope.cwd,
			Raw:        token.Text,
		}
	}
	return ReadTarget{
		Path:       resolved,
		Resolvable: true,
		Argv0:      perlArgv0,
		Cwd:        currentScope.cwd,
		Raw:        token.Text,
	}
}
