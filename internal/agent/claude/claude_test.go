// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

const fixtures = "../../../testdata/vendors/claude"

func fixture(name, ext string) string { return filepath.Join(fixtures, name+"."+ext) }

func readFixture(t *testing.T, name, ext string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixture(name, ext))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureExit(t *testing.T, name string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimSpace(string(readFixture(t, name, "exit"))))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func replayPlan(name string) plan {
	return plan{Mode: "replay", Stdout: fixture(name, "stdout"), Stderr: fixture(name, "stderr")}
}

func replayPlanExit(t *testing.T, name string) plan {
	p := replayPlan(name)
	p.Exit = fixtureExit(t, name)
	return p
}

// ---- command

func TestArgsWriteRole(t *testing.T) {
	s := Spec{
		Prompt: "do the slice", Model: "sonnet", Effort: "high",
		Allowlist: []string{"Read", "Edit", "Bash(slicer *)"},
	}
	got, err := s.Args()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--model", "sonnet", "--effort", "high",
		"--allowedTools", "Read,Edit,Bash(slicer *)", "--permission-prompts", "none",
		"do the slice",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args\n got %q\nwant %q", got, want)
	}
}

func TestArgsReadOnlyRole(t *testing.T) {
	s := Spec{Prompt: "review", Model: "opus", Effort: "medium", ReadOnly: true, Allowlist: []string{"Read"}}
	got, err := s.Args()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--model", "opus", "--effort", "medium",
		"--permission-mode", "plan",
		"review",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args\n got %q\nwant %q", got, want)
	}
}

func TestArgsResume(t *testing.T) {
	s := Spec{Prompt: "fix it", Model: "haiku", Allowlist: []string{"Read"}, Resume: "b17a3484-60da"}
	got, err := s.Args()
	if err != nil {
		t.Fatal(err)
	}
	i := indexOf(got, "--resume")
	if i < 0 || i+1 >= len(got) || got[i+1] != "b17a3484-60da" {
		t.Fatalf("want --resume b17a3484-60da in %q", got)
	}
	if got[0] != "-p" || got[len(got)-1] != "fix it" {
		t.Errorf("-p must come first and the prompt last: %q", got)
	}
	s.Resume = ""
	got, _ = s.Args()
	if indexOf(got, "--resume") >= 0 {
		t.Errorf("no --resume without a session: %q", got)
	}
}

func TestArgsOmitsEmptyModelAndEffort(t *testing.T) {
	got, err := Spec{Prompt: "x", ReadOnly: true}.Args()
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(got, "--model") >= 0 || indexOf(got, "--effort") >= 0 {
		t.Errorf("empty model and effort must be left out: %q", got)
	}
}

