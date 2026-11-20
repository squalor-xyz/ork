// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package engine

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/runrec"
)

// fifoEngine is an engine that was not run, and a FIFO named name in its run
// directory, where a role could have left one in place of its report.
func fifoEngine(t *testing.T, name string) (*Engine, string) {
	t.Helper()
	h := newHarness(t)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.run.Dir(), name)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return e, path
}

// within fails the test when f does not return in 2 s, as a read that blocks on
// a FIFO does not.
func within(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked on a FIFO")
	}
}

func TestReadReportFallsBackWhenTheReportIsAFIFO(t *testing.T) {
	e, path := fifoEngine(t, "implementer-1.md")
	var text, file string
	within(t, func() { text, file = e.readReport(path, "implementer-1.md", "fallback text") })
	if text != "fallback text" || file != "" {
		t.Errorf("readReport = %q, %q; want the fallback and no file", text, file)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("the report path is no longer a FIFO: %v, %v", info, err)
	}
}

func TestEarlierReportsSkipAFIFO(t *testing.T) {
	e, _ := fifoEngine(t, "implementer-1.md")
	if err := os.WriteFile(filepath.Join(e.run.Dir(), "reviewer-1.md"), []byte("Text two"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := runrec.Record{Roles: []runrec.RoleRecord{
		{Role: RoleImplementer, Round: 1, Report: "implementer-1.md"},
		{Role: RoleReviewer, Round: 1, Report: "reviewer-1.md"},
	}}
	var got []EarlierReport
	within(t, func() { got = e.earlierReports(rec) })
	if len(got) != 1 || got[0].Text != "Text two" {
		t.Errorf("earlierReports = %+v, want only Text two", got)
	}
}
