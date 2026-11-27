// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

var renders = flag.String("renders", "", "write plain-text CLI views to this directory")

// renderFixture picks the fixtures -renders writes: a comma-separated list of
// names, each written as NAME.txt. With none, -renders writes the five default
// files (adj 542, ui.render-fixture-select).
var renderFixture = flag.String("render-fixture", "", "with -renders, write only these comma-separated fixtures, one NAME.txt each")

// renderView is one fixture: its file name and the real command printer's text.
type renderView struct {
	name    string
	text    func(t *testing.T) string
	general bool // one of the five files -renders writes with no -render-fixture
}

// renderViews lists every fixture, the five default views first.
func renderViews() []renderView {
	return []renderView{
		{name: "status", general: true, text: func(t *testing.T) string {
			status := statusRebuildFixture()
			return renderOK(t, &status, "status")
		}},
		{name: "status-summary", general: true, text: func(t *testing.T) string {
			return renderOK(t, summaryApp(t, summaryFixture(), `{"next":{"id":"S7","title":"First"},"blocked":[{"id":"S8","waiting_on":["S6","S5"]}]}`, `[{"id":"S2","title":"Second"},{"id":"S7","title":"First"}]`), "status", "--summary")
		}},
		{name: "status-summary-long", general: true, text: func(t *testing.T) string {
			return renderOK(t, summaryApp(t, summaryLongFixture(), `{"next":{"id":"S7","title":"Review the complete summary display and its detailed information before starting the next item in the current project"}}`, `[]`), "status", "--summary")
		}},
		{name: "show", general: true, text: func(t *testing.T) string {
			show := showDouble(pausedShow(), map[string]string{"reviewer-3-attempt-3.md": "PENDING REPORT BODY\nline two"})
			return renderOK(t, &App{Call: show.call}, "show", "r1")
		}},
		{name: "help", general: true, text: func(t *testing.T) string {
			return renderOK(t, &App{}, "help")
		}},
		{name: "projects-set", text: renderProjectsSet},
		{name: "projects-set-refusals", text: renderProjectsSetRefusals},
		{name: "wave-start", text: renderWaveStart},
		{name: "wave-start-finding", text: renderWaveStartFinding},
		{name: "matrix-copy-preview", text: func(t *testing.T) string { return renderMatrixCopy(t, true, false) }},
		{name: "matrix-copy-ask", text: func(t *testing.T) string { return renderMatrixCopy(t, false, false) }},
		{name: "matrix-copy-stale-preview", text: func(t *testing.T) string { return renderMatrixCopy(t, false, true) }},
		{name: "status-queued-held", text: func(t *testing.T) string {
			return renderOK(t, renderStatusApp(t, renderStatusQueuedHeldReply), "status")
		}},
		{name: "status-engine", text: func(t *testing.T) string {
			return renderOK(t, renderStatusApp(t, renderStatusEngineReply), "status")
		}},
		{name: "summary-real", text: func(t *testing.T) string {
			return renderOK(t, summaryApp(t, summaryRealFixture(), `{"next":{"id":"S499","title":"let TestRenderCLIViews render the projects set summary and run error texts from fixtures, so a UI pass can put them on the approval card"},"blocked":[{"id":"S507","waiting_on":["S499","S501"]}]}`, `[{"id":"S501","title":"Show the setup steps of a new project in the dashboard settings pane"},{"id":"S499","title":"let TestRenderCLIViews render the projects set summary and run error texts from fixtures, so a UI pass can put them on the approval card"}]`), "status", "--summary")
		}},
		{name: "projects-setup", text: renderProjectsSetup},
		{name: "fake-reply", text: renderFakeReply},
		{name: "wait-block", text: renderWaitBlock},
		{name: "wait-lines", text: renderWaitLines},
		{name: "wait-stop", text: renderWaitStop},
	}
}

// renderStep is one command of a fixture that shows several: the app that
// answers it and its arguments.
type renderStep struct {
	app  *App
	args []string
}

// renderSteps runs each step and joins what they print, each under a "$ ork
// ARGS" line that also names a non-zero exit. A step may exit non-zero: its
// stderr is part of the view. stdout comes before stderr.
func renderSteps(t *testing.T, steps ...renderStep) string {
	t.Helper()
	var b strings.Builder
	for i, step := range steps {
		if i > 0 {
			b.WriteString("\n")
		}
		code, out, errs := run(t, step.app, step.args...)
		words := make([]string, len(step.args))
		for j, arg := range step.args {
			words[j] = engine.ShellWord(arg)
		}
		fmt.Fprintf(&b, "$ ork %s", strings.Join(words, " "))
		if code != ExitOK {
			fmt.Fprintf(&b, " (exit %d)", code)
		}
		b.WriteString("\n" + out + errs)
	}
	return b.String()
}

