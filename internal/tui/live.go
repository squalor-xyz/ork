// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"

	"github.com/squalor-xyz/ork/internal/slicercli"
)

// Live is the daemon's side of the TUI beyond Snapshot (S13). Refresh reads
// a project now. Focus makes the named projects the ones the owner sees,
// which the daemon reads every 5 s. Next waits for the next change notice
// for the projects in focus and names its project. The TUI calls each one in
// a command, never in Update. ItemShow reads a project item.
// Approve, Stop, Retry and Answer act on engine run IDs.
type Live interface {
	ItemShow(ctx context.Context, project, id string) (*slicercli.Detail, error)
	Refresh(ctx context.Context, project string) error
	Focus(ctx context.Context, projects []string) error
	Next(ctx context.Context) (project string, err error)
	Approve(ctx context.Context, run string) error
	Stop(ctx context.Context, run string) error
	Retry(ctx context.Context, run string) error
	Answer(ctx context.Context, run, text string) error
}

// focusMsg is the result of a Focus call.
type focusMsg struct{ err error }

// noticeMsg is the next change notice, or the error that ended the feed.
type noticeMsg struct {
	project string
	err     error
}
