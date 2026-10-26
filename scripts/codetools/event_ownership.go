// 本文件实现事件归属门（event-ownership）：按 golden/runtime/architecture.json 的
// event_ownership 冻结表核对 owner 的可解析直接调用点。通知身份只由 runtime/task
// 生产，邮箱时点只由 runtime/state 实现、由 facts 随动消费者在一处记账，宿主只做路由
// 登记与投递受理，不复制第二份消费状态。
//
// 判定只认 go/types 解析出的对象身份（声明包 + 接收者类型 + 方法名）；同一方法名的
// 其它接收者按签名区分，不做名字模糊匹配。方法值回调、按名反射与解析不到的类型信息
// 登记为 uncovered 并如实输出，绝不折算成已覆盖；冻结符号在树里声明不出来是表本身
// 烂了，直接判失败，不静默跳过。
// 规格: docs/OPERATION.md#event-gates
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const eventOwnershipUsage = "usage: codetools event-ownership --check [--golden PATH] [--repo-root PATH] [--json]"

// ownershipTable is the frozen slice of architecture.json this gate reads. Unknown
// fields are rejected on load: a typo such as "alowed" would otherwise widen the
// table silently, and a gate whose lists can be misspelled is not frozen.
type ownershipTable struct {
	SchemaVersion int               `json:"schema_version"`
	FrozenBy      string            `json:"frozen_by"`
	Statement     string            `json:"statement"`
	Symbols       []ownershipSymbol `json:"symbols"`
	Owners        []ownershipOwner  `json:"owners"`
}

// ownershipSymbol is one frozen call target: a package-level function or a method
// on one or more declared receiver types (an interface and its implementation can
// both be the front of the same time point).
type ownershipSymbol struct {
	ID        string   `json:"id"`
	Pkg       string   `json:"pkg"`
	Receivers []string `json:"receivers,omitempty"`
	Func      string   `json:"func,omitempty"`
	Method    string   `json:"method,omitempty"`
	Role      string   `json:"role"`
	Reason    string   `json:"reason"`
}

// ownershipOwner is one layer's frozen surface: the packages (and, for the
// composition root, the exact files) it speaks for, plus the symbols it may and
// may not call directly.
type ownershipOwner struct {
	ID        string   `json:"id"`
	Packages  []string `json:"packages"`
	Files     []string `json:"files,omitempty"`
	Allowed   []string `json:"allowed"`
	Forbidden []string `json:"forbidden"`
	Reason    string   `json:"reason"`
}

type ownershipOptions struct {
	Golden   string
	RepoRoot string
	JSON     bool
}

type ownershipCounts struct {
	Owners            int `json:"owners"`
	Symbols           int `json:"symbols"`
	PackagesScanned   int `json:"packages_scanned"`
	Candidates        int `json:"candidates"`
	DirectCalls       int `json:"direct_calls"`
	Allowed           int `json:"allowed"`
	Forbidden         int `json:"forbidden"`
	Unregistered      int `json:"unregistered"`
	Unowned           int `json:"unowned"`
	Ambiguous         int `json:"ambiguous"`
	Uncovered         int `json:"uncovered"`
	SameNameNonTarget int `json:"same_name_non_target"`
}

type ownershipFinding struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Caller string `json:"caller,omitempty"`
	Symbol string `json:"symbol,omitempty"`
	Owner  string `json:"owner,omitempty"`
	Rule   string `json:"rule"`
	Detail string `json:"detail"`
}

type ownershipUncovered struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Caller string `json:"caller"`
	Name   string `json:"name"`
	Symbol string `json:"symbol,omitempty"`
	Reason string `json:"reason"`
}

type ownershipReport struct {
	OK         bool                 `json:"ok"`
	Module     string               `json:"module"`
	Golden     string               `json:"golden"`
	Counts     ownershipCounts      `json:"counts"`
	Violations []ownershipFinding   `json:"violations"`
	Uncovered  []ownershipUncovered `json:"uncovered"`
	NonTarget  []ownershipFinding   `json:"non_target"`
	Notes      []string             `json:"notes"`
}

