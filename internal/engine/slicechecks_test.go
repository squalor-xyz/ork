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
	// A chain with a part ork would not run stays one orchestrator row (S406).
	commands := []string{"go test ./pkg | cat", "go test ./pkg && go -C /outside test ./pkg", "go -C /outside test ./pkg", "gofmt ./../../outside", "go test dir/../../other"}
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

// S406 (adj 454): a plain && chain is run as its parts, in order. The first
// failure stops the chain; the rest are recorded as not run.
func TestSliceChecksRunAnAndChain(t *testing.T) {
	h := sliceChecksHarness(t, "- `true && false && true`")
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	run, paused, err := e.runSliceChecks(context.Background(), e.Snapshot(), runrec.StateGate, "slice-checks", false)
	if err != nil || paused {
		t.Fatalf("runSliceChecks: paused=%v err=%v", paused, err)
	}
	if run.ok {
		t.Error("a chain with a failed part must fail the check")
	}
	var rows []runrec.CheckRecord
	for _, c := range e.Snapshot().Checks {
		if c.Slice {
			rows = append(rows, c)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("got %d check records, want 3 (one per part): %+v", len(rows), rows)
	}
	for i, command := range []string{"true", "false", "true"} {
		if rows[i].Command != command {
			t.Errorf("row %d command = %q, want %q", i, rows[i].Command, command)
		}
	}
	if !rows[0].OK || rows[0].Exit != 0 || rows[0].NotRun != "" {
		t.Errorf("first part = %+v, want OK", rows[0])
	}
	if rows[1].OK || rows[1].Exit != 1 || rows[1].NotRun != "" {
		t.Errorf("second part = %+v, want failed with exit 1", rows[1])
	}
	if rows[2].OK || rows[2].NotRun != "not run by ork: an earlier part of the chain failed" {
		t.Errorf("third part = %+v, want NotRun after an earlier failure", rows[2])
	}
	if b, err := os.ReadFile(filepath.Join(e.run.Dir(), rows[2].Log)); err != nil || !strings.Contains(string(b), rows[2].NotRun) {
		t.Errorf("third part log = %q, %v", b, err)
	}
	if run.log != rows[1].Log {
		t.Errorf("failed check log = %q, want the failed part's log %q", run.log, rows[1].Log)
	}
}

// S406 review: "must print nothing" on a chain line binds every part, as it does
// for a shell run of the whole line: output from the first part fails the line.
func TestSliceChecksChainMustPrintNothingBindsEveryPart(t *testing.T) {
	h := sliceChecksHarness(t, "- `echo x && true` must print nothing.")
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	run, paused, err := e.runSliceChecks(context.Background(), e.Snapshot(), runrec.StateGate, "slice-checks", false)
	if err != nil || paused {
		t.Fatalf("runSliceChecks: paused=%v err=%v", paused, err)
	}
	if run.ok {
		t.Error("output from the first part of a must-print-nothing chain must fail the check")
	}
	var rows []runrec.CheckRecord
	for _, c := range e.Snapshot().Checks {
		if c.Slice {
			rows = append(rows, c)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("got %d check records, want 2: %+v", len(rows), rows)
	}
	if rows[0].Command != "echo x" || rows[0].OK || rows[0].NotRun != "" {
		t.Errorf("first part = %+v, want red from its output", rows[0])
	}
	if b, err := os.ReadFile(filepath.Join(e.run.Dir(), rows[0].Log)); err != nil || !strings.Contains(string(b), "check must print nothing") {
		t.Errorf("first part log = %q, %v", b, err)
	}
	if rows[1].Command != "true" || rows[1].OK || rows[1].NotRun != notRunChainFailed {
		t.Errorf("second part = %+v, want not run after the red first part", rows[1])
	}
}

// printsNothingRow runs the Check body with a fake tool that runs script, and
// returns the one slice check row it records and that row's log (S419).
func printsNothingRow(t *testing.T, body, tool, script string) (runrec.CheckRecord, string) {
	t.Helper()
	h := sliceChecksHarness(t, body)
	writeCheckTool(t, h.W, tool, script)
	e, err := New(context.Background(), h.deps, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, paused, err := e.runSliceChecks(context.Background(), e.Snapshot(), runrec.StateGate, "slice-checks", false); err != nil || paused {
		t.Fatalf("runSliceChecks: paused=%v err=%v", paused, err)
	}
	var rows []runrec.CheckRecord
	for _, c := range e.Snapshot().Checks {
		if c.Slice {
			rows = append(rows, c)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("got %d check records, want 1: %+v", len(rows), rows)
	}
	b, err := os.ReadFile(filepath.Join(e.run.Dir(), rows[0].Log))
	if err != nil {
		t.Fatal(err)
	}
	return rows[0], string(b)
}

// S419 (adj 468): a "prints nothing" rg that finds nothing exits 1 and passes.
func TestPrintsNothingRgNoMatchPasses(t *testing.T) {
	row, log := printsNothingRow(t, "- `rg -n ZZZ docs` prints nothing.", "rg", "exit 1")
	if !row.OK || row.Exit != 1 || row.NotRun != "" {
		t.Errorf("row = %+v, want OK on the no-match exit 1\n%s", row, log)
	}
}

func TestPrintsNothingRgMatchFails(t *testing.T) {
	row, log := printsNothingRow(t, "- `rg -n ZZZ docs` prints nothing.", "rg", "echo docs/a.md:1:ZZZ")
	if row.OK || !strings.Contains(log, "check must print nothing") {
		t.Errorf("row = %+v, want red with the must-print-nothing line\n%s", row, log)
	}
}

func TestPrintsNothingRgErrorFails(t *testing.T) {
	row, log := printsNothingRow(t, "- `rg -n ZZZ docs` prints nothing.", "rg", "exit 2")
	if row.OK || row.Exit != 2 || !strings.Contains(log, "exit 2") {
		t.Errorf("row = %+v, want red naming exit 2\n%s", row, log)
	}
}

func TestPrintsNothingOtherCommandExit1Fails(t *testing.T) {
	row, log := printsNothingRow(t, "- `gofmt -l .` prints nothing.", "gofmt", "exit 1")
	if row.OK || row.Exit != 1 || !strings.Contains(log, "exit 1") {
		t.Errorf("row = %+v, want red naming exit 1\n%s", row, log)
	}
}

// ork does not run a piped line, so the rule is a pure helper (S419).
func TestPrintsNothingPipedLineJudgedByLastCommand(t *testing.T) {
	for _, tc := range []struct {
		command string
		exit    int
		output  string
		want    bool
	}{
		{"rg -n A f | rg B", 1, "", true},
		{"rg -n A f | rg B", 1, "x\n", false},
		{"rg -n A f | rg B", 2, "", false},
		{"rg -n A f | gofmt -l", 1, "", false},
		{"gofmt -l . | rg B", 1, "", true},
		{"rg -n 'a|b' f", 1, "", true},
		{"git grep -n A", 1, "", true},
		{"grep -rn A docs", 1, "", true},
		{"rg -n A", 0, "", true},
		{"rg -n A", 0, " \n", true},
		{"rg -n A", 0, "hit\n", false},
		{"gofmt -l .", 1, "", false},
	} {
		if got := printsNothingOK(tc.command, tc.exit, tc.output); got != tc.want {
			t.Errorf("printsNothingOK(%q, %d, %q) = %v, want %v", tc.command, tc.exit, tc.output, got, tc.want)
		}
	}
}

func TestPrintsNothingWordingIsMarked(t *testing.T) {
	for _, tc := range []struct {
		entry string
		want  bool
	}{
		{"- `rg -n A f` prints nothing.", true},
		{"- `rg -n A f` must print nothing.", true},
		{"- `rg -n A f` Prints Nothing.", true},
		{"- `rg -n A f` finds the exception.", false},
	} {
		got := sliceCheckCommands(checksSpec(tc.entry))
		if len(got) != 1 || got[0].empty != tc.want {
			t.Errorf("%q: commands = %+v, want empty = %v", tc.entry, got, tc.want)
		}
	}
}

// S241: an owner's "tighten" left a backticked `--yes` in the Check. It is not
// a command: ork records and names it, does not run it, and does not pause.
func TestSliceChecksSkipAFlagOnlyEntry(t *testing.T) {
	h := sliceChecksHarness(t, "- `go vet ./...`\n- Drop the `--yes` flags from the plan check.")
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	var flagRow, goRow *runrec.CheckRecord
	checks := e.Snapshot().Checks
	for i := range checks {
		switch checks[i].Command {
		case "--yes":
			flagRow = &checks[i]
		case "go vet ./...":
			goRow = &checks[i]
		}
	}
	if flagRow == nil {
		t.Fatalf("no row for the flag-only entry: %+v", checks)
	}
	if flagRow.OK || !strings.Contains(flagRow.NotRun, "not a command") || !strings.Contains(flagRow.NotRun, "--yes") {
		t.Errorf("flag row = %+v, want NotRun naming it as not a command", *flagRow)
	}
	if b, err := os.ReadFile(filepath.Join(e.run.Dir(), flagRow.Log)); err != nil || !strings.Contains(string(b), "not a command") {
		t.Errorf("flag row log = %q, %v", b, err)
	}
	if goRow == nil || !goRow.OK || goRow.NotRun != "" {
		t.Errorf("the real command did not run: %+v", goRow)
	}
}

// requireCheckCommands starts a run on a slice with the given Check body and
// requires its slice check records to name exactly want, in order, all OK.
func requireCheckCommands(t *testing.T, body string, want ...string) {
	t.Helper()
	h := sliceChecksHarness(t, body)
	e := h.start()
	requireState(t, e, runrec.StateAwaitingApproval)
	var got []string
	for _, c := range e.Snapshot().Checks {
		if !c.Slice {
			continue // the project's own check row
		}
		got = append(got, c.Command)
		if !c.OK {
			t.Errorf("check %q is not OK: %+v", c.Command, c)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("check commands = %q, want %q", got, want)
	}
}

// S407: bullets under "Acceptance criteria:" are prose, never commands.
func TestSliceChecksSkipAcceptanceCriteria(t *testing.T) {
	requireCheckCommands(t, "Acceptance criteria:\n- the run prints `friction` once\n\nCommands:\n- `true` exits 0.", "true")
}

func TestSliceChecksWithoutCriteriaUnchanged(t *testing.T) {
	requireCheckCommands(t, "- `true` exits 0.\n- `go vet ./...` exits 0.", "true", "go vet ./...")
}

func TestSliceChecksCriteriaWithoutCommandsLine(t *testing.T) {
	requireCheckCommands(t, "- `true` exits 0.\n\nAcceptance criteria:\n- the run prints `friction` once\n- and `nothing-here` too", "true")
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
