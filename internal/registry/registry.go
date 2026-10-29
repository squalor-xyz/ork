// SPDX-License-Identifier: GPL-3.0-or-later

// Package registry reads, validates and writes ork's project registry,
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
	"strings"
	"time"
)

// Version is the projects.json version this package writes. It also reads
// version 1, which lacks daemon_idle_minutes; Load returns such a file as
// version 2, so the next Save writes version 2.
const Version = 2

// DefaultDaemonIdleMinutes is how long an idle daemon waits before it exits
// (daemon.idle). A daemon_idle_minutes of 0 means this default
// (registry.zero-values).
const DefaultDaemonIdleMinutes = 10

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

// FillPattern fills {id} with the item id and {id_lower} with its lowercase form.
// Other text, including unknown placeholders, stays unchanged.
func FillPattern(pattern, id string) string {
	return strings.NewReplacer("{id}", id, "{id_lower}", strings.ToLower(id)).Replace(pattern)
}

// ValidateUIDoc checks a ui_doc value (S161, *proposed*). Empty means unset.
// Otherwise the value must be a relative path that stays inside the project
// and names a file below it, so "." and ".." are refused. Whether the file
// exists is not checked.
func ValidateUIDoc(path string) error {
	if path == "" {
		return nil
	}
	if strings.ContainsRune(path, 0) {
		return errors.New("ui_doc contains a NUL byte")
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("ui_doc %q must be relative to the project, not absolute", path)
	}
	if clean := filepath.Clean(path); clean == "." || !filepath.IsLocal(clean) {
		return fmt.Errorf("ui_doc %q must name a file inside the project", path)
	}
	return nil
}

// ValidatePattern checks a worktree_pattern or branch_pattern value (S161,
// *proposed*). Empty means unset. Otherwise it must hold {id} or {id_lower},
// so two items never plan the same worktree or branch. The placeholders are
// case sensitive, as FillPattern fills them.
func ValidatePattern(name, pattern string) error {
	if pattern == "" || strings.Contains(pattern, "{id}") || strings.Contains(pattern, "{id_lower}") {
		return nil
	}
	return fmt.Errorf("%s %q must contain {id} or {id_lower}", name, pattern)
}

// ValidateEnvLine checks one environment_lines entry (S174, *proposed*). It
// must hold text, and be one line without a NUL byte.
func ValidateEnvLine(line string) error {
	if strings.TrimSpace(line) == "" {
		return errors.New("environment_lines entry is blank")
	}
	if strings.ContainsAny(line, "\x00\r\n") {
		return errors.New("environment_lines entry must be one line without a NUL byte")
	}
	return nil
}

// ValidateVendorEnv checks one vendor_env entry (S174, *proposed*): a vendor
// name of [a-z0-9_-], a key that is a shell name ([A-Za-z_][A-Za-z0-9_]*), and
// a value without a NUL byte. The errors never quote the value.
func ValidateVendorEnv(vendor, key, value string) error {
	if vendor == "" || strings.ContainsFunc(vendor, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) {
		return fmt.Errorf("vendor_env vendor %q must be one or more of a-z, 0-9, _ and -", vendor)
	}
	if !isShellName(key) {
		return fmt.Errorf("vendor_env key %q for %s must be a shell name ([A-Za-z_][A-Za-z0-9_]*)", key, vendor)
	}
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("vendor_env value for %s:%s contains a NUL byte", vendor, key)
	}
	return nil
}

func isShellName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_'
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// ValidatePermissionEntry checks one permissions entry (S174, *proposed*): the
// preset "read-only", or a list of patterns that are not blank and hold no NUL
// byte or newline. The pattern syntax per vendor stays unchecked
// (docs/state.md). Whether the role may have an allowlist is
// engine.ResolvePermission's call, not this one's.
func ValidatePermissionEntry(role string, p Permission) error {
	name := "permissions." + role
	switch {
	case p.Preset != "" && len(p.Allow) > 0:
		return fmt.Errorf("%s must be the preset read-only or a list of patterns, not both", name)
	case p.Preset != "":
		if p.Preset != "read-only" {
			return fmt.Errorf("%s: unknown preset %q (the only preset is read-only)", name, p.Preset)
		}
		return nil
	case len(p.Allow) == 0:
		return fmt.Errorf("%s must be the preset read-only or a non-empty list of patterns", name)
	}
	for _, pat := range p.Allow {
		if strings.TrimSpace(pat) == "" {
			return fmt.Errorf("%s has a blank pattern", name)
		}
		if strings.ContainsAny(pat, "\x00\r\n") {
			return fmt.Errorf("%s has a pattern with a NUL byte or a newline", name)
		}
	}
	return nil
}

// Project is one registry entry. Fields other than Name, Path and Adj are
// optional; a zero value means unset and is not written. WorktreePattern and
// BranchPattern accept {id} and {id_lower} (S124, *proposed*).
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
	// Push says ork pushes the base branch after it lands a slice (S119,
	// *proposed*; adj 162). Off by default; the owner turns it on per project.
	Push bool `json:"push,omitempty"`
	// UIDoc is the path, relative to the project, of the project's UI design
	// doc (S136, *proposed*). The UI pass's brief names it. Empty means the
	// brief tells the pass to find the project's own UI docs.
	UIDoc string `json:"ui_doc,omitempty"`
}

// File is the content of projects.json.
type File struct {
	Version   int       `json:"version"`
	GlobalCap int       `json:"global_cap,omitempty"`
	Projects  []Project `json:"projects"`

	// DaemonIdleMinutes is new in version 2. Zero means unset.
	DaemonIdleMinutes int `json:"daemon_idle_minutes,omitempty"`
}

// fileV1 is the version 1 shape. Decoding a version 1 file into it keeps the
// strict-key rule: a key added in version 2 is unknown there.
type fileV1 struct {
	Version   int       `json:"version"`
	GlobalCap int       `json:"global_cap,omitempty"`
	Projects  []Project `json:"projects"`
}

// VersionError reports a file version this package does not know.
type VersionError struct{ Got int }

func (e *VersionError) Error() string {
	return fmt.Sprintf("unknown registry version %d (this ork reads versions 1 and %d)", e.Got, Version)
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
// empty registry without creating a file or directory.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{Version: Version, Projects: []Project{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("registry %s: %w", path, err)
	}
	if head.Version != 1 && head.Version != Version {
		return nil, fmt.Errorf("registry %s: %w", path, &VersionError{Got: head.Version})
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f File
	if head.Version == 1 {
		var v1 fileV1
		if err := dec.Decode(&v1); err != nil {
			return nil, fmt.Errorf("registry %s: %w", path, err)
		}
		f = File{Version: Version, GlobalCap: v1.GlobalCap, Projects: v1.Projects}
	} else if err := dec.Decode(&f); err != nil {
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
	if f.DaemonIdleMinutes < 0 {
		return errors.New("daemon_idle_minutes must not be negative")
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
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("registry %s: %w", path, err)
	}
	return nil
}

// DaemonIdle returns how long an idle daemon waits before it exits. An unset
// daemon_idle_minutes gives DefaultDaemonIdleMinutes.
func (f *File) DaemonIdle() time.Duration {
	if f.DaemonIdleMinutes <= 0 {
		return DefaultDaemonIdleMinutes * time.Minute
	}
	return time.Duration(f.DaemonIdleMinutes) * time.Minute
}
