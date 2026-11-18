// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// The ork tracking commit (S405, *proposed*; adj 453, adj 461): one commit on
// the base branch of the main checkout that holds the project's uncommitted
// tracking files, on the owner's or the orchestrator's explicit command. The
// files are slicer's (trackingPath) and the adj store's record files. It
// reuses the by-name commit of commitTracking.

// GitFunc runs `git -C dir ARGV...` and returns what it printed. The run-bound
// Engine.gitWrite is one, so a run logs its writes; PlainGit is the other.
type GitFunc func(ctx context.Context, dir string, argv ...string) (clirun.Result, error)

// PlainGit runs git with no run to log on.
func PlainGit(d Deps) GitFunc {
	return func(ctx context.Context, dir string, argv ...string) (clirun.Result, error) {
		return clirun.Run(ctx, clirun.Call{Bin: d.gitBin(), Args: append([]string{"-C", dir}, argv...), Timeout: 2 * time.Minute})
	}
}

// The codes of a TrackingError. They are the daemon's codes of the same names.
const (
	TrackingRefused     = "refused"
	TrackingNothingToDo = "nothing_to_commit"
	TrackingBadParams   = "bad_params"
)

// TrackingError is a refusal of CommitTrackingFiles before it commits.
type TrackingError struct {
	Code    string
	Message string
}

func (e *TrackingError) Error() string { return e.Message }

// TrackingCommit is the commit CommitTrackingFiles made. Files are relative to
// the project.
type TrackingCommit struct {
	Hash    string
	Subject string
	Files   []string
}

