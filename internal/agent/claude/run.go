// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

const (
	// waitDelay bounds how long Run waits for the output pipe after the
	// process ended or was killed, as internal/clirun does.
	waitDelay = 2 * time.Second
	// defaultTailBytes is the size of Round.Tail when Spec sets none.
	defaultTailBytes = 2048
)

// Run starts one round and returns when the process has ended. The process
// runs in its own process group, and cancelling ctx kills the whole group.
// The group is killed again at the end, so a grandchild never outlives the
// round. Events reach Spec.OnEvent as the lines arrive.
//
// A non-zero exit is not an error: it is classified. Run returns an error
// when the spec is refused, the binary cannot be started, or ctx ends the
// round; the Round then holds what was read before then. Every round that is
// not ok, denials included, saves a raw fixture candidate (vendor.fixture-candidates).
//
// internal/clirun buffers a call's output and offers no hook while the call
// runs, so Run starts the process itself and shares clirun's process-group
// helpers (code.process-groups).
func Run(ctx context.Context, s Spec) (Round, error) {
	args, err := s.Args()
	if err != nil {
		return Round{}, err
	}
	bin := s.Bin
	if bin == "" {
		bin = "claude"
	}
	round := Round{Command: append([]string{bin}, args...), ExitCode: -1}

	tail := s.TailBytes
	if tail <= 0 {
		tail = defaultTailBytes
	}
	out := &streamReader{onEvent: s.OnEvent, tailBytes: tail}
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	cmd.Stdout = out
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay
	clirun.OwnProcessGroup(cmd)

	runErr := cmd.Run()
	if cmd.ProcessState != nil {
		clirun.KillGroup(cmd)
		if errors.Is(runErr, exec.ErrWaitDelay) && cmd.ProcessState.Success() {
			runErr = nil
		}
	}
	out.flush()
	round.Stdout = out.raw.Bytes()
	round.Stderr = stderr.Bytes()
	round.Tail = out.Tail()
	round.Result = out.result
	round.SessionID = out.sessionID
	if out.result != nil {
		round.SessionID = out.result.SessionID
		round.Text = out.result.Text
		round.Usage = out.result.Usage
		round.Denials = out.result.Denials
	}
	if cmd.ProcessState == nil {
		return round, fmt.Errorf("claude: start %s: %w", bin, runErr)
	}
	round.ExitCode = cmd.ProcessState.ExitCode()
	// Only a process the cancel killed (signalled, exit code -1) is a cancelled
	// round. A cancel that lands after the process exited does not undo it.
	if ctx.Err() != nil && round.ExitCode == -1 {
		return round, fmt.Errorf("claude: %w", ctx.Err())
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) && !errors.Is(runErr, exec.ErrWaitDelay) {
		return round, fmt.Errorf("claude: %w", runErr)
	}

	round.Class, round.Reason = Classify(round.ExitCode, round.Result)
	round.Report, round.ReportSource = ReadReport(s.ReportFile, round.Result)
	if round.Class != ClassOK {
		round.Candidate, round.CandidateErr = saveCandidate(s.FixtureDir, round)
	}
	return round, nil
}