// fakeReplyApp answers each daemon method from replies, a JSON text by method,
// or fails it with errs. A method in neither is a test bug and fails loudly. A
// new subcommand is rendered by adding its method and canned reply here.
func fakeReplyApp(replies map[string]string, errs map[string]error) *App {
	return &App{Now: func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) }, Call: func(_ context.Context, method string, _, result any) error {
		if err := errs[method]; err != nil {
			return err
		}
		reply, ok := replies[method]
		if !ok {
			return fmt.Errorf("fake reply: no reply for method %s", method)
		}
		return json.Unmarshal([]byte(reply), result)
	}}
}

// renderWaitBlock is the text block of ork wait on one run (S633, adj 629). Each
// step has its own daemon double, because a double serves one list of replies.
func renderWaitBlock(t *testing.T) string {
	question := waitPause("question", "area.topic")
	question.Pending.Ask = &runrec.OwnerQuestion{Title: "Which store should the cache use?"}
	question.Next = "ork answer r1 TEXT" // as pausedNext gives a question pause
	_, questionApp := newWaitFake(t, question)

	shape, shapeApp := newWaitFake(t, waitShow("implement"), waitPause("shape", ""))
	shape.notify = true

	// The run waits for a slot, so its old question is skipped; the wait stops on
	// the new question the run asks once the slot wait is over.
	newQuestion := runQuestion("r1", "S1", "new.question")
	newQuestion.Pending.Ask = &runrec.OwnerQuestion{Title: "Which store should the cache use now?"}
	slot, slotApp := multiFake(t, map[string][]daemon.ShowReply{
		"r1": {runQuestion("r1", "S1", "old.question"), newQuestion},
	})
	slot.slotWait = map[string]bool{"r1": true}
	slot.afterRun = func(n int) {
		if n >= 2 {
			slot.mu.Lock()
			delete(slot.slotWait, "r1")
			slot.mu.Unlock()
		}
	}
	return renderSteps(t,
		renderStep{app: questionApp, args: []string{"wait", "r1"}},
		renderStep{app: shapeApp, args: []string{"wait", "r1"}},
		renderStep{app: slotApp, args: []string{"wait", "r1", "--until", "question,stopped,landed"}},
	)
}

// renderWaitLines is ork wait on several runs: one line for each run (S633,
// adj 629).
func renderWaitLines(t *testing.T) string {
	_, changedApp := multiFake(t, map[string][]daemon.ShowReply{
		"r1": {runShow("r1", "S1", "implement"), runLanded("r1", "S1")},
		"r2": {runShow("r2", "S2", "implement"), runApproval("r2", "S2")},
	})
	_, questionApp := multiFake(t, map[string][]daemon.ShowReply{
		"r1": {runShow("r1", "S1", "implement"), runLanded("r1", "S1")},
		"r2": {runShow("r2", "S2", "implement"), runQuestion("r2", "S2", "cache.store")},
	})
	return renderSteps(t,
		renderStep{app: changedApp, args: []string{"wait", "r1", "r2", "--until", "landed,awaiting-approval"}},
		renderStep{app: questionApp, args: []string{"wait", "r1", "r2", "--until", "landed"}},
	)
}

// renderWaitStop is what ork wait prints when it stops with no state to show:
// a stage end, a timeout and the nothing-active messages (S633, adj 629).
func renderWaitStop(t *testing.T) string {
	_, stageApp := multiFake(t, map[string][]daemon.ShowReply{"r1": {stageReply(false, ""), stageReply(true, "clean")}})
	_, timeoutApp := newWaitFake(t, waitShow("implement"))
	_, committedApp := newWaitFake(t, waitShow("committed"))
	unknown, unknownApp := newWaitFake(t, waitShow("implement"))
	unknown.notFound = true
	idle, idleApp := newWaitFake(t, waitShow("implement"))
	idle.runs = map[string][]string{"p": {"r1:committed", "r2:stopped"}}
	_, finalApp := multiFake(t, map[string][]daemon.ShowReply{
		"r1": {runShow("r1", "S1", "stopped")},
		"r2": {runLanded("r2", "S2")},
	})
	return renderSteps(t,
		renderStep{app: stageApp, args: []string{"wait", "r1", "--stage"}},
		renderStep{app: timeoutApp, args: []string{"wait", "r1", "--timeout", "50ms"}},
		renderStep{app: committedApp, args: []string{"wait", "r1"}},
		renderStep{app: unknownApp, args: []string{"wait", "r9"}},
		renderStep{app: idleApp, args: []string{"wait"}},
		renderStep{app: finalApp, args: []string{"wait", "r1", "r2", "--until", "awaiting-approval"}},
	)
}