// listedPackage is the subset of `go list -json` this gate needs; Export is the
// compiled package archive the standard gc importer reads, so type information is
// exact instead of a hand-built approximation.
type listedPackage struct {
	Dir        string
	ImportPath string
	Name       string
	GoFiles    []string
	Export     string
	Module     *struct {
		Path string
	}
}

type candidateKind int

const (
	candidateCall candidateKind = iota
	candidateValue
	candidateReflect
)

// callCandidate is one selector that names a frozen method, before types decide
// what it actually is. File is repo-relative for the report; Base is the exact
// file name owner scopes are declared with.
type callCandidate struct {
	file   string
	base   string
	line   int
	caller string
	pkgRel string
	name   string
	sel    *ast.SelectorExpr
	kind   candidateKind
}

// frozenSymbol is one resolved entry of the table: identity plus the signature
// used to tell a same-name operation apart from an unregistered front.
type frozenSymbol struct {
	id       string
	pkg      string
	receiver string
	name     string
	sig      *types.Signature
}

type symbolUniverse struct {
	byIdentity map[string]*frozenSymbol
	byName     map[string][]*frozenSymbol
}

type ownerScope struct {
	id        string
	packages  []string
	files     []string
	allowed   map[string]bool
	forbidden map[string]bool
}

func parseOwnershipArgs(args []string) (ownershipOptions, error) {
	opts := ownershipOptions{
		Golden:   filepath.Join("golden", "runtime", "architecture.json"),
		RepoRoot: ".",
	}
	flags := flag.NewFlagSet("event-ownership", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	golden := flags.String("golden", opts.Golden, "architecture golden carrying event_ownership")
	repo := flags.String("repo-root", opts.RepoRoot, "module root the call sites are read from")
	check := flags.Bool("check", false, "enforce the gate (required: this subcommand has no report-only mode)")
	asJSON := flags.Bool("json", false, "print the report as one JSON object")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if !*check {
		return opts, errors.New(eventOwnershipUsage)
	}
	opts.Golden, opts.RepoRoot, opts.JSON = *golden, *repo, *asJSON
	return opts, nil
}

func runEventOwnership(args []string) int {
	opts, err := parseOwnershipArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	report, code := executeEventOwnership(opts)
	if opts.JSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, "event-ownership: encode report:", err)
			return 2
		}
		return code
	}
	printOwnershipReport(report)
	return code
}

func printOwnershipReport(report *ownershipReport) {
	fmt.Printf("event-ownership: symbols %d owners %d packages %d candidates %d direct %d allowed %d\n",
		report.Counts.Symbols, report.Counts.Owners, report.Counts.PackagesScanned,
		report.Counts.Candidates, report.Counts.DirectCalls, report.Counts.Allowed)
	for _, note := range report.Notes {
		fmt.Println("event-ownership:", note)
	}
	for _, uncovered := range report.Uncovered {
		fmt.Printf("event-ownership: uncovered %s:%d %s %s — %s\n",
			uncovered.File, uncovered.Line, uncovered.Caller, uncovered.Name, uncovered.Reason)
	}
	for _, finding := range report.Violations {
		fmt.Printf("event-ownership: %s %s:%d %s owner=%s: %s\n",
			finding.Rule, finding.File, finding.Line, finding.Symbol, finding.Owner, finding.Detail)
	}
	if report.OK {
		fmt.Printf("event-ownership: OK (%d uncovered call site(s) reported, none counted as covered; "+
			"a pass is structural coverage, never product behaviour)\n", report.Counts.Uncovered)
		return
	}
	fmt.Println("event-ownership: FAILED")
}

