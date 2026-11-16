// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/runrec"
)

func checksSpec(body string) string {
	var spec map[string]any
	_ = json.Unmarshal([]byte(sliceJSON()), &spec)
	s := spec["slice"].(map[string]any)
	s["sections"] = append(s["sections"].([]any), map[string]string{"heading": "Check", "body": body})
	b, _ := json.Marshal(spec)
	return string(b)
}

func sliceChecksHarness(t *testing.T, body string) *harness {
	t.Helper()
	h := newHarness(t, func(o *harnessOpts) {
		o.routes = []clitest.Route{{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: checksSpec(body)}}
		o.routesFor = func(P, W string) []clitest.Route {
			return []clitest.Route{
				{Tool: "git", Args: []string{"-C", W, "ls-files", "--cached", "--others", "--exclude-standard", "-z"}, Stdout: "go\u0000gofmt\u0000"},
				{Tool: "git", Args: []string{"-C", W, "rev-parse", "--show-toplevel"}, Stdout: W},
				{Tool: "git", Args: []string{"-C", W, "symbolic-ref", "--short", "HEAD"}, Stdout: item},
				{Tool: "git", Args: []string{"-C", W, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git")},
				{Tool: "git", Args: []string{"-C", P, "rev-parse", "--git-common-dir"}, Stdout: filepath.Join(P, ".git")},
			}
		}
	})
	for _, name := range []string{"go", "gofmt"} {
		writeCheckTool(t, h.W, name, "printf '%s:%s:%s\\n' \"$PWD\" \"$GOOS\" \"$*\"")
	}
	t.Setenv("PATH", h.W+string(os.PathListSeparator)+os.Getenv("PATH"))
	return h
}

func writeCheckTool(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	writeFile(t, p, "#!/bin/sh\n"+body+"\n")
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestSliceChecksRunBeforeGateWithEnvironmentAndEvidence(t *testing.T) {
	h := sliceChecksHarness(t, "- `go test -race ./pkg`, `GOOS=linux go vet ./pkg`, `gofmt -l .`.")
	h.cfg.Lineup[RoleArchitect] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}}
	for _, name := range []string{"go", "gofmt"} {
		writeCheckTool(t, h.W, name, fmt.Sprintf("[ -f '%s' ] || exit 9\nprintf '%%s:%%s:%%s\\n' \"$PWD\" \"$GOOS\" \"$*\"", filepath.Join(h.W, "architect-reviewed")))
	}
	h.runner.script = func(req Request, n int) (Result, error) {
		if req.Role == RoleArchitect {
			writeFile(t, filepath.Join(h.W, "architect-reviewed"), "reviewed\n")
			return Result{Exit: ExitOK, Report: "No blockers.\n\n## Owner needed\n\nnone\n"}, nil
		}
		return Result{}, nil
	}
	e := h.start()
	rec := requireState(t, e, runrec.StateAwaitingApproval)
	for _, command := range []string{"go test -race ./pkg", "GOOS=linux go vet ./pkg", "gofmt -l ."} {
		found := false
		for _, row := range rec.Checks {
			if row.Command != command {
				continue
			}
			if !row.Slice || !row.OK || row.Exit != 0 {
				t.Fatalf("successful slice row status: %+v", row)
			}
			b, err := os.ReadFile(filepath.Join(e.run.Dir(), row.Log))
			want := h.W + "::" + command
			if strings.HasPrefix(command, "GOOS=") {
				want = h.W + ":linux:go vet ./pkg"
			}
			// The fake prints argv without the executable.
			want = strings.ReplaceAll(strings.ReplaceAll(want, ":go ", ":"), ":gofmt ", ":")
			if err != nil || !strings.Contains(string(b), want) {
				t.Fatalf("%s log = %q, %v; want %q", command, b, err, want)
			}
			for _, req := range h.runner.requests() {
				if req.Role == RoleGate && !strings.Contains(req.Brief, fmt.Sprintf("Command: %s\nCheck log: %s\nExit status: 0", command, row.Log)) {
					t.Fatalf("gate missing row evidence: %s", command)
				}
			}
			found = true
		}
		if !found {
			t.Errorf("missing slice check row: %s", command)
		}
	}
}

func TestSliceChecksFailuresReachFixBrief(t *testing.T) {
	for _, tc := range []struct{ name, command, body string }{
		{"exit", "go test ./pkg", "echo broken-row; exit 7"},
		{"output", "gofmt -l .", "echo unformatted.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := "- `" + tc.command + "`"
			if tc.name == "output" {
				entry += " must print nothing."
			}
			h := sliceChecksHarness(t, entry)
			writeCheckTool(t, h.W, strings.Fields(tc.command)[0], tc.body)
			h.runner.script = func(req Request, n int) (Result, error) {
				if req.Role == RoleImplementer && req.Round == 2 {
					want := "broken-row"
					if tc.name == "output" {
						want = "unformatted.go"
					}
					if !strings.Contains(req.Brief, want) || !strings.Contains(req.Brief, tc.command) {
						t.Errorf("fix brief missing row failure %s", want)
					}
					writeCheckTool(t, h.W, strings.Fields(tc.command)[0], "exit 0")
				}
				return Result{}, nil
			}
			e := h.start()
			if rec := requireState(t, e, runrec.StateAwaitingApproval); rec.Round != 2 {
				t.Fatalf("failed slice row did not send a fix round: %d", rec.Round)
			}
			if tc.name == "exit" {
				for _, row := range e.Snapshot().Checks {
					if row.Command == tc.command && row.Round == 1 && row.Exit != 7 {
						t.Fatalf("failure exit = %d, want 7", row.Exit)
					}
				}
			}
		})
	}
}

