// check.go implements the two comparison modes the repository gates need:
//
//	codetools comment-check --base-root DIR --head-root DIR <file>...
//	codetools merge-check   --base-root DIR --head-root DIR [--map FILE] <dir>...
//
// Paths are module-relative and must exist under both roots (comment-check) or in
// the head root (merge-check); the shell wrappers materialize the baseline with
// git archive. Both exit non-zero when a violation is printed.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// runCommentCheck reports files whose comment-stripped code differs between the
// two roots, which is the witness that a batch changed documentation only.
// Layout is not code: dropping a comment can merge an alignment group and change nothing
// but spacing, so the comparison is over the token stream (see foldLayout).
func runCommentCheck(args []string) int {
	fs := flag.NewFlagSet("comment-check", flag.ExitOnError)
	base := fs.String("base-root", "", "root holding the baseline revision")
	head := fs.String("head-root", "", "root holding the working tree")
	_ = fs.Parse(args)
	if *base == "" || *head == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: codetools comment-check --base-root DIR --head-root DIR file...")
		return 2
	}
	var fail int
	for _, rel := range fs.Args() {
		b, err := readRel(*base, rel)
		if err != nil {
			fmt.Printf("MISSING-BASE %s: %v\n", rel, err)
			fail++
			continue
		}
		h, err := readRel(*head, rel)
		if err != nil {
			fmt.Printf("MISSING-HEAD %s: %v\n", rel, err)
			fail++
			continue
		}
		sb, err := stripText(rel, b)
		if err != nil {
			fmt.Printf("UNPARSABLE-BASE %s: %v\n", rel, err)
			fail++
			continue
		}
		sh, err := stripText(rel, h)
		if err != nil {
			fmt.Printf("UNPARSABLE-HEAD %s: %v\n", rel, err)
			fail++
			continue
		}
		if foldLayout(sb) != foldLayout(sh) {
			fmt.Printf("CODE-CHANGED %s\n", rel)
			fail++
		}
	}
	if fail > 0 {
		fmt.Printf("comment-check: %d violation(s)\n", fail)
		return 1
	}
	fmt.Printf("comment-check: %d file(s), code identical under comment strip\n", len(fs.Args()))
	return 0
}

// stripText parses src as a file and returns its comment-free rendering.
func stripText(name, src string) (string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return "", err
	}
	return printNoComments(fset, file)
}

