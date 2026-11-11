// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
	"github.com/squalor-xyz/ork/internal/source"
)

func (d *Daemon) registryPath() string {
	if d.cfg.RegistryPath != "" {
		return d.cfg.RegistryPath
	}
	p, _ := registry.DefaultPath()
	return p
}

// loadRegistry reads the registry without creating it.
func (d *Daemon) loadRegistry() (*registry.File, error) {
	f, err := registry.Load(d.registryPath())
	if err != nil {
		return nil, &ipc.Error{Code: CodeRegistry, Message: err.Error()}
	}
	return f, nil
}

func badParams(err error) error {
	return &ipc.Error{Code: CodeBadParams, Message: "bad params: " + err.Error()}
}

// decode reads params into v, refusing keys v does not name. Missing params
// leave v as it is.
func decode(params json.RawMessage, v any) error {
	if len(params) == 0 || string(params) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(params))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return badParams(err)
	}
	return nil
}

func (d *Daemon) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case MethodRelayAnswer:
		var p RelayAnswerParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.relayAnswer(ctx, p)
	case MethodAdjAnswer:
		var p AdjAnswerParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.adjAnswer(ctx, p)
	case MethodAdjAsk:
		var p AdjAskParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.adjAsk(ctx, p)
	case MethodWaveStart:
		var p WaveStartParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.startWave(ctx, p)
	case MethodWaveDefine:
		var p WaveDefineParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.waveDefine(ctx, p)
	case MethodWaveList:
		var p WaveListParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.waveList(ctx, p)
	case MethodWaveRm:
		var p WaveRmParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.waveRm(p)
	case MethodWaveStop:
		var p WaveAction
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.stopWave(ctx, p)
	case MethodWaveSuggest:
		var p WaveSuggestParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.waveSuggest(ctx, p)
	case MethodWaveOverlap:
		var p WaveOverlapParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.waveOverlap(ctx, p)
	case MethodRolesMatrix:
		var p RolesMatrixParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.rolesMatrix(ctx, p)
	case MethodMatrixEdit:
		var p MatrixEditParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.matrixEdit(ctx, p)
	case MethodSpecCheck:
		var p SpecCheckParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.specCheck(ctx, p)
	case MethodSpecAnswer:
		var p SpecAnswerParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.specAnswer(ctx, p)
	case MethodDaemonInfo:
		return DaemonInfoResult{BuildID: d.cfg.BuildID}, nil
	case MethodDaemonStop:
		var p StopParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.stopDaemon(p)
	case MethodRunCleanup:
		var p RunAction
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return nil, d.cleanupRun(ctx, p)
	case MethodRunLandCheck, MethodRunDismiss, MethodRunPlan, MethodRunStart, MethodRunApprove, MethodRunStop, MethodRunAnswer, MethodRunRetry, MethodRunFile, MethodRunFix, MethodRunCheck, MethodRunRender, MethodRunReport, MethodRunShow, MethodRunEndLedger:
		return d.handleRun(ctx, method, params)
	case MethodStatus:
		var p StatusParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.status(ctx, p)
	case MethodItemNew:
		var p ItemNewParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.itemNew(ctx, p)
	case MethodItemPark, MethodItemUnpark:
		var p ItemParkParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		if method == MethodItemPark {
			return d.itemPark(ctx, p)
		}
		return d.itemUnpark(ctx, p)
	case MethodItemMove:
		var p ItemMoveParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.itemMove(ctx, p)
	case MethodItemNote:
		var p ItemNoteParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.itemNote(ctx, p)
	case MethodItemShow:
		var p ItemShowParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.itemShow(ctx, p)
	case MethodSnapshot:
		var p SnapshotParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.snapshot(ctx, p)
	case MethodSubscribe:
		var p SubscribeParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.subscribe(ctx, p)
	case MethodRefresh:
		var p RefreshParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.refresh(ctx, p)
	case MethodProjectsList:
		f, err := d.loadRegistry()
		if err != nil {
			return nil, err
		}
		return ProjectsResult{Projects: nonNil(f.Projects), Unavailable: d.unavailableProjects(f)}, nil
	case MethodProjectsAdd:
		var p AddProjectParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.addProject(p)
	case MethodProjectsRm:
		var p RemoveProjectParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return nil, d.removeProject(p.Name)
	case MethodProjectsSet:
		var p SetProjectParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.setProject(ctx, p)
	case MethodVendorsList:
		return d.listVendors()
	case MethodVendorsSet:
		var p SetVendorParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.setVendor(p)
	case MethodSettingsGet:
		return d.getSettings()
	case MethodSettingsSet:
		var p SetSettingsParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.setSettings(p)
	}
	return nil, &ipc.Error{Code: CodeUnknownMethod, Message: "unknown method " + method}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// NoProjectsGuide tells an owner how to register the first project.
