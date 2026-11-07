// SPDX-License-Identifier: GPL-3.0-or-later

// Package projscan finds the projects below a folder (S302, *proposed*). The
// TUI checklist and ork projects add --scan share it.
//
// A project is a folder that holds .slicer, .adjudicate.json or .git. The
// walk lists the folder itself when it is a project. Otherwise it looks one
// and two levels below it, stops at the first project on each branch, and
// skips hidden folders (which covers .worktrees) and node_modules. It does
// not follow symbolic links.
package projscan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/squalor-xyz/ork/internal/registry"
)

// MaxDepth is how many levels below the folder the walk looks.
const MaxDepth = 2

// The scan is bounded: it quits after it has looked at MaxFolders folders, or
// has found MaxCandidates projects, and says so in Result.Stopped.
const (
	MaxFolders    = 500
	MaxCandidates = 200
)

// Candidate is a folder that holds a project marker. Name is the folder name,
// which is the project name ork registers it under.
type Candidate struct {
	Path, Name       string
	Slicer, Adj, Git bool
}

// Tracked reports a slicer or adj marker. Tracked candidates start ticked.
func (c Candidate) Tracked() bool { return c.Slicer || c.Adj }

// Tag is what the folder holds: slicer, adj or both ("slicer+adj"), and "git"
// only for a plain git repo.
func (c Candidate) Tag() string {
	var tags []string
	if c.Slicer {
		tags = append(tags, "slicer")
	}
	if c.Adj {
		tags = append(tags, "adj")
	}
	if len(tags) == 0 && c.Git {
		tags = append(tags, "git")
	}
	return strings.Join(tags, "+")
}

func inspect(path string) (Candidate, bool) {
	c := Candidate{Path: path, Name: filepath.Base(path)}
	if info, err := os.Stat(filepath.Join(path, ".slicer")); err == nil && info.IsDir() {
		c.Slicer = true
	}
	if info, err := os.Stat(filepath.Join(path, ".adjudicate.json")); err == nil && info.Mode().IsRegular() {
		c.Adj = true
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil { // a directory, or a file in a linked worktree
		c.Git = true
	}
	return c, c.Slicer || c.Adj || c.Git
}

// Result is what a scan found. Folders is how many folders it looked at, and
// Stopped is set when it quit at a limit before it had looked at them all.
type Result struct {
	Candidates []Candidate
	Folders    int
	Stopped    bool
}

// Note says where a stopped scan quit, such as "stopped after 500 folders and
// 3 projects". It is empty for a scan that looked at everything.
func (r Result) Note() string {
	if !r.Stopped {
		return ""
	}
	word := "projects"
	if len(r.Candidates) == 1 {
		word = "project"
	}
	return fmt.Sprintf("stopped after %d folders and %d %s", r.Folders, len(r.Candidates), word)
}

// Scan lists the projects at and below root, sorted by path. It returns an
// error when root is not a readable folder.
func Scan(root string) (Result, error) { return ScanLimit(root, MaxFolders, MaxCandidates) }

// ScanLimit is Scan with its own limits.
func ScanLimit(root string, maxFolders, maxCandidates int) (Result, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("path %q is not a directory", root)
	}
	res := Result{Folders: 1}
	if c, ok := inspect(root); ok {
		res.Candidates = []Candidate{c}
		return res, nil
	}
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || strings.HasPrefix(name, ".") || name == "node_modules" {
				continue
			}
			// Once a limit is hit, no folder is looked at and the flag stays set.
			if res.Stopped {
				return nil
			}
			if res.Folders >= maxFolders {
				res.Stopped = true
				return nil
			}
			res.Folders++
			path := filepath.Join(dir, name)
			if c, ok := inspect(path); ok {
				if len(res.Candidates) >= maxCandidates {
					res.Stopped = true
					return nil
				}
				res.Candidates = append(res.Candidates, c)
			} else if depth < MaxDepth {
				// An unreadable folder below the root is skipped.
				_ = walk(path, depth+1)
			}
		}
		return nil
	}
	if err := walk(root, 1); err != nil {
		return Result{}, err
	}
	slices.SortFunc(res.Candidates, func(a, b Candidate) int { return strings.Compare(a.Path, b.Path) })
	return res, nil
}

// Row is a candidate set against the registry. Reason is empty for a row that
// can be ticked. A row whose path is registered has Registered set and is
// shown greyed. A row whose name another registered project holds is blocked
// with the reason.
type Row struct {
	Candidate
	Registered bool
	Reason     string
}

// Blocked reports a row that cannot be ticked.
func (r Row) Blocked() bool { return r.Reason != "" }

// Preselected reports a row that starts ticked: tracked and not blocked.
func (r Row) Preselected() bool { return r.Tracked() && !r.Blocked() }

// canonical is a path with symbolic links resolved, to compare two spellings
// of one folder. A path that cannot be resolved (it is gone) is cleaned only.
func canonical(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// Classify sets each candidate against the registered projects, and against
// the other candidates: of free rows with one name, one claims it and the rest
// are blocked, because the daemon would refuse their name. Tracked rows
// (slicer or adj) claim before plain git repos, which the command line does not
// add; rows of one kind go by path.
func Classify(cands []Candidate, registered []registry.Project) []Row {
	regPaths := make([]string, len(registered))
	for i, p := range registered {
		regPaths[i] = canonical(p.Path)
	}
	rows := make([]Row, 0, len(cands))
	for _, c := range cands {
		r := Row{Candidate: c}
		path := canonical(c.Path)
		for i, p := range registered {
			if regPaths[i] == path {
				r.Registered = true
				r.Reason = fmt.Sprintf("already registered as %s", p.Name)
				break
			}
		}
		if !r.Registered {
			for _, p := range registered {
				if p.Name == c.Name {
					r.Reason = fmt.Sprintf("name %q is registered for %s", c.Name, p.Path)
					break
				}
			}
		}
		rows = append(rows, r)
	}
	var order []int
	for i, r := range rows {
		if r.Reason == "" {
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if ta, tb := rows[a].Tracked(), rows[b].Tracked(); ta != tb {
			if ta {
				return -1
			}
			return 1
		}
		return strings.Compare(rows[a].Path, rows[b].Path)
	})
	claimed := map[string]string{} // name -> path of the row that holds it
	for _, i := range order {
		if first, dup := claimed[rows[i].Name]; dup {
			rows[i].Reason = fmt.Sprintf("name %q is also found at %s; add it with ork projects add PATH --name N", rows[i].Name, first)
		} else {
			claimed[rows[i].Name] = rows[i].Path
		}
	}
	return rows
}

// Outcome is what adding one candidate did.
type Outcome struct {
	Candidate
	Project registry.Project
	Err     error
}

// AddAll adds each candidate with add, one at a time and in order. A failure
// is kept in its outcome and does not stop the rest. When ctx ends, each
// remaining outcome carries the context's error.
func AddAll(ctx context.Context, cands []Candidate, add func(context.Context, Candidate) (registry.Project, error)) []Outcome {
	out := make([]Outcome, 0, len(cands))
	for _, c := range cands {
		o := Outcome{Candidate: c}
		if err := ctx.Err(); err != nil {
			o.Err = err
		} else {
			o.Project, o.Err = add(ctx, c)
		}
		out = append(out, o)
	}
	return out
}
