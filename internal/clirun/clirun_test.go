// SPDX-License-Identifier: GPL-3.0-or-later

package clirun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The tests fake the CLI with the helper-process pattern: the test binary
// runs itself with ORK_CLIRUN_HELPER set, and TestMain serves the call.

func TestMain(m *testing.M) {
	if os.Getenv("ORK_CLIRUN_HELPER") == "1" {
		helperMain(os.Args[1:])
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperMain(args []string) {
	switch args[0] {
	case "emit": // emit STDOUT STDERR CODE
		os.Stdout.WriteString(args[1])
		os.Stderr.WriteString(args[2])
		code, _ := strconv.Atoi(args[3])
		os.Exit(code)
	case "cat": // cat FILE CODE: replay a recorded file on stdout
		data, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(99)
		}
		os.Stdout.Write(data)
		code, _ := strconv.Atoi(args[2])
		os.Exit(code)
	case "spawn": // start a grandchild that blocks, print its pid, then block
		child := exec.Command(os.Args[0], "block")
		child.Env = os.Environ()
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(98)
		}
		fmt.Fprintf(os.Stdout, "%d\n", child.Process.Pid)
		time.Sleep(time.Hour)
	case "leave": // start a grandchild that holds stdout, print its pid, exit 0
		child := exec.Command(os.Args[0], "block")
		child.Env = os.Environ()
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(98)
		}
		fmt.Fprintf(os.Stdout, "%d\n", child.Process.Pid)
	case "block":
		time.Sleep(30 * time.Second) // bounded, so a failing test leaves no long-lived orphan
	}
}

