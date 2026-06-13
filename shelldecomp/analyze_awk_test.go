package shelldecomp

import (
	"sort"
	"testing"
)

// awkRegion returns the sole awk embedded region of a decomposition, failing
// when the count is not exactly one.
func awkRegion(t *testing.T, decomposition *Decomposition) EmbeddedRegion {
	t.Helper()
	regions := decomposition.EmbeddedRegions()
	awkRegions := make([]EmbeddedRegion, 0, len(regions))
	for _, region := range regions {
		if region.Lang == LangAwk {
			awkRegions = append(awkRegions, region)
		}
	}
	if len(awkRegions) != 1 {
		t.Fatalf("awk regions = %d, want 1", len(awkRegions))
	}
	return awkRegions[0]
}

// awkRegionReads returns the resolved read-target paths of the sole awk
// embedded region of a decomposition, read through Parsed.ReadTargets() so the
// test asserts on the region's own in-script reads.
func awkRegionReads(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := awkRegion(t, decomposition)
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

// awkRegionWrites returns the resolved write-target paths of the sole awk
// region of a decomposition.
func awkRegionWrites(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := awkRegion(t, decomposition)
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

func TestAwkGetlineLiteralRead(t *testing.T) {
	command := `awk '{ while ((getline line < "data.txt") > 0) print line }' f`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := awkRegionReads(t, decomposition)
	want := []string{"/repo/data.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestAwkGetlineWithVarLiteralRead(t *testing.T) {
	command := `awk '{ getline x < "f.txt" }' f`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := awkRegionReads(t, decomposition)
	want := []string{"/repo/f.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestAwkGetlineAbsoluteRead(t *testing.T) {
	command := `awk '{ getline < "/abs/in.txt" }' f`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := awkRegionReads(t, decomposition)
	want := []string{"/abs/in.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestAwkPrintLiteralWrite(t *testing.T) {
	command := `awk 'BEGIN { print "x" > "out.txt" }' f`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	reads := awkRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for a print redirect", reads)
	}
	writes := awkRegionWrites(t, decomposition)
	want := []string{"/repo/out.txt"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestAwkPrintAppendLiteralWrite(t *testing.T) {
	command := `awk 'BEGIN { printf "%s", y >> "app.txt" }' f`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	writes := awkRegionWrites(t, decomposition)
	want := []string{"/repo/app.txt"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestAwkGetlineNonLiteralTargetDropped(t *testing.T) {
	command := `awk '{ getline < f }' x`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := awkRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none for a variable getline target", got)
	}
}

func TestAwkGetlineArgvTargetDropped(t *testing.T) {
	command := `awk '{ getline < ARGV[1] }' x`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := awkRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none for an ARGV[i] getline target", got)
	}
}

func TestAwkPrintNonLiteralTargetDropped(t *testing.T) {
	command := `awk 'BEGIN { x="o"; print "y" > x }' f`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	writes := awkRegionWrites(t, decomposition)
	if len(writes) != 0 {
		t.Fatalf("writes = %v, want none for a variable redirect target", writes)
	}
}

func TestAwkPipeGetlineCommandDropped(t *testing.T) {
	command := `awk '{ "ls" | getline z }' f`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := awkRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none for a command pipe into getline", got)
	}
}

func TestAwkEscapedStringTargetDropped(t *testing.T) {
	command := `awk 'BEGIN { print "y" > "a\tb.txt" }' f`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	writes := awkRegionWrites(t, decomposition)
	if len(writes) != 0 {
		t.Fatalf("writes = %v, want none for an escaped-string redirect target", writes)
	}
}

// TestAwkInplaceReadScanNoProgramLeak covers the gawk -i inplace read-scan fix:
// gawk -i inplace '{...}' file must not surface the awk program text as a top
// level read path. With -i added to the awk value flags, -i inplace consumes
// both tokens, the program is consumed as the script operand, and only the real
// data file is a read target.
func TestAwkInplaceReadScanNoProgramLeak(t *testing.T) {
	command := `gawk -i inplace '{ gsub(/a/,"b") }' file.txt`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	for _, target := range decomposition.ReadTargets() {
		if target.Raw == `{ gsub(/a/,"b") }` || target.Raw == `'{ gsub(/a/,"b") }'` {
			t.Fatalf("awk program leaked as a read path: %+v", target)
		}
	}
	reads := make([]string, 0)
	for _, target := range decomposition.ReadTargets() {
		if target.Resolvable {
			reads = append(reads, target.Path)
		}
	}
	sort.Strings(reads)
	want := []string{"/repo/file.txt"}
	if !equalStrings(reads, want) {
		t.Fatalf("top-level reads = %v, want %v", reads, want)
	}
}
