// SPDX-License-Identifier: GPL-3.0-or-later

// Package registry reads, validates, writes and seeds ork's project registry,
// projects.json (docs/state.md).
package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Version is the only projects.json version this package reads and writes.
const Version = 1

// Budgets caps a run. A zero value means unset.
type Budgets struct {
	Minutes int `json:"minutes,omitempty"`
	Tokens  int `json:"tokens,omitempty"`
}

// Permission is one role's allowlist. In JSON it is either a preset name
// (the string "read-only") or a list of allowed tool patterns. The exact
// pattern syntax per vendor waits on the vendor spike (docs/state.md).
type Permission struct {
	Preset string
	Allow  []string
}

// MarshalJSON writes the preset as a string, otherwise the list.
func (p Permission) MarshalJSON() ([]byte, error) {
	if p.Preset != "" {
		return json.Marshal(p.Preset)
	}
	allow := p.Allow
	if allow == nil {
		allow = []string{}
	}
	return json.Marshal(allow)
}

// UnmarshalJSON reads a string preset or a list of strings.
func (p *Permission) UnmarshalJSON(data []byte) error {
	var preset string
	if err := json.Unmarshal(data, &preset); err == nil {
		*p = Permission{Preset: preset}
		return nil
	}
	var allow []string
	if err := json.Unmarshal(data, &allow); err != nil {
		return errors.New("permission must be a string or a list of strings")
	}
	*p = Permission{Allow: allow}
	return nil
}

// Project is one registry entry. Fields other than Name, Path and Adj are
// optional; a zero value means unset and is not written.
type Project struct {
	Name             string                       `json:"name"`
	Path             string                       `json:"path"`
	Adj              bool                         `json:"adj"`
	Cap              int                          `json:"cap,omitempty"`
	WorktreePattern  string                       `json:"worktree_pattern,omitempty"`
	BranchPattern    string                       `json:"branch_pattern,omitempty"`
	CheckCommand     string                       `json:"check_command,omitempty"`
	CheckMinutes     int                          `json:"check_minutes,omitempty"`
	EnvironmentLines []string                     `json:"environment_lines,omitempty"`
	Budgets          Budgets                      `json:"budgets,omitzero"`
	Permissions      map[string]Permission        `json:"permissions,omitempty"`
	VendorEnv        map[string]map[string]string `json:"vendor_env,omitempty"`
}

// File is the content of projects.json.
type File struct {
	Version   int       `json:"version"`
	GlobalCap int       `json:"global_cap,omitempty"`
	Projects  []Project `json:"projects"`
}

// VersionError reports a file version this package does not know.
type VersionError struct{ Got int }

func (e *VersionError) Error() string {
	return fmt.Sprintf("unknown registry version %d (this ork reads version %d)", e.Got, Version)
}

// DefaultPath returns $XDG_CONFIG_HOME/ork/projects.json, else
// ~/.config/ork/projects.json.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "ork", "projects.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ork", "projects.json"), nil
}

// Load reads and validates the registry at path. A missing file returns an
// error for which errors.Is(err, os.ErrNotExist) is true.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("registry %s: %w", path, err)
	}
	if head.Version != Version {
		return nil, fmt.Errorf("registry %s: %w", path, &VersionError{Got: head.Version})
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("registry %s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("registry %s: trailing data after the JSON object", path)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("registry %s: %w", path, err)
	}
	return &f, nil
}

// Validate checks the version and the project entries.
func (f *File) Validate() error {
	if f.Version != Version {
		return &VersionError{Got: f.Version}
	}
	if f.GlobalCap < 0 {
		return errors.New("global_cap must not be negative")
	}
	seen := map[string]bool{}
	for i, p := range f.Projects {
		if p.Name == "" {
			return fmt.Errorf("project %d: name is empty", i)
		}
		if seen[p.Name] {
			return fmt.Errorf("project %q: duplicate name", p.Name)
		}
		seen[p.Name] = true
		if !filepath.IsAbs(p.Path) {
			return fmt.Errorf("project %q: path %q is not absolute", p.Name, p.Path)
		}
		if p.Cap < 0 || p.CheckMinutes < 0 || p.Budgets.Minutes < 0 || p.Budgets.Tokens < 0 {
			return fmt.Errorf("project %q: cap, check_minutes and budgets must not be negative", p.Name)
		}
	}
	return nil
}

// writeTemp writes the data to the temporary file. Tests replace it to inject
// a write failure.
var writeTemp = func(f *os.File, data []byte) error {
	_, err := f.Write(data)
	return err
}

// Save validates f and writes it to path atomically: a temporary file in the
// same directory, then a rename. On any failure the old file is untouched and
// no temporary file is left behind.
func Save(path string, f *File) error {
	return save(path, f, false)
}

// beforeCommit runs after the temporary file is complete and before it is
// moved into place. Tests use it to change the directory at that moment.
var beforeCommit = func() {}

// save is Save. With noReplace it never replaces an existing file: when the
// target exists at commit time it returns an error for which
// errors.Is(err, os.ErrExist) is true, and leaves the target untouched.
func save(path string, f *File, noReplace bool) error {
	if err := f.Validate(); err != nil {
		return fmt.Errorf("registry %s: %w", path, err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".projects-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("registry %s: %w", path, err)
	}
	if err := writeTemp(tmp, data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("registry %s: %w", path, err)
	}
	beforeCommit()
	if noReplace {
		// A hard link fails if path exists, so it never replaces a file that
		// appeared after the caller's check.
		err := os.Link(tmpName, path)
		os.Remove(tmpName)
		if err != nil {
			return fmt.Errorf("registry %s: %w", path, err)
		}
		return nil
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("registry %s: %w", path, err)
	}
	return nil
}
