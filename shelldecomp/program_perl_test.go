package shelldecomp

import (
	"sort"
	"testing"
)

// perlOperandReads returns the resolved top-level read-target paths whose Argv0
// is "perl", which are the data-file operands a perl -n/-p command reads.
func perlOperandReads(t *testing.T, decomposition *Decomposition) []string {
	t.Helper()
	paths := make([]string, 0)
	for _, target := range decomposition.ReadTargets() {
		if target.Argv0 != "perl" {
			continue
		}
		if !target.Resolvable {
			continue
		}
		paths = append(paths, target.Path)
	}
	sort.Strings(paths)
	return paths
}

func TestPerlDashNeReadsOperands(t *testing.T) {
	command := `perl -ne 'print' a.pl b.pl`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlOperandReads(t, decomposition)
	want := []string{"/repo/a.pl", "/repo/b.pl"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPerlDashPeReadsOperand(t *testing.T) {
	command := `perl -pe 's/a/b/' a.pl`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlOperandReads(t, decomposition)
	want := []string{"/repo/a.pl"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPerlDashEWithoutNorPNoOperandRead(t *testing.T) {
	command := `perl -e 'print' a.pl`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlOperandReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none for a -e script without -n/-p", got)
	}
}

func TestPerlClusteredDashLneReadsOperand(t *testing.T) {
	command := `perl -lne 'print' a.pl`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlOperandReads(t, decomposition)
	want := []string{"/repo/a.pl"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPerlDashNCapitalEReadsOperand(t *testing.T) {
	command := `perl -nE 'say' a.pl`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlOperandReads(t, decomposition)
	want := []string{"/repo/a.pl"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPerlSeparateDashNAndDashEReadsOperand(t *testing.T) {
	command := `perl -n -e 'print' a.pl`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlOperandReads(t, decomposition)
	want := []string{"/repo/a.pl"}
	if !equalStrings(got, want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
}

func TestPerlDashNeNonLiteralOperandDropped(t *testing.T) {
	command := `perl -ne 'print' "$f"`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlOperandReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none for an unresolvable operand", got)
	}
}

func TestPerlLoopFlagNoScriptNoOperandRead(t *testing.T) {
	command := `perl -n script.pl data.txt`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlOperandReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none when -n carries no -e/-E script (script.pl is the program file)", got)
	}
}

func TestPerlClusteredDashNeConsumesNextOperandAsScript(t *testing.T) {
	command := `perl -ne a.pl`
	decomposition := ParseWithOptions(command, "/repo", Options{Home: "/home/u"})
	got := perlOperandReads(t, decomposition)
	if len(got) != 0 {
		t.Fatalf("reads = %v, want none: -ne consumes a.pl as the inline script body", got)
	}
}
