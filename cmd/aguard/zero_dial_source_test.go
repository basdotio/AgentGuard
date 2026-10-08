// SPDX-License-Identifier: MIT
package main

// The source half of the zero-dial claim. TestZeroDial_OnlyTheJudgeConnects counts the requests
// that cross judge.Transport or http.DefaultTransport; a client built with a transport of its own
// crosses neither, and would dial while both counters read zero. Inside internal/judge that
// cannot go unnoticed — the positive control goes red the moment the judge's requests stop
// crossing the seam — so the place it could is the rest of the product code, and that is what
// this reads: every non-test file of every package the aguard binary is built from.
//
// It reads source, not behaviour, and only for these two shapes. A raw net.Dial, a child process
// and an asynchronous send are still outside both tests (invariant #1 lists them).

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestZeroDial_NoClientOutsideTheJudge: outside internal/judge, product code never names
// net/http's Client or Transport type — so it cannot build a client or a transport of its own,
// whether by literal, new(), a declared variable or a Clone of the default one — and no non-test
// file assigns judge.Transport or takes its address, which is what keeps "nil in production" true.
func TestZeroDial_NoClientOutsideTheJudge(t *testing.T) {
	module := goModulePath(t)
	fset := token.NewFileSet()
	var files int
	var seamDeclared bool
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repoRoot(), top), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(repoRoot(), p)
			if err != nil {
				return err
			}
			f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			files++
			found, declared := dialSourceViolations(fset, module, filepath.ToSlash(rel), f)
			seamDeclared = seamDeclared || declared
			for _, v := range found {
				t.Error(v)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Both are reverse assertions: a walk that read nothing, or a seam that moved, would make
	// every line above pass while checking nothing.
	if files == 0 {
		t.Fatal("no product .go file was read under cmd/ or internal/: the check is looking in the wrong place")
	}
	if !seamDeclared {
		t.Error("no package-level `var Transport` in internal/judge: the seam this check guards was renamed or removed, so it guards nothing")
	}
}

// dialSourceViolations reports, for one file at repo-relative path rel, every way it steps around
// the two counters, and whether it is the file that declares the judge's seam.
func dialSourceViolations(fset *token.FileSet, module, rel string, f *ast.File) (found []string, seamDeclared bool) {
	inJudge := path.Dir(rel) == "internal/judge"
	judgePath := module + "/internal/judge"
	at := func(n ast.Node) string { return fmt.Sprintf("%s:%d", rel, fset.Position(n.Pos()).Line) }

	var httpName, judgeName string
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		local := path.Base(p)
		if imp.Name != nil {
			local = imp.Name.Name
		}
		switch {
		case p == "net/http":
			httpName = local
		case p == judgePath:
			judgeName = local
		case strings.HasPrefix(p, module+"/") && !strings.HasPrefix(p, module+"/internal/") && !strings.HasPrefix(p, module+"/cmd/"):
			found = append(found, fmt.Sprintf("%s imports %s, which this check does not read: the binary is now built from more than cmd/ and internal/, so widen the walk", at(imp), p))
		}
		if local == "." && (p == "net/http" || p == judgePath) {
			found = append(found, fmt.Sprintf("%s dot-imports %s, which hides the names this check looks for", at(imp), p))
		}
	}

	isSeam := func(e ast.Expr) bool {
		switch e := ast.Unparen(e).(type) {
		case *ast.SelectorExpr:
			id, ok := e.X.(*ast.Ident)
			return ok && judgeName != "" && id.Name == judgeName && e.Sel.Name == "Transport"
		case *ast.Ident:
			return inJudge && e.Name == "Transport"
		}
		return false
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			id, ok := n.X.(*ast.Ident)
			if ok && !inJudge && httpName != "" && id.Name == httpName && (n.Sel.Name == "Client" || n.Sel.Name == "Transport") {
				found = append(found, fmt.Sprintf("%s names http.%s outside internal/judge: a client or transport built here can carry a transport of its own, which crosses neither judge.Transport nor http.DefaultTransport, and TestZeroDial_OnlyTheJudgeConnects cannot count it — send through the judge's client, or move the code into internal/judge where the positive control watches it", at(n), n.Sel.Name))
			}
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if isSeam(lhs) {
					found = append(found, fmt.Sprintf("%s assigns judge.Transport outside a test: it is a test seam, and production must leave it nil (http.DefaultTransport)", at(lhs)))
				}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND && isSeam(n.X) {
				found = append(found, fmt.Sprintf("%s takes the address of judge.Transport outside a test, which is an assignment this check cannot follow", at(n)))
			}
		}
		return true
	})

	if inJudge {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for _, name := range vs.Names {
					if name.Name != "Transport" {
						continue
					}
					seamDeclared = true
					if len(vs.Values) > 0 {
						found = append(found, fmt.Sprintf("%s gives judge.Transport an initial value: production must leave it nil (http.DefaultTransport)", at(name)))
					}
				}
			}
		}
	}
	return found, seamDeclared
}

// goModulePath reads the module path from go.mod rather than repeating it: it already has to
// agree in three places, and a fourth copy here would be the one a rename forgets.
func goModulePath(t *testing.T) string {
	t.Helper()
	f, err := os.Open(filepath.Join(repoRoot(), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatal("go.mod has no module line")
	return ""
}
