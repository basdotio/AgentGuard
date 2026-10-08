// SPDX-License-Identifier: MIT
package main

// The source half of the zero-dial claim. TestZeroDial_OnlyTheJudgeConnects counts the requests
// that cross judge.Transport or http.DefaultTransport; a client built with a transport of its own
// crosses neither, and would dial while both counters read zero. The positive control does not
// close that inside internal/judge either: it watches the client its own paths go through, so a
// second client the judge package builds, called from an entry point in the zero table, dials
// unseen. So this reads every non-test file of every package of this module the aguard binary is
// built from, internal/judge included: outside it, no client or transport at all; inside it,
// exactly one client — NewHTTP's, whose transport is the seam.
//
// It reads source, not behaviour, and only these shapes. A raw net.Dial, a child process, an
// asynchronous send, and a dependency that builds a client with a transport of its own in its own
// code (the walk reads this module, not what it imports) are still outside both tests (invariant
// #1 lists them).

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
	"slices"
	"strconv"
	"strings"
	"testing"
)

// seamClientFunc is the one function in internal/judge that may build an http.Client, and only
// as http.Client{Transport: Transport}: the client every judge request crosses the seam in.
const seamClientFunc = "NewHTTP"

// TestZeroDial_NoClientOutsideTheJudge: outside internal/judge, product code never names
// net/http's Client or Transport type — so it cannot build a client or a transport of its own,
// whether by literal, new(), a declared variable or a Clone of the default one. Inside
// internal/judge the one client built is NewHTTP's http.Client{Transport: Transport}: the package
// never names http.Transport, and names http.Client only as that literal's type or as the type of
// a *http.Client field, parameter or result, which declares a client and builds none. No non-test
// file assigns judge.Transport or takes its address, which keeps "nil in production" true.
func TestZeroDial_NoClientOutsideTheJudge(t *testing.T) {
	module := goModulePath(t)
	fset := token.NewFileSet()
	var files, seamBuilt int
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
			r := dialSourceViolations(fset, module, filepath.ToSlash(rel), f)
			seamDeclared = seamDeclared || r.seamDeclared
			seamBuilt += r.seamBuilt
			for _, v := range r.found {
				t.Error(v)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// All three are reverse assertions: a walk that read nothing, a seam that moved, or a judge
	// client now built some other way would make every line above pass while checking nothing.
	if files == 0 {
		t.Fatal("no product .go file was read under cmd/ or internal/: the check is looking in the wrong place")
	}
	if !seamDeclared {
		t.Error("no package-level `var Transport` in internal/judge: the seam this check guards was renamed or removed, so it guards nothing")
	}
	if seamBuilt != 1 {
		t.Errorf("found %d `http.Client{Transport: Transport}` literal(s) in judge.%s, want exactly 1: the one client construction this check allows moved or changed shape — move the check with it, or it no longer names the client the counters watch", seamBuilt, seamClientFunc)
	}
}

// dialSourceReport is what one file contributes to TestZeroDial_NoClientOutsideTheJudge.
type dialSourceReport struct {
	found        []string // every way the file steps around the two counters
	seamDeclared bool     // the file declares internal/judge's package-level `var Transport`
	seamBuilt    int      // http.Client{Transport: Transport} literals in judge.NewHTTP
}

// dialSourceViolations reads one file at repo-relative path rel.
func dialSourceViolations(fset *token.FileSet, module, rel string, f *ast.File) dialSourceReport {
	var r dialSourceReport
	inJudge := path.Dir(rel) == "internal/judge"
	at := func(n ast.Node) string { return fmt.Sprintf("%s:%d", rel, fset.Position(n.Pos()).Line) }
	httpName, judgeName := dialSourceImports(module, f, at, &r)

	isHTTP := func(e ast.Expr, names ...string) bool { return isPkgSelector(e, httpName, names...) }
	isSeam := func(e ast.Expr) bool {
		if id, ok := ast.Unparen(e).(*ast.Ident); ok {
			return inJudge && id.Name == "Transport"
		}
		return isPkgSelector(ast.Unparen(e), judgeName, "Transport")
	}
	var allowed map[ast.Expr]bool // inside internal/judge: the http.Client uses that build no other client
	if inJudge {
		allowed, r.seamBuilt = judgeClientAllowances(f, func(e ast.Expr) bool { return isHTTP(e, "Client") })
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if isHTTP(n, "Client", "Transport") && !allowed[n] {
				r.found = append(r.found, httpTypeViolation(at(n), n.Sel.Name, inJudge))
			}
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if isSeam(lhs) {
					r.found = append(r.found, fmt.Sprintf("%s assigns judge.Transport outside a test: it is a test seam, and production must leave it nil (http.DefaultTransport)", at(lhs)))
				}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND && isSeam(n.X) {
				r.found = append(r.found, fmt.Sprintf("%s takes the address of judge.Transport outside a test, which is an assignment this check cannot follow", at(n)))
			}
		}
		return true
	})
	if inJudge {
		r.seamDeclared = judgeSeamDeclared(f, at, &r)
	}
	return r
}

