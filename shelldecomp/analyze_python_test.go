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