type mergeViolation struct {
	Pkg  string `json:"pkg"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Note string `json:"note"`
}

// runMergeCheck verifies a test-file consolidation kept every test entry intact:
// same set of test/benchmark functions per package (after applying the rename
// map), each with an identical comment-free body, identical assertion count and
// identical t.Parallel count. Non-test declarations that moved or changed are
// reported separately so the batch ledger must account for each one.
func runMergeCheck(args []string) int {
	fs := flag.NewFlagSet("merge-check", flag.ExitOnError)
	base := fs.String("base-root", "", "root holding the baseline revision")
	head := fs.String("head-root", "", "root holding the working tree")
	mapFile := fs.String("map", "", "TSV of old-name<TAB>new-name renames")
	explain := fs.String("explain", "", "file listing non-test declarations allowed to differ")
	diffOut := fs.String("diff-out", "", "directory to write normalized base/head text for each body-changed declaration")
	_ = fs.Parse(args)
	if *base == "" || *head == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: codetools merge-check --base-root DIR --head-root DIR [--map F] [--explain F] dir...")
		return 2
	}
	renames := loadRenames(*mapFile)
	explained := loadSet(*explain)

	var out []mergeViolation
	for _, dir := range fs.Args() {
		b, err := packageDecls(filepath.Join(*base, dir), dir, renames)
		if err != nil {
			out = append(out, mergeViolation{Pkg: dir, Kind: "read", Name: "-", Note: err.Error()})
			continue
		}
		h, err := packageDecls(filepath.Join(*head, dir), dir, nil)
		if err != nil {
			out = append(out, mergeViolation{Pkg: dir, Kind: "read", Name: "-", Note: err.Error()})
			continue
		}
		vs := diffDecls(dir, b, h, renames, explained)
		if *diffOut != "" {
			if err := writeDiffs(*diffOut, dir, b, h, renames, vs); err != nil {
				fmt.Fprintln(os.Stderr, "diff-out:", err)
				return 2
			}
		}
		out = append(out, vs...)
	}
	if len(out) > 0 {
		enc := json.NewEncoder(os.Stdout)
		for _, v := range out {
			_ = enc.Encode(v)
		}
		fmt.Printf("merge-check: %d violation(s)\n", len(out))
		return 1
	}
	fmt.Printf("merge-check: %d package(s) intact\n", fs.NArg())
	return 0
}

// writeDiffs dumps the normalized text of both sides for every body-changed
// declaration, so an explain entry can be justified by reading the difference
// instead of guessing from a hash mismatch.
func writeDiffs(dir, pkg string, b, h map[string]decl, renames map[string]string, vs []mergeViolation) error {
	if err := os.MkdirAll(filepath.Join(dir, pkg), 0o755); err != nil {
		return err
	}
	for _, v := range vs {
		if v.Kind != "body-changed" {
			continue
		}
		bd, ok := b[v.Name]
		if !ok {
			continue
		}
		hn := applyRenames(v.Name, renames)
		hd, ok := h[hn]
		if !ok {
			for name, cand := range h {
				if cand.Name == bd.Name {
					hd, ok = cand, true
					hn = name
					break
				}
			}
		}
		if !ok {
			continue
		}
		base := filepath.Join(dir, pkg, sanitize(v.Name))
		if err := os.WriteFile(base+".base.txt", []byte(bd.norm), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(base+".head.txt", []byte(hd.norm), 0o600); err != nil {
			return err
		}
		_ = hn
	}
	return nil
}

func sanitize(name string) string {
	repl := regexp.MustCompile(`[^A-Za-z0-9_.-]`)
	return repl.ReplaceAllString(name, "_")
}

// diffDecls compares two packages' declaration sets under the rename map.
// A declared rename must not stop a function from being a test — losing the Test prefix is
// coverage silently turned into dead code — and assertion counts are monotone like the
// policy ratchet: they may only grow, so a merge cannot weaken coverage while claiming to
// be a move.
func diffDecls(dir string, b, h map[string]decl, renames map[string]string, explained map[string]bool) []mergeViolation {
	var out []mergeViolation
	matched := map[string]bool{}
	for oldName, bd := range b {
		name := applyRenames(oldName, renames)
		hd, ok := h[name]
		if !ok {
			if bd.Test {
				out = append(out, mergeViolation{Pkg: dir, Kind: "missing-test", Name: oldName, Note: "no counterpart after merge/rename"})
			} else if !explained[oldName] && !explained[name] {
				out = append(out, mergeViolation{Pkg: dir, Kind: "missing-helper", Name: oldName, Note: "not in explain list"})
			}
			continue
		}
		matched[name] = true
		if bd.Test && !hd.Test {
			out = append(out, mergeViolation{Pkg: dir, Kind: "test-name-mangled", Name: oldName,
				Note: fmt.Sprintf("renamed to %q, which is no longer a test function", name)})
		}
		if !bd.Test {
			if explained[oldName] || explained[name] {
				continue
			}
		}
		switch {
		case bd.Hash != hd.Hash:
			out = append(out, mergeViolation{Pkg: dir, Kind: "body-changed", Name: oldName, Note: "comment-free body differs"})
		case hd.Asserts < bd.Asserts:
			out = append(out, mergeViolation{Pkg: dir, Kind: "assert-count", Name: oldName, Note: fmt.Sprintf("base %d head %d", bd.Asserts, hd.Asserts)})
		case bd.Parallel != hd.Parallel:
			out = append(out, mergeViolation{Pkg: dir, Kind: "parallel-count", Name: oldName, Note: fmt.Sprintf("base %d head %d", bd.Parallel, hd.Parallel)})
		}
	}
	for name, hd := range h {
		if matched[name] {
			continue
		}
		if _, isRenamed := renames[name]; isRenamed {
			continue
		}
		if hd.Test {
			out = append(out, mergeViolation{Pkg: dir, Kind: "extra-test", Name: name, Note: "present only in head"})
		} else if !explained[name] {
			out = append(out, mergeViolation{Pkg: dir, Kind: "extra-helper", Name: name, Note: "not in explain list"})
		}
	}
	return out
}

// foldLayout collapses every run of whitespace to a single space.
func foldLayout(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// qualifierBlind replaces every package qualifier prefix with a fixed marker.
//
// A consolidation batch may unify one import path's alias per merged file, which is
// a per-FILE decision; a package-wide textual map cannot express it and instead
// rewrites the same spelling two different ways, manufacturing false differences.
// Blinding the qualifier keeps every selector name (and therefore the semantics of
// each call) comparable while making alias choice irrelevant to the witness.
func qualifierBlind(text string) string {
	return qualifierRE.ReplaceAllString(text, "Q.")
}

var qualifierRE = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.`)

