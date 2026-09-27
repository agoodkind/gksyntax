package shelldecomp

import (
	"testing"
)

// jsRegionReadsFor returns the resolved read paths of the sole javascript
// region of a decomposition.
func jsRegionReadsFor(t *testing.T, command string) []string {
	t.Helper()
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	return jsRegionReads(t, decomposition)
}

// TestJavaScriptDestructuredBindingRespectsItsModule is the regression for a
// binding that forgot where it came from. A name destructured from
// fs/promises was checked against the whole fs method table, so
// `const { readdirSync } = require("fs").promises` recorded a read at a call
// site that throws before touching a file: readdirSync does not exist on
// fs.promises.
//
// A phantom read is worse than a missed one, because it is indistinguishable
// downstream from a file the program really opened.
func TestJavaScriptDestructuredBindingRespectsItsModule(t *testing.T) {
	cases := []struct {
		name    string
		program string
		want    []string
	}{
		{
			name:    "promises method on promises module",
			program: `const { readFile } = require("fs").promises; readFile("/abs/a.js")`,
			want:    []string{"/abs/a.js"},
		},
		{
			name:    "sync method on fs module",
			program: `const { readFileSync } = require("fs"); readFileSync("/abs/b.js")`,
			want:    []string{"/abs/b.js"},
		},
		{
			name:    "sync method destructured from promises does not exist",
			program: `const { readdirSync } = require("fs").promises; readdirSync("/abs/dir")`,
			want:    []string{},
		},
		{
			name:    "readFileSync destructured from promises does not exist",
			program: `const { readFileSync } = require("fs").promises; readFileSync("/abs/c.js")`,
			want:    []string{},
		},
		{
			name:    "import spelling of the same mistake",
			program: `import { readFileSync } from "fs/promises"; readFileSync("/abs/d.js")`,
			want:    []string{},
		},
		{
			name:    "import spelling of the correct method",
			program: `import { readFile } from "fs/promises"; readFile("/abs/e.js")`,
			want:    []string{"/abs/e.js"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := jsRegionReadsFor(t, `node -e '`+testCase.program+`'`)
			if !equalStrings(got, testCase.want) {
				t.Fatalf("reads = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestJavaScriptRenamedBindingRespectsItsModule covers the renamed spelling of
// the same rule, so the module is carried through a pair pattern too.
func TestJavaScriptRenamedBindingRespectsItsModule(t *testing.T) {
	cases := []struct {
		name    string
		program string
		want    []string
	}{
		{
			name:    "renamed promises method",
			program: `const { readFile: rf } = require("fs").promises; rf("/abs/a.js")`,
			want:    []string{"/abs/a.js"},
		},
		{
			name:    "renamed sync method from promises does not exist",
			program: `const { readFileSync: rf } = require("fs").promises; rf("/abs/b.js")`,
			want:    []string{},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := jsRegionReadsFor(t, `node -e '`+testCase.program+`'`)
			if !equalStrings(got, testCase.want) {
				t.Fatalf("reads = %v, want %v", got, testCase.want)
			}
		})
	}
}
