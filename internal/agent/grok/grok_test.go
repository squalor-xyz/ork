// SPDX-License-Identifier: GPL-3.0-or-later

package grok

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The fake is the test executable on PATH. It never invokes a vendor CLI.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "grok" {
		if pidFile := os.Getenv("ORK_GROK_SPAWN"); pidFile != "" {
			child := exec.Command("sleep", "300")
			if err := child.Start(); err != nil {
				os.Exit(98)
			}
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
				os.Exit(99)
			}
		}
		if os.Getenv("ORK_GROK_BLOCK") == "1" {
			for {
				time.Sleep(time.Hour)
			}
		}
		if path := os.Getenv("ORK_GROK_CAPTURE"); path != "" {
			dir, _ := os.Getwd()
			b, _ := json.Marshal(struct {
				Dir  string
				Args []string
			}{dir, os.Args[1:]})
			if err := os.WriteFile(path, b, 0o600); err != nil {
				os.Exit(99)
			}
		}
		os.Stdout.WriteString(os.Getenv("ORK_GROK_OUTPUT"))
		os.Stderr.WriteString(os.Getenv("ORK_GROK_STDERR"))
		if os.Getenv("ORK_GROK_FAIL") == "1" {
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
	if err := os.Symlink(exe, filepath.Join(dir, "grok")); err != nil {
		t.Fatal(err)
	}
	// sleep is looked up by the spawn mode only.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin"+string(os.PathListSeparator)+"/usr/bin")
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("ORK_GROK_OUTPUT", output)
	t.Setenv("ORK_GROK_FAIL", "0")
	if fail {
		t.Setenv("ORK_GROK_FAIL", "1")
	}
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return Spec{Dir: dir, Prompt: "prompt", Model: "model", Effort: "high", ReadOnly: true}
}

func fixture(t *testing.T, name, ext string) string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/vendors/grok/" + name + "." + ext)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// out returns a fixture's stdout.
func out(t *testing.T, name string) string { return fixture(t, name, "stdout") }

func TestGrokCreditsStopClassifiesAsOutOfCredits(t *testing.T) {
	// 07-out-of-credits-403 is the real 403 sample (exit 1) from before the
	// credits ran out, kept when the probes were re-run.
	got, err := Run(context.Background(), fake(t, out(t, "07-out-of-credits-403"), true))
	if err != nil || got.Exit != ExitOutOfCredits || got.Exit == Exit("rate_limited") {
		t.Fatalf("%+v %v", got, err)
	}
	if got.Reason == "" || strings.Contains(got.Reason, "REDACTED-TEAM-ID") {
		t.Fatalf("reason %q", got.Reason)
	}
}

