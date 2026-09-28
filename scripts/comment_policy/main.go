// Command comment_policy checks Go sources against the repository's comment
// whitelist: documentation comments may only state a contract, and any pointer to
// longer-lived documentation must use the index form.
//
// Usage:
//
//	comment_policy [-baseline F] [-update-baseline] [-strict] [-no-baseline] [dir...]
//
// A run that consults the ratchet exits non-zero on any count above the baseline; -v
// prints every finding rather than only the regressions. -strict additionally requires
// the converged end state: zero findings.
//
// A directory argument is scanned recursively, skipping subdirectories that carry
// their own go.mod — which is why the ratchet refuses to run over a set that leaves
// a nested module ungated, or a set other than the one the baseline was written over.
// -no-baseline measures a scope without consulting the ratchet at all.
//
// scripts/lint.sh owns the canonical directory set, so a batch author and CI scan the
// same tree; read and lower the baseline through it rather than invoking this command
// with an ad-hoc scope.
//
// Rules are named in the output and documented on the matcher table below. Length
// never decides compliance: a long contract comment is legal and a short piece of
// design narrative is not, so content shape is the only judge.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// finding is one policy violation.
type finding struct {
	Path string
	Line int
	Rule string
	Note string
	Text string
}

func (f finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s | %s", f.Path, f.Line, f.Rule, f.Note, f.Text)
}

// auditMarker matches iteration and process residue inside a doc comment: change
// entry numbers, round numbers, dates and replacement narration.
var auditMarker = regexp.MustCompile(`(?i)(§[0-9]|\b轮[0-9]+\b|\b20[0-9]{2}-[0-9]{2}|已移除|已废弃|已删除|不再|曾经|历史上|教训|审阅发现|原计划|第一版|旧实现|旧版|本次变更|上一轮|这一轮|修复于|重构于|取代原|\blegacy\b|\bpreviously\b|\bno longer\b|\bdeprecated in\b|\bwas removed\b)`)

// rationale matches design reasoning: why a shape was chosen over another.
var rationale = regexp.MustCompile(`(?i)(之所以|理由[:：]|因为.{0,80}(所以|故)|选择.{0,40}(而|但)排[除]|权衡|否决|(排除|摒弃).{0,20}方案|为了避免|为防止|这是关键|关键是|教训是)`)

// mechanismStep matches implementation narration that the code already expresses.
var mechanismStep = regexp.MustCompile(`(?i)(先.{0,30}(再|然后|之后)|在 [A-Za-z0-9_]+ 锁内|按序执行|流程[:：]|步骤[:：]|第[一二三四五六七八九十]步)`)

// docPathRef finds a citation of the documentation tree, which must use the index
// form. Naming a deciding source file (e.g. "see event_bus.go") is legal prose per
// the wiki/code sync contract and is deliberately not matched here.
// A bare `name.md` in prose is usually a runtime asset (a prompt file, a data doc),
// not a documentation citation; only slash-containing paths need the index form.
var docPathRef = regexp.MustCompile(`(openspec/[A-Za-z0-9_./-]+|docs/[A-Za-z0-9_./-]+|[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]*\.md)`)

// changeArtifactRef marks references to change-process artifacts, which are not
// long-lived documentation.
var changeArtifactRef = regexp.MustCompile(`openspec/changes/`)

// indexLine is the only accepted form of a documentation pointer.
var indexLine = regexp.MustCompile(`^\s*(契约|规格):\s+\S+\s*$`)

// indexTargetRoots are the roots a documentation index may point at. The requirement
// tree is deliberately NOT a legal target: a code comment indexes the mechanism
// documentation that explains the code, and that single source is the wiki tree (plus
// each module's README). Measured before tightening: no index pointed there at all.
var indexTargetRoots = []string{"docs/"}