func renderProjectsSet(t *testing.T) string {
	bell := false
	updated := daemon.SetProjectResult{
		Project: registry.Project{Name: "demo", Path: "/home/jon/code/squalor-xyz/demo", Push: true, Bell: &bell, Cap: 3, UIDoc: "docs/ui.md",
			UIPassSkills: []string{"frontend-design", "ux-review"},
			Permissions:  map[string]registry.Permission{"implementer": {Allow: []string{"Bash(go test*)", "Bash(go vet*)", "Bash(gofmt *)"}}}},
		Added:         map[string][]string{"implementer": {"Bash(gofmt *)"}},
		AlreadyListed: map[string][]string{"implementer": {"Bash(go vet*)"}},
	}
	cleared := daemon.SetProjectResult{Project: registry.Project{Name: "demo", Path: "/home/jon/code/squalor-xyz/demo"}}
	reply := func(r daemon.SetProjectResult) *App {
		b, _ := json.Marshal(r)
		return fakeReplyApp(map[string]string{daemon.MethodProjectsSet: string(b)}, nil)
	}
	return renderSteps(t,
		renderStep{reply(updated), []string{"projects", "set", "demo", "--push", "on", "--bell", "off", "--cap", "3", "--ui-doc", "docs/ui.md", "--ui-pass-skills", "frontend-design,ux-review", "--preset", "go", "--allow", "implementer=Bash(gofmt *)"}},
		renderStep{reply(cleared), []string{"projects", "set", "demo", "--dry-run", "--ui-doc", "", "--ui-pass-skills", "", "--cap", "0"}},
	)
}

// renderProjectsSetRefusals shows the texts a projects set refusal and a run
// error print on stderr: the command's own usage errors, the daemon's refusals
// and the no-check refusal of ork run.
func renderProjectsSetRefusals(t *testing.T) string {
	refused := func(message string) *App {
		return fakeReplyApp(nil, map[string]error{daemon.MethodProjectsSet: &ipc.Error{Code: daemon.CodeRefused, Message: message}})
	}
	noProject := fakeReplyApp(nil, map[string]error{daemon.MethodProjectsSet: &ipc.Error{Code: daemon.CodeNotFound, Message: `no project "nope"`}})
	plan := daemon.RunPlan{Project: registry.Project{Name: "demo"}, Item: "S1", Worktree: "/home/jon/code/squalor-xyz/demo/.worktrees/S1", Branch: "S1", StopAfter: "", Lineup: map[string][]runrec.LineupEntry{
		"implementer": {{Vendor: "claude", Model: "claude-sonnet-5-5", Effort: "medium"}},
		"reviewer":    {{Vendor: "claude", Model: "claude-opus-5-5", Effort: "high"}},
		"gate":        {{Vendor: "claude", Model: "claude-opus-5-5", Effort: "high"}},
	}, Permissions: map[string]engine.Permission{
		"implementer": {Allow: []string{"Read", "Edit", "Write", "Bash(go test*)", "Bash(go vet*)"}},
		"reviewer":    {ReadOnly: true},
		"gate":        {ReadOnly: true},
	}}
	planJSON, _ := json.Marshal(plan)
	runNoCheck := fakeReplyApp(map[string]string{
		daemon.MethodRunPlan:      string(planJSON),
		daemon.MethodProjectsList: `{"projects":[{"name":"demo","path":"/home/jon/code/squalor-xyz/demo"}]}`,
		daemon.MethodItemShow:     `{"detail":{"slice":{"sections":[]}}}`,
	}, nil)
	runBusy := fakeReplyApp(nil, map[string]error{daemon.MethodRunPlan: &ipc.Error{Code: daemon.CodeProjectBusy, Message: "project demo has unfinished runs: ork-20261125-131038-69e9"}})
	return renderSteps(t,
		renderStep{&App{}, []string{"projects", "set", "demo"}},
		renderStep{&App{}, []string{"projects", "set", "demo", "--push", "maybe"}},
		renderStep{&App{}, []string{"projects", "set", "demo", "--cap", "many"}},
		renderStep{refused("round_check_command needs a check_command: the check before the gate and the landing re-check run it; set --check with --round-check, or clear --round-check"), []string{"projects", "set", "demo", "--round-check", "make fast"}},
		renderStep{noProject, []string{"projects", "set", "nope", "--push", "on"}},
		renderStep{runNoCheck, []string{"run", "demo", "S1", "--yes"}},
		renderStep{runBusy, []string{"run", "demo", "S1", "--yes"}},
	)
}

// renderWaveStart is the wave start of S337: run with no terminal to confirm
// on and no --yes, it prints the plans and stops before the spec check.
func renderWaveStart(t *testing.T) string {
	stub := &specStub{t: t, check: `{"items":[{"item":"S501","ready":true},{"item":"S502","ready":true}]}`}
	app := renderWaveApp(stub, nil)
	text := renderSteps(t, renderStep{app, []string{"wave", "start", "ork", "S501", "S502", "--well-specified", "yes", "--split", "no"}})
	if stub.called("spec.check") || stub.called("wave.start") {
		t.Fatalf("a wave start with no terminal and no --yes made calls %v", stub.calls)
	}
	return text
}

