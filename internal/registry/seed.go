// SPDX-License-Identifier: GPL-3.0-or-later

package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// seedNames are the projects registry.seed decided on, in this order.
var seedNames = []string{"slicer", "adjudicate", "DataBall", "agents"}

// SeedReport says what Seed wrote.
type SeedReport struct {
	Path    string    // the file written
	Written []Project // the projects written, in file order
	Skipped []string  // decided projects whose directory is missing under the seed root
}

// DefaultSeedRoot returns $ORK_SEED_ROOT, else ~/code/squalor-xyz. The
// variable and the default are proposed, not decided.
func DefaultSeedRoot() (string, error) {
	if root := os.Getenv("ORK_SEED_ROOT"); root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "code", "squalor-xyz"), nil
}

// Seed writes a registry at path holding the decided projects (registry.seed)
// whose directories exist under seedRoot. A project gets adj true when its
// directory holds .adjudicate.json. Seed never overwrites: when path exists
// it returns an error for which errors.Is(err, os.ErrExist) is true.
func Seed(path, seedRoot string) (*SeedReport, error) {
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("registry %s: %w", path, os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	report := &SeedReport{Path: path}
	f := &File{Version: Version, Projects: []Project{}}
	for _, name := range seedNames {
		dir := filepath.Join(seedRoot, name)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			report.Skipped = append(report.Skipped, name)
			continue
		}
		p := Project{Name: name, Path: dir}
		if info, err := os.Stat(filepath.Join(dir, ".adjudicate.json")); err == nil && info.Mode().IsRegular() {
			p.Adj = true
		}
		f.Projects = append(f.Projects, p)
	}
	if err := save(path, f, true); err != nil {
		return nil, err
	}
	report.Written = f.Projects
	return report, nil
}

// LoadOrSeed loads the registry at path. When the file is missing it seeds
// one and returns the report; otherwise the report is nil.
func LoadOrSeed(path, seedRoot string) (*File, *SeedReport, error) {
	f, err := Load(path)
	if err == nil {
		return f, nil, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	report, err := Seed(path, seedRoot)
	if errors.Is(err, os.ErrExist) { // the file appeared after our check: use it
		f, err := Load(path)
		return f, nil, err
	}
	if err != nil {
		return nil, nil, err
	}
	f, err = Load(path)
	if err != nil {
		return nil, nil, err
	}
	return f, report, nil
}
