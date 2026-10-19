// SPDX-License-Identifier: GPL-3.0-or-later

package clitest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
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