// renderWaveStartFinding is a wave start with --yes where the spec check holds
// S501 on a Check finding and S502 is ready: the view shows the held line and
// the run of S502 only.
func renderWaveStartFinding(t *testing.T) string {
	stub := &specStub{t: t, check: `{"items":[` +
		`{"item":"S501","ready":false,"findings":["Check entry \"q quit\": first word \"q\" is not a runnable command"]},` +
		`{"item":"S502","ready":true}]}`}
	app := renderWaveApp(stub, func(params any) any {
		var items []daemon.WaveItemResult
		for _, item := range params.(daemon.WaveStartParams).Items {
			if item.Item != "S502" {
				t.Fatalf("wave.start was asked to start %s, want S502 only", item.Item)
			}
			items = append(items, daemon.WaveItemResult{Item: item.Item, Run: &runrec.Record{ID: "ork-20261125-131040-71aa", State: runrec.StateQueued}})
		}
		return daemon.WaveResult{Wave: "wave-20261125-ab", Items: items}
	})
	text := renderSteps(t, renderStep{app, []string{"wave", "start", "ork", "S501", "S502", "--well-specified", "yes", "--split", "no", "--yes"}})
	if !stub.called("spec.check") || !stub.called("wave.start") {
		t.Fatalf("a wave start with --yes made calls %v", stub.calls)
	}
	return text
}

// renderWaveApp is the fake daemon of the wave start fixtures: it answers
// run.plan with a plan for the item, wave.start with start when it is set, and
// every other method with stub.
func renderWaveApp(stub *specStub, start func(params any) any) *App {
	plan := func(item string) daemon.RunPlan {
		return daemon.RunPlan{
			Project: registry.Project{Name: "ork"}, Item: item, Worktree: "/home/jon/code/squalor-xyz/ork/.worktrees/" + item, Branch: item,
			CheckCommand: "go vet ./... && go test ./...", CheckSource: "slicer",
			Lineup: map[string][]runrec.LineupEntry{
				"implementer": {{Vendor: "claude", Model: "claude-sonnet-5-5", Effort: "medium"}},
				"reviewer":    {{Vendor: "claude", Model: "claude-opus-5-5", Effort: "high"}},
				"gate":        {{Vendor: "claude", Model: "claude-opus-5-5", Effort: "high"}},
			},
			Permissions: map[string]engine.Permission{
				"implementer": {Allow: []string{"Read", "Edit", "Write", "Bash(go test*)", "Bash(go vet*)"}},
				"reviewer":    {ReadOnly: true},
				"gate":        {ReadOnly: true},
			},
		}
	}
	return &App{Input: strings.NewReader(""), Terminal: func() bool { return false }, Call: func(ctx context.Context, method string, params, result any) error {
		switch {
		case method == daemon.MethodRunPlan:
			stub.calls = append(stub.calls, method)
			*(result.(*daemon.RunPlan)) = plan(params.(daemon.RunParams).Item)
			return nil
		case method == "wave.start" && start != nil:
			stub.calls = append(stub.calls, method)
			b, err := json.Marshal(start(params))
			if err != nil {
				return err
			}
			return json.Unmarshal(b, result)
		}
		return stub.call(ctx, method, params, result)
	}}
}

// renderStatusApp answers a status with raw. It fixes the zone, so the clock
// times in the view do not depend on the machine.
func renderStatusApp(t *testing.T, raw string) *App {
	t.Helper()
	localZone(t)
	return fakeReplyApp(map[string]string{daemon.MethodStatus: raw}, nil)
}

// renderStatusQueuedHeldReply is a status with runs that wait to start (queued
// and paused for a free slot) and items the owner holds.
const renderStatusQueuedHeldReply = `{"build_id":"dev","projects":[{"name":"ork","path":"/home/jon/code/squalor-xyz/ork","needs_you":3,
"open":[{"id":542,"key":"ui.render-fixture-select","title":"How does a UI pass pick the CLI render fixtures?"}],
"held":[{"id":"S512","title":"Rewrite the launcher configuration so a self-host daemon keeps its build"}],
"spec_questions":[{"item":"S514","keys":["#1"],"list":"ork wave start ork S514 --questions"}],
"engine_runs":[
{"id":"ork-20261125-131038-69e9","item":"S499","state":"implement","wave":"wave-20261125-ab","updated_at":"2026-11-25T11:58:00Z"},
{"id":"ork-20261125-131040-71aa","item":"S510","state":"queued","wave":"wave-20261125-ab"},
{"id":"ork-20261125-131041-72bb","item":"S511","state":"queued","wave":"wave-20261125-ab"},
{"id":"ork-20261125-101500-5c1d","item":"S505","state":"paused","resume_waiting":true,"wait_started_at":"2026-11-25T10:15:00Z","wait":"waiting for a free slot to resume; next: ork retry ork-20261125-101500-5c1d"}]}]}`