// directivePrefixes and todoLine are the mechanically exempt comment shapes.
var (
	directivePrefixes = []string{"//go:", "//line ", "//export ", "//cgo ", "/*go:", "/*line ", "//nolint:"}
	todoLine          = regexp.MustCompile(`^\s*TODO\([A-Za-z0-9_.-]+\):[^。；;]*$`)
	exportedName      = regexp.MustCompile(`^[A-Z]`)
)

// defaultBaseline is where the ratchet lives, so a plain `comment_policy` run in
// CI enforces "no new violations" without any extra flags.
const defaultBaseline = "scripts/comment_policy/baseline.json"

// baseline stores per-rule violation totals and the scan set they were generated
// over: the ratchet a batch may only lower, never raise. A blocking gate over a
// large legacy backlog would be switched off within a day; a ratchet bites on the
// first new violation and stays on.
type baseline struct {
	Counts map[string]int `json:"counts"`
	// Dirs is the scan set the counts were generated over. collectGoFiles does not
	// descend into a nested module, so a run over a smaller set reports a smaller
	// total for every rule — indistinguishable from a batch that lowered the ratchet.
	Dirs []string `json:"dirs"`
}

func main() {
	var (
		baselinePath = flag.String("baseline", "", "ratchet baseline file (default: "+defaultBaseline+" when present)")
		update       = flag.Bool("update-baseline", false, "write the current counts as the baseline and exit")
		strict       = flag.Bool("strict", false, "require an empty baseline (the converged end state)")
		verbose      = flag.Bool("v", false, "print every finding, not only regressions")
		noRatchet    = flag.Bool("no-baseline", false, "measure a scope only: never read or write the ratchet, so a partial scan cannot lower it by accident")
	)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: comment_policy [-baseline F] [-update-baseline] [-strict] [-no-baseline] [-v] [dir...]")
	}
	flag.Parse()
	dirs := flag.Args()
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	var all []finding
	for _, d := range dirs {
		files, err := collectGoFiles(d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", d, err)
			os.Exit(1)
		}
		for _, f := range files {
			fs2, err := checkFile(f)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
				os.Exit(1)
			}
			all = append(all, fs2...)
		}
	}
	all = dropRedundantPackageDocs(all)
	sort.Slice(all, func(i, j int) bool {
		if all[i].Path != all[j].Path {
			return all[i].Path < all[j].Path
		}
		return all[i].Line < all[j].Line
	})
	counts := map[string]int{}
	for _, f := range all {
		counts[f.Key()]++
	}

	total := 0
	for _, n := range counts {
		total += n
	}
	if *noRatchet {
		if *verbose {
			for _, f := range all {
				fmt.Println(f.String())
			}
		}
		fmt.Printf("comment_policy: %d finding(s) over %v (measured only, ratchet not consulted)\n", total, canonicalDirs(dirs))
		return
	}

	path := *baselinePath
	if path == "" {
		if _, err := os.Stat(defaultBaseline); err == nil {
			path = defaultBaseline
		}
	}
	if *update {
		if path == "" {
			path = defaultBaseline
		}
		if err := checkScanCoverage(".", dirs); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if err := writeBaseline(path, counts, dirs); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Printf("comment_policy: baseline written to %s over %v (%d entries, %d findings)\n", path, canonicalDirs(dirs), len(counts), len(all))
		return
	}

	if *verbose {
		for _, f := range all {
			fmt.Println(f.String())
		}
	}
	if path == "" {
		fmt.Printf("comment_policy: %d violation(s) (no baseline given)\n", len(all))
		if len(all) > 0 {
			os.Exit(1)
		}
		return
	}
	base, err := readBaseline(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		os.Exit(1)
	}
	if err := checkScanSet(".", base.Dirs, dirs); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		os.Exit(1)
	}
	regressions, improvements := compareRatchet(counts, base.Counts)
	// Name every regressed rule with its delta, and show one example finding so a
	// CI log is actionable: a bare count would hide which rule and where.
	type sample struct{ text string }
	one := map[string]sample{}
	for _, f := range all {
		if _, ok := one[f.Key()]; !ok {
			one[f.Key()] = sample{f.String()}
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if counts[k] > base.Counts[k] {
			fmt.Printf("REGRESSION %s: baseline %d -> %d (+%d)\n   e.g. %s\n", k, base.Counts[k], counts[k], counts[k]-base.Counts[k], one[k].text)
		}
	}
	fmt.Printf("comment_policy: %d finding(s); %d beyond baseline; %d ratchet slot(s) can be lowered\n",
		total, regressions, improvements)
	if *strict && total > 0 {
		fmt.Println("comment_policy: -strict requires zero findings (converged end state)")
		os.Exit(1)
	}
	if regressions > 0 {
		fmt.Println("comment_policy: new violations relative to the baseline are not allowed")
		os.Exit(1)
	}
}

