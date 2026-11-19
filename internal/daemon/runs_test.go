// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

type roundRunner func(context.Context, engine.Request) (engine.Result, error)

func (f roundRunner) Run(c context.Context, r engine.Request) (engine.Result, error) { return f(c, r) }

var lastFake *clitest.Fake // the fakes of the latest runDaemon

func runDaemon(t *testing.T, runner roundRunner, routes ...clitest.Route) (*Daemon, *runrec.Store, registry.Project) {
	t.Helper()
	return runDaemonIn(t, t.TempDir(), runner, routes...)
}

// runDaemonIn is runDaemon with its own root directory, so a test can name the
// project's paths in its routes: the project is root/p.
func runDaemonIn(t *testing.T, root string, runner roundRunner, routes ...clitest.Route) (*Daemon, *runrec.Store, registry.Project) {
	t.Helper()
	if runner == nil {
		runner = func(context.Context, engine.Request) (engine.Result, error) {
			return engine.Result{}, fmt.Errorf("unexpected role call")
		}
	}
	// Auto-land is off: these runs rest at the approval card and the tests
	// approve by hand (S331). A test of the default sets the field itself.
	noAutoLand := false
	p := registry.Project{Name: "p", Path: filepath.Join(root, "p"), CheckCommand: "go test ./...", AutoLand: &noAutoLand}
	store := &runrec.Store{Dir: filepath.Join(root, "runs")}
	reg := filepath.Join(root, "projects.json")
	if err := registry.Save(reg, &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	fake := clitest.Install(t, append(routes, []clitest.Route{
		clitest.Route{Tool: "slicer", Args: []string{"deps"}, Prefix: true, Stdout: `{"id":"S1"}`},
		clitest.Route{Tool: "slicer", Args: []string{"plan-parallel", "--json", "--lean", "--root", p.Path}, File: filepath.Join("..", "..", "testdata", "cli", "slicer", "plan-parallel.json")},
		clitest.Route{Tool: "slicer", Args: []string{"relay-roles"}, Prefix: true, Stdout: `{"roles":[{"role":"implementer","row":{"agent":"Unknown","vendor":"Claude","model":"wrong"},"alternatives":[{"agent":"Claude Code","vendor":"Acme","model":"sonnet","reasoning":"high"},{"agent":"Codex","vendor":"OpenAI","model":"gpt","reasoning":"high"}]},{"role":"reviewer","row":{"agent":"Claude Code","vendor":"Claude","model":"opus","reasoning":"medium"}},{"role":"architect","row":null,"alternatives":[],"selected_by":"unmatched"}]}`},
		clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", p.Path}, File: filepath.Join("..", "..", "testdata", "cli", "slicer", "relay-v12.json")},
		clitest.Route{Tool: "slicer", Args: []string{"show"}, Prefix: true, Stdout: `{"title":"Title","slice":{"sections":[{"heading":"Git","body":"One commit: \"S1: test\"."}]}}`},
		// slicer has no check_command here, so the registry's stays in force (S246).
		slicerCheckRoute(p.Path, ""),
		clitest.Route{Tool: "slicer", Args: []string{"config"}, Prefix: true, Stdout: `{"value":"done"}`},
		clitest.Route{Tool: "slicer", Args: []string{"check"}, Prefix: true, Stdout: `{"ok":true}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "begin"}, Prefix: true, Stdout: `{"run":{"id":"relay-1"}}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "step"}, Prefix: true, Stdout: `{"step":{"id":"1"}}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run", "end"}, Prefix: true, Stdout: `{}`},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "rev-parse", "--git-path", "info/exclude"}, Stdout: filepath.Join(root, "exclude")},
		clitest.Route{Tool: "git", Args: []string{"-C", filepath.Join(p.Path, ".worktrees", "S1"), "rev-parse", "HEAD"}, Stdout: "abc123\n"},
		// The landing after an approval (S119) on a base branch that did not move.
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "symbolic-ref", "--short", "HEAD"}, Stdout: "main\n"},
		clitest.Route{Tool: "git", Args: []string{"-C", p.Path, "merge-base", "--is-ancestor", "S1", "main"}, Exit: 1},
		clitest.Route{Tool: "slicer", Args: []string{"land"}, Prefix: true, Stdout: `{}`},
		clitest.Route{Tool: "git", Prefix: true},
	}...)...)
	lastFake = fake
	deps := engine.Deps{Store: store, Slicer: slicercli.Client{Bin: fake.Bin("slicer")}, GitBin: fake.Bin("git"), Runner: runner,
		Check: func(context.Context, string, string, time.Duration) (engine.CheckOutput, error) {
			return engine.CheckOutput{OK: true}, nil
		},
		// The setup verification looks at PATH, which a test may replace with a
		// directory of fake launchers (S295).
		Verify: func(context.Context, string, string) []string { return nil }}
	d := New(Config{Paths: PathsIn(filepath.Join(root, "state")), RegistryPath: reg, Source: &stubSource{}, Engine: &deps})
	t.Cleanup(d.cache.Close)
	t.Cleanup(d.closeRuns)
	return d, store, p
}
func request(t *testing.T, d *Daemon, method, raw string) json.RawMessage {
	t.Helper()
	v, err := d.handle(context.Background(), method, json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func waitState(t *testing.T, store *runrec.Store, id, state string) runrec.Record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := store.Open(id)
		if err == nil && r.Snapshot().State == state {
			return r.Snapshot()
		}
		time.Sleep(time.Millisecond)
	}
	r, _ := store.Open(id)
	t.Fatalf("run did not reach %s: %+v", state, r.Snapshot())
	return runrec.Record{}
}

// waitIdle waits until the run's job has ended. The record shows a rest state
// before the job goroutine clears busy.
func waitIdle(t *testing.T, d *Daemon, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d.runs.mu.Lock()
		r := d.runs.runs[id]
		busy := r != nil && r.busy
		d.runs.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("run %s is still busy", id)
}
func TestRunPlanKeepsEveryRowWithAnAdapter(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	b := request(t, d, "run.plan", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	for _, want := range []string{filepath.Join(p.Path, ".worktrees", "S1"), `"branch":"S1"`, `"check_command":"go test ./..."`, `"model":"sonnet"`, `"model":"gpt"`, `"vendor":"codex"`, `"model":"opus"`, `Bash(go test ./...*)`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("plan missing %s: %s", want, b)
		}
	}
	if strings.Contains(string(b), "wrong") {
		t.Fatalf("wrong lineup: %s", b)
	}
	var plan RunPlan
	if err := json.Unmarshal(b, &plan); err != nil {
		t.Fatal(err)
	}
	want := []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}, {Vendor: "codex", Model: "gpt", Effort: "high"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], want) {
		t.Fatalf("ordered adapters: %+v", plan.Lineup)
	}
	runs, err := store.List()
	if err != nil || len(runs) != 0 {
		t.Fatalf("plan created run: %v %v", runs, err)
	}
}

// slicerCheckRoute makes `slicer config check_command --json --root PATH`
// answer with value (S246).
func slicerCheckRoute(path, value string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"config", "check_command", "--json", "--root", path}, Stdout: fmt.Sprintf(`{"key":"check_command","value":%q}`, value)}
}

const planParams = `{"project":"p","item":"S1","well_specified":true,"split":false}`

func planOf(t *testing.T, d *Daemon) RunPlan {
	t.Helper()
	var plan RunPlan
	if err := json.Unmarshal(request(t, d, "run.plan", planParams), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestRunPlanPrefersSlicersCheckCommand(t *testing.T) {
	root := t.TempDir()
	d, _, p := runDaemonIn(t, root, nil, slicerCheckRoute(filepath.Join(root, "p"), "make test"))
	plan, err := d.planRun(context.Background(), RunParams{Project: "p", Item: "S1", WellSpecified: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.CheckCommand != "make test" || plan.CheckSource != "slicer" {
		t.Fatalf("plan check = %q from %q, want make test from slicer (registry has %q)", plan.CheckCommand, plan.CheckSource, p.CheckCommand)
	}
	if plan.project.CheckCommand != "go test ./..." {
		t.Fatalf("plan.project must stay the raw registry entry: %q", plan.project.CheckCommand)
	}
	if got := plan.Permissions[engine.RoleImplementer].Allow; !slices.Contains(got, "Bash(make test*)") {
		t.Fatalf("the implementer allowlist lacks the effective command: %v", got)
	}
}

func TestRunPlanFallsBackToTheRegistryCheckCommand(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	plan := planOf(t, d)
	if plan.CheckCommand != "go test ./..." || plan.CheckSource != "registry" {
		t.Fatalf("slicer empty: plan check = %q from %q", plan.CheckCommand, plan.CheckSource)
	}
	request(t, d, "projects.set", `{"name":"p","check_command":""}`)
	plan = planOf(t, d)
	if plan.CheckCommand != "" || plan.CheckSource != "none" {
		t.Fatalf("both empty: plan check = %q from %q", plan.CheckCommand, plan.CheckSource)
	}
}

func TestRunPlanCheckCommandReadFailureKeepsTheRegistry(t *testing.T) {
	root := t.TempDir()
	d, _, _ := runDaemonIn(t, root, nil, clitest.Route{Tool: "slicer", Args: []string{"config", "check_command", "--json", "--root", filepath.Join(root, "p")}, Stderr: "no config", Exit: 3})
	plan := planOf(t, d)
	if plan.CheckCommand != "go test ./..." || plan.CheckSource != "registry" {
		t.Fatalf("failed read: plan check = %q from %q", plan.CheckCommand, plan.CheckSource)
	}
	if !strings.Contains(plan.Note, "slicer config check_command failed") {
		t.Fatalf("the plan note does not name the failure: %q", plan.Note)
	}
}

func TestRunStartPassesTheEffectiveCheckCommandToTheEngine(t *testing.T) {
	root := t.TempDir()
	d, store, _ := runDaemonIn(t, root, retryRunner, slicerCheckRoute(filepath.Join(root, "p"), "make test"))
	var mu sync.Mutex
	var checks []string
	d.runs.deps.Check = func(_ context.Context, _, command string, _ time.Duration) (engine.CheckOutput, error) {
		mu.Lock()
		checks = append(checks, command)
		mu.Unlock()
		return engine.CheckOutput{OK: true}, nil
	}
	// The owner confirms the plan as shown: the start must not call it changed.
	shown := request(t, d, "run.plan", planParams)
	b := request(t, d, "run.start", `{"project":"p","item":"S1","well_specified":true,"split":false,"expected":`+string(shown)+`}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	waitIdle(t, d, rec.ID)
	mu.Lock()
	defer mu.Unlock()
	if len(checks) == 0 {
		t.Fatal("the engine ran no check")
	}
	for _, c := range checks {
		if c != "make test" {
			t.Fatalf("the engine ran %q, want make test: %v", c, checks)
		}
	}
}

func TestReloadedRunUsesTheEffectiveCheckCommand(t *testing.T) {
	root := t.TempDir()
	d, store, p := runDaemonIn(t, root, retryRunner, slicerCheckRoute(filepath.Join(root, "p"), "make test"))
	lineup := map[string][]runrec.LineupEntry{}
	for _, role := range []string{engine.RoleImplementer, engine.RoleReviewer, engine.RoleGate} {
		lineup[role] = []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet"}}
	}
	if _, err := store.Create(runrec.Record{
		ID: "r1", Project: p.Name, Item: "S1", Worktree: filepath.Join(p.Path, ".worktrees", "S1"),
		State: runrec.StatePaused, Round: 1, Lineup: lineup,
		Pending: &runrec.Pending{Kind: runrec.PendingError, State: runrec.StateChecks, Reason: "check failed"},
	}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var checks []string
	d.runs.deps.Check = func(_ context.Context, _, command string, _ time.Duration) (engine.CheckOutput, error) {
		mu.Lock()
		checks = append(checks, command)
		mu.Unlock()
		return engine.CheckOutput{OK: true}, nil
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	request(t, d, "run.retry", `{"run":"r1"}`)
	mu.Lock()
	defer mu.Unlock()
	if len(checks) == 0 || checks[0] != "make test" {
		t.Fatalf("the reloaded run ran %v, want make test", checks)
	}
}

// modelLists is a fake of the vendors' model lists (S244): it answers from
// names and records each vendor it was asked for.
type modelLists struct {
	mu    sync.Mutex
	names map[string][]string
	calls []string
}

func (m *modelLists) list(_ context.Context, vendor string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, vendor)
	return m.names[vendor], nil
}

func (m *modelLists) called() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

// useModelLists replaces the daemon's vendor list commands with a fake.
func useModelLists(d *Daemon, names map[string][]string) *modelLists {
	l := &modelLists{names: names}
	d.models.list = l.list
	return l
}

var codexModelNames = []string{"gpt-6.1-sol", "gpt-6.2-sol", "gpt-5.5", "gpt-6.3-terra"}

// rolesRoute makes `slicer relay-roles` answer with the roles JSON.
func rolesRoute(roles string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"relay-roles"}, Prefix: true, Stdout: roles}
}

func latestRow(agent, vendor, model, effort string) string {
	return fmt.Sprintf(`{"id":"r9","agent":%q,"vendor":%q,"model":%q,"reasoning":%q}`, agent, vendor, model, effort)
}

func TestRunPlanResolvesLatestWithinAFamily(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":` + latestRow("Codex", "OpenAI", "gpt-*-sol@latest", "high") + `,"alternatives":[` + latestRow("Claude Code", "Anthropic", "sonnet", "high") + `]},{"role":"reviewer","row":` + latestRow("Claude Code", "Anthropic", "opus@latest", "medium") + `}]}`
	d, _, _ := runDaemon(t, nil, rolesRoute(roles))
	useModelLists(d, map[string][]string{"codex": codexModelNames})
	b := request(t, d, MethodRunPlan, `{"project":"p","item":"S1"}`)
	var plan RunPlan
	if err := json.Unmarshal(b, &plan); err != nil {
		t.Fatal(err)
	}
	wantImpl := []runrec.LineupEntry{{Vendor: "codex", Model: "gpt-6.2-sol", Effort: "high", Requested: "gpt-*-sol@latest"}, {Vendor: "claude", Model: "sonnet", Effort: "high"}}
	wantReview := []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium", Requested: "opus@latest"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], wantImpl) {
		t.Errorf("implementer %+v, want %+v", plan.Lineup[engine.RoleImplementer], wantImpl)
	}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], wantReview) {
			t.Errorf("%s %+v, want %+v", role, plan.Lineup[role], wantReview)
		}
	}
	// The plan's JSON carries requested for the three resolved entries and for no pinned one.
	if n := strings.Count(string(b), `"requested"`); n != 3 {
		t.Errorf("%d requested keys in the plan, want 3: %s", n, b)
	}
}

func TestRunPlanRefusesBareLatest(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":` + latestRow("Claude Code", "Anthropic", "sonnet", "high") + `},{"role":"reviewer","row":` + latestRow("Grok", "xAI", "latest", "medium") + `}]}`
	d, _, _ := runDaemon(t, nil, rolesRoute(roles))
	lists := useModelLists(d, map[string][]string{"grok": {"grok-4.7"}})
	_, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1"}`))
	if err == nil || !strings.Contains(err.Error(), `model "latest" names no family; write FAMILY@latest, for example grok-*@latest`) {
		t.Fatalf("a row at bare latest must refuse the plan with the family message: %v", err)
	}
	if got := lists.called(); len(got) != 0 {
		t.Errorf("a refused text ran list commands: %v", got)
	}
}

func TestRunPlanRefusesWhenTheListFails(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":` + latestRow("Codex", "OpenAI", "gpt-*-sol@latest", "high") + `},{"role":"reviewer","row":` + latestRow("Claude Code", "Anthropic", "opus", "medium") + `}]}`
	d, store, _ := runDaemon(t, nil, rolesRoute(roles))
	bin := t.TempDir()
	log := filepath.Join(bin, "log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\necho 'no catalog' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, method := range []string{MethodRunPlan, MethodRunStart} {
		_, err := d.handle(context.Background(), method, json.RawMessage(`{"project":"p","item":"S1"}`))
		if err == nil {
			t.Fatalf("%s: the plan must be refused", method)
		}
		for _, want := range []string{"codex debug models --bundled", "exit 1", "gpt-*-sol"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q lacks %q", method, err, want)
			}
		}
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "debug models --bundled") {
		t.Errorf("the list command was not run: %q", b)
	}
	if runs, err := store.List(); err != nil || len(runs) != 0 {
		t.Errorf("a refused start created runs: %v %v", runs, err)
	}
}

func TestModelListIsReadOncePerDaemon(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":` + latestRow("Codex", "OpenAI", "gpt-*-sol@latest", "high") + `},{"role":"reviewer","row":` + latestRow("Codex", "OpenAI", "gpt-*-terra@latest", "medium") + `,"alternatives":[` + latestRow("Grok", "xAI", "grok-*@latest", "high") + `]}]}`
	d, _, _ := runDaemon(t, nil, rolesRoute(roles))
	lists := useModelLists(d, map[string][]string{"codex": codexModelNames, "grok": {"grok-4.7", "grok-4.10"}})
	for range 2 {
		plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
		if got := plan.Lineup[engine.RoleImplementer]; len(got) != 1 || got[0].Model != "gpt-6.2-sol" {
			t.Fatalf("implementer %+v", got)
		}
		if got := plan.Lineup[engine.RoleReviewer]; len(got) != 2 || got[0].Model != "gpt-6.3-terra" || got[1].Model != "grok-4.10" {
			t.Fatalf("reviewer %+v", got)
		}
	}
	got := lists.called()
	slices.Sort(got)
	if !slices.Equal(got, []string{"codex", "grok"}) {
		t.Fatalf("list calls %v, want one for each vendor", got)
	}
}

func TestResumedRoundKeepsTheRecordedModel(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":` + latestRow("Claude Code", "Anthropic", "sonnet", "high") + `},{"role":"reviewer","row":` + latestRow("Codex", "OpenAI", "gpt-*-sol@latest", "medium") + `}]}`
	var mu sync.Mutex
	var reviewing []string // the vendor and model of each review and gate round
	runner := func(ctx context.Context, r engine.Request) (engine.Result, error) {
		if r.Role == engine.RoleReviewer || r.Role == engine.RoleGate {
			mu.Lock()
			reviewing = append(reviewing, r.Vendor+" "+r.Model)
			mu.Unlock()
		}
		return retryRunner(ctx, r)
	}
	rig := stoppedDaemon(t, runner, false, stoppedDiff, func(rec *runrec.Record) {
		for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
			rec.Lineup[role] = []runrec.LineupEntry{{Vendor: "codex", Model: "gpt-6.2-sol", Effort: "medium", Requested: "gpt-*-sol@latest"}}
		}
	}, rolesRoute(roles))
	useModelLists(rig.d, map[string][]string{"codex": {"gpt-6.2-sol", "gpt-6.3-sol"}})
	// The list has moved on: a fresh plan now says gpt-6.3-sol.
	if plan := launchPlan(t, rig.d, `{"project":"p","item":"S1"}`); plan.Lineup[engine.RoleReviewer][0].Model != "gpt-6.3-sol" {
		t.Fatalf("a new plan must use the current list: %+v", plan.Lineup)
	}
	var reply RunReply
	if err := json.Unmarshal(request(t, rig.d, "run.start", `{"project":"p","item":"S1"}`), &reply); err != nil || reply.ID != "r1" {
		t.Fatalf("run.start must resume r1: %+v %v", reply, err)
	}
	waitIdle(t, rig.d, "r1")
	stored := waitState(t, rig.store, "r1", runrec.StateAwaitingApproval)
	mu.Lock()
	got := append([]string(nil), reviewing...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatal("the resumed run ran no review round")
	}
	for _, ran := range got {
		if ran != "codex gpt-6.2-sol" {
			t.Errorf("a resumed round ran %q, want codex gpt-6.2-sol: %v", ran, got)
		}
	}
	if e := stored.Lineup[engine.RoleReviewer]; len(e) != 1 || e[0].Model != "gpt-6.2-sol" || e[0].Requested != "gpt-*-sol@latest" {
		t.Errorf("the stored lineup changed: %+v", e)
	}
}

