// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/wavestore"
)

// Saved waves (S215, *proposed*). A saved wave is a name and the item ids of
// one project, kept in waves.json beside the registry. It holds no item status,
// title or spec: wave.list reads those from slicer on every call.

// Member statuses wave.list adds to slicer's own.
const (
	// WaveStatusMissing is a member that slicer's list no longer holds.
	WaveStatusMissing = "missing"
	// WaveStatusUnknown is a member of a wave whose project's statuses could not
	// be read (SavedWaveView.Error is set).
	WaveStatusUnknown = "unknown"
)

// waveWriteHook runs between the read and the write of a waves.json update,
// while waveMu is held. Tests set it to hold writers there.
var waveWriteHook func()

// SavedWave is a saved wave as the file holds it: the result of wave.define and
// wave.rm.
type SavedWave struct {
	Project   string    `json:"project"`
	Name      string    `json:"name"`
	Items     []string  `json:"items"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WaveDefineParams is a wave.define call. A name already saved for the project
// is refused with duplicate_name unless Replace is set (NW-Q3).
type WaveDefineParams struct {
	Project string   `json:"project"`
	Name    string   `json:"name"`
	Items   []string `json:"items"`
	Replace bool     `json:"replace,omitempty"`
}

// WaveListParams is a wave.list call. An empty Project lists every project.
type WaveListParams struct {
	Project string `json:"project,omitempty"`
}

// WaveRmParams is a wave.rm call.
type WaveRmParams struct {
	Project string `json:"project"`
	Name    string `json:"name"`
}

// WaveMember is one item of a saved wave with what slicer says of it now.
type WaveMember struct {
	Item   string `json:"item"`
	Status string `json:"status"`
	Title  string `json:"title"`
}

// SavedWaveView is a saved wave with its members' status and title. Error is
// set when slicer could not be read for the wave's project; the members then
// carry no status.
type SavedWaveView struct {
	Project   string       `json:"project"`
	Name      string       `json:"name"`
	Items     []WaveMember `json:"items"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Error     *ipc.Error   `json:"error,omitempty"`
}

// WaveListResult is the result of wave.list, in the order the waves were saved.
type WaveListResult struct {
	Waves []SavedWaveView `json:"waves"`
}

// SkippedItem is a member that a start leaves out, with its status.
type SkippedItem struct {
	Item   string `json:"item"`
	Status string `json:"status"`
}

// Startable is the one rule of what starting a saved wave starts (NW-Q4, owner
// 2026-11-07): the members whose status is neither done, retired nor missing,
// in saved order, and the skipped members with their status. It writes nothing
// back. A wave whose statuses could not be read starts nothing: every member
// is skipped with the status "unknown". Both results are never nil.
func (w SavedWaveView) Startable() (ids []string, skipped []SkippedItem) {
	ids, skipped = []string{}, []SkippedItem{}
	for _, m := range w.Items {
		switch {
		case w.Error != nil:
			skipped = append(skipped, SkippedItem{Item: m.Item, Status: WaveStatusUnknown})
		case m.Status == "done" || m.Status == "retired" || m.Status == WaveStatusMissing:
			skipped = append(skipped, SkippedItem{Item: m.Item, Status: m.Status})
		default:
			ids = append(ids, m.Item)
		}
	}
	return ids, skipped
}

func savedWaveOf(w wavestore.Wave) *SavedWave {
	return &SavedWave{Project: w.Project, Name: w.Name, Items: slices.Clone(w.Items), CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
}

// loadWaves reads waves.json from the registry's directory. The caller holds
// waveMu.
func (d *Daemon) loadWaves() (*wavestore.File, error) {
	f, err := wavestore.Load(wavestore.PathBeside(d.registryPath()))
	if err != nil {
		return nil, &ipc.Error{Code: CodeRegistry, Message: err.Error()}
	}
	return f, nil
}

// saveWaves writes waves.json. The caller holds waveMu.
func (d *Daemon) saveWaves(f *wavestore.File) error {
	if err := wavestore.Save(wavestore.PathBeside(d.registryPath()), f); err != nil {
		return &ipc.Error{Code: CodeRegistry, Message: err.Error()}
	}
	return nil
}

func noSavedWave(project, name string) error {
	return &ipc.Error{Code: CodeNotFound, Message: fmt.Sprintf("no saved wave %q for project %s", name, project)}
}

// waveDefine saves the items under the name for a registered project. The name
// and items follow wavestore's rules (NW-Q2) and every item must be one slicer
// lists with --all, so a done or retired item is known (NW-Q4).
func (d *Daemon) waveDefine(ctx context.Context, p WaveDefineParams) (*SavedWave, error) {
	if p.Project == "" {
		return nil, badParams(errors.New("wave.define needs a project"))
	}
	if err := wavestore.ValidateName(p.Name); err != nil {
		return nil, badParams(err)
	}
	if err := wavestore.ValidateItems(p.Items); err != nil {
		return nil, badParams(err)
	}
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	listed, err := d.runs.deps.Slicer.ListAll(ctx, proj.Path)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(listed))
	for _, item := range listed {
		known[item.ID] = true
	}
	for _, id := range p.Items {
		if !known[id] {
			return nil, badParams(fmt.Errorf("slicer does not know item %s in project %s", id, p.Project))
		}
	}
	d.waveMu.Lock()
	defer d.waveMu.Unlock()
	// A projects rm since the check above has saved the registry and now waits
	// for waveMu to drop the project's waves; do not save one after it.
	if _, err := d.registered(p.Project); err != nil {
		return nil, err
	}
	f, err := d.loadWaves()
	if err != nil {
		return nil, err
	}
	if waveWriteHook != nil {
		waveWriteHook()
	}
	now := time.Now().UTC().Truncate(time.Second)
	saved, err := f.Put(wavestore.Wave{Project: p.Project, Name: p.Name, Items: p.Items, CreatedAt: now, UpdatedAt: now}, p.Replace)
	if errors.Is(err, wavestore.ErrDuplicate) {
		return nil, &ipc.Error{Code: CodeDuplicateName, Message: fmt.Sprintf("a saved wave %q already exists for project %s; define it with --replace to replace it", p.Name, p.Project)}
	}
	if err != nil {
		return nil, badParams(err)
	}
	if err := d.saveWaves(f); err != nil {
		return nil, err
	}
	return savedWaveOf(saved), nil
}

