// SPDX-License-Identifier: GPL-3.0-or-later

// Package clitest fakes the slicer and adj CLIs for tests with the standard
// library's helper-process pattern. Install makes a directory holding
// symlinks named "slicer" and "adj" to the running test binary. When the test
// binary is started through one of them, Main (called first in TestMain)
// replays the recorded file that matches the arguments, and logs the call.
// Nothing here runs the real slicer or adj.
package clitest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	envRoutes = "ORK_CLITEST_ROUTES"
	envLog    = "ORK_CLITEST_LOG"
)

// Route answers one call: the tool ("slicer" or "adj") run with exactly Args.
// It prints File's bytes (or Stdout when File is empty) on stdout, Stderr on
// stderr, and exits with Exit.
type Route struct {
	Tool   string
	Args   []string
	File   string
	Stdout string
	Stderr string
	Exit   int
}

// Call is one logged invocation of a fake.
type Call struct {
	Tool string
	Args []string
}

// Fake is an installed set of routes.
type Fake struct {
	Dir     string // holds the "slicer" and "adj" symlinks
	logPath string
}

// Bin returns the path of the fake binary for tool.
func (f *Fake) Bin(tool string) string { return filepath.Join(f.Dir, tool) }

// Calls returns every call made so far, in order.
func (f *Fake) Calls() []Call {
	data, err := os.ReadFile(f.logPath)
	if err != nil {
		return nil
	}
	var calls []Call
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var c Call
		if err := json.Unmarshal([]byte(line), &c); err == nil {
			calls = append(calls, c)
		}
	}
	return calls
}

// CallsTo returns the calls made to tool.
func (f *Fake) CallsTo(tool string) []Call {
	var out []Call
	for _, c := range f.Calls() {
		if c.Tool == tool {
			out = append(out, c)
		}
	}
	return out
}

// Install creates the fakes for the test and points the environment at them.
// The test must not run in parallel, because it sets environment variables.
func Install(t *testing.T, routes ...Route) *Fake {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, tool := range []string{"slicer", "adj"} {
		if err := os.Symlink(exe, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(routes)
	if err != nil {
		t.Fatal(err)
	}
	routesPath := filepath.Join(dir, "routes.json")
	if err := os.WriteFile(routesPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv(envRoutes, routesPath)
	t.Setenv(envLog, logPath)
	return &Fake{Dir: dir, logPath: logPath}
}

// Main must be the first call in TestMain. When the process was started as a
// fake CLI it serves the call and exits; otherwise it returns.
func Main() {
	routesPath := os.Getenv(envRoutes)
	tool := filepath.Base(os.Args[0])
	if routesPath == "" || (tool != "slicer" && tool != "adj") {
		return
	}
	args := os.Args[1:]
	if logPath := os.Getenv(envLog); logPath != "" {
		line, _ := json.Marshal(Call{Tool: tool, Args: args})
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			f.Write(append(line, '\n'))
			f.Close()
		}
	}
	data, err := os.ReadFile(routesPath)
	if err != nil {
		fail("cannot read routes: %v", err)
	}
	var routes []Route
	if err := json.Unmarshal(data, &routes); err != nil {
		fail("cannot parse routes: %v", err)
	}
	for _, r := range routes {
		if r.Tool != tool || !equal(r.Args, args) {
			continue
		}
		out := []byte(r.Stdout)
		if r.File != "" {
			if out, err = os.ReadFile(r.File); err != nil {
				fail("cannot read %s: %v", r.File, err)
			}
		}
		os.Stdout.Write(out)
		os.Stderr.WriteString(r.Stderr)
		os.Exit(r.Exit)
	}
	fail("no route for %s %s", tool, strings.Join(args, " "))
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "clitest: "+format+"\n", a...)
	os.Exit(97)
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