func TestSliceChecksRecordUnsupportedCommands(t *testing.T) {
	commands := []string{"go test ./pkg | cat", "go test ./pkg && go vet ./pkg", "go -C /outside test ./pkg", "gofmt ./../../outside", "go test dir/../../other"}
	body := ""
	for _, cmd := range commands {
		body += "- `" + cmd + "`\n"
	}
	h := sliceChecksHarness(t, body+"- HUMAN CHECK: `go test ./human`\n- [AFTER LAND] `go test ./later`")
	trace := filepath.Join(t.TempDir(), "unsupported-output")
	t.Setenv("UNSUPPORTED_TRACE", trace)
	for _, tool := range []string{"go", "gofmt"} {
		writeCheckTool(t, h.W, tool, "printf '%s\\n' \"$*\" >> \"$UNSUPPORTED_TRACE\"")
	}
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	for _, cmd := range commands {
		found := false
		for _, row := range e.Snapshot().Checks {
			if row.Command == cmd {
				b, err := os.ReadFile(filepath.Join(e.run.Dir(), row.Log))
				if err != nil || row.NotRun != "not run by ork: orchestrator" || !strings.Contains(string(b), row.NotRun) {
					t.Errorf("unsupported row ran or lost its log: %+v %q %v", row, b, err)
				}
				for _, req := range h.runner.requests() {
					if req.Role == RoleGate {
						_, evidence, _ := strings.Cut(req.Brief, "## Ork slice check results\n\n")
						if !strings.Contains(evidence, fmt.Sprintf("Command: %s\nCheck log: %s\nExit status: not run by ork: orchestrator", cmd, row.Log)) {
							t.Errorf("gate missing orchestrator row: %s", cmd)
						}
					}
				}
				found = true
			}
		}
		if !found {
			t.Errorf("missing unsupported row: %s", cmd)
		}
	}
	for _, row := range e.Snapshot().Checks {
		if strings.Contains(row.Command, "./human") || strings.Contains(row.Command, "./later") {
			t.Errorf("deferred human/landing row was run: %s", row.Command)
		}
	}
	if b, err := os.ReadFile(trace); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unsupported tool executed: %q, %v", b, err)
	}
}