func TestGrokBadModelAndAuthStayUnclassified(t *testing.T) {
	for name, output := range map[string]string{
		"bad model (06, real)": out(t, "06-bad-model"),
		// Synthetic: the text "You are not authenticated." was seen from
		// `grok models` only (docs/design.md), not from a -p run (adj 77).
		"unauthenticated": `{"type":"error","message":"You are not authenticated.","cwd":"/work"}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Run(context.Background(), fake(t, output, true))
			if err != nil || got.Exit != ExitUnclassified || got.ExitCode != 1 {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestGrokArgsRefuseFlagLikeInputs(t *testing.T) {
	for _, s := range []Spec{{ReadOnly: true}, {Prompt: "  ", ReadOnly: true}, {Prompt: "-x", ReadOnly: true}, {Prompt: "brief", Resume: "-x", ReadOnly: true}} {
		if _, err := s.Args(); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
}

func TestGrokReadOnlyRoleUsesPlanMode(t *testing.T) {
	// Probe 05: --permission-mode plan left the probe repo clean.
	got, err := (Spec{Dir: "/work", Prompt: "brief", Model: "m", Effort: "high", ReadOnly: true}).Args()
	want := []string{"-p", "brief", "--output-format", "streaming-messages-json", "--include-partial-messages", "-m", "m", "--reasoning-effort", "high", "--permission-mode", "plan", "--cwd", "/work"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("args %q %v, want %q", got, err, want)
	}
}

func TestGrokArgsLatestModelPassesNoModelFlag(t *testing.T) {
	for _, model := range []string{"", "latest", "Latest"} {
		got, err := (Spec{Prompt: "brief", Model: model, ReadOnly: true}).Args()
		if err != nil || len(got) == 0 || strings.Contains(strings.Join(got, " "), "-m ") {
			t.Fatalf("model %q: args %q %v", model, got, err)
		}
	}
}

func TestGrokArgsResume(t *testing.T) {
	got, err := (Spec{Prompt: "brief", Resume: "01a1a178-1dc7-76d3-98bc-4a0415443df2", ReadOnly: true}).Args()
	want := []string{"-p", "brief", "--output-format", "streaming-messages-json", "--include-partial-messages", "--resume", "01a1a178-1dc7-76d3-98bc-4a0415443df2", "--permission-mode", "plan"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("args %q %v, want %q", got, err, want)
	}
}

// Headless grok refuses every write under dontAsk and acceptEdits (probes 10 to
// 12) and bypassPermissions is ruled out, so a write role is refused.
func TestGrokArgsRefuseWriteRoles(t *testing.T) {
	_, err := (Spec{Dir: "/work", Prompt: "brief"}).Args()
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("err %v", err)
	}
	if _, err := Run(context.Background(), Spec{Dir: "/work", Prompt: "brief"}); err == nil {
		t.Fatal("Run accepted a write role")
	}
	for _, flag := range []string{"bypassPermissions", "acceptEdits", "auto"} {
		got, _ := (Spec{Prompt: "brief", ReadOnly: true}).Args()
		if strings.Contains(strings.Join(got, " "), flag) {
			t.Fatalf("args carry %s: %q", flag, got)
		}
	}
}

// The refusal above rests on these probes: the write was refused, nothing was
// written, and the run still exited 0 with stopReason cancelled.
func TestGrokWriteProbesWereAllRefused(t *testing.T) {
	for _, name := range []string{"10-write-dontask", "11-write-acceptedits", "12-write-outside"} {
		if got := fixture(t, name, "stderr"); !strings.Contains(got, "headless run cannot ask for approval") {
			t.Errorf("%s stderr %q", name, got)
		}
		if got := fixture(t, name, "stdout"); !strings.Contains(got, `"stopReason": "cancelled"`) {
			t.Errorf("%s stdout %q", name, got)
		}
		if got := fixture(t, name, "exit"); got != "0\n" {
			t.Errorf("%s exit %q", name, got)
		}
		if got := fixture(t, name, "repo-status"); got != "" {
			t.Errorf("%s changed the repo: %q", name, got)
		}
	}
	if got := fixture(t, "12-write-outside", "outside-status"); !strings.Contains(got, "absent") {
		t.Errorf("outside file: %q", got)
	}
}

func TestGrokParsesAPlainJSONFixtureWithUsage(t *testing.T) {
	got, err := ParsePlain([]byte(out(t, "01-plain-json")))
	if err != nil {
		t.Fatal(err)
	}
	// 18014 input + 1152 cache read + 24 output = total_tokens 19190.
	if got.Session != "01a1a178-1dc7-76d3-98bc-4a0415443df2" || got.Report != "OK" || got.Tokens != 19190 || got.Usage.InputTokens != 18014 || got.Usage.OutputTokens != 24 || got.Usage.CacheReadTokens != 1152 {
		t.Fatalf("%+v", got)
	}
	if _, err := ParsePlain([]byte(out(t, "06-bad-model"))); err == nil {
		t.Fatal("an error line parsed as a result")
	}
}

func TestGrokRunFallsBackToAPlainJSONResult(t *testing.T) {
	got, err := Run(context.Background(), fake(t, out(t, "01-plain-json"), false))
	if err != nil || got.Exit != ExitOK || got.Report != "OK" || got.Session != "01a1a178-1dc7-76d3-98bc-4a0415443df2" || got.Tokens != 19190 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestGrokParsesAStreamedFixture(t *testing.T) {
	s := fake(t, out(t, "02-stream-messages-json"), false)
	starts := 0
	s.OnStart = func(pid int) {
		if pid <= 0 {
			t.Error("bad pid")
		}
		starts++
	}
	got, err := Run(context.Background(), s)
	// 13831 input + 6528 cache read + 48 output.
	if err != nil || got.Exit != ExitOK || got.Session != "01a1a178-2b08-7f41-940f-b00892af7f7d" || got.Report != "1\n2\n3\n4\n5" || got.Tokens != 20407 || starts != 1 {
		t.Fatalf("%+v %v starts %d", got, err, starts)
	}
}

func TestGrokResumesByARealSessionID(t *testing.T) {
	first := fixture(t, "01-plain-json", "stdout")
	resumed := fixture(t, "03-resume-by-id", "stdout")
	a, err := ParsePlain([]byte(first))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParsePlain([]byte(resumed))
	if err != nil {
		t.Fatal(err)
	}
	if a.Session == "" || a.Session != b.Session {
		t.Fatalf("sessions %q and %q", a.Session, b.Session)
	}
	// The .cmd keeps the flag; the id in it is redacted by spike/common.zsh.
	if cmd := fixture(t, "03-resume-by-id", "cmd"); !strings.Contains(cmd, "--resume ") {
		t.Fatalf("cmd %q", cmd)
	}
	s := fake(t, fixture(t, "02-stream-messages-json", "stdout"), false)
	s.Resume = a.Session
	s.Env = append(os.Environ(), "ORK_GROK_CAPTURE="+filepath.Join(s.Dir, "capture"))
	if _, err := Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Dir, "capture"))
	var capture struct {
		Dir  string
		Args []string
	}
	json.Unmarshal(raw, &capture)
	want, _ := s.Args()
	realDir, _ := filepath.EvalSymlinks(s.Dir)
	if err != nil || !reflect.DeepEqual(capture.Args, want) || capture.Dir != realDir || !strings.Contains(strings.Join(capture.Args, " "), "--resume "+a.Session) {
		t.Fatalf("capture %+v %v, want %q in %q", capture, err, a.Session, realDir)
	}
}

func TestGrokCallsOnEventForEachStreamLine(t *testing.T) {
	output := out(t, "02-stream-messages-json")
	s := fake(t, output, false)
	var types []string
	s.OnEvent = func(e Event) { types = append(types, e.Type) }
	if _, err := Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if want := strings.Count(output, "\n"); len(types) != want || types[0] != "system" || types[len(types)-1] != "result" {
		t.Fatalf("%d events, want %d: %v", len(types), want, types)
	}
}

func TestGrokStreamSplitsLinesAndForwardsTextDeltas(t *testing.T) {
	var text []string
	r := &stream{onEvent: func(e Event) {
		if s, ok := e.TextDelta(); ok {
			text = append(text, s)
		}
	}}
	for _, b := range []byte(out(t, "02-stream-messages-json")) {
		r.Write([]byte{b})
	}
	if strings.Join(text, "") != "1\n2\n3\n4\n5" {
		t.Fatalf("text %q", text)
	}
}

func TestGrokReportFileTakesPrecedence(t *testing.T) {
	s := fake(t, out(t, "02-stream-messages-json"), false)
	s.ReportFile = filepath.Join(s.Dir, "report")
	if err := os.WriteFile(s.ReportFile, []byte("file report"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Run(context.Background(), s)
	if err != nil || got.Report != "file report" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestGrokIncompleteOrFailedStreamIsNotOK(t *testing.T) {
	system := `{"type":"system","subtype":"init","session_id":"s"}` + "\n"
	for name, output := range map[string]string{
		"empty":      "",
		"not json":   "not json\n",
		"no result":  system,
		"is_error":   system + `{"type":"result","subtype":"error","is_error":true,"result":"bad","usage":{}}` + "\n",
		"error line": system + `{"type":"error","message":"bad"}` + "\n" + `{"type":"result","subtype":"success","is_error":false,"result":"x","usage":{}}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Run(context.Background(), fake(t, output, false))
			if err != nil || got.Exit != ExitUnclassified || got.Report != "" {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func permissionCancelSpec(t *testing.T, prefix string) Spec {
	t.Helper()
	s := fake(t, prefix+out(t, "08-permission-cancelled"), false)
	t.Setenv("ORK_GROK_STDERR", fixture(t, "08-permission-cancelled", "stderr"))
	return s
}

func assistantText(text string) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}})
	return string(b) + "\n"
}

