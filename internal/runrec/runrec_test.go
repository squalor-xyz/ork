// SPDX-License-Identifier: GPL-3.0-or-later

package runrec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clitest"
)

func TestMain(m *testing.M) {
	clitest.Main()
	os.Exit(m.Run())
}

var t0 = time.Date(2026, 10, 19, 16, 12, 0, 0, time.UTC)

// stepClock is a fake clock: every call returns the previous time plus step.
type stepClock struct {
	mu   sync.Mutex
	next time.Time
	step time.Duration
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.next
	c.next = c.next.Add(c.step)
	return t
}

func newStore(t *testing.T) *Store {
	t.Helper()
	clock := &stepClock{next: t0, step: time.Minute}
	return &Store{Dir: filepath.Join(t.TempDir(), "runs"), Now: clock.Now}
}

func rec(id string) Record {
	return Record{ID: id, Project: "ork", Item: "S04", Worktree: "/p/.worktrees/S04", Branch: "S04", State: StateSetup, Round: 1}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCreateWritesRunJSONAndOpenReadsItBack(t *testing.T) {
	s := newStore(t)
	r := rec("ork-20261019-161200-a1b2")
	r.Roles = []RoleRecord{{Role: "implementer", Round: 1, Vendor: "claude", PGID: 4242, StartedAt: t0}}
	r.Lineup = map[string][]LineupEntry{"implementer": {{Vendor: "claude", Model: "sonnet", Effort: "high"}}}
	run, err := s.Create(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, r.ID, "run.json")
	if run.Dir() != filepath.Join(s.Dir, r.ID) || s.RunDir(r.ID) != run.Dir() {
		t.Fatalf("Dir() = %q, RunDir = %q", run.Dir(), s.RunDir(r.ID))
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["version"] != float64(1) || raw["id"] != r.ID || raw["item"] != "S04" || raw["state"] != "setup" {
		t.Fatalf("run.json: %v", raw)
	}
	for _, key := range []string{"roles", "checks", "approvals", "questions", "writes"} {
		if _, ok := raw[key].([]any); !ok {
			t.Errorf("%s must be a list, got %v", key, raw[key])
		}
	}
	if raw["commit"] != nil {
		t.Errorf("commit must be null before a commit, got %v", raw["commit"])
	}
	if !strings.HasPrefix(readFile(t, path), "{\n  \"version\": 1,") {
		t.Errorf("run.json is not indented JSON:\n%s", readFile(t, path))
	}
	got, err := s.Open(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	snap := got.Snapshot()
	if snap.Version != Version || snap.CreatedAt != t0 || snap.UpdatedAt != t0 {
		t.Fatalf("version and times: %+v", snap)
	}
	if len(snap.Roles) != 1 || snap.Roles[0].PGID != 4242 || !snap.Roles[0].Running() {
		t.Fatalf("roles: %+v", snap.Roles)
	}
	if !reflect.DeepEqual(snap.Lineup, r.Lineup) {
		t.Fatalf("lineup: %+v", snap.Lineup)
	}
}

func TestCreateRefusesAnExistingRunAndAnEmptyID(t *testing.T) {
	s := newStore(t)
	if _, err := s.Create(rec("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(rec("a")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second create: %v", err)
	}
	for _, bad := range []string{"", "../x", "a/b"} {
		if _, err := s.Create(rec(bad)); err == nil {
			t.Errorf("id %q was accepted", bad)
		}
	}
}

func TestUpdateWritesAtomicallyAndStampsUpdatedAt(t *testing.T) {
	s := newStore(t)
	run, err := s.Create(rec("a"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Update(func(r *Record) { r.State = StateImplement; r.Round = 2 }); err != nil {
		t.Fatal(err)
	}
	snap := run.Snapshot()
	if snap.State != StateImplement || snap.Round != 2 || !snap.UpdatedAt.After(snap.CreatedAt) {
		t.Fatalf("snapshot: %+v", snap)
	}
	reopened, err := s.Open("a")
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Snapshot(); got.State != StateImplement || got.Round != 2 {
		t.Fatalf("reopened: %+v", got)
	}
	entries, _ := os.ReadDir(run.Dir())
	if len(entries) != 1 || entries[0].Name() != "run.json" {
		t.Fatalf("directory holds %v, want run.json only", entries)
	}
}

func TestFailedUpdateLeavesTheFileAndTheRecordAsTheyWere(t *testing.T) {
	s := newStore(t)
	run, err := s.Create(rec("a"))
	if err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(run.Dir(), "run.json"))
	boom := errors.New("disk full")
	orig := writeTemp
	writeTemp = func(f *os.File, data []byte) error { return boom }
	defer func() { writeTemp = orig }()
	if err := run.Update(func(r *Record) { r.State = StateReview }); !errors.Is(err, boom) {
		t.Fatalf("Update: %v", err)
	}
	writeTemp = orig
	if got := readFile(t, filepath.Join(run.Dir(), "run.json")); got != before {
		t.Fatalf("run.json changed by a failed write:\n%s", got)
	}
	if got := run.Snapshot().State; got != StateSetup {
		t.Fatalf("record kept the failed change: state %q", got)
	}
	entries, _ := os.ReadDir(run.Dir())
	if len(entries) != 1 {
		t.Fatalf("a temporary file was left behind: %v", entries)
	}
}

func TestOpenRefusesWhatItDoesNotKnow(t *testing.T) {
	s := newStore(t)
	run, _ := s.Create(rec("a"))
	path := filepath.Join(run.Dir(), "run.json")
	good := readFile(t, path)
	for name, bad := range map[string]string{
		"version":  strings.Replace(good, `"version": 1`, `"version": 2`, 1),
		"key":      strings.Replace(good, `"version": 1,`, `"version": 1, "surprise": true,`, 1),
		"trailing": good + "{}",
		"json":     "{",
	} {
		if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Open("a"); err == nil {
			t.Errorf("%s: Open accepted the file", name)
		} else if name == "version" && !strings.Contains(err.Error(), "version 2") {
			t.Errorf("version error does not name the version: %v", err)
		} else if name == "key" && !strings.Contains(err.Error(), "surprise") {
			t.Errorf("unknown key error does not name the key: %v", err)
		}
	}
	if _, err := s.Open("nope"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing run: %v", err)
	}
}

func TestDeniedCommandTextNamesTheCallAndItsDetail(t *testing.T) {
	for _, tc := range []struct {
		d    DeniedCommand
		want string
	}{
		{DeniedCommand{Tool: "Bash", Command: "git push origin main"}, "Bash: git push origin main"},
		{DeniedCommand{Tool: "Write", Command: "/x.txt"}, "Write: /x.txt"},
		{DeniedCommand{Tool: "Write"}, "Write"},
	} {
		if got := tc.d.Text(); got != tc.want {
			t.Errorf("Text() of %+v is %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestDeniedCommandsRoundTripThroughRunJSON(t *testing.T) {
	s := newStore(t)
	r := rec("denied")
	r.Roles = []RoleRecord{{Role: "implementer", Round: 1, Vendor: "claude", StartedAt: t0,
		Denials:        []string{"Bash", "Write"},
		DeniedCommands: []DeniedCommand{{Tool: "Bash", Command: "git status"}, {Tool: "Write"}}}}
	run, err := s.Create(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Open("denied")
	if err != nil {
		t.Fatal(err)
	}
	if roles := got.Snapshot().Roles; len(roles) != 1 || !reflect.DeepEqual(roles[0].DeniedCommands, r.Roles[0].DeniedCommands) {
		t.Fatalf("denied commands did not round-trip: %+v", roles)
	}
	if strings.Contains(readFile(t, filepath.Join(run.Dir(), "run.json")), `"command": ""`) {
		t.Errorf("an entry with no detail must omit command")
	}
}

func TestRunRecordFromBeforeS45LoadsWithoutCommands(t *testing.T) {
	// The run.json a pre-S45 ork wrote: a role with denials and no denied_commands.
	const legacy = `{"version":1,"id":"legacy","project":"ork","item":"S04","worktree":"/p/.worktrees/S04","branch":"S04",
"contract":7,"state":"stopped","round":1,
"roles":[{"role":"implementer","round":1,"vendor":"claude","started_at":"2026-10-19T16:12:00Z","ended_at":"2026-10-19T16:20:00Z","exit":"error","denials":["Bash","Write"]}],
"checks":[],"approvals":[],"questions":[],"writes":[],"created_at":"2026-10-19T16:12:00Z","updated_at":"2026-10-19T16:20:00Z"}`
	s := newStore(t)
	if err := os.MkdirAll(s.RunDir("legacy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.RunDir("legacy"), "run.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Open("legacy")
	if err != nil {
		t.Fatalf("pre-S45 record refused: %v", err)
	}
	if role := got.Snapshot().Roles[0]; !reflect.DeepEqual(role.Denials, []string{"Bash", "Write"}) || role.DeniedCommands != nil {
		t.Fatalf("pre-S45 role: %+v", role)
	}
	run, err := s.Open("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Update(func(r *Record) { r.State = StateCommitted }); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, filepath.Join(s.RunDir("legacy"), "run.json")), "denied_commands") {
		t.Errorf("a record without commands must not write denied_commands")
	}
}

func TestStateDocSaysACommandIsCappedNotScrubbed(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "state.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.Join(strings.Fields(string(b)), " ")
	for _, want := range []string{"denied_commands", "200 characters", "A recorded command can hold secrets", "capped, not scrubbed", "pre-S45 ork refuses"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/state.md lacks %q", want)
		}
	}
}

func TestListIsOldestFirstAndKeepsGoodRunsBesideACorruptOne(t *testing.T) {
	s := newStore(t)
	for _, id := range []string{"c", "a", "b"} {
		if _, err := s.Create(rec(id)); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(s.Dir, "empty-dir"), 0o755) // no run.json: ignored
	runs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range runs {
		ids = append(ids, r.Snapshot().ID)
	}
	if want := []string{"c", "a", "b"}; !reflect.DeepEqual(ids, want) { // creation order
		t.Fatalf("List = %v, want %v", ids, want)
	}
	os.WriteFile(filepath.Join(s.Dir, "a", "run.json"), []byte("{"), 0o644)
	runs, err = s.List()
	if err == nil || !strings.Contains(err.Error(), "a") || len(runs) != 2 {
		t.Fatalf("with a corrupt record: %d runs, err %v", len(runs), err)
	}
}

func TestListOfAMissingDirectoryIsEmpty(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "never-made")}
	runs, err := s.List()
	if err != nil || len(runs) != 0 {
		t.Fatalf("%v, %v", runs, err)
	}
}

func TestAddWritePersistsAndIsSafeFromManyGoroutines(t *testing.T) {
	s := newStore(t)
	run, _ := s.Create(rec("a"))
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := run.AddWrite(Write{Tool: "slicer", Root: "/r", Argv: []string{"note", fmt.Sprint(i)}}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	reopened, err := s.Open("a")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(reopened.Snapshot().Writes); n != 20 {
		t.Fatalf("%d writes persisted, want 20", n)
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	s := newStore(t)
	run, _ := s.Create(rec("a"))
	run.AddWrite(Write{Tool: "git", Argv: []string{"commit"}})
	snap := run.Snapshot()
	snap.Writes[0].Argv[0] = "changed"
	snap.Writes = append(snap.Writes, Write{})
	if got := run.Snapshot().Writes; len(got) != 1 || got[0].Argv[0] != "commit" {
		t.Fatalf("a caller changed the record through its snapshot: %+v", got)
	}
}

func TestPruneKeepsTheLatestFinishedAndOnlyRemovesLanded(t *testing.T) {
	s := newStore(t)
	mk := func(id, state string) {
		r := rec(id)
		r.State = state
		if _, err := s.Create(r); err != nil {
			t.Fatal(err)
		}
	}
	mk("r1", StateCommitted) // oldest
	mk("r2", StateStopped)
	mk("r3", StateCommitted)
	mk("r4", StatePaused) // not finished
	mk("r5", StateCommitted)
	mk("r6", StateImplement) // active
	exists := func(id string) bool {
		_, err := os.Stat(filepath.Join(s.Dir, id))
		return err == nil
	}

	removed, err := s.Prune(2, nil)
	if err != nil || len(removed) != 0 {
		t.Fatalf("a nil landed predicate must remove nothing: %v, %v", removed, err)
	}

	landed := func(r Record) bool { return r.ID != "r2" } // r2's branch is not landed
	removed, err = s.Prune(2, landed)
	if err != nil {
		t.Fatal(err)
	}
	// Finished runs: r1, r2, r3, r5. The latest 2 (r3, r5) stay; r1 and r2 are
	// the excess, and only r1 is landed.
	if want := []string{"r1"}; !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed %v, want %v", removed, want)
	}
	for id, want := range map[string]bool{"r1": false, "r2": true, "r3": true, "r4": true, "r5": true, "r6": true} {
		if exists(id) != want {
			t.Errorf("run %s present = %v, want %v", id, exists(id), want)
		}
	}
}

func TestDefaultDirHonorsXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x/state")
	got, err := DefaultDir()
	if err != nil || got != "/x/state/ork/runs" {
		t.Fatalf("%q, %v", got, err)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/h")
	if got, err := DefaultDir(); err != nil || got != "/h/.local/state/ork/runs" {
		t.Fatalf("%q, %v", got, err)
	}
}

// ---- Writer ----

func TestWriterReportsRunRecordFailure(t *testing.T) {
	run, err := newStore(t).Create(rec("run-a"))
	if err != nil {
		t.Fatal(err)
	}
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"x", "--root", "/w/S04"}, Stdout: "ok"})
	boom := errors.New("run record disk full")
	orig := writeTemp
	writeTemp = func(*os.File, []byte) error { return boom }
	t.Cleanup(func() { writeTemp = orig })
	_, err = (Writer{Sink: run}).Run(context.Background(), slicerCmd(fake, "x"))
	if !errors.Is(err, boom) {
		t.Fatalf("write recording failure was hidden: %v", err)
	}
}

func TestWriterDoesNotRetryAnUnrecordedLockedWrite(t *testing.T) {
	run, err := newStore(t).Create(rec("run-a"))
	if err != nil {
		t.Fatal(err)
	}
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"x", "--root", "/w/S04"}, Seq: []clitest.Reply{
		{Stdout: lockedJSON, Exit: 3}, {Stdout: "retried"},
	}})
	boom := errors.New("run record disk full")
	orig := writeTemp
	writeTemp = func(*os.File, []byte) error { return boom }
	t.Cleanup(func() { writeTemp = orig })
	w := Writer{Sink: run, Sleep: (&sleeper{}).Sleep}
	res, _ := w.Run(context.Background(), slicerCmd(fake, "x"))
	if string(res.Stdout) != lockedJSON {
		t.Fatalf("unrecorded write was retried: %q", res.Stdout)
	}
}

const lockedJSON = `{"error":{"code":"locked","message":"another slicer holds the lock","command":"note"}}` + "\n"

type sleeper struct{ delays []time.Duration }

func (s *sleeper) Sleep(ctx context.Context, d time.Duration) error {
	s.delays = append(s.delays, d)
	return ctx.Err()
}

type memSink struct{ writes []Write }

func (m *memSink) AddWrite(w Write) error { m.writes = append(m.writes, w); return nil }

func slicerCmd(fake *clitest.Fake, argv ...string) Cmd {
	return Cmd{Tool: "slicer", Bin: fake.Bin("slicer"), Root: "/w/S04", Argv: argv, Args: append(append([]string{}, argv...), "--root", "/w/S04")}
}

func TestWriterLogsASuccessfulWrite(t *testing.T) {
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"note", "S04", "--root", "/w/S04"}, Stdout: "ok\n"})
	sink := &memSink{}
	clock := &stepClock{next: t0, step: 7 * time.Millisecond}
	w := Writer{Sink: sink, Now: clock.Now, Sleep: (&sleeper{}).Sleep}
	res, err := w.Run(context.Background(), slicerCmd(fake, "note", "S04"))
	if err != nil || string(res.Stdout) != "ok\n" {
		t.Fatalf("%q, %v", res.Stdout, err)
	}
	want := []Write{{At: t0, Tool: "slicer", Root: "/w/S04", Argv: []string{"note", "S04"}, Exit: 0, MS: 7}}
	if !reflect.DeepEqual(sink.writes, want) {
		t.Fatalf("writes %+v\nwant   %+v", sink.writes, want)
	}
}

