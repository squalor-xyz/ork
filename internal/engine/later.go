// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// questionText is the raw text of a finding's question, in the shape of an
// Owner needed block.
func questionText(ask *OwnerQuestion) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\nOptions:\n", ask.Title)
	for i, o := range ask.Options {
		fmt.Fprintf(&b, "%d. %s: %s\n", i+1, o.Label, o.Summary)
	}
	fmt.Fprintf(&b, "Recommend: %d - %s", ask.Recommend, ask.Reason)
	return b.String()
}

func findingKey(kind, title string) string {
	return kind + "\x00" + strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// recordFindings keeps the owner findings that do not block and the later
// findings of a report in the run record, once each: a finding that a later
// round raises again under the same title is not added twice. A fix finding
// goes to the fix round, and a blocker is asked by askBlocker.
func (e *Engine) recordFindings(r round, t *Triaged) error {
	var add []runrec.Finding
	for _, f := range t.Owner {
		if !f.Blocker {
			add = append(add, runrec.Finding{Kind: runrec.FindingOwner, Title: f.Title, Text: f.Text, Ask: f.Ask, Key: f.Key})
		}
	}
	for _, f := range t.Later {
		add = append(add, runrec.Finding{Kind: runrec.FindingLater, Title: f.Title, Text: f.Text, Scores: f.Scores})
	}
	if len(add) == 0 {
		return nil
	}
	return e.run.Update(func(rr *runrec.Record) {
		seen := map[string]bool{}
		for _, f := range rr.Findings {
			seen[findingKey(f.Kind, f.Title)] = true
		}
		for _, f := range add {
			if k := findingKey(f.Kind, f.Title); !seen[k] {
				seen[k] = true
				f.Role, f.Round, f.At = r.role, r.n, e.d.now()
				rr.Findings = append(rr.Findings, f)
			}
		}
	})
}

// askBlocker pauses the slice on an owner finding that its report marks as a
// blocker, as a question with the finding's structured shape. After the
// answer the role runs again with it.
func (e *Engine) askBlocker(r round, f Finding) error {
	text := fmt.Sprintf("The %s's report marks this owner finding as a blocker.\n%s", r.role, questionText(f.Ask))
	err := e.run.Update(func(rr *runrec.Record) {
		rr.Questions = append(rr.Questions, runrec.Question{Ask: f.Ask, Role: r.role, Round: r.n, Text: text, Key: f.Key, At: e.d.now()})
	})
	if err != nil {
		return err
	}
	return e.pause(runrec.Pending{
		Kind: runrec.PendingQuestion, State: r.state, Role: r.role, Round: r.n,
		Reason:   fmt.Sprintf("%s round %d has an owner finding that blocks", r.role, r.n),
		Question: text, Ask: f.Ask, Key: f.Key,
	})
}

// FileLater files the later finding n (one-based, in the record's findings)
// as a slice, after the owner confirmed it: `slicer add TITLE
// --discovered-from ITEM` in the project, then a Why section from the finding
// (`slicer promote`). slicer owns the new record; ork only runs the two calls
// and keeps the new id on the finding. The add key makes a retry return the
// slice that the first call filed. It does not wait for the run: a role may
// be running.
func (e *Engine) FileLater(ctx context.Context, n int) (string, error) {
	e.fileMu.Lock()
	defer e.fileMu.Unlock()
	rec := e.run.Snapshot()
	if n < 1 || n > len(rec.Findings) {
		return "", fmt.Errorf("run %s has no finding %d", rec.ID, n)
	}
	f := rec.Findings[n-1]
	if f.Kind != runrec.FindingLater {
		return "", fmt.Errorf("finding %d is an %s finding; only a later finding is filed as a slice", n, f.Kind)
	}
	if f.Filed != "" {
		return "", fmt.Errorf("finding %d is already filed as %s", n, f.Filed)
	}
	added, err := e.d.Slicer.Add(ctx, e.wr, e.proj.Path, f.Title, rec.Item, rec.ID+"-later-"+strconv.Itoa(n))
	if err != nil {
		return "", fmt.Errorf("slicer add: %w", err)
	}
	if added.Existing && added.HasSlice {
		// An earlier call already wrote the Why section; promote would refuse it.
		return e.scoreAndMarkFiled(ctx, n, added.ID, f.Scores)
	}
	outline := fmt.Sprintf("## %s\n\n### Why\n\nRaised by the %s in round %d of ork run %s for %s (discovered from %s):\n\n%s\n",
		f.Title, f.Role, f.Round, rec.ID, rec.Item, rec.Item, f.Text)
	path := filepath.Join(e.run.Dir(), fmt.Sprintf("later-%d.md", n))
	if err := os.WriteFile(path, []byte(outline), 0o600); err != nil {
		return "", fmt.Errorf("outline: %w", err)
	}
	if err := e.d.Slicer.Promote(ctx, e.wr, e.proj.Path, added.ID, path); err != nil {
		return "", fmt.Errorf("filed %s, but its Why section failed (filing again retries it): slicer promote: %w", added.ID, err)
	}
	return e.scoreAndMarkFiled(ctx, n, added.ID, f.Scores)
}

// markFiled keeps the new slice's id on finding n.
func (e *Engine) markFiled(n int, id string) (string, error) {
	err := e.run.Update(func(rr *runrec.Record) {
		if n <= len(rr.Findings) {
			rr.Findings[n-1].Filed = id
			rr.Findings[n-1].FiledAt = e.d.now()
		}
	})
	return id, err
}

// scoreAndMarkFiled applies a role's proposal only after the slice has its Why
// section. Failed scoring keeps the offer retryable with the same filing key.
func (e *Engine) scoreAndMarkFiled(ctx context.Context, n int, id string, scores *runrec.FindingScores) (string, error) {
	if scores != nil {
		if err := e.d.Slicer.SetScores(ctx, e.wr, e.proj.Path, id, *scores); err != nil {
			return "", fmt.Errorf("filed %s, but proposed scores failed (filing again retries them): %w", id, err)
		}
	}
	return e.markFiled(n, id)
}

// AnswerFinding records a non-blocking finding's answer without advancing or
// pausing the run. Filing and answering share the record's action mutex.
func (e *Engine) AnswerFinding(ctx context.Context, n int, text, by string) (string, error) {
	e.fileMu.Lock()
	defer e.fileMu.Unlock()
	rec := e.Snapshot()
	if n < 1 || n > len(rec.Findings) {
		return "", fmt.Errorf("run %s has no finding %d", rec.ID, n)
	}
	f := rec.Findings[n-1]
	if f.Kind != runrec.FindingOwner || f.Ask == nil {
		return "", fmt.Errorf("finding %d is not an owner question", n)
	}
	if f.Answer != "" {
		return "", fmt.Errorf("finding %d is already answered", n)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("the answer is empty")
	}
	message := "answer recorded"
	if f.Key != "" && e.proj.Adj {
		if strings.TrimSpace(by) == "" {
			return "", fmt.Errorf("ADJ_DECIDED_BY is unset; set it in the caller before filing an answer in adj")
		}
		var err error
		message, err = fileDecision(ctx, e.d, e.wr, e.AdjRoot(), f.Key, f.Title, rec.Item, questionText(f.Ask), text, by)
		if err != nil {
			return "", err
		}
	}
	err := e.run.Update(func(r *runrec.Record) {
		r.Findings[n-1].Answer = text
		r.Findings[n-1].AnsweredAt = e.d.now()
	})
	return message, err
}
