// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

var (
	decisionEntryPath = regexp.MustCompile(`\blanded-changed/[A-Za-z0-9_-]+\.md\b`)
	decisionNumber    = regexp.MustCompile(`(?i)\b(?:adj|decision)\s+#?([0-9]+)\b`)
	decisionPathKey   = regexp.MustCompile("[\\[`]([a-z][a-z0-9_-]*(?:\\.[a-z][a-z0-9_-]*)+)[\\]`,]")
)

// ownerAnswerText keeps, for each line of an owner decision text, what follows
// the first "->" (the answer, not the question's title). A line without "->"
// is kept whole.
func ownerAnswerText(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if _, answer, ok := strings.Cut(line, "->"); ok {
			lines[i] = answer
		}
	}
	return strings.Join(lines, "\n")
}

// decisionPathsSection runs after implementation, before review. Like the
// direction digest, unavailable evidence is advisory and never pauses a run.
func (e *Engine) decisionPathsSection(ctx context.Context, rec runrec.Record, raw []byte) *BriefSection {
	var texts []string
	texts = append(texts, ownerAnswerText(rec.Decisions))
	for _, q := range rec.Questions {
		if q.Answer != "" {
			texts = append(texts, q.Answer)
		}
	}
	var unavailable []string
	var detail slicercli.Detail
	if json.Unmarshal(raw, &detail) != nil || detail.Slice == nil {
		unavailable = append(unavailable, "slice decisions could not be read")
	} else {
		var citationText strings.Builder
		citationText.WriteString(strings.Join(detail.Slice.Lead, "\n"))
		citationText.WriteString(detail.Slice.Boundary)
		for _, section := range detail.Slice.Sections {
			citationText.WriteString("\n" + section.Body)
			for _, paragraph := range ownerDecisionParagraphs(section.Body) {
				texts = append(texts, ownerAnswerText(paragraph))
			}
		}
		if e.proj.Adj {
			seen := map[int]bool{}
			for _, match := range decisionNumber.FindAllStringSubmatch(citationText.String(), -1) {
				id, _ := strconv.Atoi(match[1])
				if id <= 0 || seen[id] {
					continue
				}
				seen[id] = true
				r, err := e.d.Adj.Show(ctx, e.AdjRoot(), id)
				if err != nil {
					unavailable = append(unavailable, "adj "+match[1]+" could not be read")
				} else if r.Status == "decided" {
					texts = append(texts, r.Decision, r.Rationale)
				}
			}
			seenKeys := map[string]bool{}
			for _, match := range decisionPathKey.FindAllStringSubmatch(citationText.String(), -1) {
				key := match[1]
				if seenKeys[key] {
					continue
				}
				seenKeys[key] = true
				state, r, err := e.d.Adj.CheckRecord(ctx, e.wr, e.AdjRoot(), key)
				if err != nil {
					unavailable = append(unavailable, "adj key "+key+" could not be read")
				} else if state == "decided" && r != nil {
					texts = append(texts, r.Decision, r.Rationale)
				}
			}
		}
	}
	paths := decisionEntryPath.FindAllString(strings.Join(texts, "\n"), -1)
	slices.Sort(paths)
	paths = slices.Compact(paths)
	var missing []string
	if len(paths) > 0 {
		changed, err := e.branchPaths(ctx, rec)
		if err != nil {
			unavailable = append(unavailable, "branch diff against the base could not be read")
		} else {
			for _, path := range paths {
				if !slices.Contains(changed, path) {
					missing = append(missing, "`"+path+"`")
				}
			}
		}
	}
	if len(missing) == 0 && len(unavailable) == 0 {
		return nil
	}
	var body string
	if len(missing) > 0 {
		body = "Flagged finding: owner decisions name paths absent from the branch diff against the base:\n\n- " + strings.Join(missing, "\n- ") + "\n\nReview these omissions. This check is advisory and does not block the run."
	}
	if len(unavailable) > 0 {
		if body != "" {
			body += "\n\n"
		}
		body += "Check incomplete: " + strings.Join(unavailable, "; ") + ". Review the decision paths yourself; this does not block the run."
	}
	return &BriefSection{Title: "Decision changelog paths", Body: body}
}
