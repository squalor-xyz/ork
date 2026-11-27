// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

var decisionCitation = regexp.MustCompile("(?i)\\b(?:owner\\s+[0-9]{4}-[0-9]{2}-[0-9]{2}|adj(?:\\s+[0-9]+)?)\\b\\s*[:,]?\\s*[`\\[(]?(?-i:([a-z][a-z0-9_-]*(?:\\.[a-z][a-z0-9_-]*)+))\\b")
var specOpenWord = regexp.MustCompile(`(?i)\b(?:open|undecided|not\s+decided)\b`)

type decisionKeyResult struct {
	state  string
	record *adjcli.Record
	err    error
}

// Each role round shares a cache between the two advisory sections. It also
// retains failed checks so unavailable evidence is not repeatedly requested.
func (e *Engine) decisionKeyChecker(ctx context.Context) func(string) decisionKeyResult {
	cache := map[string]decisionKeyResult{}
	return func(key string) decisionKeyResult {
		if !e.proj.Adj {
			return decisionKeyResult{}
		}
		if r, ok := cache[key]; ok {
			return r
		}
		state, record, err := e.d.Adj.CheckRecord(ctx, e.wr, e.AdjRoot(), key)
		r := decisionKeyResult{state, record, err}
		cache[key] = r
		return r
	}
}

// Go test files hold citation-shaped fixtures, not owner-decision citations
// (S652, narrows review.decision-key-citation).
func skipDecisionKeyPath(path string) bool {
	if strings.HasSuffix(path, "_test.go") {
		return true
	}
	for _, prefix := range []string{".slicer/", "decisions/", "landed-changed/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// decisionDiffLines keeps hunk additions and the paths with new-side headers.
// Header state matters: an added source line itself may start with "+++".
func decisionDiffLines(diff string) ([]string, map[string]bool) {
	var lines []string
	paths := map[string]bool{}
	path := ""
	hunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			path = ""
			hunk = false
		case !hunk && strings.HasPrefix(line, "+++ "):
			path = strings.TrimPrefix(line, "+++ ")
			if strings.HasPrefix(path, "\"") {
				if p, err := strconv.Unquote(path); err == nil {
					path = p
				}
			}
			path = strings.TrimPrefix(path, "b/")
			paths[path] = true
		case strings.HasPrefix(line, "@@"):
			hunk = true
		case hunk && path != "" && !skipDecisionKeyPath(path) && strings.HasPrefix(line, "+"):
			lines = append(lines, line[1:])
		}
	}
	return lines, paths
}

func citedDecisionKeys(lines []string) []string {
	var keys []string
	for _, line := range lines {
		for _, m := range decisionCitation.FindAllStringSubmatch(line, -1) {
			keys = append(keys, m[1])
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func diffCitedKeys(diff string) []string {
	lines, _ := decisionDiffLines(diff)
	return citedDecisionKeys(lines)
}

func specOpenLines(text string) []string {
	var keys []string
	for _, line := range strings.Split(text, "\n") {
		if !specOpenWord.MatchString(decisionPathKey.ReplaceAllString(line, "")) {
			continue
		}
		for _, m := range decisionPathKey.FindAllStringSubmatch(line, -1) {
			keys = append(keys, m[1])
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func decisionKeysSection(title string, hits, unavailable []string) *BriefSection {
	if len(hits) == 0 && len(unavailable) == 0 {
		return nil
	}
	body := strings.Join(hits, "\n")
	for _, item := range unavailable {
		if body != "" {
			body += "\n\n"
		}
		body += "Check incomplete: " + item + "."
	}
	body += "\n\nThis check is advisory and does not block the run."
	return &BriefSection{Title: title, Body: body}
}

// readDecisionKeyFile bounds reads even when a file grows after Stat. Lstat
// excludes symlinks and special files before opening them.
func readDecisionKeyFile(path string) (string, error) {
	const limit = 1 << 20
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit || strings.ContainsRune(string(data), 0) {
		return "", nil
	}
	return string(data), nil
}

func (e *Engine) unfoundDecisionKeysSection(ctx context.Context, rec runrec.Record, change branchChangeResult, check func(string) decisionKeyResult) *BriefSection {
	if !e.proj.Adj {
		return nil
	}
	const title = "Decision keys not found"
	if change.Err != nil || change.MergeBase == "" {
		return decisionKeysSection(title, nil, []string{"the branch diff against the base could not be read"})
	}
	diff, err := e.gitRead(ctx, rec.Worktree, "diff", "--no-color", "--no-ext-diff", "--no-renames", "--unified=0", change.MergeBase)
	if err != nil {
		return decisionKeysSection(title, nil, []string{"the branch diff against the base could not be read"})
	}
	lines, paths := decisionDiffLines(diff)
	var unavailable []string
	for _, path := range change.Paths {
		if paths[path] || skipDecisionKeyPath(path) {
			continue
		}
		if !filepath.IsLocal(path) {
			unavailable = append(unavailable, path+" could not be read")
			continue
		}
		src, err := readDecisionKeyFile(filepath.Join(rec.Worktree, filepath.FromSlash(path)))
		if err != nil {
			unavailable = append(unavailable, path+" could not be read")
			continue
		}
		lines = append(lines, strings.Split(src, "\n")...)
	}
	var hits []string
	for _, key := range citedDecisionKeys(lines) {
		r := check(key)
		if r.err != nil {
			unavailable = append(unavailable, "adj check "+key+" could not be read")
		} else if r.state == "not found" {
			hits = append(hits, "- `"+key+"`")
		}
	}
	if len(hits) > 0 {
		hits = append([]string{"These keys are cited as owner decisions but are not in adj. Check that each decision exists.\n"}, hits...)
	}
	return decisionKeysSection(title, hits, unavailable)
}

func (e *Engine) specOpenKeysSection(ctx context.Context, raw []byte, check func(string) decisionKeyResult) *BriefSection {
	if !e.proj.Adj {
		return nil
	}
	const title = "Spec text calls a decided key open"
	var detail slicercli.Detail
	if json.Unmarshal(raw, &detail) != nil || detail.Slice == nil {
		return decisionKeysSection(title, nil, []string{"slice spec could not be read"})
	}
	sections := []slicercli.Section{{Heading: "Lead", Body: strings.Join(detail.Slice.Lead, "\n")}, {Heading: "Boundary", Body: detail.Slice.Boundary}}
	sections = append(sections, detail.Slice.Sections...)
	var hits, unavailable []string
	failed := map[string]bool{}
	for _, section := range sections {
		for _, line := range strings.Split(section.Body, "\n") {
			for _, key := range specOpenLines(line) {
				r := check(key)
				if r.err != nil {
					if !failed[key] {
						unavailable = append(unavailable, "adj check "+key+" could not be read")
						failed[key] = true
					}
				} else if r.state == "decided" && r.record != nil {
					hits = append(hits, fmt.Sprintf("- %s: %s (key `%s`, adj %d)", section.Heading, line, key, r.record.ID))
				}
			}
		}
	}
	return decisionKeysSection(title, hits, unavailable)
}