// isPkgSelector reports whether e is pkg.<one of names>, pkg being the local name a package is
// imported under ("" when the file does not import it, which matches nothing).
func isPkgSelector(e ast.Expr, pkg string, names ...string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || pkg == "" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg && slices.Contains(names, sel.Sel.Name)
}

// httpTypeViolation says why naming http.<name> at pos steps around the two counters.
func httpTypeViolation(pos, name string, inJudge bool) string {
	switch {
	case !inJudge:
		return fmt.Sprintf("%s names http.%s outside internal/judge: a client or transport built here can carry a transport of its own, which crosses neither judge.Transport nor http.DefaultTransport, and TestZeroDial_OnlyTheJudgeConnects cannot count it — send through a client from judge.%s", pos, name, seamClientFunc)
	case name == "Transport":
		return fmt.Sprintf("%s names http.Transport inside internal/judge: the judge's one client takes its transport from the seam (judge.Transport), and a transport built here crosses neither counter", pos)
	default:
		return fmt.Sprintf("%s names http.Client inside internal/judge other than as the type of judge.%s's http.Client{Transport: Transport} or of a *http.Client field, parameter or result: that can build a second client with a transport of its own, which crosses neither counter, and the positive control only watches the paths it runs — get the client from judge.%s", pos, seamClientFunc, seamClientFunc)
	}
}

// dialSourceImports returns the local names net/http and internal/judge are imported under in f
// ("" when not imported), recording the imports that would hide something from this check.
func dialSourceImports(module string, f *ast.File, at func(ast.Node) string, r *dialSourceReport) (httpName, judgeName string) {
	judgePath := module + "/internal/judge"
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
			r.found = append(r.found, fmt.Sprintf("%s imports %s, which this check does not read: the binary is now built from more than cmd/ and internal/, so widen the walk", at(imp), p))
		}
		if local == "." && (p == "net/http" || p == judgePath) {
			r.found = append(r.found, fmt.Sprintf("%s dot-imports %s, which hides the names this check looks for", at(imp), p))
		}
	}
	return httpName, judgeName
}

// judgeClientAllowances returns the http.Client type expressions in an internal/judge file that
// build no client but the seam's: the type of each http.Client{Transport: Transport} literal in
// NewHTTP (counted in seamBuilt), and the X of each *http.Client that is the type of a field,
// parameter or result — a declaration. Every other http.Client in the package is red.
//
// Which `Transport` the literal's value resolves to is not checked here. Whatever it is had to be
// built somewhere, and in this module that means naming http.Transport (red), wrapping
// http.DefaultTransport (counted), or one of the blind spots invariant #1 lists.
func judgeClientAllowances(f *ast.File, isClient func(ast.Expr) bool) (allowed map[ast.Expr]bool, seamBuilt int) {
	allowed = map[ast.Expr]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != seamClientFunc || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.CompositeLit); ok && isClient(lit.Type) && isSeamClientLiteral(lit) {
				allowed[lit.Type] = true
				seamBuilt++
			}
			return true
		})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if field, ok := n.(*ast.Field); ok {
			if star, ok := field.Type.(*ast.StarExpr); ok && isClient(star.X) {
				allowed[star.X] = true
			}
		}
		return true
	})
	return allowed, seamBuilt
}

// isSeamClientLiteral reports whether an http.Client literal is the seam's: every element keyed,
// and Transport set to the identifier Transport. Other fields (a redirect policy, a timeout) may
// join it; none of them carries a transport.
func isSeamClientLiteral(lit *ast.CompositeLit) bool {
	seam := false
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return false
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return false
		}
		if key.Name == "Transport" {
			v, ok := ast.Unparen(kv.Value).(*ast.Ident)
			if !ok || v.Name != "Transport" {
				return false
			}
			seam = true
		}
	}
	return seam
}

// judgeSeamDeclared reports whether an internal/judge file declares the package-level
// `var Transport`, recording a violation if it gives it an initial value.
func judgeSeamDeclared(f *ast.File, at func(ast.Node) string, r *dialSourceReport) (declared bool) {
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
				declared = true
				if len(vs.Values) > 0 {
					r.found = append(r.found, fmt.Sprintf("%s gives judge.Transport an initial value: production must leave it nil (http.DefaultTransport)", at(name)))
				}
			}
		}
	}
	return declared
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