// applyRenames rewrites declared old identifiers to their new names in a key or a
// rendered body, so a rename batch is compared modulo the renames it declares.
// Only exact identifier occurrences change; anything else still has to match.
func applyRenames(text string, renames map[string]string) string {
	if len(renames) == 0 {
		return text
	}
	var b strings.Builder
	for _, seg := range splitOutsideLiterals(text) {
		if seg.literal {
			b.WriteString(seg.text)
			continue
		}
		for old, nw := range renames {
			seg.text = renameRE(old).ReplaceAllString(seg.text, nw)
		}
		b.WriteString(seg.text)
	}
	return b.String()
}

type textSegment struct {
	text    string
	literal bool
}

// splitOutsideLiterals cuts text into literal and non-literal runs so that an
// identifier rename never rewrites the content of a string or rune literal. A
// textual rename that reaches into fixtures manufactures differences no rename
// declared, which the gate would then have to be talked out of.
func splitOutsideLiterals(text string) []textSegment {
	var segs []textSegment
	var cur strings.Builder
	flush := func(lit bool) {
		if cur.Len() > 0 {
			segs = append(segs, textSegment{text: cur.String(), literal: lit})
			cur.Reset()
		}
	}
	for i := 0; i < len(text); {
		c := text[i]
		switch c {
		case '`':
			flush(false)
			j := strings.IndexByte(text[i+1:], '`')
			if j < 0 {
				cur.WriteString(text[i:])
				i = len(text)
			} else {
				segs = append(segs, textSegment{text: text[i : i+j+2], literal: true})
				i += j + 2
			}
		case '"', '\'':
			flush(false)
			start := i
			i++
			for i < len(text) {
				if text[i] == '\\' && i+1 < len(text) {
					i += 2
					continue
				}
				if text[i] == c {
					i++
					break
				}
				i++
			}
			segs = append(segs, textSegment{text: text[start:i], literal: true})
		default:
			cur.WriteByte(c)
			i++
		}
	}
	flush(false)
	return segs
}

// renameRE matches a whole identifier.
func renameRE(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
}

// packageDecls collects every top-level declaration from the test files found in
// root, keyed by declaration name (with file as tie-breaker suffix).
// Blank identifiers are skipped: they carry no identity, so keying on them would turn a
// file move into a spurious missing/extra pair. An empty raw never overwrites a real hash,
// which would silently disable the body check.
func packageDecls(root, dir string, renames map[string]string) (map[string]decl, error) {
	fis, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]decl{}, nil
		}
		return nil, err
	}
	out := map[string]decl{}
	for _, fi := range fis {
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), "_test.go") {
			continue
		}
		ds, err := runDeclsFor(filepath.Join(root, fi.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fi.Name(), err)
		}
		for _, d := range ds {
			if d.Name == "_" {
				continue
			}
			if d.raw != "" {
				d.norm = qualifierBlind(applyRenames(d.raw, renames))
				d.Hash = hashString(foldLayout(d.norm))
			}
			key := d.Name
			if _, dup := out[key]; dup {
				key = key + "@" + d.File
			}
			out[key] = d
		}
	}
	return out, nil
}

