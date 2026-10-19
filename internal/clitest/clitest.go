// SPDX-License-Identifier: GPL-3.0-or-later

// Package clitest fakes the slicer and adj CLIs for tests with the standard
// library's helper-process pattern. Install makes a directory holding
// symlinks named "slicer", "adj" and "git" to the running test binary. When the test
// binary is started through one of them, Main (called first in TestMain)
// replays the recorded file that matches the arguments, and logs the call.
// Nothing here runs the real slicer or adj.
package clitest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	envRoutes = "ORK_CLITEST_ROUTES"
	envLog    = "ORK_CLITEST_LOG"
)

// Route answers one call: the tool ("slicer", "adj" or "git") run with exactly
// Args, or with Args as its first arguments when Prefix is set. It prints
// File's bytes (or Stdout when File is empty) on stdout, Stderr on stderr, and
// exits with Exit. When Seq is set it answers instead: the n-th call the route
// matches (from 0) gets Seq[n], and every call past the end gets the last one.
// The first route that matches wins, so a specific route goes before a
// prefix route.
type Route struct {
	Tool   string
	Args   []string
	Prefix bool
	File   string
	Stdout string
	Stderr string
	Exit   int
	Seq    []Reply
}

// Reply is one answer of a Route's Seq.
type Reply struct {
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
	for _, tool := range []string{"slicer", "adj", "git"} {
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
	// A fake started from a test binary built with -race would sleep one
	// second at exit, which is 30 seconds for a test that makes 30 calls.
	if os.Getenv("GORACE") == "" {
		t.Setenv("GORACE", "atexit_sleep_ms=0")
	}
	return &Fake{Dir: dir, logPath: logPath}
}

// Main must be the first call in TestMain. When the process was started as a
// fake CLI it serves the call and exits; otherwise it returns.
func Main() {
	routesPath := os.Getenv(envRoutes)
	tool := filepath.Base(os.Args[0])
	if routesPath == "" || (tool != "slicer" && tool != "adj" && tool != "git") {
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
	for i, r := range routes {
		if r.Tool != tool || !matches(r, args) {
			continue
		}
		if len(r.Seq) > 0 {
			n := nextCount(filepath.Join(filepath.Dir(routesPath), fmt.Sprintf("seq-%d", i)))
			rep := r.Seq[min(n, len(r.Seq)-1)]
			os.Stdout.WriteString(rep.Stdout)
			os.Stderr.WriteString(rep.Stderr)
			os.Exit(rep.Exit)
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

func matches(r Route, args []string) bool {
	if r.Prefix {
		return len(args) >= len(r.Args) && equal(r.Args, args[:len(r.Args)])
	}
	return equal(r.Args, args)
}

// nextCount returns how many times it was called before, counting in a file.
// Calls to a fake are sequential, so the file needs no lock.
func nextCount(path string) int {
	n := 0
	if b, err := os.ReadFile(path); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	os.WriteFile(path, []byte(strconv.Itoa(n+1)), 0o644)
	return n
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
