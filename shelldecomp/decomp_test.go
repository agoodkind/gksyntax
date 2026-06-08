package shelldecomp

import (
	"testing"
)

// findCommand returns the first command with the given argv0 and whether one
// was found, so a test can assert on a specific stage of a pipeline or list.
func findCommand(decomposition *Decomposition, argv0 string) (Command, bool) {
	for _, command := range decomposition.Commands() {
		if command.Argv0 == argv0 {
			return command, true
		}
	}
	var zero Command
	return zero, false
}

// hasCommand reports whether any command in the decomposition has the argv0.
func hasCommand(decomposition *Decomposition, argv0 string) bool {
	_, found := findCommand(decomposition, argv0)
	return found
}

func TestCdExpansionPoisonsGrepCwdAndReads(t *testing.T) {
	decomposition := Parse(`cd "$VAR" && grep -rn X .`, "/start", "/home/u")
	grep, found := findCommand(decomposition, "grep")
	if !found {
		t.Fatal("expected a grep command")
	}
	if grep.Cwd != Unresolvable {
		t.Fatalf("grep cwd = %q, want Unresolvable", grep.Cwd)
	}
	if len(decomposition.ReadTargets()) == 0 {
		t.Fatal("expected at least one read target")
	}
	for _, target := range decomposition.ReadTargets() {
		if target.Resolvable {
			t.Fatalf("read target %q is resolvable, want unresolvable", target.Path)
		}
		if target.Path != Unresolvable {
			t.Fatalf("read target path = %q, want Unresolvable", target.Path)
		}
	}
}

func TestCdAbsoluteResolvesGrepCwdAndReads(t *testing.T) {
	decomposition := Parse(`cd /indexed/repo && grep -rn X .`, "/start", "/home/u")
	grep, found := findCommand(decomposition, "grep")
	if !found {
		t.Fatal("expected a grep command")
	}
	if grep.Cwd != "/indexed/repo" {
		t.Fatalf("grep cwd = %q, want /indexed/repo", grep.Cwd)
	}
	reads := decomposition.ReadTargets()
	if len(reads) != 1 {
		t.Fatalf("read targets = %d, want 1", len(reads))
	}
	if !reads[0].Resolvable || reads[0].Path != "/indexed/repo" {
		t.Fatalf("read target = %+v, want resolvable /indexed/repo", reads[0])
	}
}

func TestHeredocBodyIsDataNotCommands(t *testing.T) {
	decomposition := Parse("cat <<'EOF'\ngrep secret file\nEOF", "/w", "/home/u")
	if !hasCommand(decomposition, "cat") {
		t.Fatal("expected a cat command")
	}
	if hasCommand(decomposition, "grep") {
		t.Fatal("grep inside the heredoc body must not be a live command")
	}
	regions := decomposition.EmbeddedRegions()
	if len(regions) != 1 {
		t.Fatalf("embedded regions = %d, want 1", len(regions))
	}
	if regions[0].Lang != LangOpaque {
		t.Fatalf("heredoc region lang = %v, want LangOpaque", regions[0].Lang)
	}
	if regions[0].Parsed != nil {
		t.Fatal("a cat heredoc body must stay opaque (nil Parsed)")
	}
}

func TestGitGrepLabelAndTarget(t *testing.T) {
	decomposition := Parse(`git grep "x"`, "/repo", "/home/u")
	reads := decomposition.ReadTargets()
	if len(reads) != 1 {
		t.Fatalf("read targets = %d, want 1", len(reads))
	}
	if reads[0].Argv0 != "git grep" {
		t.Fatalf("read target argv0 = %q, want \"git grep\"", reads[0].Argv0)
	}
	if !reads[0].Resolvable || reads[0].Path != "/repo" {
		t.Fatalf("read target = %+v, want resolvable /repo", reads[0])
	}
}

func TestGrepWithExplicitPath(t *testing.T) {
	decomposition := Parse(`grep -nE "x" /tmp/log`, "/w", "/home/u")
	reads := decomposition.ReadTargets()
	if len(reads) != 1 {
		t.Fatalf("read targets = %d, want 1", len(reads))
	}
	if !reads[0].Resolvable || reads[0].Path != "/tmp/log" {
		t.Fatalf("read target = %+v, want resolvable /tmp/log", reads[0])
	}
}