func TestLaunchOverrideResolvesLatest(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	lists := useModelLists(d, map[string][]string{"codex": codexModelNames})
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"reviewer":[{"vendor":"codex","model":"gpt-*-sol@latest","effort":"medium"}]}}`)
	want := []runrec.LineupEntry{{Vendor: "codex", Model: "gpt-6.2-sol", Effort: "medium", Requested: "gpt-*-sol@latest"}}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], want) {
			t.Errorf("%s %+v, want %+v", role, plan.Lineup[role], want)
		}
	}
	if got := lists.called(); !slices.Equal(got, []string{"codex"}) {
		t.Errorf("list calls %v", got)
	}
}

// An override replaces a role's lineup before anything is resolved, so it gets
// the owner past a row whose family cannot be resolved.
func TestLaunchOverrideRescuesAFailedResolution(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":` + latestRow("Codex", "OpenAI", "gpt-*-luna@latest", "high") + `},{"role":"reviewer","row":` + latestRow("Claude Code", "Anthropic", "opus", "medium") + `}]}`
	d, _, _ := runDaemon(t, nil, rolesRoute(roles))
	useModelLists(d, map[string][]string{"codex": codexModelNames})
	if _, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1"}`)); err == nil || !strings.Contains(err.Error(), "gpt-*-luna") {
		t.Fatalf("an unmatched family must refuse the plan naming it: %v", err)
	}
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"claude","model":"sonnet","effort":"high"}]}}`)
	if got := plan.Lineup[engine.RoleImplementer]; len(got) != 1 || got[0].Model != "sonnet" {
		t.Fatalf("override %+v", got)
	}
}

func TestSpecChainResolvesLatestWithinAFamily(t *testing.T) {
	recommend := `{"roles":[{"role":"implementer","row":` + latestRow("Claude Code", "Anthropic", "sonnet", "high") + `},{"role":"reviewer","row":` + latestRow("Claude Code", "Anthropic", "opus", "medium") + `,"alternatives":[{"id":"r16","agent":"Codex","vendor":"OpenAI","model":"gpt-*-sol@latest","reasoning":"low"}]}]}`
	rig := newSpecRig(t, rulesJSON(u11), clitest.Route{Tool: "slicer", Args: []string{"relay-roles", "recommend"}, Prefix: true, Stdout: recommend})
	useModelLists(rig.d, map[string][]string{"codex": codexModelNames})
	chain, err := specChainOf(t, rig)
	if err != nil {
		t.Fatal(err)
	}
	want := []runrec.LineupEntry{{Vendor: "codex", Model: "gpt-6.2-sol", Effort: "low", Requested: "gpt-*-sol@latest"}}
	if !reflect.DeepEqual(chain, want) {
		t.Fatalf("chain %+v, want %+v", chain, want)
	}
}

func TestRunPlanPassesALocalRowsModelThrough(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":` + latestRow("Claude Code", "Anthropic", "sonnet", "high") + `},{"role":"reviewer","row":` + latestRow("grok-local", "xAI", "qwen3.8-27b", "medium") + `,"alternatives":[` + latestRow("Codex", "Local", "qwen3.8-27b", "medium") + `]}]}`
	d, _, _ := runDaemon(t, nil, rolesRoute(roles))
	localLaunchers(t)
	lists := useModelLists(d, map[string][]string{"codex": codexModelNames, "grok": {"grok-4.7"}})
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	want := []runrec.LineupEntry{{Vendor: "grok-local", Model: "qwen3.8-27b", Effort: "medium"}, {Vendor: "codex", Model: "qwen3.8-27b", Effort: "medium"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleReviewer], want) {
		t.Fatalf("reviewer %+v, want %+v", plan.Lineup[engine.RoleReviewer], want)
	}
	if got := lists.called(); len(got) != 0 {
		t.Errorf("a local row ran list commands: %v", got)
	}
}

func TestRunPlanRefusesLatestOnALocalRow(t *testing.T) {
	for name, row := range map[string]string{
		"a local launcher": latestRow("grok-local", "xAI", "qwen3.8-27b@latest", "medium"),
		"a Local vendor":   latestRow("Codex", "Local", "gpt-*-sol@latest", "medium"),
	} {
		t.Run(name, func(t *testing.T) {
			roles := `{"roles":[{"role":"implementer","row":` + latestRow("Claude Code", "Anthropic", "sonnet", "high") + `},{"role":"reviewer","row":` + row + `}]}`
			d, _, _ := runDaemon(t, nil, rolesRoute(roles))
			localLaunchers(t)
			lists := useModelLists(d, map[string][]string{"codex": codexModelNames, "grok": {"grok-4.7"}})
			_, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1"}`))
			if err == nil || !strings.Contains(err.Error(), "local models are pinned") {
				t.Fatalf("a local row at @latest must be refused: %v", err)
			}
			if got := lists.called(); len(got) != 0 {
				t.Errorf("list commands ran: %v", got)
			}
		})
	}
	t.Run("an override", func(t *testing.T) {
		d, _, _ := runDaemon(t, nil)
		localLaunchers(t)
		useModelLists(d, nil)
		_, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1","lineup":{"reviewer":[{"vendor":"claude-local","model":"qwen3.8-27b@latest","effort":"medium"}]}}`))
		if err == nil || !strings.Contains(err.Error(), "local models are pinned") {
			t.Fatalf("an override of a local launcher at @latest must be refused: %v", err)
		}
	})
}

// The local mark belongs to its row: a cloud row that gives the same lineup
// entry as a Local row, in another role, is still resolved.
func TestLocalMarkStaysWithItsRow(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":` + latestRow("Codex", "OpenAI", "gpt-*-sol@latest", "high") + `},{"role":"reviewer","row":` + latestRow("Codex", "Local", "gpt-*-sol@latest", "high") + `}]}`
	d, _, _ := runDaemon(t, nil, rolesRoute(roles))
	useModelLists(d, map[string][]string{"codex": codexModelNames})
	_, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1"}`))
	if err == nil || !strings.Contains(err.Error(), "lineup for reviewer") || !strings.Contains(err.Error(), "local models are pinned") {
		t.Fatalf("the Local reviewer row must be refused, and only it: %v", err)
	}
	// An override replaces the Local role, so its mark goes too, and the cloud
	// implementer row and the override resolve as any cloud entry does.
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"reviewer":[{"vendor":"codex","model":"gpt-*-sol@latest","effort":"high"}]}}`)
	want := []runrec.LineupEntry{{Vendor: "codex", Model: "gpt-6.2-sol", Effort: "high", Requested: "gpt-*-sol@latest"}}
	for _, role := range []string{engine.RoleImplementer, engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], want) {
			t.Errorf("%s %+v, want %+v", role, plan.Lineup[role], want)
		}
	}
}

// A Grok alternate comes after the recommended row, never before it (S72). The
// implementer's Grok alternate stays too: Grok writes in dontAsk mode (S305).
func TestPlanKeepsAGrokAlternateAfterTheRecommendedRow(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":{"agent":"Claude Code","vendor":"Anthropic","model":"sonnet","reasoning":"high"},"alternatives":[{"agent":"Grok","vendor":"xAI","model":"grok-*@latest","reasoning":"high"},{"agent":"Codex","vendor":"OpenAI","model":"gpt","reasoning":"high"}]},{"role":"reviewer","row":{"agent":"Claude Code","vendor":"Anthropic","model":"opus","reasoning":"medium"},"alternatives":[{"agent":"Grok","vendor":"xAI","model":"grok-*@latest","reasoning":"high"}]}]}`
	d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"relay-roles"}, Prefix: true, Stdout: roles})
	useModelLists(d, map[string][]string{"grok": {"grok-4.7", "grok-4.7-build-fast", "grok-4.6"}})
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	wantImpl := []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "high"}, {Vendor: "grok", Model: "grok-4.7", Effort: "high", Requested: "grok-*@latest"}, {Vendor: "codex", Model: "gpt", Effort: "high"}}
	wantReview := []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}, {Vendor: "grok", Model: "grok-4.7", Effort: "high", Requested: "grok-*@latest"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], wantImpl) || !reflect.DeepEqual(plan.Lineup[engine.RoleReviewer], wantReview) || !reflect.DeepEqual(plan.Lineup[engine.RoleGate], wantReview) {
		t.Fatalf("lineup %+v", plan.Lineup)
	}
}

// startRunRegistrationDaemon changes the registry inside the fake
// RecommendRoles call, after planRun's read and before startRun's admission.
func startRunRegistrationDaemon(t *testing.T, remove bool) (*Daemon, *runrec.Store, registry.Project, <-chan struct{}) {
	t.Helper()
	return startRunRegistryChange(t, func(f *registry.File) {
		if remove {
			f.Projects = f.Projects[1:]
		}
	})
}

// startRunRegistryChange runs change on the registry inside the fake
// RecommendRoles call, as startRunRegistrationDaemon does. The registry holds
// the project first, then "other".
func startRunRegistryChange(t *testing.T, change func(*registry.File)) (*Daemon, *runrec.Store, registry.Project, <-chan struct{}) {
	t.Helper()
	started := make(chan struct{}, 1)
	d, store, p := runDaemon(t, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
		started <- struct{}{}
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	other := registry.Project{Name: "other", Path: filepath.Join(t.TempDir(), "other")}
	f := &registry.File{Version: registry.Version, Projects: []registry.Project{p, other}}
	if err := registry.Save(d.registryPath(), f); err != nil {
		t.Fatal(err)
	}
	change(f)
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORK_TEST_ADMISSION_REGISTRY", d.registryPath())
	t.Setenv("ORK_TEST_ADMISSION_CONTENT", string(data))
	t.Setenv("ORK_TEST_ADMISSION_SLICER", lastFake.Bin("slicer"))
	bin := filepath.Join(t.TempDir(), "slicer")
	script := `#!/bin/sh
if [ "$1" = relay-roles ] && [ "$2" = recommend ]; then
    printf '%s\n' "$ORK_TEST_ADMISSION_CONTENT" > "$ORK_TEST_ADMISSION_REGISTRY" || exit 1
fi
exec "$ORK_TEST_ADMISSION_SLICER" "$@"
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	d.runs.deps.Slicer = slicercli.Client{Bin: bin}
	return d, store, p, started
}

func TestStartRunRefusesAProjectRemovedAfterPlanning(t *testing.T) {
	d, store, p, _ := startRunRegistrationDaemon(t, true)
	_, err := d.startRun(context.Background(), RunParams{Project: p.Name, Item: "S1"})
	var ipcErr *ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != CodeNotFound || !strings.Contains(ipcErr.Message, `"`+p.Name+`"`) {
		t.Errorf("removed project must fail with not_found naming %q: %v", p.Name, err)
	}
	// Stop any incorrectly admitted job before inspecting its side effects.
	d.closeRuns()
	if runs, err := store.List(); err != nil || len(runs) != 0 {
		t.Errorf("removed project created run records: %v, %v", runs, err)
	}
	if len(d.runs.runs) != 0 {
		t.Errorf("removed project created managed engines: %d", len(d.runs.runs))
	}
	for _, call := range lastFake.CallsTo("slicer") {
		if len(call.Args) >= 2 && slices.Equal(call.Args[:2], []string{"ai", "relay"}) {
			t.Error("removed project reached engine.New's contract read")
		}
	}
}

func TestStartRunRefusesAProjectReAddedUnderANewPath(t *testing.T) {
	d, store, p, _ := startRunRegistryChange(t, func(f *registry.File) {
		replacement := f.Projects[0]
		replacement.Path = filepath.Join(t.TempDir(), "replacement")
		f.Projects = append(f.Projects[1:], replacement)
	})
	_, err := d.startRun(context.Background(), RunParams{Project: p.Name, Item: "S1"})
	var ipcErr *ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != CodeNotFound || !strings.Contains(ipcErr.Message, `"`+p.Name+`"`) {
		t.Errorf("re-added project must fail with not_found naming %q: %v", p.Name, err)
	}
	// Stop any incorrectly admitted job before inspecting its side effects.
	d.closeRuns()
	if runs, err := store.List(); err != nil || len(runs) != 0 {
		t.Errorf("re-added project created run records: %v, %v", runs, err)
	}
	if len(d.runs.runs) != 0 {
		t.Errorf("re-added project created managed engines: %d", len(d.runs.runs))
	}
	for _, call := range lastFake.CallsTo("slicer") {
		if len(call.Args) >= 2 && slices.Equal(call.Args[:2], []string{"ai", "relay"}) {
			t.Error("re-added project reached engine.New's contract read")
		}
	}
}

// A projects.set that lands between planRun and admission must not start the
// run with the old fields (S161).
func TestStartRunRefusesAProjectChangedAfterPlanning(t *testing.T) {
	d, store, p, _ := startRunRegistryChange(t, func(f *registry.File) {
		f.Projects[0].WorktreePattern = ".wt/{id}"
	})
	_, err := d.startRun(context.Background(), RunParams{Project: p.Name, Item: "S1"})
	if err == nil || !strings.Contains(err.Error(), "plan changed; run the command again to confirm it") {
		t.Errorf("changed project must fail with plan changed: %v", err)
	}
	if runs, err := store.List(); err != nil || len(runs) != 0 {
		t.Errorf("changed project created run records: %v, %v", runs, err)
	}
	d.runs.mu.Lock()
	engines := len(d.runs.runs)
	d.runs.mu.Unlock()
	if engines != 0 {
		t.Errorf("changed project created managed engines: %d", engines)
	}
	// Planning again reads the new fields, so the second start is admitted.
	rec, err := d.startRun(context.Background(), RunParams{Project: p.Name, Item: "S1"})
	if err != nil || !strings.HasSuffix(rec.Worktree, filepath.Join(".wt", "S1")) {
		t.Errorf("second start: %+v, %v", rec, err)
	}
}

func TestStartRunStillAdmitsARegisteredProject(t *testing.T) {
	d, store, p, started := startRunRegistrationDaemon(t, false)
	rec, err := d.startRun(context.Background(), RunParams{Project: p.Name, Item: "S1"})
	if err != nil {
		t.Fatal(err)
	}
	if runs, err := store.List(); err != nil || len(runs) != 1 {
		t.Fatalf("registered project run records: %v, %v", runs, err)
	}
	d.runs.mu.Lock()
	managed := d.runs.runs[rec.ID]
	d.runs.mu.Unlock()
	if rec.Project != p.Name || managed == nil || managed.engine == nil {
		t.Fatalf("registered project not admitted: %+v, %v", rec, managed)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("registered project's engine did not start its role")
	}
}

func TestPlanAddsTheArchitectRowOnlyWhenRecommended(t *testing.T) {
	for _, recommended := range []bool{false, true} {
		t.Run(fmt.Sprint(recommended), func(t *testing.T) {
			roles := `{"roles":[{"role":"implementer","row":{"agent":"Claude Code","model":"sonnet","reasoning":"high"}},{"role":"reviewer","row":{"agent":"Claude Code","model":"opus","reasoning":"medium"}}`
			if recommended {
				roles += `,{"role":"architect","row":{"agent":"Codex","vendor":"OpenAI","model":"sol","reasoning":"high"},"alternatives":[{"agent":"Unknown","vendor":"Claude","model":"wrong"},{"agent":"Claude Code","vendor":"Anthropic","model":"opus","reasoning":"medium"}]}`
			} else {
				roles += `,{"role":"architect","row":null,"alternatives":[],"selected_by":"unmatched"}`
			}
			roles += `]}`
			d, store, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"relay-roles"}, Prefix: true, Stdout: roles})
			plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
			entries, present := plan.Lineup["architect"]
			if present != recommended {
				t.Fatalf("architect present %v, recommended %v: %+v", present, recommended, plan.Lineup)
			}
			if recommended {
				want := []runrec.LineupEntry{{Vendor: "codex", Model: "sol", Effort: "high"}, {Vendor: "claude", Model: "opus", Effort: "medium"}}
				if !reflect.DeepEqual(entries, want) || !plan.Permissions["architect"].ReadOnly || len(plan.Permissions["architect"].Allow) != 0 {
					t.Fatalf("architect plan: entries %+v, permission %+v", entries, plan.Permissions["architect"])
				}
			} else if _, present := plan.Permissions["architect"]; present {
				t.Fatal("absent architect has a permission entry")
			}
			if runs, err := store.List(); err != nil || len(runs) != 0 {
				t.Fatalf("planning created runs: %v %v", runs, err)
			}
		})
	}
}

func TestRunPlanRefusesAnArchitectRecommendationWithoutAnAdapter(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":{"agent":"Claude Code","model":"sonnet","reasoning":"high"}},{"role":"reviewer","row":{"agent":"Claude Code","model":"opus","reasoning":"medium"}},{"role":"architect","row":{"agent":"Unknown","vendor":"Claude","model":"wrong"}}]}`
	for _, extra := range []string{"", `,"lineup":{"architect":[]}`, `,"lineup":{"architect":[{"vendor":"codex","model":"sol","effort":"high"}]}`} {
		d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"relay-roles"}, Prefix: true, Stdout: roles})
		_, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1"`+extra+`}`))
		if err == nil || !strings.Contains(err.Error(), "no row with an adapter for architect") {
			t.Fatalf("unsupported architect row with %s: %v", extra, err)
		}
	}
}

func launchPlan(t *testing.T, d *Daemon, raw string) RunPlan {
	t.Helper()
	var plan RunPlan
	if err := json.Unmarshal(request(t, d, MethodRunPlan, raw), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestRunPlanAppliesLineupOverride(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	baseline := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"claude","model":"custom","effort":"max"},{"vendor":"claude","model":"backup","effort":"low"}]}}`)
	want := []runrec.LineupEntry{{Vendor: "claude", Model: "custom", Effort: "max"}, {Vendor: "claude", Model: "backup", Effort: "low"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], want) {
		t.Fatalf("override chain: got %+v, want %+v", plan.Lineup[engine.RoleImplementer], want)
	}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], baseline.Lineup[role]) {
			t.Errorf("%s changed: %+v", role, plan.Lineup[role])
		}
	}
}

func TestRunPlanGateFollowsReviewerOverride(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"reviewer":[{"vendor":"claude","model":"custom-review","effort":"high"},{"vendor":"claude","model":"backup-review","effort":"low"}]}}`)
	want := []runrec.LineupEntry{{Vendor: "claude", Model: "custom-review", Effort: "high"}, {Vendor: "claude", Model: "backup-review", Effort: "low"}}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], want) {
			t.Errorf("%s: got %+v, want %+v", role, plan.Lineup[role], want)
		}
	}
}

func TestRunPlanWithoutOverrideIsUnchanged(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	want := map[string][]runrec.LineupEntry{
		engine.RoleImplementer: {{Vendor: "claude", Model: "sonnet", Effort: "high"}, {Vendor: "codex", Model: "gpt", Effort: "high"}},
		engine.RoleReviewer:    {{Vendor: "claude", Model: "opus", Effort: "medium"}},
		engine.RoleGate:        {{Vendor: "claude", Model: "opus", Effort: "medium"}},
	}
	for _, extra := range []string{"", `,"lineup":null`, `,"lineup":{}`} {
		plan := launchPlan(t, d, `{"project":"p","item":"S1"`+extra+`}`)
		if !reflect.DeepEqual(plan.Lineup, want) || plan.Budgets != (registry.Budgets{}) {
			t.Fatalf("defaults changed: lineup %+v, budgets %+v", plan.Lineup, plan.Budgets)
		}
	}
}

func TestRunPlanRefusesBadOverride(t *testing.T) {
	for name, override := range map[string]string{
		"unknown role":       `"lineup":{"unknown":[{"vendor":"claude","model":"m","effort":"high"}]}`,
		"gate":               `"lineup":{"gate":[{"vendor":"claude","model":"m","effort":"high"}]}`,
		"empty chain":        `"lineup":{"implementer":[]}`,
		"null chain":         `"lineup":{"reviewer":null}`,
		"empty model":        `"lineup":{"implementer":[{"vendor":"claude","effort":"high"}]}`,
		"empty effort":       `"lineup":{"reviewer":[{"vendor":"claude","model":"m"}]}`,
		"unsupported vendor": `"lineup":{"implementer":[{"vendor":"acme","model":"m","effort":"high"}]}`,
		"bad alternative":    `"lineup":{"implementer":[{"vendor":"claude","model":"m","effort":"high"},{"vendor":"acme","model":"m","effort":"high"}]}`,
		"alternative model":  `"lineup":{"reviewer":[{"vendor":"claude","model":"m","effort":"high"},{"vendor":"claude","effort":"low"}]}`,
		"alternative effort": `"lineup":{"reviewer":[{"vendor":"claude","model":"m","effort":"high"},{"vendor":"claude","model":"m"}]}`,
		"negative minutes":   `"budgets":{"minutes":-1}`,
		"negative tokens":    `"budgets":{"tokens":-1}`,
	} {
		for _, method := range []string{MethodRunPlan, MethodRunStart} {
			t.Run(name+"/"+method, func(t *testing.T) {
				d, store, _ := runDaemon(t, nil)
				_, err := d.handle(context.Background(), method, json.RawMessage(`{"project":"p","item":"S1",`+override+`}`))
				var wireErr *ipc.Error
				if !errors.As(err, &wireErr) || wireErr.Code != "bad_params" {
					t.Errorf("want bad_params, got %v", err)
				}
				if runs, err := store.List(); err != nil || len(runs) != 0 {
					t.Errorf("invalid override created run: %v %v", runs, err)
				}
				for _, call := range lastFake.CallsTo("slicer") {
					// The read of slicer's check_command (S246) comes before the override is checked.
					if len(call.Args) >= 2 && call.Args[0] == "config" && call.Args[1] == "check_command" {
						continue
					}
					if len(call.Args) < 2 || call.Args[0] != "relay-roles" || (call.Args[1] != "recommend" && call.Args[1] != "show") {
						t.Errorf("invalid override caused slicer call: %v", call.Args)
					}
				}
				if calls := lastFake.CallsTo("git"); len(calls) != 0 {
					t.Errorf("invalid override caused git calls: %v", calls)
				}
			})
		}
	}
}