func TestArgsRefuses(t *testing.T) {
	cases := map[string]Spec{
		"empty prompt":              {ReadOnly: true},
		"prompt looks like a flag":  {Prompt: "--help", ReadOnly: true},
		"write role, no allowlist":  {Prompt: "x"},
		"allowlist entry is empty":  {Prompt: "x", Allowlist: []string{"Read", " "}},
		"allowlist entry has comma": {Prompt: "x", Allowlist: []string{"Bash(a,b)"}},
	}
	for name, s := range cases {
		if _, err := s.Args(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// ---- stream and result

func parseAll(t *testing.T, data []byte) []Event {
	t.Helper()
	var evs []Event
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		ev, err := ParseLine(line)
		if err != nil {
			t.Fatalf("ParseLine: %v\n%.200s", err, line)
		}
		evs = append(evs, ev)
	}
	return evs
}

func TestParseStreamFixtureGivesTextDeltas(t *testing.T) {
	evs := parseAll(t, readFixture(t, "02-stream-json", "stdout"))
	var deltas []string
	kinds := map[EventKind]int{}
	for _, ev := range evs {
		kinds[ev.Kind]++
		if ev.Kind == EventTextDelta {
			deltas = append(deltas, ev.Text)
		}
	}
	if want := []string{"1\n2\n3\n4", "\n5"}; !reflect.DeepEqual(deltas, want) {
		t.Errorf("deltas %q, want %q", deltas, want)
	}
	if kinds[EventInit] != 1 || kinds[EventRateLimit] != 1 || kinds[EventResult] != 1 {
		t.Errorf("kinds %v, want one init, one rate-limit, one result", kinds)
	}
	if evs[0].Kind != EventInit || evs[0].SessionID != "9833ecf5-8ffa-4737-8bcd-58479d5fb1a2" {
		t.Errorf("first event %+v, want the init event with the session id", evs[0])
	}
}

func TestParseResultFixtures(t *testing.T) {
	cases := []struct {
		name, session, text string
		in, out, cc, cr     int64
		cost                float64
		errStatus           int
		isError             bool
		denials             int
	}{
		{"01-plain-json", "b17a3484-60da-4931-b01f-f685541bdf82", "OK", 2, 39, 14888, 8858, 0.0030858800000000005, 0, false, 0},
		{"03-resume", "b17a3484-60da-4931-b01f-f685541bdf82", "OK", 2, 4, 89, 23746, 0.0033433400000000006, 0, false, 0},
		{"04-allowlist-denies-write", "da4b94a3-71db-416c-802f-f6ea4d5810b1", "", 4, 505, 12719, 35249, 0.00314919, 0, false, 1},
		{"06-bad-model", "d40a2b6f-8002-4358-b8b0-ea4ca4a6c598", "", 0, 0, 0, 0, 0, 404, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			evs := parseAll(t, readFixture(t, c.name, "stdout"))
			if len(evs) != 1 || evs[0].Kind != EventResult || evs[0].Result == nil {
				t.Fatalf("want one result event, got %+v", evs)
			}
			r := evs[0].Result
			if r.SessionID != c.session {
				t.Errorf("session %q, want %q", r.SessionID, c.session)
			}
			if c.text != "" && r.Text != c.text {
				t.Errorf("text %q, want %q", r.Text, c.text)
			}
			if c.text == "" && r.Text == "" {
				t.Error("text is empty")
			}
			want := Usage{c.in, c.out, c.cc, c.cr, c.cost}
			if r.Usage != want {
				t.Errorf("usage %+v, want %+v", r.Usage, want)
			}
			if r.IsError != c.isError || r.Subtype != "success" {
				t.Errorf("is_error %v subtype %q, want %v success", r.IsError, r.Subtype, c.isError)
			}
			if c.errStatus != 0 && (r.APIErrorStatus == nil || *r.APIErrorStatus != c.errStatus) {
				t.Errorf("api_error_status %v, want %d", r.APIErrorStatus, c.errStatus)
			}
			if c.errStatus == 0 && r.APIErrorStatus != nil {
				t.Errorf("api_error_status %d, want none", *r.APIErrorStatus)
			}
			if len(r.Denials) != c.denials {
				t.Errorf("%d denials, want %d", len(r.Denials), c.denials)
			}
			if len(r.Raw) == 0 {
				t.Error("Raw is empty")
			}
		})
	}
}

func TestParseStreamResultOfFixture02(t *testing.T) {
	evs := parseAll(t, readFixture(t, "02-stream-json", "stdout"))
	r := evs[len(evs)-1].Result
	if r == nil {
		t.Fatal("last event has no result")
	}
	if r.Text != "1\n2\n3\n4\n5" || r.SessionID != "9833ecf5-8ffa-4737-8bcd-58479d5fb1a2" {
		t.Errorf("result %q session %q", r.Text, r.SessionID)
	}
	want := Usage{2, 11, 12259, 11491, 0.0025724100000000007}
	if r.Usage != want {
		t.Errorf("usage %+v, want %+v", r.Usage, want)
	}
}

func TestParseLineDenialDetail(t *testing.T) {
	ev, err := ParseLine(bytes.TrimSpace(readFixture(t, "04-allowlist-denies-write", "stdout")))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Result == nil || len(ev.Result.Denials) != 1 {
		t.Fatalf("want one denial in %+v", ev)
	}
	d := ev.Result.Denials[0]
	if d.ToolName != "Write" || d.ToolUseID != "toolu_01SoBAKVJxM26E4RTox9r9U2" || !strings.Contains(string(d.ToolInput), "made-by-claude.txt") {
		t.Errorf("denial %+v", d)
	}
}

// denialWithInput returns a denial whose tool_input is the JSON of input.
func denialWithInput(t *testing.T, tool string, input map[string]any) Denial {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return Denial{ToolName: tool, ToolInput: raw}
}

func TestDenialDetailOfABashCallIsItsCommand(t *testing.T) {
	d := denialWithInput(t, "Bash", map[string]any{"command": "git push origin main", "description": "push"})
	if got := d.Detail(); got != "git push origin main" {
		t.Errorf("detail %q, want the command", got)
	}
}

func TestDenialDetailOfAWriteIsItsPathNeverItsContent(t *testing.T) {
	d := denialWithInput(t, "Write", map[string]any{"file_path": "/work/notes.txt", "content": "TOKEN=secret\n"})
	if got := d.Detail(); got != "/work/notes.txt" {
		t.Errorf("detail %q, want the file path only", got)
	}
}

func TestDenialDetailOfTheFixtureWriteIsItsPath(t *testing.T) {
	ev, err := ParseLine(bytes.TrimSpace(readFixture(t, "04-allowlist-denies-write", "stdout")))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Result == nil || len(ev.Result.Denials) != 1 {
		t.Fatalf("want one denial in %+v", ev)
	}
	if got := ev.Result.Denials[0].Detail(); got != "<probe-repo>/made-by-claude.txt" {
		t.Errorf("detail %q, want the fixture's file_path", got)
	}
}

func TestDenialDetailPrefersCommandThenFilePathAndIsEmptyForNeither(t *testing.T) {
	for name, tc := range map[string]struct {
		tool  string
		input map[string]any
		want  string
	}{
		"edit keeps the path, not the new text": {"Edit", map[string]any{"file_path": "/w/a.go", "old_string": "x", "new_string": "SECRET"}, "/w/a.go"},
		"empty command falls back to the path":  {"Bash", map[string]any{"command": "", "file_path": "/w/b"}, "/w/b"},
		"a tool with no command or path":        {"Glob", map[string]any{"pattern": "*.go"}, ""},
		"no tool input at all":                  {"Monitor", nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			d := Denial{ToolName: tc.tool}
			if tc.input != nil {
				d = denialWithInput(t, tc.tool, tc.input)
			}
			if got := d.Detail(); got != tc.want {
				t.Errorf("detail %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDenialDetailCapsAt200CharactersIncludingTheEllipsis(t *testing.T) {
	for name, tc := range map[string]struct {
		command string
		want    string
	}{
		"200 characters are kept whole": {strings.Repeat("a", 200), strings.Repeat("a", 200)},
		"201 characters are cut":        {strings.Repeat("b", 201), strings.Repeat("b", 197) + "..."},
		"characters, not bytes":         {strings.Repeat("é", 300), strings.Repeat("é", 197) + "..."},
	} {
		t.Run(name, func(t *testing.T) {
			got := denialWithInput(t, "Bash", map[string]any{"command": tc.command}).Detail()
			if got != tc.want {
				t.Errorf("detail is %d characters, want %d: %q", utf8.RuneCountInString(got), utf8.RuneCountInString(tc.want), got)
			}
			if n := utf8.RuneCountInString(got); n > 200 {
				t.Errorf("detail is %d characters, over the 200 cap", n)
			}
		})
	}
}

func TestDenialDetailTurnsCRAndLFIntoSpacesBeforeTheCap(t *testing.T) {
	d := denialWithInput(t, "Bash", map[string]any{"command": "git commit -m 'one\r\ntwo\nthree'"})
	if got := d.Detail(); got != "git commit -m 'one  two three'" {
		t.Errorf("detail %q, want line breaks as spaces", got)
	}
}

func TestOnlyTextDeltasFeedTheTail(t *testing.T) {
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}},"session_id":"s"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"a\""}},"session_id":"s"}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"no"}},"session_id":"s"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"yes"}},"session_id":"s"}`,
	}
	var sr streamReader
	sr.tailBytes = 100
	for _, l := range lines {
		ev, err := ParseLine([]byte(l))
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == EventTextDelta && ev.Text != "yes" {
			t.Errorf("non-text delta read as text: %+v", ev)
		}
		sr.Write([]byte(l + "\n"))
	}
	if got := sr.Tail(); got != "yes" {
		t.Errorf("tail %q, want yes", got)
	}
}

func TestTailCutsAtRuneStart(t *testing.T) {
	sr := streamReader{tailBytes: 3}
	sr.addTail("a\u00e9\u00e9") // 1 + 2 + 2 bytes; the last 3 bytes start inside a rune
	if got := sr.Tail(); got != "\u00e9" {
		t.Errorf("tail %q, want one whole rune", got)
	}
}

func TestParseLineRejectsBadJSON(t *testing.T) {
	if _, err := ParseLine([]byte("not json")); err == nil {
		t.Error("want an error for a line that is not JSON")
	}
}

// ---- exit class

func resultOf(t *testing.T, name string) *Result {
	t.Helper()
	evs := parseAll(t, readFixture(t, name, "stdout"))
	r := evs[len(evs)-1].Result
	if r == nil {
		t.Fatalf("%s: last event is not a result: %+v", name, evs[len(evs)-1])
	}
	return r
}

func TestClassifyFixtures(t *testing.T) {
	cases := []struct {
		name string
		want Class
	}{
		{"01-plain-json", ClassOK},
		{"02-stream-json", ClassOK},
		{"03-resume", ClassOK},
		{"04-allowlist-denies-write", ClassOK},
		{"05-plan-mode-edit", ClassOK},
		{"06-bad-model", ClassUnclassified},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := Classify(fixtureExit(t, c.name), resultOf(t, c.name))
			if got != c.want {
				t.Errorf("class %q (%s), want %q", got, reason, c.want)
			}
			if reason == "" {
				t.Error("reason is empty")
			}
		})
	}
}

func TestClassifySuccessfulDenialIsOK(t *testing.T) {
	if got, _ := Classify(0, resultOf(t, "04-allowlist-denies-write")); got != ClassOK {
		t.Fatalf("class %q, want ok", got)
	}
}

func TestClassifyUnsuccessfulDenialIsDenied(t *testing.T) {
	for _, change := range []string{"exit", "subtype", "is_error"} {
		t.Run(change, func(t *testing.T) {
			r := resultOf(t, "04-allowlist-denies-write")
			exit := 0
			switch change {
			case "exit":
				exit = 1
			case "subtype":
				r.Subtype = "error"
			case "is_error":
				r.IsError = true
			}
			if got, _ := Classify(exit, r); got != ClassDenied {
				t.Fatalf("class %q, want denied", got)
			}
		})
	}
}

func TestClassifyUnsampledStatusesAreUnclassified(t *testing.T) {
	// vendor.unsampled-classes: no auth failure, bad-model class or rate limit
	// has a real sample, so none of these statuses is a class of its own.
	for _, status := range []int{401, 403, 404, 429, 503, 529} {
		r := resultOf(t, "06-bad-model")
		s := status
		r.APIErrorStatus = &s
		if got, _ := Classify(1, r); got != ClassUnclassified {
			t.Errorf("status %d: class %q, want unclassified", status, got)
		}
	}
}

func TestClassifyNeverGuessesRateLimit(t *testing.T) {
	for _, status := range []int{429, 529, 503} {
		r := resultOf(t, "06-bad-model")
		s := status
		r.APIErrorStatus = &s
		r.Text = "You've hit your usage limit"
		if got, _ := Classify(1, r); got == ClassRateLimited {
			t.Errorf("status %d: rate-limited from an undocumented signal", status)
		}
	}
	// A rejected rate_limit_event is unverified and is not a signal either.
	stream := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour"},"session_id":"s"}` + "\n"
	ev, err := ParseLine([]byte(stream))
	if err != nil || ev.Kind != EventRateLimit {
		t.Fatalf("ev %+v err %v", ev, err)
	}
	if got, _ := Classify(1, nil); got == ClassRateLimited {
		t.Error("exit 1 without a result must not be rate-limited")
	}
}

