package shelldecomp

import (
	"slices"
	"testing"
)

// sedRegionTargets parses a sed command and returns the resolved read and write
// paths the sed script's in-program file commands produced, found on the sed
// embedded region's recursive decomposition.
func sedRegionTargets(t *testing.T, command, cwd string) ([]string, []string) {
	t.Helper()
	decomposition := Parse(command, cwd, "/home/u")
	for _, region := range decomposition.EmbeddedRegions() {
		if region.Lang != LangSed || region.Parsed == nil {
			continue
		}
		var reads []string
		for _, target := range region.Parsed.ReadTargets() {
			reads = append(reads, target.Path)
		}
		var writes []string
		for _, target := range region.Parsed.WriteTargets() {
			writes = append(writes, target.Path)
		}
		return reads, writes
	}
	return nil, nil
}

func TestAnalyzeSedReads(t *testing.T) {
	const cwd = "/repo"
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{"r relative", `sed 'r data.txt' in`, []string{"/repo/data.txt"}},
		{"r absolute", `sed 'r /etc/hosts' in`, []string{"/etc/hosts"}},
		{"R command", `sed 'R lines.txt' in`, []string{"/repo/lines.txt"}},
		{"address then r", `sed '/pat/r inc.txt' in`, []string{"/repo/inc.txt"}},
		{"line address then r", `sed '3r inc.txt' in`, []string{"/repo/inc.txt"}},
		{"r after p on same line", `sed 'p;r inc.txt' in`, []string{"/repo/inc.txt"}},
		{"empty r filename", `sed 'r' in`, nil},
		{"w in regex is not a read", `sed 's/w/x/g' in`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reads, _ := sedRegionTargets(t, tc.command, cwd)
			if !slices.Equal(reads, tc.want) {
				t.Fatalf("reads for %q = %v, want %v", tc.command, reads, tc.want)
			}
		})
	}
}

func TestAnalyzeSedWrites(t *testing.T) {
	const cwd = "/repo"
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{"w relative", `sed -n 'w out.txt' in`, []string{"/repo/out.txt"}},
		{"W command", `sed -n 'W first.txt' in`, []string{"/repo/first.txt"}},
		{"s flag w", `sed 's/a/b/w log.txt' in`, []string{"/repo/log.txt"}},
		{"s flag gw", `sed 's/a/b/gw log.txt' in`, []string{"/repo/log.txt"}},
		{"range w", `sed -n '1,5w slice.txt' in`, []string{"/repo/slice.txt"}},
		{"plain substitution no w", `sed 's/a/b/g' in`, nil},
		{"empty w filename", `sed -n 'w' in`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, writes := sedRegionTargets(t, tc.command, cwd)
			if !slices.Equal(writes, tc.want) {
				t.Fatalf("writes for %q = %v, want %v", tc.command, writes, tc.want)
			}
		})
	}
}

// TestAnalyzeSedMultilineScript checks that a newline-separated script surfaces
// a file command on each line.
func TestAnalyzeSedMultilineScript(t *testing.T) {
	reads, writes := sedRegionTargets(t, "sed -n 'r inc.txt\nw out.txt' in", "/repo")
	if !slices.Equal(reads, []string{"/repo/inc.txt"}) {
		t.Fatalf("reads = %v, want [/repo/inc.txt]", reads)
	}
	if !slices.Equal(writes, []string{"/repo/out.txt"}) {
		t.Fatalf("writes = %v, want [/repo/out.txt]", writes)
	}
}