func TestWriterRetriesALockedExitWithBackoffAndLogsEachAttempt(t *testing.T) {
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"note", "S04", "--root", "/w/S04"}, Seq: []clitest.Reply{
		{Stdout: lockedJSON, Exit: 3}, {Stdout: lockedJSON, Exit: 3}, {Stdout: "ok\n"},
	}})
	sink := &memSink{}
	sl := &sleeper{}
	clock := &stepClock{next: t0, step: time.Millisecond}
	w := Writer{Sink: sink, Now: clock.Now, Sleep: sl.Sleep, Backoff: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}}
	res, err := w.Run(context.Background(), slicerCmd(fake, "note", "S04"))
	if err != nil || string(res.Stdout) != "ok\n" {
		t.Fatalf("%q, %v", res.Stdout, err)
	}
	if want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}; !reflect.DeepEqual(sl.delays, want) {
		t.Fatalf("delays %v, want %v", sl.delays, want)
	}
	if len(sink.writes) != 3 {
		t.Fatalf("%d entries: %+v", len(sink.writes), sink.writes)
	}
	for i, want := range []struct {
		exit  int
		code  string
		retry int
	}{{3, "locked", 0}, {3, "locked", 1}, {0, "", 2}} {
		g := sink.writes[i]
		if g.Exit != want.exit || g.Code != want.code || g.Retry != want.retry || g.Tool != "slicer" || g.Root != "/w/S04" || g.MS != 1 {
			t.Errorf("entry %d = %+v, want %+v", i, g, want)
		}
	}
	if n := len(fake.CallsTo("slicer")); n != 3 {
		t.Fatalf("the CLI was called %d times, want 3", n)
	}
}