// runDeclsFor parses one path on disk and returns its declarations.
func runDeclsFor(path string) ([]decl, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Base(path), src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	file = declView(file)
	base := filepath.Base(path)
	out := make([]decl, 0, len(file.Decls))
	for _, d := range file.Decls {
		names, kind, node := classify(d)
		body, err := render(fset, hashView(d))
		if err != nil {
			return nil, err
		}
		for _, nm := range names {
			out = append(out, decl{
				File: base, Name: nm, Kind: kind, Test: isTestName(nm),
				Hash:    hashString(dropBlankLines(body)),
				raw:     dropBlankLines(body),
				Asserts: countAsserts(node), Parallel: countParallel(node),
			})
		}
	}
	return out, nil
}

// loadRenames reads an optional old<TAB>new TSV.
func loadRenames(path string) map[string]string {
	out := map[string]string{}
	for _, l := range loadLines(path) {
		f := strings.Split(l, "\t")
		if len(f) >= 2 && f[0] != "" && !strings.HasPrefix(f[0], "#") {
			out[f[0]] = f[1]
		}
	}
	return out
}

// loadSet reads an optional list file into a set.
func loadSet(path string) map[string]bool {
	out := map[string]bool{}
	for _, l := range loadLines(path) {
		l = strings.TrimSpace(strings.Split(l, "#")[0])
		if l != "" {
			out[l] = true
		}
	}
	return out
}

func loadLines(path string) []string {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: cannot read %s: %v\n", path, err)
		return nil
	}
	defer f.Close()
	var out []string
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<16), 1<<20)
	for s.Scan() {
		out = append(out, s.Text())
	}
	return out
}

// readRel loads a module-relative path from root.
func readRel(root, rel string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// hashString renders a declaration's comment-free form into a stable digest.
func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// hashView returns the form of a declaration that the merge gate compares: a
// function's receiver, signature and body WITHOUT its declared name, so a rename
// recorded in the map still compares equal on everything that must not change.
// Other declarations are compared as written, which makes a helper rename visible
// and therefore subject to the explain list.
func hashView(d ast.Decl) ast.Node {
	fd, ok := d.(*ast.FuncDecl)
	if !ok {
		return d
	}
	masked := *fd
	masked.Name = ast.NewIdent("_")
	return &masked
}

// runMapLint checks a rename table against the package's baseline. A row is legitimate
// when its name is a declaration or merely appears in the baseline text — renaming a local
// identifier inside a body is equally valid. A name present nowhere is a stray or foreign
// row, and one equal to an imported package name is an alias: applying either rewrites
// arbitrary text and manufactures violations. This is the guard that would have caught the
// agent alias rows leaking into the root table.
func runMapLint(args []string) int {
	fs := flag.NewFlagSet("map-lint", flag.ExitOnError)
	base := fs.String("base-root", "", "root holding the baseline revision")
	pkg := fs.String("pkg", ".", "package directory the table belongs to")
	_ = fs.Parse(args)
	if *base == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: codetools map-lint --base-root DIR --pkg DIR table.tsv...")
		return 2
	}
	bad := 0
	for _, table := range fs.Args() {
		renames := loadRenames(table)
		dir := *pkg
		decls, err := packageDecls(filepath.Join(*base, dir), dir, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", table, err)
			return 2
		}
		decls_ := map[string]bool{}
		for _, d := range decls {
			decls_[d.Name] = true
		}
		blob, imports := packageText(filepath.Join(*base, dir))
		var stray, alias []string
		for old := range renames {
			switch {
			case !decls_[old] && !wordPresent(blob, old):
				stray = append(stray, old)
			case imports[old]:
				alias = append(alias, old)
			}
		}
		sort.Strings(stray)
		sort.Strings(alias)
		if len(stray) > 0 || len(alias) > 0 {
			bad = 1
			for _, n := range stray {
				fmt.Printf("STRAY   %s: %s (name appears nowhere in %s)\n", filepath.Base(table), n, dir)
			}
			for _, n := range alias {
				fmt.Printf("ALIAS?  %s: %s is an imported package name in %s — a rename row should not be an import alias\n", filepath.Base(table), n, dir)
			}
		} else {
			fmt.Printf("%s: all %d rows are declared or in-body names in %q\n", filepath.Base(table), len(renames), dir)
		}
	}
	if bad != 0 {
		return 1
	}
	return 0
}

