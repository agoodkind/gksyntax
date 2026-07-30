package shelldecomp

import (
	"strings"
	"testing"
)

// onlyRegion returns the sole embedded region of a decomposition, failing the
// test when there is not exactly one.
func onlyRegion(t *testing.T, decomposition *Decomposition) EmbeddedRegion {
	t.Helper()
	regions := decomposition.EmbeddedRegions()
	if len(regions) != 1 {
		t.Fatalf("embedded regions = %d, want 1", len(regions))
	}
	return regions[0]
}

func TestXargsEmbeddingIsParsedShell(t *testing.T) {
	decomposition := Parse(`xargs grep PATTERN`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangShell {
		t.Fatalf("xargs region lang = %v, want LangShell", region.Lang)
	}
	if region.Parsed == nil {
		t.Fatal("xargs embedding should be parsed (shell grammar exists)")
	}
}

func TestFindExecEmbeddingIsParsedShell(t *testing.T) {
	decomposition := Parse(`find . -name '*.go' -exec grep TODO {} \;`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangShell {
		t.Fatalf("find -exec region lang = %v, want LangShell", region.Lang)
	}
	if region.Parsed == nil {
		t.Fatal("find -exec embedding should be parsed (shell grammar exists)")
	}
	if !hasCommand(region.Parsed, "grep") {
		t.Fatal("find -exec embedding should contain the grep command")
	}
	if strings.Contains(region.Text, "{}") {
		t.Fatalf("find -exec embedding text %q should drop the {} placeholder", region.Text)
	}
}

func TestDockerRunEmbeddingIsParsedShellNewScope(t *testing.T) {
	decomposition := Parse(`docker run img grep x /etc/passwd`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangShell {
		t.Fatalf("docker region lang = %v, want LangShell", region.Lang)
	}
	if !region.NewScope {
		t.Fatal("docker embedding should start a new scope")
	}
	if region.Parsed == nil {
		t.Fatal("docker embedding should be parsed (shell grammar exists)")
	}
}

func TestParallelEmbeddingIsParsedShell(t *testing.T) {
	decomposition := Parse(`parallel grep x ::: a b`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangShell {
		t.Fatalf("parallel region lang = %v, want LangShell", region.Lang)
	}
	if region.Parsed == nil {
		t.Fatal("parallel embedding should be parsed (shell grammar exists)")
	}
}

func TestPerlEmbeddingIsParsedPerl(t *testing.T) {
	decomposition := Parse(`perl -e "print 1"`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangPerl {
		t.Fatalf("perl region lang = %v, want LangPerl", region.Lang)
	}
	if region.Parsed == nil {
		t.Fatal("perl embedding should be parsed (perl grammar exists)")
	}
}

func TestNodeEmbeddingIsParsedJavaScript(t *testing.T) {
	decomposition := Parse(`node -e "console.log(1)"`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangJavaScript {
		t.Fatalf("node region lang = %v, want LangJavaScript", region.Lang)
	}
	if region.Parsed == nil {
		t.Fatal("node embedding should be parsed (javascript grammar exists)")
	}
}

func TestRubyEmbeddingIsParsedRuby(t *testing.T) {
	decomposition := Parse(`ruby -e "puts 1"`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangRuby {
		t.Fatalf("ruby region lang = %v, want LangRuby", region.Lang)
	}
	if region.Parsed == nil {
		t.Fatal("ruby embedding should be parsed (ruby grammar exists)")
	}
}

func TestOsascriptEmbeddingIsOpaque(t *testing.T) {
	decomposition := Parse(`osascript -e 'display dialog "hi"'`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangAppleScript {
		t.Fatalf("osascript region lang = %v, want LangAppleScript", region.Lang)
	}
	if region.Parsed != nil {
		t.Fatal("osascript embedding should be opaque (no grammar, nil Parsed)")
	}
}

func TestSqlite3EmbeddingIsParsed(t *testing.T) {
	decomposition := Parse(`sqlite3 db.sqlite "select 1"`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangSQL {
		t.Fatalf("sqlite3 region lang = %v, want LangSQL", region.Lang)
	}
	if region.Parsed == nil {
		t.Fatal("sqlite3 embedding should be parsed (LangSQL has a registered text-scan analyzer, even with no grammar)")
	}
}

// TestSqlite3MultilineEmbeddingPreservesNewline covers the reported bug: a
// multi-line double-quoted sqlite3 script reached through Parse, the path a
// real caller uses (dispatchSqlite3 reads the argument's already-resolved
// Word.Value, produced by literalStringValue). Before the fix, the newline
// between the two SQL statements was dropped, fusing "mytable" and "ATTACH"
// into "mytableATTACH". The fix must preserve the newline exactly.
func TestSqlite3MultilineEmbeddingPreservesNewline(t *testing.T) {
	command := "sqlite3 db.sqlite \".import /data/in.csv mytable\nATTACH DATABASE '/other/aux.db' AS aux;\""
	decomposition := Parse(command, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangSQL {
		t.Fatalf("sqlite3 region lang = %v, want LangSQL", region.Lang)
	}
	want := ".import /data/in.csv mytable\nATTACH DATABASE '/other/aux.db' AS aux;"
	if region.Text != want {
		t.Fatalf("sqlite3 region text = %q, want %q", region.Text, want)
	}
}

func TestAwkEmbeddingIsParsed(t *testing.T) {
	decomposition := Parse(`awk '{print}' f`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangAwk {
		t.Fatalf("awk region lang = %v, want LangAwk", region.Lang)
	}
	if region.Parsed == nil {
		t.Fatal("awk embedding should be parsed (awk grammar exists)")
	}
}

func TestAwkInplaceWrites(t *testing.T) {
	decomposition := Parse(`awk -i inplace '{print}' f`, "/w", "/home/u")
	writes := decomposition.WriteTargets()
	if len(writes) != 1 {
		t.Fatalf("write targets = %d, want 1", len(writes))
	}
	if !writes[0].Resolvable || writes[0].Path != "/w/f" {
		t.Fatalf("write target = %+v, want resolvable /w/f", writes[0])
	}
}

func TestJQEmbeddingIsOpaque(t *testing.T) {
	decomposition := Parse(`jq '.x' file.json`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangJQ {
		t.Fatalf("jq region lang = %v, want LangJQ", region.Lang)
	}
	if region.Parsed != nil {
		t.Fatal("jq embedding should be opaque (no grammar, nil Parsed)")
	}
}

func TestSshEmbeddingIsParsedShellNewScope(t *testing.T) {
	decomposition := Parse(`ssh host 'grep TOKEN ~/.config/x'`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangShell {
		t.Fatalf("ssh region lang = %v, want LangShell", region.Lang)
	}
	if !region.NewScope {
		t.Fatal("ssh embedding should start a new scope")
	}
	if region.Parsed == nil {
		t.Fatal("ssh embedding should be parsed (shell grammar exists)")
	}
}

func TestEnvSudoWrapperStripping(t *testing.T) {
	decomposition := Parse(`env FOO=1 sudo grep x /etc/shadow`, "/w", "/home/u")
	grep, found := findCommand(decomposition, "grep")
	if !found {
		t.Fatal("expected the wrapped grep command after stripping env and sudo")
	}
	if grep.Kind != CommandKindSearch {
		t.Fatalf("grep kind = %q, want %q", grep.Kind, CommandKindSearch)
	}
	reads := decomposition.ReadTargets()
	if len(reads) != 1 || !reads[0].Resolvable || reads[0].Path != "/etc/shadow" {
		t.Fatalf("read targets = %+v, want one resolvable /etc/shadow", reads)
	}
}

func TestDeepNestStopsAtMaxDepth(t *testing.T) {
	command := buildHeredocNest(10)
	decomposition := Parse(command, "/w", "/home/u")
	depth := nestDepth(decomposition)
	if depth > DefaultMaxDepth {
		t.Fatalf("nest depth = %d, want at most DefaultMaxDepth %d (recursion must stop)", depth, DefaultMaxDepth)
	}
	if depth < DefaultMaxDepth-1 {
		t.Fatalf("nest depth = %d, want the recursion to descend near DefaultMaxDepth %d", depth, DefaultMaxDepth)
	}
}

// buildHeredocNest builds a command nesting `levels` heredoc-fed bash shells,
// each body being the next level in, ending in a grep. Each heredoc gives one
// clean quoting context, so the nest descends without quote-escaping collapse
// and lets the depth cutoff be observed rather than a parser breakdown.
func buildHeredocNest(levels int) string {
	body := "grep secret /etc/shadow"
	for level := levels; level >= 1; level-- {
		delimiter := "LVL" + string(rune('A'+level%26))
		body = "bash <<" + delimiter + "\n" + body + "\n" + delimiter
	}
	return body
}

// nestDepth measures how many levels of parsed shell embeddings a decomposition
// nests, walking only the first parsed region at each level. It bounds its own
// recursion well above DefaultMaxDepth so a runaway nest is observable as a too
// deep result rather than a hang.
func nestDepth(decomposition *Decomposition) int {
	const guard = 50
	depth := 0
	current := decomposition
	for depth < guard {
		var nextParsed *Decomposition
		for _, region := range current.EmbeddedRegions() {
			if region.Parsed != nil {
				nextParsed = region.Parsed
				break
			}
		}
		if nextParsed == nil {
			return depth
		}
		depth++
		current = nextParsed
	}
	return depth
}

func TestUnparseableIsOpaque(t *testing.T) {
	decomposition := Parse("", "/w", "/home/u")
	if !decomposition.IsOpaque() {
		t.Fatal("empty input should be opaque")
	}
	if len(decomposition.Commands()) != 0 {
		t.Fatal("opaque decomposition should have no classified commands")
	}
}

func TestEffectiveCwdAtTracksCd(t *testing.T) {
	command := `cd /a && cat one && cd /b && cat two`
	decomposition := Parse(command, "/start", "/home/u")
	firstCat, found := findCommand(decomposition, "cat")
	if !found {
		t.Fatal("expected cat commands")
	}
	if cwd := decomposition.EffectiveCwdAt(firstCat.Node.StartByte); cwd != "/a" {
		t.Fatalf("cwd at first cat = %q, want /a", cwd)
	}
	lastByte := uint(len(command) - 1)
	if cwd := decomposition.EffectiveCwdAt(lastByte); cwd != "/b" {
		t.Fatalf("cwd at end = %q, want /b", cwd)
	}
}
