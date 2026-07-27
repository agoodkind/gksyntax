package shelldecomp

import (
	"sort"
	"testing"
)

// jsRegionReads returns the resolved read-target paths of the sole
// javascript embedded region of a decomposition, failing when the count is
// not exactly one. It reads through EmbeddedRegions()[i].Parsed.ReadTargets()
// so the test asserts on the region's own reads, not a flattened top-level
// list.
func jsRegionReads(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := jsRegion(t, decomposition)
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

// jsRegionWrites returns the resolved write-target paths of the sole
// javascript region of a decomposition.
func jsRegionWrites(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	region := jsRegion(t, decomposition)
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

// jsRegion returns the sole javascript embedded region of a decomposition,
// failing when the count is not exactly one.
func jsRegion(t *testing.T, decomposition *Decomposition) EmbeddedRegion {
	t.Helper()
	regions := decomposition.EmbeddedRegions()
	jsRegions := make([]EmbeddedRegion, 0, len(regions))
	for _, region := range regions {
		if region.Lang == LangJavaScript {
			jsRegions = append(jsRegions, region)
		}
	}
	if len(jsRegions) != 1 {
		t.Fatalf("javascript regions = %d, want 1", len(jsRegions))
	}
	return jsRegions[0]
}

func TestJSReadFileSyncLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.readFileSync('/abs/y.js')"`, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/y.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestJSReadFileRelative(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.readFile('rel.js', () => {})"`, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/repo/rel.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestJSPromisesReadFileLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.promises.readFile('/abs/z.js')"`, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/z.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestJSDestructuredReadFileSyncCallSite covers the exact form the brief
// calls out verbatim: `const { readFileSync } = require("fs")` binds the
// bare name, so the later call site `readFileSync(path)` carries no fs.
// prefix at all.
func TestJSDestructuredReadFileSyncCallSite(t *testing.T) {
	command := `node -e "const { readFileSync } = require('fs'); readFileSync('/abs/a.js')"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/a.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestJSRenamedDestructuredReadCallSite covers the renamed destructure form
// the brief calls out verbatim: `const { readFileSync: rf } = require("fs")`
// binds the local name rf to the canonical readFileSync method.
func TestJSRenamedDestructuredReadCallSite(t *testing.T) {
	command := `node -e "const { readFileSync: rf } = require('fs'); rf('/abs/b.js')"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/b.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestJSImportedReadFileSyncCallSite covers the ESM form the brief calls out
// verbatim: `import { readFileSync } from "fs"`.
func TestJSImportedReadFileSyncCallSite(t *testing.T) {
	command := `node -e "import { readFileSync } from 'fs'; readFileSync('/abs/c.js')"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/c.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestJSRenamedImportCallSite covers a renamed ESM import,
// `import { readFileSync as rf } from "fs"`, the import-side counterpart of
// the require-side rename the brief calls out.
func TestJSRenamedImportCallSite(t *testing.T) {
	command := `node -e "import { readFileSync as rf } from 'fs'; rf('/abs/d.js')"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/d.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestJSReaddirSyncLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.readdirSync('/abs/dir')"`, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/dir"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestJSReaddirCallbackForm(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.readdir('/abs/dir2', () => {})"`, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/dir2"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestJSCreateReadStreamLiteral(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.createReadStream('/abs/e.js')"`, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/e.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestJSTemplateLiteralWithSubstitutionDropped covers a template literal
// with a substitution, which the brief says is never resolvable, unlike a
// no-substitution template literal.
func TestJSTemplateLiteralWithSubstitutionDropped(t *testing.T) {
	// The script is shell-single-quoted, not double-quoted, because bash
	// treats a backtick inside a double-quoted argument as command
	// substitution syntax; a single-quoted argument passes the backtick
	// through literally so the node -e script actually parses as intended.
	command := "node -e 'const dir = \"/abs\"; fs.readFileSync(`${dir}/y.js`)'"
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a template literal with a substitution is never resolvable)", got)
	}
}

// TestJSTemplateLiteralNoSubstitutionResolves covers the brief's converse
// rule: a template literal with no substitution is an ordinary literal.
func TestJSTemplateLiteralNoSubstitutionResolves(t *testing.T) {
	// Shell-single-quoted for the same reason as
	// TestJSTemplateLiteralWithSubstitutionDropped: a double-quoted shell
	// argument would let bash treat the backtick as command substitution.
	command := "node -e 'fs.readFileSync(`/abs/f.js`)'"
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/f.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestJSPathJoinLiteralsResolves covers the brief's path.join rule: a
// path.join call whose every argument is a string literal resolves to their
// join.
func TestJSPathJoinLiteralsResolves(t *testing.T) {
	command := `node -e "const path = require('path'); fs.readFileSync(path.join('/abs', 'g.js'))"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	want := []string{"/abs/g.js"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

// TestJSPathJoinVariableArgDropped covers the brief's converse path.join
// rule: any variable argument means the whole path.join call is dropped, not
// partially resolved.
func TestJSPathJoinVariableArgDropped(t *testing.T) {
	command := `node -e "const name = 'h.js'; fs.readFileSync(path.join('/abs', name))"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a path.join argument that is not a literal drops the whole call)", got)
	}
}

func TestJSVariablePathDropped(t *testing.T) {
	command := `node -e "const p = '/abs/i.js'; fs.readFileSync(p)"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a variable path is never resolved, only dropped)", got)
	}
}

// TestJSBackslashInStringDropped covers a double-quoted path containing raw
// backslashes. Copying the raw source text of the escape_sequence children
// would double every backslash in the resolved path, a value the javascript
// runtime never constructs, matching analyze_ruby.go's equivalent test.
func TestJSBackslashInStringDropped(t *testing.T) {
	command := `node -e 'fs.readFileSync("C:\\Users\\x\\f.js")'`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := jsRegionReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none (a raw backslash in the string is not decoded, so it drops)", got)
	}
}

func TestJSWriteFileSyncNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.writeFileSync('/abs/j.js', 'x')"`, "/repo", Options{Home: "/home/u"})
	reads := jsRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for fs.writeFileSync", reads)
	}
	writes := jsRegionWrites(t, decomposition)
	want := []string{"/abs/j.js"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestJSWriteFileNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.writeFile('/abs/k.js', 'x', () => {})"`, "/repo", Options{Home: "/home/u"})
	reads := jsRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for fs.writeFile", reads)
	}
	writes := jsRegionWrites(t, decomposition)
	want := []string{"/abs/k.js"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestJSAppendFileSyncNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.appendFileSync('/abs/l.js', 'x')"`, "/repo", Options{Home: "/home/u"})
	reads := jsRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for fs.appendFileSync", reads)
	}
	writes := jsRegionWrites(t, decomposition)
	want := []string{"/abs/l.js"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestJSCreateWriteStreamNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.createWriteStream('/abs/m.js')"`, "/repo", Options{Home: "/home/u"})
	reads := jsRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for fs.createWriteStream", reads)
	}
	writes := jsRegionWrites(t, decomposition)
	want := []string{"/abs/m.js"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

func TestJSOpenSyncWriteModeNotRead(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.openSync('/abs/n.js', 'w')"`, "/repo", Options{Home: "/home/u"})
	reads := jsRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none for a write-flag openSync", reads)
	}
	writes := jsRegionWrites(t, decomposition)
	want := []string{"/abs/n.js"}
	if !equalStrings(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}

// TestJSOpenSyncNonLiteralFlagsDropped covers an fs.openSync call whose
// flags argument is present but not a resolvable literal. Since the flags
// cannot be classified as read or write, the whole call is dropped rather
// than guessed, matching analyze_ruby.go's non-literal File.open mode test.
func TestJSOpenSyncNonLiteralFlagsDropped(t *testing.T) {
	command := `node -e "const flags = 'w'; fs.openSync('/abs/o.js', flags)"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	reads := jsRegionReads(t, decomposition)
	if len(reads) != 0 {
		t.Fatalf("reads = %v, want none", reads)
	}
	writes := jsRegionWrites(t, decomposition)
	if len(writes) != 0 {
		t.Fatalf("writes = %v, want none (a non-literal flags argument is dropped, not guessed)", writes)
	}
}

func TestJSArgv0RecordedOnReadTarget(t *testing.T) {
	decomposition := ParseWithOptions(`node -e "fs.readFileSync('/abs/p.js')"`, "/repo", Options{Home: "/home/u"})
	region := jsRegion(t, decomposition)
	if region.Parsed == nil {
		t.Fatal("javascript region should be parsed")
	}
	found := false
	for _, target := range region.Parsed.ReadTargets() {
		if target.Resolvable && target.Path == "/abs/p.js" && target.Argv0 == "node" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a node-labeled read of /abs/p.js, got %+v", region.Parsed.ReadTargets())
	}
}