const NoProjectsGuide = "no projects: run ork projects add PATH (or run ork to add one in the TUI)"

func notFound(name string, f *registry.File) error {
	if f != nil && len(f.Projects) == 0 {
		return &ipc.Error{Code: CodeNotFound, Message: NoProjectsGuide}
	}
	return &ipc.Error{Code: CodeNotFound, Message: fmt.Sprintf("no project named %q", name)}
}

func find(f *registry.File, name string) (registry.Project, bool) {
	for _, p := range f.Projects {
		if p.Name == name {
			return p, true
		}
	}
	return registry.Project{}, false
}

func (d *Daemon) snapshot(ctx context.Context, p SnapshotParams) (*SnapshotResult, error) {
	f, err := d.loadRegistry()
	if err != nil {
		return nil, err
	}
	proj, ok := find(f, p.Project)
	if !ok {
		for _, group := range d.unavailableProjects(f) {
			if group.Name == p.Project {
				return &SnapshotResult{Unavailable: true, Project: registry.Project{Name: group.Name}, EngineRuns: group.EngineRuns}, nil
			}
		}
		return nil, notFound(p.Project, f)
	}
	s := d.snapshotOf(ctx, proj)
	if reader, ok := d.cfg.Source.(source.BoardReader); ok {
		s.BoardRuns = reader.BoardRuns(ctx, proj)
	}
	now := d.cfg.Clock.Now()
	selfHost, behindMain := d.projectBuild(ctx, proj)
	return &SnapshotResult{
		DaemonBuild:  d.cfg.BuildID,
		SelfHost:     selfHost,
		BehindMain:   behindMain,
		RolesRunning: d.cfg.WaveRunning() || d.activeRuns(),
		VendorsOut:   d.vendorsOut(),
		Project:      s.Project,
		EngineRuns:   s.EngineRuns,
		Items:        partOf(s.Items, now),
		Deps:         partOf(s.Deps, now),
		Goals:        partOf(s.Goals, now),
		Runs:         partOf(s.Runs, now),
		BoardRuns:    partOf(s.BoardRuns, now),
		Open:         partOf(s.Open, now),
		Decided:      partOf(s.Decided, now),
	}, nil
}

func (d *Daemon) status(ctx context.Context, p StatusParams) (*StatusResult, error) {
	f, err := d.loadRegistry()
	if err != nil {
		return nil, err
	}
	projects := f.Projects
	unavailable := d.unavailableProjects(f)
	if p.Project != "" {
		proj, ok := find(f, p.Project)
		if !ok {
			projects = nil
			found := false
			for _, group := range unavailable {
				if group.Name == p.Project {
					found = true
				}
			}
			if !found {
				return nil, notFound(p.Project, f)
			}
		} else {
			projects = []registry.Project{proj}
		}
	}
	snaps := make([]source.Snapshot, len(projects))
	var wg sync.WaitGroup
	for i, proj := range projects {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snaps[i] = d.snapshotOf(ctx, proj)
			if reader, ok := d.cfg.Source.(source.BoardReader); ok {
				snaps[i].BoardRuns = reader.BoardRuns(ctx, proj)
			}
		}()
	}
	wg.Wait()

	now := d.cfg.Clock.Now()
	out := &StatusResult{BuildID: d.cfg.BuildID, RolesRunning: d.cfg.WaveRunning() || d.activeRuns(), VendorsOut: d.vendorsOut(), Projects: make([]ProjectStatus, 0, len(projects))}
	for i, s := range snaps {
		ps := projectStatus(projects[i], s, now)
		ps.SelfHost, ps.BehindMain = d.projectBuild(ctx, projects[i])
		out.Projects = append(out.Projects, ps)
	}
	for _, group := range unavailable {
		if p.Project == "" || group.Name == p.Project {
			out.Projects = append(out.Projects, group)
		}
	}
	return out, nil
}

