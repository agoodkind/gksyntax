package shelldecomp

import (
	"strings"
	"testing"
)

// resolvableWritePaths returns the resolvable write-target paths of a command.
func resolvableWritePaths(decomposition *Decomposition) []string {
	var paths []string
	for _, write := range decomposition.WriteTargets() {
		if write.Resolvable {
			paths = append(paths, write.Path)
		}
	}
	return paths
}

func TestTeeWithInputRedirectStillWrites(t *testing.T) {
	decomposition := Parse(`tee out.txt < in.txt`, "/w", "/home/u")
	paths := resolvableWritePaths(decomposition)
	if len(paths) != 1 || paths[0] != "/w/out.txt" {
		t.Fatalf("write paths = %v, want [/w/out.txt] (input redirect must not suppress the tee write)", paths)
	}
}

func TestPatchWithInputRedirectStillWrites(t *testing.T) {
	decomposition := Parse(`patch f.txt < diff.patch`, "/w", "/home/u")
	paths := resolvableWritePaths(decomposition)
	if len(paths) != 1 || paths[0] != "/w/f.txt" {
		t.Fatalf("write paths = %v, want [/w/f.txt]", paths)
	}
}

func TestHeredocThenOutputRedirectWrites(t *testing.T) {
	decomposition := Parse("cat <<EOF >> /tmp/log\nx\nEOF", "/w", "/home/u")
	paths := resolvableWritePaths(decomposition)
	if len(paths) != 1 || paths[0] != "/tmp/log" {
		t.Fatalf("write paths = %v, want [/tmp/log] (output redirect after a heredoc must be recorded)", paths)
	}
	if len(decomposition.EmbeddedRegions()) != 1 {
		t.Fatalf("embedded regions = %d, want 1 (the heredoc body)", len(decomposition.EmbeddedRegions()))
	}
}

func TestProcessSubstitutionIsNotFabricatedWrite(t *testing.T) {
	decomposition := Parse(`echo hi > >(tee log)`, "/w", "/home/u")
	for _, write := range decomposition.WriteTargets() {
		if strings.Contains(write.Path, ">(") || strings.Contains(write.Path, "tee log") {
			t.Fatalf("fabricated write path from process substitution: %q", write.Path)
		}
	}
}

func TestRedirectedGrepKeepsReadAndWrite(t *testing.T) {
	decomposition := Parse(`grep x file.txt > out.txt`, "/w", "/home/u")
	reads := decomposition.ReadTargets()
	if len(reads) != 1 || !reads[0].Resolvable || reads[0].Path != "/w/file.txt" {
		t.Fatalf("read targets = %+v, want one resolvable /w/file.txt (redirected reader must keep its read)", reads)
	}
	writes := resolvableWritePaths(decomposition)
	if len(writes) != 1 || writes[0] != "/w/out.txt" {
		t.Fatalf("write paths = %v, want [/w/out.txt]", writes)
	}
}