func TestClassifyOtherFailuresAreUnclassified(t *testing.T) {
	ok := resultOf(t, "01-plain-json")
	cases := map[string]struct {
		exit int
		r    func() *Result
	}{
		"exit 1, no result":         {1, func() *Result { return nil }},
		"exit 0, no result":         {0, func() *Result { return nil }},
		"killed by a signal":        {-1, func() *Result { return nil }},
		"exit 1 with ok-looking":    {1, func() *Result { return ok }},
		"exit 0, is_error":          {0, func() *Result { c := *ok; c.IsError = true; return &c }},
		"exit 0, subtype not ok":    {0, func() *Result { c := *ok; c.Subtype = "error_max_turns"; return &c }},
		"exit 0, error_during_exec": {0, func() *Result { c := *ok; c.Subtype = "error_during_execution"; return &c }},
	}
	for name, c := range cases {
		if got, reason := Classify(c.exit, c.r()); got != ClassUnclassified || reason == "" {
			t.Errorf("%s: class %q reason %q, want unclassified", name, got, reason)
		}
	}
}

// ---- report

func TestReportFileThenResultText(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "report.md")
	res := &Result{Text: "result text"}

	if got, src := ReadReport(file, res); got != "result text" || src != ReportFromResult {
		t.Errorf("missing file: %q %q, want the result text", got, src)
	}
	os.WriteFile(file, []byte("  \n"), 0o644)
	if got, src := ReadReport(file, res); got != "result text" || src != ReportFromResult {
		t.Errorf("blank file: %q %q, want the result text", got, src)
	}
	os.WriteFile(file, []byte("# Report\nfrom file\n"), 0o644)
	if got, src := ReadReport(file, res); got != "# Report\nfrom file\n" || src != ReportFromFile {
		t.Errorf("file: %q %q, want the file", got, src)
	}
	if got, src := ReadReport("", res); got != "result text" || src != ReportFromResult {
		t.Errorf("no path: %q %q, want the result text", got, src)
	}
}

