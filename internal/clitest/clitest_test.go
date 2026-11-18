// SPDX-License-Identifier: GPL-3.0-or-later

package clitest

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "--clitest-hold-lock" {
		unlock := lock(os.Args[2])
		os.Stdout.WriteString("locked\n")
		var b [1]byte
		os.Stdin.Read(b[:])
		unlock()
		os.Exit(0)
	}
	Main()
	os.Exit(m.Run())
}

func run(t *testing.T, f *Fake, tool string, args ...string) (string, int) {
	t.Helper()
	out, err := exec.Command(f.Bin(tool), args...).Output()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestPrefixRouteMatchesLongerCalls(t *testing.T) {
	f := Install(t,
		Route{Tool: "git", Args: []string{"status", "-s", "x"}, Stdout: "exact\n"},
		Route{Tool: "git", Args: []string{"status", "-s"}, Prefix: true, Stdout: "prefix\n"},
	)
	if out, _ := run(t, f, "git", "status", "-s", "x"); out != "exact\n" {
		t.Errorf("exact route first: %q", out)
	}
	if out, _ := run(t, f, "git", "status", "-s", "y", "z"); out != "prefix\n" {
		t.Errorf("prefix route: %q", out)
	}
	if out, code := run(t, f, "git", "status"); out != "" || code != 97 {
		t.Errorf("a shorter call must not match the prefix route: %q, exit %d", out, code)
	}
	if out, code := run(t, f, "git", "status", "-x"); out != "" || code != 97 {
		t.Errorf("a different argument must not match the prefix route: %q, exit %d", out, code)
	}
}

// Concurrent calls (an engine's parts run at the same time) must each get their
// own Seq reply, and the reply of a call must be its place in the call log.
func TestSeqAnswersConcurrentCallsOnceEachInLogOrder(t *testing.T) {
	const n = 40
	var seq []Reply
	for i := 1; i <= n; i++ {
		seq = append(seq, Reply{Stdout: strconv.Itoa(i)})
	}
	f := Install(t, Route{Tool: "slicer", Args: []string{"x"}, Prefix: true, Seq: seq})
	replies := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, _ := run(t, f, "slicer", "x", strconv.Itoa(i))
			replies[i] = out
		}()
	}
	wg.Wait()
	calls := f.CallsTo("slicer")
	if len(calls) != n {
		t.Fatalf("%d calls logged, want %d", len(calls), n)
	}
	for k, c := range calls {
		i, _ := strconv.Atoi(c.Args[1])
		if want := strconv.Itoa(k + 1); replies[i] != want {
			t.Fatalf("the call logged at place %d got reply %q, want %q", k+1, replies[i], want)
		}
	}
}

func TestSeqAnswersCallsInOrderThenRepeatsTheLast(t *testing.T) {
	f := Install(t, Route{Tool: "slicer", Args: []string{"x"}, Seq: []Reply{
		{Stdout: "one\n"}, {Stdout: "two\n", Exit: 3}, {Stdout: "three\n"},
	}})
	var got []string
	for range 4 {
		out, code := run(t, f, "slicer", "x")
		got = append(got, strings.TrimSpace(out)+":"+string(rune('0'+code)))
	}
	if want := "one:0 two:3 three:0 three:0"; strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}