func executeEventOwnership(opts ownershipOptions) (*ownershipReport, int) {
	report := &ownershipReport{Golden: opts.Golden, Uncovered: []ownershipUncovered{},
		NonTarget: []ownershipFinding{}, Violations: []ownershipFinding{}, Notes: []string{}}
	if absolute, err := filepath.Abs(opts.RepoRoot); err == nil {
		opts.RepoRoot = absolute
	}
	table, modulePath, err := loadOwnershipTable(opts.Golden)
	if err != nil {
		report.Notes = append(report.Notes, "load-error: "+err.Error())
		return report, 2
	}
	report.Module = modulePath
	packages, exports, err := listModulePackages(opts.RepoRoot)
	if err != nil {
		report.Notes = append(report.Notes, "go-list-error: "+err.Error())
		return report, 2
	}
	firstParty := map[string]*listedPackage{}
	excluded := map[string]bool{}
	for _, name := range architectureScope(opts.Golden) {
		excluded[name] = true
	}
	for _, pkg := range packages {
		if pkg.Module == nil || pkg.Module.Path != modulePath {
			continue
		}
		rel := relativePackage(pkg.ImportPath, modulePath)
		if rel == "" {
			continue
		}
		firstParty[rel] = pkg
	}
	importerFor := importer.ForCompiler(token.NewFileSet(), "gc", exportLookup(exports))
	universe, findings := buildSymbolUniverse(report, table, firstParty, importerFor)
	report.Violations = append(report.Violations, findings...)
	owners := buildOwnerScopes(table, universe, &report.Violations)
	report.Counts.Owners = len(owners)
	report.Counts.Symbols = len(table.Symbols)
	if len(report.Violations) > 0 {
		report.OK = false
		return report, 1
	}
	frozenNames := map[string]bool{}
	for name := range universe.byName {
		frozenNames[name] = true
	}
	rels := make([]string, 0, len(firstParty))
	for rel := range firstParty {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if scopeExcluded(rel, excluded) {
			continue
		}
		report.Counts.PackagesScanned++
		scanPackage(report, firstParty[rel], rel, opts.RepoRoot, modulePath, frozenNames,
			ownerScopes(owners), universe, importerFor)
	}
	report.Counts.Uncovered = len(report.Uncovered)
	report.OK = len(report.Violations) == 0
	if report.OK {
		return report, 0
	}
	return report, 1
}

// loadOwnershipTable reads the frozen table beside the package policy it extends.
// The module path is read from the same file so the gate cannot point at another
// tree's golden by accident.
func loadOwnershipTable(path string) (*ownershipTable, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("cannot read architecture golden: %w", err)
	}
	var head struct {
		ModulePath     string          `json:"module_path"`
		EventOwnership json.RawMessage `json:"event_ownership"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, "", fmt.Errorf("architecture golden is not valid JSON: %w", err)
	}
	if head.ModulePath == "" {
		return nil, "", errors.New("architecture golden carries no module_path")
	}
	if len(head.EventOwnership) == 0 {
		return nil, "", errors.New("architecture golden carries no event_ownership table")
	}
	decoder := json.NewDecoder(bytes.NewReader(head.EventOwnership))
	decoder.DisallowUnknownFields()
	var table ownershipTable
	if err := decoder.Decode(&table); err != nil {
		return nil, "", fmt.Errorf("event_ownership is malformed: %w", err)
	}
	if table.SchemaVersion != 1 {
		return nil, "", fmt.Errorf("event_ownership schema_version must be 1, got %d", table.SchemaVersion)
	}
	if len(table.Symbols) == 0 || len(table.Owners) == 0 {
		return nil, "", errors.New("event_ownership needs a non-empty symbols and owners list")
	}
	return &table, head.ModulePath, nil
}

// architectureScope reads the existing package scope's excluded directory names,
// so the ownership gate scans exactly the first-party product tree the package
// policy already judges and never reads test fixtures or tool sources.
func architectureScope(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var head struct {
		Scope struct {
			ExcludeDirNames []string `json:"exclude_dir_names"`
		} `json:"scope"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil
	}
	return head.Scope.ExcludeDirNames
}