// Key identifies a ratchet slot: the rule alone, not (file, rule). Keying by file
// path would turn a consolidation batch — which moves comment text verbatim — into
// hundreds of apparent "new" violations, and would force a re-baseline on every
// rename. Per-rule totals are invariant under moves and still rise the moment new
// narrative text is written, which is what the gate exists to stop.
func (f finding) Key() string { return f.Rule }

// readBaseline loads per-slot counts and the scan set they belong to; a missing file
// means "expect zero".
func readBaseline(path string) (baseline, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return baseline{}, err
	}
	var parsed baseline
	if err := json.Unmarshal(b, &parsed); err != nil {
		return baseline{}, err
	}
	if parsed.Counts == nil {
		parsed.Counts = map[string]int{}
	}
	return parsed, nil
}

// canonicalDirs cleans, deduplicates and sorts a scan set so that a spelling or
// ordering difference cannot be mistaken for a different scope.
func canonicalDirs(dirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		c := filepath.Clean(d)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// allGoFiles collects every Go file under root, descending into nested modules. It is
// the counterpart of collectGoFiles, whose skip rule is what the coverage half of the
// guard exists to catch.
func allGoFiles(root string) (map[string]bool, error) {
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name != root && (name == "vendor" || name == ".git" || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			out[filepath.Clean(rel)] = true
		}
		return nil
	})
	return out, err
}