// adjStore is the project's adj store folder, relative to the project and
// slash-separated, read from .adjudicate.json. It is empty when the file is
// absent, names no store, or names one outside the project.
func adjStore(P string) (string, error) {
	b, err := os.ReadFile(filepath.Join(P, ".adjudicate.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read .adjudicate.json: %w", err)
	}
	var cfg struct {
		Store string `json:"store"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return "", fmt.Errorf(".adjudicate.json: %w", err)
	}
	store := path.Clean(filepath.ToSlash(cfg.Store))
	if cfg.Store == "" || store == "." || path.IsAbs(store) || store == ".." || strings.HasPrefix(store, "../") {
		return "", nil
	}
	return store, nil
}

// commitPath reports whether rel (relative to the project) is a file the
// tracking commit takes: one of trackingPath's, or a file in the adj store.
func commitPath(store, rel string) bool {
	return trackingPath(rel) || (store != "" && strings.HasPrefix(rel, store+"/"))
}

// statusEntry is one record of `git status --porcelain=v1 -z`.
type statusEntry struct {
	x      byte
	path   string
	source string // the old name of a rename or copy
}

// parseStatusZ reads status v1 with -z, keeping each record's index column.
func parseStatusZ(out string) []statusEntry {
	records := strings.Split(out, "\x00")
	var entries []statusEntry
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 || record[2] != ' ' || record[:2] == "!!" {
			continue
		}
		entry := statusEntry{x: record[0], path: record[3:]}
		if strings.ContainsAny(record[:2], "RC") && i+1 < len(records) {
			i++
			entry.source = records[i]
		}
		entries = append(entries, entry)
	}
	return entries
}

// staged reports whether the index holds a change to the entry's path.
func (e statusEntry) staged() bool { return e.x != ' ' && e.x != '?' && e.x != '!' }

// CommitTrackingFiles commits the project's uncommitted tracking files on the
// base branch, by literal path, and leaves every other change as it is. It
// refuses, and commits nothing, when the checkout is not on base, when a staged
// path is outside the files it would commit, or when there are none. leave
// names project-relative tracking paths to keep out of the commit.
func CommitTrackingFiles(ctx context.Context, git GitFunc, P, base string, leave []string) (TrackingCommit, error) {
	res, err := git(ctx, P, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		var exit *clirun.ExitError
		if errors.As(err, &exit) && exit.Result.ExitCode == 1 {
			return TrackingCommit{}, &TrackingError{Code: TrackingRefused, Message: "the main checkout is not on a branch, not the base branch " + base}
		}
		return TrackingCommit{}, fmt.Errorf("git symbolic-ref: %w", err)
	}
	if branch := strings.TrimSpace(string(res.Stdout)); branch != base {
		return TrackingCommit{}, &TrackingError{Code: TrackingRefused, Message: runrec.PlainText(oneLine("the main checkout is on " + branch + ", not the base branch " + base))}
	}
	store, err := adjStore(P)
	if err != nil {
		return TrackingCommit{}, err
	}
	left := map[string]bool{}
	for _, l := range leave {
		clean := path.Clean(filepath.ToSlash(l))
		if !commitPath(store, clean) {
			return TrackingCommit{}, &TrackingError{Code: TrackingBadParams, Message: runrec.PlainText(oneLine("--leave " + l + " is not a tracking path"))}
		}
		left[clean] = true
	}
	res, err = git(ctx, P, "rev-parse", "--show-prefix")
	if err != nil {
		return TrackingCommit{}, fmt.Errorf("git rev-parse: %w", err)
	}
	prefix := strings.TrimSpace(string(res.Stdout))
	res, err = git(ctx, P, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return TrackingCommit{}, fmt.Errorf("git status: %w", err)
	}
	entries := parseStatusZ(string(res.Stdout))
	selected := map[string]bool{}
	for _, en := range entries {
		for _, name := range []string{en.path, en.source} {
			if rel, ok := strings.CutPrefix(name, prefix); ok && name != "" && commitPath(store, rel) && !left[rel] {
				selected[rel] = true
			}
		}
	}
	var outside []string
	seen := map[string]bool{}
	for _, en := range entries {
		if !en.staged() {
			continue
		}
		for _, name := range []string{en.path, en.source} {
			if name == "" || seen[name] {
				continue
			}
			if rel, ok := strings.CutPrefix(name, prefix); !ok || !selected[rel] {
				seen[name] = true
				outside = append(outside, name)
			}
		}
	}
	if len(outside) > 0 {
		sort.Strings(outside)
		return TrackingCommit{}, &TrackingError{Code: TrackingRefused, Message: runrec.PlainText(oneLine("other paths are staged: " + nameList(outside) + "; commit or unstage them first"))}
	}
	if len(selected) == 0 {
		return TrackingCommit{}, &TrackingError{Code: TrackingNothingToDo, Message: "no uncommitted tracking files"}
	}
	files := make([]string, 0, len(selected))
	for rel := range selected {
		files = append(files, rel)
	}
	sort.Strings(files)
	subject := trackingMessage(base, store, files)
	if reason := commitLiteralPaths(ctx, git, P, files, subject); reason != "" {
		return TrackingCommit{}, errors.New(reason)
	}
	res, err = git(ctx, P, "rev-parse", "--short", "HEAD")
	if err != nil {
		return TrackingCommit{}, fmt.Errorf("git rev-parse: %w", err)
	}
	return TrackingCommit{Hash: strings.TrimSpace(string(res.Stdout)), Subject: subject, Files: files}, nil
}

// numberedName is a name that may end in a plain number: a slice ID such as
// S387 (prefix S, n 387) or a record number (no prefix). ok is false for a name
// that does not, or whose digits have a leading zero, so it never folds.
type numberedName struct {
	text, prefix string
	n            int
	ok           bool
}

// splitNumbered reads the trailing number of id.
func splitNumbered(id string) numberedName {
	i := len(id)
	for i > 0 && id[i-1] >= '0' && id[i-1] <= '9' {
		i--
	}
	n, err := strconv.Atoi(id[i:])
	if err != nil || strconv.Itoa(n) != id[i:] {
		return numberedName{text: id, prefix: id}
	}
	return numberedName{text: id, prefix: id[:i], n: n, ok: true}
}

// foldRuns joins names with ", ", in order by prefix and number, and writes
// each run of two or more consecutive numbers with one prefix as FIRST-LAST
// (adj 462, *proposed*: S387-S389, 402-405).
func foldRuns(names []numberedName) string {
	sort.Slice(names, func(i, j int) bool {
		a, b := names[i], names[j]
		switch {
		case a.prefix != b.prefix:
			return a.prefix < b.prefix
		case a.n != b.n:
			return a.n < b.n
		}
		return a.text < b.text
	})
	var out []string
	for i := 0; i < len(names); {
		j := i
		for names[i].ok && j+1 < len(names) && names[j+1].ok && names[j+1].prefix == names[i].prefix && names[j+1].n == names[j].n+1 {
			j++
		}
		if j > i {
			out = append(out, names[i].text+"-"+names[j].text)
		} else {
			out = append(out, names[i].text)
		}
		i = j + 1
	}
	return strings.Join(out, ", ")
}

// trackingMessage is the subject of a tracking commit, built from the paths:
// the slice IDs of `.slicer/slices/ID.json` and the record numbers of
// `STORE/NNNN-*.md`, each run of consecutive numbers folded into a range. It
// names neither when the paths hold none.
func trackingMessage(base, store string, paths []string) string {
	var ids []string
	var numbers []int
	for _, p := range paths {
		if id, ok := strings.CutPrefix(p, ".slicer/slices/"); ok {
			if id, ok = strings.CutSuffix(id, ".json"); ok && id != "" && !strings.Contains(id, "/") {
				ids = append(ids, id)
			}
			continue
		}
		if store == "" {
			continue
		}
		name, ok := strings.CutPrefix(p, store+"/")
		if !ok || strings.Contains(name, "/") || !strings.HasSuffix(name, ".md") {
			continue
		}
		digits, _, found := strings.Cut(name, "-")
		if n, err := strconv.Atoi(digits); found && err == nil && n > 0 {
			numbers = append(numbers, n)
		}
	}
	sort.Ints(numbers)
	var records []numberedName
	for i, n := range numbers {
		if i == 0 || n != numbers[i-1] {
			records = append(records, numberedName{text: strconv.Itoa(n), n: n, ok: true})
		}
	}
	var sliceNames []numberedName
	for _, id := range ids {
		sliceNames = append(sliceNames, splitNumbered(id))
	}
	var parts []string
	if len(sliceNames) > 0 {
		parts = append(parts, foldRuns(sliceNames))
	}
	if len(records) > 0 {
		parts = append(parts, "adj "+foldRuns(records))
	}
	msg := "Tracking edits on " + base
	if len(parts) > 0 {
		msg += ": " + strings.Join(parts, "; ")
	}
	return msg
}