func scopeExcluded(rel string, excluded map[string]bool) bool {
	for _, segment := range strings.Split(rel, "/") {
		if excluded[segment] {
			return true
		}
	}
	return false
}

func listModulePackages(repoRoot string) ([]*listedPackage, map[string]string, error) {
	command := exec.Command("go", "list", "-export", "-deps", "-json", "./...")
	command.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", err, tailLines(stderr.String(), 6))
	}
	decoder := json.NewDecoder(&stdout)
	var packages []*listedPackage
	exports := map[string]string{}
	for {
		var record listedPackage
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("cannot parse go list output: %w", err)
		}
		packages = append(packages, &record)
		if record.Export != "" {
			exports[record.ImportPath] = record.Export
		}
	}
	return packages, exports, nil
}

func tailLines(text string, count int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, " | ")
}

func exportLookup(exports map[string]string) func(string) (io.ReadCloser, error) {
	return func(path string) (io.ReadCloser, error) {
		export, ok := exports[path]
		if !ok || export == "" {
			return nil, fmt.Errorf("no compiled export data for %q (go list did not build it)", path)
		}
		return os.Open(export)
	}
}

func relativePackage(importPath, modulePath string) string {
	if importPath == modulePath {
		return "."
	}
	if strings.HasPrefix(importPath, modulePath+"/") {
		return strings.TrimPrefix(importPath, modulePath+"/")
	}
	return ""
}

// buildSymbolUniverse resolves every frozen symbol against the compiled tree. A
// symbol that cannot be resolved means the table drifted from the code: the gate
// fails instead of checking a name nobody declares.
func buildSymbolUniverse(report *ownershipReport, table *ownershipTable, firstParty map[string]*listedPackage,
	importerFor types.Importer) (*symbolUniverse, []ownershipFinding) {
	universe := &symbolUniverse{byIdentity: map[string]*frozenSymbol{}, byName: map[string][]*frozenSymbol{}}
	var findings []ownershipFinding
	ids := map[string]bool{}
	for index, symbol := range table.Symbols {
		where := fmt.Sprintf("symbols[%d]", index)
		if symbol.ID == "" || ids[symbol.ID] {
			findings = append(findings, ownershipFinding{Rule: "table", Symbol: symbol.ID,
				Detail: where + ": symbol id must be non-empty and unique"})
			continue
		}
		ids[symbol.ID] = true
		record, ok := firstParty[symbol.Pkg]
		if !ok {
			findings = append(findings, ownershipFinding{Rule: "symbol-unresolved", Symbol: symbol.ID,
				Detail: where + ": package " + symbol.Pkg + " is not a first-party package of this tree"})
			continue
		}
		if symbol.Func == "" && (len(symbol.Receivers) == 0 || symbol.Method == "") {
			findings = append(findings, ownershipFinding{Rule: "table", Symbol: symbol.ID,
				Detail: where + ": declare either func or receivers+method"})
			continue
		}
		if symbol.Func != "" && (len(symbol.Receivers) > 0 || symbol.Method != "") {
			findings = append(findings, ownershipFinding{Rule: "table", Symbol: symbol.ID,
				Detail: where + ": a function symbol carries no receivers or method"})
			continue
		}
		pkg, err := importerFor.Import(record.ImportPath)
		if err != nil {
			findings = append(findings, ownershipFinding{Rule: "symbol-unresolved", Symbol: symbol.ID,
				Detail: where + ": cannot load package " + symbol.Pkg + ": " + err.Error()})
			continue
		}
		if symbol.Func != "" {
			object := pkg.Scope().Lookup(symbol.Func)
			function, ok := object.(*types.Func)
			if !ok {
				findings = append(findings, ownershipFinding{Rule: "symbol-unresolved", Symbol: symbol.ID,
					Detail: where + ": " + symbol.Pkg + "." + symbol.Func + " is not a declared function"})
				continue
			}
			entry := &frozenSymbol{id: symbol.ID, pkg: symbol.Pkg, name: symbol.Func, sig: function.Signature()}
			universe.add(entry)
			continue
		}
		seen := map[string]bool{}
		for _, receiver := range symbol.Receivers {
			if receiver == "" || seen[receiver] {
				findings = append(findings, ownershipFinding{Rule: "table", Symbol: symbol.ID,
					Detail: where + ": receiver names must be non-empty and unique"})
				continue
			}
			seen[receiver] = true
			object := pkg.Scope().Lookup(receiver)
			if object == nil {
				findings = append(findings, ownershipFinding{Rule: "symbol-unresolved", Symbol: symbol.ID,
					Detail: where + ": type " + symbol.Pkg + "." + receiver + " is not declared"})
				continue
			}
			method, _, _ := types.LookupFieldOrMethod(object.Type(), true, pkg, symbol.Method)
			function, ok := method.(*types.Func)
			if !ok {
				findings = append(findings, ownershipFinding{Rule: "symbol-unresolved", Symbol: symbol.ID,
					Detail: where + ": " + symbol.Pkg + "." + receiver + "." + symbol.Method + " is not a method"})
				continue
			}
			entry := &frozenSymbol{id: symbol.ID, pkg: symbol.Pkg, receiver: receiver,
				name: symbol.Method, sig: function.Signature()}
			universe.add(entry)
		}
	}
	report.Notes = append(report.Notes,
		fmt.Sprintf("frozen symbols resolved: %d identities of %d table entries", len(universe.byIdentity), len(ids)))
	return universe, findings
}

