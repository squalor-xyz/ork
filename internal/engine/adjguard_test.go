// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// adjGuardAllowlist names the engine files whose adj client calls need no
// project-flag guard. The value is the reason (adj 593).
var adjGuardAllowlist = map[string]string{
	"adjrecord.go":  "owner-invoked record functions take no project flag; their callers guard",
	"answer_adj.go": "decideInAdj and the owner-invoked answer functions take no project flag; their callers guard",
}

// unguardedAdjCalls returns one "line: func" description for each use of the
// adj client (Deps.Adj) in f that is not guarded by a project Adj flag.
//
// The check is syntactic. A client use is a ".Adj" selector that is the
// operand of another selector (d.Adj.Check) or the right-hand side of an
// assignment (c := d.Adj). Any other ".Adj" selector is a bool read
// (Project.Adj). A client use is guarded when it lies in the body of an if
// whose condition reads the bool, or when an earlier statement in the same or
// an enclosing statement list is such an if whose body ends in return,
// continue or break.
func unguardedAdjCalls(fset *token.FileSet, f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		var stack []ast.Node
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if sel, ok := n.(*ast.SelectorExpr); ok && isAdjClientUse(sel, parentOf(stack)) && !adjGuarded(sel, stack) {
				out = append(out, strconv.Itoa(fset.Position(sel.Pos()).Line)+": "+fd.Name.Name)
			}
			stack = append(stack, n)
			return true
		})
	}
	return out
}

func parentOf(stack []ast.Node) ast.Node {
	if len(stack) == 0 {
		return nil
	}
	return stack[len(stack)-1]
}

// isAdjClientUse reports whether sel is a ".Adj" selector used as the adj
// client rather than as a bool read.
func isAdjClientUse(sel *ast.SelectorExpr, parent ast.Node) bool {
	if sel.Sel.Name != "Adj" {
		return false
	}
	switch p := parent.(type) {
	case *ast.SelectorExpr:
		return p.X == ast.Expr(sel)
	case *ast.AssignStmt:
		for _, r := range p.Rhs {
			if r == ast.Expr(sel) {
				return true
			}
		}
	}
	return false
}

// readsAdjBool reports whether expr contains a bool ".Adj" read.
func readsAdjBool(expr ast.Expr) bool {
	found := false
	var stack []ast.Node
	ast.Inspect(expr, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Adj" && !isAdjClientUse(sel, parentOf(stack)) {
			found = true
		}
		stack = append(stack, n)
		return !found
	})
	return found
}

// exitsEarly reports whether the block ends in return, continue or break.
func exitsEarly(b *ast.BlockStmt) bool {
	if len(b.List) == 0 {
		return false
	}
	switch s := b.List[len(b.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok == token.CONTINUE || s.Tok == token.BREAK
	}
	return false
}

// adjGuarded reports whether the client use n, with ancestors stack, is
// guarded by a bool Adj condition.
func adjGuarded(n ast.Node, stack []ast.Node) bool {
	for i, anc := range stack {
		child := n
		if i+1 < len(stack) {
			child = stack[i+1]
		}
		switch a := anc.(type) {
		case *ast.IfStmt:
			if child == ast.Node(a.Body) && readsAdjBool(a.Cond) {
				return true
			}
		case *ast.BlockStmt:
			if earlyExitGuardBefore(a.List, n) {
				return true
			}
		case *ast.CaseClause:
			if earlyExitGuardBefore(a.Body, n) {
				return true
			}
		case *ast.CommClause:
			if earlyExitGuardBefore(a.Body, n) {
				return true
			}
		}
	}
	return false
}

// earlyExitGuardBefore reports whether a statement in list that ends before n
// is an if on a bool Adj read with a body that exits early.
func earlyExitGuardBefore(list []ast.Stmt, n ast.Node) bool {
	for _, s := range list {
		if s.End() > n.Pos() {
			break
		}
		if ifs, ok := s.(*ast.IfStmt); ok && readsAdjBool(ifs.Cond) && exitsEarly(ifs.Body) {
			return true
		}
	}
	return false
}

func checkAdjSnippet(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "snippet.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse snippet: %v", err)
	}
	return unguardedAdjCalls(fset, f)
}

func TestAdjGuardFlagsUnguardedCall(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"no condition", `package p

func one(e *E) {
	e.d.Adj.Check(1)
}
`, "4: one"},
		{"unrelated if body", `package p

func two(e *E) {
	if e.n > 0 {
		e.d.Adj.Check(1)
	}
}
`, "5: two"},
		{"unrelated early exit", `package p

func three(e *E) {
	if e.n > 0 {
		return
	}
	e.d.Adj.Check(1)
}
`, "7: three"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkAdjSnippet(t, c.src)
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("findings = %q, want [%q]", got, c.want)
			}
		})
	}
}

func TestAdjGuardFlagsAliasedClient(t *testing.T) {
	got := checkAdjSnippet(t, `package p

func alias(d Deps) {
	c := d.Adj
	_ = c
}
`)
	if len(got) != 1 || got[0] != "4: alias" {
		t.Fatalf("findings = %q, want [\"4: alias\"]", got)
	}
}

func TestAdjGuardAcceptsIfBodyGuard(t *testing.T) {
	got := checkAdjSnippet(t, `package p

func plain(e *E) {
	if e.proj.Adj {
		e.d.Adj.Check(1)
	}
}

func compound(e *E, f F) {
	if f.Key != "" && e.proj.Adj {
		e.d.Adj.Check(1)
	}
}

func nested(e *E) {
	if e.proj.Adj {
		for i := 0; i < 2; i++ {
			c := e.d.Adj
			_ = c
		}
	}
}
`)
	if len(got) != 0 {
		t.Fatalf("findings = %q, want none", got)
	}
}

func TestAdjGuardAcceptsEarlyExitGuard(t *testing.T) {
	got := checkAdjSnippet(t, `package p

func returns(e *E) int {
	if !e.proj.Adj {
		return 0
	}
	e.d.Adj.Check(1)
	return 1
}

func continues(d Deps, project P, qs []Q) {
	for _, q := range qs {
		if !project.Adj || q.Key == "" {
			continue
		}
		d.Adj.CheckRecord(q.Key)
	}
}

func enclosing(e *E, xs []int) {
	if !e.proj.Adj {
		return
	}
	for range xs {
		if len(xs) > 1 {
			e.d.Adj.Check(1)
		}
	}
}

func breaks(e *E, xs []int) {
	for range xs {
		if !e.proj.Adj {
			break
		}
		e.d.Adj.Check(1)
	}
}
`)
	if len(got) != 0 {
		t.Fatalf("findings = %q, want none", got)
	}
}

func TestAdjGuardIgnoresBoolReads(t *testing.T) {
	got := checkAdjSnippet(t, `package p

func reads(cfg Config) int {
	n := 0
	if cfg.Project.Adj {
		n++
	}
	if !cfg.Project.Adj || n > 1 {
		return 0
	}
	return n
}
`)
	if len(got) != 0 {
		t.Fatalf("findings = %q, want none", got)
	}
}

func TestEngineAdjCallsAreGuarded(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no .go files found in the package directory")
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if _, ok := adjGuardAllowlist[name]; ok {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		for _, finding := range unguardedAdjCalls(fset, f) {
			t.Errorf("%s:%s: adj client call is not guarded; guard it on the project's Adj flag, or add the file to adjGuardAllowlist with a reason", name, finding)
		}
	}
	if checked == 0 {
		t.Fatal("no engine files were checked")
	}
}
