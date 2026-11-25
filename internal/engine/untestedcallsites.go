// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// untestedCallSitesLimit caps the functions the reviewer's Untested call sites
// section lists.
const untestedCallSitesLimit = 20

// untestedSite is one product call site: the file and the function around it.
type untestedSite struct {
	File string
	Func string
}

// untestedCollision names reached call-site functions the check cannot distinguish.
type untestedCollision struct {
	Name  string
	Decls []untestedSite
}

// untestedFinding is a new function that tests call directly while no test
// reaches the function around any of its product call sites.
type untestedFinding struct {
	Key   string // Recv.Name, or Name for a plain function
	File  string
	Sites []untestedSite
	Tests []string // the test files that call it directly
}

// ucsFunc is one function declaration with the names it calls.
type ucsFunc struct {
	file  string
	key   string
	name  string
	test  bool
	calls map[string]bool
}

// ucsParse parses src and lists its function declarations. It reports false
// when src does not parse.
func ucsParse(file, src string) ([]ucsFunc, bool) {
	f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	test := strings.HasSuffix(file, "_test.go")
	var funcs []ucsFunc
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		fn := ucsFunc{file: file, key: fd.Name.Name, name: fd.Name.Name, test: test, calls: map[string]bool{}}
		if fd.Recv != nil && len(fd.Recv.List) == 1 {
			if recv := ucsReceiver(fd.Recv.List[0].Type); recv != "" {
				fn.key = recv + "." + fd.Name.Name
			}
		}
		if fd.Body != nil {
			testingT := map[string]bool{}
			gather := func(params *ast.FieldList) {
				if params == nil {
					return
				}
				for _, field := range params.List {
					if ucsIsTestingT(field.Type) {
						for _, name := range field.Names {
							testingT[name.Name] = true
						}
					}
				}
			}
			gather(fd.Type.Params)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					gather(lit.Type.Params)
				}
				return true
			})
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					fn.calls[fun.Name] = true
				case *ast.SelectorExpr:
					if recv, ok := fun.X.(*ast.Ident); ok && testingT[recv.Name] {
						break
					}
					fn.calls[fun.Sel.Name] = true
				}
				return true
			})
		}
		funcs = append(funcs, fn)
	}
	return funcs, true
}

func ucsIsTestingT(expr ast.Expr) bool {
	ptr, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := ptr.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "T" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing"
}

// ucsReceiver is the base type name of a receiver type expression.
func ucsReceiver(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.Ident:
			return e.Name
		default:
			return ""
		}
	}
}