// unavailableProjects keeps display-only groups apart from registered projects.
func (d *Daemon) unavailableProjects(f *registry.File) []ProjectStatus {
	d.runs.mu.Lock()
	stored, err := d.runs.deps.Store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unavailable runs: %v\n", err)
	}
	names := map[string]bool{}
	for _, run := range stored {
		rec := run.Snapshot()
		if m := d.runs.runs[rec.ID]; m != nil && m.engine == nil && !runrec.IsFinal(rec.State) {
			if _, registered := find(f, rec.Project); !registered {
				names[rec.Project] = true
			}
		}
	}
	d.runs.mu.Unlock()
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var groups []ProjectStatus
	for _, name := range keys {
		var rows []EngineRun
		for _, row := range d.engineRuns(name) {
			if row.Unavailable {
				rows = append(rows, row)
			}
		}
		groups = append(groups, ProjectStatus{Name: name, Unavailable: true, NeedsYou: len(rows), EngineRuns: rows, ActiveRelays: []Relay{}})
	}
	return groups
}

func projectStatus(p registry.Project, s source.Snapshot, now time.Time) ProjectStatus {
	ps := ProjectStatus{
		BuildCommand: p.BuildCommand,
		Name:         p.Name,
		Path:         p.Path,
		Adj:          p.Adj,
		NeedsYou:     len(s.Open.Value),
		ActiveRelays: []Relay{},
		EngineRuns:   s.EngineRuns,
	}
	for _, r := range s.Open.Value {
		ps.Open = append(ps.Open, OpenRecord{ID: r.ID, Key: r.Key, Title: r.Title})
	}
	for _, r := range ps.EngineRuns {
		ps.NeedsYou += r.FindingNeeds(ps.EngineRuns...)
		if r.Section(ps.EngineRuns) == source.SectionNeeds {
			ps.NeedsYou++
		}
	}
	for _, it := range s.Items.Value {
		if slices.Contains(it.Fields.Flags, "needs-owner") {
			ps.Held = append(ps.Held, HeldItem{ID: it.ID, Title: it.Title})
			ps.NeedsYou++
		}
	}
	ps.Parts = map[string]PartState{
		"items": stateOf(s.Items, now),
		"goals": stateOf(s.Goals, now),
		"runs":  stateOf(s.Runs, now),
	}
	if p.Adj {
		ps.Parts["open"] = stateOf(s.Open, now)
		ps.Parts["decided"] = stateOf(s.Decided, now)
	}
	appendRelay := func(r slicercli.RelayRun) {
		relay := Relay{
			ID: r.ID, Items: nonNil(r.Items), State: r.State, Worktree: r.Worktree,
			BeganAt: r.BeganAt, UpdatedAt: r.UpdatedAt, Steps: r.Counts.Steps, Running: r.Counts.Running,
		}
		// Ledger order makes the working subagent win over its orchestrator.
		for _, step := range r.Steps {
			summary := RelayStep{Role: step.Role, Vendor: step.Vendor, Model: step.Model, Task: step.Task, Phase: step.Phase}
			switch step.Phase {
			case "start":
				relay.RunningStep = &summary
			case "fail":
				relay.FailedStep = &summary
			}
		}
		ps.ActiveRelays = append(ps.ActiveRelays, relay)
	}
	for _, r := range s.Runs.Value {
		appendRelay(r)
	}
	for _, r := range s.BoardRuns.Value {
		if r.State == "stale" {
			appendRelay(r)
		}
	}
	for _, part := range []struct {
		name string
		err  error
	}{
		{"items", s.Items.Err}, {"goals", s.Goals.Err}, {"runs", s.Runs.Err},
		{"open", s.Open.Err}, {"decided", s.Decided.Err},
	} {
		if part.err != nil {
			e := ipc.ErrorOf(part.err)
			ps.Errors = append(ps.Errors, PartError{Part: part.name, Code: e.Code, Message: e.Message})
		}
	}
	return ps
}

