package shelldecomp

import (
	"sort"
	"testing"
)

// rubyRegionReads returns the resolved read-target paths of the sole ruby
// embedded region of a decomposition, failing when there is not exactly one
// ruby region. It reads through EmbeddedRegions()[i].Parsed.ReadTargets() so
// the test asserts on the region's own reads, not a flattened top-level list.
func rubyRegionReads(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := rubyRegion(t, decomposition)
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

// rubyRegionWrites returns the resolved write-target paths of the sole ruby
// region of a decomposition.
func rubyRegionWrites(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := rubyRegion(t, decomposition)
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

// rubyRegion returns the sole ruby embedded region of a decomposition,
// failing when the count is not exactly one.
func rubyRegion(t *testing.T, decomposition *Decomposition) EmbeddedRegion {
	t.Helper()
	regions := decomposition.EmbeddedRegions()
	rubyRegions := make([]EmbeddedRegion, 0, len(regions))
	for _, region := range regions {
		if region.Lang == LangRuby {
			rubyRegions = append(rubyRegions, region)
		}
	}
	if len(rubyRegions) != 1 {
		t.Fatalf("ruby regions = %d, want 1", len(rubyRegions))
	}
	return rubyRegions[0]
}

func TestRubyFileReadLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "File.read('/abs/y.rb')"`, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs/y.rb"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyFileReadlinesRelative(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "File.readlines('rel.rb')"`, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/repo/rel.rb"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyIOReadLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "IO.read('/abs/z.rb')"`, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs/z.rb"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyFileForeachLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "File.foreach('/abs/a.rb') { |l| puts l }"`, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs/a.rb"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyFileOpenDefaultModeRead(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "File.open('/abs/b.rb')"`, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs/b.rb"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyFileOpenExplicitReadMode(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "File.open('/abs/c.rb', 'r')"`, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs/c.rb"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyDirGlobResolvesPrefixDir(t *testing.T) {
	command := `ruby -e "Dir.glob('/abs/indexed/**/*.rb').each { |f| puts File.read(f) }"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs/indexed"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyDirBracketResolvesPrefixDir(t *testing.T) {
	command := `ruby -e "Dir['/abs/indexed/**/*.rb'].each { |f| puts File.read(f) }"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs/indexed"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyDirEntriesLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "Dir.entries('/abs/d')"`, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs/d"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyDirEachChildLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "Dir.each_child('/abs/e')"`, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs/e"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestRubyVariablePathDropped(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "path = '/abs/f.rb'; File.read(path)"`, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a variable path is never resolved, only dropped)", got)
	}
}

func TestRubyInterpolatedPathDropped(t *testing.T) {
	command := `ruby -e 'x = "y"; File.read("#{x}/y.rb")'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (an interpolated string is not a literal)", got)
	}
}

func TestRubyFileWriteNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "File.write('/abs/g.rb', 'x')"`, "/repo", Options{Home: "/home/u"})
	reads := rubyRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for File.write", reads)
	}
	writes := rubyRegionWrites(t, decomposition)
	want := []string{"/abs/g.rb"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestRubyFileOpenWriteModeNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "File.open('/abs/h.rb', 'w')"`, "/repo", Options{Home: "/home/u"})
	reads := rubyRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for a write-mode open", reads)
	}
	writes := rubyRegionWrites(t, decomposition)
	want := []string{"/abs/h.rb"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestRubyFileOpenAppendModeNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "File.open('/abs/i.rb', 'a')"`, "/repo", Options{Home: "/home/u"})
	reads := rubyRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for an append-mode open", reads)
	}
	writes := rubyRegionWrites(t, decomposition)
	want := []string{"/abs/i.rb"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestRubyArgv0RecordedOnReadTarget(t *testing.T) {
	decomposition := ParseWithOptions(`ruby -e "File.read('/abs/j.rb')"`, "/repo", Options{Home: "/home/u"})
	region := rubyRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("ruby region should be parsed")
	}
	found := false
	for _, target := range region.Parsed.ReadTargets() {
		if target.Resolvable && target.Path == "/abs/j.rb" && target.Argv0 == "ruby" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a ruby-labeled read of /abs/j.rb, got %+v", region.Parsed.ReadTargets())
	}
}

// TestRubyEscapedSingleQuoteDropped covers a single-quoted path containing an
// escaped quote. tree-sitter-ruby folds the escape straight into one
// string_content node with no separate node to catch, so the whole string
// must be dropped by its raw backslash rather than resolved to the
// un-decoded raw text (which would record a path Ruby never actually opens).
func TestRubyEscapedSingleQuoteDropped(t *testing.T) {
	command := `ruby -e "File.read('/abs/it\'s/file.rb')"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (an escaped single quote is not decoded, so it drops)", got)
	}
}

// TestRubyEscapedBackslashDropped covers a double-quoted path with repeated
// \\ escape_sequence children. Copying their raw source text would double
// every backslash in the resolved path, a value Ruby never constructs.
func TestRubyEscapedBackslashDropped(t *testing.T) {
	command := `ruby -e 'File.read("C:\\Users\\x\\f.rb")'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (escape_sequence raw text is not the decoded value, so it drops)", got)
	}
}

// TestRubyEscapedInterpolationMarkerDropped covers a double-quoted path with
// a \#{ escape, which prevents interpolation in real Ruby but still parses
// as an escape_sequence child. Copying its raw text would leave a spurious
// backslash the real string does not have.
func TestRubyEscapedInterpolationMarkerDropped(t *testing.T) {
	command := `ruby -e 'File.read("/abs/\#{lit}.rb")'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (\\# escape_sequence raw text is not the decoded value, so it drops)", got)
	}
}

// TestRubyDirGlobBraceAlternationPrefixDir covers a brace-alternation glob
// pattern. The wildcard set must include { so the prefix directory resolves
// to the real parent directory rather than a path containing an unexpanded
// {a,b} segment, which cannot exist on disk.
func TestRubyDirGlobBraceAlternationPrefixDir(t *testing.T) {
	command := `ruby -e "Dir.glob('/abs/{a,b}/*.rb')"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := rubyRegionReads(t, decomposition)
	want := []string{"/abs"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestRubyFileOpenNonLiteralModeDropped covers a File.open call whose mode
// argument is present but not a resolvable literal (a variable that could
// hold "w" at runtime). Since the mode cannot be classified as read or
// write, the whole call is dropped rather than defaulted to a read the way
// File.open's own absent-mode default is.
func TestRubyFileOpenNonLiteralModeDropped(t *testing.T) {
	command := `ruby -e "path = '/abs/k.rb'; mode = 'r'; File.open(path, mode)"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	reads := rubyRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none (a non-literal mode is dropped, not defaulted to read)", reads)
	}
	writes := rubyRegionWrites(t, decomposition)
	if len(writes) != 0 {
		t.Fatalf("writes = %v, want none", writes)
	}
}
