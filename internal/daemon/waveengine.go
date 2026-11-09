// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"slices"
	"sync"

	"github.com/squalor-xyz/ork/internal/slicercli"
)

// maxShowReads bounds the `slicer show` calls SuggestWithEngineRule runs at
// once. Twelve sequential calls took 1.1 s on 2026-10-31.
const maxShowReads = 8

// Shower reads one item the way `slicer show` does. slicercli.Client
// satisfies it.
type Shower interface {
	Show(ctx context.Context, root, id string) (*slicercli.Detail, error)
}

// SuggestWithEngineRule applies the engine rule (design.md, adj 166) to
// slicer's plan-parallel answer: a wave holds at most one slice that
// EngineSlice flags. It reads each placed item's Files with shower, at most
// maxShowReads at a time, and skips the reads when no wave holds a second
// item. A read that fails leaves that item as a plain slice; the start-time
// check in startWave stays the guard. *Proposed* (S218).
func SuggestWithEngineRule(ctx context.Context, shower Shower, root string, plan *slicercli.ParallelPlan) *slicercli.ParallelPlan {
	if plan == nil {
		return nil
	}
	crowded := false
	var ids []string
	for _, w := range plan.Waves {
		crowded = crowded || len(w.Items) > 1
		for _, it := range w.Items {
			ids = append(ids, it.ID)
		}
	}
	if !crowded {
		return ApplyEngineRule(plan, nil)
	}
	flags := make([]bool, len(ids))
	slots := make(chan struct{}, maxShowReads)
	var wg sync.WaitGroup
	for i, id := range ids {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			detail, err := shower.Show(ctx, root, id)
			flags[i] = err == nil && EngineSlice(detail)
		}()
	}
	wg.Wait()
	engine := map[string]bool{}
	for i, id := range ids {
		if flags[i] {
			engine[id] = true
		}
	}
	return ApplyEngineRule(plan, engine)
}

// ApplyEngineRule returns a copy of plan in which no wave holds more than one
// id of engine. Each wave keeps its first engine item; every further one moves,
// alone, into a new wave right after the wave it came from, with the reason
// "engine: ID" naming the item kept. Items stay in slicer's order, no
// dependency order can break (a later wave still comes after the new one), and
// the waves are renumbered 1..n. Unplaced, blocked, held, unspecified and
// in-flight items are copied as they are; an in-flight engine slice takes no
// slot in a wave. *Proposed* (S218).
func ApplyEngineRule(plan *slicercli.ParallelPlan, engine map[string]bool) *slicercli.ParallelPlan {
	if plan == nil {
		return nil
	}
	out := clonePlan(plan)
	waves := slices.Grow(out.Waves[:0:0], len(out.Waves))
	for _, w := range out.Waves {
		kept := ""
		var deferred []slicercli.ParallelItem
		items := w.Items[:0]
		for _, it := range w.Items {
			if engine[it.ID] {
				if kept != "" {
					it.Reason = "engine: " + kept
					deferred = append(deferred, it)
					continue
				}
				kept = it.ID
			}
			items = append(items, it)
		}
		w.Items = items
		waves = append(waves, w)
		for _, it := range deferred {
			alone := w
			alone.Items = []slicercli.ParallelItem{it}
			waves = append(waves, alone)
		}
	}
	for i := range waves {
		waves[i].Wave = i + 1
	}
	out.Waves = waves
	return out
}

// clonePlan copies every slice of plan, so the result can be edited without
// touching slicer's answer. A nil slice stays nil.
func clonePlan(plan *slicercli.ParallelPlan) *slicercli.ParallelPlan {
	out := *plan
	out.Waves = slices.Clone(plan.Waves)
	for i := range out.Waves {
		out.Waves[i].Items = slices.Clone(out.Waves[i].Items)
	}
	out.InFlight = slices.Clone(plan.InFlight)
	out.Unplaced = slices.Clone(plan.Unplaced)
	out.Blocked = slices.Clone(plan.Blocked)
	for i := range out.Blocked {
		out.Blocked[i].WaitingOn = slices.Clone(out.Blocked[i].WaitingOn)
	}
	out.Held = slices.Clone(plan.Held)
	for i := range out.Held {
		out.Held[i].Flags = slices.Clone(out.Held[i].Flags)
	}
	out.Unspecified = slices.Clone(plan.Unspecified)
	for i := range out.Unspecified {
		out.Unspecified[i].Missing = slices.Clone(out.Unspecified[i].Missing)
	}
	return &out
}