func (d *Daemon) addProject(p AddProjectParams) (registry.Project, error) {
	if !filepath.IsAbs(p.Path) {
		return registry.Project{}, &ipc.Error{Code: CodeInvalidPath, Message: fmt.Sprintf("path %q is not absolute", p.Path)}
	}
	path := filepath.Clean(p.Path)
	name := p.Name
	if name == "" {
		name = filepath.Base(path)
	}
	if name == string(filepath.Separator) {
		return registry.Project{}, &ipc.Error{Code: CodeInvalidPath, Message: fmt.Sprintf("path %q has no base name; pass a name", p.Path)}
	}
	if info, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return registry.Project{}, &ipc.Error{Code: CodeInvalidPath, Message: fmt.Sprintf("path %q does not exist", path)}
		}
		return registry.Project{}, &ipc.Error{Code: CodeInvalidPath, Message: err.Error()}
	} else if !info.IsDir() {
		return registry.Project{}, &ipc.Error{Code: CodeInvalidPath, Message: fmt.Sprintf("path %q is not a directory", path)}
	}
	proj := registry.Project{Name: name, Path: path}
	if info, err := os.Stat(filepath.Join(path, ".adjudicate.json")); err == nil && info.Mode().IsRegular() {
		proj.Adj = true
	}

	d.regMu.Lock()
	defer d.regMu.Unlock()
	f, err := d.loadRegistry()
	if err != nil {
		return registry.Project{}, err
	}
	if _, dup := find(f, name); dup {
		return registry.Project{}, &ipc.Error{Code: CodeDuplicateName, Message: fmt.Sprintf("a project named %q is already registered", name)}
	}
	f.Projects = append(f.Projects, proj)
	if err := registry.Save(d.registryPath(), f); err != nil {
		return registry.Project{}, &ipc.Error{Code: CodeRegistry, Message: err.Error()}
	}
	return proj, nil
}

func (d *Daemon) removeProject(name string) error {
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	d.regMu.Lock()
	defer d.regMu.Unlock()
	f, err := d.loadRegistry()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(f.Projects, func(p registry.Project) bool { return p.Name == name })
	if i < 0 {
		return notFound(name, f)
	}
	// Hold runs.mu through Save; the check sees starts that took it first.
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	ids, err := d.unfinishedRunsLocked(name)
	if err != nil {
		return ipc.ErrorOf(err)
	}
	if len(ids) != 0 {
		return &ipc.Error{Code: CodeProjectBusy, Message: fmt.Sprintf("project %s has unfinished runs: %s", name, strings.Join(ids, ", "))}
	}
	f.Projects = slices.Delete(f.Projects, i, i+1)
	if err := registry.Save(d.registryPath(), f); err != nil {
		return &ipc.Error{Code: CodeRegistry, Message: err.Error()}
	}
	d.cache.Forget(name)
	// The project is gone: so are its saved waves (S215, NW-Q6, owner
	// 2026-11-07), as are its spec check records. Both run; the first error is
	// returned.
	wavesErr := d.dropSavedWaves(name)
	if err := d.removeProjectSpecsLocked(name); err != nil {
		return err
	}
	return wavesErr
}

