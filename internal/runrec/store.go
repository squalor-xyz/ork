// SPDX-License-Identifier: GPL-3.0-or-later

package runrec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is the runs directory, one subdirectory per run.
type Store struct {
	Dir string           // the runs directory
	Now func() time.Time // the clock; nil means time.Now
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// DefaultDir returns $XDG_STATE_HOME/ork/runs, else ~/.local/state/ork/runs.
func DefaultDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "ork", "runs"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "ork", "runs"), nil
}

// RunDir returns the directory of the run with this id.
func (s *Store) RunDir(id string) string { return filepath.Join(s.Dir, id) }

// Run is one open run record. Its methods are safe for concurrent use.
type Run struct {
	store *Store
	dir   string
	mu    sync.Mutex
	rec   Record
}

func validID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("run id %q is not a plain name", id)
	}
	return nil
}

// Create writes the record as a new run and returns it. It sets Version and
// the times. It refuses an id that already exists.
func (s *Store) Create(rec Record) (*Run, error) {
	if err := validID(rec.ID); err != nil {
		return nil, err
	}
	rec.Version = Version
	rec.CreatedAt = s.now()
	rec.UpdatedAt = rec.CreatedAt
	rec = normalize(rec)
	dir := s.RunDir(rec.ID)
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, fmt.Errorf("run %s: %w", rec.ID, err)
	}
	r := &Run{store: s, dir: dir, rec: rec}
	if err := r.save(); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return r, nil
}

// normalize makes the lists empty rather than null, so run.json always holds
// lists.
func normalize(rec Record) Record {
	if rec.Roles == nil {
		rec.Roles = []RoleRecord{}
	}
	if rec.Checks == nil {
		rec.Checks = []CheckRecord{}
	}
	if rec.Approvals == nil {
		rec.Approvals = []Approval{}
	}
	if rec.Questions == nil {
		rec.Questions = []Question{}
	}
	if rec.Writes == nil {
		rec.Writes = []Write{}
	}
	return rec
}

// Open reads the record of the run with this id. It refuses a version or a
// key it does not know.
func (s *Store) Open(id string) (*Run, error) {
	if err := validID(id); err != nil {
		return nil, err
	}
	dir := s.RunDir(id)
	path := filepath.Join(dir, "run.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("run record %s: %w", path, err)
	}
	if head.Version != Version {
		return nil, fmt.Errorf("run record %s: unknown version %d (this ork reads version %d)", path, head.Version, Version)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var rec Record
	if err := dec.Decode(&rec); err != nil {
		return nil, fmt.Errorf("run record %s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("run record %s: trailing data after the JSON object", path)
	}
	return &Run{store: s, dir: dir, rec: normalize(rec)}, nil
}

// List opens every run in the directory, oldest first. A directory without a
// run.json is ignored. A record that cannot be read is left out and named in
// the returned error, so one bad record does not hide the others.
func (s *Store) List() ([]*Run, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []*Run
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.Dir, e.Name(), "run.json")); err != nil {
			continue
		}
		r, err := s.Open(e.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		runs = append(runs, r)
	}
	sort.SliceStable(runs, func(i, j int) bool {
		a, b := runs[i].rec, runs[j].rec
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return runs, errors.Join(errs...)
}

// Prune removes the oldest finished runs beyond keep, but only those for
// which landed returns true. A nil landed removes nothing: a run whose
// branch is not landed is never removed, and an active run is never removed.
// It returns the removed ids.
func (s *Store) Prune(keep int, landed func(Record) bool) ([]string, error) {
	runs, err := s.List()
	var finished []*Run
	for _, r := range runs {
		if IsFinal(r.Snapshot().State) {
			finished = append(finished, r)
		}
	}
	excess := len(finished) - keep
	var removed []string
	var errs []error
	for i := 0; i < excess && landed != nil; i++ {
		rec := finished[i].Snapshot()
		if !landed(rec) {
			continue
		}
		if rmErr := os.RemoveAll(finished[i].dir); rmErr != nil {
			errs = append(errs, rmErr)
			continue
		}
		removed = append(removed, rec.ID)
	}
	return removed, errors.Join(append(errs, err)...)
}

// Dir returns the run's directory.
func (r *Run) Dir() string { return r.dir }

// Snapshot returns a copy of the record.
func (r *Run) Snapshot() Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return clone(r.rec)
}

// clone copies a record through its JSON form, so no slice, map or pointer
// is shared with the original.
func clone(rec Record) Record {
	data, err := json.Marshal(rec)
	if err != nil {
		panic(err) // a Record always marshals
	}
	var out Record
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return normalize(out)
}

// Update applies fn to the record and writes it atomically. When the write
// fails the record in memory is rolled back and the file is untouched.
func (r *Run) Update(fn func(*Record)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	before := clone(r.rec)
	fn(&r.rec)
	r.rec.UpdatedAt = r.store.now()
	r.rec = normalize(r.rec)
	if err := r.save(); err != nil {
		r.rec = before
		return err
	}
	return nil
}

// AddWrite appends a write to writes[] and saves the record.
func (r *Run) AddWrite(w Write) error {
	return r.Update(func(rec *Record) { rec.Writes = append(rec.Writes, w) })
}

// writeTemp writes the data to the temporary file. Tests replace it to inject
// a write failure.
var writeTemp = func(f *os.File, data []byte) error {
	_, err := f.Write(data)
	return err
}

// save writes the record to run.json atomically: a temporary file in the
// same directory, then a rename. The caller holds r.mu, or owns r.
func (r *Run) save() error {
	data, err := json.MarshalIndent(r.rec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(r.dir, "run.json")
	tmp, err := os.CreateTemp(r.dir, ".run-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("run record %s: %w", path, err)
	}
	if err := writeTemp(tmp, data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("run record %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("run record %s: %w", path, err)
	}
	return nil
}
