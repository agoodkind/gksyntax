package shelldecomp

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// handleFileRedirect records a write target for an output redirect. A redirect
// with a descriptor that points at another descriptor (2>&1) names no file and
// is skipped; a redirect to a literal file is a resolvable write target; a
// redirect to an expansion is an unresolvable write target.
func (walk *walker) handleFileRedirect(node *tree_sitter.Node, command Command, currentScope *scope) {
	if !isOutputRedirect(node) {
		return
	}
	destination := node.ChildByFieldName("destination")
	if destination == nil {
		return
	}
	if destination.Kind() == "number" || destination.Kind() == "file_descriptor" {
		return
	}
	token := resolveArgNode(destination, walk.source)
	walk.result.writes = append(walk.result.writes, resolveWriteTarget(command.Argv0, token, currentScope))
}

// outputRedirectOps are the file_redirect operator tokens that write a file.
var outputRedirectOps = map[redirectOp]bool{
	opRedirectOut:       true,
	opRedirectAppend:    true,
	opRedirectAll:       true,
	opRedirectAllAmp:    true,
	opRedirectAllAppend: true,
}

// isOutputRedirect reports whether a file_redirect writes to a file, covering
// >, >>, and &>. An input redirect (<) or a here-string does not write.
func isOutputRedirect(node *tree_sitter.Node) bool {
	for index := range node.ChildCount() {
		child := node.Child(index)
		if child == nil || child.IsNamed() {
			continue
		}
		if outputRedirectOps[redirectOp(child.Kind())] {
			return true
		}
	}
	return false
}

// collectInlineWrites records write targets a command names through its own
// operands rather than a redirect: tee files, dd of=, sed -i, awk -i inplace,
// patch <file>, and git apply <file>.
func (walk *walker) collectInlineWrites(command Command, args []rawArg, currentScope *scope) {
	argv0 := command.Argv0
	if argv0 == "tee" {
		walk.teeWrites(argv0, args, currentScope)
		return
	}
	if argv0 == "dd" {
		walk.ddWrite(argv0, args, currentScope)
		return
	}
	if argv0 == "sed" {
		walk.sedInplaceWrite(command, args, currentScope)
		return
	}
	if argv0 == "awk" || argv0 == "gawk" {
		walk.awkInplaceWrite(command, args, currentScope)
		return
	}
	if argv0 == "patch" {
		walk.patchWrite(argv0, args, currentScope)
		return
	}
	if argv0 == "git" {
		walk.gitApplyWrite(args, currentScope)
	}
}

// teeWrites records each non-flag operand of tee as a write target.
func (walk *walker) teeWrites(argv0 string, args []rawArg, currentScope *scope) {
	for _, arg := range args {
		if strings.HasPrefix(arg.text, "-") {
			continue
		}
		walk.result.writes = append(walk.result.writes, resolveWriteTarget(argv0, arg, currentScope))
	}
}

// ddWrite records the of= operand of dd as a write target.
func (walk *walker) ddWrite(argv0 string, args []rawArg, currentScope *scope) {
	for _, arg := range args {
		if !strings.HasPrefix(arg.text, "of=") {
			continue
		}
		value := strings.TrimPrefix(arg.text, "of=")
		token := rawArg{
			text:       value,
			value:      value,
			resolvable: arg.resolvable && !containsExpansion(value),
			startByte:  arg.startByte,
			endByte:    arg.endByte,
		}
		walk.result.writes = append(walk.result.writes, resolveWriteTarget(argv0, token, currentScope))
	}
}

// sedInplaceWrite records sed's target files as write targets when an in-place
// flag (-i or -i.bak) is present. Without -i, sed only reads.
func (walk *walker) sedInplaceWrite(command Command, args []rawArg, currentScope *scope) {
	if !hasInPlaceSedFlag(args) {
		return
	}
	walk.scriptToolWrites(command.Argv0, args, currentScope)
}

// awkInplaceWrite records awk's target files as write targets when the gawk
// in-place extension (-i inplace) is requested.
func (walk *walker) awkInplaceWrite(command Command, args []rawArg, currentScope *scope) {
	if !hasInPlaceAwkFlag(args) {
		return
	}
	walk.scriptToolWrites(command.Argv0, args, currentScope)
}

