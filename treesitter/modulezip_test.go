package treesitter

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gitlinkMode is the git index mode of a submodule entry. A Go module zip
// includes none of the files inside a submodule.
const gitlinkMode = "160000"

// TestModuleZipContentsBuild builds every package of this module from a copy of
// the files that a Go module zip of the repository contains. The copy includes
// each git-tracked regular file. It leaves out submodules, nested modules, and
// vendor directories, which module zips also leave out. The build disables the
// workspace, matching a consumer that requires goodkind.io/gksyntax from the
// module proxy. The build fails when a cgo grammar compiles C sources from a
// submodule, from a build-time generator, or from a file that git ignores.
func TestModuleZipContentsBuild(t *testing.T) {
	moduleRoot := moduleRootDir(t)
	zipTree := t.TempDir()
	for _, trackedPath := range moduleZipPaths(t, moduleRoot) {
		copyTrackedFile(t, moduleRoot, zipTree, trackedPath)
	}

	build := exec.Command("go", "build", "./...")
	build.Dir = zipTree
	// GOFLAGS replaces any inherited value. An inherited -trimpath removes the
	// package directory from the build cache key, and an object cached from the
	// repository checkout could then mask a C source that the copy lacks.
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./... over the module zip contents failed: %v\n%s", err, output)
	}
}

// moduleRootDir returns the nearest directory at or above the test working
// directory that contains a go.mod file.
func moduleRootDir(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("read working directory: %v", err)
	}
	for {
		_, statErr := os.Stat(filepath.Join(directory, "go.mod"))
		if statErr == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatalf("no go.mod at or above the test working directory")
		}
		directory = parent
	}
}

// moduleZipPaths lists the git-tracked paths under moduleRoot that a module zip
// includes. The test skips when moduleRoot is not a git work tree. That case is
// a copy extracted from a module zip, and compiling this test package there
// already built every grammar from the zip contents.
func moduleZipPaths(t *testing.T, moduleRoot string) []string {
	t.Helper()
	probe := exec.Command("git", "-C", moduleRoot, "rev-parse", "--is-inside-work-tree")
	probeOutput, probeErr := probe.CombinedOutput()
	if probeErr != nil {
		t.Skipf("%s is not a git work tree (%v: %s)", moduleRoot, probeErr, bytes.TrimSpace(probeOutput))
	}

	listing, err := exec.Command("git", "-C", moduleRoot, "ls-files", "--stage", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files --stage: %v", err)
	}
	records := strings.Split(strings.TrimSuffix(string(listing), "\x00"), "\x00")
	trackedPaths := make([]string, 0, len(records))
	nestedModuleDirs := make([]string, 0)
	for _, record := range records {
		metadata, trackedPath, found := strings.Cut(record, "\t")
		if !found {
			t.Fatalf("git ls-files --stage record %q has no tab", record)
		}
		if strings.HasPrefix(metadata, gitlinkMode+" ") {
			continue
		}
		if trackedPath != "go.mod" && path.Base(trackedPath) == "go.mod" {
			nestedModuleDirs = append(nestedModuleDirs, path.Dir(trackedPath)+"/")
		}
		trackedPaths = append(trackedPaths, trackedPath)
	}

	zipPaths := make([]string, 0, len(trackedPaths))
	for _, trackedPath := range trackedPaths {
		if insideNestedModule(trackedPath, nestedModuleDirs) || insideVendorDirectory(trackedPath) {
			continue
		}
		zipPaths = append(zipPaths, trackedPath)
	}
	return zipPaths
}

// insideNestedModule reports whether a tracked path lies under a subdirectory
// that has its own go.mod. A module zip leaves out that subdirectory.
func insideNestedModule(trackedPath string, nestedModuleDirs []string) bool {
	for _, moduleDir := range nestedModuleDirs {
		if strings.HasPrefix(trackedPath, moduleDir) {
			return true
		}
	}
	return false
}

// insideVendorDirectory reports whether a tracked path has a directory named
// vendor among its parents. A module zip leaves out vendored packages, and this
// check treats every vendor directory that way.
func insideVendorDirectory(trackedPath string) bool {
	parents := strings.Split(path.Dir(trackedPath), "/")
	return slices.Contains(parents, "vendor")
}

// copyTrackedFile copies one tracked regular file from the checkout into the
// zip tree. A tracked path that no longer exists in the checkout is a pending
// deletion and is not copied. A symlink is not copied because module zips
// include only regular files.
func copyTrackedFile(t *testing.T, moduleRoot string, zipTree string, trackedPath string) {
	t.Helper()
	source := filepath.Join(moduleRoot, filepath.FromSlash(trackedPath))
	info, err := os.Lstat(source)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("stat %s: %v", source, err)
	}
	if !info.Mode().IsRegular() {
		return
	}
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	destination := filepath.Join(zipTree, filepath.FromSlash(trackedPath))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(destination), err)
	}
	if err := os.WriteFile(destination, content, info.Mode().Perm()); err != nil {
		t.Fatalf("write %s: %v", destination, err)
	}
}
