package shelldecomp

import (
	"sort"
	"testing"
)

// phpRegionReads returns the resolved read-target paths of the sole php
// embedded region of a decomposition, failing when there is not exactly one
// php region. It reads through EmbeddedRegions()[i].Parsed.ReadTargets() so
// the test asserts on the region's own reads, not a flattened top-level list.
func phpRegionReads(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := phpRegion(t, decomposition)
	if region.Parsed == nil {
		return nil
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

// phpRegionWrites returns the resolved write-target paths of the sole php
// region of a decomposition.
func phpRegionWrites(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := phpRegion(t, decomposition)
	if region.Parsed == nil {
		return nil
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

// phpRegion returns the sole php embedded region of a decomposition, failing
// the test when the count is not exactly one.
func phpRegion(t *testing.T, decomposition *Decomposition) EmbeddedRegion {
	t.Helper()
	regions := decomposition.EmbeddedRegions()
	phpRegions := make([]EmbeddedRegion, 0, len(regions))
	for _, region := range regions {
		if region.Lang == LangPHP {
			phpRegions = append(phpRegions, region)
		}
	}
	if len(phpRegions) != 1 {
		t.Fatalf("php regions = %d, want 1", len(phpRegions))
	}
	return phpRegions[0]
}

// TestPHPDashRProducesParsedPHPRegion is the dispatch-level check the task
// calls out by name: before this task, php had no Lang constant, no
// dispatcher, and no grammar registration, so a `php -r` command produced no
// embedded region at all. This asserts the region now exists, is tagged
// LangPHP, and is actually parsed (Parsed != nil) rather than merely located.
func TestPHPDashRProducesParsedPHPRegion(t *testing.T) {
	decomposition := Parse(`php -r "file_get_contents('/abs/y.php');"`, "/w", "/home/u")
	region := onlyRegion(t, decomposition)
	if region.Lang != LangPHP {
		t.Fatalf("php region lang = %v, want LangPHP", region.Lang)
	}
	if region.Parsed == nil {
		t.Fatal("php embedding should be parsed (php_only grammar exists)")
	}
}

func TestPHPFileGetContentsLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "file_get_contents('/abs/y.php');"`, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	want := []string{"/abs/y.php"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPHPFileGetContentsRelative(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "file_get_contents('rel.php');"`, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	want := []string{"/repo/rel.php"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPHPFileLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "file('/abs/z.php');"`, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	want := []string{"/abs/z.php"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPHPReadfileLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "readfile('/abs/a.php');"`, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	want := []string{"/abs/a.php"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPHPScandirLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "scandir('/abs/d');"`, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	want := []string{"/abs/d"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// The php -r script below is wrapped in bash single quotes rather than
// double quotes, and its PHP string literals use double quotes rather than
// single: a php $variable is otherwise indistinguishable from a bash
// double-quoted expansion, and dispatchPHPDashR only extracts a -r value the
// bash grammar itself resolved to a literal.
func TestPHPGlobResolvesPrefixDir(t *testing.T) {
	command := `php -r 'foreach (glob("/abs/indexed/*.php") as $f) { file_get_contents($f); }'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	want := []string{"/abs/indexed"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPHPFopenReadMode(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "fopen('/abs/b.php', 'r');"`, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	want := []string{"/abs/b.php"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPHPFopenWriteModeNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "fopen('/abs/c.php', 'w');"`, "/repo", Options{Home: "/home/u"})
	reads := phpRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for a write-mode fopen", reads)
	}
	writes := phpRegionWrites(t, decomposition)
	want := []string{"/abs/c.php"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestPHPFopenAppendModeNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "fopen('/abs/e.php', 'a');"`, "/repo", Options{Home: "/home/u"})
	reads := phpRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for an append-mode fopen", reads)
	}
	writes := phpRegionWrites(t, decomposition)
	want := []string{"/abs/e.php"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

// TestPHPFopenNonLiteralModeDropped covers an fopen call whose mode argument
// is present but not a resolvable literal (a variable that could hold "w" at
// runtime). Since the mode cannot be classified as read or write, the whole
// call is dropped rather than guessed in either direction.
func TestPHPFopenNonLiteralModeDropped(t *testing.T) {
	command := `php -r '$path = "/abs/k.php"; $mode = "r"; fopen($path, $mode);'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	reads := phpRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none (a non-literal mode is dropped, not guessed)", reads)
	}
	writes := phpRegionWrites(t, decomposition)
	if len(writes) != 0 {
		t.Fatalf("writes = %v, want none", writes)
	}
}

func TestPHPFilePutContentsIsWriteNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "file_put_contents('/abs/g.php', 'x');"`, "/repo", Options{Home: "/home/u"})
	reads := phpRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for file_put_contents", reads)
	}
	writes := phpRegionWrites(t, decomposition)
	want := []string{"/abs/g.php"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestPHPVariablePathDropped(t *testing.T) {
	command := `php -r '$path = "/abs/f.php"; file_get_contents($path);'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a variable path is never resolved, only dropped)", got)
	}
}

func TestPHPInterpolatedPathDropped(t *testing.T) {
	command := `php -r '$x = "y"; file_get_contents("$x.php");'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (an interpolated double-quoted string is not a literal)", got)
	}
}

// TestPHPEscapedSingleQuoteDropped covers a single-quoted path containing an
// escaped quote. tree-sitter-php surfaces \' as its own escape_sequence
// child, whose php-decoded value differs from its raw source text, so the
// whole string must be dropped by its raw backslash rather than resolved to
// the un-decoded raw text (which would record a path PHP never actually
// opens).
func TestPHPEscapedSingleQuoteDropped(t *testing.T) {
	command := `php -r "file_get_contents('/abs/it\'s/file.php');"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (an escaped single quote is not decoded, so it drops)", got)
	}
}

// TestPHPEscapedBackslashDropped covers a double-quoted path with repeated \\
// escape_sequence children (a Windows-style path). Copying their raw source
// text would double every backslash in the resolved path, a value PHP never
// constructs.
func TestPHPEscapedBackslashDropped(t *testing.T) {
	command := `php -r 'file_get_contents("C:\\Users\\x\\f.php");'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (escape_sequence raw text is not the decoded value, so it drops)", got)
	}
}

// TestPHPHeredocPathDropped covers a heredoc passed directly as a call
// argument. A heredoc is its own "heredoc" grammar node, distinct from
// "string" and "encapsed_string", so phpStringLiteralValue's kind check
// drops it without needing a separate rule; a heredoc body can itself
// interpolate variables the way a double-quoted string can, so treating its
// raw text as a literal would risk the same fabrication failure.
func TestPHPHeredocPathDropped(t *testing.T) {
	command := "php -r \"file_get_contents(<<<EOT\n/abs/g.php\nEOT\n);\""
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a heredoc is not a plain string literal, so it drops)", got)
	}
}

// TestPHPFopenNamedArgumentsReorderedDropped covers the fabrication case a
// review flagged: fopen's named arguments given out of declaration order.
// phpPositionalArg resolves by source-order index, so without the
// phpCallHasNamedArgument guard, position 0 ("mode") would be recorded as a
// read of the literal "r" and the real target /etc/passwd would be dropped.
// That resolved-but-wrong path is worse than a drop: it would displace the
// real target so the consumer's guard checks a path the program never
// touches. This asserts neither the fabricated "r" read nor any write
// appears, only that the call produces no target at all.
func TestPHPFopenNamedArgumentsReorderedDropped(t *testing.T) {
	command := `php -r "fopen(mode: 'r', filename: '/etc/passwd');"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	reads := phpRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none (named arguments are dropped, not resolved positionally)", reads)
	}
	writes := phpRegionWrites(t, decomposition)
	if len(writes) != 0 {
		t.Fatalf("writes = %v, want none", writes)
	}
}

