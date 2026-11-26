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
// operand of another selector (d.Adj.Check), an assignment right-hand side
// (c := d.Adj), a call argument (use(d.Adj)), or a var value (var c = d.Adj).
// A bool passed as a call argument also counts as a client use; the tree has
// no such site. Any other ".Adj" selector is a bool read (Project.Adj).
// A client use is guarded in an if body whose condition being true implies
// the flag is set, or after an earlier if in the same or an enclosing statement
// list whose condition being false implies the flag is set and whose body
// ends in return, continue or break.
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
	case *ast.CallExpr:
		for _, arg := range p.Args {
			if arg == ast.Expr(sel) {
				return true
			}
		}
	case *ast.ValueSpec:
		for _, value := range p.Values {
			if value == ast.Expr(sel) {
				return true
			}
		}
	}
	return false
}

// condImpliesAdj reports whether cond evaluating to whenTrue implies the
// project's Adj flag is set. Only boolean operators and parentheses are walked;
// client expressions such as e.d.Adj != nil are not descended into.
func condImpliesAdj(cond ast.Expr, whenTrue bool) bool {
	switch c := cond.(type) {
	case *ast.ParenExpr:
		return condImpliesAdj(c.X, whenTrue)
	case *ast.UnaryExpr:
		if c.Op == token.NOT {
			return condImpliesAdj(c.X, !whenTrue)
		}
	case *ast.BinaryExpr:
		switch c.Op {
		case token.LAND:
			return whenTrue && (condImpliesAdj(c.X, true) || condImpliesAdj(c.Y, true))
		case token.LOR:
			return !whenTrue && (condImpliesAdj(c.X, false) || condImpliesAdj(c.Y, false))
		}
	case *ast.SelectorExpr:
		return c.Sel.Name == "Adj" && whenTrue
	}
	return false
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
			if child == ast.Node(a.Body) && condImpliesAdj(a.Cond, true) {
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
// is an if whose false condition implies Adj is set and whose body exits early.
func earlyExitGuardBefore(list []ast.Stmt, n ast.Node) bool {
	for _, s := range list {
		if s.End() > n.Pos() {
			break
		}
		if ifs, ok := s.(*ast.IfStmt); ok && condImpliesAdj(ifs.Cond, false) && exitsEarly(ifs.Body) {
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

func TestAdjGuardFlagsClientArgument(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"only argument", `package p

func argument(e *E) {
	use(e.d.Adj)
}
`, "4: argument"},
		{"second argument", `package p

func second(e *E) {
	use(ctx,
		e.d.Adj)
}
`, "5: second"},
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

func TestAdjGuardFlagsVarBoundClient(t *testing.T) {
	got := checkAdjSnippet(t, `package p

func bound(e *E) {
	var c = e.d.Adj
	_ = c
}
`)
	if len(got) != 1 || got[0] != "4: bound" {
		t.Fatalf("findings = %q, want [\"4: bound\"]", got)
	}
}

func TestAdjGuardFlagsInvertedGuard(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"positive early exit", `package p

func positive(e *E) {
	if e.proj.Adj { return }
	e.d.Adj.Check(1)
}
`, "5: positive"},
		{"negative body", `package p

func negative(e *E) {
	if !e.proj.Adj {
		e.d.Adj.Check(1)
	}
}
`, "5: negative"},
		{"positive OR body", `package p

func disjunction(e *E, f F) {
	if e.proj.Adj || f.Key != "" {
		e.d.Adj.Check(1)
	}
}
`, "5: disjunction"},
		{"negative AND early exit", `package p

func conjunction(e *E, f F) {
	if !e.proj.Adj && f.Key != "" { return }
	e.d.Adj.Check(1)
}
`, "5: conjunction"},
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
	for _, use := range []string{"use(e.d.Adj)", "var c = e.d.Adj"} {
		t.Run(use, func(t *testing.T) {
			got := checkAdjSnippet(t, "package p\nfunc guarded(e *E) {\nif e.proj.Adj {\n"+use+"\n}\n}")
			if len(got) != 0 {
				t.Fatalf("findings = %q, want none", got)
			}
		})
	}
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
	t.Run("parenthesized negation", func(t *testing.T) {
		got := checkAdjSnippet(t, `package p
func parenthesized(e *E) {
	if !(e.proj.Adj) { return }
	e.d.Adj.Check(1)
}
`)
		if len(got) != 0 {
			t.Fatalf("findings = %q, want none", got)
		}
	})
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