func TestReportIgnoresErrorResultText(t *testing.T) {
	res := resultOf(t, "06-bad-model")
	if got, src := ReadReport("", res); got != "" || src != "" {
		t.Errorf("error text became a report: %q %q", got, src)
	}
	if got, src := ReadReport("", nil); got != "" || src != "" {
		t.Errorf("nil result: %q %q", got, src)
	}
}

// ---- fixture directory

func TestDefaultFixtureDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	got, err := DefaultFixtureDir()
	if err != nil || got != "/home/u/.local/state/ork/fixtures/claude" {
		t.Errorf("got %q, %v", got, err)
	}
	t.Setenv("XDG_STATE_HOME", "/x/state")
	got, err = DefaultFixtureDir()
	if err != nil || got != "/x/state/ork/fixtures/claude" {
		t.Errorf("got %q, %v", got, err)
	}
}

// ---- Run against the fake claude

func TestRunReplaysFixtures(t *testing.T) {
	cases := []struct {
		name    string
		class   Class
		session string
		text    string
		tokens  int64 // output tokens
		cost    float64
	}{
		{"01-plain-json", ClassOK, "b17a3484-60da-4931-b01f-f685541bdf82", "OK", 39, 0.0030858800000000005},
		{"02-stream-json", ClassOK, "9833ecf5-8ffa-4737-8bcd-58479d5fb1a2", "1\n2\n3\n4\n5", 11, 0.0025724100000000007},
		{"03-resume", ClassOK, "b17a3484-60da-4931-b01f-f685541bdf82", "OK", 4, 0.0033433400000000006},
		{"05-plan-mode-edit", ClassOK, "59c83c18-a718-40ca-90ac-504f0a8175df", "", 957, 0.0818584},
		{"04-allowlist-denies-write", ClassOK, "da4b94a3-71db-416c-802f-f6ea4d5810b1", "", 505, 0.00314919},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := fakeSpec(t, replayPlanExit(t, c.name))
			s.ReadOnly = true
			round, err := Run(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			if round.Class != c.class {
				t.Errorf("class %q (%s), want %q", round.Class, round.Reason, c.class)
			}
			if round.ExitCode != 0 {
				t.Errorf("exit %d", round.ExitCode)
			}
			if round.SessionID != c.session {
				t.Errorf("session %q, want %q", round.SessionID, c.session)
			}
			if c.text != "" && round.Text != c.text {
				t.Errorf("text %q, want %q", round.Text, c.text)
			}
			if round.Usage.OutputTokens != c.tokens || round.Usage.CostUSD != c.cost {
				t.Errorf("usage %+v, want %d output tokens and cost %v", round.Usage, c.tokens, c.cost)
			}
			entries, _ := os.ReadDir(s.FixtureDir)
			if c.class == ClassOK && len(round.Denials) == 0 && (round.Candidate != "" || len(entries) != 0) {
				t.Errorf("candidate saved for an ok round: %q %v", round.Candidate, entries)
			}
			if (c.class != ClassOK || len(round.Denials) > 0) && (round.Candidate == "" || len(entries) != 1) {
				t.Errorf("class %q: no candidate saved (%q, %v)", c.class, round.Candidate, entries)
			}
			if !bytes.Equal(round.Stdout, readFixture(t, c.name, "stdout")) {
				t.Error("Stdout is not the raw stdout")
			}
		})
	}
}