func (u *symbolUniverse) add(entry *frozenSymbol) {
	key := identityKey(entry.pkg, entry.receiver, entry.name)
	u.byIdentity[key] = entry
	u.byName[entry.name] = append(u.byName[entry.name], entry)
}

func identityKey(pkg, receiver, name string) string {
	return pkg + "\x00" + receiver + "\x00" + name
}

func buildOwnerScopes(table *ownershipTable, universe *symbolUniverse, findings *[]ownershipFinding) map[string]*ownerScope {
	scopes := map[string]*ownerScope{}
	for index, owner := range table.Owners {
		where := fmt.Sprintf("owners[%d]", index)
		if owner.ID == "" || scopes[owner.ID] != nil {
			*findings = append(*findings, ownershipFinding{Rule: "table", Owner: owner.ID,
				Detail: where + ": owner id must be non-empty and unique"})
			continue
		}
		if len(owner.Packages) == 0 {
			*findings = append(*findings, ownershipFinding{Rule: "table", Owner: owner.ID,
				Detail: where + ": an owner needs at least one package"})
			continue
		}
		scope := &ownerScope{id: owner.ID, packages: append([]string{}, owner.Packages...),
			files: append([]string{}, owner.Files...), allowed: map[string]bool{}, forbidden: map[string]bool{}}
		problem := ""
		for _, id := range owner.Allowed {
			if universe.symbol(id) == nil {
				problem = "allowed symbol " + id + " is not declared in symbols"
			}
			scope.allowed[id] = true
		}
		for _, id := range owner.Forbidden {
			if universe.symbol(id) == nil {
				problem = "forbidden symbol " + id + " is not declared in symbols"
			}
			if scope.allowed[id] {
				problem = "symbol " + id + " is both allowed and forbidden"
			}
			scope.forbidden[id] = true
		}
		if len(owner.Allowed) == 0 {
			problem = "an owner needs at least one allowed symbol"
		}
		if problem != "" {
			*findings = append(*findings, ownershipFinding{Rule: "table", Owner: owner.ID, Detail: where + ": " + problem})
			continue
		}
		scopes[owner.ID] = scope
	}
	for id, scope := range scopes {
		if len(scope.files) == 0 {
			continue
		}
		for file := range stringSet(scope.files) {
			if strings.Contains(file, "*") || strings.Contains(file, "/") {
				*findings = append(*findings, ownershipFinding{Rule: "table", Owner: id,
					Detail: "file scopes must be exact base names (" + file + ")"})
			}
		}
	}
	return scopes
}

