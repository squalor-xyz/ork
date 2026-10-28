// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
)

// Hash the identities so project and item names never become path components.
func specPathKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (d *Daemon) specDir(project, item string) string {
	return filepath.Join(d.cfg.Paths.Dir, "spec-checks", specPathKey(project), specPathKey(item))
}

type storedSpecAnswers struct {
	Fingerprint string `json:"fingerprint"`
	Decisions   string `json:"decisions"`
	OwnerNeeded string `json:"owner_needed,omitempty"`
}

func writeSpecJSON(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// loadSpecAnswers serializes the short file and cache operation. Only owner answers are durable;
// readiness from a role is still a cache, pending slicer's readiness record.
func (d *Daemon) loadSpecAnswers(project, item, fp string) error {
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	return d.loadSpecAnswersLocked(project, item, fp)
}

// The caller holds specStateMu.
func (d *Daemon) loadSpecAnswersLocked(project, item, fp string) error {
	path := filepath.Join(d.specDir(project, item), "answers.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read stored spec answers %s: %w", path, err)
	}
	var a storedSpecAnswers
	invalid := json.Unmarshal(b, &a)
	if invalid == nil && (a.Fingerprint == "" || strings.TrimSpace(a.Decisions) == "") {
		invalid = errors.New("empty fingerprint or decisions")
	}
	if invalid != nil {
		fmt.Fprintf(os.Stderr, "discard stored spec answers %s: %v\n", path, invalid)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("discard stored spec answers %s: %w", path, err)
		}
		d.runs.mu.Lock()
		delete(d.runs.specs, specKey(project, item))
		d.runs.mu.Unlock()
		return nil
	}
	if a.Fingerprint != fp {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale spec answers %s: %w", path, err)
		}
		return nil
	}
	d.runs.mu.Lock()
	d.runs.specs[specKey(project, item)] = specEntry{fingerprint: fp, ready: true, decisions: a.Decisions, ownerNeeded: a.OwnerNeeded}
	d.runs.mu.Unlock()
	return nil
}

type specCheckIdentity struct {
	Project string `json:"project"`
	Item    string `json:"item"`
}

var legacySpecDir = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

// Sweep old legacy directories and identified items that are done, retired or
// gone. A failed slicer inventory never proves that an item disappeared.
func (d *Daemon) sweepSpecDirs(ctx context.Context) {
	root := filepath.Join(d.cfg.Paths.Dir, "spec-checks")
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if !entry.IsDir() || !legacySpecDir.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err == nil && time.Since(info.ModTime()) > 14*24*time.Hour {
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				fmt.Fprintf(os.Stderr, "sweep legacy spec check: %v\n", err)
			}
		}
	}
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", "check.json"))
	if err != nil || len(paths) == 0 {
		return
	}
	f, err := d.loadRegistry()
	if err != nil {
		return
	}
	inventories := map[string]map[string]bool{}
	for _, path := range paths {
		dir := filepath.Dir(path)
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || time.Since(info.ModTime()) <= 14*24*time.Hour {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var id specCheckIdentity
		if json.Unmarshal(b, &id) != nil || id.Project == "" || id.Item == "" || dir != d.specDir(id.Project, id.Item) {
			continue
		}
		p, ok := find(f, id.Project)
		if !ok {
			if err := os.RemoveAll(dir); err != nil {
				fmt.Fprintf(os.Stderr, "sweep removed project spec check: %v\n", err)
			}
			continue
		}
		items, read := inventories[p.Name]
		if !read {
			all, err := d.runDeps(p).Slicer.List(ctx, p.Path)
			if err == nil {
				items = map[string]bool{}
				for _, item := range all {
					if item.Status != "done" && item.Status != "retired" {
						items[item.ID] = true
					}
				}
			}
			inventories[p.Name] = items
		}
		if items != nil && !items[id.Item] {
			if err := os.RemoveAll(dir); err != nil {
				fmt.Fprintf(os.Stderr, "sweep spec check: %v\n", err)
			}
		}
	}
}

// The caller holds specStateMu and runs.mu.
func (d *Daemon) removeSpecDirLocked(project, item string) {
	if err := os.RemoveAll(d.specDir(project, item)); err != nil {
		fmt.Fprintf(os.Stderr, "remove spec check: %v\n", err)
		return
	}
	delete(d.runs.specs, specKey(project, item))
}

// The caller holds specStateMu and runs.mu after the registry removal succeeds.
func (d *Daemon) removeProjectSpecsLocked(project string) error {
	if err := os.RemoveAll(filepath.Join(d.cfg.Paths.Dir, "spec-checks", specPathKey(project))); err != nil {
		return err
	}
	for key := range d.runs.specs {
		if strings.HasPrefix(key, project+"\x00") {
			delete(d.runs.specs, key)
		}
	}
	return nil
}

// Run starts also restore answers when a caller skips the cached check.
// The slicer read happens outside specStateMu; only files and entries use it.
func (d *Daemon) restoreRunSpecAnswers(ctx context.Context, p registry.Project, item string) error {
	path := filepath.Join(d.specDir(p.Name, item), "answers.json")
	d.runs.specStateMu.Lock()
	_, statErr := os.Stat(path)
	d.runs.specStateMu.Unlock()
	if errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	if statErr != nil {
		return fmt.Errorf("stat stored spec answers %s: %w", path, statErr)
	}
	raw, err := d.runDeps(p).Slicer.ShowRaw(ctx, p.Path, item)
	if err != nil {
		return fmt.Errorf("restore stored spec answers %s: %w", path, err)
	}
	fp, err := engine.SpecFingerprint(raw)
	if err != nil {
		return fmt.Errorf("restore stored spec answers %s: %w", path, err)
	}
	d.runs.specStateMu.Lock()
	defer d.runs.specStateMu.Unlock()
	if err := d.loadSpecAnswersLocked(p.Name, item, fp); err != nil {
		return err
	}
	d.runs.mu.Lock()
	if e := d.runs.specs[specKey(p.Name, item)]; e.fingerprint != fp {
		delete(d.runs.specs, specKey(p.Name, item))
	}
	d.runs.mu.Unlock()
	return nil
}
