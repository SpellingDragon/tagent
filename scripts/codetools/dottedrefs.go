// 本文件负责点号模块引用门：机器会真去 import 的引用必须可解析或显式登记。
// 规格: docs/comment-gate-tooling.md#dotted-refs
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// pythonModuleRe captures the module after a `python -m` invocation, allowing
// intermediate flags. The module must start with a letter or underscore so curl's
// `-m 3` timeout and install's `-m 644` mode cannot match.
var pythonModuleRe = regexp.MustCompile(`\bpython3?(\s+-{1,2}[\w.-]+)*\s+-m\s+([A-Za-z_][\w.]*)`)

// yamlWorkflowRe captures the value of a top-level-ish `workflow:` mapping key.
var yamlWorkflowRe = regexp.MustCompile(`^\s*workflow:\s*(\S+)\s*$`)

// moduleShapeRe is what a resolvable dotted module reference must look like.
var moduleShapeRe = regexp.MustCompile(`^[A-Za-z_]\w*(\.[A-Za-z_]\w*)+$`)

// externalModuleAllowlist registers dotted module prefixes that resolve OUTSIDE
// this repository (installed packages, stdlib tools). An entry needs a reason;
// an empty table is the healthy state — the gate exists to keep it that way.
var externalModuleAllowlist = map[string]string{}

// refSite is one machine-consumed dotted module reference found in a file.
type refSite struct {
	file   string
	line   int
	module string
}

// extractModuleRefs pulls the dotted references a runtime would actually import
// from one line: `python -m <mod>` in shell, `workflow: <mod>` in yaml. Position
// precision is the whole point — `scheduler.type=local` config overrides and
// prose mentions of dotted names are deliberately out of scope.
func extractModuleRefs(path, line string, ln int) []refSite {
	var out []refSite
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		if m := yamlWorkflowRe.FindStringSubmatch(line); m != nil && moduleShapeRe.MatchString(m[1]) {
			out = append(out, refSite{path, ln, m[1]})
		}
		return out
	}
	for _, m := range pythonModuleRe.FindAllStringSubmatch(line, -1) {
		if moduleShapeRe.MatchString(m[2]) {
			out = append(out, refSite{path, ln, m[2]})
		}
	}
	return out
}

// checkDottedRefs resolves each site against a resolver; unresolved sites that
// miss the allowlist become findings. Pure over resolve for unit testing.
func checkDottedRefs(sites []refSite, resolve func(string) bool) []string {
	var out []string
	for _, s := range sites {
		if resolve(s.module) {
			continue
		}
		if _, ok := allowlistedPrefix(s.module); ok {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%d: %s (dotted module reference resolves to no repository file and carries no allowlist entry)", s.file, s.line, s.module))
	}
	return out
}

// allowlistedPrefix reports whether any registered prefix covers the module.
func allowlistedPrefix(module string) (string, bool) {
	for prefix := range externalModuleAllowlist {
		if module == prefix || strings.HasPrefix(module, prefix+".") {
			return prefix, true
		}
	}
	return "", false
}

// scanDottedRefFiles lists the tracked shell and yaml files the gate reads.
func scanDottedRefFiles() ([]string, error) {
	out, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" {
			continue
		}
		if strings.Contains(p, "archive/") || strings.Contains(p, "_tmp_report") || strings.HasPrefix(p, "openspec/") {
			continue
		}
		if strings.HasSuffix(p, ".sh") || strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml") {
			files = append(files, p)
		}
	}
	return files, nil
}

func runDottedRefs([]string) int {
	files, err := scanDottedRefFiles()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dotted-refs: git ls-files failed:", err)
		return 1
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dotted-refs: getwd failed:", err)
		return 1
	}
	if st, err := os.Stat(filepath.Join(root, ".git")); err != nil || !st.IsDir() {
		fmt.Fprintln(os.Stderr, "dotted-refs: not run from a repository root")
		return 1
	}
	var sites []refSite
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dotted-refs: unreadable %s: %v\n", f, err)
			return 1
		}
		for i, line := range strings.Split(string(raw), "\n") {
			sites = append(sites, extractModuleRefs(f, line, i+1)...)
		}
	}
	findings := checkDottedRefs(sites, func(mod string) bool {
		rel := filepath.FromSlash(strings.ReplaceAll(mod, ".", "/"))
		for _, cand := range []string{rel + ".py", filepath.Join(rel, "__init__.py")} {
			if st, err := os.Stat(filepath.Join(root, cand)); err == nil && !st.IsDir() {
				return true
			}
		}
		return false
	})
	for _, f := range findings {
		fmt.Println("dotted-refs:", f)
	}
	if len(findings) > 0 {
		return 1
	}
	return 0
}
