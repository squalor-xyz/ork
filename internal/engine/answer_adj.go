// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// ResolveOwner is the owner's name for adj: the environment's ADJ_DECIDED_BY
// when it is set, else the global owner setting (S314, *proposed*). Empty
// means no owner is known. Every path that files an answer under the owner's
// name resolves it here.
func ResolveOwner(env, setting string) string {
	if env = strings.TrimSpace(env); env != "" {
		return env
	}
	return strings.TrimSpace(setting)
}

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
	why := "Answered in ork on " + d.now().Format("2006-01-02") + ": " + title
	id, already, err := decideInAdj(ctx, d, wr, root, key, title, ref, question, text, why, by)
	if err != nil {
		return "", err
	}
	if already {
		return fmt.Sprintf("%s already decided as #%d; not changed", key, id), nil
	}
	return fmt.Sprintf("decided in adj as #%d", id), nil
}

// decideInAdj does the adj calls of fileDecision and returns the record's ID,
// and whether the key was already decided (then nothing was changed).
func decideInAdj(ctx context.Context, d Deps, wr runrec.Writer, root, key, title, ref, question, text, why, by string) (int, bool, error) {
	c := d.Adj
	state, id, err := c.Check(ctx, wr, root, key)
	if err != nil {
		return 0, false, err
	}
	if state == "decided" {
		return id, true, nil
	}
	if state != "open" {
		id, err = c.Ask(ctx, wr, root, title, key, ref, question, by)
		if err != nil {
			return 0, false, err
		}
	}
	if err := c.Decide(ctx, wr, root, id, text, why, by); err != nil {
		return 0, false, err
	}
	return id, false, nil
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
