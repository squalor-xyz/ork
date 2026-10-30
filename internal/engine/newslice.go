// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// NewSliceConfig is an owner's request to file a thin slice without a run.
type NewSliceConfig struct {
	Project                     registry.Project
	Title, Why, From            string
	FromNote                    int
	Size                        string
	Importance, Urgency, Effort int
	TempDir                     string
}

// ValidateNewSlice checks the request before any write. Note bounds require Show.
func ValidateNewSlice(cfg NewSliceConfig) error {
	if strings.TrimSpace(strings.TrimLeft(cfg.Title, "- ")) == "" || strings.ContainsAny(cfg.Title, "\r\n") {
		return fmt.Errorf("title must be one non-blank line")
	}
	if cfg.FromNote < 0 || (cfg.FromNote > 0 && strings.TrimSpace(cfg.From) == "") {
		return fmt.Errorf("from-note needs a source item and a positive note number")
	}
	for _, n := range []int{cfg.Importance, cfg.Urgency, cfg.Effort} {
		if n < 0 || n > 3 {
			return fmt.Errorf("scores must be 1-3, or 0 to leave unspecified")
		}
	}
	if cfg.Size != "" && cfg.Size != "S" && cfg.Size != "M" && cfg.Size != "L" {
		return fmt.Errorf("size must be S, M or L")
	}
	return nil
}

// FileNewSlice delegates add and promote to slicer. Retrying the same request
// recovers an incomplete promote or returns the already filed slice.
func FileNewSlice(ctx context.Context, d Deps, cfg NewSliceConfig) (id string, existing bool, err error) {
	if err := ValidateNewSlice(cfg); err != nil {
		return "", false, err
	}
	title := strings.Join(strings.Fields(cfg.Title), " ")
	why := cfg.Why
	if cfg.FromNote > 0 {
		detail, err := d.Slicer.Show(ctx, cfg.Project.Path, cfg.From)
		if err != nil {
			return "", false, err
		}
		if cfg.FromNote > len(detail.Notes) {
			return "", false, fmt.Errorf("%s has no note %d", cfg.From, cfg.FromNote)
		}
		if strings.TrimSpace(why) == "" {
			why = "> " + strings.ReplaceAll(detail.Notes[cfg.FromNote-1], "\n", "\n> ")
		}
	}
	// Encode fields separately so titles and text cannot collide at a delimiter.
	identity, _ := json.Marshal([]any{cfg.Project.Path, title, cfg.Why, cfg.From, cfg.FromNote})
	sum := sha256.Sum256(identity)
	key := fmt.Sprintf("ork-new-%x", sum[:8])
	if strings.TrimSpace(why) == "" {
		why = "Filed in ork on " + d.now().Format("2006-01-02") + ". No description was given."
	}
	if cfg.From != "" {
		why += "\n\nDiscovered from " + cfg.From + "."
	}
	w := runrec.Writer{Now: d.now, Sleep: d.Sleep, Backoff: d.Backoff}
	added, err := d.Slicer.AddItem(ctx, w, cfg.Project.Path, slicercli.AddSpec{Title: title, DiscoveredFrom: cfg.From, Key: key, Size: cfg.Size, Importance: cfg.Importance, Urgency: cfg.Urgency, Effort: cfg.Effort})
	if err != nil {
		return "", false, fmt.Errorf("slicer add: %w", err)
	}
	if added.Existing && added.HasSlice {
		return added.ID, true, nil
	}
	// Any failure after add names the item so the owner can retry it.
	fail := func(err error) (string, bool, error) {
		return "", false, fmt.Errorf("filed %s, but its Why section failed (filing again retries it): %w", added.ID, err)
	}
	f, err := os.CreateTemp(cfg.TempDir, "new-*.md")
	if err != nil {
		return fail(err)
	}
	defer os.Remove(f.Name())
	// slicer treats column-zero hashes as outline structure, even in Why.
	lines := strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(why), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "#") {
			lines[i] = " " + line
		}
	}
	why = strings.Join(lines, "\n")
	_, err = fmt.Fprintf(f, "## %s\n\n### Why\n\n%s\n", title, why)
	closeErr := f.Close()
	if err != nil {
		return fail(err)
	}
	if closeErr != nil {
		return fail(closeErr)
	}
	if err := d.Slicer.Promote(ctx, w, cfg.Project.Path, added.ID, f.Name()); err != nil {
		return fail(err)
	}
	return added.ID, false, nil
}
