package shelldecomp

import "testing"

// readPaths returns the resolvable read-target paths of a decomposition.
func readPaths(decomposition *Decomposition) []string {
	var paths []string
	for _, target := range decomposition.ReadTargets() {
		if target.Resolvable {
			paths = append(paths, target.Path)
		}
	}
	return paths
}

// hasNoFabricatedReadPath asserts no resolvable read target carries a shell
// metacharacter, which would mean a program or option value was joined into a
// path.
func hasNoFabricatedReadPath(t *testing.T, decomposition *Decomposition) {
	t.Helper()
	for _, target := range decomposition.ReadTargets() {
		if !target.Resolvable {
			continue
		}
		for _, marker := range []string{"$", "{", "`", "(", "\t", "\n"} {
			if containsRune(target.Path, marker) {
				t.Errorf("read target path %q contains %q (fabricated from a non-path operand)", target.Path, marker)
			}
		}
	}
}

func containsRune(s, sub string) bool {
	return len(sub) > 0 && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestAwkScriptIsNotReadPath(t *testing.T) {
	decomposition := Parse(`awk '{print $1}' data.txt`, "/w", "/home/u")
	hasNoFabricatedReadPath(t, decomposition)
	if got := readPaths(decomposition); len(got) != 1 || got[0] != "/w/data.txt" {
		t.Fatalf("read paths = %v, want [/w/data.txt]", got)
	}
}

func TestAwkWithFieldSeparatorScriptIsNotReadPath(t *testing.T) {
	decomposition := Parse(`awk -F: '{print $1}' data.txt`, "/w", "/home/u")
	hasNoFabricatedReadPath(t, decomposition)
	if got := readPaths(decomposition); len(got) != 1 || got[0] != "/w/data.txt" {
		t.Fatalf("read paths = %v, want [/w/data.txt]", got)
	}
}

func TestSedScriptIsNotReadPath(t *testing.T) {
	decomposition := Parse(`sed 's/a/b/' data.txt`, "/w", "/home/u")
	hasNoFabricatedReadPath(t, decomposition)
	if got := readPaths(decomposition); len(got) != 1 || got[0] != "/w/data.txt" {
		t.Fatalf("read paths = %v, want [/w/data.txt]", got)
	}
}

func TestJqFilterIsNotReadPath(t *testing.T) {
	decomposition := Parse(`jq '.items[]' data.json`, "/w", "/home/u")
	hasNoFabricatedReadPath(t, decomposition)
	if got := readPaths(decomposition); len(got) != 1 || got[0] != "/w/data.json" {
		t.Fatalf("read paths = %v, want [/w/data.json]", got)
	}
}

func TestProcessSubstitutionIsNotReadPath(t *testing.T) {
	decomposition := Parse(`grep -F -f <(cat patterns) target.txt`, "/w", "/home/u")
	hasNoFabricatedReadPath(t, decomposition)
}

func TestFullPathBinaryClassifiesByBasename(t *testing.T) {
	decomposition := Parse(`/usr/bin/grep -rn pat /tmp/log`, "/w", "/home/u")
	hasNoFabricatedReadPath(t, decomposition)
	command, found := findCommand(decomposition, "grep")
	if !found {
		t.Fatal("expected /usr/bin/grep to classify as grep")
	}
	if command.Kind != CommandKindSearch {
		t.Fatalf("kind = %q, want %q", command.Kind, CommandKindSearch)
	}
	if got := readPaths(decomposition); len(got) != 1 || got[0] != "/tmp/log" {
		t.Fatalf("read paths = %v, want [/tmp/log]", got)
	}
}
