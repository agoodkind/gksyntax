package shelldecomp

import (
	"sort"
	"testing"
)

// sqlRegion returns the sole LangSQL embedded region of a decomposition,
// failing when the count is not exactly one, matching rubyRegion in
// analyze_ruby_test.go.
func sqlRegion(t *testing.T, decomposition *Decomposition) EmbeddedRegion {
	t.Helper()
	regions := decomposition.EmbeddedRegions()
	sqlRegions := make([]EmbeddedRegion, 0, len(regions))
	for _, region := range regions {
		if region.Lang == LangSQL {
			sqlRegions = append(sqlRegions, region)
		}
	}
	if len(sqlRegions) != 1 {
		t.Fatalf("sql regions = %d, want 1", len(sqlRegions))
	}
	return sqlRegions[0]
}

// sqlRegionReads returns the resolved read-target paths of the sole SQL
// region of a decomposition.
func sqlRegionReads(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := sqlRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("sql region should be parsed (analyzer is registered for LangSQL)")
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

// sqlRegionWrites returns the resolved write-target paths of the sole SQL
// region of a decomposition.
func sqlRegionWrites(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := sqlRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("sql region should be parsed (analyzer is registered for LangSQL)")
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

func TestSQLAttachDatabaseLiteralRead(t *testing.T) {
	command := `sqlite3 db.sqlite "ATTACH DATABASE '/other/aux.db' AS aux;"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionReads(t, decomposition)
	want := []string{"/other/aux.db"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestSQLAttachWithoutDatabaseKeywordRead(t *testing.T) {
	command := `sqlite3 db.sqlite "ATTACH '/other/aux.db' AS aux;"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionReads(t, decomposition)
	want := []string{"/other/aux.db"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestSQLReadfileRead(t *testing.T) {
	command := `sqlite3 db.sqlite "SELECT readfile('/data/blob.bin');"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionReads(t, decomposition)
	want := []string{"/data/blob.bin"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestSQLImportRead(t *testing.T) {
	command := `sqlite3 db.sqlite ".import /data/in.csv mytable"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionReads(t, decomposition)
	want := []string{"/data/in.csv"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestSQLImportRelativePathResolvesAgainstCwd(t *testing.T) {
	command := `sqlite3 db.sqlite ".import in.csv mytable"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionReads(t, decomposition)
	want := []string{"/repo/in.csv"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestSQLWritefileIsWriteNotRead(t *testing.T) {
	command := `sqlite3 db.sqlite "SELECT writefile('/data/out.bin', 'payload');"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	reads := sqlRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for writefile", reads)
	}
	writes := sqlRegionWrites(t, decomposition)
	want := []string{"/data/out.bin"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestSQLOutputWrite(t *testing.T) {
	command := `sqlite3 db.sqlite ".output /data/log.txt"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	reads := sqlRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for .output", reads)
	}
	writes := sqlRegionWrites(t, decomposition)
	want := []string{"/data/log.txt"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

// TestSQLOutputStdoutIsNotWrite covers the sqlite3 shell's magic ".output
// stdout" argument, which resets output to the console rather than naming a
// file. Recording it as a write would resolve a path the shell never opens.
func TestSQLOutputStdoutIsNotWrite(t *testing.T) {
	command := `sqlite3 db.sqlite ".output stdout"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	writes := sqlRegionWrites(t, decomposition)
	if len(writes) != 0 {
		t.Fatalf("writes = %v, want none for .output stdout", writes)
	}
}

// TestSQLOutputOffIsWriteToDevNull covers the sqlite3 shell's magic
// ".output off" argument. Unlike "stdout", "off" is a real open of
// /dev/null (dotCmdOutput in src/shell.c.in), so it is recorded as a write
// to that path rather than dropped.
func TestSQLOutputOffIsWriteToDevNull(t *testing.T) {
	command := `sqlite3 db.sqlite ".output off"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	writes := sqlRegionWrites(t, decomposition)
	want := []string{"/dev/null"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

// TestSQLImportFlagPrefixedDropped covers a .import invocation carrying an
// option flag before the file, such as --csv. sqlite3's own .import loops
// past a leading '-' token before accepting the file argument; this analyzer
// does not model every flag's arity, so it drops the whole call rather than
// misresolving the flag token itself as the path (which would fabricate a
// path like /repo/--csv and silently miss the real file /data/in.csv).
func TestSQLImportFlagPrefixedDropped(t *testing.T) {
	command := `sqlite3 db.sqlite ".import --csv /data/in.csv mytable"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a flag-prefixed .import is dropped, not misresolved)", got)
	}
}

// TestSQLOutputFlagPrefixedDropped covers a .output invocation carrying an
// option flag before the file, such as -bom, matching the same drop rule as
// TestSQLImportFlagPrefixedDropped.
func TestSQLOutputFlagPrefixedDropped(t *testing.T) {
	command := `sqlite3 db.sqlite ".output -bom /data/log.txt"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionWrites(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("writes = %v, want none (a flag-prefixed .output is dropped, not misresolved)", got)
	}
}

func TestSQLBindParameterDropped(t *testing.T) {
	command := `sqlite3 db.sqlite "SELECT readfile(?1);"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a bind parameter is never resolved, only dropped)", got)
	}
}

func TestSQLColumnReferenceDropped(t *testing.T) {
	command := `sqlite3 db.sqlite "SELECT readfile(path_column) FROM files;"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a column reference is never resolved, only dropped)", got)
	}
}

// TestSQLBackslashLiteralDropped covers a single-quoted literal whose raw
// source span carries a backslash. SQLite's own string literals have no
// backslash escape (only a doubled ” quote), but this analyzer applies the
// same conservative rule as analyze_ruby.go's rubyStringLiteralValue and
// drops the whole literal on a raw backslash, keeping the discipline uniform
// across every analyzer in this package.
func TestSQLBackslashLiteralDropped(t *testing.T) {
	command := `sqlite3 db.sqlite "SELECT readfile('/data/x\y.bin');"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := sqlRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a raw backslash in the literal span drops it)", got)
	}
}

func TestSQLArgv0RecordedOnReadTarget(t *testing.T) {
	command := `sqlite3 db.sqlite "SELECT readfile('/data/blob.bin');"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	region := sqlRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("sql region should be parsed")
	}
	found := false
	for _, target := range region.Parsed.ReadTargets() {
		if target.Resolvable && target.Path == "/data/blob.bin" && target.Argv0 == "sqlite3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a sqlite3-labeled read of /data/blob.bin, got %+v", region.Parsed.ReadTargets())
	}
}

// TestSQLImportAndAttachTogether covers a multi-line SQL script mixing a dot
// command line with ordinary SQL text, checking that the per-line dot-command
// split does not lose the SQL statement on the other lines. It calls
// analyzeSQL directly with a literal newline in the source rather than
// routing a multi-line double-quoted argument through Parse: this package's
// bash double-quoted string extraction collapses an embedded newline when
// building the embedding text, which is a shell-layer property unrelated to
// this analyzer, so a direct AnalyzerInput is the only way to exercise the
// two-line split analyzeSQL itself performs.
func TestSQLImportAndAttachTogether(t *testing.T) {
	source := ".import /data/in.csv mytable\nATTACH DATABASE '/other/aux.db' AS aux;"
	reads, _ := analyzeSQL(AnalyzerInput{Source: []byte(source), Cwd: "/repo", Home: "/home/u"})
	got := make([]string, 0, len(reads))
	for _, target := range reads {
		if target.Resolvable {
			got = append(got, target.Path)
		}
	}
	sort.Strings(got)
	want := []string{"/data/in.csv", "/other/aux.db"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}
