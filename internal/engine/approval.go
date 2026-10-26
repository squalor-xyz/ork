// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// humanCheckLine matches a [HUMAN CHECK] item: a bullet or numbered line of
// any section of the slice, as the slices' Check sections write them.
var humanCheckLine = regexp.MustCompile(`(?i)^\s*(?:[-*+]\s+|\d+[.)]\s+)?\[HUMAN CHECK\]\s*(\S.*?)\s*$`)

// humanCheckTexts lists the [HUMAN CHECK] items of a slice, in order.
func humanCheckTexts(sections []slicercli.Section) []string {
	var out []string
	for _, s := range sections {
		for _, line := range strings.Split(s.Body, "\n") {
			if m := humanCheckLine.FindStringSubmatch(line); m != nil {
				out = append(out, m[1])
			}
		}
	}
	return out
}

// buildCard collects what the owner needs to approve: the gate's verdict and
// test counts, the diff stat of the worktree against HEAD (ork commits only
// after approval), and the slice's human checks. Marks the owner already made
// on a check with the same text carry over. A diff stat that cannot be read
// is left empty; human checks that cannot be read are named on the card.
func (e *Engine) buildCard(ctx context.Context, rec runrec.Record, rep *Report) *runrec.ApprovalCard {
	card := &runrec.ApprovalCard{Round: rec.Round, Verdict: rep.Verdict, Passed: rep.Passed, Failed: rep.Failed}
	if out, err := e.gitRead(ctx, rec.Worktree, "diff", "--stat", "HEAD"); err == nil {
		card.DiffStat = strings.TrimSpace(out)
	}
	if out, err := e.gitRead(ctx, rec.Worktree, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, name := range strings.Split(out, "\n") {
			if name = strings.TrimSpace(name); name != "" {
				card.Untracked = append(card.Untracked, name)
			}
		}
	}
	marks := map[string]string{}
	if rec.Card != nil {
		for _, c := range rec.Card.HumanChecks {
			marks[c.Text] = c.State
		}
	}
	raw, err := e.d.Slicer.ShowRaw(ctx, rec.Worktree, rec.Item)
	var detail slicercli.Detail
	if err == nil {
		err = json.Unmarshal(raw, &detail)
	}
	if err != nil {
		card.ChecksError = err.Error()
		return card
	}
	if detail.Slice != nil {
		for _, text := range humanCheckTexts(detail.Slice.Sections) {
			card.HumanChecks = append(card.HumanChecks, runrec.HumanCheck{Text: text, State: marks[text]})
		}
	}
	return card
}

// openChecks lists the human checks the owner has neither ticked nor marked
// later, as "N. text".
func openChecks(card *runrec.ApprovalCard) []string {
	if card == nil {
		return nil
	}
	var open []string
	for i, c := range card.HumanChecks {
		if !c.Resolved() {
			open = append(open, fmt.Sprintf("%d. %s", i+1, c.Text))
		}
	}
	return open
}

// checksNote is the slicer note that records the owner's human checks.
func checksNote(item string, card *runrec.ApprovalCard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Human checks at approval (%s):", item)
	for _, c := range card.HumanChecks {
		fmt.Fprintf(&b, "\n- %s: %s", c.State, c.Text)
	}
	return b.String()
}

// MarkCheck sets the owner's mark on the n-th (one-based) human check of the
// approval card: runrec.CheckTicked, runrec.CheckLater, or "" to take a mark
// back. Approve needs every check ticked or marked later.
func (e *Engine) MarkCheck(n int, state string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.run.Snapshot()
	if rec.State != runrec.StateAwaitingApproval {
		return fmt.Errorf("the slice is %s, not awaiting approval", rec.State)
	}
	if rec.Card == nil || n < 1 || n > len(rec.Card.HumanChecks) {
		return fmt.Errorf("the slice has no human check %d", n)
	}
	switch state {
	case "", runrec.CheckTicked, runrec.CheckLater:
	default:
		return fmt.Errorf("a human check is ticked, later or open, not %q", state)
	}
	return e.run.Update(func(r *runrec.Record) { r.Card.HumanChecks[n-1].State = state })
}

// Fix sends the owner's note from the approval card to a fix round: the
// implementer, resumed, acts on the note; then the checks and the gate run,
// and the slice returns to awaiting approval. No review runs in between.
func (e *Engine) Fix(ctx context.Context, note string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.run.Snapshot()
	if rec.State != runrec.StateAwaitingApproval {
		return fmt.Errorf("the slice is %s, not awaiting approval", rec.State)
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return errors.New("the fix note is empty")
	}
	if rec.Round+1 > e.d.maxRounds() {
		return fmt.Errorf("the round limit of %d implementer rounds was reached; approve or stop the slice", e.d.maxRounds())
	}
	name := fmt.Sprintf("owner-fix-%d.md", rec.Round+1)
	path := filepath.Join(e.run.Dir(), name)
	if err := os.WriteFile(path, []byte(note+"\n"), 0o600); err != nil {
		return fmt.Errorf("fix note file: %w", err)
	}
	err := e.run.Update(func(r *runrec.Record) {
		r.Round++
		r.Fixes = append(r.Fixes, runrec.OwnerFix{Round: r.Round, Note: note, At: e.d.now()})
		r.Feedback = &runrec.Feedback{Kind: "owner", File: name}
		r.State = runrec.StateImplement
		r.Reason = ""
	})
	if err != nil {
		return err
	}
	return e.advance(ctx)
}
