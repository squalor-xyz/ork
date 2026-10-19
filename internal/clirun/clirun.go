// SPDX-License-Identifier: GPL-3.0-or-later

// Package clirun runs one CLI call with a deadline and returns its captured
// stdout, stderr and exit code. Each call runs in its own process group. The
// whole group is killed when the call passes its deadline or is cancelled,
// and again when the call ends, so a grandchild never outlives it.
package clirun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout is the deadline of a call that sets none.
const DefaultTimeout = 10 * time.Second

// waitDelay bounds how long Run waits for output pipes after the process
// ended or was killed.
const waitDelay = 2 * time.Second

// Call is one CLI invocation.
type Call struct {
	Bin     string        // a name looked up on PATH, or a path
	Args    []string      // arguments after the binary
	Dir     string        // working directory; empty keeps the caller's
	Env     []string      // full environment; nil inherits the caller's
	Timeout time.Duration // deadline; zero means DefaultTimeout
}

func (c Call) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

func (c Call) String() string {
	return strings.Join(append([]string{c.Bin}, c.Args...), " ")
}

// Result is what a call printed and how it exited.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ToolError is the {"error":{"code","message","command"}} object slicer
// prints on stdout (with --json) or stderr when a command fails. The code is
// kept exactly as printed.
type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Command string `json:"command"`
}

func (e *ToolError) Error() string {
	if e.Command != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Command, e.Message, e.Code)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

// ExitError is a call that ran and exited non-zero. Result holds its output
// verbatim. Tool is set when stdout or stderr held an error object; it is
// part of the error chain, so errors.As(err, &*ToolError) finds it.
type ExitError struct {
	Call   Call
	Result Result
	Tool   *ToolError
}

func (e *ExitError) Error() string {
	if e.Tool != nil {
		return fmt.Sprintf("%s: exit %d: %v", e.Call, e.Result.ExitCode, e.Tool)
	}
	msg := strings.TrimSpace(string(e.Result.Stderr))
	if msg == "" {
		return fmt.Sprintf("%s: exit %d", e.Call, e.Result.ExitCode)
	}
	return fmt.Sprintf("%s: exit %d: %s", e.Call, e.Result.ExitCode, msg)
}

func (e *ExitError) Unwrap() error {
	if e.Tool == nil {
		return nil
	}
	return e.Tool
}

// TimeoutError is a call that passed its deadline and was killed. Result
// holds what it printed before then.
type TimeoutError struct {
	Call   Call
	Result Result
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s: timed out after %v", e.Call, e.Call.timeout())
}

func (e *TimeoutError) Is(target error) bool { return target == context.DeadlineExceeded }

// ParseToolError returns the error object found on stdout, else on stderr,
// or nil when neither holds one with a code.
func ParseToolError(stdout, stderr []byte) *ToolError {
	for _, b := range [][]byte{stdout, stderr} {
		var v struct {
			Error *ToolError `json:"error"`
		}
		if json.Unmarshal(bytes.TrimSpace(b), &v) == nil && v.Error != nil && v.Error.Code != "" {
			return v.Error
		}
	}
	return nil
}

// Run runs the call. It returns *ExitError for a non-zero exit and
// *TimeoutError past the deadline; both carry the captured Result, and Run
// also returns that Result. A parent context that ends first gives an error
// wrapping the context's error.
func Run(ctx context.Context, c Call) (Result, error) {
	tctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(tctx, c.Bin, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay
	ownProcessGroup(cmd)

	err := cmd.Run()
	// The call is over, so nothing it started may outlive it. A grandchild
	// that kept the output pipes open made Wait give up after waitDelay.
	if cmd.ProcessState != nil {
		killGroup(cmd)
		if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState.Success() {
			err = nil
		}
	}
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: -1}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err == nil {
		return res, nil
	}
	if cmd.ProcessState == nil { // never started
		return res, fmt.Errorf("%s: %w", c, err)
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("%s: %w", c, ctx.Err())
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return res, &TimeoutError{Call: c, Result: res}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return res, &ExitError{Call: c, Result: res, Tool: ParseToolError(res.Stdout, res.Stderr)}
	}
	return res, fmt.Errorf("%s: %w", c, err)
}

// RunJSON runs the call and decodes its stdout into v. Fields v does not
// name are ignored.
func RunJSON(ctx context.Context, c Call, v any) error {
	res, err := Run(ctx, c)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(res.Stdout, v); err != nil {
		return fmt.Errorf("%s: decode output: %w", c, err)
	}
	return nil
}