// projectStatuses is what one slicer list --all said of a project's items.
type projectStatuses struct {
	byID map[string]WaveMember
	err  *ipc.Error
}

// waveList lists the saved waves with each member's status and title. It calls
// slicer once for each project that has a wave to show. A project slicer cannot
// be read for, or that is no longer registered, puts an error on its waves and
// does not fail the call.
func (d *Daemon) waveList(ctx context.Context, p WaveListParams) (*WaveListResult, error) {
	reg, err := d.loadRegistry()
	if err != nil {
		return nil, err
	}
	if p.Project != "" {
		if _, ok := find(reg, p.Project); !ok {
			return nil, notFound(p.Project, reg)
		}
	}
	d.waveMu.Lock()
	f, err := d.loadWaves()
	d.waveMu.Unlock()
	if err != nil {
		return nil, err
	}
	read := map[string]*projectStatuses{}
	statuses := func(project string) *projectStatuses {
		if s, ok := read[project]; ok {
			return s
		}
		s := &projectStatuses{}
		read[project] = s
		proj, ok := find(reg, project)
		if !ok {
			s.err = ipc.ErrorOf(notFound(project, reg))
			return s
		}
		items, err := d.runs.deps.Slicer.ListAll(ctx, proj.Path)
		if err != nil {
			s.err = ipc.ErrorOf(err)
			return s
		}
		s.byID = make(map[string]WaveMember, len(items))
		for _, item := range items {
			s.byID[item.ID] = WaveMember{Item: item.ID, Status: item.Status, Title: item.Title}
		}
		return s
	}
	result := &WaveListResult{Waves: []SavedWaveView{}}
	for _, w := range f.Waves {
		if p.Project != "" && w.Project != p.Project {
			continue
		}
		s := statuses(w.Project)
		view := SavedWaveView{Project: w.Project, Name: w.Name, Items: make([]WaveMember, 0, len(w.Items)), CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, Error: s.err}
		for _, id := range w.Items {
			member := WaveMember{Item: id}
			if s.err == nil {
				member.Status = WaveStatusMissing
				if m, ok := s.byID[id]; ok {
					member = m
				}
			}
			view.Items = append(view.Items, member)
		}
		result.Waves = append(result.Waves, view)
	}
	return result, nil
}

// waveRm removes one saved wave and returns it.
func (d *Daemon) waveRm(p WaveRmParams) (*SavedWave, error) {
	if p.Project == "" || p.Name == "" {
		return nil, badParams(errors.New("wave.rm needs a project and a name"))
	}
	d.waveMu.Lock()
	defer d.waveMu.Unlock()
	f, err := d.loadWaves()
	if err != nil {
		return nil, err
	}
	w, ok := f.Find(p.Project, p.Name)
	if !ok {
		return nil, noSavedWave(p.Project, p.Name)
	}
	if waveWriteHook != nil {
		waveWriteHook()
	}
	f.Remove(p.Project, p.Name)
	if err := d.saveWaves(f); err != nil {
		return nil, err
	}
	return savedWaveOf(w), nil
}

// dropSavedWaves removes every saved wave of a project that projects rm took
// out of the registry (NW-Q6, owner 2026-11-07). It writes nothing when the
// project has none.
func (d *Daemon) dropSavedWaves(project string) error {
	d.waveMu.Lock()
	defer d.waveMu.Unlock()
	f, err := d.loadWaves()
	if err != nil {
		return err
	}
	if f.RemoveProject(project) == 0 {
		return nil
	}
	return d.saveWaves(f)
}

// checkSavedWave is the check of a wave.start that names a saved wave: the
// project must have a wave of that name, and every item must be one of its
// members. A subset is fine, because done members are left out by the caller.
func (d *Daemon) checkSavedWave(project, name string, items []string) error {
	d.waveMu.Lock()
	f, err := d.loadWaves()
	d.waveMu.Unlock()
	if err != nil {
		return err
	}
	w, ok := f.Find(project, name)
	if !ok {
		return noSavedWave(project, name)
	}
	for _, id := range items {
		if !slices.Contains(w.Items, id) {
			return badParams(fmt.Errorf("item %s is not a member of saved wave %q", id, name))
		}
	}
	return nil
}