func TestSliceChecksRepeatOnlyAfterProductChanges(t *testing.T) {
	for _, name := range []string{"docs/guide.md", ".slicer/extra", "product.go", "internal/roles/prompts/gate.md", "CHANGELOG.md", "decisions/example.txt"} {
		t.Run(name, func(t *testing.T) {
			h := sliceChecksHarness(t, "- `go test ./pkg`")
			trace := filepath.Join(t.TempDir(), "check-output")
			t.Setenv("CHECK_TRACE", trace)
			t.Setenv("CHECK_PRODUCT", name)
			writeCheckTool(t, h.W, "go", "if [ -f \"$CHECK_PRODUCT\" ]; then cat \"$CHECK_PRODUCT\"; else printf 'initial\\n'; fi >> \"$CHECK_TRACE\"")
			// A real repository supplies content fingerprints, independently of the fake CLI.
			gitRun(t, h.W, "init", "-q", "-b", "main")
			gitRun(t, h.W, "add", "-A")
			gitRun(t, h.W, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "seed")
			e, err := New(context.Background(), h.deps, h.cfg)
			if err != nil {
				t.Fatal(err)
			}
			e.d.GitBin = "git"
			e.run.Update(func(r *runrec.Record) { r.State = runrec.StateGate })
			h.runner.script = func(req Request, n int) (Result, error) {
				if req.Role == RoleGate && req.Round == 1 {
					return Result{Exit: ExitOK, Report: gateFail}, nil
				}
				return Result{}, nil
			}
			if err := e.gate(context.Background()); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(h.W, name), "changed\n")
			if err := e.gate(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := "initial\n"
			if name != "docs/guide.md" && name != ".slicer/extra" {
				want += "changed\n"
			}
			got, err := os.ReadFile(trace)
			if err != nil || string(got) != want {
				t.Fatalf("%s: check output = %q, %v; want %q", name, got, err, want)
			}
		})
	}
}

func TestSliceChecksCancelledCommandRunsAgain(t *testing.T) {
	h := sliceChecksHarness(t, "- `go test ./pkg`")
	started := filepath.Join(t.TempDir(), "started")
	release := filepath.Join(t.TempDir(), "release")
	t.Setenv("CHECK_STARTED", started)
	t.Setenv("CHECK_RELEASE", release)
	writeCheckTool(t, h.W, "go", "if [ -f \"$CHECK_RELEASE\" ]; then printf 'recovered\\n'; else printf started > \"$CHECK_STARTED\"; while :; do :; done; fi")
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.run.Update(func(r *runrec.Record) { r.State = runrec.StateGate }); err != nil {
		t.Fatal(err)
	}
	before := e.Snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.gate(ctx) }()
	// Always cancel and join the gate goroutine, even if startup times out.
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("slice command did not start")
		case <-tick.C:
		}
	}
	cancel()
	err = <-done
	joined = true
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled gate error = %v, want context canceled", err)
	}
	if rec := e.Snapshot(); rec.Round != before.Round || rec.State != before.State || len(rec.Checks) != 0 {
		t.Errorf("cancellation recorded evidence or spent a round: round=%d state=%s checks=%+v", rec.Round, rec.State, rec.Checks)
	}
	writeFile(t, release, "release\n")
	if err := e.gate(context.Background()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range e.Snapshot().Checks {
		b, err := os.ReadFile(filepath.Join(e.run.Dir(), row.Log))
		if err == nil && row.OK && strings.Contains(string(b), "recovered") {
			found = true
		}
	}
	if !found {
		t.Error("next live gate did not execute unchanged slice command: no recovered output")
	}
}

func TestSliceChecksMissingCommandPauses(t *testing.T) {
	h := sliceChecksHarness(t, "- `ork-missing-slice-check`")
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.run.Update(func(r *runrec.Record) { r.State = runrec.StateGate }); err != nil {
		t.Fatal(err)
	}
	before := e.Snapshot().Round
	if err := e.gate(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := e.Snapshot()
	if rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.State != runrec.StateGate || rec.Round != before || len(rec.Checks) != 0 {
		t.Errorf("missing command must pause without evidence or a spent round: state=%s round=%d pending=%+v checks=%+v", rec.State, rec.Round, rec.Pending, rec.Checks)
	}
}

func TestSliceChecksLandingRechecksMergedTree(t *testing.T) {
	env := newLandEnv(t)
	writeFile(t, filepath.Join(env.fakes, "show.json"), checksSpec("- `git show HEAD:base.txt`"))
	env.moveBase("base.txt", "merged-base-evidence\n")
	env.approve()
	rec := requireState(t, env.e, runrec.StateCommitted)
	for _, row := range rec.Checks {
		if row.Command == "git show HEAD:base.txt" {
			b, err := os.ReadFile(filepath.Join(env.e.run.Dir(), row.Log))
			if err != nil || !strings.Contains(string(b), "merged-base-evidence") {
				t.Fatalf("landing row log = %q, %v", b, err)
			}
			return
		}
	}
	t.Fatal(fmt.Sprintf("landing did not record slice row: %+v", rec.Checks))
}
