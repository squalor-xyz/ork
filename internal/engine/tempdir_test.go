// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"os"
	"sync"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// S271: ork points a Claude or Codex role's TMPDIR at Request.TempDir, so the
// directory must already exist when the runner is called: the role must not
// need a mkdir (which no allowlist pattern matches).
func TestRoleTempDirExistsWhenTheRunnerIsCalled(t *testing.T) {
	h := newHarness(t)
	var mu sync.Mutex
	var problems []string
	h.runner.script = func(req Request, n int) (Result, error) {
		fi, err := os.Stat(req.TempDir)
		mu.Lock()
		defer mu.Unlock()
		if req.TempDir == "" || err != nil || !fi.IsDir() {
			problems = append(problems, req.Role+": "+req.TempDir)
		}
		return Result{}, nil
	}
	requireState(t, h.start(), runrec.StateAwaitingApproval)
	if len(h.runner.requests()) == 0 {
		t.Fatal("no role ran")
	}
	if len(problems) != 0 {
		t.Fatalf("temp directory missing when the runner was called: %v", problems)
	}
}