// renderStatusEngineReply is a status with engine runs at several states, a
// multi-line wait, a denial, findings and a relay row.
const renderStatusEngineReply = `{"build_id":"dev","projects":[{"name":"ork","path":"/home/jon/code/squalor-xyz/ork","needs_you":4,
"active_relays":[{"id":"run-20261125-aa","items":["S261"],"state":"active","steps":5,"running":1,"running_step":{"role":"reviewer","vendor":"claude","model":"claude-opus-5-5","task":"S261 reviewer"},"failed_step":{"role":"implementer","task":"S261 implementer"}}],
"engine_runs":[
{"id":"ork-20261125-120000-aa01","item":"S507","state":"paused","wave":"wave-20261125-ab","wait_started_at":"2026-11-25T10:55:00Z","wait":"Which fixtures does S499 add, and under what names?\n1. confirm list: the eight named fixtures\n2. fewer: only the status fixtures\n3. other: say which; next: ork answer ork-20261125-120000-aa01 TEXT","denials":["Bash(rm -rf build)","Bash(curl https://example.com/install.sh)"]},
{"id":"ork-20261125-120500-bb02","item":"S508","state":"awaiting-approval","wait":"owner approval","wait_started_at":"2026-11-25T11:20:00Z","findings":[{"n":1,"kind":"owner","title":"Pick one of the two table layouts"},{"n":2,"kind":"later","title":"Wrap the long path column"}]},
{"id":"ork-20261125-121000-cc03","item":"S509","state":"checks","updated_at":"2026-11-25T11:57:00Z","last_check":{"round":2,"command":"go vet ./... && go test ./...","ok":false},"working":[{"what":"checks","since":"2026-11-25T11:57:00Z"}]},
{"id":"ork-20261125-121500-dd04","item":"S510","state":"implement","shape_retry":true,"error":"the implementer report had no Outcome line","working":[{"what":"implementer round 3","since":"2026-11-25T11:40:00Z","last_output":"2026-11-25T11:58:00Z"}]},
{"id":"ork-20261125-110000-ee05","item":"S511","state":"committed","findings":[{"n":1,"kind":"later","title":"Name the render fixtures in docs/dashboard.md"}]}]}]}`

// summaryRealFixture is summaryFixture with the ids, titles and paths of a
// real project, so a width problem shows (adj 517).
func summaryRealFixture() daemon.ProjectStatus {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	return daemon.ProjectStatus{Name: "ork", Path: "/home/jon/code/squalor-xyz/ork",
		Open:          []daemon.OpenRecord{{ID: 542, Key: "ui.render-fixture-select", Title: "How does a UI pass pick the CLI render fixtures?"}},
		Held:          []daemon.HeldItem{{ID: "S512", Title: "Rewrite the launcher configuration so a self-host daemon keeps its build"}},
		SpecQuestions: []daemon.SpecQuestions{{Item: "S514", Keys: []string{"render.fixture-names", "#2"}, List: "ork wave start ork S514 --questions"}},
		EngineRuns: []source.EngineRun{
			{ID: "ork-20261120-014727-adb2", Item: "S307", State: "implementing", Working: []source.EngineWork{{What: "implementer round 2", Since: now.Add(-5 * time.Minute)}}, Roles: []source.EngineRole{{Role: "implementer", Vendor: "claude", Round: 2, StartedAt: now.Add(-5 * time.Minute)}}},
			{ID: "ork-20261120-014730-c0de", Item: "S308", State: "awaiting-approval", Card: &runrec.ApprovalCard{HumanChecks: []runrec.HumanCheck{{Text: "Inspect the projects set summary on the approval card at 80 columns", State: runrec.CheckLater}}}},
			{ID: "ork-20261120-014735-f00d", Item: "S309", State: "paused"},
		}}
}

func renderProjectsSetup(t *testing.T) string {
	commands := map[string]string{"slicer-init": "slicer init", "relay-roles-init": "slicer relay-roles init --render", "adj-init": "adj init", "adj-off": "registry: adj off"}
	adj := map[string]*bool{"adj-init": new(bool), "adj-off": new(bool)}
	*adj["adj-init"] = true
	app := func(step string, done bool) *App {
		b, _ := json.Marshal(daemon.ProjectSetupResult{Command: commands[step], Done: done, Adj: adj[step]})
		return fakeReplyApp(map[string]string{daemon.MethodProjectSetup: string(b)}, nil)
	}
	var steps []renderStep
	for _, step := range []string{"slicer-init", "relay-roles-init", "adj-init", "adj-off"} {
		steps = append(steps, renderStep{app(step, false), []string{"projects", "setup", "demo", step, "--dry-run"}})
	}
	for _, step := range []string{"slicer-init", "adj-init", "adj-off"} {
		steps = append(steps, renderStep{app(step, true), []string{"projects", "setup", "demo", step}})
	}
	return renderSteps(t, steps...)
}

