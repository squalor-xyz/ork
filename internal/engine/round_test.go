// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/squalor-xyz/ork/internal/runrec"
)

func requireDirectoryMode0700(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("%s mode %s, want directory 0700", path, info.Mode())
	}
}

func TestRunDirectoriesAre0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix modes")
	}
	h := newHarness(t)
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	requireDirectoryMode0700(t, h.store.Dir)
	requireDirectoryMode0700(t, h.store.RunDir(rec.ID))
	reqs := h.runner.requests()
	if len(reqs) == 0 {
		t.Fatal("no role requests")
	}
	for _, req := range reqs {
		requireDirectoryMode0700(t, filepath.Dir(req.TempDir))
		requireDirectoryMode0700(t, req.TempDir)
	}
}

func TestSpecCheckTempDirectoryIs0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix modes")
	}
	h := specHarness(t)
	cfg := specConfig(h, t)
	if _, err := RunSpecCheck(context.Background(), h.deps, cfg); err != nil {
		t.Fatal(err)
	}
	requireDirectoryMode0700(t, filepath.Join(cfg.Dir, "tmp"))
}

func TestVendorReportModeIsLeftAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix modes")
	}
	h := newHarness(t)
	h.runner.script = func(req Request, n int) (Result, error) {
		result := defaultResult(req)
		if err := os.WriteFile(req.ReportPath, []byte(result.Report), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(req.ReportPath, 0o644); err != nil {
			t.Fatal(err)
		}
		return result, nil
	}
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	for _, req := range h.runner.requests() {
		info, err := os.Stat(req.ReportPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode %04o, want 0644", req.ReportPath, info.Mode().Perm())
		}
		data, err := os.ReadFile(req.ReportPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != defaultResult(req).Report {
			t.Errorf("vendor report changed: %q", data)
		}
	}
}