func TestWriterGivesUpAfterTheBackoffIsSpent(t *testing.T) {
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"note", "S04", "--root", "/w/S04"}, Stdout: lockedJSON, Exit: 3})
	sink := &memSink{}
	sl := &sleeper{}
	w := Writer{Sink: sink, Sleep: sl.Sleep, Backoff: []time.Duration{time.Millisecond, time.Millisecond}}
	_, err := w.Run(context.Background(), slicerCmd(fake, "note", "S04"))
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("err %v", err)
	}
	if len(sink.writes) != 3 || len(sl.delays) != 2 {
		t.Fatalf("%d attempts, %d sleeps; want 3 and 2", len(sink.writes), len(sl.delays))
	}
}

func TestWriterDoesNotRetryOtherFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		out  string
		exit int
	}{
		"state error":            {`{"error":{"code":"state","message":"x","command":"note"}}`, 3},
		"usage":                  {`{"error":{"code":"usage","message":"x","command":"note"}}`, 2},
		"locked text but exit 1": {lockedJSON, 1},
		"no code":                {"plain failure", 3},
	} {
		t.Run(name, func(t *testing.T) {
			fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"note", "S04", "--root", "/w/S04"}, Stdout: tc.out, Exit: tc.exit})
			sink := &memSink{}
			sl := &sleeper{}
			w := Writer{Sink: sink, Sleep: sl.Sleep}
			if _, err := w.Run(context.Background(), slicerCmd(fake, "note", "S04")); err == nil {
				t.Fatal("expected an error")
			}
			if len(sink.writes) != 1 || len(sl.delays) != 0 || sink.writes[0].Exit != tc.exit {
				t.Fatalf("attempts %+v, sleeps %v", sink.writes, sl.delays)
			}
		})
	}
}

