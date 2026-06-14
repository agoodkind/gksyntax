package shelldecomp

import (
	"sort"
	"testing"
)

// perlRegion returns the sole perl embedded region of a decomposition, failing
// when the count is not exactly one.
func perlRegion(t *testing.T, decomposition *Decomposition) EmbeddedRegion {
	t.Helper()
	regions := decomposition.EmbeddedRegions()
	perlRegions := make([]EmbeddedRegion, 0, len(regions))
	for _, region := range regions {
		if region.Lang == LangPerl {
			perlRegions = append(perlRegions, region)
		}
	}
	if len(perlRegions) != 1 {
		t.Fatalf("perl regions = %d, want 1", len(perlRegions))
	}
	return perlRegions[0]
}

// perlRegionReads returns the resolved in-script read-target paths of the sole
// perl embedded region of a decomposition.
func perlRegionReads(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := perlRegion(t, decomposition)
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

// perlRegionWrites returns the resolved in-script write-target paths of the sole
// perl embedded region of a decomposition.
func perlRegionWrites(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := perlRegion(t, decomposition)
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

func TestPerlOpenThreeArgRead(t *testing.T) {
	command := `perl -e 'open(my $fh, "<", "data.txt");'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlRegionReads(t, decomposition)
	want := []string{"/repo/data.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
	if writes := perlRegionWrites(t, decomposition); len(writes) != 0 {
		t.Fatalf("writes = %v, want none for a read open", writes)
	}
}

func TestPerlOpenThreeArgWrite(t *testing.T) {
	command := `perl -e 'open(my $fh, ">", "out.txt");'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlRegionWrites(t, decomposition)
	want := []string{"/repo/out.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("writes = %v, want %v", got, want)
	}
	if reads := perlRegionReads(t, decomposition); len(reads) != 0 {
		t.Fatalf("reads = %v, want none for a write open", reads)
	}
}

func TestPerlOpenThreeArgAppendWrite(t *testing.T) {
	command := `perl -e 'open(my $fh, ">>", "app.txt");'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlRegionWrites(t, decomposition)
	want := []string{"/repo/app.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("writes = %v, want %v", got, want)
	}
}

func TestPerlOpenTwoArgRead(t *testing.T) {
	command := `perl -e 'open(FH, "<file.txt");'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlRegionReads(t, decomposition)
	want := []string{"/repo/file.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPerlOpenTwoArgWrite(t *testing.T) {
	command := `perl -e 'open(FH, ">out2.txt");'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlRegionWrites(t, decomposition)
	want := []string{"/repo/out2.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("writes = %v, want %v", got, want)
	}
}

func TestPerlOpenTwoArgAbsoluteRead(t *testing.T) {
	command := `perl -e 'open(FH, "</abs/in.txt");'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlRegionReads(t, decomposition)
	want := []string{"/abs/in.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPerlOpenDupModeSkipped(t *testing.T) {
	command := `perl -e 'open($fh, "<&", $other);'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	if reads := perlRegionReads(t, decomposition); len(reads) != 0 {
		t.Fatalf("reads = %v, want none for a dup open", reads)
	}
	if writes := perlRegionWrites(t, decomposition); len(writes) != 0 {
		t.Fatalf("writes = %v, want none for a dup open", writes)
	}
}

func TestPerlOpenVariablePathDropped(t *testing.T) {
	command := `perl -e 'open(my $fh, "<", $path);'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	if reads := perlRegionReads(t, decomposition); len(reads) != 0 {
		t.Fatalf("reads = %v, want none for a variable path", reads)
	}
}

func TestPerlOpenInterpolatedPathDropped(t *testing.T) {
	command := `perl -e 'open(my $fh, "<", "$dir/x.txt");'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	if reads := perlRegionReads(t, decomposition); len(reads) != 0 {
		t.Fatalf("reads = %v, want none for an interpolated path", reads)
	}
}
