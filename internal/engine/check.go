// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// RunCheck runs the command with `sh -c` in dir, with a deadline. A command
// that exits non-zero or runs out of time is a result with OK false; only a
// failure to run it at all is an error.
func RunCheck(ctx context.Context, dir, command string, timeout time.Duration) (CheckOutput, error) {
	res, err := clirun.Run(ctx, clirun.Call{Bin: "sh", Args: []string{"-c", command}, Dir: dir, Timeout: timeout})
	out := string(res.Stdout) + string(res.Stderr)
	var exitErr *clirun.ExitError
	var timeoutErr *clirun.TimeoutError
	switch {
	case err == nil:
		return CheckOutput{OK: true, Output: out}, nil
	case errors.As(err, &exitErr):
		return CheckOutput{Output: out + fmt.Sprintf("\nexit %d\n", res.ExitCode)}, nil
	case errors.As(err, &timeoutErr):
		return CheckOutput{Output: out + fmt.Sprintf("\ntimed out after %v\n", timeout)}, nil
	}
	return CheckOutput{}, err
}

// tail keeps the last n bytes of s, so a huge test log does not fill a brief.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "[...cut...]\n" + s[len(s)-n:]
}