func TestWriterStopsRetryingWhenTheContextEnds(t *testing.T) {
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"note", "S04", "--root", "/w/S04"}, Stdout: lockedJSON, Exit: 3})
	sink := &memSink{}
	ctx, cancel := context.WithCancel(context.Background())
	w := Writer{Sink: sink, Sleep: func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }}
	_, err := w.Run(ctx, slicerCmd(fake, "note", "S04"))
	if !errors.Is(err, context.Canceled) || len(sink.writes) != 1 {
		t.Fatalf("err %v, %d attempts", err, len(sink.writes))
	}
}

func TestWriterLogsACallThatCouldNotStart(t *testing.T) {
	sink := &memSink{}
	w := Writer{Sink: sink}
	_, err := w.Run(context.Background(), Cmd{Tool: "git", Bin: filepath.Join(t.TempDir(), "no-such-git"), Root: "/p", Argv: []string{"commit"}, Args: []string{"-C", "/p", "commit"}})
	if err == nil || len(sink.writes) != 1 {
		t.Fatalf("err %v, writes %+v", err, sink.writes)
	}
	if g := sink.writes[0]; g.Tool != "git" || g.Exit != -1 || g.Error == "" || !reflect.DeepEqual(g.Argv, []string{"commit"}) {
		t.Fatalf("entry %+v", g)
	}
}

func TestWriterWithoutASinkStillRuns(t *testing.T) {
	fake := clitest.Install(t, clitest.Route{Tool: "slicer", Args: []string{"x", "--root", "/w/S04"}, Stdout: "ok"})
	if _, err := (Writer{}).Run(context.Background(), slicerCmd(fake, "x")); err != nil {
		t.Fatal(err)
	}
}
