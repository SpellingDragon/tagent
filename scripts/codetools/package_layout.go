// 本文件负责包布局门：文件名只写小写下划线、每包一份正名骨架断言、测试文件镜像生产文件。
// 规格: docs/comment-gate-tooling.md#package-layout
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// skeletonAssertBase is the only name a package may give its compile-time shape
// pin. A second spelling is a variant of the same file, not a second file: the
// assertions are one contract, so the package keeps one file for them.
const skeletonAssertBase = "skeleton_assert.go"

// lowerSnakeGo is the only file-name spelling the repository accepts: lower-case
// words joined by single underscores. Go permits other spellings, which is exactly
// why the gate has to state this one (sort order, import diffs and case-insensitive
// filesystems all depend on a single spelling).
var lowerSnakeGo = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*\.go$`)

// contractExamplesDir holds the generated contract samples, whose file name is the
// interface it implements (`<Iface>_example.go`). That spelling is enforced by
// scripts/gates/check_concept_budget.py, so a second naming rule here would either
// duplicate or contradict it.
const contractExamplesDir = "examples/contract/"

// mirrorVariantSuffixes is the frozen mirror-tolerance list shared with
// scripts/comment_policy: only the existing real-API variant may mirror through a
// suffix. Growth here would launder a missing mirror, so it is a deliberate review.
var mirrorVariantSuffixes = []string{"_real"}

// mirrorExemptTrees hold Go files that are not a production family: contract
// samples (named by interface) and the integration harness packages. Their
// production files are fixtures for driving the runtime, so no test file there
// can mirror a family member.
var mirrorExemptTrees = []string{contractExamplesDir, "tests/integration/"}

// mirrorExemptFiles are the cross-cutting proofs whose subject is an assembled
// face rather than one file beside them: an end-to-end smoke drives the whole
// example host, and a lifecycle proof drives the query faces of one package
// across its exec/state/monitor files, so no single production stem exists to
// mirror. Exact paths keep the exemption from covering a neighbour that does
// have a family; a renamed or copied fixture must be reviewed as a new entry.
//
// The second group holds the same kind of proof from the hot-update work: each subject is an assembled face or a seam spanning files, and the sibling <production>_test.go already mirrors those files, so renaming one of these to a production stem would only make the file name state the wrong subject.
//   - assembly/anchored_boot_parity_test.go: one rebuild's projection and judgments across host_reload, host_document and host_delivery
//   - assembly/genregistry_rebuild_test.go: the generation-scoped registry, whose subject lives in runtime/state while the composition root drives it
//   - assembly/host_delivery_bench_test.go: the benchmark counterpart of host_delivery.go, beside the existing host_delivery_test.go
//   - assembly/hotupdate_anchor_test.go: the symptom anchor of a running Host.Reload that moves one agent's exec section (build tag only)
//   - assembly/hotupdate_generation_test.go: the three per-generation host faces — native face, document dialect and self-registration
//   - assembly/recovery_judgment_test.go: recovery judgments answered alike on the assembled faces
//   - assembly/recovery_sites_test.go: the recovery sites over the call chain
//   - assembly/reload_outcome_test.go: one reload attempt files one outcome, across host_reload and settle_turn
//   - assembly/retired_state_hygiene_test.go: retired-state hygiene across commit, host_capture and run
//   - memory/ledger/original_write_ahead_test.go: one original's write-ahead boundary across original_store, localfile and journal
//   - runtime/agent/consumer_round_seed_test.go: the consumer round seed across consumer and loop
//   - runtime/agent/effective_config_test.go: the effective config across loop, request_assembly and the model stand-in
//   - tool/recall/get_public_test.go: get's public entry over the durable read face, beside the existing get_test.go
var mirrorExemptFiles = []string{
	"examples/wechat-bot/smoke_test.go",
	"examples/wechat-bot/smoke_harness_test.go",
	"examples/wechat-bot/smoke_data_test.go",
	"examples/wechat-bot/smoke_errors_test.go",
	"examples/wechat-bot/smoke_bench_test.go",
	"examples/wechat-bot/smoke_recall_test.go",
	"examples/wechat-bot/smoke_skill_test.go",
	"examples/wechat-bot/smoke_mcp_test.go",
	"examples/wechat-bot/smoke_exec_test.go",
	"tool/exec/event_integrity_test.go",
	"assembly/anchored_boot_parity_test.go",
	"assembly/genregistry_rebuild_test.go",
	"assembly/host_delivery_bench_test.go",
	"assembly/hotupdate_anchor_test.go",
	"assembly/hotupdate_generation_test.go",
	"assembly/recovery_judgment_test.go",
	"assembly/recovery_sites_test.go",
	"assembly/reload_outcome_test.go",
	"assembly/retired_state_hygiene_test.go",
	"memory/ledger/original_write_ahead_test.go",
	"runtime/agent/consumer_round_seed_test.go",
	"runtime/agent/effective_config_test.go",
	"tool/recall/get_public_test.go",
}

// layoutFile is one Go source the layout rules judge, as a repo-relative path.
type layoutFile struct {
	path string
	dir  string
	base string
	test bool
}

// layoutFinding is one violation: the path plus the rule that rejected it.
type layoutFinding struct {
	path  string
	rule  string
	cause string
}

func (f layoutFinding) String() string {
	return fmt.Sprintf("%s: %s (%s)", f.path, f.rule, f.cause)
}

// checkPackageLayout applies the three rules to a file list. It is pure so the gate
// is unit-testable without a repository, and so the same predicate list is what the
// live command feeds from the walked tree.
func checkPackageLayout(files []layoutFile) []layoutFinding {
	production := map[string]map[string]bool{}
	skeletons := map[string][]string{}
	for _, f := range files {
		if f.test {
			continue
		}
		if production[f.dir] == nil {
			production[f.dir] = map[string]bool{}
		}
		production[f.dir][strings.TrimSuffix(f.base, ".go")] = true
		if strings.Contains(f.base, "skeleton") {
			skeletons[f.dir] = append(skeletons[f.dir], f.base)
		}
	}

	var out []layoutFinding
	for _, f := range files {
		if !strings.HasPrefix(f.path, contractExamplesDir) && !lowerSnakeGo.MatchString(f.base) {
			out = append(out, layoutFinding{f.path, "file-name-not-lower-snake", "rename to lower_snake.go: no capitals, hyphens or spaces"})
		}
		if f.test || !strings.Contains(f.base, "skeleton") || f.base == skeletonAssertBase {
			continue
		}
		out = append(out, layoutFinding{f.path, "skeleton-variant", "one shape pin per package: merge into " + skeletonAssertBase})
	}
	for _, dir := range sortedKeys(skeletons) {
		names := skeletons[dir]
		if len(names) > 1 {
			out = append(out, layoutFinding{dir, "skeleton-collision", "one shape pin per package, found " + strings.Join(names, ", ")})
		}
	}
	for _, f := range files {
		if !f.test || exemptFromMirror(f.path) || len(production[f.dir]) == 0 {
			continue
		}
		if !mirrorsProduction(production[f.dir], testStem(f.base)) {
			out = append(out, layoutFinding{f.path, "test-mirror-missing", "name a test file <production>_test.go beside the file it guards"})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].path != out[j].path {
			return out[i].path < out[j].path
		}
		return out[i].rule < out[j].rule
	})
	return out
}

// testStem strips the extension and the _test marker, leaving the production stem
// the file must mirror.
func testStem(base string) string {
	return strings.TrimSuffix(strings.TrimSuffix(base, ".go"), "_test")
}

// mirrorsProduction reports whether the stem names a production file of the same
// directory, exactly or through a frozen variant suffix. Exact spelling is the
// point: it aligns the name with the file it guards, and a fuzzy match would
// dissolve the pressure to align it.
func mirrorsProduction(family map[string]bool, stem string) bool {
	if family[stem] {
		return true
	}
	for _, v := range mirrorVariantSuffixes {
		if s := strings.TrimSuffix(stem, v); s != stem && family[s] {
			return true
		}
	}
	return false
}

// exemptFromMirror reports whether the path lives in a tree that has no production
// family to mirror.
func exemptFromMirror(path string) bool {
	for _, exact := range mirrorExemptFiles {
		if path == exact {
			return true
		}
	}
	for _, prefix := range mirrorExemptTrees {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// skipDirNames are trees that hold no first-party source: version control, third
// party modules and test fixtures (whose file names are deliberately irregular).
var skipDirNames = map[string]bool{"vendor": true, "node_modules": true, "testdata": true}

// collectLayoutFiles walks the given roots and returns every Go source under them.
// Hidden directories are skipped (tool and runtime space), as are the names above.
func collectLayoutFiles(roots []string) ([]layoutFile, error) {
	seen := map[string]bool{}
	var out []layoutFile
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if p == root {
					return nil
				}
				if skipDirNames[name] || strings.HasPrefix(name, ".") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") {
				return nil
			}
			path := filepath.ToSlash(filepath.Clean(p))
			if seen[path] {
				return nil
			}
			seen[path] = true
			out = append(out, layoutFile{
				path: path,
				dir:  filepath.ToSlash(filepath.Dir(path)),
				base: filepath.Base(path),
				test: strings.HasSuffix(path, "_test.go"),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

func runPackageLayout(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: codetools package-layout <dir>...")
		return 2
	}
	files, err := collectLayoutFiles(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "package-layout: walk failed:", err)
		return 2
	}
	findings := checkPackageLayout(files)
	for _, f := range findings {
		fmt.Println("package-layout:", f)
	}
	if len(findings) > 0 {
		return 1
	}
	fmt.Printf("package-layout: %d file(s); names, skeleton pins and test mirrors hold\n", len(files))
	return 0
}
