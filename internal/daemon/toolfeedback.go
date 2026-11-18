// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// toolFeedbackFiling remembers the decided tool feedback records that the
// daemon has filed or is filing since it started (S388, *proposed*). The
// memory is in this process only: a restart sees a record again, and slicer
// returns the item that its key already names, so nothing is filed twice.
type toolFeedbackFiling struct {
	mu   sync.Mutex
	done map[string]bool // project path and record id
}

// toolFeedbackDir is where the Why outlines of filed items are written.
func (d *Daemon) toolFeedbackDir() string {
	return filepath.Join(d.cfg.Paths.Dir, "tool-feedback")
}

// fileDecidedToolFeedback files a slice in p for each record of a fetched
// decided part whose key starts with "tool-feedback." and whose decision is
// "file", unless it has handled that record since start (S388, *proposed*; adj
// 415, 416). Whoever decided the record, ork or the owner's delegate with a
// direct `adj decide`, the next fetch of the part gets here. It never decides
// a record. A failure is printed and the record is tried again on the next
// fetch.
func (d *Daemon) fileDecidedToolFeedback(ctx context.Context, p registry.Project, records []adjcli.Record) {
	for _, rec := range records {
		if !engine.ToolFeedbackDecidedFile(rec) {
			continue
		}
		mark := p.Path + "\x00" + strconv.Itoa(rec.ID)
		d.toolFeedback.mu.Lock()
		if d.toolFeedback.done[mark] {
			d.toolFeedback.mu.Unlock()
			continue
		}
		if d.toolFeedback.done == nil {
			d.toolFeedback.done = map[string]bool{}
		}
		d.toolFeedback.done[mark] = true
		d.toolFeedback.mu.Unlock()
		if err := d.fileToolFeedbackRecord(ctx, p, rec); err != nil {
			d.toolFeedback.mu.Lock()
			delete(d.toolFeedback.done, mark)
			d.toolFeedback.mu.Unlock()
			fmt.Fprintf(os.Stderr, "tool feedback %s adj #%d: %v\n", p.Name, rec.ID, err)
		}
	}
}

func (d *Daemon) fileToolFeedbackRecord(ctx context.Context, p registry.Project, rec adjcli.Record) error {
	dir := d.toolFeedbackDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	deps := d.runDeps(p)
	w := runrec.Writer{Now: deps.Now, Sleep: deps.Sleep, Backoff: deps.Backoff}
	_, err := engine.FileDecidedToolFeedback(ctx, deps, w, dir, p, rec)
	return err
}