func (u *symbolUniverse) symbol(id string) *frozenSymbol {
	for _, entry := range u.byIdentity {
		if entry.id == id {
			return entry
		}
	}
	return nil
}

func stringSet(values []string) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	return set
}

func ownerScopes(scopes map[string]*ownerScope) []*ownerScope {
	ordered := make([]*ownerScope, 0, len(scopes))
	for _, scope := range scopes {
		ordered = append(ordered, scope)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].id < ordered[j].id })
	return ordered
}

func scanPackage(report *ownershipReport, pkg *listedPackage, rel, repoRoot, modulePath string,
	frozenNames map[string]bool, owners []*ownerScope, universe *symbolUniverse, importerFor types.Importer) {
	fset := token.NewFileSet()
	var files []*ast.File
	var candidates []callCandidate
	for _, name := range pkg.GoFiles {
		path := filepath.Join(pkg.Dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			report.Violations = append(report.Violations, ownershipFinding{File: displayPath(repoRoot, path),
				Rule: "parse-error", Detail: err.Error()})
			continue
		}
		files = append(files, file)
		candidates = append(candidates, collectCandidates(fset, file, displayPath(repoRoot, path), name,
			rel, frozenNames)...)
	}
	if len(candidates) == 0 {
		return
	}
	report.Counts.Candidates += len(candidates)
	_, info, typeErr := typeCheckPackage(pkg, fset, files, importerFor)
	for _, candidate := range candidates {
		if candidate.kind == candidateCall {
			report.Counts.DirectCalls++
		}
		classifyCandidate(report, candidate, info, typeErr, universe, owners, modulePath)
	}
}

func typeCheckPackage(pkg *listedPackage, fset *token.FileSet, files []*ast.File,
	importerFor types.Importer) (*types.Package, *types.Info, error) {
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	var first error
	config := types.Config{
		Importer: importerFor,
		Error: func(err error) {
			if first == nil {
				first = err
			}
		},
	}
	checked, err := config.Check(pkg.ImportPath, fset, files, info)
	if err != nil && first == nil {
		first = err
	}
	return checked, info, first
}

func displayPath(repoRoot, path string) string {
	if rel, err := filepath.Rel(repoRoot, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func collectCandidates(fset *token.FileSet, file *ast.File, name, base, pkgRel string,
	frozenNames map[string]bool) []callCandidate {
	var out []callCandidate
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		caller := function.Name.Name
		if function.Recv != nil && len(function.Recv.List) == 1 {
			caller = receiverName(function.Recv.List[0].Type) + "." + caller
		}
		seen := map[*ast.SelectorExpr]bool{}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
					if frozenNames[selector.Sel.Name] {
						seen[selector] = true
						out = append(out, callCandidate{file: name, base: base, line: fset.Position(selector.Pos()).Line,
							caller: caller, pkgRel: pkgRel, name: selector.Sel.Name, sel: selector, kind: candidateCall})
					}
					if selector.Sel.Name == "MethodByName" && len(call.Args) == 1 {
						if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
							if decoded, err := strconv.Unquote(literal.Value); err == nil && frozenNames[decoded] {
								out = append(out, callCandidate{file: name, base: base, line: fset.Position(call.Pos()).Line,
									caller: caller, pkgRel: pkgRel, name: decoded, sel: selector, kind: candidateReflect})
							}
						}
					}
				}
				return true
			}
			selector, ok := node.(*ast.SelectorExpr)
			if ok && frozenNames[selector.Sel.Name] && !seen[selector] {
				out = append(out, callCandidate{file: name, base: base, line: fset.Position(selector.Pos()).Line,
					caller: caller, pkgRel: pkgRel, name: selector.Sel.Name, sel: selector, kind: candidateValue})
			}
			return true
		})
	}
	return out
}

