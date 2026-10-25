// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	root := e.AdjRoot()
	c := e.d.Adj
	state, id, err := c.Check(ctx, e.wr, root, rec.Pending.Key)
	if err != nil {
		return "", err
	}
	if state == "decided" {
		return fmt.Sprintf("%s already decided as #%d; not changed", rec.Pending.Key, id), nil
	}
	title := questionTitle(rec.Pending.Question)
	if state != "open" {
		id, err = c.Ask(ctx, e.wr, root, title, rec.Pending.Key, rec.Item, rec.Pending.Question, by)
		if err != nil {
			return "", err
		}
	}
	why := "Answered in ork on " + e.d.now().Format("2006-01-02") + ": " + title
	if err := c.Decide(ctx, e.wr, root, id, text, why, by); err != nil {
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
