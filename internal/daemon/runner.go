// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"

	"github.com/squalor-xyz/ork/internal/agent/claude"
	"github.com/squalor-xyz/ork/internal/engine"
)

// claudeRunner connects the engine's vendor-neutral request to S16.
type claudeRunner struct {
	round      func(context.Context, claude.Spec) (claude.Round, error)
	fixtureDir string
	env        []string
}

func (r claudeRunner) Run(ctx context.Context, req engine.Request) (engine.Result, error) {
	if req.Vendor != "claude" {
		return engine.Result{}, fmt.Errorf("unsupported vendor %q", req.Vendor)
	}
	call := r.round
	if call == nil {
		call = claude.Run
	}
	round, err := call(ctx, claude.Spec{Dir: req.Worktree, Env: r.env, Prompt: req.Brief, Model: req.Model, Effort: req.Effort,
		ReadOnly: req.ReadOnly, Allowlist: req.Allowlist, Resume: req.Resume, ReportFile: req.ReportPath, OnStart: req.OnStart, FixtureDir: r.fixtureDir})
	class := engine.ExitError
	switch round.Class {
	case claude.ClassOK:
		class = engine.ExitOK
	case claude.ClassRateLimited:
		class = engine.ExitRateLimited
	}
	result := engine.Result{Exit: class, Session: round.SessionID, Report: round.Report, CostUSD: round.Usage.CostUSD,
		Tokens: int(round.Usage.InputTokens + round.Usage.OutputTokens + round.Usage.CacheReadTokens + round.Usage.CacheCreationTokens)}
	for _, d := range round.Denials {
		result.Denials = append(result.Denials, d.ToolName)
	}
	return result, err
}
