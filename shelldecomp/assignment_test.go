package shelldecomp

import "testing"

func TestAssignmentsExposeLiteralFacts(t *testing.T) {
	decomposition := Parse("E='/repo/src'\ngrep -rn X "+`"$E"`, "/work", "/home/u")
	assignments := decomposition.Assignments()
	if len(assignments) != 1 {
		t.Fatalf("assignments = %d, want 1", len(assignments))
	}
	if assignments[0].Name != "E" {
		t.Fatalf("assignment name = %q, want E", assignments[0].Name)
	}
	if !assignments[0].Resolvable || assignments[0].Value != "/repo/src" {
		t.Fatalf("assignment = %+v, want resolvable /repo/src", assignments[0])
	}
	if assignments[0].ScopeID != 0 {
		t.Fatalf("assignment scope = %d, want 0", assignments[0].ScopeID)
	}
}

func TestAssignmentResolvesLaterQuotedExpansion(t *testing.T) {
	decomposition := Parse("E=/repo/src\ngrep -rn X "+`"$E"`, "/work", "/home/u")
	if got := readPaths(decomposition); len(got) != 1 || got[0] != "/repo/src" {
		t.Fatalf("read paths = %v, want [/repo/src]", got)
	}
}

func TestAssignmentResolvesLaterUnquotedExpansion(t *testing.T) {
	decomposition := Parse("E=/repo/src\nrg needle $E", "/work", "/home/u")
	if got := readPaths(decomposition); len(got) != 1 || got[0] != "/repo/src" {
		t.Fatalf("read paths = %v, want [/repo/src]", got)
	}
}

func TestUnknownExpansionStaysUnresolvable(t *testing.T) {
	decomposition := Parse(`grep -rn X "$E"`, "/work", "/home/u")
	if len(decomposition.ReadTargets()) != 1 {
		t.Fatalf("read targets = %d, want 1", len(decomposition.ReadTargets()))
	}
	if decomposition.ReadTargets()[0].Resolvable {
		t.Fatalf("read target = %+v, want unresolvable", decomposition.ReadTargets()[0])
	}
}

func TestSubshellAssignmentDoesNotLeak(t *testing.T) {
	decomposition := Parse("( E=/repo/src; grep -rn X "+`"$E"`+" )\ngrep -rn X "+`"$E"`, "/work", "/home/u")
	if len(decomposition.ReadTargets()) != 2 {
		t.Fatalf("read targets = %d, want 2", len(decomposition.ReadTargets()))
	}
	if !decomposition.ReadTargets()[0].Resolvable || decomposition.ReadTargets()[0].Path != "/repo/src" {
		t.Fatalf("first read target = %+v, want resolvable /repo/src", decomposition.ReadTargets()[0])
	}
	if decomposition.ReadTargets()[1].Resolvable {
		t.Fatalf("second read target = %+v, want unresolvable", decomposition.ReadTargets()[1])
	}
}

func TestCommandSubstitutionAssignmentStaysUnresolvable(t *testing.T) {
	decomposition := Parse("E=$(pwd)\ngrep -rn X "+`"$E"`, "/work", "/home/u")
	if len(decomposition.ReadTargets()) != 1 {
		t.Fatalf("read targets = %d, want 1", len(decomposition.ReadTargets()))
	}
	if decomposition.ReadTargets()[0].Resolvable {
		t.Fatalf("read target = %+v, want unresolvable", decomposition.ReadTargets()[0])
	}
}

func TestAmbientExpansionAssignmentStaysUnresolvable(t *testing.T) {
	decomposition := Parse("E=$HOME/project\ngrep -rn X "+`"$E"`, "/work", "/home/u")
	if len(decomposition.ReadTargets()) != 1 {
		t.Fatalf("read targets = %d, want 1", len(decomposition.ReadTargets()))
	}
	if decomposition.ReadTargets()[0].Resolvable {
		t.Fatalf("read target = %+v, want unresolvable", decomposition.ReadTargets()[0])
	}
}

func TestCommandPrefixEnvAssignmentDoesNotResolveSiblingExpansion(t *testing.T) {
	decomposition := Parse("E=/repo/src grep -rn X "+`"$E"`, "/work", "/home/u")
	if len(decomposition.ReadTargets()) != 1 {
		t.Fatalf("read targets = %d, want 1", len(decomposition.ReadTargets()))
	}
	if decomposition.ReadTargets()[0].Resolvable {
		t.Fatalf("read target = %+v, want unresolvable", decomposition.ReadTargets()[0])
	}
}

func TestResolveWordUsesEarlierSameScopeAssignment(t *testing.T) {
	decomposition := Parse("E=/repo/src\ngrep -rn X "+`"$E"`, "/work", "/home/u")
	assignments := decomposition.Assignments()
	if len(assignments) != 1 {
		t.Fatalf("assignments = %d, want 1", len(assignments))
	}
	grep, found := findCommand(decomposition, "grep")
	if !found {
		t.Fatal("expected a grep command")
	}
	value, ok := decomposition.ResolveWord(`"$E"`, grep.ScopeID, grep.Node.StartByte)
	if !ok || value != "/repo/src" {
		t.Fatalf("ResolveWord = (%q, %t), want (/repo/src, true)", value, ok)
	}
}
