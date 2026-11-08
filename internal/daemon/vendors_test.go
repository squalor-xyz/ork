// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
)

const planRaw = `{"project":"p","item":"S1","well_specified":true,"split":false}`

func TestPlanNotesAVendorThatIsOut(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	var before RunPlan
	raw := request(t, d, "run.plan", planRaw)
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(before.Note, "vendor back") {
		t.Fatalf("a plan with no vendor out has a vendor note: %q", before.Note)
	}

	since := time.Now()
	d.runs.deps.Vendors.MarkOut("codex", "You've hit your usage limit", since)
	var plan RunPlan
	if err := json.Unmarshal(request(t, d, "run.plan", planRaw), &plan); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"codex is out until " + since.Add(engine.VendorOutFor).Format("15:04") + " (since " + since.Format("15:04") + "; You've hit your usage limit)",
		"ork vendor back codex",
	} {
		if !strings.Contains(plan.Note, want) {
			t.Errorf("note %q lacks %q", plan.Note, want)
		}
	}

	// A mark the owner set has no end.
	d.runs.deps.Vendors.MarkOutOwner("codex", "marked out by the owner", since)
	var owner RunPlan
	if err := json.Unmarshal(request(t, d, "run.plan", planRaw), &owner); err != nil {
		t.Fatal(err)
	}
	if want := "codex is out until back (since " + since.Format("15:04") + "; marked out by the owner)"; !strings.Contains(owner.Note, want) || !strings.Contains(owner.Note, "ork vendor back codex") {
		t.Errorf("note %q lacks %q", owner.Note, want)
	}
	d.runs.deps.Vendors.MarkOut("codex", "You've hit your usage limit", since) // an exit mark leaves the owner's alone
	d.runs.deps.Vendors.Back("codex")
	d.runs.deps.Vendors.MarkOut("codex", "You've hit your usage limit", since)

	// The plan the owner confirmed is no longer the plan: the start is refused.
	stale, _ := json.Marshal(before)
	_, err := d.handle(context.Background(), "run.start", json.RawMessage(`{"project":"p","item":"S1","well_specified":true,"split":false,"expected":`+string(stale)+`}`))
	if err == nil || !strings.Contains(err.Error(), "plan changed") {
		t.Fatalf("run.start with the earlier plan: %v", err)
	}

	// A vendor that no chain uses adds no note.
	var narrow RunPlan
	lineup := `"lineup":{"implementer":[{"vendor":"claude","model":"sonnet","effort":"high"}]}`
	if err := json.Unmarshal(request(t, d, "run.plan", `{"project":"p","item":"S1","well_specified":true,"split":false,`+lineup+`}`), &narrow); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(narrow.Note, "codex") {
		t.Fatalf("codex is in no chain but the note names it: %q", narrow.Note)
	}

	// Back clears the note.
	d.runs.deps.Vendors.Back("codex")
	var after RunPlan
	if err := json.Unmarshal(request(t, d, "run.plan", planRaw), &after); err != nil {
		t.Fatal(err)
	}
	if after.Note != before.Note {
		t.Fatalf("note after back %q, before %q", after.Note, before.Note)
	}
}