func TestRunPlanNormalisesVendorCase(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"Claude","model":"custom","effort":"max"},{"vendor":" CLAUDE ","model":"backup","effort":"low"}],"reviewer":[{"vendor":"Claude","model":"rev","effort":"high"}]}}`)
	wantImplementer := []runrec.LineupEntry{{Vendor: "claude", Model: "custom", Effort: "max"}, {Vendor: "claude", Model: "backup", Effort: "low"}}
	if !reflect.DeepEqual(plan.Lineup[engine.RoleImplementer], wantImplementer) {
		t.Fatalf("implementer: got %+v, want %+v", plan.Lineup[engine.RoleImplementer], wantImplementer)
	}
	wantReviewer := []runrec.LineupEntry{{Vendor: "claude", Model: "rev", Effort: "high"}}
	for _, role := range []string{engine.RoleReviewer, engine.RoleGate} {
		if !reflect.DeepEqual(plan.Lineup[role], wantReviewer) {
			t.Errorf("%s: got %+v, want %+v", role, plan.Lineup[role], wantReviewer)
		}
	}
}

func TestRunPlanUnsupportedVendorListsTheVendors(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	for _, method := range []string{MethodRunPlan, MethodRunStart} {
		_, err := d.handle(context.Background(), method, json.RawMessage(`{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"acme","model":"m","effort":"high"}]}}`))
		var wireErr *ipc.Error
		want := `unsupported vendor "acme" for implementer; use ` + strings.Join(Vendors(), ", ")
		if !errors.As(err, &wireErr) || wireErr.Code != "bad_params" || !strings.Contains(wireErr.Message, want) {
			t.Errorf("%s: got %v, want bad_params with %q", method, err, want)
		}
	}
}

func setLaunchBudgets(t *testing.T, d *Daemon, budgets registry.Budgets) {
	t.Helper()
	f, err := d.loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	f.Projects[0].Budgets = budgets
	if err := registry.Save(d.cfg.RegistryPath, f); err != nil {
		t.Fatal(err)
	}
}

func TestRunPlanBudgetsFallBackPerField(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want registry.Budgets
	}{
		"absent":  {``, registry.Budgets{Minutes: 60, Tokens: 10000}},
		"null":    {`,"budgets":null`, registry.Budgets{Minutes: 60, Tokens: 10000}},
		"zero":    {`,"budgets":{}`, registry.Budgets{Minutes: 60, Tokens: 10000}},
		"minutes": {`,"budgets":{"minutes":90}`, registry.Budgets{Minutes: 90, Tokens: 10000}},
		"tokens":  {`,"budgets":{"tokens":20000}`, registry.Budgets{Minutes: 60, Tokens: 20000}},
		"both":    {`,"budgets":{"minutes":90,"tokens":20000}`, registry.Budgets{Minutes: 90, Tokens: 20000}},
	} {
		t.Run(name, func(t *testing.T) {
			d, _, _ := runDaemon(t, nil)
			setLaunchBudgets(t, d, registry.Budgets{Minutes: 60, Tokens: 10000})
			plan := launchPlan(t, d, `{"project":"p","item":"S1"`+tc.raw+`}`)
			if plan.Budgets != tc.want {
				t.Fatalf("budgets: got %+v, want %+v", plan.Budgets, tc.want)
			}
		})
	}
}

func TestRunStartRecordsOverridesInRunJSON(t *testing.T) {
	d, store, _ := runDaemon(t, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	setLaunchBudgets(t, d, registry.Budgets{Minutes: 60, Tokens: 10000})
	raw := `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"claude","model":"custom","effort":"max"},{"vendor":"claude","model":"backup","effort":"low"}],"reviewer":[{"vendor":"claude","model":"custom-review","effort":"high"}]},"budgets":{"minutes":90}}`
	plan := launchPlan(t, d, raw)
	var params map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		t.Fatal(err)
	}
	params["expected"], _ = json.Marshal(plan)
	confirmed, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var rec runrec.Record
	if err := json.Unmarshal(request(t, d, MethodRunStart, string(confirmed)), &rec); err != nil {
		t.Fatal(err)
	}
	run, err := store.Open(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []runrec.Record{rec, run.Snapshot()} {
		if !reflect.DeepEqual(got.Lineup, plan.Lineup) || got.Lineup[engine.RoleImplementer][0].Model != "custom" || got.Lineup[engine.RoleReviewer][0].Model != "custom-review" || got.Budgets != (registry.Budgets{Minutes: 90, Tokens: 10000}) {
			t.Fatalf("launch values not recorded: lineup %+v, budgets %+v", got.Lineup, got.Budgets)
		}
	}
}

func TestRunStartExpectedCoversOverrides(t *testing.T) {
	for name, changed := range map[string]string{
		"lineup":      `"lineup":{"implementer":[{"vendor":"claude","model":"changed","effort":"high"},{"vendor":"claude","model":"backup","effort":"low"}]}`,
		"alternative": `"lineup":{"implementer":[{"vendor":"claude","model":"custom","effort":"high"},{"vendor":"claude","model":"changed","effort":"low"}]}`,
		"budgets":     `"budgets":{"tokens":20000}`,
	} {
		t.Run(name, func(t *testing.T) {
			d, store, _ := runDaemon(t, nil)
			plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"claude","model":"custom","effort":"high"},{"vendor":"claude","model":"backup","effort":"low"}]}}`)
			shown, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			// Keep the original chain when only the budgets change.
			lineup := ""
			if name == "budgets" {
				lineup = `,"lineup":{"implementer":[{"vendor":"claude","model":"custom","effort":"high"},{"vendor":"claude","model":"backup","effort":"low"}]}`
			}
			_, err = d.handle(context.Background(), MethodRunStart, json.RawMessage(`{"project":"p","item":"S1","expected":`+string(shown)+`,`+changed+lineup+`}`))
			if err == nil || !strings.Contains(err.Error(), "plan changed") {
				t.Errorf("changed override accepted: %v", err)
			}
			if runs, err := store.List(); err != nil || len(runs) != 0 {
				t.Errorf("changed plan created run: %v %v", runs, err)
			}
		})
	}
}

