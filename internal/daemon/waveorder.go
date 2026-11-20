// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"slices"
	"sort"
	"strings"

	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// OrderHold says that Item, later in the typed order of a wave, waits until
// After lands because the two items share Files paths (S342, *proposed*; adj
// 477 wave.overlap-order = hold). slicer computes the overlap; ork only reads
// the typed order.
type OrderHold struct {
	Item   string   `json:"item"`
	After  string   `json:"after"`
	Shared []string `json:"shared"`
}

// WaveOrder turns slicer's overlap pairs into holds. ids is the typed order of
// the wave. A pair that shares no path holds nothing, and neither does a pair
// with an item outside ids. The holds come ordered by the typed position of the
// later item, then by that of the earlier one.
func WaveOrder(ids []string, pairs []slicercli.OverlapPair) []OrderHold {
	pos := map[string]int{}
	for i, id := range ids {
		if _, ok := pos[id]; !ok {
			pos[id] = i
		}
	}
	var holds []OrderHold
	seen := map[[2]string]bool{}
	for _, pair := range pairs {
		a, okA := pos[pair.A]
		b, okB := pos[pair.B]
		if !okA || !okB || a == b || len(pair.Shared) == 0 {
			continue
		}
		hold := OrderHold{Item: pair.B, After: pair.A, Shared: slices.Clone(pair.Shared)}
		if b < a {
			hold.Item, hold.After = pair.A, pair.B
		}
		key := [2]string{hold.Item, hold.After}
		if seen[key] {
			continue
		}
		seen[key] = true
		holds = append(holds, hold)
	}
	sort.SliceStable(holds, func(i, j int) bool {
		if pi, pj := pos[holds[i].Item], pos[holds[j].Item]; pi != pj {
			return pi < pj
		}
		return pos[holds[i].After] < pos[holds[j].After]
	})
	return holds
}

// OrderLine is the line ork wave start prints before the confirm, for example
// "order: s456 after s455 (snapshot.py); s451 after s455 (snapshot.py)". It is
// empty when nothing is held.
func OrderLine(holds []OrderHold) string {
	if len(holds) == 0 {
		return ""
	}
	parts := make([]string, len(holds))
	for i, h := range holds {
		parts[i] = h.Item + " after " + h.After + " (" + strings.Join(h.Shared, ", ") + ")"
	}
	return "order: " + strings.Join(parts, "; ")
}

// holdsOf returns the holds of item whose earlier member is not in skip.
func holdsOf(holds []OrderHold, item string, skip map[string]bool) []OrderHold {
	var out []OrderHold
	for _, h := range holds {
		if h.Item == item && !skip[h.After] {
			out = append(out, h)
		}
	}
	return out
}

// holdMessage is what wave.start says of a held member.
func holdMessage(holds []OrderHold) string {
	parts := make([]string, len(holds))
	for i, h := range holds {
		parts[i] = h.After + " (" + strings.Join(h.Shared, ", ") + ")"
	}
	verb := "lands"
	if len(holds) > 1 {
		verb = "land"
	}
	return "held until " + strings.Join(parts, ", ") + " " + verb
}

// runLanded reports that the run's slice is on the base branch: ork landed it,
// or the cleanup found it merged. A run that is committed but not landed, as at
// stop_after commit, has not landed.
func runLanded(rec runrec.Record) bool {
	if rec.State != runrec.StateCommitted {
		return false
	}
	return !rec.LandedAt.IsZero() || (!rec.CleanedAt.IsZero() && rec.CleanedReason == "")
}

// waitingOn names what a queued run still waits on: each item of rec.After
// whose newest run has not landed, with that run's state, as "s455 (stopped)".
// all holds the records of the project's runs. An item with no run is named
// "(no run)": the hold then stays until the owner stops the run. An item that
// runs as a sliver chain has runs named for its slivers, with the item as their
// Parent; the chain lands the item through its last sliver, so that run stands
// for the item.
func waitingOn(rec runrec.Record, all []runrec.Record) []string {
	var out []string
	for _, item := range rec.After {
		var newest *runrec.Record
		for i := range all {
			r := &all[i]
			if r.Project != rec.Project {
				continue
			}
			if r.Item != item && (r.Chain == "" || r.Parent != item || r.SliverIndex != r.SliverCount) {
				continue
			}
			if newest == nil || r.CreatedAt.After(newest.CreatedAt) || (r.CreatedAt.Equal(newest.CreatedAt) && r.ID > newest.ID) {
				newest = r
			}
		}
		switch {
		case newest == nil:
			out = append(out, item+" (no run)")
		case !runLanded(*newest):
			out = append(out, item+" ("+newest.State+")")
		}
	}
	return out
}

// runRecordsLocked lists every stored run as its engine reports it. The caller
// holds the manager lock. A store that cannot be read gives no records, so a
// hold stays.
func (d *Daemon) runRecordsLocked() []runrec.Record {
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		return nil
	}
	all := make([]runrec.Record, 0, len(stored))
	for _, run := range stored {
		rec := run.Snapshot()
		if r := d.runs.runs[rec.ID]; r != nil && r.engine != nil {
			rec = r.engine.Snapshot()
		}
		all = append(all, rec)
	}
	return all
}
