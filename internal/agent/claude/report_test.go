// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package claude

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestClaudeReadReportUsesTheResultWhenTheFileIsAFIFO(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "report.md")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	type got struct {
		text string
		src  ReportSource
	}
	done := make(chan got, 1)
	go func() {
		text, src := ReadReport(fifo, &Result{Text: "final"})
		done <- got{text, src}
	}()
	select {
	case g := <-done:
		if g.text != "final" || g.src != ReportFromResult {
			t.Errorf("ReadReport = %q, %q; want the result text", g.text, g.src)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked on a FIFO")
	}
}
