package shelldecomp

import (
	"testing"
)

// TestRubyPlainStringsStillResolve is the regression for making the ruby string
// reader fail closed on a child kind it does not model, matching the python
// reader. The guard itself cannot fire with the vendored grammar, whose string
// children are closed at string_content, interpolation, and escape_sequence,
// and escape_sequence is already unreachable behind the backslash check. There
// is therefore no input that distinguishes the guard, and a test asserting it
// could never fail.
//
// What can be asserted is that the change costs nothing: every literal shape
// that resolved before still resolves.
func TestRubyPlainStringsStillResolve(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{
			name:    "single quoted absolute",
			command: `ruby -e "File.read('/abs/a.rb')"`,
			want:    []string{"/abs/a.rb"},
		},
		{
			name:    "double quoted absolute",
			command: `ruby -e 'File.read("/abs/b.rb")'`,
			want:    []string{"/abs/b.rb"},
		},
		{
			name:    "relative path",
			command: `ruby -e "File.read('lib/c.rb')"`,
			want:    []string{"/repo/lib/c.rb"},
		},
		{
			name:    "readlines",
			command: `ruby -e "File.readlines('/abs/d.rb')"`,
			want:    []string{"/abs/d.rb"},
		},
		{
			name:    "directory entries",
			command: `ruby -e "Dir.entries('/abs/dir')"`,
			want:    []string{"/abs/dir"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decomposition := ParseWithOptions(testCase.command, "/repo", Options{Home: "/home/u"})
			got := rubyRegionReads(t, decomposition)
			if !equalStrings(got, testCase.want) {
				t.Fatalf("reads = %v, want %v", got, testCase.want)
			}
		})
	}
}