// checkScanCoverage verifies the scan set against the tree: every nested module must be
// named (its files are otherwise skipped in silence), and no two scan dirs may cover
// the same file (that would inflate a rule's total).
//
// The write path checks this alone: rewriting restamps the scan set deliberately, so a
// changed set is allowed while a set that leaves part of the tree ungated is not — that
// is exactly how a module would drop out of the ratchet with every total still falling.
func checkScanCoverage(root string, effective []string) error {
	all, err := allGoFiles(root)
	if err != nil {
		return err
	}
	count := map[string]int{}
	for _, d := range effective {
		files, err := collectGoFiles(filepath.Join(root, d))
		if err != nil {
			return err
		}
		for _, f := range files {
			rel, e := filepath.Rel(root, f)
			if e != nil {
				return e
			}
			count[filepath.Clean(rel)]++
		}
	}
	var doubled []string
	for f, n := range count {
		if n > 1 {
			doubled = append(doubled, f)
		}
	}
	if len(doubled) > 0 {
		sort.Strings(doubled)
		return fmt.Errorf("scan set %v counts %d Go file(s) twice (e.g. %s) — a nested directory inside the same module inflates every rule it touches", effective, len(doubled), doubled[0])
	}
	var missing []string
	for f := range all {
		if count[f] == 0 {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("scan set %v leaves %d Go file(s) ungated (e.g. %s): a directory with its own go.mod is skipped, so it must be listed explicitly", effective, len(missing), missing[0])
	}
	return nil
}

// checkScanSet is the guard a ratchet run must pass before any count is read as a
// verdict: counts are comparable only when the same tree was scanned, so the set in use
// must equal the one the baseline was generated over, and that set must cover the tree.
// The failure shape being prevented is a partial scan reporting "N slots can be lowered"
// and a following -update-baseline deleting the budget of whatever it skipped.
func checkScanSet(root string, recorded, effective []string) error {
	eff := canonicalDirs(effective)
	rec := canonicalDirs(recorded)
	if len(rec) > 0 && !reflect.DeepEqual(eff, rec) {
		return fmt.Errorf("scan set %v differs from the set the baseline was generated over %v — read or rewrite it through `bash scripts/lint.sh`, or regenerate deliberately with --update-baseline", eff, rec)
	}
	return checkScanCoverage(root, eff)
}

// writeBaseline records the current counts and their scan set, sorted for a stable diff.
func writeBaseline(path string, counts map[string]int, dirs []string) error {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{\n  \"_comment\": \"Per-rule violation counts over the dirs listed in \\\"dirs\\\". A batch may only LOWER these numbers\",\n")
	b.WriteString("  \"_regenerate\": \"bash scripts/lint.sh --update-baseline\",\n")
	buf, err := json.Marshal(canonicalDirs(dirs))
	if err != nil {
		return err
	}
	b.WriteString("  \"dirs\": " + string(buf) + ",\n")
	b.WriteString("  \"counts\": {\n")
	for i, k := range keys {
		sep := ","
		if i == len(keys)-1 {
			sep = ""
		}
		b.WriteString(fmt.Sprintf("    %q: %d%s\n", k, counts[k], sep))
	}
	b.WriteString("  }\n}\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// collectGoFiles walks dir for Go files, not descending into nested modules.
func collectGoFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == dir {
				return nil
			}
			if name := d.Name(); name == "vendor" || name == ".git" || name == "node_modules" || name == "testdata" {
				return filepath.SkipDir
			}
			if st, e := os.Stat(filepath.Join(path, "go.mod")); e == nil && !st.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// checkFile applies every rule to one file.
func checkFile(path string) ([]finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	docSlots := map[*ast.CommentGroup]string{}
	collectDocSlots(file, docSlots)

	var out []finding
	isTest := strings.HasSuffix(path, "_test.go")
	for _, g := range file.Comments {
		text := strings.TrimSpace(g.Text())
		if text == "" || isExempt(g) {
			continue
		}
		line := fset.Position(g.Pos()).Line
		declName, isSlot := docSlots[g]
		if !isSlot {
			out = append(out, finding{Path: path, Line: line, Rule: "free-standing", Note: "comment is not in a documentation slot", Text: firstLine(text)})
			out = append(out, checkDocGroup(path, fset, g, text, isTest, "")...)
			continue
		}
		out = append(out, checkDocGroup(path, fset, g, text, isTest, declName)...)
	}
	out = append(out, checkCoverage(fset, file, path, isTest)...)
	out = append(out, checkDocForm(fset, file, path, isTest)...)
	return out, nil
}

// collectDocSlots records every comment group that sits in a documentation slot.
func collectDocSlots(file *ast.File, into map[*ast.CommentGroup]string) {
	if file.Doc != nil {
		into[file.Doc] = ""
	}
	name := func(names []*ast.Ident) string {
		if len(names) == 0 {
			return ""
		}
		return names[0].Name
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.GenDecl:
			if v.Doc != nil {
				into[v.Doc] = ""
				for _, sp := range v.Specs {
					if ts, ok := sp.(*ast.TypeSpec); ok {
						into[v.Doc] = ts.Name.Name
						break
					}
					if vs, ok := sp.(*ast.ValueSpec); ok {
						into[v.Doc] = name(vs.Names)
						break
					}
				}
			}
		case *ast.FuncDecl:
			if v.Doc != nil {
				into[v.Doc] = v.Name.Name
			}
		case *ast.ValueSpec:
			if v.Doc != nil {
				into[v.Doc] = name(v.Names)
			}
		case *ast.TypeSpec:
			if v.Doc != nil {
				into[v.Doc] = v.Name.Name
			}
		case *ast.ImportSpec:
			if v.Doc != nil {
				into[v.Doc] = ""
			}
		case *ast.Field:
			if v.Doc != nil {
				into[v.Doc] = name(v.Names)
			}
		}
		return true
	})
}

// isExempt reports mechanical directives and single-line actionable TODOs.
func isExempt(g *ast.CommentGroup) bool {
	for _, c := range g.List {
		t := strings.TrimSpace(c.Text)
		for _, pre := range directivePrefixes {
			if strings.HasPrefix(t, pre) {
				return true
			}
		}
	}
	if len(g.List) == 1 && todoLine.MatchString(strings.TrimPrefix(g.List[0].Text, "//")) {
		return true
	}
	return false
}

// checkDocGroup applies content rules to one documentation comment.
// externalCoord catches references that only make sense inside a change artifact:
// a design-line pointer, a phase/step coordinate, or a red-green narrative. It is
// merged with the change-name axis (externalCoordRef) so one rule owns the whole
// "external document coordinate" idea instead of two overlapping word lists.
var externalCoord = regexp.MustCompile(`(?i)(design line\s*\d+|阶段\s*\d|验收标准\s*\d|\d+\.\d+[①②③④]|Red→green|Fail-before|before the fix)`)

// changeNamesOnce caches the enumeration of change names (active and archived).
var changeNamesOnce sync.Once

// changeNames lists every change name this repo has carried. Archived entries are
// stored date-prefixed, so the prefix is stripped: the comment audience cites the
// name, not the directory.
var changeNames []string

// loadChangeNames 从 openspec 目录枚举变更名（命令由仓根运行；测试从包目录运行，
// 故向上查找）。
func loadChangeNames() {
	names := map[string]bool{}
	root := "."
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(root, "openspec", "changes")); err == nil {
			break
		}
		root = filepath.Join(root, "..")
	}
	add := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			n := e.Name()
			if len(n) > 11 && n[4] == '-' && n[7] == '-' { // 2006-01-02-<name>
				n = n[11:]
			}
			// A change name is a kebab identifier; require a dash and enough length
			// so ordinary prose never trips this check.
			if len(n) >= 8 && strings.Contains(n, "-") {
				names[n] = true
			}
		}
	}
	add(filepath.Join(root, "openspec", "changes"))
	add(filepath.Join(root, "openspec", "changes", "archive"))
	for n := range names {
		changeNames = append(changeNames, n)
	}
	sort.Strings(changeNames)
}

