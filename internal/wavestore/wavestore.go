// SPDX-License-Identifier: GPL-3.0-or-later

// Package wavestore reads and writes waves.json, the file where ork keeps
// saved waves (S215, *proposed*). A saved wave is a name and the item ids of
// one project; it never holds an item's status, title or spec, because those
// are slicer's and are read on every use. The file sits in the registry's
// directory and is separate from projects.json.
package wavestore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Version is the one file version this package reads and writes.
	Version = 1
	// FileName is the name of the file, beside projects.json.
	FileName = "waves.json"
	// MaxNameLength is the longest name (NW-Q2, owner 2026-11-07).
	MaxNameLength = 32
	// MaxItems is the most items a saved wave holds (NW-Q2).
	MaxItems = 20
	// Mode is the file mode of waves.json (NW-Q5, owner 2026-11-07).
	Mode os.FileMode = 0o600
)

// ErrDuplicate means a wave of that project and name is already saved.
var ErrDuplicate = errors.New("a saved wave of that name already exists")

// Wave is one saved wave. The key is Project plus Name.
type Wave struct {
	Project   string    `json:"project"`
	Name      string    `json:"name"`
	Items     []string  `json:"items"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// File is the content of waves.json. Waves keep the order they were saved in.
type File struct {
	Version int    `json:"version"`
	Waves   []Wave `json:"waves"`
}

// VersionError reports a file version this package does not know.
type VersionError struct{ Got int }

func (e *VersionError) Error() string {
	return fmt.Sprintf("unknown waves file version %d (this ork reads version %d)", e.Got, Version)
}

// ValidateName checks a saved wave's name: 1 to MaxNameLength characters of
// [a-z0-9._-], starting with a letter or digit, and never starting with
// "wave-", which ork stop reads as a wave id (adj 138).
func ValidateName(name string) error {
	if name == "" {
		return errors.New("wave name is empty")
	}
	if len(name) > MaxNameLength {
		return fmt.Errorf("wave name %q is longer than %d characters", name, MaxNameLength)
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		letterOrDigit := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if letterOrDigit || i > 0 && (c == '.' || c == '_' || c == '-') {
			continue
		}
		if i == 0 {
			return fmt.Errorf("wave name %q must start with a letter or digit from a-z0-9", name)
		}
		return fmt.Errorf("wave name %q may hold only the characters a-z, 0-9, '.', '_' and '-'", name)
	}
	if strings.HasPrefix(name, "wave-") {
		return fmt.Errorf("wave name %q must not start with %q, which ork stop reads as a wave id", name, "wave-")
	}
	return nil
}

// ValidateItems checks a saved wave's item ids: at least one, at most
// MaxItems, none empty and none repeated. Whether slicer knows an id is the
// daemon's check.
func ValidateItems(items []string) error {
	if len(items) == 0 {
		return errors.New("wave needs at least one item")
	}
	if len(items) > MaxItems {
		return fmt.Errorf("wave holds %d items; the most is %d", len(items), MaxItems)
	}
	seen := make(map[string]bool, len(items))
	for _, id := range items {
		if id == "" {
			return errors.New("wave item id is empty")
		}
		if seen[id] {
			return fmt.Errorf("wave item %s is listed twice", id)
		}
		seen[id] = true
	}
	return nil
}

// Validate checks the version and every wave.
func (f *File) Validate() error {
	if f.Version != Version {
		return &VersionError{Got: f.Version}
	}
	seen := map[[2]string]bool{}
	for i, w := range f.Waves {
		if w.Project == "" {
			return fmt.Errorf("wave %d: project is empty", i)
		}
		if err := ValidateName(w.Name); err != nil {
			return fmt.Errorf("wave %d: %w", i, err)
		}
		if err := ValidateItems(w.Items); err != nil {
			return fmt.Errorf("wave %q of %s: %w", w.Name, w.Project, err)
		}
		key := [2]string{w.Project, w.Name}
		if seen[key] {
			return fmt.Errorf("wave %q of %s: duplicate name", w.Name, w.Project)
		}
		seen[key] = true
	}
	return nil
}

// Find returns the saved wave of the project and name.
func (f *File) Find(project, name string) (Wave, bool) {
	for _, w := range f.Waves {
		if w.Project == project && w.Name == name {
			return w, true
		}
	}
	return Wave{}, false
}

// Put saves w. A new wave goes to the end. A wave of the same project and name
// is refused with ErrDuplicate unless replace is set; a replace keeps the
// wave's place and CreatedAt, and takes the rest from w. Put returns the wave
// as stored. On an error f is unchanged.
func (f *File) Put(w Wave, replace bool) (Wave, error) {
	if w.Project == "" {
		return Wave{}, errors.New("wave project is empty")
	}
	if err := ValidateName(w.Name); err != nil {
		return Wave{}, err
	}
	if err := ValidateItems(w.Items); err != nil {
		return Wave{}, err
	}
	w.Items = append([]string(nil), w.Items...)
	for i, have := range f.Waves {
		if have.Project != w.Project || have.Name != w.Name {
			continue
		}
		if !replace {
			return Wave{}, ErrDuplicate
		}
		w.CreatedAt = have.CreatedAt
		f.Waves[i] = w
		return w, nil
	}
	f.Waves = append(f.Waves, w)
	return w, nil
}

// Remove deletes the saved wave of the project and name and reports whether it
// existed.
func (f *File) Remove(project, name string) bool {
	for i, w := range f.Waves {
		if w.Project == project && w.Name == name {
			f.Waves = append(f.Waves[:i], f.Waves[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveProject deletes every saved wave of the project and returns how many.
func (f *File) RemoveProject(project string) int {
	kept := f.Waves[:0]
	removed := 0
	for _, w := range f.Waves {
		if w.Project == project {
			removed++
			continue
		}
		kept = append(kept, w)
	}
	f.Waves = kept
	return removed
}

// PathBeside returns the path of waves.json in the directory of the given
// registry path.
func PathBeside(registryPath string) string {
	return filepath.Join(filepath.Dir(registryPath), FileName)
}

// Load reads and validates the file at path. A missing file returns an empty
// file without creating a file or directory. Unknown keys and a version this
// package does not know are errors, as for projects.json.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{Version: Version, Waves: []Wave{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("waves %s: %w", path, err)
	}
	if head.Version != Version {
		return nil, fmt.Errorf("waves %s: %w", path, &VersionError{Got: head.Version})
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("waves %s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("waves %s: trailing data after the JSON object", path)
	}
	if f.Waves == nil {
		f.Waves = []Wave{}
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("waves %s: %w", path, err)
	}
	return &f, nil
}

// writeTemp writes the data to the temporary file. Tests replace it to inject
// a write failure.
var writeTemp = func(f *os.File, data []byte) error {
	_, err := f.Write(data)
	return err
}

// Save validates f and writes it to path atomically: a temporary file in the
// same directory, synced, then a rename. On any failure the old file is
// untouched and no temporary file is left behind.
func Save(path string, f *File) error {
	if err := f.Validate(); err != nil {
		return fmt.Errorf("waves %s: %w", path, err)
	}
	out := *f
	if out.Waves == nil {
		out.Waves = []Wave{}
	}
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".waves-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("waves %s: %w", path, err)
	}
	if err := writeTemp(tmp, data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(Mode); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("waves %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("waves %s: %w", path, err)
	}
	return nil
}
