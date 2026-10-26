// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// FileAnswer files a pending question in adj before Answer resumes the role.
// The daemon serializes this with the engine's other owner actions.
func (e *Engine) FileAnswer(ctx context.Context, text, by string) (string, error) {
	rec := e.Snapshot()
	if rec.Pending == nil || rec.Pending.Key == "" {
		return "", nil
	}
	if strings.TrimSpace(by) == "" {
		return "", errors.New("ADJ_DECIDED_BY is unset; set it in the caller before filing an answer in adj")
	}
	title := questionTitle(rec.Pending.Question)
	if rec.Pending.Ask != nil {
		title = rec.Pending.Ask.Title
	}
	return fileDecision(ctx, e.d, e.wr, e.AdjRoot(), rec.Pending.Key, title, rec.Item, rec.Pending.Question, text, by)
}

// fileDecision files an owner's answer in adj: it asks the question when the
// key is not yet open, then decides it. A key that is already decided is left
// alone. The notice says what happened.
func fileDecision(ctx context.Context, d Deps, wr runrec.Writer, root, key, title, ref, question, text, by string) (string, error) {
	c := d.Adj
	state, id, err := c.Check(ctx, wr, root, key)
	if err != nil {
		return "", err
	}
	if state == "decided" {
		return fmt.Sprintf("%s already decided as #%d; not changed", key, id), nil
	}
	if state != "open" {
		id, err = c.Ask(ctx, wr, root, title, key, ref, question, by)
		if err != nil {
			return "", err
		}
	}
	why := "Answered in ork on " + d.now().Format("2006-01-02") + ": " + title
	if err := c.Decide(ctx, wr, root, id, text, why, by); err != nil {
		return "", err
	}
	return fmt.Sprintf("decided in adj as #%d", id), nil
}

func questionTitle(question string) string {
	for _, line := range strings.Split(question, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Decision key:") {
			continue
		}
		line = strings.TrimLeft(line, " -*#>\t")
		if line == "" {
			continue
		}
		chars := []rune(line)
		return string(chars[:min(len(chars), 100)])
	}
	return "Owner question"
}