func TestVendorsListSetAndBackOverTheWire(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	var list VendorsResult
	listRaw := request(t, d, "vendors.list", ``)
	if err := json.Unmarshal(listRaw, &list); err != nil || len(list.VendorsOut) != 0 || !strings.Contains(string(listRaw), `"vendors_out":[]`) {
		t.Fatalf("empty list: %s (%v)", listRaw, err)
	}
	if b := request(t, d, "status", `{}`); strings.Contains(string(b), "vendors_out") {
		t.Fatalf("status without a vendor out names the field: %s", b)
	}

	var set VendorsResult
	if err := json.Unmarshal(request(t, d, "vendors.set", `{"vendor":"codex","out":true,"reason":"out of credits"}`), &set); err != nil {
		t.Fatal(err)
	}
	if len(set.VendorsOut) != 1 {
		t.Fatalf("set: %+v", set)
	}
	got := set.VendorsOut[0]
	if got.Vendor != "codex" || got.Reason != "out of credits" || got.Since.IsZero() || !got.Until.IsZero() || set.WasOut {
		t.Fatalf("an owner mark has no Until: %+v", set)
	}
	if b := request(t, d, "vendors.list", ``); strings.Contains(string(b), `"until"`) || strings.Contains(string(b), "was_out") {
		t.Fatalf("an owner mark names an end, or a list names was_out: %s", b)
	}
	if !d.runs.deps.Vendors.IsOut("codex") {
		t.Fatal("the daemon's engines do not see the mark")
	}
	// The default reason names the owner.
	request(t, d, "vendors.set", `{"vendor":"claude","out":true}`)
	if r := d.runs.deps.Vendors.Out()[0]; r.Vendor != "claude" || !strings.Contains(r.Reason, "owner") {
		t.Fatalf("default reason: %+v", r)
	}
	request(t, d, "vendors.set", `{"vendor":"claude","out":false}`)

	for _, method := range []string{"vendors.list", "status", "snapshot"} {
		raw := `{}`
		if method == "snapshot" {
			raw = `{"project":"p"}`
		}
		if method == "vendors.list" {
			raw = ``
		}
		var res struct {
			VendorsOut []VendorOut `json:"vendors_out"`
		}
		if err := json.Unmarshal(request(t, d, method, raw), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.VendorsOut) != 1 || res.VendorsOut[0].Vendor != "codex" || res.VendorsOut[0].Reason != "out of credits" {
			t.Errorf("%s: vendors_out %+v", method, res.VendorsOut)
		}
	}

	var back VendorsResult
	if err := json.Unmarshal(request(t, d, "vendors.set", `{"vendor":"codex","out":false}`), &back); err != nil || len(back.VendorsOut) != 0 || !back.WasOut {
		t.Fatalf("back: %+v (%v)", back, err)
	}
	var again VendorsResult
	if err := json.Unmarshal(request(t, d, "vendors.set", `{"vendor":"codex","out":false}`), &again); err != nil || again.WasOut {
		t.Fatalf("back when not out must say so: %+v (%v)", again, err)
	}
	// An exit mark has an end, 60 minutes after it was made.
	d.runs.deps.Vendors.MarkOut("claude", "limit", time.Now())
	var listed VendorsResult
	if err := json.Unmarshal(request(t, d, "vendors.list", ``), &listed); err != nil || len(listed.VendorsOut) != 1 || !listed.VendorsOut[0].Until.Equal(listed.VendorsOut[0].Since.Add(engine.VendorOutFor)) {
		t.Fatalf("exit mark: %+v (%v)", listed, err)
	}
	d.runs.deps.Vendors.Back("claude")
	if d.runs.deps.Vendors.IsOut("codex") {
		t.Fatal("back did not clear the mark")
	}

	for _, raw := range []string{`{"vendor":"gemini","out":true}`, `{"vendor":"","out":true}`, `{"out":true}`, `{"vendor":"codex","out":true,"nope":1}`} {
		_, err := d.handle(context.Background(), "vendors.set", json.RawMessage(raw))
		if e := ipc.ErrorOf(err); err == nil || e.Code != CodeBadParams {
			t.Errorf("%s: err %v, want bad_params", raw, err)
		}
	}
	if len(d.runs.deps.Vendors.Out()) != 0 {
		t.Fatalf("a refused call marked something: %+v", d.runs.deps.Vendors.Out())
	}
}

func TestVendorOutAndBackAcceptGrok(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	var set VendorsResult
	if err := json.Unmarshal(request(t, d, "vendors.set", `{"vendor":"grok","out":true,"reason":"credits"}`), &set); err != nil || len(set.VendorsOut) != 1 || set.VendorsOut[0].Vendor != "grok" || !d.runs.deps.Vendors.IsOut("grok") {
		t.Fatalf("out: %+v %v", set, err)
	}
	var back VendorsResult
	if err := json.Unmarshal(request(t, d, "vendors.set", `{"vendor":"grok","out":false}`), &back); err != nil || !back.WasOut || d.runs.deps.Vendors.IsOut("grok") {
		t.Fatalf("back: %+v %v", back, err)
	}
	_, err := d.handle(context.Background(), "vendors.set", json.RawMessage(`{"vendor":"acme","out":true}`))
	if err == nil || !strings.Contains(err.Error(), "use claude, codex or grok") {
		t.Fatalf("refusal names the vendors: %v", err)
	}
	// Grok has no slot cap (owner 2026-11-03).
	if _, ok := defaultVendorCaps["grok"]; ok {
		t.Fatalf("defaultVendorCaps %v", defaultVendorCaps)
	}
}

