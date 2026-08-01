package shelldecomp

import (
	"slices"
	"testing"
)

// TestJqExprFlagsAreToolSpecific is the regression for a flag table shared
// across tools that do not agree on what the flag means.
//
// The -e/-f scan assumed any -e supplies the pattern, which holds for grep and
// rg. jq's -e sets the exit status from the output and supplies nothing, so the
// first bare operand is still jq's program. Treating the program as a path both
// fabricates a target and drops the real input file.
//
// --from-file is the opposite mistake: it does supply jq's program, but was not
// recognized, so the first data file was consumed as the program and lost.
func TestJqExprFlagsAreToolSpecific(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{
			name:    "exit-status flag supplies nothing",
			command: `jq -e '.x' /repo/a.json`,
			want:    []string{"/repo/a.json"},
		},
		{
			name:    "exit-status flag with two inputs",
			command: `jq -e '.x' /repo/a.json /repo/b.json`,
			want:    []string{"/repo/a.json", "/repo/b.json"},
		},
		{
			name:    "from-file supplies the program",
			command: `jq --from-file /repo/prog.jq /repo/a.json`,
			want:    []string{"/repo/a.json"},
		},
		{
			name:    "joined from-file supplies the program",
			command: `jq --from-file=/repo/prog.jq /repo/a.json`,
			want:    []string{"/repo/a.json"},
		},
		{
			name:    "short program flag supplies the program",
			command: `jq -f /repo/prog.jq /repo/a.json`,
			want:    []string{"/repo/a.json"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := sortedReadPaths(Parse(testCase.command, "/repo", "/home/u"))
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("read paths = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestGrepExprFlagsStillSupplyThePattern keeps the tool-specific split from
// changing the family the original rule was written for. grep and rg do take
// their pattern from -e and -f, so the first bare operand is a path.
func TestGrepExprFlagsStillSupplyThePattern(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{
			name:    "grep -e",
			command: `grep -e pattern /repo/a.go`,
			want:    []string{"/repo/a.go"},
		},
		{
			name:    "grep --regexp=",
			command: `grep --regexp=pattern /repo/a.go`,
			want:    []string{"/repo/a.go"},
		},
		{
			name:    "grep without -e takes the pattern positionally",
			command: `grep pattern /repo/a.go`,
			want:    []string{"/repo/a.go"},
		},
		{
			name:    "rg -e",
			command: `rg -e pattern /repo/a.go`,
			want:    []string{"/repo/a.go"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := sortedReadPaths(Parse(testCase.command, "/repo", "/home/u"))
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("read paths = %v, want %v", got, testCase.want)
			}
		})
	}
}