// renderFakeReply shows a command whose daemon reply is canned, the way a new
// subcommand renders with no overlay test: add the method and its reply to
// fakeReplyApp's table here.
func renderFakeReply(t *testing.T) string {
	app := fakeReplyApp(map[string]string{daemon.MethodProjectsShow: `{"project":{"name":"demo","path":"/home/jon/code/squalor-xyz/demo","adj":true,"push":true,"cap":2,"check_command":"go vet ./... && go test ./..."},` +
		`"global":{"global_cap":4,"effective_global_cap":4,"daemon_idle_minutes":0,"effective_daemon_idle_minutes":30},` +
		`"slicer":{"config":{"check_command":"go test ./...","implement_finish":"handoff"},"matrix":null,"errors":[{"part":"matrix","code":"unavailable","message":"slicer relay-roles show failed"}]},` +
		`"adj":{"enabled":true,"config_file":true}}`}, nil)
	return renderSteps(t, renderStep{app, []string{"projects", "show", "demo"}})
}

// renderOK runs one command that must exit 0 with an empty stderr and returns
// its stdout.
func renderOK(t *testing.T, app *App, args ...string) string {
	t.Helper()
	code, out, errs := run(t, app, args...)
	if code != ExitOK || errs != "" {
		t.Fatalf("ork %s: exit %d, stderr %q", strings.Join(args, " "), code, errs)
	}
	return out
}

func renderMatrixCopy(t *testing.T, dry, stale bool) string {
	t.Helper()
	a, b := wouldChange("adjudicate"), wouldChange("slicer")
	a.Warnings = []string{noAdapterWarn}
	preview := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{a, b}}
	written := daemon.MatrixCopyResult{From: "ork", Projects: []daemon.MatrixCopyProject{
		{Project: a.Project, Status: daemon.CopyChanged, Steps: a.Steps},
		{Project: b.Project, Status: daemon.CopyChanged, Steps: b.Steps},
	}}
	if stale {
		written.Partial = true
		written.Projects[0] = daemon.MatrixCopyProject{Project: a.Project, Status: daemon.CopyFailed, Error: &ipc.Error{Code: daemon.CodeStalePreview, Message: "the rows or rules the copy to adjudicate would write differ from the ones the preview showed: preview again, then copy"}}
	}
	var calls []daemon.MatrixCopyParams
	// Closed input shows each question on its own line, as readLine prints the
	// missing terminal newline on EOF. The refusal fixture writes with --yes.
	app := copyApp(t, &calls, preview, written, "")
	args := []string{"roles", "matrix", "copy", "ork", "--all"}
	if dry {
		args = append(args, "--dry-run")
	}
	if stale {
		args = append(args, "--yes")
	}
	code, out, errs := run(t, app, args...)
	want := ExitOK
	if stale {
		want = ExitFailure
	}
	if code != want || errs != "" {
		t.Fatalf("matrix copy render: exit %d, stderr %q", code, errs)
	}
	return out
}

// renderViewText returns the text of the named fixture.
func renderViewText(t *testing.T, name string) string {
	t.Helper()
	var names []string
	for _, view := range renderViews() {
		if view.name == name {
			return view.text(t)
		}
		names = append(names, view.name)
	}
	t.Fatalf("no render fixture %q; the fixtures are %s", name, strings.Join(names, ", "))
	return ""
}

// renderSelected returns the fixtures one render run writes: the ones named by
// -render-fixture, or the five default views when it is empty.
func renderSelected(t *testing.T, spec string) []string {
	t.Helper()
	if strings.TrimSpace(spec) == "" {
		var names []string
		for _, view := range renderViews() {
			if view.general {
				names = append(names, view.name)
			}
		}
		return names
	}
	var names []string
	for _, name := range strings.Split(spec, ",") {
		names = append(names, strings.TrimSpace(name))
	}
	return names
}