func helper(t *testing.T, timeout time.Duration, args ...string) Call {
	t.Helper()
	t.Setenv("ORK_CLIRUN_HELPER", "1")
	return Call{Bin: os.Args[0], Args: args, Timeout: timeout}
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func TestTimeoutKillsTheProcessGroup(t *testing.T) {
	call := helper(t, 1500*time.Millisecond, "spawn")
	start := time.Now()
	res, err := Run(context.Background(), call)
	elapsed := time.Since(start)

	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("want *TimeoutError, got %T: %v", err, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error does not wrap context.DeadlineExceeded: %v", err)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("call took %v", elapsed)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if perr != nil {
		t.Fatalf("grandchild pid not captured: stdout %q, stderr %q", res.Stdout, res.Stderr)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d is still running after the timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestGrandchildHoldingStdoutAfterExitIsKilled(t *testing.T) {
	call := helper(t, 6*time.Second, "leave")
	start := time.Now()
	res, err := Run(context.Background(), call)
	elapsed := time.Since(start)

	// Register the kill first, so no check below can leave the grandchild behind.
	pid, perr := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if perr == nil {
		t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	}
	if err != nil {
		t.Fatalf("a child that exited 0 must succeed, got %T: %v", err, err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("call took %v, deadline was 6s", elapsed)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code %d", res.ExitCode)
	}
	if perr != nil {
		t.Fatalf("output lost: stdout %q, stderr %q", res.Stdout, res.Stderr)
	}
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d is still running after Run returned", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNonZeroExitKeepsOutputVerbatim(t *testing.T) {
	stdout := "  line one\n\nline two \t\n"
	stderr := "warn: a\n\n"
	res, err := Run(context.Background(), helper(t, 0, "emit", stdout, stderr, "3"))
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want *ExitError, got %T: %v", err, err)
	}
	if ee.Result.ExitCode != 3 || res.ExitCode != 3 {
		t.Fatalf("exit code: error %d, result %d", ee.Result.ExitCode, res.ExitCode)
	}
	if string(res.Stdout) != stdout || string(res.Stderr) != stderr {
		t.Fatalf("output changed: stdout %q stderr %q", res.Stdout, res.Stderr)
	}
	if string(ee.Result.Stdout) != stdout || string(ee.Result.Stderr) != stderr {
		t.Fatalf("error result changed: %+v", ee.Result)
	}
	if ee.Tool != nil {
		t.Fatalf("plain text parsed as a tool error: %+v", ee.Tool)
	}
}

func TestSuccessCapturesOutput(t *testing.T) {
	res, err := Run(context.Background(), helper(t, 0, "emit", "out", "err", "0"))
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "out" || string(res.Stderr) != "err" || res.ExitCode != 0 {
		t.Fatalf("result: %+v", res)
	}
}

func TestToolErrorOnStdout(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "cli", "slicer", "error-no-such-item.json")
	recorded, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), helper(t, 0, "cat", fixture, "2"))
	var te *ToolError
	if !errors.As(err, &te) {
		t.Fatalf("want a *ToolError in the chain, got %T: %v", err, err)
	}
	if te.Code != "no_such_item" || te.Message != "no such item: NOPE" || te.Command != "show" {
		t.Fatalf("tool error: %+v", te)
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Result.ExitCode != 2 {
		t.Fatalf("want an *ExitError with code 2: %v", err)
	}
	if string(res.Stdout) != string(recorded) {
		t.Fatalf("stdout changed:\n%s", res.Stdout)
	}
}

func TestToolErrorOnStderr(t *testing.T) {
	body := `{"error":{"code":"Weird_Code-9","message":"bad flag","command":"list"}}` + "\n"
	res, err := Run(context.Background(), helper(t, 0, "emit", "", body, "2"))
	var te *ToolError
	if !errors.As(err, &te) {
		t.Fatalf("want a *ToolError, got %T: %v", err, err)
	}
	if te.Code != "Weird_Code-9" || te.Message != "bad flag" || te.Command != "list" {
		t.Fatalf("tool error: %+v", te)
	}
	if string(res.Stderr) != body {
		t.Fatalf("stderr changed: %q", res.Stderr)
	}
}

func TestJSONWithoutErrorObjectIsNotAToolError(t *testing.T) {
	_, err := Run(context.Background(), helper(t, 0, "emit", `{"records": []}`, "", "1"))
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Tool != nil {
		t.Fatalf("want an *ExitError without a tool error: %v", err)
	}
}

func TestParseToolError(t *testing.T) {
	if got := ParseToolError([]byte(`{"error":{"code":"","message":"m"}}`), nil); got != nil {
		t.Fatalf("empty code parsed: %+v", got)
	}
	if got := ParseToolError([]byte("not json"), []byte("")); got != nil {
		t.Fatalf("garbage parsed: %+v", got)
	}
}

func TestCallTimeoutDefault(t *testing.T) {
	if DefaultTimeout != 10*time.Second {
		t.Fatalf("DefaultTimeout = %v", DefaultTimeout)
	}
	if got := (Call{}).timeout(); got != DefaultTimeout {
		t.Fatalf("zero Timeout resolves to %v", got)
	}
	if got := (Call{Timeout: 3 * time.Second}).timeout(); got != 3*time.Second {
		t.Fatalf("explicit Timeout resolves to %v", got)
	}
}

func TestMissingBinary(t *testing.T) {
	_, err := Run(context.Background(), Call{Bin: "ork-no-such-binary-xyz"})
	if err == nil || !strings.Contains(err.Error(), "ork-no-such-binary-xyz") {
		t.Fatalf("error: %v", err)
	}
	var ee *ExitError
	var te *TimeoutError
	if errors.As(err, &ee) || errors.As(err, &te) {
		t.Fatalf("a start failure is not an exit or a timeout: %v", err)
	}
}

func TestParentCancelIsNotATimeout(t *testing.T) {
	call := helper(t, time.Minute, "block")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	_, err := Run(ctx, call)
	var te *TimeoutError
	if errors.As(err, &te) || !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled and no *TimeoutError, got %T: %v", err, err)
	}
}

func TestRunJSON(t *testing.T) {
	var v struct {
		A int `json:"a"`
	}
	// Unknown fields are ignored.
	if err := RunJSON(context.Background(), helper(t, 0, "emit", `{"a": 7, "extra": [1]}`, "", "0"), &v); err != nil || v.A != 7 {
		t.Fatalf("RunJSON: %+v, %v", v, err)
	}
	err := RunJSON(context.Background(), helper(t, 0, "emit", `not json`, "", "0"), &v)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("RunJSON on garbage: %v", err)
	}
}