func TestDaemonSharesOneVendorsValueAndDefaultsCodexToOne(t *testing.T) {
	var mu sync.Mutex
	var started []string
	release := make(chan struct{})
	var active, peak atomic.Int32
	d, _, _ := runDaemon(t, func(ctx context.Context, r engine.Request) (engine.Result, error) {
		mu.Lock()
		started = append(started, r.Worktree)
		mu.Unlock()
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-release:
			return engine.Result{Exit: engine.ExitAuth}, nil // ends the run
		case <-ctx.Done():
			return engine.Result{}, ctx.Err()
		}
	})
	if d.runs.deps.Vendors == nil {
		t.Fatal("the daemon has no Vendors value")
	}
	codex := `"lineup":{"implementer":[{"vendor":"codex","model":"gpt","effort":"high"}]}`
	request(t, d, "run.start", `{"project":"p","item":"S1",`+codex+`}`)
	request(t, d, "run.start", `{"project":"p","item":"S2",`+codex+`}`)

	// The second run waits for the one codex slot and says so.
	deadline := time.Now().Add(5 * time.Second)
	var waiting []EngineRun
	for time.Now().Before(deadline) {
		waiting = waiting[:0]
		for _, row := range d.engineRuns("p") {
			if row.VendorWait != "" {
				waiting = append(waiting, row)
			}
		}
		if len(waiting) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(waiting) != 1 || waiting[0].VendorWait != "codex" {
		t.Fatalf("waiting runs: %+v", waiting)
	}
	// The first run's round starts (it holds the slot), and the second does not
	// start while it holds it.
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(started)
	}
	for deadline = time.Now().Add(5 * time.Second); count() == 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if n := count(); n != 1 {
		close(release)
		t.Fatalf("%d codex rounds started while the slot was held, want 1", n)
	}

	close(release)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(started)
		mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(started) != 2 || peak.Load() != 1 {
		t.Fatalf("started %v, peak codex rounds at once %d, want 2 and 1", started, peak.Load())
	}
}

func TestDaemonKeepsTheVendorsValueItWasGiven(t *testing.T) {
	v := engine.NewVendors(map[string]int{"codex": 3})
	d := New(Config{Paths: PathsIn(t.TempDir()), Source: &stubSource{}, Engine: &engine.Deps{Vendors: v}})
	t.Cleanup(d.cache.Close)
	if d.runs.deps.Vendors != v {
		t.Fatal("the daemon replaced the Vendors value it was given")
	}
}

// The client carries the vendors that are out into the snapshot the TUI shows,
// and the vendor a run waits for with its row.
func TestClientCarriesVendorsOutAndTheWaitingRun(t *testing.T) {
	since := time.Date(2026, 10, 29, 14, 5, 0, 0, time.UTC)
	out := []VendorOut{{Vendor: "codex", Reason: "usage limit", Since: since, Until: since.Add(time.Hour)}}
	socket := filepath.Join(shortDir(t), "s")
	listener, err := ipc.Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &ipc.Server{Handler: func(_ context.Context, method string, raw json.RawMessage) (any, error) {
		switch method {
		case MethodSnapshot:
			return SnapshotResult{VendorsOut: out, Project: registry.Project{Name: "ork"}, EngineRuns: []EngineRun{{ID: "r1", VendorWait: "codex"}}}, nil
		case MethodVendorsList:
			return VendorsResult{VendorsOut: out}, nil
		case MethodVendorsSet:
			var got SetVendorParams
			if err := json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, SetVendorParams{Vendor: "codex", Out: true, Reason: "why"}) {
				return nil, fmt.Errorf("wrong set request: %s", raw)
			}
			return VendorsResult{VendorsOut: out}, nil
		}
		return nil, fmt.Errorf("unexpected %s", method)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server did not stop")
		}
	})
	c, err := Dial(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	snap := c.Snapshot(ctx, registry.Project{Name: "ork"})
	if !reflect.DeepEqual(snap.VendorsOut, out) || len(snap.EngineRuns) != 1 || snap.EngineRuns[0].VendorWait != "codex" {
		t.Fatalf("snapshot: vendors %+v, runs %+v", snap.VendorsOut, snap.EngineRuns)
	}
	if res, err := c.Vendors(ctx); err != nil || !reflect.DeepEqual(res.VendorsOut, out) {
		t.Fatalf("Vendors: %+v %v", res, err)
	}
	if res, err := c.SetVendor(ctx, SetVendorParams{Vendor: "codex", Out: true, Reason: "why"}); err != nil || !reflect.DeepEqual(res.VendorsOut, out) {
		t.Fatalf("SetVendor: %+v %v", res, err)
	}
}