// nonIndexProse drops index lines: they carry paths by design, so a change name or
// a docs path inside an index line is the sanctioned form, not a citation.
func nonIndexProse(text string) string {
	var kept []string
	for _, l := range strings.Split(text, "\n") {
		if indexLine.MatchString(strings.TrimSpace(l)) {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}

func externalCoordRef(prose string) string {
	changeNamesOnce.Do(loadChangeNames)
	if m := externalCoord.FindString(prose); m != "" {
		return m
	}
	for _, n := range changeNames {
		if strings.Contains(prose, n) {
			return n
		}
	}
	return ""
}

func checkDocGroup(path string, fset *token.FileSet, g *ast.CommentGroup, text string, isTest bool, declName string) []finding {
	var out []finding
	line := fset.Position(g.Pos()).Line
	add := func(rule, note string) {
		out = append(out, finding{Path: path, Line: line, Rule: rule, Note: note, Text: firstLine(text)})
	}
	// A Go identifier may itself contain a residue word (a test named …Legacy…).
	// Quoting that declared name at the head of its own doc comment is not change
	// narration, so the residue rules see the prose with the name masked out.
	prose := text
	if declName != "" && declName != "_" {
		prose = strings.ReplaceAll(prose, declName, "____")
	}
	switch {
	case auditMarker.MatchString(prose):
		add("audit-marker", fmt.Sprintf("doc comment carries change/iteration residue: %q", auditMarker.FindString(prose)))
	case rationale.MatchString(prose):
		add("rationale", "doc comment carries design reasoning; move it to docs/wiki or specs")
	}
	if mechanismStep.MatchString(prose) && !isTest {
		add("mechanism-narrative", "doc comment narrates implementation steps the code already shows")
	}
	if changeArtifactRef.MatchString(text) {
		add("process-artifact-ref", "documentation index may not point at change artifacts")
	}
	if coord := externalCoordRef(nonIndexProse(text)); coord != "" {
		add("external-coord-ref", fmt.Sprintf("comment cites a planning coordinate or a change name: %q", coord))
	}
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if indexLine.MatchString(trimmed) {
			target := strings.TrimSpace(trimmed[strings.Index(trimmed, ":")+1:])
			if !hasAllowedRoot(target) {
				out = append(out, finding{Path: path, Line: fset.Position(g.Pos()).Line, Rule: "index-root", Note: "index target must live under docs/ or openspec/specs/: " + target, Text: trimmed})
			} else if _, err := os.Stat(strings.SplitN(target, "#", 2)[0]); err != nil {
				out = append(out, finding{Path: path, Line: fset.Position(g.Pos()).Line, Rule: "index-target-missing", Note: "index target does not exist: " + target, Text: trimmed})
			} else if rule := indexAnchor(target); rule != "" {
				note := "anchor resolves to no section in the target document"
				if rule == "index-anchor-required" {
					note = "target document is long; the index must name the section (#anchor)"
				}
				out = append(out, finding{Path: path, Line: fset.Position(g.Pos()).Line, Rule: rule, Note: note + ": " + target, Text: trimmed})
			}
			continue
		}
		if m := docPathRef.FindString(raw); m != "" {
			out = append(out, finding{Path: path, Line: fset.Position(g.Pos()).Line, Rule: "unindexed-path-ref", Note: "documentation path must be cited with the index form (契约:/规格:)", Text: trimmed})
		}
	}
	return out
}

// hasAllowedRoot reports an index target pointing into the long-lived docs.
func hasAllowedRoot(target string) bool {
	clean := strings.TrimPrefix(strings.SplitN(target, "#", 2)[0], "./")
	for _, root := range indexTargetRoots {
		if strings.HasPrefix(clean, root) {
			return true
		}
	}
	return false
}

// checkCoverage enforces package documentation, exported-symbol documentation and
// the responsibility index a test file must declare.
// dropRedundantPackageDocs removes missing-package-doc findings from files in a
// package that does have a package doc somewhere: the package comment belongs to the
// package, so requiring it in every file would force duplicate package docs (which the
// go tool itself rejects) and produce near-duplicate findings per file.
func dropRedundantPackageDocs(all []finding) []finding {
	pkgsWithDoc := map[string]bool{}
	// Parse the directories involved once; a package doc marks the whole package covered.
	dirs := map[string]bool{}
	for _, f := range all {
		if f.Rule == "missing-package-doc" {
			dirs[filepath.Dir(f.Path)] = true
		}
	}
	for dir := range dirs {
		files, err := collectGoFiles(dir)
		if err != nil {
			continue
		}
		for _, file := range files {
			fset := token.NewFileSet()
			parsed, perr := parser.ParseFile(fset, file, nil, parser.PackageClauseOnly|parser.ParseComments)
			if perr == nil && parsed.Doc != nil && strings.TrimSpace(parsed.Doc.Text()) != "" {
				pkgsWithDoc[dir] = true
				break
			}
		}
	}
	kept := make([]finding, 0, len(all))
	for _, f := range all {
		if f.Rule == "missing-package-doc" && pkgsWithDoc[filepath.Dir(f.Path)] {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

func checkCoverage(fset *token.FileSet, file *ast.File, path string, isTest bool) []finding {
	var out []finding
	if file.Doc == nil || strings.TrimSpace(file.Doc.Text()) == "" {
		out = append(out, finding{Path: path, Line: fset.Position(file.Package).Line, Rule: "missing-package-doc", Note: "package has no doc comment", Text: "package " + file.Name.Name})
	}
	if isTest {
		has := false
		for _, g := range file.Comments {
			if strings.TrimSpace(indexLineText(g)) != "" {
				has = true
			}
		}
		if !has {
			out = append(out, finding{Path: path, Line: fset.Position(file.Package).Line, Rule: "missing-test-responsibility", Note: "test file must declare one 契约: index naming its responsibility", Text: "package " + file.Name.Name})
		}
		return out
	}
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || !exportedName.MatchString(fd.Name.Name) {
			continue
		}
		label := fd.Name.Name
		if fd.Recv != nil {
			recv := recvName(fd)
			if !exportedName.MatchString(recv) {
				// A method on an unexported type is not part of the rendered API
				// surface, so it carries no public documentation obligation.
				continue
			}
			label = recv + "." + fd.Name.Name
		}
		if fd.Doc == nil {
			out = append(out, finding{Path: path, Line: fset.Position(fd.Pos()).Line, Rule: "missing-symbol-doc", Note: "exported function has no doc comment", Text: label})
		}
	}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			name := ""
			switch s := sp.(type) {
			case *ast.TypeSpec:
				if s.Doc == nil && gd.Doc == nil && exportedName.MatchString(s.Name.Name) {
					name = s.Name.Name
				}
			case *ast.ValueSpec:
				if s.Doc == nil && gd.Doc == nil {
					for _, id := range s.Names {
						if exportedName.MatchString(id.Name) {
							name = id.Name
						}
					}
				}
			}
			if name != "" {
				out = append(out, finding{Path: path, Line: fset.Position(sp.Pos()).Line, Rule: "missing-symbol-doc", Note: "exported declaration has no doc comment", Text: name})
			}
		}
	}
	return out
}

// anchorRequiredAbove is the document size beyond which a bare file path is not
// an actionable index: the reader cannot find the passage, so the index must
// name the section.
const anchorRequiredAbove = 200

// indexAnchor checks that an index either carries no anchor or carries one that
// resolves (explicit <a id="x"> or a heading containing the anchor text), and
// that a large target document is referenced with an anchor at all.
func indexAnchor(target string) string {
	file, frag := target, ""
	if i := strings.Index(target, "#"); i >= 0 {
		file, frag = target[:i], target[i+1:]
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(raw), "\n")
	if frag == "" {
		if len(lines) > anchorRequiredAbove {
			return "index-anchor-required"
		}
		return ""
	}
	slug := strings.ToLower(strings.ReplaceAll(frag, "-", " "))
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.Contains(t, `<a id="`+frag+`"`) || strings.Contains(t, `<a id='`+frag+`'`) {
			return ""
		}
		if strings.HasPrefix(t, "#") && strings.Contains(strings.ToLower(t), slug) {
			return ""
		}
	}
	return "index-anchor-unknown"
}

