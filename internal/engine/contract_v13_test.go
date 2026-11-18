// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

func TestEngineStartWithV13CreatesARun(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) { o.contract = 13 })
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rec := e.Snapshot(); rec.Contract != 13 || rec.State != runrec.StateSetup {
		t.Fatalf("contract/state = %d/%s, want 13/setup", rec.Contract, rec.State)
	}
}

func TestFinishOnV13StillResumesTheImplementer(t *testing.T) {
	h := newHarness(t, func(o *harnessOpts) { o.contract = 13 })
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	_ = e.Approve(context.Background()) // landing is outside this assertion
	var finish *Request
	for _, req := range h.runner.requests() {
		if strings.Contains(req.Brief, "## Finish step") {
			r := req
			finish = &r
		}
	}
	if finish == nil {
		t.Fatal("no finish implementer round")
	}
	if finish.Role != RoleImplementer || finish.Resume != "sess-implementer" {
		t.Fatalf("finish role/session: %+v", finish)
	}
	section := finishSection(t, finish.Brief)
	for _, want := range []string{`slicer note S04 --text "Describe the verified outcome" --render`, `slicer done S04 --note "Describe the verified outcome" --render --check`} {
		if !strings.Contains(section, want) {
			t.Errorf("finish brief lacks %q: %s", want, section)
		}
	}
	if strings.Index(section, "slicer note S04") >= strings.Index(section, "slicer done S04") {
		t.Errorf("note must precede done: %s", section)
	}
}
