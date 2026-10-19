// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// The tests fake the claude CLI with the helper-process pattern, as clirun's
// tests do: the test binary runs itself with ORK_CLAUDE_HELPER=1 and
// TestMain serves the call from the plan in ORK_CLAUDE_PLAN. No test starts
// the real claude, and none makes a model call.

const (
	envHelper = "ORK_CLAUDE_HELPER"
	envPlan   = "ORK_CLAUDE_PLAN"
)

// plan tells the fake what to do.
type plan struct {
	Mode       string `json:"mode"`                  // replay, gated, spawn, leave, block
	Stdout     string `json:"stdout,omitempty"`      // file replayed on stdout
	Stderr     string `json:"stderr,omitempty"`      // file replayed on stderr
	Exit       int    `json:"exit"`                  //
	ArgsLog    string `json:"args_log,omitempty"`    // file that receives the arguments
	Gate       string `json:"gate,omitempty"`        // gated: file to wait for after the first line
	PidFile    string `json:"pid_file,omitempty"`    // spawn: file that receives the grandchild pid
	ChildHolds bool   `json:"child_holds,omitempty"` // spawn: grandchild inherits stdout
}

func TestMain(m *testing.M) {
	if os.Getenv(envHelper) == "1" {
		helperMain(os.Args[1:])
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperMain(args []string) {
	var p plan
	if err := json.Unmarshal([]byte(os.Getenv(envPlan)), &p); err != nil {
		fmt.Fprintln(os.Stderr, "fake claude: bad plan:", err)
		os.Exit(99)
	}
	if p.ArgsLog != "" {
		data, _ := json.Marshal(args)
		os.WriteFile(p.ArgsLog, data, 0o644)
	}
	switch p.Mode {
	case "replay":
		replay(p)
		os.Exit(p.Exit)
	case "gated": // print the first stdout line, wait for the gate file, print the rest
		data, err := os.ReadFile(p.Stdout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(99)
		}
		lines := splitLines(data)
		os.Stdout.Write(lines[0])
		for {
			if _, err := os.Stat(p.Gate); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		for _, l := range lines[1:] {
			os.Stdout.Write(l)
		}
		os.Exit(p.Exit)
	case "spawn": // start a grandchild that blocks, record its pid, print one stream line, block
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), envPlan+`={"mode":"block"}`)
		if p.ChildHolds {
			child.Stdout = os.Stdout
		}
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(98)
		}
		os.WriteFile(p.PidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o644)
		fmt.Println(`{"type":"system","subtype":"init","session_id":"sess-cancel"}`)
		time.Sleep(time.Hour)
	case "leave": // start a grandchild that holds stdout, record its pid, replay stdout, exit
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), envPlan+`={"mode":"block"}`)
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(98)
		}
		os.WriteFile(p.PidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o644)
		replay(p)
		os.Exit(p.Exit)
	case "block":
		time.Sleep(time.Hour)
	}
	os.Exit(97)
}

func replay(p plan) {
	if p.Stdout != "" {
		data, err := os.ReadFile(p.Stdout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(99)
		}
		os.Stdout.Write(data)
	}
	if p.Stderr != "" {
		data, err := os.ReadFile(p.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(99)
		}
		os.Stderr.Write(data)
	}
}

// splitLines splits data after each newline, keeping the newlines.
func splitLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, data[start:i+1])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

// fakeSpec returns a Spec whose binary is this test binary playing claude.
// It also points HOME and XDG_STATE_HOME at a temp directory, so no test can
// reach the real ~/.local/state, and gives the round its own fixture dir.
func fakeSpec(t *testing.T, p plan) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return Spec{
		Bin: exe,
		// GORACE: the race runtime sleeps 1 s at exit by default, which would
		// keep every fake alive after it has finished.
		Env:        append(os.Environ(), envHelper+"=1", envPlan+"="+string(data), "GORACE=atexit_sleep_ms=0"),
		Prompt:     "Reply with exactly the word OK.",
		Model:      "haiku",
		FixtureDir: t.TempDir(),
	}
}