// scriptToolWrites records the file operands of a sed or awk command as write
// targets, skipping flags, the flag value of -i inplace, and the script
// operand. The script is the first bare operand and is not a file.
func (walk *walker) scriptToolWrites(argv0 string, args []rawArg, currentScope *scope) {
	scriptTaken := false
	index := 0
	for index < len(args) {
		arg := args[index]
		if strings.HasPrefix(arg.text, "-") {
			if arg.text == "-i" && index+1 < len(args) && args[index+1].text == "inplace" {
				index += 2
				continue
			}
			index++
			continue
		}
		if !scriptTaken {
			scriptTaken = true
			index++
			continue
		}
		walk.result.writes = append(walk.result.writes, resolveWriteTarget(argv0, arg, currentScope))
		index++
	}
}

// hasInPlaceSedFlag reports whether sed's operands request in-place editing
// through -i or a suffixed -i.bak form.
func hasInPlaceSedFlag(args []rawArg) bool {
	for _, arg := range args {
		if arg.text == "-i" || strings.HasPrefix(arg.text, "-i.") || strings.HasPrefix(arg.text, "--in-place") {
			return true
		}
	}
	return false
}

// hasInPlaceAwkFlag reports whether awk's operands request the gawk in-place
// extension through -i inplace.
func hasInPlaceAwkFlag(args []rawArg) bool {
	for index := range args {
		if args[index].text == "-i" && index+1 < len(args) && args[index+1].text == "inplace" {
			return true
		}
	}
	return false
}

// patchWrite records the file patch edits as a write target. The file is the
// last non-flag operand, since patch reads the diff from stdin or a -i file but
// writes the named target.
func (walk *walker) patchWrite(argv0 string, args []rawArg, currentScope *scope) {
	target, found := lastNonFlag(args)
	if !found {
		return
	}
	walk.result.writes = append(walk.result.writes, resolveWriteTarget(argv0, target, currentScope))
}

// gitApplyWrite records the patch file of git apply. git apply rewrites the
// files named in the diff, which are not pinnable from the command line, so the
// recorded write target is the diff path with the argv0 marked "git apply".
func (walk *walker) gitApplyWrite(args []rawArg, currentScope *scope) {
	if len(args) == 0 || !args[0].resolvable || args[0].value != "apply" {
		return
	}
	target, found := lastNonFlagFrom(args, 1)
	if !found {
		return
	}
	walk.result.writes = append(walk.result.writes, resolveWriteTarget("git apply", target, currentScope))
}

// lastNonFlag returns the last operand that is not a flag.
func lastNonFlag(args []rawArg) (rawArg, bool) {
	return lastNonFlagFrom(args, 0)
}

// lastNonFlagFrom returns the last operand at or after start that is not a flag.
func lastNonFlagFrom(args []rawArg, start int) (rawArg, bool) {
	for index := len(args) - 1; index >= start; index-- {
		if !strings.HasPrefix(args[index].text, "-") {
			return args[index], true
		}
	}
	var zero rawArg
	return zero, false
}

// resolveWriteTarget resolves one path operand against the command cwd into a
// write target. An unresolvable operand or cwd yields a non-resolvable target
// with Path set to Unresolvable, so a consumer can default-deny rather than act
// on a fabricated path.
func resolveWriteTarget(argv0 string, token rawArg, currentScope *scope) WriteTarget {
	if !token.resolvable {
		return WriteTarget{
			Path:       Unresolvable,
			Resolvable: false,
			Argv0:      argv0,
			Cwd:        currentScope.cwd,
			Raw:        token.text,
		}
	}
	resolved := currentScope.resolvePath(token.value)
	if resolved == Unresolvable {
		return WriteTarget{
			Path:       Unresolvable,
			Resolvable: false,
			Argv0:      argv0,
			Cwd:        currentScope.cwd,
			Raw:        token.text,
		}
	}
	return WriteTarget{
		Path:       resolved,
		Resolvable: true,
		Argv0:      argv0,
		Cwd:        currentScope.cwd,
		Raw:        token.text,
	}
}
