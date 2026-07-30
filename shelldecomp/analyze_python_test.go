package shelldecomp

import (
	"sort"
	"testing"
)

// fakeResolver builds a FileResolver backed by an in-memory map from absolute
// path to file content, so a test can exercise off-disk script reads without
// touching the filesystem. A path absent from the map is a miss.
func fakeResolver(files map[string]string) FileResolver {
	return func(absPath string) ([]byte, bool) {
		content, ok := files[absPath]
		if !ok {
			return nil, false
		}
		return []byte(content), true
	}
}

// pythonRegionReads returns the resolved read-target paths of the sole python
// embedded region of a decomposition, failing when there is not exactly one
// python region. It reads through EmbeddedRegions()[i].Parsed.ReadTargets() so
// the test asserts on the region's own reads, not a flattened top-level list.
func pythonRegionReads(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := pythonRegion(t, decomposition)
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

// pythonRegionWrites returns the resolved write-target paths of the sole python
// region of a decomposition.
func pythonRegionWrites(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := pythonRegion(t, decomposition)
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

// pythonRegion returns the sole python embedded region of a decomposition,
// failing when the count is not exactly one.
func pythonRegion(t *testing.T, decomposition *Decomposition) EmbeddedRegion {
	t.Helper()
	regions := decomposition.EmbeddedRegions()
	pythonRegions := make([]EmbeddedRegion, 0, len(regions))
	for _, region := range regions {
		if region.Lang == LangPython {
			pythonRegions = append(pythonRegions, region)
		}
	}
	if len(pythonRegions) != 1 {
		t.Fatalf("python regions = %d, want 1", len(pythonRegions))
	}
	return pythonRegions[0]
}

// equalStrings reports whether two string slices hold the same elements in the
// same order.
func equalStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestPythonInlineArgvRead(t *testing.T) {
	command := `python -c "open(sys.argv[1])" /repo/x.go`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo/x.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestPythonMultilineDashCStatementsRead covers a multi-line double-quoted
// -c script reached through Parse, the path a real caller uses. Before the
// literalStringValue fix, tree-sitter-bash's dropped newline fused the two
// statements into "import osopen(...)", which python cannot parse as two
// statements and would not surface the open() read at all. With the newline
// preserved, the script parses as two statements and the read resolves.
func TestPythonMultilineDashCStatementsRead(t *testing.T) {
	command := "python -c \"import os\nopen('/abs/x.go').read()\""
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/abs/x.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPythonInlineAbsoluteRead(t *testing.T) {
	decomposition := ParseWithOptions(`python -c "open('/abs/y.go')"`, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/abs/y.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPythonInlineRelativeRead(t *testing.T) {
	decomposition := ParseWithOptions(`python -c "open('rel.go')"`, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo/rel.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPythonFStringDropsPath(t *testing.T) {
	decomposition := ParseWithOptions(`python -c "open(f'{x}.go')"`, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (f-string is not a literal)", got)
	}
}

func TestPythonImportOnlyNoReads(t *testing.T) {
	decomposition := ParseWithOptions(`python -c "import os"`, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none", got)
	}
}

func TestPythonScriptFileOffDiskRead(t *testing.T) {
	resolver := fakeResolver(map[string]string{
		"/repo/s.py": `open("/repo/y.go")`,
	})
	decomposition := ParseWithOptions(`python /repo/s.py`, "/repo", Options{Home: "/home/u", FileResolver: resolver})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo/y.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPythonScriptFileArgvRead(t *testing.T) {
	resolver := fakeResolver(map[string]string{
		"/repo/s.py": `open(sys.argv[1])`,
	})
	decomposition := ParseWithOptions(`python s.py /repo/x.go`, "/repo", Options{Home: "/home/u", FileResolver: resolver})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo/x.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPythonResolverMissNoReadNoPanic(t *testing.T) {
	resolver := fakeResolver(map[string]string{})
	decomposition := ParseWithOptions(`python /repo/missing.py`, "/repo", Options{Home: "/home/u", FileResolver: resolver})
	got := pythonRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none on resolver miss", got)
	}
}

func TestPythonNilResolverNoBodyNoPanic(t *testing.T) {
	decomposition := Parse(`python /repo/s.py`, "/repo", "/home/u")
	got := pythonRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none with nil resolver", got)
	}
}

func TestPythonWriteNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`python -c "open('/r/o.go','w')"`, "/repo", Options{Home: "/home/u"})
	reads := pythonRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for a write-mode open", reads)
	}
	writes := pythonRegionWrites(t, decomposition)
	want := []string{"/r/o.go"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestPythonPathlibRead(t *testing.T) {
	decomposition := ParseWithOptions(`python -c "from pathlib import Path; Path('/r/a.go').read_text()"`, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/r/a.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPythonFileinputArgv(t *testing.T) {
	command := `python -c "import fileinput; fileinput.input()" /r/a.go /r/b.go`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/r/a.go", "/r/b.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPythonSubprocessRecursion(t *testing.T) {
	decomposition := ParseWithOptions(`python -c "import subprocess; subprocess.run(['grep','TODO','/repo/x.go'])"`, "/repo", Options{Home: "/home/u"})
	region := pythonRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("python region should be parsed")
	}
	found := false
	for _, target := range region.Parsed.ReadTargets() {
		if target.Resolvable && target.Path == "/repo/x.go" && target.Argv0 == "grep" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a grep read of /repo/x.go from subprocess, got %+v", region.Parsed.ReadTargets())
	}
}

func TestPythonOsSystemRecursion(t *testing.T) {
	decomposition := ParseWithOptions(`python -c "import os; os.system('rg P /repo')"`, "/repo", Options{Home: "/home/u"})
	region := pythonRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("python region should be parsed")
	}
	found := false
	for _, target := range region.Parsed.ReadTargets() {
		if target.Resolvable && target.Path == "/repo" && target.Argv0 == "rg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an rg read of /repo from os.system, got %+v", region.Parsed.ReadTargets())
	}
}

// pythonHeredoc wraps a multi-line python program in a `python3 - <<'PY'`
// heredoc so a test can exercise statement-form programs (for-loops, def) the
// way an agent actually writes them, not just `-c` one-liners.
func pythonHeredoc(body string) string {
	return "python3 - <<'PY'\n" + body + "\nPY\n"
}

// TestPythonRglobReadResolvesToDir is the exact leaked shape: a recursive walk
// of cwd whose discovered paths are read for their contents resolves to the
// walked directory, so the index-aware validator can block it.
func TestPythonRglobReadResolvesToDir(t *testing.T) {
	body := "import pathlib\n" +
		"root = pathlib.Path(\".\")\n" +
		"for p in sorted(root.rglob(\"*\")):\n" +
		"    text = p.read_text()\n"
	decomposition := ParseWithOptions(pythonHeredoc(body), "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestPythonOsWalkJoinOpenResolvesToDir covers os.walk with a tuple target and
// os.path.join feeding open: the walked directory is the read target.
func TestPythonOsWalkJoinOpenResolvesToDir(t *testing.T) {
	body := "import os\n" +
		"for d, dirs, files in os.walk(\"/abs/repo\"):\n" +
		"    for f in files:\n" +
		"        open(os.path.join(d, f)).read()\n"
	decomposition := ParseWithOptions(pythonHeredoc(body), "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/abs/repo"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestPythonGlobInHelperResolvesToDir covers a read buried in a helper function
// fed by glob.glob with a ** pattern: the link is def-use, not adjacency, and
// the pattern's pre-wildcard root (cwd here) is the target.
func TestPythonGlobInHelperResolvesToDir(t *testing.T) {
	body := "import glob\n" +
		"def scan():\n" +
		"    for p in glob.glob(\"**/*.py\", recursive=True):\n" +
		"        open(p).read()\n" +
		"scan()\n"
	decomposition := ParseWithOptions(pythonHeredoc(body), "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestPythonComprehensionRglobResolvesToDir covers the comprehension for-clause
// taint path: [p.read_text() for p in Path('.').rglob('*')].
func TestPythonComprehensionRglobResolvesToDir(t *testing.T) {
	command := `python -c "import pathlib; [p.read_text() for p in pathlib.Path('.').rglob('*')]"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestPythonIterdirNamesOnlyNoDir is a negative: enumerating a directory and
// using only the names (no content read) is a filename listing, not a search,
// so no directory target is emitted.
func TestPythonIterdirNamesOnlyNoDir(t *testing.T) {
	body := "import pathlib\n" +
		"for p in pathlib.Path(\".\").iterdir():\n" +
		"    print(p.name)\n"
	decomposition := ParseWithOptions(pythonHeredoc(body), "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (names only, no content read)", got)
	}
}

// TestPythonEnumerationPlusUnrelatedLiteralRead is a negative for over-firing:
// an enumeration whose paths never reach a read sink must not emit the walked
// directory, while a separate literal open is still a named-file read.
func TestPythonEnumerationPlusUnrelatedLiteralRead(t *testing.T) {
	body := "import pathlib\n" +
		"for p in pathlib.Path(\".\").iterdir():\n" +
		"    pass\n" +
		"open(\"a.txt\")\n"
	decomposition := ParseWithOptions(pythonHeredoc(body), "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo/a.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v (literal only, no walked dir)", got, want)
	}
}

// TestPythonVariableBoundLiteralRead covers the def-use resolution of a
// variable assigned a string literal then opened: open(x) where x = "rel.go".
func TestPythonVariableBoundLiteralRead(t *testing.T) {
	command := `python -c "x = 'rel.go'; open(x)"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo/rel.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestPythonReassignedVarClearsStaleBinding covers that a variable reassigned to
// a non-path expression drops its earlier literal binding, so a later open does
// not resolve to the stale path.
func TestPythonReassignedVarClearsStaleBinding(t *testing.T) {
	command := `python -c "x = 'a.go'; x = compute(); open(x)"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (binding cleared by reassignment)", got)
	}
}

// TestPythonVariablePathOpen covers Path(...).open() through a variable receiver:
// p = pathlib.Path('x.go'); p.open() resolves the file the variable names.
func TestPythonVariablePathOpen(t *testing.T) {
	command := `python -c "import pathlib; p = pathlib.Path('x.go'); p.open()"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := pythonRegionReads(t, decomposition)
	want := []string{"/repo/x.go"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPythonSelfReferentialCycleTerminates(t *testing.T) {
	resolver := fakeResolver(map[string]string{
		"/repo/s.py": `import subprocess; subprocess.run(["python","/repo/s.py"])`,
	})
	done := make(chan *Decomposition, 1)
	go func() {
		done <- ParseWithOptions(`python /repo/s.py`, "/repo", Options{Home: "/home/u", FileResolver: resolver})
	}()
	select {
	case <-done:
	default:
		// The parse runs synchronously; a deadlock or infinite loop would block
		// the goroutine and leave done empty, which the test below detects.
	}
	decomposition := <-done
	if decomposition == nil {
		t.Fatal("self-referential python parse did not terminate")
	}
}
