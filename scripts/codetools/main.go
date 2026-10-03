// Command codetools emits the mechanical facts the repository comment and test-file
// policies are checked against: strip prints a file with comments removed in canonical
// go/printer form, decls prints one JSON object per top-level declaration.
//
// - Both subcommands exit non-zero on unreadable or unparsable input so a silent skip cannot masquerade as a pass.
// 规格: docs/comment-gate-tooling.md#usage
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: codetools <strip|decls|comment-check|merge-check|map-lint|name-check|doc-refs|proc-refs|tracked-hygiene> ...")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "comment-check":
		os.Exit(runCommentCheck(os.Args[2:]))
	case "map-lint":
		os.Exit(runMapLint(os.Args[2:]))
	case "merge-check":
		os.Exit(runMergeCheck(os.Args[2:]))
	case "name-check":
		os.Exit(runNameCheck(os.Args[2:]))
	case "doc-refs":
		os.Exit(runDocRefs(os.Args[2:]))
	case "proc-refs":
		os.Exit(runProcRefs(os.Args[2:]))
	case "tracked-hygiene":
		os.Exit(runTrackedHygiene(os.Args[2:]))
	}
	var fail int
	switch os.Args[1] {
	case "strip":
		for _, f := range os.Args[2:] {
			if err := runStrip(f); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
				fail++
			}
		}
	case "decls":
		for _, f := range os.Args[2:] {
			ds, err := runDecls(f)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
				fail++
				continue
			}
			enc := json.NewEncoder(os.Stdout)
			for _, d := range ds {
				if err := enc.Encode(d); err != nil {
					fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
					fail++
				}
			}
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
	if fail > 0 {
		os.Exit(1)
	}
}

// parse reads a file and returns its AST with comment attachment intact.
func parse(path string) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	return fset, file, nil
}

// isDirective reports whether a comment group carries a compiler or tool
// directive rather than documentation.
func isDirective(g *ast.CommentGroup) bool {
	if g == nil || len(g.List) == 0 {
		return false
	}
	t := g.List[0].Text
	for _, pre := range []string{"//go:", "//line ", "//export ", "//cgo ", "/*go:", "/*line "} {
		if strings.HasPrefix(t, pre) {
			return true
		}
	}
	return false
}

// printNoComments renders the file without any comment, in canonical form, with
// layout whitespace normalized by dropBlankLines.
func printNoComments(fset *token.FileSet, file *ast.File) (string, error) {
	clearComments(file)
	var b strings.Builder
	if err := printer.Fprint(&b, fset, file); err != nil {
		return "", err
	}
	return dropBlankLines(b.String()), nil
}

// dropBlankLines removes lines that hold nothing but whitespace, and trailing whitespace
// on the lines that remain.
//
// - Lines carrying string-literal content are exempt, including the empty lines a raw string holds.
// 规格: docs/comment-gate-tooling.md#equality-witness
func dropBlankLines(s string) string {
	flags := literalFlags(s)
	var out []string
	off := 0
	for _, l := range strings.Split(s, "\n") {
		insideLiteral := false
		for i := off; i < off+len(l)+1; i++ {
			if i < len(flags) && flags[i] {
				insideLiteral = true
				break
			}
		}
		off += len(l) + 1
		if insideLiteral {
			out = append(out, l)
			continue
		}
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, strings.TrimRight(l, " \t"))
	}
	return strings.Join(out, "\n") + "\n"
}

// literalFlags marks every byte that belongs to a string or rune literal, so
// line-oriented normalization can tell fixture content from layout.
func literalFlags(text string) []bool {
	flags := make([]bool, len(text))
	off := 0
	for _, seg := range splitOutsideLiterals(text) {
		if seg.literal {
			for i := off; i < off+len(seg.text) && i < len(flags); i++ {
				flags[i] = true
			}
		}
		off += len(seg.text)
	}
	return flags
}