func receiverName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.StarExpr:
		return receiverName(typed.X)
	case *ast.Ident:
		return typed.Name
	case *ast.IndexExpr:
		return receiverName(typed.X)
	case *ast.IndexListExpr:
		return receiverName(typed.X)
	}
	return "?"
}

// resolvedCall is the exact object go/types resolved: package + receiver named
// type + method name, with the method signature telling a same-name operation
// apart from an unregistered front.
type resolvedCall struct {
	pkgPath  string
	receiver string
	name     string
	sig      *types.Signature
}

func resolveCandidate(info *types.Info, selector *ast.SelectorExpr) (resolvedCall, bool) {
	if selection, ok := info.Selections[selector]; ok {
		switch selection.Kind() {
		case types.MethodVal, types.MethodExpr:
			function, ok := selection.Obj().(*types.Func)
			if !ok {
				return resolvedCall{}, false
			}
			return resolvedCall{pkgPath: packagePath(function), receiver: namedTypeName(selection.Recv()),
				name: function.Name(), sig: function.Signature()}, true
		case types.FieldVal:
			field, ok := selection.Obj().(*types.Var)
			if !ok {
				return resolvedCall{}, false
			}
			return resolvedCall{pkgPath: packagePath(field), name: field.Name(),
				sig: signatureOf(field.Type())}, true
		}
	}
	if function, ok := info.Uses[selector.Sel].(*types.Func); ok {
		return resolvedCall{pkgPath: packagePath(function), name: function.Name(), sig: function.Signature()}, true
	}
	return resolvedCall{}, false
}

func packagePath(object types.Object) string {
	if object == nil || object.Pkg() == nil {
		return ""
	}
	return object.Pkg().Path()
}

func namedTypeName(value types.Type) string {
	for {
		switch typed := value.(type) {
		case *types.Pointer:
			value = typed.Elem()
		case *types.Named:
			return typed.Obj().Name()
		case *types.Alias:
			value = types.Unalias(typed)
		default:
			return ""
		}
	}
}

func signatureOf(value types.Type) *types.Signature {
	if signature, ok := value.(*types.Signature); ok {
		return signature
	}
	if named, ok := value.(*types.Named); ok {
		if signature, ok := named.Underlying().(*types.Signature); ok {
			return signature
		}
	}
	return nil
}