// indexLineText returns the group's index line when it has one.
func indexLineText(g *ast.CommentGroup) string {
	for _, l := range strings.Split(strings.TrimSpace(g.Text()), "\n") {
		if indexLine.MatchString(strings.TrimSpace(l)) {
			return strings.TrimSpace(l)
		}
	}
	return ""
}

// firstLine shortens a finding's text for display.
func firstLine(text string) string {
	l := strings.SplitN(strings.TrimSpace(text), "\n", 2)[0]
	if len(l) > 120 {
		l = l[:120] + "…"
	}
	return l
}

// recvName renders a receiver's base type name for a finding label.
func recvName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	t := fd.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}

// compareRatchet counts findings above the recorded level and slots that have
// fallen below it. A slot absent from the baseline counts from zero, so a new
// file or a new rule cannot slip in unrecorded.
func compareRatchet(counts, base map[string]int) (regressions, improvements int) {
	for k, n := range counts {
		if n > base[k] {
			regressions += n - base[k]
		}
	}
	for k, want := range base {
		if counts[k] < want {
			improvements++
		}
	}
	return regressions, improvements
}

// docContentLines returns a doc group's substantive lines: blank separators,
// index lines (契约:/规格:) and directives are not documentation prose.
// testDocMaxLineRunes caps how much a single test-doc line may carry, so the
// one-intent-line norm cannot be satisfied by cramming a paragraph into one line.
const testDocMaxLineRunes = 160