// setProject changes the optional registry fields p sets (S161, *proposed*).
// It validates every value before it takes regMu, and saves the registry once.
// While the project has unfinished runs it refuses a call that sets a field a
// running slice reads, and allows the cap, the budgets, the patterns and the
// bell (S265, S224, *proposed*).
func (d *Daemon) setProject(ctx context.Context, p SetProjectParams) (registry.Project, error) {
	bad := func(format string, args ...any) error {
		return &ipc.Error{Code: CodeBadParams, Message: fmt.Sprintf(format, args...)}
	}
	if p.Name == "" {
		return registry.Project{}, bad("projects.set needs a project name")
	}
	if p.SetsNothing() {
		return registry.Project{}, bad("projects.set sets no field")
	}
	if p.UIPassSkills != nil {
		if err := registry.ValidateUIPassSkills(*p.UIPassSkills); err != nil {
			return registry.Project{}, bad("%v", err)
		}
	}
	if p.SpecCheckRows != nil {
		if err := registry.ValidateSpecCheckRows(*p.SpecCheckRows); err != nil {
			return registry.Project{}, bad("%v", err)
		}
	}
	if p.UIDoc != nil {
		if err := registry.ValidateUIDoc(*p.UIDoc); err != nil {
			return registry.Project{}, bad("%v", err)
		}
	}
	if p.WorktreePattern != nil {
		if err := registry.ValidatePattern("worktree_pattern", *p.WorktreePattern); err != nil {
			return registry.Project{}, bad("%v", err)
		}
	}
	if p.BranchPattern != nil {
		if err := registry.ValidatePattern("branch_pattern", *p.BranchPattern); err != nil {
			return registry.Project{}, bad("%v", err)
		}
	}
	for _, n := range []struct {
		name string
		v    *int
	}{{"check_minutes", p.CheckMinutes}, {"cap", p.Cap}, {"budget_minutes", p.BudgetMinutes}, {"budget_tokens", p.BudgetTokens}} {
		if n.v != nil && *n.v < 0 {
			return registry.Project{}, bad("%s must not be negative, got %d", n.name, *n.v)
		}
	}

	if err := validateProjectExtras(p); err != nil {
		return registry.Project{}, err
	}

	d.regMu.Lock()
	defer d.regMu.Unlock()
	f, err := d.loadRegistry()
	if err != nil {
		return registry.Project{}, err
	}
	i := slices.IndexFunc(f.Projects, func(q registry.Project) bool { return q.Name == p.Name })
	if i < 0 {
		return registry.Project{}, notFound(p.Name, f)
	}
	// Hold runs.mu through Save, as removeProject does. startRun admits a run
	// under the same lock, so a start sees either the old entry before the
	// check above or the new one after Save. A start whose plan read the old
	// entry and that admits after Save refuses with "plan changed".
	d.runs.mu.Lock()
	defer d.runs.mu.Unlock()
	ids, err := d.unfinishedRunsLocked(p.Name)
	if err != nil {
		return registry.Project{}, ipc.ErrorOf(err)
	}
	if refused := p.fieldsReadByARun(); len(ids) != 0 && len(refused) != 0 {
		return registry.Project{}, &ipc.Error{Code: CodeProjectBusy, Message: busyMessage(p.Name, ids, refused)}
	}
	proj := &f.Projects[i]
	// The preset and allow_add read the check command this call saves.
	base := *proj
	if p.CheckCommand != nil {
		base.CheckCommand = *p.CheckCommand
	}
	// S285 (*proposed*): a round check command needs a check command, so the
	// check before the gate and the landing re-check always run a real one. The
	// rule holds for what the registry would hold after this call.
	if p.RoundCheckCommand != nil {
		base.RoundCheckCommand = *p.RoundCheckCommand
	}
	if base.RoundCheckCommand != "" && base.CheckCommand == "" {
		return registry.Project{}, bad("round_check_command needs a check_command: the check before the gate and the landing re-check run it; set --check with --round-check, or clear --round-check")
	}
	if !p.NoVerify {
		if refusal := verifyNewCheck(ctx, *proj, p); refusal != "" {
			return registry.Project{}, bad("%s", refusal)
		}
	}
	withExtras, err := withProjectExtras(base, p)
	if err != nil {
		return registry.Project{}, err
	}
	proj.EnvironmentLines, proj.Permissions, proj.VendorEnv = withExtras.EnvironmentLines, withExtras.Permissions, withExtras.VendorEnv
	if p.Push != nil {
		proj.Push = *p.Push
	}
	if p.Bell != nil {
		bell := *p.Bell
		proj.Bell = &bell
	}
	if p.AutoLand != nil {
		autoLand := *p.AutoLand
		proj.AutoLand = &autoLand
	}
	if p.UIPassSkills != nil {
		proj.UIPassSkills = append([]string(nil), (*p.UIPassSkills)...)
	}
	if p.SpecCheckRows != nil {
		proj.SpecCheckRows = append([]string(nil), (*p.SpecCheckRows)...)
	}
	if p.UIDoc != nil {
		proj.UIDoc = *p.UIDoc
		if proj.UIDoc != "" {
			proj.UIDoc = filepath.Clean(proj.UIDoc)
		}
	}
	if p.WorktreePattern != nil {
		proj.WorktreePattern = *p.WorktreePattern
	}
	if p.BranchPattern != nil {
		proj.BranchPattern = *p.BranchPattern
	}
	if p.CheckCommand != nil {
		proj.CheckCommand = *p.CheckCommand
	}
	if p.BuildCommand != nil {
		proj.BuildCommand = *p.BuildCommand
	}
	if p.RoundCheckCommand != nil {
		proj.RoundCheckCommand = *p.RoundCheckCommand
	}
	if p.CheckCommand != nil || p.RoundCheckCommand != nil {
		// S295 (*proposed*; owner decision 2026-11-10): --no-verify is
		// remembered with the commands it stored, and a change of either
		// command without it clears it.
		proj.NoVerifyCheck = p.NoVerify
	}
	if p.CheckMinutes != nil {
		proj.CheckMinutes = *p.CheckMinutes
	}
	if p.Cap != nil {
		proj.Cap = *p.Cap
	}
	if p.BudgetMinutes != nil {
		proj.Budgets.Minutes = *p.BudgetMinutes
	}
	if p.BudgetTokens != nil {
		proj.Budgets.Tokens = *p.BudgetTokens
	}
	if err := registry.Save(d.registryPath(), f); err != nil {
		return registry.Project{}, &ipc.Error{Code: CodeRegistry, Message: err.Error()}
	}
	if p.Cap != nil {
		// A raised cap is read at the next admission; start the queued runs that
		// now fit, as settings.set does (S265, *proposed*). A lowered cap stops nothing.
		d.runs.lastRegistry = f
		d.startQueuedLocked(f)
	}
	return *proj, nil
}