func classifyCandidate(report *ownershipReport, candidate callCandidate, info *types.Info, typeErr error,
	universe *symbolUniverse, owners []*ownerScope, modulePath string) {
	if candidate.kind == candidateReflect {
		report.Uncovered = append(report.Uncovered, ownershipUncovered{File: candidate.file, Line: candidate.line,
			Caller: candidate.caller, Name: candidate.name,
			Reason: "reflective dispatch by literal method name cannot be resolved statically"})
		return
	}
	resolved, ok := resolveCandidate(info, candidate.sel)
	if !ok {
		reason := "no type information for this selector"
		if typeErr != nil {
			reason += ": " + typeErr.Error()
		}
		report.Uncovered = append(report.Uncovered, ownershipUncovered{File: candidate.file, Line: candidate.line,
			Caller: candidate.caller, Name: candidate.sel.Sel.Name, Reason: reason})
		return
	}
	key := identityKey(relativePackage(resolved.pkgPath, modulePath), resolved.receiver, resolved.name)
	symbol, hit := universe.byIdentity[key]
	if !hit {
		if front := universe.matchSignature(resolved.name, resolved.sig); front != nil {
			report.Uncovered = append(report.Uncovered, ownershipUncovered{File: candidate.file, Line: candidate.line,
				Caller: candidate.caller, Name: resolved.name, Symbol: front.id,
				Reason: "a call with the frozen signature on an unregistered receiver cannot be proven not to reach it"})
			return
		}
		report.NonTarget = append(report.NonTarget, ownershipFinding{File: candidate.file, Line: candidate.line,
			Caller: candidate.caller, Symbol: resolved.name, Rule: "same-name-non-target",
			Detail: "resolved to another declaration with a different signature; not a frozen target"})
		report.Counts.SameNameNonTarget++
		return
	}
	if candidate.kind != candidateCall {
		report.Uncovered = append(report.Uncovered, ownershipUncovered{File: candidate.file, Line: candidate.line,
			Caller: candidate.caller, Name: resolved.name, Symbol: symbol.id,
			Reason: "method value of a frozen symbol is passed on as a callback; the call happens inside the port"})
		return
	}
	ownerID := ownerOf(owners, candidate.pkgRel, candidate.base)
	switch ownerID {
	case "":
		report.Counts.Unowned++
		report.Violations = append(report.Violations, ownershipFinding{File: candidate.file, Line: candidate.line,
			Caller: candidate.caller, Symbol: symbol.id, Rule: "unowned",
			Detail: "no frozen owner covers this call site; declare the owner before calling a frozen symbol"})
		return
	case "!":
		report.Counts.Ambiguous++
		report.Violations = append(report.Violations, ownershipFinding{File: candidate.file, Line: candidate.line,
			Caller: candidate.caller, Symbol: symbol.id, Rule: "ambiguous-owner",
			Detail: "multiple frozen owners claim this file; ownership must be one place"})
		return
	}
	scope := ownerScopeByID(owners, ownerID)
	switch {
	case scope.forbidden[symbol.id]:
		report.Counts.Forbidden++
		report.Violations = append(report.Violations, ownershipFinding{File: candidate.file, Line: candidate.line,
			Caller: candidate.caller, Symbol: symbol.id, Owner: ownerID, Rule: "forbidden",
			Detail: "the frozen table forbids this owner from calling the symbol directly"})
	case scope.allowed[symbol.id]:
		report.Counts.Allowed++
	default:
		report.Counts.Unregistered++
		report.Violations = append(report.Violations, ownershipFinding{File: candidate.file, Line: candidate.line,
			Caller: candidate.caller, Symbol: symbol.id, Owner: ownerID, Rule: "unregistered",
			Detail: "the frozen table does not allow this owner this symbol; a new edge needs an orchestrator ruling"})
	}
}

func ownerOf(owners []*ownerScope, pkgRel, file string) string {
	var hits []string
	for _, owner := range owners {
		if !containsString(owner.packages, pkgRel) {
			continue
		}
		if len(owner.files) > 0 && !containsString(owner.files, file) {
			continue
		}
		hits = append(hits, owner.id)
	}
	switch len(hits) {
	case 0:
		return ""
	case 1:
		return hits[0]
	default:
		return "!"
	}
}

func ownerScopeByID(owners []*ownerScope, id string) *ownerScope {
	for _, owner := range owners {
		if owner.id == id {
			return owner
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (u *symbolUniverse) matchSignature(name string, signature *types.Signature) *frozenSymbol {
	if signature == nil {
		return nil
	}
	for _, entry := range u.byName[name] {
		if signaturesMatch(entry.sig, signature) {
			return entry
		}
	}
	return nil
}

// signaturesMatch compares the callable shape without the receiver: a method on an
// unrelated type with the same name but another shape is a different operation,
// while the same shape on an unregistered front could be the frozen one.
func signaturesMatch(left, right *types.Signature) bool {
	if left == nil || right == nil {
		return false
	}
	if left.Variadic() != right.Variadic() {
		return false
	}
	if left.Params().Len() != right.Params().Len() || left.Results().Len() != right.Results().Len() {
		return false
	}
	if left.TypeParams().Len() != right.TypeParams().Len() || left.RecvTypeParams().Len() != right.RecvTypeParams().Len() {
		return false
	}
	for index := 0; index < left.Params().Len(); index++ {
		if !types.Identical(left.Params().At(index).Type(), right.Params().At(index).Type()) {
			return false
		}
	}
	for index := 0; index < left.Results().Len(); index++ {
		if !types.Identical(left.Results().At(index).Type(), right.Results().At(index).Type()) {
			return false
		}
	}
	return true
}