// TestRenderCLIViews runs the real command printers with existing CLI fixtures.
// With no flag it does no work and writes nothing.
func TestRenderCLIViews(t *testing.T) {
	if *renders == "" {
		return
	}
	for _, name := range renderSelected(t, *renderFixture) {
		text := renderViewText(t, name)
		if err := os.WriteFile(filepath.Join(*renders, name+".txt"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// renderFixtureNames are the fixtures that -render-fixture adds beside the five
// default files (adj 542, owner 2026-11-25).
var renderFixtureNames = []string{"projects-set", "projects-set-refusals", "wave-start", "wave-start-finding", "status-queued-held", "status-engine", "summary-real", "projects-setup", "fake-reply", "matrix-copy-preview", "matrix-copy-ask", "matrix-copy-stale-preview", "wait-block", "wait-lines", "wait-stop"}

func TestUIPassBriefListsEveryRenderFixture(t *testing.T) {
	brief, err := engine.BuildBrief(engine.BriefInput{
		Role: engine.RoleUI, SliceID: "S607", Worktree: "/worktree",
		TempDir: "/temp", ReportPath: "/temp/ui.md", SliceJSON: `{"id":"S607"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(brief, "The names are the five above (without .txt) and ")
	if !ok {
		t.Fatal("UI brief lacks fixture list opening")
	}
	list, _, ok := strings.Cut(rest, ". Copy the views")
	if !ok {
		t.Fatal("UI brief lacks fixture list ending")
	}
	names := make(map[string]bool)
	for _, part := range strings.Split(list, ", ") {
		for _, name := range strings.Split(strings.TrimPrefix(part, "and "), " and ") {
			names[name] = true
		}
	}
	for _, name := range renderFixtureNames {
		if !names[name] {
			t.Errorf("UI brief fixture list lacks %q", name)
		}
	}
}

func TestRenderFixtureDocsListEveryFixture(t *testing.T) {
	for _, doc := range []string{"guide.md", "design.md"} {
		t.Run(doc, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "docs", doc))
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range renderFixtureNames {
				t.Run(name, func(t *testing.T) {
					if !strings.Contains(string(data), "`"+name+"`") {
						t.Errorf("docs/%s lacks exact render fixture name %q", doc, name)
					}
				})
			}
		})
	}
}

func TestRenderCLIViewsWritesFiles(t *testing.T) {
	if flag.Lookup("renders") == nil {
		t.Fatal("missing -renders test flag")
	}
	if flag.Lookup("render-fixture") == nil {
		t.Fatal("missing -render-fixture test flag")
	}
	defaults := []string{"help.txt", "show.txt", "status-summary-long.txt", "status-summary.txt", "status.txt"}
	type renderCase struct {
		name    string
		args    []string
		want    []string
		failure string // text the failing run must print
	}
	cases := []renderCase{
		{name: "unset", want: []string{}},
		{name: "set", args: []string{"-renders"}, want: defaults},
		{name: "two", args: []string{"-renders", "-render-fixture", "wave-start,projects-set"}, want: []string{"projects-set.txt", "wave-start.txt"}},
		{name: "unknown", args: []string{"-renders", "-render-fixture", "nope"}, failure: `no render fixture "nope"`},
		{name: "fixture without renders", args: []string{"-render-fixture", "wave-start"}, want: []string{}},
	}
	for _, name := range renderFixtureNames {
		cases = append(cases, renderCase{name: name, args: []string{"-renders", "-render-fixture", name}, want: []string{name + ".txt"}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			args := []string{"-test.run=^TestRenderCLIViews$"}
			for i := 0; i < len(tc.args); i++ {
				args = append(args, tc.args[i])
				if tc.args[i] == "-renders" {
					args = append(args, dir)
				}
			}
			cmd := exec.CommandContext(ctx, os.Args[0], args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if tc.failure != "" {
				if err == nil || !strings.Contains(string(out), tc.failure) {
					t.Fatalf("render command: %v, want it to fail with %q\n%s", err, tc.failure, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("render command: %v\n%s", err, out)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("render files %q, want %q", names, tc.want)
			}
			for _, name := range names {
				text, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if strings.TrimSpace(string(text)) == "" {
					t.Errorf("%s is empty", name)
				}
				if strings.ContainsRune(string(text), '\x1b') {
					t.Errorf("%s contains ANSI escapes", name)
				}
				if name == "status-summary.txt" && !strings.Contains(string(text), "  S7 First\n") {
					t.Errorf("short summary used another fixture: %s", text)
				}
				if name == "status-summary-long.txt" && !strings.Contains(string(text), "  S7 Review the complete summary") {
					t.Errorf("long summary used another fixture: %s", text)
				}
			}
		})
	}
}

// assertRenderLine checks that one fixture's view has the literal line.
func assertRenderLine(t *testing.T, fixture, line string) {
	t.Helper()
	text := renderViewText(t, fixture)
	if !strings.Contains("\n"+text, "\n"+line+"\n") {
		t.Fatalf("%s lacks the line %q:\n%s", fixture, line, text)
	}
}

// The projects set summary line.
func TestRenderProjectsSetFixture(t *testing.T) {
	assertRenderLine(t, "projects-set", `updated demo: push=on, bell=off, ui_doc="docs/ui.md", ui_pass_skills="frontend-design,ux-review", cap=3, permissions=implementer: 1 pattern added, 1 already listed (preset go)`)
}

// The run error: the no-check refusal of ork run.
func TestRenderProjectsSetRefusalsFixture(t *testing.T) {
	assertRenderLine(t, "projects-set-refusals", "ork: project demo: no check command: ork refuses to start a run (fix: slicer config check_command CMD, ork projects set demo --check CMD, or start with --allow-no-check)")
}

// The S337 line a wave start prints with no terminal and no --yes.
func TestRenderWaveStartFixture(t *testing.T) {
	assertRenderLine(t, "wave-start", "not started: input is not a terminal to confirm on; rerun with --yes, or with --questions to see the spec questions")
}

// The held line of an item the spec check holds on a Check finding, and the
// started line of the item that stays in the wave.
func TestRenderWaveStartFindingFixture(t *testing.T) {
	assertRenderLine(t, "wave-start-finding", `held: S501: Check entry "q quit": first word "q" is not a runnable command`)
	text := renderViewText(t, "wave-start-finding")
	if !strings.Contains(text, "S502 ork-20261125-131040-71aa queued\n") {
		t.Errorf("wave-start-finding does not list S502 as started:\n%s", text)
	}
	if strings.Contains(text, "S501 ork-") {
		t.Errorf("wave-start-finding lists the held S501 as started:\n%s", text)
	}
	if strings.Contains(text, "Start this wave?") {
		t.Errorf("wave-start-finding has a confirm prompt:\n%s", text)
	}
}

// A queued run's row.
func TestRenderStatusQueuedHeldFixture(t *testing.T) {
	assertRenderLine(t, "status-queued-held", "  ork-20261125-131040-71aa S510 queued")
}

// The relay row of the engine fixture.
func TestRenderStatusEngineFixture(t *testing.T) {
	assertRenderLine(t, "status-engine", "  relay run-20261125-aa S261 active (5 steps, 1 running)")
}

// A held item with a real-length title, wrapped to its own line.
func TestRenderSummaryRealFixture(t *testing.T) {
	assertRenderLine(t, "summary-real", "  S512 Rewrite the launcher configuration so a self-host daemon keeps its build")
}

// The confirmation line of a projects setup step that ran.
func TestRenderProjectsSetupFixture(t *testing.T) {
	assertRenderLine(t, "projects-setup", "ran: adj init (adj on)")
}

// The "$ ork ARGS" header quotes an argument that has a space, is empty or has a
// shell metacharacter, so the line is the command that ran.
func TestRenderStepHeaderQuotesArguments(t *testing.T) {
	assertRenderLine(t, "projects-set-refusals", "$ ork projects set demo --round-check 'make fast' (exit 1)")
	assertRenderLine(t, "projects-set", "$ ork projects set demo --dry-run --ui-doc '' --ui-pass-skills '' --cap 0")
	assertRenderLine(t, "projects-set", "$ ork projects set demo --push on --bell off --cap 3 --ui-doc docs/ui.md --ui-pass-skills 'frontend-design,ux-review' --preset go --allow 'implementer=Bash(gofmt *)'")
}

// A line of projects show, which the fake daemon reply made.
func TestRenderFakeReplyFixture(t *testing.T) {
	assertRenderLine(t, "fake-reply", "unavailable  slicer relay-roles show failed (unavailable)")
}

// The text block of one run's wait: a question pause with its decision key, a
// shape pause with none, and the slot-wait run that stops on a new question.
func TestRenderWaitBlockFixture(t *testing.T) {
	assertRenderLine(t, "wait-block", "decision key: area.topic")
	text := renderViewText(t, "wait-block")
	for _, want := range []string{
		"\nr1 paused\nquestion pause, reviewer round 1\nquestion: Which store should the cache use?\ndecision key: area.topic\nnext: ork answer r1 TEXT\n",
		"\nr1 paused\nshape pause, reviewer round 1\nnext: ork retry r1\n",
		"\nquestion: Which store should the cache use now?\ndecision key: new.question\nnext: ork answer r1 TEXT\n",
	} {
		if !strings.Contains("\n"+text, want) {
			t.Errorf("wait-block lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "old.question") {
		t.Errorf("wait-block shows the skipped slot-wait question:\n%s", text)
	}
	if n := strings.Count(text, "decision key:"); n != 2 {
		t.Errorf("wait-block has %d decision key lines, want 2 (the shape pause has none):\n%s", n, text)
	}
}

// The one line of each run, and a question line with its key that exits 3.
func TestRenderWaitLinesFixture(t *testing.T) {
	assertRenderLine(t, "wait-lines", "r1 S1 landed")
	assertRenderLine(t, "wait-lines", "r2 S2 awaiting-approval next: ork approve r2")
	assertRenderLine(t, "wait-lines", "r2 S2 question key: cache.store next: ork answer r2 TEXT")
	text := renderViewText(t, "wait-lines")
	if !strings.Contains(text, "(exit 3)\n") {
		t.Errorf("wait-lines has no step that exits 3:\n%s", text)
	}
}

// The stage-end line, the timeout message and the nothing-active messages.
func TestRenderWaitStopFixture(t *testing.T) {
	assertRenderLine(t, "wait-stop", "ork: timed out after 50ms; run r1 is still implement")
	assertRenderLine(t, "wait-stop", "r1 S1 stage implementer round 1 ended clean")
	text := renderViewText(t, "wait-stop")
	for _, want := range []string{
		"ork: run r1 is committed, so nothing is active\n",
		"ork: no run r9: it is unknown to the daemon, so nothing is active\n",
		"ork: project p has no unfinished run, so nothing is active\n",
		"ork: nothing is active: r1 stopped, r2 landed\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("wait-stop lacks %q:\n%s", want, text)
		}
	}
}