func TestRunOverrideWritesNoMatrix(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	before, err := os.ReadFile(d.cfg.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	baseline := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	request(t, d, MethodRunStart, `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"claude","model":"custom","effort":"high"}]},"budgets":{"minutes":90}}`)
	d.closeRuns()
	for _, call := range lastFake.CallsTo("slicer") {
		if call.Args[0] == "relay-roles" && (len(call.Args) < 2 || (call.Args[1] != "recommend" && call.Args[1] != "show")) {
			t.Errorf("matrix write: %v", call.Args)
		}
	}
	after, err := os.ReadFile(d.cfg.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("launch changed the registry")
	}
	if next := launchPlan(t, d, `{"project":"p","item":"S1"}`); !reflect.DeepEqual(next, baseline) {
		t.Fatalf("launch changed defaults: got %+v, want %+v", next, baseline)
	}
}
func TestRunStartAnswerApproveThroughDaemon(t *testing.T) {
	runner := roundRunner(func(_ context.Context, r engine.Request) (engine.Result, error) {
		report := "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"
		if r.Role == engine.RoleImplementer && !strings.Contains(r.Brief, "## Finish step") && !strings.Contains(r.Brief, "use stdlib") {
			report = "Tests: 1 passed, 0 failed\n\n## Owner needed\nWhich library?\n"
		}
		if r.Role == engine.RoleReviewer {
			report = "(A): nothing\n(B): nothing\n(D): nothing\n\n## Owner needed\nnone\n"
		}
		if r.Role == engine.RoleGate {
			report = "VERDICT: PASS\n" + report
		}
		return engine.Result{Exit: engine.ExitOK, Session: "session", Report: report}, nil
	})
	d, store, _ := runDaemon(t, runner)
	b := request(t, d, "run.start", `{"project":"p","item":"S1","well_specified":true,"split":false}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, rec.ID, runrec.StatePaused)
	waitIdle(t, d, rec.ID)
	request(t, d, "run.answer", fmt.Sprintf(`{"run":%q,"text":"use stdlib"}`, rec.ID))
	waitState(t, store, rec.ID, runrec.StateAwaitingApproval)
	waitIdle(t, d, rec.ID)
	b = request(t, d, "run.approve", fmt.Sprintf(`{"run":%q}`, rec.ID))
	if !strings.Contains(string(b), `"state":"committed"`) || !strings.Contains(string(b), `"sha":"abc123"`) {
		t.Fatalf("approval did not commit: %s", b)
	}
}
func TestDaemonStopCancelsAnActiveRole(t *testing.T) {
	started := make(chan struct{})
	killed := make(chan struct{})
	d, store, _ := runDaemon(t, func(ctx context.Context, r engine.Request) (engine.Result, error) {
		close(started)
		<-ctx.Done()
		close(killed)
		return engine.Result{}, ctx.Err()
	})
	b := request(t, d, "run.start", `{"project":"p","item":"S1"}`)
	var rec runrec.Record
	json.Unmarshal(b, &rec)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("role did not start")
	}
	b = request(t, d, "run.stop", fmt.Sprintf(`{"run":%q}`, rec.ID))
	select {
	case <-killed:
	default:
		t.Fatal("stop returned before role was killed")
	}
	if !strings.Contains(string(b), `"state":"stopped"`) {
		t.Fatalf("stop: %s", b)
	}
	stopped := waitState(t, store, rec.ID, runrec.StateStopped)
	for _, role := range stopped.Roles {
		if role.Running() {
			t.Fatalf("stopped role is still running: %+v", role)
		}
	}
}

// holdProc keeps a surviving process group alive until the test frees it, so
// the stop job stays in flight.
type holdProc struct {
	killing chan struct{}
	release chan struct{}
}

func (p holdProc) Alive(int) bool { return true }
func (p holdProc) Kill(int) error {
	close(p.killing)
	<-p.release
	return nil
}

// stopOutcome is what one owner stop returned.
type stopOutcome struct {
	rec runrec.Record
	err error
}

// stopInBackground sends an owner stop without waiting for it.
func stopInBackground(d *Daemon, id string) <-chan stopOutcome {
	out := make(chan stopOutcome, 1)
	go func() {
		rec, err := d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: id})
		out <- stopOutcome{rec, err}
	}()
	return out
}

func awaitStop(t *testing.T, out <-chan stopOutcome) stopOutcome {
	t.Helper()
	select {
	case o := <-out:
		return o
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
	return stopOutcome{}
}

func TestSecondStopJoinsTheInFlightStop(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	hold := holdProc{killing: make(chan struct{}), release: make(chan struct{})}
	free := sync.OnceFunc(func() { close(hold.release) })
	t.Cleanup(free)
	d.runs.deps.Proc = hold
	run, err := store.Create(runrec.Record{ID: "busy", Project: p.Name, State: runrec.StateImplement, RelayRun: "relay-1",
		Roles: []runrec.RoleRecord{{Role: "implementer", PGID: 4242, StartedAt: time.Now()}}})
	if err != nil {
		t.Fatal(err)
	}
	r := &managedRun{engine: engine.Load(d.runDeps(p), p, run)}
	d.runs.mu.Lock()
	d.runs.runs["busy"] = r
	// The role blocks until the first stop cancels it. Its process group
	// survives, so the stop job has to kill it.
	d.launchRun(r, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	d.runs.mu.Unlock()
	first := stopInBackground(d, "busy")
	select {
	case <-hold.killing:
	case <-time.After(5 * time.Second):
		t.Fatal("stop job did not reach the kill")
	}
	second := stopInBackground(d, "busy")
	time.Sleep(100 * time.Millisecond) // the second stop must arrive before the kill returns
	free()
	a, b := awaitStop(t, first), awaitStop(t, second)
	if a.err != nil {
		t.Fatalf("first stop: %v", a.err)
	}
	if b.err != nil {
		t.Fatalf("second stop: %v, want the first stop's record", b.err)
	}
	if !reflect.DeepEqual(a.rec, b.rec) || a.rec.State != runrec.StateStopped {
		t.Fatalf("second stop returned %+v, want the first stop's %+v", b.rec, a.rec)
	}
	// A cancelled job context fails the relay end write, and the record keeps the failure.
	ended := false
	for _, w := range a.rec.Writes {
		if len(w.Argv) > 1 && w.Argv[0] == "relay-run" && w.Argv[1] == "end" {
			ended = true
			if w.Error != "" {
				t.Fatalf("stop job context was cancelled: %s", w.Error)
			}
		}
	}
	if !ended {
		t.Fatalf("stop job did not end the relay: %+v", a.rec.Writes)
	}
}
func TestSecondStopJoinsAStopStillWaiting(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	run, err := store.Create(runrec.Record{ID: "waiting", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	r := &managedRun{engine: engine.Load(d.runDeps(p), p, run)}
	release := make(chan struct{})
	free := sync.OnceFunc(func() { close(release) })
	t.Cleanup(free)
	d.runs.mu.Lock()
	d.runs.runs["waiting"] = r
	// The role blocks past its cancel, so the first stop waits for it.
	d.launchRun(r, func(ctx context.Context) error { <-ctx.Done(); <-release; return ctx.Err() })
	d.runs.mu.Unlock()
	first := stopInBackground(d, "waiting")
	deadline := time.Now().Add(5 * time.Second)
	for {
		d.runs.mu.Lock()
		waiting := r.stopping
		d.runs.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first stop did not wait for its role")
		}
		time.Sleep(time.Millisecond)
	}
	second := stopInBackground(d, "waiting")
	time.Sleep(100 * time.Millisecond) // the second stop must arrive before the role is freed
	free()
	a, b := awaitStop(t, first), awaitStop(t, second)
	if a.err != nil {
		t.Fatalf("first stop: %v", a.err)
	}
	if b.err != nil {
		t.Fatalf("second stop: %v, want the first stop's record", b.err)
	}
	if !reflect.DeepEqual(a.rec, b.rec) || a.rec.State != runrec.StateStopped {
		t.Fatalf("second stop returned %+v, want the first stop's %+v", b.rec, a.rec)
	}
}
func TestStopOfAFinishedRunNamesItsState(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	run, err := store.Create(runrec.Record{ID: "finished", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	d.runs.mu.Lock()
	d.runs.runs["finished"] = &managedRun{engine: engine.Load(d.runDeps(p), p, run)}
	d.runs.mu.Unlock()
	first, err := d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: "finished"})
	if err != nil || first.State != runrec.StateStopped {
		t.Fatalf("first stop: %+v, %v", first, err)
	}
	// The run has finished, so a later stop is an error and does not join the old stop.
	_, err = d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: "finished"})
	if err == nil || !strings.Contains(err.Error(), "already") || !strings.Contains(err.Error(), runrec.StateStopped) {
		t.Fatalf("stop after the run finished: %v, want an error naming %s", err, runrec.StateStopped)
	}
}
func TestFailedStopHandleDoesNotJoinALaterJob(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	run, err := store.Create(runrec.Record{ID: "failed", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	r := &managedRun{engine: engine.Load(d.runDeps(p), p, run)}
	cancelled := make(chan struct{})
	d.runs.mu.Lock()
	d.runs.runs["failed"] = r
	// An earlier stop failed and left its handle behind. The run is not final.
	failed := &stopCall{started: make(chan struct{})}
	failed.abandon(errors.New("reconcile failed"))
	r.stop = failed
	// A later job starts as a queued run does. It blocks until a stop cancels it.
	d.launchRun(r, func(ctx context.Context) error { <-ctx.Done(); close(cancelled); return ctx.Err() })
	d.runs.mu.Unlock()
	o := awaitStop(t, stopInBackground(d, "failed"))
	if o.err != nil {
		t.Fatalf("stop joined the failed stop: %v", o.err)
	}
	if o.rec.State != runrec.StateStopped {
		t.Fatalf("stop returned %+v, want a fresh stopped record", o.rec)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("stop returned without cancelling the later job")
	}
}
func TestStatusIncludesEngineWaitsAndNeedsYou(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	for _, s := range []string{runrec.StateAwaitingApproval, runrec.StatePaused, runrec.StateStopped, runrec.StateCommitted} {
		_, err := store.Create(runrec.Record{ID: s, Item: s, Project: p.Name, State: s, Reason: "owner action"})
		if err != nil {
			t.Fatal(err)
		}
	}
	b := request(t, d, "status", `{"project":"p"}`)
	if !strings.Contains(string(b), `"needs_you":3`) || !strings.Contains(string(b), `"state":"awaiting-approval"`) || !strings.Contains(string(b), `"wait":"owner action"`) {
		t.Fatalf("status: %s", b)
	}
}

// A landing whose re-check was red had a fix round; the row carries the count
// so `ork status` can say the next approval is a repeat (S238).
func TestEngineRunRowCarriesLandFixes(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	for id, fixes := range map[string]int{"again": 1, "first": 0} {
		if _, err := store.Create(runrec.Record{ID: id, Item: id, Project: p.Name, State: runrec.StateAwaitingApproval, LandFixes: fixes}); err != nil {
			t.Fatal(err)
		}
	}
	b := request(t, d, "status", `{"project":"p"}`)
	var reply struct {
		Projects []struct {
			EngineRuns []json.RawMessage `json:"engine_runs"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(b, &reply); err != nil || len(reply.Projects) != 1 {
		t.Fatalf("status: %s (%v)", b, err)
	}
	seen := 0
	for _, raw := range reply.Projects[0].EngineRuns {
		var row source.EngineRun
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatal(err)
		}
		want := map[string]int{"again": 1, "first": 0}[row.ID]
		if row.LandFixes != want {
			t.Errorf("%s: LandFixes %d, want %d", row.ID, row.LandFixes, want)
		}
		if has := strings.Contains(string(raw), "land_fixes"); has != (want > 0) {
			t.Errorf("%s: land_fixes in the JSON = %v: %s", row.ID, has, raw)
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("%d rows, want 2: %s", seen, b)
	}
}

// The row names the command of the newest check row, so a status says whether
// the round command or the full command ran last (S285, *proposed*).
func TestEngineRunRowCarriesTheNewestCheck(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	checks := []runrec.CheckRecord{
		{Round: 1, Command: "fast", OK: true, Log: "checks-1.log"},
		{Round: 1, Command: "full", OK: false, Log: "checks-full-1.log"},
	}
	if _, err := store.Create(runrec.Record{ID: "with", Item: "S1", Project: p.Name, State: runrec.StateImplement, Round: 2, Checks: checks}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(runrec.Record{ID: "without", Item: "S2", Project: p.Name, State: runrec.StateImplement}); err != nil {
		t.Fatal(err)
	}
	b := request(t, d, "status", `{"project":"p"}`)
	var reply struct {
		Projects []struct {
			EngineRuns []source.EngineRun `json:"engine_runs"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(b, &reply); err != nil || len(reply.Projects) != 1 {
		t.Fatalf("status: %s (%v)", b, err)
	}
	seen := 0
	for _, row := range reply.Projects[0].EngineRuns {
		switch row.ID {
		case "with":
			want := &source.EngineCheck{Round: 1, Command: "full", OK: false, Log: "checks-full-1.log"}
			if !reflect.DeepEqual(row.LastCheck, want) {
				t.Errorf("last check %+v, want %+v", row.LastCheck, want)
			}
		case "without":
			if row.LastCheck != nil {
				t.Errorf("a run with no check has last check %+v", row.LastCheck)
			}
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("%d rows, want 2: %s", seen, b)
	}
}

func TestActiveRunInhibitsDaemonIdleExit(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "active", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{}
	exited := false
	idle := &idleTimer{clock: clock, timeout: func() time.Duration { return time.Minute }, waveRunning: d.activeRuns, onIdle: func() { exited = true }}
	idle.start()
	defer idle.stop()
	clock.Advance(time.Minute)
	if exited {
		t.Fatal("daemon idle-exited during active run")
	}
}

func TestRecoveredRunCanBeStoppedAndActiveRunIsPaused(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "interrupted", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := waitState(t, store, "interrupted", runrec.StatePaused)
	if rec.Pending == nil || rec.Pending.Kind != runrec.PendingInterrupted {
		t.Fatalf("recovery: %+v", rec)
	}
	b := request(t, d, "run.stop", `{"run":"interrupted"}`)
	if !strings.Contains(string(b), `"state":"stopped"`) {
		t.Fatalf("recovered stop: %s", b)
	}
}

// Seed through JSON to exercise the durable marker across daemon instances.
func rebuildResumeRecord(t *testing.T, store *runrec.Store, p registry.Project, marked bool) {
	t.Helper()
	raw := fmt.Sprintf(`{"id":"rebuild","project":%q,"item":"S1","worktree":%q,"state":"paused","round":1,"rebuild_resume":%t,"pending":{"kind":"interrupted","state":%q,"role":"implementer","round":1},"lineup":{"implementer":[{"vendor":"claude","model":"sonnet"}],"reviewer":[{"vendor":"claude","model":"sonnet"}]}}`, p.Name, filepath.Join(p.Path, ".worktrees", "S1"), marked, runrec.StateImplement)
	var rec runrec.Record
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(rec); err != nil {
		t.Fatal(err)
	}
}

func rebuildMarker(t *testing.T, store *runrec.Store, id string) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(store.RunDir(id), "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		RebuildResume bool `json:"rebuild_resume"`
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	return rec.RebuildResume
}

func recoverRebuildDaemon(t *testing.T, d *Daemon) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := d.recoverRuns(ctx); err != nil {
		t.Fatal(err)
	}
}

func rebuildResumeRunner(started chan<- engine.Request) roundRunner {
	return func(ctx context.Context, req engine.Request) (engine.Result, error) {
		started <- req
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	}
}

func TestRebuildHeldRunResumesAtStart(t *testing.T) {
	started := make(chan engine.Request, 1)
	d, store, p := runDaemon(t, rebuildResumeRunner(started))
	rebuildResumeRecord(t, store, p, true)
	recoverRebuildDaemon(t, d)
	select {
	case req := <-started:
		r, _ := store.Open("rebuild")
		if req.Role != engine.RoleImplementer || r.Snapshot().State != runrec.StateImplement {
			t.Fatalf("resumed role/state = %s/%s, want implementer/implement", req.Role, r.Snapshot().State)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("marked run did not resume its implementer at startup")
	}
	if rebuildMarker(t, store, "rebuild") {
		t.Fatal("successful resume kept the rebuild marker")
	}
}

func TestUnmarkedInterruptedRunStillWaits(t *testing.T) {
	started := make(chan engine.Request, 1)
	d, store, p := runDaemon(t, rebuildResumeRunner(started))
	rebuildResumeRecord(t, store, p, false)
	recoverRebuildDaemon(t, d)
	r, _ := store.Open("rebuild")
	rec := r.Snapshot()
	if rec.State != runrec.StatePaused || rec.Pending == nil || rec.Pending.Kind != runrec.PendingInterrupted || len(rec.Roles) != 0 || rec.Resume != nil {
		t.Fatalf("unmarked interrupted run resumed: %+v", rec)
	}
}

func TestRebuildMarkerRetriedOnlyOnce(t *testing.T) {
	d, store, p := runDaemon(t, retryRunner)
	d.runs.deps.Check = func(context.Context, string, string, time.Duration) (engine.CheckOutput, error) {
		return engine.CheckOutput{}, context.DeadlineExceeded
	}
	rebuildResumeRecord(t, store, p, true)
	recoverRebuildDaemon(t, d)
	waitIdle(t, d, "rebuild")
	r, _ := store.Open("rebuild")
	first := r.Snapshot()
	if len(first.Roles) != 1 || first.State != runrec.StatePaused || first.Pending == nil || first.Pending.Kind != runrec.PendingError {
		t.Fatalf("marked run did not attempt and fail its retry: %+v", first)
	}
	if rebuildMarker(t, store, "rebuild") {
		t.Error("failed retry kept the rebuild marker")
	}
	d.closeRuns()
	deps := d.runs.deps
	deps.Hold, deps.Wake, deps.Vendors = nil, nil, nil
	d2 := New(Config{Paths: d.cfg.Paths, RegistryPath: d.cfg.RegistryPath, Source: &stubSource{}, Engine: &deps})
	t.Cleanup(d2.cache.Close)
	t.Cleanup(d2.closeRuns)
	recoverRebuildDaemon(t, d2)
	waitIdle(t, d2, "rebuild")
	r, err := store.Open("rebuild")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot(); !reflect.DeepEqual(got, first) {
		t.Fatalf("failed retry ran again at next startup: %+v", got)
	}
}

func TestRebuildExitMarksOnlyHeldRuns(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	pausedRun(t, d, store, p, "held")
	for _, state := range []string{runrec.StatePaused, runrec.StateQueued, runrec.StateAwaitingApproval} {
		pausedRun(t, d, store, p, state)
		r, _ := store.Open(state)
		if err := r.Update(func(rec *runrec.Record) { rec.State = state }); err != nil {
			t.Fatal(err)
		}
	}
	d.runs.deps.Hold.Set(true)
	request(t, d, MethodRunRetry, `{"run":"held","no_wait":true}`)
	waitFor(t, "held role start", func() bool { return engineRunWait(d, "held") == engine.HoldReason })
	if !d.exitForRebuild(func(context.Context) error { return nil }) {
		t.Fatal("rebuild exit refused held runs")
	}
	if !rebuildMarker(t, store, "held") {
		t.Error("held run has no durable rebuild marker")
	}
	for _, state := range []string{runrec.StatePaused, runrec.StateQueued, runrec.StateAwaitingApproval} {
		if rebuildMarker(t, store, state) {
			t.Errorf("resting run %s marked for rebuild", state)
		}
	}
}

func TestRebuildMarkerWriteFailureStillExits(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	pausedRun(t, d, store, p, "held")
	d.runs.deps.Hold.Set(true)
	request(t, d, MethodRunRetry, `{"run":"held","no_wait":true}`)
	waitFor(t, "held role start", func() bool { return engineRunWait(d, "held") == engine.HoldReason })

	// The engine retains its open record. Replacing its directory with a file
	// makes Update fail without depending on permission bits or privileges.
	dir := store.RunDir("held")
	saved := filepath.Join(t.TempDir(), "held")
	if err := os.Rename(dir, saved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
		if err := os.Rename(saved, dir); err != nil {
			t.Error(err)
		}
	})
	if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	exited := captureExit(d)
	didExit := d.exitForRebuild(func(context.Context) error { return nil })
	signalled := false
	select {
	case <-exited:
		signalled = true
	default:
	}
	if !didExit || !signalled {
		t.Fatalf("marker write failure blocked rebuild exit: returned %t, stop signalled %t", didExit, signalled)
	}
}

func TestTwoOpenTUIsShareCLIReads(t *testing.T) {
	e := newEnv(t)
	e.writeRegistry(t, 0, registry.Project{Name: "alpha", Path: "/p/alpha", Adj: true})
	fake := clitest.Install(t,
		// Items, goals and runs are read with one batched snapshot call (S335).
		clitest.Route{Tool: "slicer", Args: []string{"snapshot"}, Prefix: true,
			Stdout: `{"snapshot":2,"projects":[{"root":"/p/alpha","revision":"r","parts":{` +
				`"items":{"revision":"i","fingerprint":null,"value":[{"id":"S1","title":"shared"}]},` +
				`"goals":{"revision":"g","fingerprint":null,"value":{}},` +
				`"runs":{"revision":"u","fingerprint":null,"value":{"runs":[]}}},"error":null}]}`},
		clitest.Route{Tool: "slicer", Args: []string{"relay-run"}, Prefix: true, Stdout: `{"runs":[]}`},
		clitest.Route{Tool: "adj", Prefix: true, Stdout: `{"records":[]}`},
		clitest.Route{Tool: "git", Prefix: true, Stdout: "head\n"},
	)
	direct := source.NewDirect(slicercli.Client{Bin: fake.Bin("slicer")}, adjcli.Client{Bin: fake.Bin("adj")})
	direct.Git = fake.Bin("git")
	e.fetch = direct
	e.start(t)
	ctx := context.Background()
	f1, f2 := NewFeed(e.paths.Socket), NewFeed(e.paths.Socket)
	defer f1.Close()
	defer f2.Close()
	if err := f1.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	c1, c2 := e.dial(t), e.dial(t)
	s1 := c1.Snapshot(ctx, registry.Project{Name: "alpha"})
	if err := f2.Focus(ctx, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	s2 := c2.Snapshot(ctx, registry.Project{Name: "alpha"})
	if len(s1.Items.Value) != 1 || len(s2.Items.Value) != 1 || s1.Items.Value[0].ID != "S1" || s2.Items.Value[0].ID != "S1" {
		t.Fatalf("TUI snapshots: %+v %+v", s1.Items, s2.Items)
	}
	settle()
	board := 0
	for _, call := range fake.CallsTo("slicer") {
		if call.Args[0] == "relay-run" && !slices.Contains(call.Args, "--active") {
			board++
		}
	}
	if board != 2 {
		t.Fatalf("board reads %d, want one per snapshot", board)
	}
	for verb, want := range map[string]int{"snapshot": 1, "list": 0, "goals": 0} {
		n := 0
		for _, call := range fake.CallsTo("slicer") {
			if call.Args[0] == verb {
				n++
			}
		}
		if n != want {
			t.Fatalf("two open TUIs: %s invoked %d times, want %d (one shared batched read)", verb, n, want)
		}
	}
	for _, call := range fake.CallsTo("slicer") {
		if call.Args[0] == "relay-run" && slices.Contains(call.Args, "--active") {
			t.Fatalf("the active runs were read apart from the batch: %v", call.Args)
		}
	}
}

func TestChangedConfirmationPlanCannotStart(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	_, err := d.handle(context.Background(), "run.start", json.RawMessage(`{"project":"p","item":"S1","expected":{"item":"S1","branch":"stale"}}`))
	if err == nil || !strings.Contains(err.Error(), "plan changed") {
		t.Fatalf("changed plan: %v", err)
	}
}

func TestRecoveryIgnoresFinishedRunsFromRemovedProjects(t *testing.T) {
	d, store, _ := runDaemon(t, nil)
	for _, state := range []string{runrec.StateCommitted, runrec.StateStopped} {
		if _, err := store.Create(runrec.Record{ID: state, Project: "removed", State: state}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatalf("finished history blocks daemon startup: %v", err)
	}
}

func TestDaemonActiveRunHoldsOffIdle(t *testing.T) {
	started := make(chan struct{})
	d, _, _ := runDaemon(t, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
		close(started)
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	e := newEnv(t)
	d.cfg.Paths = e.paths
	d.cfg.Clock = e.clock
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	select {
	case <-d.Ready():
	case err := <-done:
		done <- err
		t.Fatalf("daemon startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not become ready")
	}
	b := request(t, d, "run.start", `{"project":"p","item":"S1"}`)
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("role did not start")
	}
	e.clock.Advance(10 * time.Minute)
	select {
	case err := <-done:
		done <- err
		t.Fatalf("daemon idle-exited during active run: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	request(t, d, "run.stop", fmt.Sprintf(`{"run":%q}`, rec.ID))
	e.clock.Advance(10 * time.Minute)
	select {
	case err := <-done:
		done <- err
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("idle daemon did not exit after stopping its run")
	}
}

func TestBusyRunHoldsOffIdle(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	d.runs.runs["busy"] = &managedRun{busy: true}
	clock := &fakeClock{}
	exited := false
	idle := &idleTimer{clock: clock, timeout: func() time.Duration { return time.Minute }, waveRunning: d.activeRuns, onIdle: func() { exited = true }}
	idle.start()
	defer idle.stop()
	clock.Advance(time.Minute)
	if exited {
		t.Fatal("daemon idle-exited during busy job")
	}
}

type failedRecoveryProc struct{}

func (failedRecoveryProc) Alive(int) bool { return true }
func (failedRecoveryProc) Kill(int) error { return errors.New("kill refused") }

func captureRunErrors(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old; r.Close(); w.Close() }()
	fn()
	w.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRecoverySkipsBadRuns(t *testing.T) {
	for _, kind := range []string{"removed-project", "kill", "list"} {
		t.Run(kind, func(t *testing.T) {
			d, store, p := runDaemon(t, nil)
			bad := runrec.Record{ID: "bad", Project: p.Name, State: runrec.StateImplement}
			want := ""
			switch kind {
			case "removed-project":
				bad.Project = "removed"
				want = "not registered"
			case "kill":
				d.runs.deps.Proc = failedRecoveryProc{}
				bad.Roles = []runrec.RoleRecord{{Role: engine.RoleImplementer, PGID: 987654, StartedAt: time.Now()}}
				want = "kill refused"
			case "list":
				want = "run record"
			}
			if _, err := store.Create(bad); err != nil {
				t.Fatal(err)
			}
			if kind == "list" {
				if err := os.WriteFile(filepath.Join(store.RunDir("bad"), "run.json"), []byte("broken"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.Create(runrec.Record{ID: "good", Project: p.Name, State: runrec.StateImplement}); err != nil {
				t.Fatal(err)
			}
			log := captureRunErrors(t, func() {
				if err := d.recoverRuns(context.Background()); err != nil {
					t.Errorf("bad run blocks startup: %v", err)
				}
			})
			if !strings.Contains(log, want) {
				t.Errorf("recovery stderr %q lacks %q", log, want)
			}
			if kind != "list" {
				rows := d.engineRuns(bad.Project)
				var row EngineRun
				for _, candidate := range rows {
					if candidate.ID == bad.ID {
						row = candidate
					}
				}
				if got := wireRunError(t, row); !strings.Contains(got, want) {
					t.Errorf("recovery row error = %q, want %q", got, want)
				}
				rec, err := d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: bad.ID})
				if err == nil || !strings.Contains(err.Error(), want) || rec.ID != bad.ID || rec.State != bad.State {
					t.Errorf("failed recovery stop must return run and error: %+v, %v", rec, err)
				}
			}
			// Exercise the recovered run through the owner surface.
			b, err := d.handle(context.Background(), MethodRunStop, json.RawMessage(`{"run":"good"}`))
			if err != nil {
				t.Fatalf("healthy recovered run unavailable: %v", err)
			}
			if b.(runrec.Record).State != runrec.StateStopped {
				t.Fatalf("healthy stop: %+v", b)
			}
		})
	}
}

func TestStatusSurvivesRunListError(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	for _, id := range []string{"bad", "good"} {
		if _, err := store.Create(runrec.Record{ID: id, Project: p.Name, State: runrec.StateAwaitingApproval}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(store.RunDir("bad"), "run.json"), []byte("broken"), 0644); err != nil {
		t.Fatal(err)
	}
	log := captureRunErrors(t, func() {
		b, err := d.handle(context.Background(), "status", json.RawMessage(`{"project":"p"}`))
		if err != nil {
			t.Fatalf("run list error blocks status: %v", err)
		}
		raw, _ := json.Marshal(b)
		if !strings.Contains(string(raw), `"id":"good"`) {
			t.Errorf("healthy run missing from status: %s", raw)
		}
	})
	if !strings.Contains(log, "run record") {
		t.Errorf("status stderr missing list error: %q", log)
	}
}

func TestClosedRunManagerRefusesJobs(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	r, err := store.Create(runrec.Record{ID: "waiting", Project: p.Name, State: runrec.StateAwaitingApproval})
	if err != nil {
		t.Fatal(err)
	}
	d.runs.runs["waiting"] = &managedRun{engine: engine.Load(d.runDeps(p), p, r)}
	cancelled := make(chan struct{})
	release := make(chan struct{})
	d.runs.mu.Lock()
	busy := &managedRun{engine: d.runs.runs["waiting"].engine}
	d.runs.runs["busy"] = busy
	d.launchRun(busy, func(ctx context.Context) error {
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	})
	d.runs.mu.Unlock()
	closed := make(chan struct{})
	go func() { d.closeRuns(); close(closed) }()
	defer func() { close(release); <-closed }()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("close did not cancel active job")
	}
	if _, err := d.startRun(context.Background(), RunParams{Project: "p", Item: "S1"}); err == nil {
		t.Error("start accepted after close")
	}
	if _, err := d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: "waiting"}); err == nil {
		t.Error("owner job accepted after close")
	}
}

func TestCloseRefusesAStopWaitingForItsRole(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	run, err := store.Create(runrec.Record{ID: "waiting", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	r := &managedRun{engine: engine.Load(d.runDeps(p), p, run)}
	cancelled, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRole := func() { releaseOnce.Do(func() { close(release) }) }
	d.runs.mu.Lock()
	d.runs.runs["waiting"] = r
	d.launchRun(r, func(ctx context.Context) error {
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	})
	d.runs.mu.Unlock()
	result := make(chan error, 1)
	go func() {
		_, err := d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: "waiting"})
		result <- err
	}()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		releaseRole()
		t.Fatal("stop did not cancel role")
	}
	closed := make(chan struct{})
	go func() { d.closeRuns(); close(closed) }()
	defer func() { releaseRole(); <-closed }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		d.runs.mu.Lock()
		closing := d.runs.closed
		d.runs.mu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("close did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	// Release the old job after shutdown has closed admission.
	releaseRole()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("waiting stop launched a new job during close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting stop did not return")
	}
}

// waveRig holds roles open until the test frees them one at a time.
type waveRig struct {
	d       *Daemon
	store   *runrec.Store
	p       registry.Project
	started chan string // the worktree of each role that began
	free    chan struct{}
	fake    *clitest.Fake
	briefs  sync.Map // the latest brief of each role, by worktree name
}

func newWaveRig(t *testing.T, global, projectCap int) *waveRig {
	t.Helper()
	return newWaveRigRoutes(t, global, projectCap)
}

// newWaveRigRoutes is newWaveRig with fake routes of its own, which go before
// runDaemon's.
func newWaveRigRoutes(t *testing.T, global, projectCap int, routes ...clitest.Route) *waveRig {
	t.Helper()
	rig := &waveRig{started: make(chan string, 32), free: make(chan struct{}, 32)}
	d, store, p := runDaemon(t, func(ctx context.Context, r engine.Request) (engine.Result, error) {
		rig.briefs.Store(filepath.Base(r.Worktree), r.Brief)
		rig.started <- filepath.Base(r.Worktree)
		select {
		case <-rig.free:
			// Free the slot with a terminal exit, even with fallback rows.
			return engine.Result{Exit: engine.ExitAuth}, nil
		case <-ctx.Done():
			return engine.Result{}, ctx.Err()
		}
	}, routes...)
	p.Cap = projectCap
	if err := registry.Save(d.cfg.RegistryPath, &registry.File{Version: registry.Version, GlobalCap: global, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	rig.d, rig.store, rig.p, rig.fake = d, store, p, lastFake
	return rig
}

func (w *waveRig) start(t *testing.T, item string) runrec.Record {
	t.Helper()
	b := request(t, w.d, "run.start", fmt.Sprintf(`{"project":"p","item":%q}`, item))
	var rec runrec.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func (w *waveRig) expectStart(t *testing.T, item string) {
	t.Helper()
	select {
	case got := <-w.started:
		if got != item {
			t.Fatalf("role started for %s, want %s", got, item)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no role started for %s", item)
	}
}

// expectStarts waits for one role start per item, in any order.
func (w *waveRig) expectStarts(t *testing.T, items ...string) {
	t.Helper()
	want := map[string]bool{}
	for _, item := range items {
		want[item] = true
	}
	for range items {
		select {
		case got := <-w.started:
			if !want[got] {
				t.Fatalf("role started for %s, want one of %v", got, items)
			}
			delete(want, got)
		case <-time.After(5 * time.Second):
			t.Fatalf("roles did not start for %v", want)
		}
	}
}

func (w *waveRig) expectNoStart(t *testing.T) {
	t.Helper()
	select {
	case got := <-w.started:
		t.Fatalf("role started for %s over the cap", got)
	case <-time.After(150 * time.Millisecond):
	}
}

func (w *waveRig) startWithLineup(t *testing.T, id string, entries []runrec.LineupEntry) runrec.Record {
	t.Helper()
	p := RunParams{Project: "p", Item: id, Lineup: map[string][]runrec.LineupEntry{engine.RoleImplementer: entries}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var rec runrec.Record
	if err := json.Unmarshal(request(t, w.d, MethodRunStart, string(b)), &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func (w *waveRig) startLocal(t *testing.T, id string) runrec.Record {
	t.Helper()
	return w.startWithLineup(t, id, []runrec.LineupEntry{{Vendor: "claude-local", Model: "qwen3.8-27b", Effort: "medium"}})
}

func TestLocalRunBypassesFullFrontierCaps(t *testing.T) {
	for _, tc := range []struct {
		name            string
		global, project int
	}{{"global", 1, 0}, {"project", 10, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWaveRig(t, tc.global, tc.project)
			localLaunchers(t)
			w.start(t, "S1")
			w.expectStart(t, "S1")
			rec := w.startLocal(t, "S2")
			if rec.State == runrec.StateQueued {
				t.Fatalf("local run queued behind full %s cap", tc.name)
			}
			w.expectStart(t, "S2")
		})
	}
}

func TestFrontierRunBypassesFourLocalReservations(t *testing.T) {
	w := newWaveRig(t, 10, 10)
	localLaunchers(t)
	for i := 1; i <= 4; i++ {
		w.startLocal(t, fmt.Sprintf("S%d", i))
	}
	w.expectStarts(t, "S1", "S2", "S3", "S4")
	p := w.p
	p.Cap = 1
	if err := registry.Save(w.d.cfg.RegistryPath, &registry.File{Version: registry.Version, GlobalCap: 1, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	rec := w.start(t, "S5")
	if rec.State == runrec.StateQueued {
		t.Fatal("frontier run queued behind local reservations")
	}
	w.expectStart(t, "S5")
}

func TestFifthLocalRunQueuesUntilReservationReleased(t *testing.T) {
	w := newWaveRig(t, 10, 10)
	localLaunchers(t)
	for i := 1; i <= 4; i++ {
		w.startLocal(t, fmt.Sprintf("S%d", i))
	}
	w.expectStarts(t, "S1", "S2", "S3", "S4")
	fifth := w.startLocal(t, "S5")
	if fifth.State != runrec.StateQueued {
		t.Fatalf("fifth local run state %s, want queued", fifth.State)
	}
	w.expectNoStart(t)
	w.free <- struct{}{}
	w.expectStart(t, "S5")
}

func TestLocalReservationWakesQueueWhenRoleSlotFrees(t *testing.T) {
	w := newWaveRig(t, 10, 10)
	localLaunchers(t)
	var holders []func()
	for range 4 {
		release, _, err := w.d.runs.deps.Vendors.Acquire(context.Background(), "grok-local")
		if err != nil {
			t.Fatal(err)
		}
		holders = append(holders, release)
	}
	defer func() {
		for _, release := range holders {
			release()
		}
	}()
	rec := w.startLocal(t, "S1")
	if rec.State != runrec.StateQueued {
		t.Fatalf("local run over role pool state %s, want queued", rec.State)
	}
	holders[0]() // no run job ends: the pool release itself must wake admission
	w.expectStart(t, "S1")
}

func localCapReport(role string) engine.Result {
	report := "Tests: 1 passed, 0 failed\n\n## Owner needed\nnone\n"
	if role == engine.RoleReviewer || role == engine.RoleUI {
		report = "(A): nothing\n(B): nothing\n(D): nothing\n\n## Owner needed\nnone\n"
	}
	if role == engine.RoleGate {
		report = "VERDICT: PASS\n" + report
	}
	return engine.Result{Exit: engine.ExitOK, Report: report}
}

func TestLocalRunReservationLastsThroughCloudRolesAndOwnerWaits(t *testing.T) {
	for _, phase := range []string{engine.RoleReviewer, engine.RoleGate, runrec.StateAwaitingApproval, runrec.StatePaused} {
		t.Run(phase, func(t *testing.T) {
			w := newWaveRig(t, 10, 10)
			localLaunchers(t)
			w.d.runs.deps.Runner = roundRunner(func(ctx context.Context, req engine.Request) (engine.Result, error) {
				if req.Role == phase {
					w.started <- filepath.Base(req.Worktree)
					select {
					case <-ctx.Done():
						return engine.Result{}, ctx.Err()
					case <-w.free:
					}
				}
				res := localCapReport(req.Role)
				if phase == runrec.StatePaused && req.Role == engine.RoleImplementer {
					res.Report = "Tests: 1 passed, 0 failed\n\n## Owner needed\nQuestion: Continue?\nOptions:\n1. YES: continue\n2. NO: stop\nRecommend: 1 - test pause\n"
				}
				return res, nil
			})
			var recs []runrec.Record
			for i := 1; i <= 4; i++ {
				recs = append(recs, w.startLocal(t, fmt.Sprintf("S%d", i)))
			}
			if phase == engine.RoleReviewer || phase == engine.RoleGate {
				w.expectStarts(t, "S1", "S2", "S3", "S4")
			} else {
				for _, rec := range recs {
					waitState(t, w.store, rec.ID, phase)
					waitIdle(t, w.d, rec.ID)
				}
			}
			fifth := w.startLocal(t, "S5")
			if fifth.State != runrec.StateQueued {
				t.Fatalf("fifth local run during %s is %s, want queued", phase, fifth.State)
			}
			if phase == runrec.StatePaused {
				answered, err := w.d.ownerRun(context.Background(), MethodRunAnswer, RunAction{Run: recs[0].ID, Text: "YES"})
				if err != nil || answered.Resume != nil {
					t.Fatalf("local answer waited on its own reservation: %+v, %v", answered.Resume, err)
				}
			}
			method := MethodRunStop
			if phase == runrec.StateAwaitingApproval {
				method = MethodRunApprove
			}
			if _, err := w.d.ownerRun(context.Background(), method, RunAction{Run: recs[0].ID}); err != nil {
				t.Fatal(err)
			}
			if phase == engine.RoleReviewer || phase == engine.RoleGate {
				w.expectStart(t, "S5")
			} else {
				waitState(t, w.store, fifth.ID, phase)
			}
		})
	}
}

func TestPrimaryLocalRunKeepsCapClassAfterCloudFallback(t *testing.T) {
	w := newWaveRig(t, 1, 1)
	localLaunchers(t)
	w.d.runs.deps.Runner = roundRunner(func(ctx context.Context, req engine.Request) (engine.Result, error) {
		if req.Vendor == "claude-local" {
			return engine.Result{Exit: engine.ExitError}, nil
		}
		w.started <- filepath.Base(req.Worktree)
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	w.startWithLineup(t, "S1", []runrec.LineupEntry{{Vendor: "claude-local", Model: "qwen", Effort: "medium"}, {Vendor: "claude", Model: "sonnet", Effort: "medium"}})
	w.expectStart(t, "S1")
	frontier := w.start(t, "S2")
	if frontier.State == runrec.StateQueued {
		t.Fatal("primary-local run consumed frontier cap after cloud fallback")
	}
	w.expectStart(t, "S2")
}

func TestPrimaryFrontierRunKeepsCapClassAfterLocalFallback(t *testing.T) {
	w := newWaveRig(t, 1, 1)
	localLaunchers(t)
	w.d.runs.deps.Runner = roundRunner(func(ctx context.Context, req engine.Request) (engine.Result, error) {
		if filepath.Base(req.Worktree) == "S1" && req.Vendor == "claude" {
			return engine.Result{Exit: engine.ExitError}, nil
		}
		w.started <- filepath.Base(req.Worktree)
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	w.startWithLineup(t, "S1", []runrec.LineupEntry{{Vendor: "claude", Model: "sonnet", Effort: "medium"}, {Vendor: "claude-local", Model: "qwen", Effort: "medium"}})
	w.expectStart(t, "S1")
	frontier := w.start(t, "S2")
	if frontier.State != runrec.StateQueued {
		t.Fatalf("second frontier run state %s, want queued", frontier.State)
	}
	local := w.startLocal(t, "S3")
	if local.State == runrec.StateQueued {
		t.Fatal("local run queued behind primary-frontier fallback")
	}
	w.expectStart(t, "S3")
}

func TestLocalReservationsReleasedAfterDaemonJoinsRoles(t *testing.T) {
	w := newWaveRig(t, 10, 10)
	localLaunchers(t)
	for i := 1; i <= 4; i++ {
		w.startLocal(t, fmt.Sprintf("S%d", i))
	}
	w.expectStarts(t, "S1", "S2", "S3", "S4")
	closed := make(chan struct{})
	go func() { w.d.closeRuns(); close(closed) }()
	defer func() {
		if w.d.runs.localWakeCancel != nil {
			w.d.runs.localWakeCancel()
		}
		<-closed
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("daemon close did not join its local admission watcher")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	for range 4 {
		release, waited, err := w.d.runs.deps.Vendors.Acquire(ctx, "codex-local")
		if err != nil || waited {
			t.Fatalf("daemon close kept a local reservation: waited=%v err=%v", waited, err)
		}
		releases = append(releases, release)
	}
}

func TestLocalReservationsRecoveredBeforeQueuedRuns(t *testing.T) {
	w := newWaveRig(t, 10, 10)
	localLaunchers(t)
	w.d.runs.deps.Runner = roundRunner(func(_ context.Context, req engine.Request) (engine.Result, error) {
		res := localCapReport(req.Role)
		res.Report = "Tests: 1 passed, 0 failed\n\n## Owner needed\nQuestion: Continue?\nOptions:\n1. YES: continue\n2. NO: stop\nRecommend: 1 - test pause\n"
		return res, nil
	})
	var recs []runrec.Record
	for i := 1; i <= 4; i++ {
		recs = append(recs, w.startLocal(t, fmt.Sprintf("S%d", i)))
	}
	for _, rec := range recs {
		waitState(t, w.store, rec.ID, runrec.StatePaused)
		waitIdle(t, w.d, rec.ID)
	}
	fifth := w.startLocal(t, "S5")
	w.d.closeRuns()
	deps, cfg := w.d.runs.deps, w.d.cfg
	cfg.Engine = &deps
	recovered := New(cfg)
	t.Cleanup(recovered.cache.Close)
	t.Cleanup(recovered.closeRuns)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := recovered.recoverRuns(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := w.store.Open(fifth.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := run.Snapshot().State; got != runrec.StateQueued {
		t.Fatalf("recovered fifth local run is %s, want queued", got)
	}
	if _, err := recovered.ownerRun(context.Background(), MethodRunStop, RunAction{Run: recs[0].ID}); err != nil {
		t.Fatal(err)
	}
	waitState(t, w.store, fifth.ID, runrec.StatePaused)
}

func TestDefaultGlobalCapQueuesTheThirdRunUntilASlotFrees(t *testing.T) {
	w := newWaveRig(t, 0, 0)
	w.start(t, "S1")
	w.start(t, "S2")
	w.expectStarts(t, "S1", "S2")
	third := w.start(t, "S3")
	if third.State != runrec.StateQueued {
		t.Fatalf("third run state %q, want queued", third.State)
	}
	w.expectNoStart(t)
	w.free <- struct{}{}
	w.expectStart(t, "S3")
}

func TestWaveGlobalCapQueuesTheFifthRun(t *testing.T) {
	w := newWaveRig(t, 4, 0)
	var recs []runrec.Record
	for i := 1; i <= 5; i++ {
		recs = append(recs, w.start(t, fmt.Sprintf("S%d", i)))
	}
	w.expectStarts(t, "S1", "S2", "S3", "S4")
	w.expectNoStart(t)
	for i, rec := range recs {
		want := runrec.StateSetup
		if i == 4 {
			want = runrec.StateQueued
		}
		if i < 4 && rec.State == runrec.StateQueued {
			t.Errorf("run %d queued under the cap", i+1)
		}
		if i == 4 && rec.State != want {
			t.Errorf("fifth run state %q, want queued", rec.State)
		}
		if !regexp.MustCompile(`^wave-\d{8}-\d{6}-[0-9a-f]{4}$`).MatchString(rec.Wave) {
			t.Errorf("run %d wave id %q", i+1, rec.Wave)
		}
	}
	got := w.store.Dir
	b, err := os.ReadFile(filepath.Join(got, recs[4].ID, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if err := json.Unmarshal(b, &disk); err != nil {
		t.Fatal(err)
	}
	if disk["state"] != "queued" || disk["wave"] == "" || disk["wave"] == nil {
		t.Errorf("queued run.json: state %v wave %v", disk["state"], disk["wave"])
	}
	if ws, _ := disk["writes"].([]any); len(ws) != 0 {
		t.Errorf("queued run wrote: %v", ws)
	}
}

func TestWaveProjectCapHoldsASecondRun(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	first := w.start(t, "S1")
	second := w.start(t, "S2")
	w.expectStart(t, "S1")
	w.expectNoStart(t)
	if first.State == runrec.StateQueued || second.State != runrec.StateQueued {
		t.Errorf("states %q %q", first.State, second.State)
	}
}

func TestQueuedRunStartsWhenASlotFrees(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	w.start(t, "S1")
	w.start(t, "S2")
	third := w.start(t, "S3")
	w.expectStart(t, "S1")
	w.free <- struct{}{}
	w.expectStart(t, "S2") // oldest first
	waitState(t, w.store, third.ID, runrec.StateQueued)
	w.expectNoStart(t)
	w.free <- struct{}{}
	w.expectStart(t, "S3")
}

func TestQueuedRunOfARemovedProjectStopsWithAReason(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	w.start(t, "S1")
	removed := w.start(t, "S2")
	w.expectStart(t, "S1")
	if removed.State != runrec.StateQueued {
		t.Fatal("second run must queue")
	}
	// Share the fixture path so the next project's run uses the same fake tools.
	q := w.p
	q.Name = "q"
	if err := registry.Save(w.d.cfg.RegistryPath, &registry.File{Version: registry.Version, GlobalCap: 1, Projects: []registry.Project{w.p, q}}); err != nil {
		t.Fatal(err)
	}
	request(t, w.d, "run.start", `{"project":"q","item":"S3"}`)
	if err := registry.Save(w.d.cfg.RegistryPath, &registry.File{Version: registry.Version, GlobalCap: 1, Projects: []registry.Project{q}}); err != nil {
		t.Fatal(err)
	}
	w.free <- struct{}{}
	rec := waitState(t, w.store, removed.ID, runrec.StateStopped)
	if rec.Reason != "project was removed" {
		t.Errorf("reason %q, want project was removed", rec.Reason)
	}
	w.expectStart(t, "S3") // skipping the removed project must advance the queue
	w.expectNoStart(t)
}

func TestQueuedRunStartsWithCachedRegistry(t *testing.T) {
	for _, caps := range []struct {
		name            string
		global, project int
	}{{"global", 1, 0}, {"project", 0, 1}} {
		t.Run(caps.name, func(t *testing.T) {
			w := newWaveRig(t, caps.global, caps.project)
			w.start(t, "S1")
			second := w.start(t, "S2")
			third := w.start(t, "S3")
			w.expectStart(t, "S1")
			if second.State != runrec.StateQueued || third.State != runrec.StateQueued {
				t.Fatal("runs over the cap must queue")
			}
			if err := os.WriteFile(w.d.cfg.RegistryPath, []byte(`{"projects":`), 0600); err != nil {
				t.Fatal(err)
			}
			w.free <- struct{}{}
			w.expectStart(t, "S2")
			w.expectNoStart(t) // cached caps still hold the third run
			waitState(t, w.store, third.ID, runrec.StateQueued)
			w.free <- struct{}{}
			w.expectStart(t, "S3") // a failed read must not erase the cache
		})
	}
}

func TestQueuedRunUsesLatestJobEndRegistry(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	w.start(t, "S1")
	w.start(t, "S2")
	w.start(t, "S3")
	fourth := w.start(t, "S4")
	w.expectStart(t, "S1")
	if fourth.State != runrec.StateQueued {
		t.Fatal("fourth run must queue")
	}
	// Only a job-end read sees the new cap before the registry breaks.
	if err := registry.Save(w.d.cfg.RegistryPath, &registry.File{Version: registry.Version, GlobalCap: 2, Projects: []registry.Project{w.p}}); err != nil {
		t.Fatal(err)
	}
	w.free <- struct{}{}
	w.expectStarts(t, "S2", "S3")
	w.expectNoStart(t)
	if err := os.WriteFile(w.d.cfg.RegistryPath, []byte(`{"projects":`), 0600); err != nil {
		t.Fatal(err)
	}
	w.free <- struct{}{}
	w.expectStart(t, "S4")
}

func TestQueuedRunWaitsForFirstGoodRegistryRead(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	// Install records without admission or recovery: neither has read a registry.
	queued, err := w.store.Create(runrec.Record{ID: "queued", Project: "p", Item: "S1", State: runrec.StateQueued,
		Worktree: filepath.Join(w.p.Path, ".worktrees", "S1"), Lineup: map[string][]runrec.LineupEntry{
			engine.RoleImplementer: {{Vendor: "claude", Model: "sonnet"}}, engine.RoleReviewer: {{Vendor: "claude", Model: "opus"}}}})
	if err != nil {
		t.Fatal(err)
	}
	held, err := w.store.Create(runrec.Record{ID: "held", Project: "p", Item: "H1", State: runrec.StatePaused})
	if err != nil {
		t.Fatal(err)
	}
	w.d.runs.runs["queued"] = &managedRun{engine: engine.Load(w.d.runDeps(w.p), w.p, queued)}
	r := &managedRun{engine: engine.Load(w.d.runDeps(w.p), w.p, held)}
	w.d.runs.runs["held"] = r
	endJob := func() {
		t.Helper()
		w.d.runs.mu.Lock()
		w.d.launchRun(r, func(context.Context) error { return nil })
		done := r.done
		w.d.runs.mu.Unlock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("job did not end")
		}
		// done closes before the queue check, which holds this lock.
		w.d.runs.mu.Lock()
		w.d.runs.mu.Unlock()
	}
	if err := os.WriteFile(w.d.cfg.RegistryPath, []byte(`{"projects":`), 0600); err != nil {
		t.Fatal(err)
	}
	endJob()
	w.expectNoStart(t)
	waitState(t, w.store, "queued", runrec.StateQueued)
	if err := registry.Save(w.d.cfg.RegistryPath, &registry.File{Version: registry.Version, GlobalCap: 1, Projects: []registry.Project{w.p}}); err != nil {
		t.Fatal(err)
	}
	endJob()
	w.expectStart(t, "S1")
}

func TestQueuedRunsRestartOldestFirst(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	// Created newest id first, so the order is the creation time, not the id.
	base := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for i, item := range []string{"S3", "S1", "S2"} {
		w.store.Now = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		_, err := w.store.Create(runrec.Record{ID: "q-" + item, Project: "p", Item: item, State: runrec.StateQueued, Wave: "wave-x",
			Worktree: filepath.Join(w.p.Path, ".worktrees", item), Lineup: map[string][]runrec.LineupEntry{
				engine.RoleImplementer: {{Vendor: "claude", Model: "sonnet"}}, engine.RoleReviewer: {{Vendor: "claude", Model: "opus"}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.expectStart(t, "S3")
	w.free <- struct{}{}
	w.expectStart(t, "S1")
	w.free <- struct{}{}
	w.expectStart(t, "S2")
}

func TestQueuedRunCountsAsBusy(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	// A busy run whose record rests fills the only slot, so S1 queues. It is
	// then made idle, so only the queued record can keep the daemon.
	held, err := w.store.Create(runrec.Record{ID: "held", Project: "p", Item: "H1", State: runrec.StatePaused})
	if err != nil {
		t.Fatal(err)
	}
	w.d.runs.runs["held"] = &managedRun{engine: engine.Load(w.d.runDeps(w.p), w.p, held), busy: true}
	queued := w.start(t, "S1")
	if queued.State != runrec.StateQueued {
		t.Fatalf("run over the cap: %q", queued.State)
	}
	w.d.runs.mu.Lock()
	w.d.runs.runs["held"].busy = false
	w.d.runs.mu.Unlock()
	if !w.d.activeRuns() {
		t.Error("a queued run must count as active")
	}
	if _, err := w.d.stopDaemon(StopParams{}); err == nil || !strings.Contains(err.Error(), "running") {
		t.Errorf("daemon stop without force accepted with a queued run: %v", err)
	}
	clock := &fakeClock{}
	exited := false
	idle := &idleTimer{clock: clock, timeout: func() time.Duration { return time.Minute }, waveRunning: w.d.activeRuns, onIdle: func() { exited = true }}
	idle.start()
	defer idle.stop()
	clock.Advance(time.Minute)
	if exited {
		t.Error("idle timer fired with a queued run")
	}
}

func TestStopQueuedRunMakesNoWrites(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	w.start(t, "S1")
	queued := w.start(t, "S2")
	w.expectStart(t, "S1")
	before := len(w.fake.Calls())
	rec, err := w.d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: queued.ID})
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != runrec.StateStopped || len(rec.Writes) != 0 {
		t.Errorf("stopped queued run: state %q writes %+v", rec.State, rec.Writes)
	}
	if got := w.fake.Calls(); len(got) != before {
		t.Errorf("queued stop called tools: %v", got[before:])
	}
}

// mkPaused stores a paused run of project p, as a restart would find it.
func (w *waveRig) mkPaused(t *testing.T, id, item string, pending *runrec.Pending, resume *runrec.ResumeRequest) {
	t.Helper()
	_, err := w.store.Create(runrec.Record{ID: id, Project: "p", Item: item, State: runrec.StatePaused, Pending: pending, Resume: resume,
		Worktree: filepath.Join(w.p.Path, ".worktrees", item), Lineup: map[string][]runrec.LineupEntry{
			engine.RoleImplementer: {{Vendor: "claude", Model: "sonnet"}}, engine.RoleReviewer: {{Vendor: "claude", Model: "opus"}}}})
	if err != nil {
		t.Fatal(err)
	}
}

func errorPause() *runrec.Pending {
	return &runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Role: engine.RoleImplementer, Reason: "x"}
}

func questionPause() *runrec.Pending {
	return &runrec.Pending{Kind: runrec.PendingQuestion, State: runrec.StateImplement, Role: engine.RoleImplementer, Reason: "q", Question: "which?"}
}

// retryNoWait asks for a retry the way the CLI does: the reply comes once the
// action is applied, or at once when the resume waits.
func (w *waveRig) retryNoWait(t *testing.T, run string) RunReply {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reply, err := w.d.ownerRunReply(ctx, MethodRunRetry, RunAction{Run: run, NoWait: true})
	if err != nil {
		t.Fatalf("retry %s: %v", run, err)
	}
	return reply
}

func (w *waveRig) record(t *testing.T, id string) runrec.Record {
	t.Helper()
	r, err := w.store.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	return r.Snapshot()
}

func TestRetryOfAPausedRunWaitsForTheProjectSlot(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	w.mkPaused(t, "s1", "S1", errorPause(), nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.start(t, "S2") // the paused run holds no slot, so S2 starts
	w.expectStart(t, "S2")
	reply := w.retryNoWait(t, "s1")
	if reply.State != runrec.StatePaused || reply.Resume == nil || reply.Resume.Method != runrec.ResumeRetry {
		t.Fatalf("reply: state %q resume %+v, want paused with a retry waiting", reply.State, reply.Resume)
	}
	if !strings.Contains(reply.Message, "waits for a slot") {
		t.Errorf("reply message %q", reply.Message)
	}
	w.expectNoStart(t)
	if got := w.record(t, "s1"); got.State != runrec.StatePaused || got.Resume == nil {
		t.Fatalf("stored record: state %q resume %+v", got.State, got.Resume)
	}
	w.free <- struct{}{} // S2 ends
	w.expectStart(t, "S1")
	waitFor(t, "resume cleared", func() bool { return w.record(t, "s1").Resume == nil })
}

// S346 (*proposed*; adj 479): a retry with --allow that waits for a slot keeps
// the patterns in its resume request and stores them as the run's grants when
// it starts. A bad pattern does not wait.
func TestRetryAllowWaitsForASlotAndIsStoredWhenItStarts(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	w.mkPaused(t, "s1", "S1", errorPause(), nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.start(t, "S2")
	w.expectStart(t, "S2")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.d.ownerRunReply(ctx, MethodRunRetry, RunAction{Run: "s1", Allow: []string{"Bash(make test*)", " "}, NoWait: true}); err == nil || !strings.Contains(err.Error(), "blank pattern") {
		t.Fatalf("a retry with a blank pattern: %v", err)
	}
	if got := w.record(t, "s1"); got.Resume != nil || len(got.Grants) != 0 {
		t.Fatalf("a refused retry stored resume %+v grants %v", got.Resume, got.Grants)
	}
	reply, err := w.d.ownerRunReply(ctx, MethodRunRetry, RunAction{Run: "s1", Allow: []string{"Bash(make test*)"}, NoWait: true})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Resume == nil || !reflect.DeepEqual(reply.Resume.Allow, []string{"Bash(make test*)"}) {
		t.Fatalf("reply resume %+v, want the patterns kept", reply.Resume)
	}
	if got := w.record(t, "s1"); len(got.Grants) != 0 {
		t.Errorf("the grant was stored before the run started: %v", got.Grants)
	}
	w.free <- struct{}{} // S2 ends
	w.expectStart(t, "S1")
	waitFor(t, "grant stored", func() bool {
		return reflect.DeepEqual(w.record(t, "s1").Grants, []string{"Bash(make test*)"})
	})
}

func TestAnswerOfAPausedRunWaitsForTheGlobalSlot(t *testing.T) {
	w := newWaveRig(t, 1, 0)
	q := w.p
	q.Name = "q"
	if err := registry.Save(w.d.cfg.RegistryPath, &registry.File{Version: registry.Version, GlobalCap: 1, Projects: []registry.Project{w.p, q}}); err != nil {
		t.Fatal(err)
	}
	w.mkPaused(t, "a1", "A1", questionPause(), nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	request(t, w.d, "run.start", `{"project":"q","item":"B1"}`)
	w.expectStart(t, "B1")
	b := request(t, w.d, "run.answer", `{"run":"a1","text":"use stdlib","no_wait":true}`)
	var reply RunReply
	if err := json.Unmarshal(b, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.State != runrec.StatePaused || reply.Resume == nil || reply.Resume.Method != runrec.ResumeAnswer || reply.Resume.Text != "use stdlib" {
		t.Fatalf("reply: %s", b)
	}
	if !strings.Contains(reply.Message, "waits for a slot") {
		t.Errorf("reply message %q", reply.Message)
	}
	w.expectNoStart(t)
	w.free <- struct{}{} // B1 ends
	w.expectStart(t, "A1")
	brief, _ := w.briefs.Load("A1")
	if got, _ := brief.(string); !strings.Contains(got, "use stdlib") {
		t.Errorf("A1 brief lacks the stored answer: %q", got)
	}
}

func TestAWaitingResumeGoesBeforeTheOldestQueuedRun(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	w.mkPaused(t, "s1", "S1", errorPause(), nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.start(t, "S2")
	w.expectStart(t, "S2")
	s3 := w.start(t, "S3")
	if s3.State != runrec.StateQueued {
		t.Fatalf("S3 state %q, want queued", s3.State)
	}
	w.retryNoWait(t, "s1")
	w.expectNoStart(t)   // the resume waits for S2's slot
	w.free <- struct{}{} // S2 ends
	w.expectStart(t, "S1")
	w.expectNoStart(t)
	waitState(t, w.store, s3.ID, runrec.StateQueued)
	w.free <- struct{}{} // S1 ends
	w.expectStart(t, "S3")
}

func TestRetryWithRoomStartsAtOnce(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	w.mkPaused(t, "s1", "S1", errorPause(), nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	reply := w.retryNoWait(t, "s1")
	w.expectStart(t, "S1")
	if reply.Resume != nil || reply.Message != "" {
		t.Errorf("a resume with room waited: resume %+v message %q", reply.Resume, reply.Message)
	}
}

func TestApproveStillBypassesTheCaps(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	if _, err := w.store.Create(runrec.Record{ID: "p1", Project: "p", Item: "P1", State: runrec.StateAwaitingApproval,
		Worktree: filepath.Join(w.p.Path, ".worktrees", "P1"), Lineup: map[string][]runrec.LineupEntry{
			engine.RoleImplementer: {{Vendor: "claude", Model: "sonnet"}}, engine.RoleReviewer: {{Vendor: "claude", Model: "opus"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.start(t, "S2")
	w.expectStart(t, "S2") // fills the project cap of 1
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go w.d.ownerRun(ctx, MethodRunApprove, RunAction{Run: "p1"})
	w.expectStart(t, "P1") // the finish step runs a role at once, over the cap
}

func TestAWaitingResumeSurvivesARestart(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	resume := &runrec.ResumeRequest{Method: runrec.ResumeAnswer, Text: "use stdlib", At: time.Now()}
	w.mkPaused(t, "s1", "S1", questionPause(), resume)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.expectStart(t, "S1") // a free slot starts the stored resume
	brief, _ := w.briefs.Load("S1")
	if got, _ := brief.(string); !strings.Contains(got, "use stdlib") {
		t.Errorf("S1 brief lacks the stored answer: %q", got)
	}
	waitFor(t, "resume cleared", func() bool { return w.record(t, "s1").Resume == nil })
}

func TestARestartStartsTheOldestWaitingResumeFirst(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	at := time.Now()
	// The ids sort against the request times, so only the times can order them.
	w.mkPaused(t, "a-late", "S2", errorPause(), &runrec.ResumeRequest{Method: runrec.ResumeRetry, At: at})
	w.mkPaused(t, "b-early", "S1", errorPause(), &runrec.ResumeRequest{Method: runrec.ResumeRetry, At: at.Add(-time.Minute)})
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.expectStart(t, "S1")
	w.expectNoStart(t) // the project cap of 1 holds the later resume
	if got := w.record(t, "a-late"); got.State != runrec.StatePaused || got.Resume == nil {
		t.Fatalf("later resume: state %q resume %+v", got.State, got.Resume)
	}
	w.free <- struct{}{}
	w.expectStart(t, "S2")
}

func TestAWaitingRetryKeepsItsRoundGrant(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	w.mkPaused(t, "s1", "S1", &runrec.Pending{Kind: runrec.PendingRounds, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1, Reason: "rounds"}, nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.start(t, "S2")
	w.expectStart(t, "S2")
	reply, err := w.d.ownerRunReply(context.Background(), MethodRunRetry, RunAction{Run: "s1", Rounds: 3, NoWait: true})
	if err != nil || reply.Resume == nil || reply.Resume.Rounds != 3 {
		t.Fatalf("retry --rounds 3: resume %+v, err %v", reply.Resume, err)
	}
	w.free <- struct{}{} // S2 ends
	w.expectStart(t, "S1")
	if got := w.record(t, "s1"); got.RoundLimit != 3 {
		t.Errorf("round limit %d, want the stored grant of 3", got.RoundLimit)
	}
}

func TestStopClearsAWaitingResume(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	w.mkPaused(t, "s1", "S1", errorPause(), nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.start(t, "S2")
	w.expectStart(t, "S2")
	w.start(t, "S3")
	w.retryNoWait(t, "s1")
	w.expectNoStart(t)
	b := request(t, w.d, "run.stop", `{"run":"s1"}`)
	var stopped runrec.Record
	if err := json.Unmarshal(b, &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.State != runrec.StateStopped || stopped.Resume != nil {
		t.Fatalf("stopped reply: state %q resume %+v", stopped.State, stopped.Resume)
	}
	if got := w.record(t, "s1"); got.State != runrec.StateStopped || got.Resume != nil {
		t.Fatalf("stored record: state %q resume %+v", got.State, got.Resume)
	}
	w.free <- struct{}{} // S2 ends
	w.expectStart(t, "S3")
	w.expectNoStart(t)
}

func TestASecondResumeIsRefusedWhileOneWaits(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	w.mkPaused(t, "s1", "S1", errorPause(), nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.start(t, "S2")
	w.expectStart(t, "S2")
	w.retryNoWait(t, "s1")
	_, err := w.d.ownerRunReply(context.Background(), MethodRunRetry, RunAction{Run: "s1", NoWait: true})
	if err == nil || !strings.Contains(err.Error(), "already has a resume waiting; use ork stop s1") {
		t.Fatalf("second retry error: %v", err)
	}
}

// A second answer or retry is refused as a waiting resume whatever the kind of
// the first one or of the pause.
func TestACrossKindResumeIsRefusedWhileOneWaits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pause   *runrec.Pending
		first   string
		second  string
		secondA RunAction
	}{
		{"answer while a retry waits", errorPause(), MethodRunRetry, MethodRunAnswer, RunAction{Run: "s1", Text: "use stdlib", NoWait: true}},
		{"retry while an answer waits", questionPause(), MethodRunAnswer, MethodRunRetry, RunAction{Run: "s1", NoWait: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWaveRig(t, 0, 1)
			w.mkPaused(t, "s1", "S1", tc.pause, nil)
			if err := w.d.recoverRuns(context.Background()); err != nil {
				t.Fatal(err)
			}
			w.start(t, "S2")
			w.expectStart(t, "S2")
			if _, err := w.d.ownerRunReply(context.Background(), tc.first, RunAction{Run: "s1", Text: "use stdlib", NoWait: true}); err != nil {
				t.Fatal(err)
			}
			_, err := w.d.ownerRunReply(context.Background(), tc.second, tc.secondA)
			if err == nil || err.Error() != "run s1 already has a resume waiting; use ork stop s1" {
				t.Fatalf("second %s: %v", tc.second, err)
			}
		})
	}
}

// The retry reply carries the wait note, as the answer reply does, so a client
// can show it (S274).
func TestRetryReplyCarriesTheWaitNote(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	w.mkPaused(t, "s1", "S1", errorPause(), nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.start(t, "S2")
	w.expectStart(t, "S2")
	var reply RunReply
	if err := json.Unmarshal(request(t, w.d, "run.retry", `{"run":"s1","no_wait":true}`), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Message != ResumeWaitNote || reply.Resume == nil {
		t.Fatalf("retry reply: message %q resume %+v", reply.Message, reply.Resume)
	}
}

func TestARetryThatTheEngineWouldRefuseDoesNotWait(t *testing.T) {
	w := newWaveRig(t, 0, 1)
	w.mkPaused(t, "s1", "S1", errorPause(), nil)
	if err := w.d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.start(t, "S2")
	w.expectStart(t, "S2")
	// Rounds can only be granted at a rounds pause; the owner hears that now.
	_, err := w.d.ownerRunReply(context.Background(), MethodRunRetry, RunAction{Run: "s1", Rounds: 3, NoWait: true})
	if err == nil || !strings.Contains(err.Error(), "rounds can only be granted at a rounds pause") {
		t.Fatalf("retry --rounds error: %v", err)
	}
	if got := w.record(t, "s1"); got.Resume != nil {
		t.Errorf("a refused retry stored %+v", got.Resume)
	}
}

func TestStatusNamesAWaitingResume(t *testing.T) {
	for _, tc := range []struct {
		name            string
		global, project int
		want            string
	}{{"project", 0, 1, "resume waits for a slot under project cap 1; next: ork stop s1"}, {"global", 1, 0, "resume waits for a slot under global cap 1; next: ork stop s1"}} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWaveRig(t, tc.global, tc.project)
			w.mkPaused(t, "s1", "S1", errorPause(), nil)
			if err := w.d.recoverRuns(context.Background()); err != nil {
				t.Fatal(err)
			}
			w.start(t, "S2")
			w.expectStart(t, "S2")
			w.retryNoWait(t, "s1")
			var found bool
			for _, run := range w.d.engineRuns("p") {
				if run.ID == "s1" {
					found = true
					if run.Wait != tc.want {
						t.Errorf("wait %q, want %q", run.Wait, tc.want)
					}
					if !run.ResumeWaiting {
						t.Error("status does not mark the run as resume waiting")
					}
				}
			}
			if !found {
				t.Fatal("run s1 is not in status")
			}
		})
	}
}

func TestEngineRunRolesWire(t *testing.T) {
	d, store, _ := runDaemon(t, nil)
	at := time.Date(2026, 10, 19, 16, 40, 0, 0, time.UTC)
	role := runrec.RoleRecord{Role: "implementer", Round: 2, Vendor: "claude", Model: "sonnet", Effort: "high", Step: "step-2", StartedAt: at, EndedAt: at.Add(time.Minute), Exit: "ok", Session: "private-session", PGID: 123, Report: "implementer-2.md"}
	if _, err := store.Create(runrec.Record{ID: "tree", Project: "p", Item: "S55", State: "paused", Roles: []runrec.RoleRecord{role}}); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(d.engineRuns("p"))
	if err != nil {
		t.Fatal(err)
	}
	var got []struct {
		Roles []map[string]any `json:"roles"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Roles) != 1 {
		t.Fatalf("missing roles: %s", b)
	}
	want := map[string]any{"report": "implementer-2.md", "role": "implementer", "round": float64(2), "vendor": "claude", "model": "sonnet", "effort": "high", "step": "step-2", "started_at": at.Format(time.RFC3339), "ended_at": at.Add(time.Minute).Format(time.RFC3339), "exit": "ok"}
	if !reflect.DeepEqual(got[0].Roles[0], want) {
		t.Fatalf("roles wire: got %s, want %v", b, want)
	}
}

func wireRunError(t *testing.T, row EngineRun) string {
	t.Helper()
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	return wire.Error
}

// The wire row of a queued run carries the record's planned lineup, so the TUI
// can show what will run before it starts (S267, ui.queued-plan-source).
func TestEngineRunCarriesThePlannedLineup(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	lineup := map[string][]runrec.LineupEntry{
		engine.RoleImplementer: {{Vendor: "claude", Model: "sonnet", Effort: "high"}},
		engine.RoleReviewer:    {{Vendor: "codex", Model: "sol", Effort: "medium"}, {Vendor: "claude", Model: "opus", Effort: "high"}},
	}
	if _, err := store.Create(runrec.Record{ID: "queued", Project: p.Name, Item: "S1", State: runrec.StateQueued, Lineup: lineup}); err != nil {
		t.Fatal(err)
	}
	rows := d.engineRuns(p.Name)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if !reflect.DeepEqual(rows[0].Lineup, lineup) {
		t.Errorf("wire lineup = %v, want the record's %v", rows[0].Lineup, lineup)
	}
}

func TestBackgroundErrorStaysOnTheWireRow(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	run, err := store.Create(runrec.Record{ID: "failed", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	r := &managedRun{engine: engine.Load(d.runDeps(p), p, run)}
	d.runs.mu.Lock()
	d.runs.runs["failed"] = r
	d.launchRun(r, func(context.Context) error { return errors.New("background broke") })
	d.runs.mu.Unlock()
	waitIdle(t, d, "failed")
	row := d.engineRuns(p.Name)[0]
	if got := wireRunError(t, row); got != "background broke" {
		t.Errorf("background row error = %q, want background broke", got)
	}
	for _, method := range []string{MethodStatus, MethodSnapshot} {
		wire := request(t, d, method, `{"project":"p"}`)
		if !strings.Contains(string(wire), `"error":"background broke"`) {
			t.Errorf("%s lost background error: %s", method, wire)
		}
	}
	if row.State != runrec.StateImplement {
		t.Errorf("failure changed state: %s", row.State)
	}
	b, err := os.ReadFile(filepath.Join(store.RunDir("failed"), "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if err := json.Unmarshal(b, &disk); err != nil {
		t.Fatal(err)
	}
	if _, ok := disk["error"]; ok {
		t.Error("run.json contains error")
	}
	if !d.activeRuns() {
		t.Error("background failure must remain active")
	}
	if _, err := d.stopDaemon(StopParams{}); err == nil {
		t.Error("background failure allowed daemon stop")
	}
	started := make(chan struct{})
	release := make(chan struct{})
	d.runs.mu.Lock()
	d.launchRun(r, func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	cleared := r.failure == nil
	d.runs.mu.Unlock()
	<-started
	if !cleared {
		t.Error("launch did not clear previous error")
	}
	if got := wireRunError(t, d.engineRuns(p.Name)[0]); got != "" {
		t.Errorf("busy row kept error: %q", got)
	}
	close(release)
	waitIdle(t, d, "failed")
	if _, err := d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: "failed"}); err != nil {
		t.Fatal(err)
	}
	if got := wireRunError(t, d.engineRuns(p.Name)[0]); got != "" {
		t.Errorf("next job retained error: %q", got)
	}
}

func TestBackgroundErrorCanceledIsNotFailure(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	run, err := store.Create(runrec.Record{ID: "cancelled", Project: p.Name, State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	r := &managedRun{engine: engine.Load(d.runDeps(p), p, run)}
	d.runs.mu.Lock()
	d.runs.runs["cancelled"] = r
	d.launchRun(r, func(ctx context.Context) error { <-ctx.Done(); return fmt.Errorf("round: %w", ctx.Err()) })
	r.cancel()
	d.runs.mu.Unlock()
	waitIdle(t, d, "cancelled")
	if got := wireRunError(t, d.engineRuns(p.Name)[0]); got != "" {
		t.Errorf("canceled job recorded failure: %q", got)
	}
}

// TestRecoverRunsWritesTheRunMapUnderTheLock exercises every publication path.
// Run with -race: the reader uses the same lock as run-manager clients.
func TestRecoverRunsWritesTheRunMapUnderTheLock(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   string
		removed bool
		failed  bool
	}{
		{name: "queued", state: runrec.StateQueued},
		{name: "reconciled", state: runrec.StateImplement},
		{name: "reconcile_failed", state: runrec.StateImplement, failed: true},
		{name: "removed_queued", state: runrec.StateQueued, removed: true},
		{name: "removed_active", state: runrec.StateImplement, removed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, store, p := runDaemon(t, nil)
			// Keep registered queued runs queued; admission is not under test.
			d.runs.closed = true
			project := p.Name
			if tc.removed {
				project = "removed"
			}
			rec := runrec.Record{ID: "recover", Project: project, State: tc.state}
			if tc.failed {
				d.runs.deps.Proc = failedRecoveryProc{}
				rec.Roles = []runrec.RoleRecord{{Role: engine.RoleImplementer, PGID: 987654, StartedAt: time.Now()}}
			}
			if _, err := store.Create(rec); err != nil {
				t.Fatal(err)
			}
			ready, stop, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			go func() {
				defer close(done)
				close(ready)
				for {
					select {
					case <-stop:
						return
					default:
						d.runs.mu.Lock()
						_ = d.runs.runs[rec.ID]
						d.runs.mu.Unlock()
					}
				}
			}()
			<-ready
			defer func() { close(stop); <-done }()
			if err := d.recoverRuns(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFailedRecoveryAllowsIdleAndDaemonStop(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(fmt.Sprint(removed), func(t *testing.T) {
			d, store, p := runDaemon(t, nil)
			d.runs.deps.Proc = failedRecoveryProc{}
			project := p.Name
			if removed {
				project = "removed"
			}
			_, err := store.Create(runrec.Record{ID: "failed", Project: project, State: runrec.StateImplement, Roles: []runrec.RoleRecord{{Role: engine.RoleImplementer, PGID: 987654, StartedAt: time.Now()}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.recoverRuns(context.Background()); err != nil {
				t.Fatal(err)
			}
			clock := &fakeClock{}
			exited := false
			idle := &idleTimer{clock: clock, timeout: func() time.Duration { return time.Minute }, waveRunning: d.activeRuns, onIdle: func() { exited = true }}
			idle.start()
			defer idle.stop()
			clock.Advance(time.Minute)
			if !exited {
				t.Error("failed recovery blocked idle exit")
			}
			if _, err := d.stopDaemon(StopParams{}); err != nil {
				t.Errorf("failed recovery blocked daemon stop: %v", err)
			}
		})
	}
}

func TestRecoveryRemovedQueuedProjectStops(t *testing.T) {
	d, store, _ := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "queued", Project: "removed", State: runrec.StateQueued})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := store.Open("queued")
	if err != nil {
		t.Fatal(err)
	}
	if rec := run.Snapshot(); rec.State != runrec.StateStopped || rec.Reason != "project was removed" {
		t.Fatalf("removed queued run: %+v", rec)
	}
	if d.activeRuns() {
		t.Error("stopped queued run is active")
	}
}

func TestRecoveryMissingProjectNilEngineSafety(t *testing.T) {
	d, store, _ := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "missing", Project: "removed", Wave: "wave-missing", State: runrec.StateImplement})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := d.runs.runs["missing"]; r == nil || r.engine != nil {
		t.Fatal("missing project run must be retained without engine")
	}
	rows := d.engineRuns("removed")
	if len(rows) != 1 || rows[0].Section(rows) != source.SectionNeeds {
		t.Errorf("missing project not in Needs you: %+v", rows)
	}
	for _, method := range []string{MethodRunStop, MethodRunRetry, MethodRunApprove, MethodRunAnswer} {
		rec, err := d.ownerRun(context.Background(), method, RunAction{Run: "missing", Text: "answer"})
		if rec.ID != "missing" || err == nil || !strings.Contains(err.Error(), `project "removed" is not registered`) {
			t.Errorf("%s: %+v, %v", method, rec, err)
		}
	}
	if _, err := d.stopWave(context.Background(), WaveAction{Wave: "wave-missing"}); err != nil {
		t.Fatal(err)
	}
	d.runs.mu.Lock()
	f, err := d.loadRunRegistryLocked()
	if err == nil {
		d.hasRoomLocked(f, "removed")
		d.startQueuedLocked(f)
	}
	d.runs.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	d.checkMerged(context.Background(), registry.Project{Name: "removed"}, "head")
	if err := d.cleanupRun(context.Background(), RunAction{Run: "missing"}); err == nil {
		t.Error("cleanup accepted unfinished missing-project run")
	}
	d.closeRuns()
}

type recoveryProc struct {
	failure error
	kills   int
}

func (p *recoveryProc) Alive(int) bool { return true }
func (p *recoveryProc) Kill(int) error { p.kills++; return p.failure }

func TestFailedRecoveryStopReconcilesBeforeFinalizing(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	proc := &recoveryProc{failure: errors.New("first kill failure")}
	d.runs.deps.Proc = proc
	_, err := store.Create(runrec.Record{ID: "failed", Project: p.Name, State: runrec.StateImplement, Roles: []runrec.RoleRecord{{Role: engine.RoleImplementer, PGID: 987654, StartedAt: time.Now()}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	proc.failure = errors.New("second kill failure")
	rec, err := d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: "failed"})
	if err == nil || !strings.Contains(err.Error(), "second kill failure") || rec.ID != "failed" || rec.State != runrec.StateImplement {
		t.Fatalf("failed stop lost run or finalized it: %+v, %v", rec, err)
	}
	if got := wireRunError(t, d.engineRuns(p.Name)[0]); !strings.Contains(got, "second kill failure") {
		t.Errorf("stop did not record new error: %q", got)
	}
	if d.activeRuns() {
		t.Error("failed stop Reconcile did not restore idle exception")
	}
	proc.failure = nil
	rec, err = d.ownerRun(context.Background(), MethodRunStop, RunAction{Run: "failed"})
	if err != nil || rec.State != runrec.StateStopped {
		t.Fatalf("reconciled stop: %+v, %v", rec, err)
	}
	if proc.kills != 3 {
		t.Errorf("kills = %d, want 3", proc.kills)
	}
	if got := wireRunError(t, d.engineRuns(p.Name)[0]); got != "" {
		t.Errorf("successful stop retained error: %q", got)
	}
	if d.runs.runs["failed"].recoveryFailed {
		t.Error("successful reconcile retained idle exception")
	}
}

func TestRemovedProjectDisplayGroup(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	for _, id := range []string{"missing-a", "missing-b"} {
		if _, err := store.Create(runrec.Record{ID: id, Project: "removed", Item: "S35", State: runrec.StateImplement}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	b := request(t, d, MethodStatus, `{}`)
	var status struct {
		Projects []struct {
			Name        string
			Unavailable bool
			NeedsYou    int         `json:"needs_you"`
			Runs        []EngineRun `json:"engine_runs"`
		}
	}
	if err := json.Unmarshal(b, &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Projects) != 2 {
		t.Errorf("status lost missing-project group: %s", b)
	} else {
		group := status.Projects[1]
		if group.Name != "removed" || !group.Unavailable || group.NeedsYou != 2 || len(group.Runs) != 2 {
			t.Errorf("bad missing-project group: %s", b)
		}
	}
	b = request(t, d, MethodProjectsList, `{}`)
	var list struct {
		Projects    []registry.Project
		Unavailable []struct {
			Name        string
			Unavailable bool
		}
	}
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Projects) != 1 || list.Projects[0].Name != p.Name || len(list.Unavailable) != 1 || list.Unavailable[0].Name != "removed" || !list.Unavailable[0].Unavailable {
		t.Errorf("discovery mixed registered and unavailable projects: %s", b)
	}
	f, err := d.loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Projects) != 1 {
		t.Error("pseudo-project entered registry")
	}
	if _, err := d.planRun(context.Background(), RunParams{Project: "removed", Item: "S35"}); err == nil {
		t.Error("pseudo-project accepted for launch")
	}
	b, err = json.Marshal(d.engineRuns("removed")[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"unavailable":true`) {
		t.Errorf("missing unavailable flag: %s", b)
	}
}

func TestUnavailableProjectClientDiscovery(t *testing.T) {
	d, store, _ := runDaemon(t, nil)
	if _, err := store.Create(runrec.Record{ID: "missing", Project: "removed", State: runrec.StateImplement}); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	d.cfg.Paths = e.paths
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	}()
	select {
	case <-d.Ready():
	case err := <-done:
		done <- err
		t.Fatalf("daemon failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon not ready")
	}
	c, err := Dial(context.Background(), d.cfg.Paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	groups, err := c.UnavailableProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || !groups[0].Unavailable || groups[0].Project.Name != "removed" || len(groups[0].EngineRuns) != 1 || !groups[0].EngineRuns[0].Unavailable {
		t.Fatalf("client lost display group: %+v", groups)
	}
	registered, err := c.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(registered) != 1 || registered[0].Name != "p" {
		t.Errorf("display group entered registered list: %+v", registered)
	}
	snapshot := c.Snapshot(context.Background(), registry.Project{Name: "removed"})
	if !snapshot.Unavailable || len(snapshot.EngineRuns) != 1 || snapshot.Items.Err != nil {
		t.Errorf("unavailable snapshot: %+v", snapshot)
	}
	status, err := c.Status(context.Background(), "removed")
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Projects) != 1 || !status.Projects[0].Unavailable || status.Projects[0].NeedsYou != 1 {
		t.Errorf("filtered missing-project status: %+v", status)
	}
	src := d.cfg.Source.(*stubSource)
	src.mu.Lock()
	defer src.mu.Unlock()
	for _, call := range src.calls {
		if strings.HasSuffix(call, " removed") {
			t.Errorf("unavailable project fetched data: %s", call)
		}
	}
}

func TestRetryClearsFailedRecoveryIdleException(t *testing.T) {
	d, store, p := runDaemon(t, func(context.Context, engine.Request) (engine.Result, error) {
		return engine.Result{Exit: engine.ExitOK, Report: "bad report"}, nil
	})
	run, err := store.Create(runrec.Record{ID: "failed", Project: p.Name, Item: "S1", State: runrec.StatePaused, Pending: &runrec.Pending{Kind: runrec.PendingError, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1}, Lineup: map[string][]runrec.LineupEntry{engine.RoleImplementer: {{Vendor: "claude", Model: "sonnet", Effort: "high"}}}})
	if err != nil {
		t.Fatal(err)
	}
	// Model a Reconcile write failure on a paused record without live roles.
	d.runs.runs["failed"] = &managedRun{engine: engine.Load(d.runDeps(p), p, run), failure: errors.New("reconcile write failed"), recoveryFailed: true}
	if d.activeRuns() {
		t.Error("unretried failed recovery is active")
	}
	if _, err := d.ownerRun(context.Background(), MethodRunRetry, RunAction{Run: "failed"}); err != nil {
		t.Fatal(err)
	}
	d.runs.mu.Lock()
	r := d.runs.runs["failed"]
	stillFailed := r.recoveryFailed
	d.runs.mu.Unlock()
	if stillFailed {
		t.Error("retry retained failed-recovery idle exception")
	}
	if err := run.Update(func(rec *runrec.Record) { rec.State = runrec.StateImplement }); err != nil {
		t.Fatal(err)
	}
	d.runs.mu.Lock()
	d.launchRun(r, func(context.Context) error { return errors.New("background broke after retry") })
	d.runs.mu.Unlock()
	waitIdle(t, d, "failed")
	if !d.activeRuns() {
		t.Error("background failure after retry did not count as active")
	}
}

func TestRemovedProjectRecoveryGuidanceAndRestart(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	if _, err := store.Create(runrec.Record{ID: "missing", Project: "removed", State: runrec.StateImplement}); err != nil {
		t.Fatal(err)
	}
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	row := d.engineRuns("removed")[0]
	if row.Error != `project "removed" is not registered` {
		t.Errorf("wire error has run prefix: %q", row.Error)
	}
	if !strings.Contains(row.Wait, "ork projects add PATH --name removed") || !strings.Contains(row.Wait, "ork daemon restart") {
		t.Errorf("missing recovery next step: %q", row.Wait)
	}
	p.Name = "removed"
	if err := registry.Save(d.cfg.RegistryPath, &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
		t.Fatal(err)
	}
	d.initRunManager()
	if err := d.recoverRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := d.runs.runs["missing"]; r == nil || r.engine == nil {
		t.Fatal("re-add and restart did not load engine")
	}
	row = d.engineRuns("removed")[0]
	if row.Unavailable || row.Error != "" || row.State != runrec.StatePaused {
		t.Errorf("re-added recovery did not reconcile: %+v", row)
	}
}

func TestBackgroundRunnerErrorIsHandledByEngine(t *testing.T) {
	d, store, _ := runDaemon(t, func(context.Context, engine.Request) (engine.Result, error) {
		return engine.Result{}, errors.New("runner broke")
	})
	raw := request(t, d, MethodRunStart, `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"claude","model":"sonnet","effort":"high"}]}}`)
	var rec runrec.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec = waitState(t, store, rec.ID, runrec.StateStopped)
	waitIdle(t, d, rec.ID)
	if !strings.Contains(rec.Reason, "runner broke") {
		t.Errorf("engine lost runner failure: %+v", rec)
	}
	if got := wireRunError(t, d.engineRuns("p")[0]); got != "" {
		t.Errorf("handled runner failure became background error: %q", got)
	}
}

func TestRunDismissRules(t *testing.T) {
	for _, mode := range []string{"stored", "managed", "unavailable", "busy", "stopping", "paused", "implement", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			d, store, p := runDaemon(t, nil)
			state := runrec.StateStopped
			if mode == "paused" || mode == "implement" {
				state = mode
			}
			var r *runrec.Run
			if mode != "unknown" {
				var err error
				r, err = store.Create(runrec.Record{ID: "dismiss", Project: p.Name, State: state})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "managed" || mode == "busy" || mode == "stopping" {
					d.runs.runs["dismiss"] = &managedRun{engine: engine.Load(d.runDeps(p), p, r), busy: mode == "busy", stopping: mode == "stopping"}
				} else if mode == "unavailable" {
					d.runs.runs["dismiss"] = &managedRun{failure: errors.New("unavailable")}
				}
			}
			result, err := d.handle(context.Background(), "run.dismiss", json.RawMessage("{\"run\":\"dismiss\"}"))
			if mode == "unknown" {
				var wire *ipc.Error
				if !errors.As(err, &wire) || wire.Code != CodeNotFound {
					t.Fatalf("unknown: %v", err)
				}
				return
			}
			if mode == "busy" || mode == "stopping" || mode == "paused" || mode == "implement" {
				if mode == "paused" || mode == "implement" {
					if !errors.Is(err, runrec.ErrNotStopped) {
						t.Fatalf("unstopped dismissal: %v", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "busy") {
					t.Fatalf("busy dismissal: %v", err)
				}
				opened, openErr := store.Open("dismiss")
				if openErr != nil || !opened.Snapshot().DismissedAt.IsZero() {
					t.Fatalf("changed record: %v", openErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			record := result.(runrec.Record)
			if record.DismissedAt.IsZero() {
				t.Fatal("missing dismissal")
			}
			if mode == "managed" {
				if err := r.Update(func(rec *runrec.Record) { rec.Reason = "later save" }); err != nil {
					t.Fatal(err)
				}
			}
			opened, err := store.Open("dismiss")
			if err != nil || opened.Snapshot().DismissedAt != record.DismissedAt {
				t.Fatalf("dismissal lost: %v", err)
			}
		})
	}
}

func TestBudgetPausedStatusIncludesRetryHint(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	reason := "token budget 9000 reached (9100 used)"
	_, err := store.Create(runrec.Record{ID: "budget", Item: "S65", Project: p.Name, State: runrec.StatePaused, Pending: &runrec.Pending{Kind: "budget", State: runrec.StateReview, Role: engine.RoleReviewer, Round: 1, Reason: reason}})
	if err != nil {
		t.Fatal(err)
	}
	runs := d.engineRuns(p.Name)
	if len(runs) != 1 || runs[0].Pending != "budget" || runs[0].Wait != reason+"; next: ork retry budget" {
		t.Fatalf("runs: %+v", runs)
	}
	b := request(t, d, "status", `{"project":"p"}`)
	if !strings.Contains(string(b), `"needs_you":1`) || !strings.Contains(string(b), `"pending":"budget"`) {
		t.Fatalf("status: %s", b)
	}
}

func TestStructuredQuestionWire(t *testing.T) {
	d, store, _ := runDaemon(t, nil)
	ask := &runrec.OwnerQuestion{Title: "Which storage?", Options: []runrec.Option{{Label: "local", Summary: "local storage"}, {Label: "remote", Summary: "remote storage"}}, Recommend: 1, Reason: "nearby"}
	_, err := store.Create(runrec.Record{ID: "structured", Project: "p", Item: "S111", State: runrec.StatePaused, Pending: &runrec.Pending{Kind: runrec.PendingQuestion, Question: "raw question", Ask: ask}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(d.engineRuns("p"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []EngineRun
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Question == nil || rows[0].Question.Title != ask.Title || rows[0].Question.Options[1].Summary != "remote storage" || rows[0].Question.Recommend != 1 || rows[0].Question.Reason != "nearby" || !strings.Contains(rows[0].Wait, "raw question") {
		t.Fatalf("wire: %s", b)
	}
}

func TestAnsweredRunWaitClearsPendingQuestion(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	_, err := store.Create(runrec.Record{ID: "answered", Project: p.Name, Item: "S127", State: runrec.StateImplement, Pending: &runrec.Pending{Kind: runrec.PendingQuestion, Question: "Old owner question", Reason: "Old question reason"}})
	if err != nil {
		t.Fatal(err)
	}
	rows := d.engineRuns(p.Name)
	if len(rows) != 1 || rows[0].Wait != "" || rows[0].Pending != "" || rows[0].Question != nil {
		t.Fatalf("answered run exposes pending question: %+v", rows)
	}
}

func TestRunPlanAcceptsAnArchitectOverride(t *testing.T) {
	for _, chain := range []string{`[{"vendor":"codex","model":"custom","effort":"high"},{"vendor":"claude","model":"backup","effort":"medium"}]`, `[]`, `null`} {
		t.Run(chain, func(t *testing.T) {
			d, _, _ := runDaemon(t, nil)
			plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"architect":`+chain+`}}`)
			var want []runrec.LineupEntry
			if err := json.Unmarshal([]byte(chain), &want); err != nil {
				t.Fatal(err)
			}
			if len(want) == 0 {
				if _, ok := plan.Lineup["architect"]; ok {
					t.Fatal("dropped architect remains in plan")
				}
				if _, ok := plan.Permissions["architect"]; ok {
					t.Fatal("dropped architect retains permissions")
				}
			} else if !reflect.DeepEqual(plan.Lineup["architect"], want) || !plan.Permissions["architect"].ReadOnly {
				t.Fatalf("architect override: %+v", plan)
			}
		})
	}
}

func TestRunPlanDropsARecommendedArchitect(t *testing.T) {
	roles := `{"roles":[{"role":"implementer","row":{"agent":"Claude Code","model":"sonnet","reasoning":"high"}},{"role":"reviewer","row":{"agent":"Claude Code","model":"opus","reasoning":"medium"}},{"role":"architect","row":{"agent":"Codex","model":"sol","reasoning":"high"}}]}`
	d, _, _ := runDaemon(t, nil, clitest.Route{Tool: "slicer", Args: []string{"relay-roles"}, Prefix: true, Stdout: roles})
	plan := launchPlan(t, d, `{"project":"p","item":"S1","lineup":{"architect":[]}}`)
	if _, ok := plan.Lineup["architect"]; ok {
		t.Fatal("architect not dropped")
	}
	if _, ok := plan.Permissions["architect"]; ok {
		t.Fatal("architect permission not dropped")
	}
}

func TestRunPlanValidatesArchitectEntries(t *testing.T) {
	for _, entry := range []string{`{"vendor":"unknown","model":"m","effort":"high"}`, `{"vendor":"codex","effort":"high"}`, `{"vendor":"codex","model":"m"}`} {
		d, _, _ := runDaemon(t, nil)
		_, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1","lineup":{"architect":[`+entry+`]}}`))
		if err == nil || !strings.Contains(err.Error(), "architect") || strings.Contains(err.Error(), "cannot override") {
			t.Fatalf("validation: %v", err)
		}
	}
}

func TestPlanAndEngineAgreeOnPaths(t *testing.T) {
	for _, tc := range []struct{ name, worktree, branch, wantWorktree, wantBranch string }{
		{"defaults", "", "", ".worktrees/S78", "S78"},
		{"id", ".worktrees/{id}", "{id}", ".worktrees/S78", "S78"},
		{"lower", ".worktrees/{id_lower}", "{id_lower}", ".worktrees/s78", "s78"},
		{"mixed", "Fixed/{id}/{id_lower}/{id_lower}", "feat/{id}/{id_lower}", "Fixed/S78/s78/s78", "feat/S78/s78"},
		{"absolute", "/absolute/{id_lower}/../{id}", "{id_lower}", "/absolute/S78", "s78"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, p := runDaemon(t, nil)
			p.WorktreePattern, p.BranchPattern = tc.worktree, tc.branch
			if err := registry.Save(d.registryPath(), &registry.File{Version: registry.Version, Projects: []registry.Project{p}}); err != nil {
				t.Fatal(err)
			}
			plan, err := d.planRun(context.Background(), RunParams{Project: p.Name, Item: "S78"})
			if err != nil {
				t.Fatal(err)
			}
			e, err := engine.New(context.Background(), *d.cfg.Engine, engine.Config{Project: p, Item: "S78", Lineup: plan.Lineup, Queued: true})
			if err != nil {
				t.Fatal(err)
			}
			want := tc.wantWorktree
			if !filepath.IsAbs(want) {
				want = filepath.Join(p.Path, want)
			}
			rec := e.Snapshot()
			if plan.Worktree != want || rec.Worktree != want || plan.Branch != tc.wantBranch || rec.Branch != tc.wantBranch {
				t.Errorf("plan %q %q; engine %q %q; want %q %q", plan.Worktree, plan.Branch, rec.Worktree, rec.Branch, want, tc.wantBranch)
			}
			runs, err := d.cfg.Engine.Store.List()
			if err != nil || len(runs) != 1 || rec.State != runrec.StateQueued {
				t.Fatalf("queued engine record: %v %v, state %s", runs, err, rec.State)
			}
		})
	}
}

func TestRunPlanEchoesStopAfter(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	for _, stop := range []string{"", "land", "commit"} {
		plan, err := d.planRun(context.Background(), RunParams{Project: "p", Item: "S1", StopAfter: stop})
		if err != nil {
			t.Fatal(err)
		}
		want := stop
		if want == "" {
			want = "land"
		}
		if plan.StopAfter != want {
			t.Fatalf("stop %q: got %q", stop, plan.StopAfter)
		}
	}
}
func TestRunPlanRefusesAnUnknownStopAfter(t *testing.T) {
	d, _, _ := runDaemon(t, nil)
	_, err := d.handle(context.Background(), MethodRunPlan, json.RawMessage(`{"project":"p","item":"S1","stop_after":"merge"}`))
	var rpc *ipc.Error
	if !errors.As(err, &rpc) || rpc.Code != "bad_params" {
		t.Fatalf("error: %v", err)
	}
}
func TestRunStartRefusesAChangedStopAfter(t *testing.T) {
	for _, stop := range []string{"land", "commit"} {
		t.Run(stop, func(t *testing.T) {
			d, store, _ := runDaemon(t, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
				<-ctx.Done()
				return engine.Result{}, ctx.Err()
			})
			p := RunParams{Project: "p", Item: "S1", StopAfter: stop}
			plan, err := d.planRun(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			p.Expected = plan
			if stop == "land" {
				p.StopAfter = "commit"
			} else {
				p.StopAfter = "land"
			}
			if _, err := d.startRun(context.Background(), p); err == nil || err.Error() != "plan changed; run the command again to confirm it" {
				t.Fatalf("error: %v", err)
			}
			if runs, err := store.List(); err != nil || len(runs) != 0 {
				t.Fatalf("runs %v, %v", runs, err)
			}
			p.StopAfter = stop
			rec, err := d.startRun(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			disk, err := store.Open(rec.ID)
			if err != nil {
				t.Fatal(err)
			}
			if rec.StopAfter != stop || disk.Snapshot().StopAfter != stop {
				t.Fatalf("stop point lost: %+v", rec)
			}
			rows := d.engineRuns("p")
			if len(rows) != 1 || rows[0].StopAfter != stop {
				t.Fatalf("wire stop point: %+v", rows)
			}
		})
	}
}

// The hung-role watchdog (S182): the daemon's runs use the fixed 30-minute
// limit, and an engine value set in the config is kept, including the
// negative value that turns the watchdog off.
func TestDaemonRunsUseTheThirtyMinuteWatchdog(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	if got := d.runDeps(p).HungAfter; got != 30*time.Minute {
		t.Errorf("default HungAfter %s, want 30m", got)
	}
	for _, set := range []time.Duration{7 * time.Second, -1} {
		d.runs.deps.HungAfter = set
		if got := d.runDeps(p).HungAfter; got != set {
			t.Errorf("HungAfter %s from the config became %s", set, got)
		}
	}
}

// The shared sleep detector counts sleep only once it has started, so the
// daemon must start it when it recovers its runs (S259). Without the start, the
// watchdog's own 30s look would be the only sample of the clock.
func TestRecoverRunsStartsTheWake(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	wake := d.runDeps(p).Wake
	if wake == nil {
		t.Fatal("the daemon built no Wake")
	}
	if wake.Started() {
		t.Fatal("the Wake started before recoverRuns")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := d.recoverRuns(ctx); err != nil {
		t.Fatal(err)
	}
	if !wake.Started() {
		t.Error("recoverRuns did not start the Wake")
	}
}

// A running role shows in the status rows as working: what runs, since when
// and the last output (S235). A run at rest has no working key, and every
// run of the daemon shares one sleep detector.
func TestStatusCarriesWorkingForARunningRun(t *testing.T) {
	started := make(chan struct{}, 1)
	d, store, p := runDaemon(t, func(ctx context.Context, req engine.Request) (engine.Result, error) {
		req.OnEvent()
		started <- struct{}{}
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	if d.runDeps(p).Wake == nil || d.runDeps(p).Wake != d.runDeps(registry.Project{Name: "other"}).Wake {
		t.Error("the runs do not share one Wake")
	}
	before := time.Now()
	request(t, d, MethodRunStart, `{"project":"p","item":"S1","lineup":{"implementer":[{"vendor":"claude","model":"sonnet","effort":"high"}]}}`)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the role never ran")
	}
	b, err := json.Marshal(d.engineRuns("p")[0])
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Working []struct {
			What       string    `json:"what"`
			Since      time.Time `json:"since"`
			LastOutput time.Time `json:"last_output"`
		} `json:"working"`
	}
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatal(err)
	}
	if len(row.Working) != 1 || row.Working[0].What != "implementer round 1" {
		t.Fatalf("working: %s", b)
	}
	w := row.Working[0]
	if w.Since.Before(before.Add(-time.Second)) || w.LastOutput.IsZero() || w.LastOutput.Before(w.Since) {
		t.Errorf("since %s, last output %s", w.Since, w.LastOutput)
	}

	// A run at rest has no working key.
	rest, err := store.Create(runrec.Record{ID: "rest", Project: "p", Item: "S2", State: runrec.StateStopped})
	if err != nil {
		t.Fatal(err)
	}
	d.runs.mu.Lock()
	d.runs.runs["rest"] = &managedRun{engine: engine.Load(d.runDeps(p), p, rest)}
	d.runs.mu.Unlock()
	for _, r := range d.engineRuns("p") {
		if r.ID != "rest" {
			continue
		}
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"working"`) {
			t.Errorf("a run at rest has a working key: %s", b)
		}
		return
	}
	t.Error("the run at rest is not listed")
}

func TestShapeRetryStatusFromRecord(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	retry := &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateImplement, Role: engine.RoleImplementer, Round: 1, Reason: "missing Tests", AutomaticShapeRetry: true}
	record, err := store.Create(runrec.Record{ID: "shape", Item: "S275", Project: p.Name, State: runrec.StateImplement, Pending: retry, Roles: []runrec.RoleRecord{{Role: engine.RoleImplementer, Round: 1, EndedAt: time.Now(), ShapeRetry: retry}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(d.engineRuns(p.Name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), `"shape_retry":true`) != 2 {
		t.Fatalf("retry missing from run and role status: %s", b)
	}
	if err := record.Update(func(rec *runrec.Record) { rec.Pending = nil }); err != nil {
		t.Fatal(err)
	}
	b, err = json.Marshal(d.engineRuns(p.Name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), `"shape_retry":true`) != 2 {
		t.Fatalf("role-only retry missing from status: %s", b)
	}
}

// A role that does not hold the single pending slot keeps its counted shape
// pause on its role record (S298); that row was not an automatic retry and is
// not labelled one.
func TestCountedShapeContextOnARoleIsNotShownAsAShapeRetry(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	counted := &runrec.Pending{Kind: runrec.PendingShape, State: runrec.StateReview, Role: engine.RoleReviewer, Round: 1, Reason: "x", ShapePauses: 1}
	if _, err := store.Create(runrec.Record{ID: "counted", Item: "S298", Project: p.Name, State: runrec.StatePaused, Pending: counted, Roles: []runrec.RoleRecord{{Role: engine.RoleReviewer, Round: 1, EndedAt: time.Now(), ShapeRetry: counted}}}); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(d.engineRuns(p.Name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"shape_retry":true`) {
		t.Fatalf("a counted shape context is shown as a shape retry: %s", b)
	}
}

// editProject changes the registry's first project.
func editProject(t *testing.T, d *Daemon, change func(*registry.Project)) {
	t.Helper()
	f, err := d.loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	change(&f.Projects[0])
	if err := registry.Save(d.cfg.RegistryPath, f); err != nil {
		t.Fatal(err)
	}
}

func TestRunPlanCarriesSetupWarnings(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	if err := os.MkdirAll(p.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Path, "pyproject.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	editProject(t, d, func(p *registry.Project) { p.CheckCommand = "" })
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	want := []string{
		"no check command: ork refuses to start a run (fix: ork projects set p --check CMD, or start with --allow-no-check)",
		"the implementer has the built-in Go allowlist, but p has no go.mod (fix: ork projects set p --preset python)",
	}
	if !reflect.DeepEqual(plan.Warnings, want) {
		t.Fatalf("warnings %q, want %q", plan.Warnings, want)
	}

	// A Go project with a check command has none, and the JSON has no key.
	if err := os.WriteFile(filepath.Join(p.Path, "go.mod"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	editProject(t, d, func(p *registry.Project) { p.CheckCommand = "go test ./..." })
	b := request(t, d, MethodRunPlan, `{"project":"p","item":"S1"}`)
	if strings.Contains(string(b), `"warnings"`) {
		t.Fatalf("a plan with no warnings has the key: %s", b)
	}
}

// driverPlanDaemon is a daemon whose project p is a real git repository with a
// go.mod and the default check command, using the real git for the driver
// lookup and no git config of the machine (S348).
func driverPlanDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	d, _, p := runDaemon(t, nil)
	setGitBin(d, "git")
	if err := os.MkdirAll(p.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Path, "go.mod"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	trackingGit(t, p.Path, "init", "-q")
	return d, p.Path
}

// driverLines are the plan's warnings that name merge drivers.
func driverLines(warnings []string) []string {
	var out []string
	for _, w := range warnings {
		if strings.HasPrefix(w, "merge drivers") {
			out = append(out, w)
		}
	}
	return out
}

// S348 (adj 480): a plan warns, before any slice starts, when the slicer merge
// drivers are not configured; the warning is ork doctor's text.
func TestRunPlanWarnsOnMissingMergeDrivers(t *testing.T) {
	d, dir := driverPlanDaemon(t)
	if err := os.MkdirAll(filepath.Join(dir, ".slicer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".slicer", ".gitattributes"), []byte("render/* merge=slicer-render\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	want := []string{engine.MissingDriversWarning([]string{"slicer-render"})}
	if got := driverLines(plan.Warnings); !reflect.DeepEqual(got, want) {
		t.Fatalf("driver warnings %q, want %q (all: %q)", got, want, plan.Warnings)
	}
	if !strings.Contains(want[0], "slicer-render") || len(plan.Warnings) != 1 {
		t.Fatalf("warnings %q, want only the drivers line naming slicer-render", plan.Warnings)
	}
	trackingGit(t, dir, "config", "merge.slicer-render.driver", "true")
	if plan := launchPlan(t, d, `{"project":"p","item":"S1"}`); len(plan.Warnings) != 0 {
		t.Errorf("warnings %q, want none once the driver is configured", plan.Warnings)
	}
}

func TestRunPlanWithoutSlicerAttributesHasNoDriverWarning(t *testing.T) {
	d, _ := driverPlanDaemon(t)
	if plan := launchPlan(t, d, `{"project":"p","item":"S1"}`); len(plan.Warnings) != 0 {
		t.Errorf("warnings %q, want none without .slicer/.gitattributes", plan.Warnings)
	}
}

func TestRunPlanDriverReadErrorIsAWarning(t *testing.T) {
	d, dir := driverPlanDaemon(t)
	if err := os.MkdirAll(filepath.Join(dir, ".slicer", ".gitattributes"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	got := driverLines(plan.Warnings)
	if len(got) != 1 || !strings.HasPrefix(got[0], "merge drivers: ") || strings.HasPrefix(got[0], "merge drivers missing") {
		t.Fatalf("driver warnings %q, want one `merge drivers: ERR`", got)
	}
	if len(plan.Warnings) != 1 {
		t.Errorf("warnings %q, want only the read error", plan.Warnings)
	}
}

// S295 (*proposed*): the plan says when the check command cannot run in the
// project's folder, so the launch shows it before the start.
func TestRunPlanWarnsWhenTheCheckCannotRun(t *testing.T) {
	d, _, p := runDaemon(t, nil)
	if err := os.MkdirAll(p.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Path, "go.mod"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	editProject(t, d, func(p *registry.Project) { p.CheckCommand = "go build App.slnx" })
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	if len(plan.Warnings) != 1 {
		t.Fatalf("warnings %q, want one", plan.Warnings)
	}
	for _, part := range []string{"check command cannot run in " + p.Path, "App.slnx", "(command: go build App.slnx)"} {
		if !strings.Contains(plan.Warnings[0], part) {
			t.Errorf("warning %q lacks %q", plan.Warnings[0], part)
		}
	}
	if n := strings.Count(plan.Warnings[0], p.Path); n != 1 {
		t.Errorf("warning %q names the directory %d times, want once", plan.Warnings[0], n)
	}
	// A check stored with --no-verify (no_verify_check) is not warned about.
	editProject(t, d, func(p *registry.Project) { p.NoVerifyCheck = true })
	if plan := launchPlan(t, d, `{"project":"p","item":"S1"}`); len(plan.Warnings) != 0 {
		t.Errorf("warnings %q, want none for a check stored with --no-verify", plan.Warnings)
	}
	editProject(t, d, func(p *registry.Project) { p.NoVerifyCheck = false })
	// The file is there: no warning.
	if err := os.WriteFile(filepath.Join(p.Path, "App.slnx"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if plan := launchPlan(t, d, `{"project":"p","item":"S1"}`); len(plan.Warnings) != 0 {
		t.Errorf("warnings %q, want none", plan.Warnings)
	}
}

// The setup pause for a check that cannot run says to stop first: a plain
// retry verifies the same command again (S295, review finding).
func TestPausedNextOfTheSetupPauseIsAStop(t *testing.T) {
	rec := runrec.Record{ID: "r1", State: runrec.StatePaused, Pending: &runrec.Pending{
		Kind: runrec.PendingError, State: runrec.StateImplement,
		Reason: engine.CheckCannotRunPrefix + "/w: \"App.slnx\" does not exist (command: go build App.slnx); to fix the check: ork stop r1, ork projects set p --check CMD, then ork retry r1"}}
	if got := pausedNext(rec); got != "ork stop r1" {
		t.Errorf("next %q, want ork stop r1", got)
	}
	// Another error pause keeps the retry.
	rec.Pending.Reason = "check command: sh went away"
	if got := pausedNext(rec); got != "ork retry r1" {
		t.Errorf("next %q, want ork retry r1", got)
	}
}

// S246: the fix of a check command that came from slicer is slicer's config,
// not `ork projects set --check`, which writes the registry that slicer's value
// overrides (adj 367). The round check stays a registry setting.
func TestRunPlanCannotRunWarningNamesWhereToFixIt(t *testing.T) {
	for _, tc := range []struct {
		name, slicer, round string
		want, notWant       string
	}{
		{"slicer check", "go build App.slnx", "", "(fix: slicer config check_command CMD --root ", "ork projects set"},
		{"registry check", "", "", "(fix: ork projects set p --check CMD)", "slicer config"},
		{"round check", "go version", "go build App.slnx", "(fix: ork projects set p --round-check CMD)", "slicer config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			d, _, p := runDaemonIn(t, root, nil, slicerCheckRoute(filepath.Join(root, "p"), tc.slicer))
			if err := os.MkdirAll(p.Path, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p.Path, "go.mod"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			editProject(t, d, func(p *registry.Project) {
				// The registry's check is the broken one unless slicer or the round check is.
				p.CheckCommand, p.RoundCheckCommand = "go build App.slnx", tc.round
				if tc.slicer != "" || tc.round != "" {
					p.CheckCommand = "go version"
				}
			})
			plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
			if len(plan.Warnings) != 1 {
				t.Fatalf("warnings %q, want one", plan.Warnings)
			}
			if w := plan.Warnings[0]; !strings.Contains(w, tc.want) || strings.Contains(w, tc.notWant) {
				t.Fatalf("warning %q, want it to contain %q and not %q", w, tc.want, tc.notWant)
			}
		})
	}
}

func TestRunStartAcceptsAPlanWithWarnings(t *testing.T) {
	d, _, _ := runDaemon(t, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	// The project directory has no go.mod, so the plan warns of the Go default.
	plan := launchPlan(t, d, `{"project":"p","item":"S1"}`)
	if len(plan.Warnings) != 1 {
		t.Fatalf("warnings: %q", plan.Warnings)
	}
	shown, _ := json.Marshal(plan)
	var rec runrec.Record
	if err := json.Unmarshal(request(t, d, MethodRunStart, `{"project":"p","item":"S1","expected":`+string(shown)+`}`), &rec); err != nil || rec.ID == "" {
		t.Fatalf("start with a warned plan: %+v, %v", rec, err)
	}
}

// S246: slicer's check_command counts where the start refuses a project with no
// check command, in a run start and in a wave start.
func TestStartsAcceptASlicerCheckCommandWhenTheRegistryHasNone(t *testing.T) {
	for _, method := range []string{MethodRunStart, MethodWaveStart} {
		t.Run(method, func(t *testing.T) {
			root := t.TempDir()
			d, store, _ := runDaemonIn(t, root, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
				<-ctx.Done()
				return engine.Result{}, ctx.Err()
			}, slicerCheckRoute(filepath.Join(root, "p"), "make test"))
			editProject(t, d, func(p *registry.Project) { p.CheckCommand = "" })
			raw := `{"project":"p","item":"S1"}`
			if method == MethodWaveStart {
				raw = `{"items":[` + raw + `]}`
			}
			if _, err := d.handle(context.Background(), method, json.RawMessage(raw)); err != nil {
				t.Fatalf("%s with slicer's check_command: %v", method, err)
			}
			if runs, err := store.List(); err != nil || len(runs) != 1 {
				t.Fatalf("runs: %v %v", runs, err)
			}
		})
	}
}

// S246: a stopped run that is retried, held by the manager or not, runs slicer's
// check command, not the registry's.
func TestRunRetryOfAStoppedRunUsesSlicersCheckCommand(t *testing.T) {
	for _, managed := range []bool{true, false} {
		t.Run(fmt.Sprintf("managed=%v", managed), func(t *testing.T) {
			root := t.TempDir()
			rig := stoppedDaemonIn(t, root, retryRunner, managed, stoppedDiff, nil, slicerCheckRoute(filepath.Join(root, "p"), "make test"))
			request(t, rig.d, "run.retry", `{"run":"r1"}`)
			if checks := rig.ranChecks(); len(checks) != 1 || checks[0] != "make test" {
				t.Fatalf("checks after the retry: %v", checks)
			}
		})
	}
}

// S246: a retry reads slicer's check command with no manager lock held, so a
// slow `slicer config` does not stall status, starts or queue admission.
func TestRunRetryReadsSlicersCheckCommandWithoutTheManagerLock(t *testing.T) {
	rig, started, release := blockedCheckRead(t)
	done := make(chan error, 1)
	go func() {
		_, err := rig.d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
		done <- err
	}()
	waitForFile(t, started, release)
	locked := make(chan struct{})
	go func() {
		rig.d.runs.mu.Lock()
		rig.d.runs.mu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Error("the manager lock was held while slicer config check_command was blocked")
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("run.retry: %v", err)
	}
	if checks := rig.ranChecks(); len(checks) != 1 || checks[0] != "make test" {
		t.Fatalf("checks after the retry: %v", checks)
	}
}

// A project edited while the retry reads slicer's command is not resumed with
// the copy that was read: the retry is refused and says why.
func TestRunRetryRefusedWhenTheProjectChangesDuringTheRead(t *testing.T) {
	rig, started, release := blockedCheckRead(t)
	done := make(chan error, 1)
	go func() {
		_, err := rig.d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1"}`))
		done <- err
	}()
	waitForFile(t, started, release)
	editProject(t, rig.d, func(p *registry.Project) { p.CheckCommand = "go vet ./..." })
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := <-done
	if re := ipc.ErrorOf(err); err == nil || re.Code != CodeRefused || !strings.Contains(re.Message, "changed while the retry was being prepared") {
		t.Fatalf("run.retry: %v", err)
	}
	if checks := rig.ranChecks(); len(checks) != 0 {
		t.Fatalf("a refused retry ran checks: %v", checks)
	}
}

// S431 (*proposed*; adj 464): while the daemon drains for a rebuild, a run that
// reaches a role start waits and says why; after the restart the daemon that
// recovers the run starts that role.
func TestAutoRebuildHoldsNewRoles(t *testing.T) {
	var mu sync.Mutex
	var started []string
	runner := func(ctx context.Context, r engine.Request) (engine.Result, error) {
		mu.Lock()
		started = append(started, r.RunID+" "+r.Role)
		mu.Unlock()
		return retryRunner(ctx, r)
	}
	starts := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), started...)
	}
	d, store, p, _ := rebuildDaemon(t, true, runner)
	pausedRun(t, d, store, p, "r1")
	d.runs.deps.Hold.Set(true) // what a drain sets
	request(t, d, "run.retry", `{"run":"r1","no_wait":true}`)
	waitFor(t, "the run waits for the rebuild", func() bool { return engineRunWait(d, "r1") == "waiting for daemon rebuild" })
	time.Sleep(50 * time.Millisecond)
	if got := starts(); len(got) != 0 {
		t.Fatalf("roles started while the daemon drains: %v", got)
	}

	// The daemon restarts: the old one ends, a new one recovers the run, and its
	// role starts (the new daemon holds nothing).
	if !d.exitForRebuild(func(context.Context) error { return nil }) {
		t.Fatal("rebuild exit refused the held run")
	}
	d.closeRuns()
	deps := d.runs.deps
	deps.Hold, deps.Wake, deps.Vendors = nil, nil, nil
	d2 := New(Config{Paths: d.cfg.Paths, RegistryPath: d.cfg.RegistryPath, Source: &stubSource{}, Engine: &deps})
	t.Cleanup(d2.cache.Close)
	t.Cleanup(d2.closeRuns)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := d2.recoverRuns(ctx); err != nil {
		t.Fatal(err)
	}
	waitState(t, store, "r1", runrec.StateAwaitingApproval)
	waitIdle(t, d2, "r1")
	if got := starts(); len(got) == 0 || got[0] != "r1 implementer" {
		t.Fatalf("roles started after the restart: %v, want r1's implementer first", got)
	}
}

// waitForFile waits for the marker the blocked read leaves; on a timeout it
// releases the read so no goroutine stays blocked.
func waitForFile(t *testing.T, marker, release string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			os.WriteFile(release, nil, 0o644)
			t.Fatal("the retry never read check_command")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// blockedCheckRead is a stopped-run rig whose slicer blocks the read of
// check_command until the release file exists, and leaves the started file
// once it is blocked there. Every other slicer call goes to the fake slicer.
func blockedCheckRead(t *testing.T) (rig *stoppedRig, started, release string) {
	t.Helper()
	root := t.TempDir()
	rig = stoppedDaemonIn(t, root, retryRunner, false, stoppedDiff, nil, slicerCheckRoute(filepath.Join(root, "p"), "make test"))
	dir := t.TempDir()
	started, release = filepath.Join(dir, "started"), filepath.Join(dir, "release")
	script := "#!/bin/sh\nif [ \"$1\" = config ] && [ \"$2\" = check_command ]; then\n  : > '" + started + "'\n  while [ ! -e '" + release + "' ]; do sleep 0.02; done\nfi\nexec '" + rig.d.runs.deps.Slicer.Bin + "' \"$@\"\n"
	wrapper := filepath.Join(dir, "slicer")
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	rig.d.runs.deps.Slicer = slicercli.Client{Bin: wrapper}
	return rig, started, release
}

func TestRunStartRefusesAProjectWithoutACheckCommand(t *testing.T) {
	d, store, _ := runDaemon(t, func(ctx context.Context, _ engine.Request) (engine.Result, error) {
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	})
	editProject(t, d, func(p *registry.Project) { p.CheckCommand = "" })
	_, err := d.handle(context.Background(), MethodRunStart, json.RawMessage(`{"project":"p","item":"S1"}`))
	re := ipc.ErrorOf(err)
	want := "project p: no check command: ork refuses to start a run (fix: ork projects set p --check CMD, or start with --allow-no-check)"
	if err == nil || re.Code != CodeRefused || re.Message != want {
		t.Fatalf("got %v, want %s: %s", err, CodeRefused, want)
	}
	if runs, err := store.List(); err != nil || len(runs) != 0 {
		t.Fatalf("a refusal created a run: %v %v", runs, err)
	}
	// The plan is still shown: the owner sees `check: none` before the refusal.
	if plan := launchPlan(t, d, `{"project":"p","item":"S1"}`); plan.CheckCommand != "" {
		t.Fatalf("plan: %+v", plan)
	}
	// --allow-no-check starts it.
	var rec runrec.Record
	if err := json.Unmarshal(request(t, d, MethodRunStart, `{"project":"p","item":"S1","allow_no_check":true}`), &rec); err != nil || rec.ID == "" {
		t.Fatalf("allow_no_check: %+v, %v", rec, err)
	}
}

// A committed run that stopped in land is retried back into land. The main
// checkout's symbolic-ref blocks until release, so the reply is the record
// as retry sets it, before landing goes on.
func TestDaemonRetryRelandsCommittedRun(t *testing.T) {
	rig := stoppedDaemon(t, nil, false, stoppedDiff, func(r *runrec.Record) {
		r.StoppedFrom = runrec.StateLand
		r.Commit = &runrec.Commit{SHA: "abc123", Title: "S1: slice"}
		r.LandFixes = 1
		r.Reason = "land blocked: checks red after merging main"
	})
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	main := filepath.Join(rig.root, "p")
	orig := rig.d.runs.deps.GitBin
	script := "#!/bin/sh\nif [ \"$1\" = -C ] && [ \"$2\" = '" + main + "' ] && [ \"$3\" = symbolic-ref ]; then\n  while [ ! -e '" + release + "' ]; do sleep 0.02; done\n  echo 'not on a branch' >&2\n  exit 1\nfi\nexec '" + orig + "' \"$@\"\n"
	wrapper := filepath.Join(dir, "git")
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	rig.d.runs.deps.GitBin = wrapper

	v, err := rig.d.handle(context.Background(), "run.retry", json.RawMessage(`{"run":"r1","no_wait":true}`))
	if wire := ipc.ErrorOf(err); err != nil {
		t.Fatalf("retry refused: code %s: %v", wire.Code, err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeRecord(t, b)
	if got.State != runrec.StateLand || got.LandFixes != 0 || !got.LandRecheck {
		t.Fatalf("state %s land_fixes %d recheck %v, want land, 0, true", got.State, got.LandFixes, got.LandRecheck)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, rig.d, "r1")
}