// untestedCallSites finds the new non-test functions that tests call directly
// while no test reaches the function around any product call site of them
// (S564). base maps each changed non-test Go path to its merge-base source, new
// files absent; tree maps every Go path of the worktree to its source. It goes
// by names, with no type information, and measures reach per function. It
// returns findings sorted by file then key, collisions sorted by name, and
// the paths it could not parse. Selector calls on *testing.T parameters do
// not count as calls.
func untestedCallSites(base, tree map[string]string, changed []string) ([]untestedFinding, []untestedCollision, []string) {
	unparsed := map[string]bool{}
	var funcs []ucsFunc
	for p, src := range tree {
		fns, ok := ucsParse(p, src)
		if !ok {
			unparsed[p] = true
			continue
		}
		funcs = append(funcs, fns...)
	}

	decls := map[string][]untestedSite{}
	for _, fn := range funcs {
		decls[fn.name] = append(decls[fn.name], untestedSite{File: fn.file, Func: fn.key})
	}
	baseKeys := map[string]bool{}
	for p, src := range base {
		fns, ok := ucsParse(p, src)
		if !ok {
			unparsed[p] = true
			continue
		}
		for _, fn := range fns {
			baseKeys[path.Dir(p)+"\x00"+fn.key] = true
		}
	}

	// The names a test reaches: what the test functions call, then what each
	// product function with a reached name calls, until nothing changes.
	reached := map[string]bool{}
	for _, fn := range funcs {
		if fn.test {
			for name := range fn.calls {
				reached[name] = true
			}
		}
	}
	for grew := true; grew; {
		grew = false
		for _, fn := range funcs {
			if fn.test || !reached[fn.name] {
				continue
			}
			for name := range fn.calls {
				if !reached[name] {
					reached[name] = true
					grew = true
				}
			}
		}
	}

	changedSet := map[string]bool{}
	for _, p := range changed {
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			changedSet[p] = true
		}
	}
	var findings []untestedFinding
	collisionNames := map[string]bool{}
	for _, fn := range funcs {
		if fn.test || !changedSet[fn.file] || unparsed[fn.file] {
			continue
		}
		if baseKeys[path.Dir(fn.file)+"\x00"+fn.key] {
			continue
		}
		var sites []untestedSite
		var tests []string
		callSiteReached := false
		var ambiguous []string
		for _, other := range funcs {
			if !other.calls[fn.name] {
				continue
			}
			if other.test {
				tests = append(tests, other.file)
				continue
			}
			if other.file == fn.file && other.key == fn.key {
				continue
			}
			sites = append(sites, untestedSite{File: other.file, Func: other.key})
			if reached[other.name] {
				callSiteReached = true
				if len(decls[other.name]) > 1 {
					ambiguous = append(ambiguous, other.name)
				}
			}
		}
		if len(tests) > 0 {
			for _, name := range ambiguous {
				collisionNames[name] = true
			}
		}
		if len(tests) == 0 || len(sites) == 0 || callSiteReached {
			continue
		}
		slices.Sort(tests)
		slices.SortFunc(sites, func(a, b untestedSite) int {
			if c := strings.Compare(a.File, b.File); c != 0 {
				return c
			}
			return strings.Compare(a.Func, b.Func)
		})
		findings = append(findings, untestedFinding{
			Key: fn.key, File: fn.file,
			Sites: slices.Compact(sites), Tests: slices.Compact(tests),
		})
	}
	slices.SortFunc(findings, func(a, b untestedFinding) int {
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	var bad []string
	for p := range unparsed {
		bad = append(bad, p)
	}
	slices.Sort(bad)
	var collisions []untestedCollision
	for name := range collisionNames {
		sites := decls[name]
		slices.SortFunc(sites, func(a, b untestedSite) int {
			if c := strings.Compare(a.File, b.File); c != 0 {
				return c
			}
			return strings.Compare(a.Func, b.Func)
		})
		collisions = append(collisions, untestedCollision{Name: name, Decls: sites})
	}
	slices.SortFunc(collisions, func(a, b untestedCollision) int { return strings.Compare(a.Name, b.Name) })
	return findings, collisions, bad
}

// untestedCallSitesSection tells the reviewer which new functions tests call
// only directly while no test reaches the function around their product call
// sites (S564, adj 573). Like the decision paths check, a failure is advisory:
// the section says the check is incomplete and never pauses the run. It returns
// nil when there are no findings, collisions or incomplete checks.
func (e *Engine) untestedCallSitesSection(ctx context.Context, rec runrec.Record, change branchChangeResult) *BriefSection {
	const title = "Untested call sites"
	mergeBase, changed, err := change.MergeBase, change.Paths, change.Err
	if err == nil && mergeBase == "" {
		err = errors.New("the merge base is empty")
	}
	if err != nil {
		return &BriefSection{Title: title, Body: "Check incomplete: the branch diff against the base could not be read. Trace the call sites of each new function yourself; this does not block the run."}
	}
	var goFiles []string
	for _, p := range changed {
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			goFiles = append(goFiles, p)
		}
	}
	slices.Sort(goFiles)
	goFiles = slices.Compact(goFiles)
	if len(goFiles) == 0 {
		return nil
	}

	var unread []string
	base := map[string]string{}
	var readable []string
	for _, p := range goFiles {
		if !filepath.IsLocal(p) || path.IsAbs(p) {
			unread = append(unread, p)
			continue
		}
		content, err := e.gitRead(ctx, rec.Worktree, "show", mergeBase+":"+p)
		switch {
		case err == nil:
			base[p] = content
		case notInBase(err):
		default:
			unread = append(unread, p)
			continue
		}
		readable = append(readable, p)
	}

	tree := map[string]string{}
	walkErr := filepath.WalkDir(rec.Worktree, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == rec.Worktree {
				return err
			}
			unread = append(unread, ucsRel(rec.Worktree, p))
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if p != rec.Worktree && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "vendor" || name == "testdata") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || !d.Type().IsRegular() {
			return nil
		}
		rel := ucsRel(rec.Worktree, p)
		src, err := os.ReadFile(p)
		if err != nil {
			unread = append(unread, rel)
			return nil
		}
		tree[rel] = string(src)
		return nil
	})
	if walkErr != nil {
		return &BriefSection{Title: title, Body: "Check incomplete: the worktree's Go files could not be read. Trace the call sites of each new function yourself; this does not block the run."}
	}

	findings, collisions, unparsed := untestedCallSites(base, tree, readable)
	if len(findings) == 0 && len(collisions) == 0 && len(unparsed) == 0 && len(unread) == 0 {
		return nil
	}

	var body strings.Builder
	if len(findings) > 0 {
		body.WriteString("Tests call these new functions directly, but no test reaches the function around any of their call sites. The check goes by function names, with no type information, so it is advisory.\n\n")
		listed := findings
		leftOut := 0
		if len(listed) > untestedCallSitesLimit {
			leftOut = len(listed) - untestedCallSitesLimit
			listed = listed[:untestedCallSitesLimit]
		}
		for _, f := range listed {
			var sites, tests []string
			for _, s := range f.Sites {
				sites = append(sites, "`"+s.File+"` in `"+s.Func+"`")
			}
			for _, tf := range f.Tests {
				tests = append(tests, "`"+tf+"`")
			}
			fmt.Fprintf(&body, "- `%s` in `%s`: called from %s; tests call it directly in %s.\n", f.Key, f.File, strings.Join(sites, ", "), strings.Join(tests, ", "))
		}
		if leftOut > 0 {
			noun := "functions were"
			if leftOut == 1 {
				noun = "function was"
			}
			fmt.Fprintf(&body, "\n%d more %s left out (ork lists at most %d). Trace the rest yourself.\n", leftOut, noun, untestedCallSitesLimit)
		}
	}
	if len(collisions) > 0 {
		var names []string
		for _, collision := range collisions {
			var decls []string
			for _, decl := range collision.Decls {
				decls = append(decls, "`"+decl.File+"` in `"+decl.Func+"`")
			}
			names = append(names, "`"+collision.Name+"`: "+strings.Join(decls, ", "))
		}
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("Names the check cannot tell apart: " + strings.Join(names, "; ") + ".\n")
	}
	var incomplete []string
	if len(unread) > 0 {
		incomplete = append(incomplete, "could not read "+ucsQuote(unread))
	}
	if len(unparsed) > 0 {
		incomplete = append(incomplete, "could not parse "+ucsQuote(unparsed))
	}
	if len(incomplete) > 0 {
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString("Check incomplete: ork " + strings.Join(incomplete, " and ") + ". Trace the call sites yourself; this does not block the run.\n")
	}
	return &BriefSection{Title: title, Body: strings.TrimRight(body.String(), "\n")}
}

// ucsRel is p relative to the worktree root, with forward slashes.
func ucsRel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}

// ucsQuote lists paths in backticks, sorted and without repeats.
func ucsQuote(paths []string) string {
	paths = slices.Clone(paths)
	slices.Sort(paths)
	paths = slices.Compact(paths)
	for i, p := range paths {
		paths[i] = "`" + p + "`"
	}
	return strings.Join(paths, ", ")
}
