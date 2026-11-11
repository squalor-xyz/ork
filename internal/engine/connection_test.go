// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"github.com/squalor-xyz/ork/internal/runrec"
	"strings"
	"testing"
	"time"
)

func connectionHarness(t *testing.T, failures int, fallback bool) *harness {
	t.Helper()
	h := fallbackHarness(t, RoleImplementer)
	if !fallback {
		h.cfg.Lineup[RoleImplementer] = h.cfg.Lineup[RoleImplementer][:1]
	}
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleImplementer && req.Vendor == "codex" && n < failures {
			return Result{Exit: ExitClass("connection"), Session: "connection-session", Reason: "api_error connection_failed"}, nil
		}
		return Result{}, nil
	}
	return h
}

func TestConnectionResume(t *testing.T) {
	h := connectionHarness(t, 1, true)
	rec := requireState(t, h.start(), runrec.StateAwaitingApproval)
	reqs := shapeRequests(h, RoleImplementer)
	if len(reqs) != 2 {
		t.Fatalf("connection attempts %d, want 2", len(reqs))
	}
	if reqs[1].Resume != "connection-session" || reqs[1].Round != reqs[0].Round || reqs[1].Vendor != reqs[0].Vendor {
		t.Fatalf("connection resume: %+v", reqs[1])
	}
	if len(h.sleep.delays) != 1 || h.sleep.delays[0] != 30*time.Second {
		t.Fatalf("connection delays %v, want [30s]", h.sleep.delays)
	}
	if got := h.readReport(rec, rec.Roles[1].Report); got != implReport {
		t.Fatalf("resumed report %q", got)
	}
}

func TestConnectionFallback(t *testing.T) {
	h := connectionHarness(t, 2, true)
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	reqs := shapeRequests(h, RoleImplementer)
	if len(reqs) != 3 {
		t.Fatalf("connection attempts %d, want 3", len(reqs))
	}
	if reqs[2].Vendor != "claude" || reqs[2].Resume != "" || reqs[2].Round != 1 {
		t.Fatalf("connection fallback: %+v", reqs[2])
	}
}

func TestConnectionPause(t *testing.T) {
	for _, session := range []string{"connection-session", ""} {
		name, failures := "exhausted-resume", 2
		if session == "" {
			name, failures = "empty-session", 1
		}
		t.Run(name, func(t *testing.T) {
			h := connectionHarness(t, failures, false)
			script := h.runner.script
			h.runner.script = func(req Request, n int) (Result, error) {
				res, err := script(req, n)
				res.Session = session
				return res, err
			}
			e := h.start()
			rec := requireState(t, e, runrec.StatePaused)
			if !strings.Contains(rec.Reason, "codex") || !strings.Contains(rec.Reason, "api_error connection_failed") || strings.ContainsAny(rec.Reason, "\r\n") {
				t.Fatalf("connection pause reason %q", rec.Reason)
			}
			if err := e.Retry(context.Background()); err != nil {
				t.Fatal(err)
			}
			rec = requireState(t, e, runrec.StateAwaitingApproval)
			reqs := shapeRequests(h, RoleImplementer)
			if len(reqs) != failures+1 {
				t.Fatalf("owner retry attempts %d, want %d", len(reqs), failures+1)
			}
			got := reqs[failures]
			if got.Vendor != reqs[0].Vendor || got.Round != reqs[0].Round || got.Resume != session {
				t.Fatalf("owner retry vendor %q, round %d, resume %q", got.Vendor, got.Round, got.Resume)
			}
			if got := h.readReport(rec, rec.Roles[failures].Report); got != implReport {
				t.Fatalf("owner retry report %q", got)
			}
			if len(h.sleep.delays) != failures-1 {
				t.Fatalf("owner retry added an automatic wait: %v", h.sleep.delays)
			}
		})
	}
}
