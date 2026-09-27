package shelldecomp

import (
	"slices"
	"testing"
)

// sortedReadPaths returns the resolvable read-target paths in a stable order,
// so a test can compare the whole set rather than one entry.
func sortedReadPaths(decomposition *Decomposition) []string {
	paths := append([]string(nil), readPaths(decomposition)...)
	slices.Sort(paths)
	return paths
}

// TestJqTwoValueFlagsDoNotFabricateAPath is the regression for a flag whose
// value count was wrong. jq's --arg takes a name and a value, but the scan
// skipped one operand, so the name was consumed as the flag's value, the value
// became jq's program, and the real program became a path.
//
// Measured on 2026-08-01: `jq --arg x 1 '.x' /repo/a.json /repo/b.json`
// resolved to /repo/.x plus the two real files, and /repo/.x exists nowhere.
func TestJqTwoValueFlagsDoNotFabricateAPath(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{
			name:    "arg",
			command: `jq --arg x 1 '.x' /repo/a.json`,
			want:    []string{"/repo/a.json"},
		},
		{
			name:    "argjson",
			command: `jq --argjson n 1 '.n' /repo/a.json`,
			want:    []string{"/repo/a.json"},
		},
		{
			name:    "two args before the program",
			command: `jq --arg a 1 --arg b 2 '.a' /repo/a.json /repo/b.json`,
			want:    []string{"/repo/a.json", "/repo/b.json"},
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

// TestJqFileValuedFlagsResolveTheirDataFile covers the other half: --slurpfile
// and --rawfile also take two operands, and the second one is a file jq reads.
// Skipping it entirely would hide a real read.
func TestJqFileValuedFlagsResolveTheirDataFile(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{
			name:    "slurpfile",
			command: `jq --slurpfile s /repo/side.json '.x' /repo/a.json`,
			want:    []string{"/repo/a.json", "/repo/side.json"},
		},
		{
			name:    "rawfile",
			command: `jq --rawfile r /repo/raw.txt '.x' /repo/a.json`,
			want:    []string{"/repo/a.json", "/repo/raw.txt"},
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

// TestJqProgramFileIsNotADataRead pins existing intent rather than changing it.
// jq -f names the program, and a program is not data the command searched, so
// it is deliberately not a read target. This test exists so the two-value work
// below cannot alter that by accident.
func TestJqProgramFileIsNotADataRead(t *testing.T) {
	got := sortedReadPaths(Parse(`jq -f /repo/prog.jq /repo/a.json`, "/repo", "/home/u"))
	want := []string{"/repo/a.json"}
	if !slices.Equal(got, want) {
		t.Fatalf("read paths = %v, want %v", got, want)
	}
}
