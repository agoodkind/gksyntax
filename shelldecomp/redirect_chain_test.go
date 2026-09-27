package shelldecomp

import (
	"testing"
)

// A trailing redirect binds to the whole preceding chain in tree-sitter-bash, so
// `cd X && git ... 2>&1` parses as a redirected_statement whose body is the &&
// list. The walker must recurse into that compound body so the cd still applies;
// otherwise the chain collapses to one empty command and the cwd model is lost.
func TestRedirectOnChainKeepsCdEffect(t *testing.T) {
	cases := []string{
		"cd /repo/wt && git restore F 2>&1",
		"cd /repo/wt && git restore F 2>&1; echo done",
		"cd /repo/wt && git add -A > /dev/null 2>&1",
	}
	for _, command := range cases {
		decomposition := Parse(command, "/base", "/home/u")
		git, found := findCommand(decomposition, "git")
		if !found {
			t.Fatalf("%q: expected a git command, got %+v", command, decomposition.Commands())
		}
		if git.Cwd != "/repo/wt" {
			t.Fatalf("%q: git cwd = %q, want /repo/wt", command, git.Cwd)
		}
		if got := decomposition.EffectiveCwdAt(uint(len(command))); got != "/repo/wt" {
			t.Fatalf("%q: EffectiveCwdAt(end) = %q, want /repo/wt", command, got)
		}
	}
}

// The redirect target itself is still recorded as a write when the body is a
// compound chain, resolved against the cwd the cd established. The redirect
// binds to the last command in the chain (git diff), which runs in the cd dir.
func TestRedirectOnChainRecordsWriteTarget(t *testing.T) {
	decomposition := Parse("cd /repo/wt && git diff > out.txt", "/base", "/home/u")
	writes := decomposition.WriteTargets()
	found := false
	for _, write := range writes {
		if write.Path == "/repo/wt/out.txt" && write.Argv0 == "git" && write.Cwd == "/repo/wt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected git write target /repo/wt/out.txt in cwd /repo/wt, got %+v", writes)
	}
}

// A redirect on a brace group applies to the whole group and is set up in the
// cwd before the group runs, so a cd inside the group must not move the redirect
// target. Recursing into a group body (rather than a list/pipeline) would wrongly
// resolve out.txt against the cd'd dir; the group must keep the original cwd.
func TestRedirectOnGroupKeepsOuterCwd(t *testing.T) {
	decomposition := Parse("{ cd /tmp; echo hi; } > out.txt", "/base", "/home/u")
	found := false
	for _, write := range decomposition.WriteTargets() {
		if write.Path == "/tmp/out.txt" {
			t.Fatalf("group redirect wrongly resolved to /tmp/out.txt: %+v", decomposition.WriteTargets())
		}
		if write.Path == "/base/out.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected group redirect write target /base/out.txt, got %+v", decomposition.WriteTargets())
	}
}
