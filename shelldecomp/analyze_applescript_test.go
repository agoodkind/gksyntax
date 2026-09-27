package shelldecomp

import (
	"sort"
	"testing"
)

// applescriptRegion returns the sole LangAppleScript embedded region of a
// decomposition, failing when the count is not exactly one, matching
// sqlRegion in analyze_sql_test.go.
func applescriptRegion(t *testing.T, decomposition *Decomposition) EmbeddedRegion {
	t.Helper()
	regions := decomposition.EmbeddedRegions()
	appleScriptRegions := make([]EmbeddedRegion, 0, len(regions))
	for _, region := range regions {
		if region.Lang == LangAppleScript {
			appleScriptRegions = append(appleScriptRegions, region)
		}
	}
	if len(appleScriptRegions) != 1 {
		t.Fatalf("applescript regions = %d, want 1", len(appleScriptRegions))
	}
	return appleScriptRegions[0]
}

// applescriptRegionReads returns the resolved read-target paths of the sole
// AppleScript region of a decomposition.
func applescriptRegionReads(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := applescriptRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("applescript region should be parsed (analyzer is registered for LangAppleScript)")
	}
	paths := make([]string, 0)
	for _, target := range region.Parsed.ReadTargets() {
		if !target.Resolvable {
			continue
		}
		paths = append(paths, target.Path)
	}
	sort.Strings(paths)
	return paths
}

// applescriptRegionWrites returns the resolved write-target paths of the sole
// AppleScript region of a decomposition.
func applescriptRegionWrites(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := applescriptRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("applescript region should be parsed (analyzer is registered for LangAppleScript)")
	}
	paths := make([]string, 0)
	for _, target := range region.Parsed.WriteTargets() {
		if !target.Resolvable {
			continue
		}
		paths = append(paths, target.Path)
	}
	sort.Strings(paths)
	return paths
}

func TestAppleScriptReadPOSIXFileLiteralRead(t *testing.T) {
	command := `osascript -e 'read POSIX file "/etc/hosts"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := applescriptRegionReads(t, decomposition)
	want := []string{"/etc/hosts"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestAppleScriptReadFileLiteralRead(t *testing.T) {
	command := `osascript -e 'read file "/etc/passwd"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := applescriptRegionReads(t, decomposition)
	want := []string{"/etc/passwd"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestAppleScriptReadAliasLiteralRead(t *testing.T) {
	command := `osascript -e 'read alias "/data/in.txt"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := applescriptRegionReads(t, decomposition)
	want := []string{"/data/in.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestAppleScriptReadRelativePathResolvesAgainstCwd(t *testing.T) {
	command := `osascript -e 'read file "in.txt"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := applescriptRegionReads(t, decomposition)
	want := []string{"/repo/in.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestAppleScriptWriteToFileIsWriteNotRead covers write ... to file, which
// must be recorded as a write target and never appear among reads.
func TestAppleScriptWriteToFileIsWriteNotRead(t *testing.T) {
	command := `osascript -e 'write "data" to file "/data/out.txt"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	reads := applescriptRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for write ... to file", reads)
	}
	writes := applescriptRegionWrites(t, decomposition)
	want := []string{"/data/out.txt"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

// TestAppleScriptWriteToPOSIXFileIsWrite covers write ... to POSIX file,
// the second write shape the brief lists alongside write ... to file.
func TestAppleScriptWriteToPOSIXFileIsWrite(t *testing.T) {
	command := `osascript -e 'write "data" to POSIX file "/data/out2.txt"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	reads := applescriptRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for write ... to POSIX file", reads)
	}
	writes := applescriptRegionWrites(t, decomposition)
	want := []string{"/data/out2.txt"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

// TestAppleScriptHFSColonPathDropped covers a classic Mac HFS colon-separated
// path, which is not a POSIX path. It must be dropped rather than converted.
func TestAppleScriptHFSColonPathDropped(t *testing.T) {
	command := `osascript -e 'read alias "Macintosh HD:Users:x"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := applescriptRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (an HFS colon-style path is dropped, not converted)", got)
	}
}

// TestAppleScriptVariablePathDropped covers a read file target named by a
// variable rather than a string literal, which cannot be pinned without
// guessing.
func TestAppleScriptVariablePathDropped(t *testing.T) {
	command := `osascript -e 'read file thePath'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := applescriptRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a variable path is never resolved, only dropped)", got)
	}
}

// TestAppleScriptConcatenatedPathDropped covers a read file target built by
// concatenating a literal prefix with another expression using &, where the
// literal alone is not the whole path.
func TestAppleScriptConcatenatedPathDropped(t *testing.T) {
	command := `osascript -e 'read file "/tmp/" & suffix'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := applescriptRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a concatenated path is never resolved, only dropped)", got)
	}
}

// TestAppleScriptBackslashLiteralDropped covers a string literal whose raw
// source span carries a backslash. This analyzer applies the same
// conservative rule as analyze_ruby.go's rubyStringLiteralValue and
// analyze_sql.go's sqlCollector: a raw backslash drops the whole literal
// rather than decoding it.
func TestAppleScriptBackslashLiteralDropped(t *testing.T) {
	command := `osascript -e 'read POSIX file "/tmp/a\b"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := applescriptRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a raw backslash in the literal span drops it)", got)
	}
}

// TestAppleScriptDoShellScriptOutOfScope covers do shell script, which this
// analyzer deliberately does not resolve: the shell body it names belongs to
// the embedded shell layer, not to this AppleScript analyzer.
func TestAppleScriptDoShellScriptOutOfScope(t *testing.T) {
	command := `osascript -e 'do shell script "cat /etc/passwd"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := applescriptRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (do shell script is out of scope for this analyzer)", got)
	}
}

func TestAppleScriptArgv0RecordedOnReadTarget(t *testing.T) {
	command := `osascript -e 'read POSIX file "/etc/hosts"'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	region := applescriptRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("applescript region should be parsed")
	}
	found := false
	for _, target := range region.Parsed.ReadTargets() {
		if target.Resolvable && target.Path == "/etc/hosts" && target.Argv0 == "osascript" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an osascript-labeled read of /etc/hosts, got %+v", region.Parsed.ReadTargets())
	}
}

// TestAppleScriptBlockCommentSkipsFalseMatch covers a (* ... *) block comment
// wrapping what would otherwise look like a read statement; the commented-out
// text must not be recorded as a real read.
func TestAppleScriptBlockCommentSkipsFalseMatch(t *testing.T) {
	source := "(* read POSIX file \"/etc/hosts\" *)\nread file \"/data/real.txt\""
	reads, _ := analyzeAppleScript(AnalyzerInput{Source: []byte(source), Cwd: "/repo", Home: "/home/u"})
	got := make([]string, 0, len(reads))
	for _, target := range reads {
		if target.Resolvable {
			got = append(got, target.Path)
		}
	}
	sort.Strings(got)
	want := []string{"/data/real.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v (block-commented read must not be recorded)", got, want)
	}
}