func TestPipelineGrepReadsStdin(t *testing.T) {
	decomposition := Parse(`find Tests | grep -iE x`, "/w", "/home/u")
	for _, target := range decomposition.ReadTargets() {
		if target.Argv0 == "grep" {
			t.Fatalf("piped grep should have no path target, got %+v", target)
		}
	}
}

func TestSubshellExpansionPoisonsCwd(t *testing.T) {
	decomposition := Parse(`( cd "$1" && grep x . )`, "/work", "/home/u")
	grep, found := findCommand(decomposition, "grep")
	if !found {
		t.Fatal("expected a grep command")
	}
	if grep.Cwd != Unresolvable {
		t.Fatalf("grep cwd = %q, want Unresolvable (no fabricated /work/$1)", grep.Cwd)
	}
	for _, target := range decomposition.ReadTargets() {
		if target.Resolvable {
			t.Fatalf("read target %q is resolvable, want unresolvable", target.Path)
		}
	}
}

func TestTildeExpandsInCwdAndRead(t *testing.T) {
	decomposition := Parse(`cd ~/proj && cat f`, "/start", "/home/u")
	cat, found := findCommand(decomposition, "cat")
	if !found {
		t.Fatal("expected a cat command")
	}
	if cat.Cwd != "/home/u/proj" {
		t.Fatalf("cat cwd = %q, want /home/u/proj", cat.Cwd)
	}
	reads := decomposition.ReadTargets()
	if len(reads) != 1 {
		t.Fatalf("read targets = %d, want 1", len(reads))
	}
	if !reads[0].Resolvable || reads[0].Path != "/home/u/proj/f" {
		t.Fatalf("read target = %+v, want resolvable /home/u/proj/f", reads[0])
	}
}

func TestPythonDashCParsesEmbedded(t *testing.T) {
	decomposition := Parse(`python -c "import os"`, "/w", "/home/u")
	regions := decomposition.EmbeddedRegions()
	if len(regions) != 1 {
		t.Fatalf("embedded regions = %d, want 1", len(regions))
	}
	if regions[0].Lang != LangPython {
		t.Fatalf("region lang = %v, want LangPython", regions[0].Lang)
	}
	if regions[0].Parsed == nil {
		t.Fatal("python embedding should have a non-nil Parsed (grammar exists)")
	}
}

func TestBashDashCNestedShellScope(t *testing.T) {
	decomposition := Parse(`bash -c "cd /x && grep y ."`, "/w", "/home/u")
	regions := decomposition.EmbeddedRegions()
	if len(regions) != 1 {
		t.Fatalf("embedded regions = %d, want 1", len(regions))
	}
	if !regions[0].NewScope {
		t.Fatal("bash -c embedding should start a new scope")
	}
	if regions[0].Parsed == nil {
		t.Fatal("bash -c embedding should be parsed (shell grammar exists)")
	}
	innerGrep, found := findCommand(regions[0].Parsed, "grep")
	if !found {
		t.Fatal("expected a grep command inside the nested shell")
	}
	if innerGrep.Cwd != "/x" {
		t.Fatalf("nested grep cwd = %q, want /x", innerGrep.Cwd)
	}
}

func TestSedInplaceWrite(t *testing.T) {
	decomposition := Parse(`sed -i 's/a/b/' file.txt`, "/w", "/home/u")
	writes := decomposition.WriteTargets()
	if len(writes) != 1 {
		t.Fatalf("write targets = %d, want 1", len(writes))
	}
	if !writes[0].Resolvable || writes[0].Path != "/w/file.txt" {
		t.Fatalf("write target = %+v, want resolvable /w/file.txt", writes[0])
	}
}

func TestRedirectAppendWrite(t *testing.T) {
	decomposition := Parse(`echo hi >> /tmp/log`, "/w", "/home/u")
	writes := decomposition.WriteTargets()
	if len(writes) != 1 {
		t.Fatalf("write targets = %d, want 1", len(writes))
	}
	if !writes[0].Resolvable || writes[0].Path != "/tmp/log" {
		t.Fatalf("write target = %+v, want resolvable /tmp/log", writes[0])
	}
}