func TestRunPassesCommandToBinary(t *testing.T) {
	p := replayPlanExit(t, "03-resume")
	p.ArgsLog = filepath.Join(t.TempDir(), "args.json")
	s := fakeSpec(t, p)
	s.Resume = "b17a3484-60da-4931-b01f-f685541bdf82"
	s.Allowlist = []string{"Read"}
	round, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	data, err := os.ReadFile(p.ArgsLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want, _ := s.Args()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("binary got %q, want %q", got, want)
	}
	if !reflect.DeepEqual(round.Command, append([]string{s.Bin}, want...)) {
		t.Errorf("Round.Command %q", round.Command)
	}
	if indexOf(got, "--resume") < 0 {
		t.Errorf("resume lost: %q", got)
	}
}

func TestRunBadSpecStartsNothing(t *testing.T) {
	p := replayPlan("01-plain-json")
	p.ArgsLog = filepath.Join(t.TempDir(), "args.json")
	s := fakeSpec(t, p)
	s.Prompt = ""
	if _, err := Run(context.Background(), s); err == nil {
		t.Error("want an error")
	}
	if _, err := os.Stat(p.ArgsLog); err == nil {
		t.Error("the binary was started")
	}
}

func TestRunMissingBinary(t *testing.T) {
	s := fakeSpec(t, plan{Mode: "replay"})
	s.Bin = filepath.Join(t.TempDir(), "no-such-claude")
	if _, err := Run(context.Background(), s); err == nil {
		t.Error("want an error")
	}
}