// packageText returns the concatenated baseline test sources of a package and the
// set of imported package base names it declares.
func packageText(root string) (string, map[string]bool) {
	var b strings.Builder
	imports := map[string]bool{}
	fis, err := os.ReadDir(root)
	if err != nil {
		return "", imports
	}
	for _, fi := range fis {
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, fi.Name()))
		if err != nil {
			continue
		}
		b.Write(src)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, fi.Name(), src, parser.ImportsOnly)
		if perr != nil {
			continue
		}
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			base := p[strings.LastIndex(p, "/")+1:]
			if im.Name != nil {
				base = im.Name.Name
			}
			imports[base] = true
		}
	}
	return b.String(), imports
}

func wordPresent(blob, name string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(blob)
}

var (
	// segPattern matches a leading capital-plus-digits label inside a segment.
	segPattern = regexp.MustCompile(`^[A-Z]\d+`)
	// knownWords are domain vocabulary that legitimately carries digits: integer
	// widths, tier/version labels, hashes, HTTP codes, soak and percentage names.
	knownWords = regexp.MustCompile(`(?i)(Int(8|16|32|64)|Uint(8|16|32|64)|Float(32|64)|I64|L\d|V\d|MD5|SHA\d|HTTP\d|401|403|404|500|Round\d|Percent\d|Sub\d|G\d|Gen\d|P99)`)
)

// hasBatchLabel reports whether a test identifier carries a batch/round label:
// some segment starts with a capital letter plus digits and the digits end that
// segment (next char is a capital or the segment ends right after them).
func hasBatchLabel(name string) bool {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(name, "Test"), "Benchmark")
	for _, seg := range strings.Split(trimmed, "_") {
		for seg != "" {
			m := segPattern.FindString(seg)
			if m == "" {
				break
			}
			rest := seg[len(m):]
			if rest == "" || (rest[0] >= 'A' && rest[0] <= 'Z') {
				return true
			}
			seg = rest
		}
	}
	return false
}

// nameTestViolation reports a numbered identifier, applying the vocabulary
// whitelist so domain names are not flagged.
func nameTestViolation(name string) bool {
	if knownWords.MatchString(name) {
		return false
	}
	return hasBatchLabel(name)
}

// nameViolation records one identifier that carries a batch label.
type nameViolation struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// nameViolations scans Go files under root for numbered test and package-level
// identifiers.
func nameViolations(root string) []nameViolation {
	var out []nameViolation
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fd.Name.Name, "Test") {
				continue
			}
			if nameTestViolation(fd.Name.Name) {
				out = append(out, nameViolation{Path: path, Name: fd.Name.Name})
			}
		}
		return nil
	})
	return out
}

// runNameCheck reports numbered test identifiers under the given dirs.
func runNameCheck(args []string) int {
	if len(args) == 0 {
		args = []string{"."}
	}
	code := 0
	for _, dir := range args {
		for _, v := range nameViolations(dir) {
			fmt.Printf("ITERATION-NUMBER-NAME %s: %s\n", v.Path, v.Name)
			code = 1
		}
	}
	if code == 0 {
		fmt.Println("name-check: no iteration-numbered test identifiers")
	}
	return code
}

var (
	docRefPattern = regexp.MustCompile("`([A-Za-z0-9_./{}*+-]+\\.(?:go|md|sh|json|ya?ml|toml|py))`")
	docRefSkip    = regexp.MustCompile(`[{}*]|https?://|^\./\$|TODO`)
	// knownTopDirs scopes the check to citations that claim this repository.
	knownTopDirs = map[string]bool{
		"agent": true, "memory": true, "event": true, "plugin": true, "prompt": true,
		"tool": true, "rl": true, "evolution": true, "docs": true, "openspec": true,
		"examples": true, "scripts": true, "internal": true, "testutil": true,
		"train": true, "evals": true, "resources": true,
	}
)