// verifyNewCheck is the static check (S295, *proposed*) of the check command
// and the round check command that p sets and that are not empty. It returns the
// refusal, or "" when they can run. The round check runs in the project's own
// folder of the worktree (S293), and the main checkout's equivalent of that
// folder is the project's path, not the top of its repository. A project path
// that is not a directory is left to ork doctor.
func verifyNewCheck(ctx context.Context, proj registry.Project, p SetProjectParams) string {
	if info, err := os.Stat(proj.Path); err != nil || !info.IsDir() {
		return ""
	}
	var problems []string
	for _, command := range []*string{p.CheckCommand, p.RoundCheckCommand} {
		if command != nil && *command != "" {
			problems = append(problems, engine.VerifyCheckIn(ctx, proj.Path, *command, engine.CheckDirLabel)...)
		}
	}
	if len(problems) == 0 {
		return ""
	}
	return fmt.Sprintf(engine.CheckCannotRunPrefix+"%s: %s; fix it, or store it anyway with --no-verify", proj.Path, strings.Join(problems, "; "))
}

// stateOf is one part's age and state, for a status.
func stateOf[T any](r source.Result[T], now time.Time) PartState {
	var st PartState
	if !r.ReadAt.IsZero() {
		st.AgeMs = now.Sub(r.ReadAt).Milliseconds()
	}
	st.Stale = r.Stale()
	if r.Err != nil {
		st.Error = ipc.ErrorOf(r.Err)
	}
	return st
}

// refresh reads every part of a project now, and returns once they are read.
func (d *Daemon) refresh(ctx context.Context, p RefreshParams) (any, error) {
	f, err := d.loadRegistry()
	if err != nil {
		return nil, err
	}
	proj, ok := find(f, p.Project)
	if !ok {
		return nil, notFound(p.Project, f)
	}
	return nil, d.cache.Refresh(ctx, proj, allParts...)
}

// busyMessage is the project_busy refusal of a projects.set call that sets
// fields a running slice reads (S265, *proposed*). It names the command line
// flags, since the command line is where the owner fixes the call.
func busyMessage(project string, runs, flags []string) string {
	list := strings.Join(flags, ", ")
	if n := len(flags); n > 1 {
		list = strings.Join(flags[:n-1], ", ") + " and " + flags[n-1]
	}
	pronoun := "it"
	if len(flags) > 1 {
		pronoun = "them"
	}
	return fmt.Sprintf("project %s has unfinished runs: %s; a running slice reads %s, so set %s after they finish or stop (--cap, --bell, the budgets and the worktree and branch patterns may change now)",
		project, strings.Join(runs, ", "), list, pronoun)
}
