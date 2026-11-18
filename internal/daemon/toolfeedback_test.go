// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"reflect"
	"testing"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/source"
)

// toolFeedbackDaemon is a daemon whose source answers the decided part from a
// stub, and whose slicer files an item S50 in any project.
func toolFeedbackDaemon(t *testing.T) (*Daemon, *stubSource, registry.Project, *clitest.Fake) {
	t.Helper()
	old, _, p := runDaemon(t, nil,
		clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"S50","title":"x","status":"open","existing":false}`},
		clitest.Route{Tool: "slicer", Args: []string{"promote"}, Prefix: true, Stdout: `{"id":"S50"}`},
	)
	src := &stubSource{snaps: map[string]source.Snapshot{}}
	cfg := old.cfg
	cfg.Source = src
	d := New(cfg)
	t.Cleanup(d.cache.Close)
	t.Cleanup(d.closeRuns)
	p.Adj = true // the cache reads no decided part of a project without adj
	return d, src, p, lastFake
}

func decidedPart(records ...adjcli.Record) source.Snapshot {
	return source.Snapshot{Decided: source.Result[[]adjcli.Record]{Value: records}}
}

func refreshDecided(t *testing.T, d *Daemon, p registry.Project) {
	t.Helper()
	if err := d.cache.Refresh(context.Background(), p, source.Decided); err != nil {
		t.Fatal(err)
	}
}

func filingCalls(f *clitest.Fake) (adds, promotes [][]string) {
	for _, c := range f.CallsTo("slicer") {
		switch c.Args[0] {
		case "add":
			adds = append(adds, c.Args)
		case "promote":
			promotes = append(promotes, c.Args)
		}
	}
	return adds, promotes
}

const (
	adjFeedbackKey     = "tool-feedback.ork-1a2b3c4d"
	slicerFeedbackKey  = "tool-feedback-ork-1a2b3c4d"
	feedbackAskContext = "Proposal: Show the run id\nThe reviewer reported it in ork run run-1, project ork, item S04.\nEvidence: had to run ork status"
)

func feedbackRecord(id int, key, decision string) adjcli.Record {
	return adjcli.Record{ID: id, Title: "Tool feedback (feature, ork): Show the run id", Status: "decided", Key: key,
		Decision: decision, Context: feedbackAskContext, DecidedBy: "jon", Rationale: "Answered in ork (tui) on 2026-11-17"}
}

func TestDecidedToolFeedbackFileIsFiledOnce(t *testing.T) {
	d, src, p, f := toolFeedbackDaemon(t)
	rec := feedbackRecord(20, adjFeedbackKey, "file")
	rec.Refs = []string{"S04"}
	src.put(p.Name, decidedPart(rec))
	refreshDecided(t, d, p)
	adds, promotes := filingCalls(f)
	want := []string{"add", "Show the run id", "--discovered-from", "S04", "--key", slicerFeedbackKey, "--render", "--json", "--lean", "--root", p.Path}
	if len(adds) != 1 || !reflect.DeepEqual(adds[0], want) {
		t.Fatalf("slicer add calls %q, want %q", adds, want)
	}
	if len(promotes) != 1 || promotes[0][1] != "S50" {
		t.Fatalf("slicer promote calls %q", promotes)
	}
	// The next fetch sees the same decided record and files nothing.
	refreshDecided(t, d, p)
	refreshDecided(t, d, p)
	if adds, promotes = filingCalls(f); len(adds) != 1 || len(promotes) != 1 {
		t.Fatalf("a second fetch filed again: %d adds, %d promotes", len(adds), len(promotes))
	}
}

func TestDecidedToolFeedbackWithoutARefIsFiledWithoutDiscoveredFrom(t *testing.T) {
	d, src, p, f := toolFeedbackDaemon(t)
	src.put(p.Name, decidedPart(feedbackRecord(20, adjFeedbackKey, "file")))
	refreshDecided(t, d, p)
	adds, _ := filingCalls(f)
	want := []string{"add", "Show the run id", "--key", slicerFeedbackKey, "--render", "--json", "--lean", "--root", p.Path}
	if len(adds) != 1 || !reflect.DeepEqual(adds[0], want) {
		t.Fatalf("slicer add calls %q, want %q", adds, want)
	}
}

func TestDecidedToolFeedbackDropAndOtherKeysFileNothing(t *testing.T) {
	for name, rec := range map[string]adjcli.Record{
		"drop":      feedbackRecord(20, adjFeedbackKey, "drop"),
		"other key": feedbackRecord(21, "feedback.when", "file"),
		"open":      {ID: 22, Key: adjFeedbackKey, Status: "open", Context: feedbackAskContext},
	} {
		t.Run(name, func(t *testing.T) {
			d, src, p, f := toolFeedbackDaemon(t)
			src.put(p.Name, decidedPart(rec))
			refreshDecided(t, d, p)
			if adds, promotes := filingCalls(f); len(adds)+len(promotes) != 0 {
				t.Fatalf("filed: %q %q", adds, promotes)
			}
			if n := len(f.CallsTo("adj")); n != 0 {
				t.Fatalf("ork ran adj: %v", f.CallsTo("adj"))
			}
		})
	}
}

// A record that the owner's delegate decided outside ork, with `adj decide`, is
// first seen on a later refresh and filed like one answered in ork.
func TestDecidedToolFeedbackByDelegateIsFiled(t *testing.T) {
	d, src, p, f := toolFeedbackDaemon(t)
	src.put(p.Name, decidedPart())
	refreshDecided(t, d, p)
	if adds, _ := filingCalls(f); len(adds) != 0 {
		t.Fatalf("filed before any decision: %q", adds)
	}
	rec := feedbackRecord(20, adjFeedbackKey, "file")
	rec.DecidedBy = "Jon"
	rec.Rationale = "Decided in relay session 8d711f67 as the owner's delegate: file."
	src.put(p.Name, decidedPart(rec))
	refreshDecided(t, d, p)
	adds, promotes := filingCalls(f)
	if len(adds) != 1 || adds[0][1] != "Show the run id" || len(promotes) != 1 {
		t.Fatalf("slicer add %q, promote %q", adds, promotes)
	}
}