// daemonOn starts another daemon on the state directory of root, like a
// restart of the one runDaemonIn made.
func daemonOn(t *testing.T, root string) *Daemon {
	t.Helper()
	d := New(Config{Paths: PathsIn(filepath.Join(root, "state")), Source: &stubSource{}, Engine: &engine.Deps{}})
	t.Cleanup(d.cache.Close)
	t.Cleanup(d.closeRuns)
	return d
}

func listedVendors(t *testing.T, d *Daemon) []VendorOut {
	t.Helper()
	var res VendorsResult
	if err := json.Unmarshal(request(t, d, "vendors.list", ``), &res); err != nil {
		t.Fatal(err)
	}
	return res.VendorsOut
}

func TestOwnerVendorMarksSurviveADaemonRestart(t *testing.T) {
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil)
	end := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	request(t, d, "vendors.set", `{"vendor":"codex","out":true,"reason":"credits tomorrow"}`)
	request(t, d, "vendors.set", fmt.Sprintf(`{"vendor":"claude","out":true,"until":%q}`, end.Format(time.RFC3339)))
	request(t, d, "vendors.set", `{"vendor":"grok","out":true}`)
	d.runs.deps.Vendors.MarkOut("grok", "limit", time.Now()) // an exit mark leaves the owner's alone
	request(t, d, "vendors.set", `{"vendor":"grok","out":false}`)
	d.runs.deps.Vendors.MarkOut("grok", "limit", time.Now()) // memory only

	restarted := daemonOn(t, root)
	got := listedVendors(t, restarted)
	if len(got) != 2 || got[0].Vendor != "claude" || got[1].Vendor != "codex" {
		t.Fatalf("the owner marks come back after a restart, and the exit mark does not: %+v", got)
	}
	if !got[0].Until.Equal(end) || got[0].Reason != "marked out by the owner" {
		t.Errorf("claude: %+v, want an end at %v", got[0], end)
	}
	if !got[1].Until.IsZero() || got[1].Reason != "credits tomorrow" {
		t.Errorf("codex: %+v, want no end and the reason", got[1])
	}
	if !restarted.runs.deps.Vendors.IsOut("codex") {
		t.Fatal("the restarted daemon's engines do not see the mark")
	}
	// The status and the run plan of the new daemon say so too.
	var status StatusResult
	if err := json.Unmarshal(request(t, restarted, "status", `{}`), &status); err != nil || len(status.VendorsOut) != 2 {
		t.Fatalf("status: %+v %v", status, err)
	}

	// Back on the new daemon ends it for the next restart too.
	request(t, restarted, "vendors.set", `{"vendor":"codex","out":false}`)
	request(t, restarted, "vendors.set", `{"vendor":"claude","out":false}`)
	if again := listedVendors(t, daemonOn(t, root)); len(again) != 0 {
		t.Fatalf("a mark that was back came back: %+v", again)
	}
}

func TestVendorsSetUntilEndsTheMarkAndIsChecked(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	end := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	var set VendorsResult
	if err := json.Unmarshal(request(t, d, "vendors.set", fmt.Sprintf(`{"vendor":"codex","out":true,"reason":"weekly limit","until":%q}`, end.Format(time.RFC3339))), &set); err != nil {
		t.Fatal(err)
	}
	if len(set.VendorsOut) != 1 || !set.VendorsOut[0].Until.Equal(end) || set.VendorsOut[0].Reason != "weekly limit" {
		t.Fatalf("set with an end: %+v", set)
	}
	for _, method := range []string{"status", "snapshot"} {
		raw := `{}`
		if method == "snapshot" {
			raw = `{"project":"p"}`
		}
		var res struct {
			VendorsOut []VendorOut `json:"vendors_out"`
		}
		if err := json.Unmarshal(request(t, d, method, raw), &res); err != nil || len(res.VendorsOut) != 1 || !res.VendorsOut[0].Until.Equal(end) {
			t.Errorf("%s: %+v %v", method, res.VendorsOut, err)
		}
	}
	// An owner mark without an end replaces it.
	request(t, d, "vendors.set", `{"vendor":"codex","out":true}`)
	if got := d.runs.deps.Vendors.Out(); len(got) != 1 || !got[0].Until.IsZero() {
		t.Fatalf("a mark with no end replaces one with an end: %+v", got)
	}
	d.runs.deps.Vendors.Back("codex")

	past := time.Now().Add(-time.Minute).Format(time.RFC3339)
	for _, raw := range []string{
		`{"vendor":"codex","out":true,"until":"` + past + `"}`,
		`{"vendor":"codex","out":true,"until":"not a time"}`,
		`{"vendor":"codex","out":false,"until":"` + end.Format(time.RFC3339) + `"}`,
	} {
		_, err := d.handle(context.Background(), "vendors.set", json.RawMessage(raw))
		if e := ipc.ErrorOf(err); err == nil || e.Code != CodeBadParams {
			t.Errorf("%s: err %v, want bad_params", raw, err)
		}
	}
	if got := d.runs.deps.Vendors.Out(); len(got) != 0 {
		t.Fatalf("a refused call marked something: %+v", got)
	}

	// The end is on the wire only when there is one.
	b, _ := json.Marshal(SetVendorParams{Vendor: "codex", Out: true})
	if strings.Contains(string(b), "until") {
		t.Errorf("a call with no end names one: %s", b)
	}
	b, _ = json.Marshal(SetVendorParams{Vendor: "codex", Out: true, Until: end})
	if !strings.Contains(string(b), `"until":"`+end.Format(time.RFC3339)) {
		t.Errorf("a call with an end lacks it: %s", b)
	}
}

