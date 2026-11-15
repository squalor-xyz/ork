// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// notifier owns notification processes independently of the run manager.
// Its lock serializes admission with shutdown, never command execution.
type notifier struct {
	d      *Daemon
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func newNotifier(d *Daemon) *notifier {
	ctx, cancel := context.WithCancel(context.Background())
	return &notifier{d: d, ctx: ctx, cancel: cancel}
}

func notifyEvent(rec runrec.Record) string {
	switch rec.State {
	case runrec.StatePaused:
		if rec.Pending != nil && rec.Pending.Kind == runrec.PendingQuestion {
			return "question"
		}
		return "paused"
	case runrec.StateAwaitingApproval, runrec.StateStopped:
		return rec.State
	case runrec.StateCommitted:
		if !rec.LandedAt.IsZero() {
			return "landed"
		}
	}
	return ""
}

func (n *notifier) changed(run *runrec.Run, before, after runrec.Record) {
	event := notifyEvent(after)
	if event == "" || event == notifyEvent(before) {
		return
	}
	// adj 359: claim each occurrence, using its transition clock rather than
	// metadata updates that happen while the run remains in the same need.
	at := after.WaitStartedAt
	switch event {
	case "stopped":
		at = after.UpdatedAt
	case "landed":
		at = after.LandedAt
	}
	key := event + "@" + at.UTC().Format(time.RFC3339Nano)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		proj, err := n.d.registered(after.Project)
		if err != nil || proj.NotifyCommand == "" {
			return
		}
		claimed, err := run.ClaimNotification(key)
		if err != nil {
			n.failure(after.ID, event, err)
			return
		}
		if !claimed {
			return
		}
		next := ""
		reason := after.Reason
		switch after.State {
		case runrec.StatePaused:
			next = pausedNext(after)
			if after.Pending != nil {
				reason = after.Pending.Reason
			}
		case runrec.StateAwaitingApproval:
			next = "ork approve " + after.ID
		case runrec.StateStopped:
			stored, _ := n.d.runs.deps.Store.List()
			next = stoppedNext(after, stored)
		}
		command := proj.NotifyCommand // adj 358: the whole value is one executable
		if strings.ContainsRune(command, os.PathSeparator) && !filepath.IsAbs(command) {
			command = filepath.Join(proj.Path, command)
		}
		env := append(os.Environ(), "ORK_EVENT="+event, "ORK_PROJECT="+after.Project,
			"ORK_RUN="+after.ID, "ORK_ITEM="+after.Item, "ORK_STATE="+after.State,
			"ORK_REASON="+notifyLine(reason), "ORK_NEXT="+notifyLine(next))
		_, err = clirun.Run(n.ctx, clirun.Call{Bin: command, Dir: proj.Path, Env: env, Timeout: 10 * time.Second})
		if err != nil {
			n.failure(after.ID, event, err)
		}
	}()
}

func notifyLine(s string) string {
	return strings.Join(strings.Fields(runrec.PlainText(s)), " ")
}

func (n *notifier) failure(id, event string, err error) {
	fmt.Fprintf(os.Stderr, "notify %s %s: %s\n", notifyLine(id), event, notifyLine(err.Error()))
}

func (n *notifier) close() {
	n.mu.Lock()
	n.closed = true
	n.cancel()
	n.mu.Unlock()
	n.wg.Wait()
}
