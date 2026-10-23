// SPDX-License-Identifier: GPL-3.0-or-later

package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The fake is the test executable on PATH. It never invokes a vendor CLI.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "codex" {
		if os.Getenv("ORK_CODEX_BLOCK") == "1" {
			for {
				time.Sleep(time.Hour)
			}
		}
		if path := os.Getenv("ORK_CODEX_CAPTURE"); path != "" {
			dir, _ := os.Getwd()
			b, _ := json.Marshal(struct {
				Dir  string
				Args []string
			}{dir, os.Args[1:]})
			if err := os.WriteFile(path, b, 0600); err != nil {
				os.Exit(99)
			}
		}
		os.Stdout.WriteString(os.Getenv("ORK_CODEX_OUTPUT"))
		os.Stderr.WriteString(os.Getenv("ORK_CODEX_STDERR"))
		if os.Getenv("ORK_CODEX_FAIL") == "1" {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func fake(t *testing.T, output string, fail bool) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(exe, filepath.Join(dir, "codex")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("ORK_CODEX_OUTPUT", output)
	t.Setenv("ORK_CODEX_FAIL", "0")
	if fail {
		t.Setenv("ORK_CODEX_FAIL", "1")
	}
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return Spec{Dir: dir, Prompt: "prompt", Model: "model", Effort: "high"}
}
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/vendors/codex/" + name + ".stdout")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func TestCodexArgsWriteRoleUsesWorkspaceWrite(t *testing.T) {
	s := Spec{Dir: "/work", Prompt: "brief", Model: "model", Effort: "high"}
	got, err := s.Args()
	want := []string{"exec", "--json", "-m", "model", "-c", "model_reasoning_effort=high", "-s", "workspace-write", "-C", "/work", "brief"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("args %q %v, want %q", got, err, want)
	}
}
func TestCodexArgsReadOnlyRoleUsesReadOnly(t *testing.T) {
	got, err := (Spec{Dir: "/work", Prompt: "brief", ReadOnly: true}).Args()
	want := []string{"exec", "--json", "-s", "read-only", "-C", "/work", "brief"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("args %q %v", got, err)
	}
}
func TestCodexArgsResumeTakesNoSandboxFlag(t *testing.T) {
	for _, ro := range []bool{false, true} {
		got, err := (Spec{Dir: "/work", Prompt: "brief", Model: "model", Effort: "low", Resume: "thread", ReadOnly: ro}).Args()
		mode := "workspace-write"
		if ro {
			mode = "read-only"
		}
		want := []string{"exec", "resume", "thread", "-m", "model", "--json", "-c", "model_reasoning_effort=low", "-c", `sandbox_mode="` + mode + `"`, "brief"}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("args %q %v, want %q", got, err, want)
		}
	}
}
func TestCodexArgsRefuseFlagLikeInputs(t *testing.T) {
	for _, s := range []Spec{{}, {Prompt: "  "}, {Prompt: "-x"}, {Prompt: "brief", Resume: "-x"}} {
		if _, err := s.Args(); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
}
func TestCodexParsesJSONLToARoleResult(t *testing.T) {
	s := fake(t, fixture(t, "01-plain-stream-jsonl"), false)
	starts := 0
	s.OnStart = func(pid int) {
		if pid <= 0 {
			t.Error("bad pid")
		}
		starts++
	}
	got, err := Run(context.Background(), s)
	if err != nil || got.Exit != ExitOK || got.Session != "01a151a0-a99b-7d81-83a1-b099d472f408" || got.Report != "OK" || got.Tokens != 17091 || starts != 1 {
		t.Fatalf("result %+v, %v, starts %d", got, err, starts)
	}
}
func TestCodexRateLimitStringsClassifyAsRateLimited(t *testing.T) {
	// Synthetic messages from decision 51, not captured vendor fixtures.
	for _, kind := range []string{"error", "turn.failed"} {
		for _, message := range []string{"You've hit your usage limit", `HTTP 429 rate_limit_exceeded`} {
			event := map[string]any{"type": kind, "message": message}
			if kind == "turn.failed" {
				event = map[string]any{"type": kind, "error": map[string]string{"message": message}}
			}
			b, _ := json.Marshal(event)
			s := fake(t, string(b), true)
			got, err := Run(context.Background(), s)
			if err != nil || got.Exit != ExitRateLimited {
				t.Fatalf("%s %s: %+v %v", kind, message, got, err)
			}
		}
	}
}
func TestCodexBadModelClassifiesAsError(t *testing.T) {
	got, err := Run(context.Background(), fake(t, fixture(t, "04-bad-model"), true))
	if err != nil || got.Exit != ExitError {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestCodexCommandsAndUsage(t *testing.T) {
	s := fake(t, fixture(t, "05-workspace-write-paths"), false)
	var commands []Command
	s.OnEvent = func(e Event) {
		if e.Item != nil && e.Item.Type == "command_execution" {
			commands = append(commands, *e.Item)
		}
	}
	got, err := Run(context.Background(), s)
	if err != nil || got.Exit != ExitOK || got.Tokens != 33652 || len(commands) != 2 {
		t.Fatalf("%+v %v commands %+v", got, err, commands)
	}
	if commands[0].Status != "in_progress" || commands[0].ExitCode != nil || commands[1].Status != "completed" || commands[1].ExitCode == nil || *commands[1].ExitCode != 0 || !strings.Contains(commands[1].Command, "inside.txt") {
		t.Fatalf("commands %+v", commands)
	}
}
func TestCodexReportFileTakesPrecedence(t *testing.T) {
	s := fake(t, fixture(t, "01-plain-stream-jsonl"), false)
	s.ReportFile = filepath.Join(s.Dir, "report")
	if err := os.WriteFile(s.ReportFile, []byte("file report"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Run(context.Background(), s)
	if err != nil || got.Report != "file report" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestCodexIncompleteOrFailedStreamIsError(t *testing.T) {
	for _, output := range []string{"", "not json", `{"type":"turn.started"}`, `{"type":"turn.failed","error":{"message":"bad"}}`, `{"type":"error","message":"bad"}` + "\n" + `{"type":"turn.completed","usage":{}}`} {
		got, err := Run(context.Background(), fake(t, output, false))
		if err != nil || got.Exit != ExitError {
			t.Fatalf("%q: %+v %v", output, got, err)
		}
	}
}
func TestCodexDesignDocsAllowWriteRoles(t *testing.T) {
	b, err := os.ReadFile("../../../docs/design.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(strings.ToLower(text), "first claude row") || strings.Contains(text, "(currently `claude`)") {
		t.Fatal("design still describes a Claude-only plan")
	}
	if strings.Contains(text, "vendor.codex-allowlist") || !strings.Contains(text, "vendor.codex-write") {
		t.Fatal("design still limits codex to read-only roles")
	}
}

func TestCodexResumeRunsInWorktreeWithConfiguredEnvironment(t *testing.T) {
	s := fake(t, fixture(t, "02-resume-by-id"), false)
	s.Resume = "thread"
	s.Env = append(os.Environ(), "ORK_CODEX_CAPTURE="+filepath.Join(s.Dir, "capture"))
	got, err := Run(context.Background(), s)
	b, readErr := os.ReadFile(filepath.Join(s.Dir, "capture"))
	var capture struct {
		Dir  string
		Args []string
	}
	json.Unmarshal(b, &capture)
	want, _ := s.Args()
	realDir, _ := filepath.EvalSymlinks(s.Dir)
	if err != nil || readErr != nil || capture.Dir != realDir || !reflect.DeepEqual(capture.Args, want) || got.Exit != ExitOK {
		t.Fatalf("%+v %v capture %+v %v", got, err, capture, readErr)
	}
}
func TestCodexCancellationAndMissingBinary(t *testing.T) {
	s := fake(t, "", false)
	t.Setenv("ORK_CODEX_BLOCK", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.OnStart = func(int) { cancel() }
	got, err := Run(ctx, s)
	if !errors.Is(err, context.Canceled) || got.Exit != ExitError || got.ExitCode != -1 {
		t.Fatalf("%+v %v", got, err)
	}
	s.Bin = filepath.Join(s.Dir, "absent")
	got, err = Run(context.Background(), s)
	if err == nil || got.Exit != ExitError {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestCodexRateLimitDoesNotMatchOrdinaryMessages(t *testing.T) {
	for _, output := range []string{
		`{"type":"item.completed","item":{"type":"agent_message","text":"You've hit your usage limit"}}`,
		`{"type":"error","message":"rate_limit_exceeded"}`,
		`{"type":"turn.failed","error":{"message":"HTTP 429 unrelated"}}`,
	} {
		got, err := Run(context.Background(), fake(t, output, true))
		if err != nil || got.Exit != ExitError {
			t.Fatalf("%+v %v", got, err)
		}
	}
}
func TestCodexStreamSplitsLinesAndKeepsCommandOutput(t *testing.T) {
	var events []Event
	r := &stream{onEvent: func(e Event) { events = append(events, e) }}
	text := `{"type":"item.completed","item":{"type":"command_execution","command":"echo hi","aggregated_output":"hi\n","exit_code":3,"status":"failed"}}` + "\n"
	for _, b := range []byte(text) {
		r.Write([]byte{b})
	}
	if len(events) != 1 || events[0].Item.Output != "hi\n" || *events[0].Item.ExitCode != 3 || events[0].Item.Status != "failed" {
		t.Fatalf("%+v", events)
	}
}

func TestCodexFixtureCandidates(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		fail         bool
		want         Exit
		custom       bool
	}{
		{"error", "raw invalid JSON\n", true, ExitError, false},
		{"rate-limited", `{"type":"error","message":"You've hit your usage limit"}` + "\n", true, ExitRateLimited, false},
		{"ok", `{"type":"turn.completed","usage":{}}` + "\n", false, ExitOK, false},
		{"configured-dir", "raw invalid JSON\n", true, ExitError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fake(t, tc.output, tc.fail)
			if tc.custom {
				s.FixtureDir = filepath.Join(s.Dir, "custom-fixtures")
			}
			t.Setenv("ORK_CODEX_STDERR", "raw stderr\n")
			got, err := Run(context.Background(), s)
			if err != nil || got.Exit != tc.want {
				t.Fatalf("exit %s: %v", got.Exit, err)
			}
			dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "ork", "fixtures", "codex")
			if tc.custom {
				dir = s.FixtureDir
			}
			entries, err := os.ReadDir(dir)
			if tc.want == ExitOK {
				if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Fatalf("ok round saved %d candidates", len(entries))
				}
				return
			}
			if err != nil || len(entries) != 1 {
				t.Fatalf("candidates %v: %v", entries, err)
			}
			if got.CandidateErr != nil || got.Candidate != filepath.Join(dir, entries[0].Name()) {
				t.Fatalf("candidate result %q: %v", got.Candidate, got.CandidateErr)
			}
			candidate := filepath.Join(dir, entries[0].Name())
			info, err := os.Stat(candidate)
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatalf("candidate directory: %v %v", info, err)
			}
			args, _ := s.Args()
			for name, want := range map[string]string{"stdout": tc.output, "stderr": "raw stderr\n", "exit": "1\n", "cmd": "codex " + strings.Join(args, " ") + "\n"} {
				path := filepath.Join(candidate, name)
				b, err := os.ReadFile(path)
				if err != nil || string(b) != want {
					t.Fatalf("%s = %q, want %q: %v", name, b, want, err)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("%s mode: %v %v", name, info, err)
				}
			}
		})
	}
}
