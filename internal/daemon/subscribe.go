// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/source"
)

// subscribe makes the projects in params the ones this connection shows. The
// connection then gets a notice for every change (dash.refresh).
func (d *Daemon) subscribe(ctx context.Context, p SubscribeParams) (any, error) {
	conn := ipc.ConnFrom(ctx)
	if conn == nil {
		return nil, &ipc.Error{Code: ipc.CodeInternal, Message: "subscribe needs a connection"}
	}
	f, err := d.loadRegistry()
	if err != nil {
		return nil, err
	}
	for _, name := range p.Focus {
		if _, ok := find(f, name); !ok {
			return nil, notFound(name, f)
		}
	}
	d.setFocus(conn, p.Focus)
	return nil, nil
}

// setFocus replaces a connection's projects. The first call from a connection
// also makes the daemon forget it when it leaves.
func (d *Daemon) setFocus(conn *ipc.Conn, names []string) {
	d.subsMu.Lock()
	defer d.subsMu.Unlock()
	_, known := d.focus[conn]
	d.focus[conn] = names
	d.cache.SetVisible(d.visibleLocked())
	if !known {
		go func() {
			<-conn.Done()
			d.drop(conn)
		}()
	}
}

// drop forgets a connection that has ended: it stops getting notices, and its
// projects stop being visible.
func (d *Daemon) drop(conn *ipc.Conn) {
	d.subsMu.Lock()
	defer d.subsMu.Unlock()
	delete(d.focus, conn)
	d.cache.SetVisible(d.visibleLocked())
}

// visibleLocked lists the projects any subscribed connection shows. The
// caller holds subsMu.
func (d *Daemon) visibleLocked() []string {
	seen := map[string]bool{}
	var out []string
	for _, names := range d.focus {
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// changed sends a notice to every subscribed connection that a part of a
// project changed. A connection that cannot take the notice is closed by
// ipc, and drops out here when it ends.
func (d *Daemon) changed(project string, part source.Part) {
	d.subsMu.Lock()
	conns := make([]*ipc.Conn, 0, len(d.focus))
	for c := range d.focus {
		conns = append(conns, c)
	}
	d.subsMu.Unlock()
	n := SnapshotChanged{Project: project, Parts: []string{string(part)}, At: d.cfg.Clock.Now()}
	for _, c := range conns {
		c.Notify(MethodSnapshotChanged, n)
	}
}