// docDanglingRefs returns path-like citations in doc that resolve to nothing. A renamed or
// merged-away file leaves a citation pointing at nothing and no comment-level gate can see
// that, so markdown is scanned for it. Only path-like tokens count (backticked, with a
// separator and a known extension), and only when the first segment is a top-level dir of
// this repository — otherwise upstream paths, placeholders and bare filenames (which name a
// convention rather than one file) would be reported as drift they never were.
func docDanglingRefs(doc, root string) []string {
	raw, err := os.ReadFile(doc)
	if err != nil {
		return nil
	}
	rel := strings.TrimPrefix(filepath.Dir(doc), root+"/")
	seen := map[string]bool{}
	var out []string
	for _, m := range docRefPattern.FindAllStringSubmatch(string(raw), -1) {
		tok := m[1]
		if docRefSkip.MatchString(tok) {
			continue
		}
		if !strings.Contains(tok, "/") {
			continue
		}
		head := tok[:strings.Index(tok, "/")]
		head = strings.TrimPrefix(head, "./")
		if !knownTopDirs[head] {
			continue
		}
		cands := []string{filepath.Join(root, tok), filepath.Join(root, rel, tok),
			filepath.Join(root, strings.TrimPrefix(tok, "./")),
			filepath.Join(root, filepath.Base(filepath.Dir(doc)), tok)}
		found := false
		for _, c := range cands {
			if _, err := os.Stat(c); err == nil {
				found = true
				break
			}
		}
		if !found && !seen[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
	}
	sort.Strings(out)
	return out
}

// runDocRefs scans markdown under the given dirs for dangling citations.
func runDocRefs(args []string) int {
	if len(args) == 0 {
		args = []string{"docs"}
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	code := 0
	for _, dir := range args {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			for _, tok := range docDanglingRefs(path, root) {
				fmt.Printf("DANGLING-DOC-REF %s: %s\n", path, tok)
				code = 1
			}
			return nil
		})
	}
	if code == 0 {
		fmt.Println("doc-refs: no dangling file citations")
	}
	return code
}

// procRefSkip names the files that *implement* these checks. A definition or a
// test fixture that spells out the path pattern is not a citation, so the gate
// does not report itself; everything else must state the contract in a long-lived
// document instead of pointing at a process artifact.
var procRefSkip = map[string]bool{
	"scripts/comment_policy/main.go":                true,
	"scripts/comment_policy/main_test.go":           true,
	"scripts/comment_policy/testdata/violations.go": true,
	"scripts/check-openspec.sh":                     true,
	"scripts/hooks/pre-commit":                      true,
	"scripts/codetools/check.go":                    true,
	"scripts/codetools/check_test.go":               true,
	"scripts/lint.sh":                               true,
}

// procRefViolations reports "file:line" of each change-artifact citation under dir.
func procRefViolations(dir string) []string {
	var out []string
	root := filepath.Dir(dir)
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr == nil && procRefSkip[filepath.ToSlash(rel)] {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "openspec/changes/") {
				out = append(out, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), i+1))
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// runProcRefs reports change-artifact citations outside the exempt tooling.
func runProcRefs(args []string) int {
	if len(args) == 0 {
		args = []string{"scripts", ".github/workflows"}
	}
	code := 0
	for _, dir := range args {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || procRefSkip[filepath.ToSlash(path)] {
				return nil
			}
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if j := strings.Index(line, "openspec/changes/"); j >= 0 {
					fmt.Printf("PROCESS-ARTIFACT-CITE %s:%d: %s\n", path, i+1, strings.TrimSpace(line))
					code = 1
				}
			}
			return nil
		})
	}
	if code == 0 {
		fmt.Println("proc-refs: no change-artifact citations in scripts or CI")
	}
	return code
}