func TestRunUnclassifiedSavesCandidate(t *testing.T) {
	s := fakeSpec(t, replayPlanExit(t, "06-bad-model"))
	s.ReadOnly = true
	round, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if round.Class != ClassUnclassified || round.ExitCode != 1 {
		t.Fatalf("class %q exit %d", round.Class, round.ExitCode)
	}
	if round.Candidate == "" || filepath.Dir(round.Candidate) != s.FixtureDir {
		t.Fatalf("candidate %q, want a directory under %s", round.Candidate, s.FixtureDir)
	}
	for ext, want := range map[string][]byte{
		"stdout": readFixture(t, "06-bad-model", "stdout"),
		"stderr": readFixture(t, "06-bad-model", "stderr"),
	} {
		got, err := os.ReadFile(filepath.Join(round.Candidate, ext))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: %q (%v), want the raw bytes", ext, got, err)
		}
	}
	exit, _ := os.ReadFile(filepath.Join(round.Candidate, "exit"))
	if strings.TrimSpace(string(exit)) != "1" {
		t.Errorf("exit file %q", exit)
	}
	cmd, _ := os.ReadFile(filepath.Join(round.Candidate, "cmd"))
	if !strings.Contains(string(cmd), "--output-format stream-json") {
		t.Errorf("cmd file %q", cmd)
	}
	js, err := os.ReadFile(filepath.Join(round.Candidate, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(js, &obj); err != nil || obj["api_error_status"] != float64(404) {
		t.Errorf("result.json %.120s (%v)", js, err)
	}
}

func TestCandidateIsPrivate(t *testing.T) {
	s := fakeSpec(t, replayPlanExit(t, "06-bad-model"))
	s.ReadOnly = true
	s.FixtureDir = filepath.Join(t.TempDir(), "fixtures", "claude") // created by Run
	round, err := Run(context.Background(), s)
	if err != nil || round.CandidateErr != nil {
		t.Fatalf("%v %v", err, round.CandidateErr)
	}
	for _, p := range []string{s.FixtureDir, round.Candidate, filepath.Join(round.Candidate, "stdout")} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s mode %v is open to others", p, info.Mode().Perm())
		}
	}
}