// TestPHPFilePutContentsNamedArgumentsReorderedDropped covers the same
// fabrication shape for file_put_contents: without the guard, position 0
// ("data") would be recorded as a write of the literal "x" resolved against
// cwd, and the real target /etc/cron.d/evil would be dropped.
func TestPHPFilePutContentsNamedArgumentsReorderedDropped(t *testing.T) {
	command := `php -r "file_put_contents(data: 'x', filename: '/etc/cron.d/evil');"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	writes := phpRegionWrites(t, decomposition)
	if len(writes) != 0 {
		t.Fatalf("writes = %v, want none (named arguments are dropped, not resolved positionally)", writes)
	}
	reads := phpRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none", reads)
	}
}

// TestPHPGlobNamedArgumentDropped covers a single-argument function called
// with a named argument. glob only has one role to fill (pattern), so a
// named call in declaration order would resolve correctly by position, but
// phpCallHasNamedArgument drops it anyway: the guard applies uniformly to
// every call this analyzer classifies, not only the two-argument functions,
// since a caller can add a second named argument (glob's own optional
// flags parameter) in either order and this analyzer has no per-function
// exception to the rule.
func TestPHPGlobNamedArgumentDropped(t *testing.T) {
	command := `php -r "glob(pattern: '/abs/indexed/*.php');"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := phpRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a named argument drops the call)", got)
	}
}

func TestPHPArgv0RecordedOnReadTarget(t *testing.T) {
	decomposition := ParseWithOptions(`php -r "file_get_contents('/abs/j.php');"`, "/repo", Options{Home: "/home/u"})
	region := phpRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("php region should be parsed")
	}
	found := false
	for _, target := range region.Parsed.ReadTargets() {
		if target.Resolvable && target.Path == "/abs/j.php" && target.Argv0 == "php" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a php-labeled read of /abs/j.php, got %+v", region.Parsed.ReadTargets())
	}
}
