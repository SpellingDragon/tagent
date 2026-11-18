// 本文件负责追踪卫生门：索引内容必须非空、不是运行期产物、顶层目录已登记。
// 规格: docs/comment-gate-tooling.md#tracked-hygiene
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runtimeArtifactExts are extensions a test run or a live process leaves in the
// working tree (writer locks, relation journals, spill temp files, profiles,
// compiled test binaries). A tracked path ending in one of them is debris that
// entered the index before an ignore rule existed.
var runtimeArtifactExts = []string{".lock", ".journal", ".tmp", ".spill", ".prof", ".test", ".out"}

// registeredTopDirs is the deliberate set of directories allowed at the repo
// root. Adding an entry here is how a new top-level directory gets registered;
// the README layout section is its human-facing counterpart. Dot-directories are
// outside this rule (tool and runtime space); loose root files are too, but the
// module root holds no Go source: the composition root is the assembly/ package.
var registeredTopDirs = map[string]bool{
	".github": true, "api": true, "assembly": true, "bench": true, "cli": true, "docs": true,
	"examples": true, "features": true, "golden": true, "internal": true,
	"memory": true, "openspec": true, "resources": true,
	"runtime": true, "scripts": true, "tests": true, "tool": true,
}

// trackedHygieneFinding is one violation: the path plus the rule that rejected it.
type trackedHygieneFinding struct {
	path  string
	rule  string
	cause string
}

func (f trackedHygieneFinding) String() string {
	return fmt.Sprintf("%s: %s (%s)", f.path, f.rule, f.cause)
}

// checkTrackedHygiene applies the three rules to a tracked path list. It is pure
// over sizeOf so the gate is unit-testable without a git repository, and so the
// same predicate list is what the live command feeds from the index.
func checkTrackedHygiene(files []string, sizeOf func(string) (int64, error)) []trackedHygieneFinding {
	var out []trackedHygieneFinding
	seenTop := map[string]bool{}
	for _, f := range files {
		clean := filepath.ToSlash(f)
		if top, ok := topDirOf(clean); ok && !seenTop[top] {
			seenTop[top] = true
			if !registeredTopDirs[top] {
				out = append(out, trackedHygieneFinding{clean, "unregistered-top-level", "register it in scripts/codetools/hygiene.go and the README layout section"})
			}
		}
		if base := filepath.Base(clean); isRuntimeArtifact(base) {
			out = append(out, trackedHygieneFinding{clean, "runtime-artifact-tracked", "a run or test wrote this into the working tree"})
			continue
		}
		n, err := sizeOf(clean)
		if err != nil {
			out = append(out, trackedHygieneFinding{clean, "tracked-path-missing", "the index holds a path the working tree does not"})
			continue
		}
		if n == 0 {
			out = append(out, trackedHygieneFinding{clean, "empty-tracked-file", "an empty file was committed; delete it or give it content"})
		}
	}
	return out
}

// isRuntimeArtifact reports whether a file name is machine output rather than source.
func isRuntimeArtifact(base string) bool {
	for _, ext := range runtimeArtifactExts {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	return false
}

// topDirOf returns the first path segment of a nested tracked path.
func topDirOf(clean string) (string, bool) {
	i := strings.IndexByte(clean, '/')
	if i <= 0 {
		return "", false
	}
	return clean[:i], true
}

// checkTrackedAndIgnored reports paths that are BOTH in the index and excluded by an
// ignore rule. Such a file is tracked-but-invisible: adding a sibling never stages it and
// `git add <path>` needs -f, so edits and new neighbours silently drift out. A whitelist
// mode .gitignore must name every tracked file it means to keep.
//
// The ignored list must come from `git ls-files -ci --exclude-standard`; feeding paths to
// `git check-ignore` instead is wrong because it also matches NEGATION patterns, which a
// whitelist .gitignore is made of.
func checkTrackedAndIgnored(ignored []string) []trackedHygieneFinding {
	var out []trackedHygieneFinding
	for _, p := range ignored {
		if p == "" {
			continue
		}
		out = append(out, trackedHygieneFinding{filepath.ToSlash(p), "tracked-and-ignored", "drop the ignore rule or untrack the file; a whitelist .gitignore must name tracked files"})
	}
	return out
}

// checkWhitelistUnnamedSource reports Go source that no ignore rule names and no
// index holds. A whitelist-mode .gitignore ("*" first) makes every new file
// invisible to `git status`, so a test file added there is never staged and CI
// never compiles it while the author's `go test` runs it green.
//
// The list must come from `git ls-files -o -i --exclude-standard`: untracked AND
// ignored. Only `*.go` entries are judged, because that is the set the Go test
// face would compile; local scratch scripts outside that face are out of scope.
// A nested checkout comes back as a directory entry and is not a Go file.
func checkWhitelistUnnamedSource(entries []string) []trackedHygieneFinding {
	var out []trackedHygieneFinding
	for _, p := range entries {
		clean := filepath.ToSlash(p)
		if !strings.HasSuffix(clean, ".go") {
			continue
		}
		out = append(out, trackedHygieneFinding{clean, "whitelist-unnamed-source", "a whitelist-mode .gitignore must name this Go file; until it does, it is never staged and CI never compiles it while `go test` runs it locally"})
	}
	return out
}

func runTrackedHygiene([]string) int {
	out, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tracked-hygiene: git ls-files failed:", err)
		return 1
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	ign, err := exec.Command("git", "ls-files", "-ci", "--exclude-standard").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tracked-hygiene: git ls-files -ci failed:", err)
		return 1
	}
	var ignored []string
	for _, p := range strings.Split(string(ign), "\n") {
		if p != "" {
			ignored = append(ignored, p)
		}
	}
	findings := checkTrackedHygiene(files, func(p string) (int64, error) {
		st, err := os.Stat(p)
		if err != nil {
			return 0, err
		}
		return st.Size(), nil
	})
	findings = append(findings, checkTrackedAndIgnored(ignored)...)
	unnamed, err := exec.Command("git", "ls-files", "-o", "-i", "--exclude-standard", "-z").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tracked-hygiene: git ls-files -o -i failed:", err)
		return 1
	}
	var untrackedIgnored []string
	for _, p := range strings.Split(string(unnamed), "\x00") {
		if p != "" {
			untrackedIgnored = append(untrackedIgnored, p)
		}
	}
	findings = append(findings, checkWhitelistUnnamedSource(untrackedIgnored)...)
	for _, f := range findings {
		fmt.Println("tracked-hygiene:", f)
	}
	if len(findings) > 0 {
		return 1
	}
	return 0
}