func TestRunDeniedRoundSavesRawCandidate(t *testing.T) {
	// A successful denial still saves its raw output
	// (vendor.fixture-candidates).
	s := fakeSpec(t, replayPlanExit(t, "04-allowlist-denies-write"))
	s.ReadOnly = true
	round, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if round.Class != ClassOK || round.ExitCode != 0 || round.Candidate == "" {
		t.Fatalf("class %q exit %d candidate %q", round.Class, round.ExitCode, round.Candidate)
	}
	got, err := os.ReadFile(filepath.Join(round.Candidate, "stdout"))
	if err != nil || !bytes.Equal(got, readFixture(t, "04-allowlist-denies-write", "stdout")) {
		t.Errorf("stdout is not the raw bytes (%v)", err)
	}
	info, err := os.Stat(filepath.Join(round.Candidate, "stdout"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("stdout mode %v (%v), want 0600", info.Mode().Perm(), err)
	}
}

func TestRunUnclassifiedWithoutResultSavesRawOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	os.WriteFile(out, []byte("garbage, not json\n"), 0o644)
	s := fakeSpec(t, plan{Mode: "replay", Stdout: out, Exit: 2})
	s.ReadOnly = true
	round, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if round.Class != ClassUnclassified || round.Result != nil || round.Candidate == "" {
		t.Fatalf("class %q result %v candidate %q", round.Class, round.Result, round.Candidate)
	}
	got, _ := os.ReadFile(filepath.Join(round.Candidate, "stdout"))
	if string(got) != "garbage, not json\n" {
		t.Errorf("stdout %q", got)
	}
	if _, err := os.Stat(filepath.Join(round.Candidate, "result.json")); err == nil {
		t.Error("result.json written without a result")
	}
}

func TestRunReadsReportFileThenResultText(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.md")
	os.WriteFile(file, []byte("the report"), 0o644)
	s := fakeSpec(t, replayPlanExit(t, "01-plain-json"))
	s.ReadOnly = true
	s.ReportFile = file
	round, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if round.Report != "the report" || round.ReportSource != ReportFromFile {
		t.Errorf("report %q from %q", round.Report, round.ReportSource)
	}
	s.ReportFile = filepath.Join(t.TempDir(), "missing.md")
	round, err = Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if round.Report != "OK" || round.ReportSource != ReportFromResult {
		t.Errorf("report %q from %q, want the result text", round.Report, round.ReportSource)
	}
}

func TestRunStreamsEventsWhileProcessRuns(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "gate")
	p := replayPlanExit(t, "02-stream-json")
	p.Mode = "gated"
	p.Gate = gate
	s := fakeSpec(t, p)
	s.ReadOnly = true
	// The fake prints the init line, then waits for the gate. A run that
	// only parses after exit never delivers the init event, so never opens
	// the gate.
	s.OnEvent = func(ev Event) {
		if ev.Kind == EventInit {
			os.WriteFile(gate, nil, 0o644)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	round, err := Run(ctx, s)
	if err != nil {
		t.Fatalf("Run: %v (events were not delivered while the process ran)", err)
	}
	if round.Class != ClassOK {
		t.Errorf("class %q", round.Class)
	}
}

func TestRunDeliversEventsInOrderAndBuildsTail(t *testing.T) {
	s := fakeSpec(t, replayPlanExit(t, "02-stream-json"))
	s.ReadOnly = true
	var kinds []EventKind
	var deltas []string
	s.OnEvent = func(ev Event) {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == EventTextDelta {
			deltas = append(deltas, ev.Text)
		}
	}
	round, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if kinds[0] != EventInit || kinds[len(kinds)-1] != EventResult {
		t.Errorf("kinds %v", kinds)
	}
	if !reflect.DeepEqual(deltas, []string{"1\n2\n3\n4", "\n5"}) {
		t.Errorf("deltas %q", deltas)
	}
	if round.Tail != "1\n2\n3\n4\n5" {
		t.Errorf("tail %q", round.Tail)
	}
	s.TailBytes = 3
	round, err = Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if round.Tail != "4\n5" {
		t.Errorf("tail with TailBytes 3: %q, want the last 3 bytes", round.Tail)
	}
}

func TestRunHandlesLineWithoutFinalNewline(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	data := bytes.TrimRight(readFixture(t, "01-plain-json", "stdout"), "\n")
	os.WriteFile(out, data, 0o644)
	s := fakeSpec(t, plan{Mode: "replay", Stdout: out})
	s.ReadOnly = true
	round, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if round.Class != ClassOK || round.Text != "OK" {
		t.Errorf("class %q text %q", round.Class, round.Text)
	}
}

// ---- cancel

func TestCancelAfterCleanExitKeepsTheRound(t *testing.T) {
	// The fake exits 0 at once, but its grandchild holds stdout, so Run is
	// still waiting for the pipe when the context ends. The round is done.
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	p := replayPlanExit(t, "01-plain-json")
	p.Mode = "leave"
	p.PidFile = pidFile
	s := fakeSpec(t, p)
	s.ReadOnly = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.OnEvent = func(ev Event) {
		if ev.Kind == EventResult {
			go func() {
				time.Sleep(300 * time.Millisecond) // the fake has exited by then
				cancel()
			}()
		}
	}
	round, err := Run(ctx, s)
	if err != nil {
		t.Fatalf("a cancel after a clean exit turned the round into an error: %v", err)
	}
	if round.Class != ClassOK || round.ExitCode != 0 || round.Text != "OK" {
		t.Errorf("class %q exit %d text %q", round.Class, round.ExitCode, round.Text)
	}
	if ctx.Err() == nil {
		t.Error("the context was never cancelled; the test did not exercise the case")
	}
	if data, err := os.ReadFile(pidFile); err == nil {
		pid, _ := strconv.Atoi(string(data))
		deadline := time.Now().Add(5 * time.Second)
		for processAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if processAlive(pid) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("grandchild %d still running", pid)
		}
	}
}

