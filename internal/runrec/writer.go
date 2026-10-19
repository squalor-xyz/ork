// SPDX-License-Identifier: GPL-3.0-or-later

package runrec

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// Sink receives the writes a Writer makes.
type Sink interface{ AddWrite(Write) error }

// DefaultBackoff is the wait before each retry of a locked call. Its length
// is the number of retries.
var DefaultBackoff = []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}

// Cmd is one write call. Args is what the binary gets; Argv is what writes[]
// records, without the --root or -C that Root records.
type Cmd struct {
	Tool    string // "slicer", "adj" or "git"
	Bin     string
	Root    string
	Argv    []string
	Args    []string
	Dir     string
	Timeout time.Duration
}

// Writer runs write calls, retries a locked one with backoff, and logs every
// attempt to its Sink.
type Writer struct {
	Sink    Sink
	Now     func() time.Time
	Sleep   func(ctx context.Context, d time.Duration) error
	Backoff []time.Duration // nil means DefaultBackoff
}

func (w Writer) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w Writer) sleep(ctx context.Context, d time.Duration) error {
	if w.Sleep != nil {
		return w.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run runs the call. A call that exits with the tool error code "locked" is
// retried after each Backoff delay; every attempt is one entry in the sink.
// It returns the last attempt's result and error.
func (w Writer) Run(ctx context.Context, c Cmd) (clirun.Result, error) {
	backoff := w.Backoff
	if backoff == nil {
		backoff = DefaultBackoff
	}
	for retry := 0; ; retry++ {
		start := w.now()
		res, err := clirun.Run(ctx, clirun.Call{Bin: c.Bin, Args: c.Args, Dir: c.Dir, Timeout: c.Timeout})
		entry := Write{At: start.UTC(), Tool: c.Tool, Root: c.Root, Argv: append([]string(nil), c.Argv...), Exit: res.ExitCode, Retry: retry, MS: w.now().Sub(start).Milliseconds()}
		var exitErr *clirun.ExitError
		locked := false
		if err != nil {
			if errors.As(err, &exitErr) && exitErr.Tool != nil {
				entry.Code = exitErr.Tool.Code
				locked = res.ExitCode == 3 && entry.Code == "locked"
			} else if !errors.As(err, &exitErr) {
				entry.Error = err.Error()
			}
		}
		if w.Sink != nil {
			if recordErr := w.Sink.AddWrite(entry); recordErr != nil {
				return res, errors.Join(err, fmt.Errorf("record write: %w", recordErr))
			}
		}
		if err == nil || !locked || retry >= len(backoff) {
			return res, err
		}
		if serr := w.sleep(ctx, backoff[retry]); serr != nil {
			return res, serr
		}
	}
}