const cancelReason = "grok: cancelled on a permission prompt: slicer goals --json --lean && echo '---LIST---' && slicer li"

func TestGrokPermissionCancelIsAnErrorExit(t *testing.T) {
	got, err := Run(context.Background(), permissionCancelSpec(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got.Exit != ExitError {
		t.Errorf("exit = %s, want error", got.Exit)
	}
	if got.Reason != cancelReason {
		t.Errorf("reason = %q, want %q", got.Reason, cancelReason)
	}
	if got.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", got.ExitCode)
	}
}

func TestGrokPermissionCancelKeepsAFinishedReport(t *testing.T) {
	report := "Full review.\n## Owner needed\nnone\n"
	// Earlier messages and partial deltas must not replace the last complete text.
	prefix := assistantText("Earlier\n## Owner needed\nnone\n") + assistantText(report) + `{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"partial"}}}` + "\n"
	got, err := Run(context.Background(), permissionCancelSpec(t, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if got.Exit != ExitOK {
		t.Errorf("exit = %s, want ok", got.Exit)
	}
	if got.Report != report {
		t.Errorf("report = %q, want %q", got.Report, report)
	}
	if got.Reason != cancelReason {
		t.Errorf("reason = %q, want %q", got.Reason, cancelReason)
	}
}

func TestGrokPermissionCancelKeepsAReportFileToo(t *testing.T) {
	for name, prefix := range map[string]string{"message and file": assistantText("Message\n## Owner needed\nnone\n"), "file only": ""} {
		t.Run(name, func(t *testing.T) {
			s := permissionCancelSpec(t, prefix)
			s.ReportFile = filepath.Join(s.Dir, "report")
			if err := os.WriteFile(s.ReportFile, []byte("file report"), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Run(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			if got.Exit != ExitOK {
				t.Errorf("exit = %s, want ok", got.Exit)
			}
			if got.Report != "file report" {
				t.Errorf("report = %q, want file report", got.Report)
			}
		})
	}
}

func TestGrokPermissionCancelWithoutAFinishedReportIsAnErrorExit(t *testing.T) {
	for name, prefix := range map[string]string{
		"commentary":       assistantText("I will check the diff"),
		"tool only":        "",
		"inline heading":   assistantText("Text mentions ## Owner needed inline"),
		"partial only":     `{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"## Owner needed\nnone"}}}` + "\n",
		"later commentary": assistantText("Review\n## Owner needed\nnone") + assistantText("I will check more"),
		"later empty text": assistantText("Review\n## Owner needed\nnone") + assistantText(""),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Run(context.Background(), permissionCancelSpec(t, prefix))
			if err != nil {
				t.Fatal(err)
			}
			if got.Exit != ExitError {
				t.Errorf("exit = %s, want error", got.Exit)
			}
			if got.Report != "" {
				t.Errorf("unfinished report = %q", got.Report)
			}
		})
	}
}

func TestGrokOtherFailuresStayUnclassified(t *testing.T) {
	for name, output := range map[string]string{
		"bad model":            out(t, "06-bad-model"),
		"no cancellation":      `{"type":"result","is_error":true,"result":"bad"}` + "\n",
		"no permission prompt": out(t, "08-permission-cancelled"),
	} {
		t.Run(name, func(t *testing.T) {
			s := fake(t, output, false)
			if name == "no cancellation" {
				t.Setenv("ORK_GROK_STDERR", fixture(t, "08-permission-cancelled", "stderr"))
			}
			got, err := Run(context.Background(), s)
			if err != nil || got.Exit != ExitUnclassified || got.Report != "" {
				t.Fatalf("exit=%s report=%q err=%v", got.Exit, got.Report, err)
			}
		})
	}
	TestGrokIncompleteOrFailedStreamIsNotOK(t)
}

func TestGrokCreditsTextOnlyCountsInAnErrorEvent(t *testing.T) {
	text := "Your team x has either used all available credits or reached its monthly spending limit."
	system := `{"type":"system","subtype":"init","session_id":"s"}` + "\n"
	assistant, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}})
	result, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": text, "usage": map[string]int{}})
	got, err := Run(context.Background(), fake(t, system+string(assistant)+"\n"+string(result)+"\n", false))
	if err != nil || got.Exit != ExitOK || got.Report != text {
		t.Fatalf("%+v %v", got, err)
	}
	failed, _ := json.Marshal(map[string]any{"type": "result", "subtype": "error", "is_error": true, "result": text, "usage": map[string]int{}})
	got, err = Run(context.Background(), fake(t, system+string(failed)+"\n", true))
	if err != nil || got.Exit != ExitOutOfCredits {
		t.Fatalf("%+v %v", got, err)
	}
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func TestGrokReportsThePGIDThroughOnStart(t *testing.T) {
	s := fake(t, out(t, "02-stream-messages-json"), false)
	var pid, pgid int
	s.OnStart = func(p int) {
		pid = p
		pgid, _ = syscall.Getpgid(p)
	}
	if _, err := Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if pid <= 0 {
		t.Fatal("OnStart not called")
	}
	// The child leads its own group, so the pid is the pgid and not ork's.
	if pgid != pid || pgid == syscall.Getpgrp() {
		t.Fatalf("pid %d pgid %d, ork pgid %d", pid, pgid, syscall.Getpgrp())
	}
}

func TestGrokRunUsesItsOwnProcessGroupAndKillsItOnCancel(t *testing.T) {
	s := fake(t, "", false)
	pidFile := filepath.Join(s.Dir, "child.pid")
	t.Setenv("ORK_GROK_SPAWN", pidFile)
	t.Setenv("ORK_GROK_BLOCK", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	s.OnStart = func(int) { close(started) }
	type result struct {
		round Round
		err   error
	}
	done := make(chan result, 1)
	go func() {
		r, err := Run(ctx, s)
		done <- result{r, err}
	}()
	<-started
	var pid int
	for i := 0; i < 200; i++ {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err = strconv.Atoi(string(b)); err == nil {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid <= 0 || !processAlive(pid) {
		t.Fatalf("grandchild pid %d", pid)
	}
	cancel()
	select {
	case r := <-done:
		if !errors.Is(r.err, context.Canceled) {
			t.Fatalf("err %v", r.err)
		}
	case <-time.After(10 * time.Second):
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("Run did not return after cancel")
	}
	for i := 0; i < 100 && processAlive(pid); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("grandchild %d still running after cancel", pid)
	}
}

func TestGrokCancellationAndMissingBinary(t *testing.T) {
	s := fake(t, "", false)
	t.Setenv("ORK_GROK_BLOCK", "1")
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

func TestGrokFixtureCandidates(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		fail         bool
		want         Exit
		custom       bool
	}{
		{"unclassified", "raw invalid JSON\n", true, ExitUnclassified, false},
		{"out-of-credits", out(t, "07-out-of-credits-403"), true, ExitOutOfCredits, false},
		{"ok", out(t, "02-stream-messages-json"), false, ExitOK, false},
		{"configured-dir", "raw invalid JSON\n", true, ExitUnclassified, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fake(t, tc.output, tc.fail)
			if tc.custom {
				s.FixtureDir = filepath.Join(s.Dir, "custom-fixtures")
			}
			t.Setenv("ORK_GROK_STDERR", "raw stderr\n")
			got, err := Run(context.Background(), s)
			if err != nil || got.Exit != tc.want {
				t.Fatalf("exit %s: %v", got.Exit, err)
			}
			dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "ork", "fixtures", "grok")
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
			candidate := filepath.Join(dir, entries[0].Name())
			if got.CandidateErr != nil || got.Candidate != candidate {
				t.Fatalf("candidate result %q: %v", got.Candidate, got.CandidateErr)
			}
			if info, err := os.Stat(candidate); err != nil || info.Mode().Perm() != 0o700 {
				t.Fatalf("candidate directory: %v %v", info, err)
			}
			args, _ := s.Args()
			for name, want := range map[string]string{"stdout": tc.output, "stderr": "raw stderr\n", "exit": "1\n", "cmd": "grok " + strings.Join(args, " ") + "\n"} {
				path := filepath.Join(candidate, name)
				b, err := os.ReadFile(path)
				if err != nil || string(b) != want {
					t.Fatalf("%s = %q, want %q: %v", name, b, want, err)
				}
				if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("%s mode: %v %v", name, info, err)
				}
			}
		})
	}
}