func TestGrandchildIsKilledWhenRoundEnds(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	p := replayPlanExit(t, "01-plain-json")
	p.Mode = "leave"
	p.PidFile = pidFile
	s := fakeSpec(t, p)
	s.ReadOnly = true
	round, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if round.Class != ClassOK {
		t.Errorf("class %q", round.Class)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(data))
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processAlive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("grandchild %d still running after the round ended", pid)
	}
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func TestCancelKillsProcessGroup(t *testing.T) {
	for _, holds := range []bool{false, true} {
		name := "grandchild off the pipes"
		if holds {
			name = "grandchild holds stdout"
		}
		t.Run(name, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "child.pid")
			s := fakeSpec(t, plan{Mode: "spawn", PidFile: pidFile, ChildHolds: holds})
			s.ReadOnly = true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			var once bool
			s.OnEvent = func(ev Event) {
				if ev.Kind == EventInit && !once {
					once = true
					close(started)
				}
			}
			type out struct {
				round Round
				err   error
			}
			done := make(chan out, 1)
			go func() {
				r, err := Run(ctx, s)
				done <- out{r, err}
			}()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("the fake never started")
			}
			data, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil || !processAlive(pid) {
				t.Fatalf("grandchild pid %q alive=%v (%v)", data, processAlive(pid), err)
			}
			cancelled := time.Now()
			cancel()
			var o out
			select {
			case o = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("Run did not return after cancel")
			}
			if took := time.Since(cancelled); holds && took > waitDelay-500*time.Millisecond {
				// Killing the whole group closes the pipe at once. Killing only the
				// child leaves the grandchild holding it until the wait delay ends.
				t.Errorf("Run took %v to return after cancel", took)
			}
			if !errors.Is(o.err, context.Canceled) {
				t.Errorf("err %v, want context.Canceled", o.err)
			}
			if o.round.SessionID != "sess-cancel" {
				t.Errorf("session %q: a cancelled round keeps the id from the init event", o.round.SessionID)
			}
			deadline := time.Now().Add(5 * time.Second)
			for processAlive(pid) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if processAlive(pid) {
				syscall.Kill(pid, syscall.SIGKILL)
				t.Errorf("grandchild %d still running after cancel", pid)
			}
		})
	}
}

// Decision #85: a start hook must run before Wait, while the child is live.
func TestS17OnStartBeforeWait(t *testing.T) {
	gatePath := filepath.Join(t.TempDir(), "gate")
	s := fakeSpec(t, plan{Mode: "gated", Stdout: fixture("02-stream-json", "stdout"), Gate: gatePath})
	s.ReadOnly = true
	s.OnStart = func(pgid int) {
		if pgid <= 1 {
			t.Errorf("invalid pgid %d", pgid)
		}
		if err := os.WriteFile(gatePath, []byte("go"), 0600); err != nil {
			t.Error(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Run(ctx, s); err != nil {
		t.Fatal(err)
	}
}
