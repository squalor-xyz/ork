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

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
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
	case MethodRunDismiss, MethodRunPlan, MethodRunStart, MethodRunApprove, MethodRunStop, MethodRunAnswer, MethodRunRetry, MethodRunFile, MethodRunFix, MethodRunCheck, MethodRunRender, MethodRunReport, MethodRunEndLedger:
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
		return d.setProject(p)
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
	for _, r := range s.Runs.Value {
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
	return d.removeProjectSpecsLocked(name)
}

// setProject changes the optional registry fields p sets (S161, *proposed*).
// It validates every value before it takes regMu, refuses while the project
// has unfinished runs as removeProject does, and saves the registry once.
func (d *Daemon) setProject(p SetProjectParams) (registry.Project, error) {
	bad := func(format string, args ...any) error {
		return &ipc.Error{Code: CodeBadParams, Message: fmt.Sprintf(format, args...)}
	}
	if p.Name == "" {
		return registry.Project{}, bad("projects.set needs a project name")
	}
	if p.SetsNothing() {
		return registry.Project{}, bad("projects.set sets no field")
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
	if len(ids) != 0 {
		return registry.Project{}, &ipc.Error{Code: CodeProjectBusy, Message: fmt.Sprintf("project %s has unfinished runs: %s", p.Name, strings.Join(ids, ", "))}
	}
	proj := &f.Projects[i]
	withExtras, err := withProjectExtras(*proj, p)
	if err != nil {
		return registry.Project{}, err
	}
	proj.EnvironmentLines, proj.Permissions, proj.VendorEnv = withExtras.EnvironmentLines, withExtras.Permissions, withExtras.VendorEnv
	if p.Push != nil {
		proj.Push = *p.Push
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
	return *proj, nil
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
