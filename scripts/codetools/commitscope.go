// 本文件负责提交范围一致性门：docs 类提交不得夹带代码路径。
// 规格: docs/comment-gate-tooling.md#commit-scope
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// docsOnlyPrefixes are commit subjects that promise "documentation only".
var docsOnlyPrefixes = []string{"docs(", "docs:"}

// codePathSuffixes are the extensions a documentation-only commit must not carry.
// Data/config files under docs/ are documentation; build inputs are not.
var codePathSuffixes = []string{".go", ".sh", ".yml", ".yaml", ".py"}

// commitScopeFindings reports code paths carried by a commit whose subject promises
// documentation only. A `git mv` lands in the index the moment it runs, so a plain
// `git commit` without a pathspec sweeps it into the next commit regardless of what
// that commit was meant to change — which is how a docs-titled commit once broke a
// fresh checkout. The rule is subject-only: labeling a code change as docs is the
// failure mode being closed, not the volume of documentation.
func commitScopeFindings(subject string, files []string) []string {
	isDocs := false
	for _, p := range docsOnlyPrefixes {
		if strings.HasPrefix(subject, p) {
			isDocs = true
			break
		}
	}
	if !isDocs {
		return nil
	}
	var out []string
	for _, f := range files {
		if f == "" {
			continue
		}
		if strings.HasPrefix(f, "docs/") || strings.HasPrefix(f, "openspec/") {
			continue
		}
		if strings.EqualFold(filepath.Ext(f), ".md") {
			continue
		}
		for _, ext := range codePathSuffixes {
			if strings.HasSuffix(f, ext) {
				out = append(out, fmt.Sprintf("%s: docs-titled commit carries code path", f))
				break
			}
		}
	}
	return out
}

func commitFiles(ref string) ([]string, error) {
	args := []string{"show", "--name-only", "--format=", ref}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

func commitSubject(ref string) (string, error) {
	out, err := exec.Command("git", "log", "-1", "--format=%s", ref).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func runCommitScope(args []string) int {
	ref := "HEAD"
	if len(args) > 0 && args[0] != "" {
		ref = args[0]
	}
	subject, err := commitSubject(ref)
	if err != nil {
		fmt.Fprintln(os.Stderr, "commit-scope: git log failed:", err)
		return 1
	}
	files, err := commitFiles(ref)
	if err != nil {
		fmt.Fprintln(os.Stderr, "commit-scope: git show failed:", err)
		return 1
	}
	findings := commitScopeFindings(subject, files)
	for _, f := range findings {
		fmt.Printf("commit-scope: %s (%s)\n", f, ref)
	}
	if len(findings) > 0 {
		fmt.Println("commit-scope: a docs-titled commit must not carry code paths; commit them separately (git mv stages immediately, so use an explicit pathspec)")
		return 1
	}
	return 0
}
