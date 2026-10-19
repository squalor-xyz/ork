// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"sync"
)

// ErrFeedClosed is returned by a Feed after Close.
var ErrFeedClosed = errors.New("notice feed closed")

// ErrNoFocus is returned by Next before the first Focus.
var ErrNoFocus = errors.New("no projects in focus yet")

// Feed carries the daemon's change notices for the projects a client shows
// (dash.refresh, S15). Only a connection that subscribed gets notices, and a
// connection waiting for one cannot take a request. So the feed keeps its
// notice connection apart from a Client's request connection. Focus dials a
// new connection that subscribes to the projects and closes the one before.
// Next reads the current connection, and moves to the new one when Focus
// replaces it while Next waits.
type Feed struct {
	socket  string
	focusMu sync.Mutex // one Focus at a time, in the order the calls were made
	mu      sync.Mutex // guards the fields below
	conn    *Client
	gen     int // counts the connections Focus has made
	closed  bool
}

// NewFeed returns a Feed for the daemon at socket. It dials nothing until Focus.
func NewFeed(socket string) *Feed { return &Feed{socket: socket} }

// Focus makes projects the ones the feed shows, replacing the earlier focus.
// It returns once the daemon has the focus; it does not wait for a notice.
func (f *Feed) Focus(ctx context.Context, projects []string) error {
	f.focusMu.Lock()
	defer f.focusMu.Unlock()
	c, err := Dial(ctx, f.socket)
	if err != nil {
		return err
	}
	if err := c.Subscribe(ctx, projects); err != nil {
		c.Close()
		return err
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		c.Close()
		return ErrFeedClosed
	}
	old := f.conn
	f.conn, f.gen = c, f.gen+1
	f.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

// Next waits for the next change notice. When Focus replaces the connection
// during the wait, Next goes on with the new one.
func (f *Feed) Next(ctx context.Context) (SnapshotChanged, error) {
	for {
		f.mu.Lock()
		c, gen, closed := f.conn, f.gen, f.closed
		f.mu.Unlock()
		if closed {
			return SnapshotChanged{}, ErrFeedClosed
		}
		if c == nil {
			return SnapshotChanged{}, ErrNoFocus
		}
		n, err := c.Next(ctx)
		if err == nil {
			return n, nil
		}
		f.mu.Lock()
		closed, moved := f.closed, f.gen != gen
		f.mu.Unlock()
		if closed {
			return SnapshotChanged{}, ErrFeedClosed
		}
		if !moved || ctx.Err() != nil {
			return SnapshotChanged{}, err
		}
	}
}

// Close closes the feed's connection. Next returns ErrFeedClosed after Close.
func (f *Feed) Close() error {
	f.mu.Lock()
	f.closed = true
	c := f.conn
	f.conn = nil
	f.mu.Unlock()
	if c != nil {
		return c.Close()
	}
	return nil
}