func TestOwnerVendorMarkThatCannotBeStoredIsRefused(t *testing.T) {
	root := t.TempDir()
	// The state directory is a file, so nothing can be stored under it.
	if err := os.WriteFile(filepath.Join(root, "state"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, _ := runDaemonIn(t, root, nil)
	_, err := d.handle(context.Background(), "vendors.set", json.RawMessage(`{"vendor":"codex","out":true}`))
	if err == nil || !strings.Contains(err.Error(), "vendor") {
		t.Fatalf("a mark that cannot be stored must be refused, naming the vendor marks: %v", err)
	}
	if e := ipc.ErrorOf(err); e.Code != ipc.CodeInternal {
		t.Fatalf("code %q, want internal", e.Code)
	}
	if d.runs.deps.Vendors.IsOut("codex") {
		t.Fatal("the daemon says the vendor is out though the mark was not stored")
	}
}

func TestOwnerVendorBackThatCannotBeStoredIsRefusedAndTheMarkStays(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil)
	request(t, d, "vendors.set", `{"vendor":"codex","out":true}`)
	state := filepath.Join(root, "state")
	if err := os.Chmod(state, 0o500); err != nil { // nothing can be created in it
		t.Fatal(err)
	}
	defer os.Chmod(state, 0o700)
	_, err := d.handle(context.Background(), "vendors.set", json.RawMessage(`{"vendor":"codex","out":false}`))
	if e := ipc.ErrorOf(err); err == nil || e.Code != ipc.CodeInternal || !strings.Contains(err.Error(), "vendor marks") {
		t.Fatalf("a back that cannot be stored: %v", err)
	}
	if !d.runs.deps.Vendors.IsOut("codex") {
		t.Fatal("the mark ended in memory though its end was not stored")
	}
}

func TestDaemonLoadsNoStoredVendorItCannotRun(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"marks":[{"vendor":"gemini","since":"2026-10-29T12:00:00Z"},{"vendor":"grok","since":"2026-10-29T12:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(root, "state", "vendors-out.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := listedVendors(t, daemonOn(t, root))
	if len(got) != 1 || got[0].Vendor != "grok" {
		t.Fatalf("a vendor ork cannot run must not be loaded: %+v", got)
	}
}

func TestDaemonStartsWithNoMarksFromACorruptStore(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state", "vendors-out.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := daemonOn(t, root)
	if got := listedVendors(t, d); len(got) != 0 {
		t.Fatalf("marks from a corrupt store: %+v", got)
	}
	// A new mark is stored over it.
	request(t, d, "vendors.set", `{"vendor":"codex","out":true}`)
	if got := listedVendors(t, daemonOn(t, root)); len(got) != 1 {
		t.Fatalf("the store was not repaired by the next mark: %+v", got)
	}
}

func TestDaemonWithNoStateDirectoryStoresNothing(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	d := New(Config{Source: &stubSource{}, Engine: &engine.Deps{}})
	t.Cleanup(d.cache.Close)
	t.Cleanup(d.closeRuns)
	request(t, d, "vendors.set", `{"vendor":"codex","out":true}`)
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Fatalf("a daemon with no state directory wrote into the current directory: %v", entries)
	}
}