// clearComments drops documentation comments while KEEPING compiler and tool directives,
// so printing yields code whose semantics are unchanged.
//
// - Only File.Comments is consulted because it holds every comment group in the file.
// 规格: docs/comment-gate-tooling.md#equality-witness
func clearComments(file *ast.File) {
	var keep []*ast.CommentGroup
	for _, g := range file.Comments {
		if isDirective(g) {
			keep = append(keep, g)
		}
	}
	file.Comments = keep
	file.Doc = nil
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.GenDecl:
			v.Doc = nil
		case *ast.FuncDecl:
			v.Doc = nil
		case *ast.ValueSpec:
			v.Doc = nil
			v.Comment = nil
		case *ast.TypeSpec:
			v.Doc = nil
			v.Comment = nil
		case *ast.ImportSpec:
			v.Doc = nil
			v.Comment = nil
		case *ast.Field:
			v.Doc = nil
			v.Comment = nil
		}
		return true
	})
}

func runStrip(path string) error {
	fset, file, err := parse(path)
	if err != nil {
		return err
	}
	out, err := printNoComments(fset, file)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "===== %s\n", path)
	fmt.Print(out)
	return nil
}

// decl is one JSON line produced by the decls subcommand.
type decl struct {
	File string `json:"file"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Test bool   `json:"test"`
	Hash string `json:"hash"`
	// raw is the comment-free rendering, kept so the head side can normalize declared
	// renames before hashing.
	raw      string
	Asserts  int `json:"asserts"`
	Parallel int `json:"parallel"`
	// norm is raw after qualifier blinding and declared-rename normalization; the body
	// hash is taken over it.
	norm string
}

func runDecls(path string) ([]decl, error) {
	return runDeclsFor(path)
}

// declView clears comments on a parsed file so each declaration's rendering —
// and therefore its hash — is independent of comment text.
func declView(file *ast.File) *ast.File {
	clearComments(file)
	return file
}

// render prints a single declaration without comments.
func render(fset *token.FileSet, n ast.Node) (string, error) {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, n); err != nil {
		return "", err
	}
	return b.String(), nil
}

// classify returns the declared names, a kind label and the node to hash.
func classify(d ast.Decl) ([]string, string, ast.Node) {
	switch v := d.(type) {
	case *ast.FuncDecl:
		if v.Recv != nil {
			r := ""
			if fl := v.Recv.List; len(fl) == 1 {
				if t, ok := fl[0].Type.(*ast.StarExpr); ok {
					r = exprName(t.X)
				} else {
					r = exprName(fl[0].Type)
				}
			}
			return []string{r + "." + v.Name.Name}, "method", v
		}
		return []string{v.Name.Name}, "func", v
	case *ast.GenDecl:
		var names []string
		for _, sp := range v.Specs {
			switch s := sp.(type) {
			case *ast.ValueSpec:
				for _, id := range s.Names {
					names = append(names, id.Name)
				}
			case *ast.TypeSpec:
				names = append(names, s.Name.Name)
			}
		}
		return names, v.Tok.String(), v
	}
	return nil, "", nil
}

func exprName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}

// isTestName reports whether the identifier is a Go test or benchmark entry.
func isTestName(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Example")
}

// countAsserts counts assertion and fatal-reporting calls, the guard against a
// merge that quietly weakens coverage.
func countAsserts(n ast.Node) int {
	var count int
	ast.Inspect(n, func(e ast.Node) bool {
		ce, ok := e.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch base := exprName(sel.X); base {
		case "require", "assert":
			count++
		case "t", "b", "tb":
			switch sel.Sel.Name {
			case "Fatal", "Fatalf", "Fatalln", "Error", "Errorf", "Errorln", "FailNow":
				count++
			}
		}
		return true
	})
	return count
}

// countParallel counts t.Parallel calls, which carry scheduling semantics and
// must not move between tests on their own.
func countParallel(n ast.Node) int {
	var count int
	ast.Inspect(n, func(e ast.Node) bool {
		ce, ok := e.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := ce.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" {
			count++
		}
		return true
	})
	return count
}