func docContentLines(g *ast.CommentGroup) []string {
	if g == nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(g.Text(), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || indexLine.MatchString(t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// checkDocForm enforces the two shape rules a filled doc slot must still obey:
// a declaration doc starts with the identifier it documents, and a test doc
// stays a single intent line (argument and pitfall narration belong in
// assertion messages or in the wiki). Without these, mechanically moving a
// trailing comment above a field would "pass" while teaching nothing.
func checkDocForm(fset *token.FileSet, file *ast.File, path string, isTest bool) []finding {
	var out []finding
	name := func(pos token.Pos, ident, rule, note string) {
		out = append(out, finding{Path: path, Line: fset.Position(pos).Line, Rule: rule, Note: note, Text: ident})
	}
	must := func(pos token.Pos, ident string, g *ast.CommentGroup) {
		if g == nil || ident == "" || ident == "_" {
			return
		}
		lines := docContentLines(g)
		if len(lines) > 0 && !strings.HasPrefix(lines[0], ident) {
			name(pos, ident, "doc-not-name-prefixed", "doc comment must start with the documented identifier")
		}
	}
	for _, d := range file.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			must(decl.Pos(), decl.Name.Name, decl.Doc)
			if isTest && strings.HasPrefix(decl.Name.Name, "Test") {
				// Shape norm (D-26): one intent line, optionally wrapped as a bullet
				// list of parallel points, plus index lines. Prose continuation means
				// the argument belongs in an assertion message or in docs/wiki; the
				// length cap exists so "one line" never turns into a 200-rune wall.
				lines := docContentLines(decl.Doc)
				for i, l := range lines {
					if i > 0 && !strings.HasPrefix(l, "- ") {
						name(decl.Pos(), decl.Name.Name, "test-doc-not-one-sentence",
							"test doc must be one intent line, an optional bullet list, and the index; move prose into the assertion message or the wiki")
						break
					}
				}
				for _, l := range lines {
					if utf8.RuneCountInString(l) > testDocMaxLineRunes {
						name(decl.Pos(), decl.Name.Name, "test-doc-line-too-long",
							"test doc line exceeds the shape limit; split the intent into a bullet list or move the detail into docs/wiki")
						break
					}
				}
			}
		case *ast.GenDecl:
			var groupNames []string
			for _, sp := range decl.Specs {
				switch spec := sp.(type) {
				case *ast.TypeSpec:
					groupNames = append(groupNames, spec.Name.Name)
					must(spec.Pos(), spec.Name.Name, spec.Doc)
				case *ast.ValueSpec:
					for _, id := range spec.Names {
						groupNames = append(groupNames, id.Name)
					}
					if len(spec.Names) == 1 {
						must(spec.Pos(), spec.Names[0].Name, spec.Doc)
					}
				}
			}
			if decl.Doc != nil && len(groupNames) > 0 {
				lines := docContentLines(decl.Doc)
				if len(lines) > 0 && !startsWithAny(lines[0], groupNames) {
					name(decl.Pos(), strings.Join(groupNames, ","), "doc-not-name-prefixed",
						"doc comment must start with the documented identifier")
				}
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.StructType:
			if t.Fields != nil {
				for _, f := range t.Fields.List {
					must(f.Pos(), fieldLabel(f), f.Doc)
				}
			}
		case *ast.InterfaceType:
			if t.Methods != nil {
				for _, f := range t.Methods.List {
					must(f.Pos(), fieldLabel(f), f.Doc)
				}
			}
		}
		return true
	})
	return out
}

// fieldLabel is the identifier a field doc must name: the single field name, or
// "" for an embedded/anonymous field which carries no such obligation.
func fieldLabel(f *ast.Field) string {
	if len(f.Names) == 1 {
		return f.Names[0].Name
	}
	return ""
}

func startsWithAny(line string, names []string) bool {
	for _, n := range names {
		if strings.HasPrefix(line, n) {
			return true
		}
	}
	return false
}
