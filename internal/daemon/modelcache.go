// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/models"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

// modelCache holds the model list of each vendor for the daemon's life (S244,
// *proposed*): a vendor's list command runs once, on the first plan that needs
// it, and a restart reads it again. A failed read is not kept, so the next
// plan tries again.
type modelCache struct {
	mu    sync.Mutex
	lists map[string][]string
	// list reads one vendor's list. It is replaced in tests; the zero value
	// runs the vendor's own command.
	list models.Lister
}

// names returns the vendor's model names. The mutex is held while the list
// command runs, so two plans that need the same vendor make one call. A local
// launcher never has a list: its models name entries on the local server, not
// a vendor's catalog.
func (c *modelCache) names(ctx context.Context, vendor string) ([]string, error) {
	if engine.IsLocalVendor(vendor) {
		return nil, fmt.Errorf("%s is a local launcher and has no model list", vendor)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if names, ok := c.lists[vendor]; ok {
		return names, nil
	}
	list := c.list
	if list == nil {
		list = listVendorModels
	}
	names, err := list(ctx, vendor)
	if err != nil {
		return nil, err
	}
	if c.lists == nil {
		c.lists = map[string][]string{}
	}
	c.lists[vendor] = names
	return names, nil
}

// isLocalRow reports whether a matrix row's vendor column is Local (matrix row
// r33). A row whose agent is a local launcher is local by its vendor name; see
// engine.IsLocalVendor.
func isLocalRow(row slicercli.Row) bool {
	return strings.EqualFold(strings.TrimSpace(row.Vendor), "local")
}

// chainEntry is a lineup entry while a plan is built, with whether its matrix
// row is local (vendor column Local). The mark belongs to this one entry of
// this one chain, so it goes with the entry when a chain is filtered or
// replaced and never reaches an equal entry elsewhere (S244).
type chainEntry struct {
	runrec.LineupEntry
	local bool
}

// appendChain is appendEntry for chainEntry: an entry equal to the one before
// it, mark included, is dropped.
func appendChain(chain []chainEntry, e chainEntry) []chainEntry {
	if n := len(chain); n > 0 && chain[n-1] == e {
		return chain
	}
	return append(chain, e)
}

// resolveEntries resolves the model of each entry (S244, *proposed*): a
// FAMILY@latest text becomes the newest model of the family and is kept in
// Requested; a pinned name is passed unchanged. The model of a local row is
// never resolved: an entry of a local launcher or marked local is passed
// through, and one that asks for @latest is refused, because local models are
// pinned. An entry that comes out the same as the one before it is dropped, as
// in a chain built from the matrix.
func (d *Daemon) resolveEntries(ctx context.Context, chain []chainEntry) ([]runrec.LineupEntry, error) {
	out := make([]runrec.LineupEntry, 0, len(chain))
	for _, ce := range chain {
		entry := ce.LineupEntry
		if engine.IsLocalVendor(entry.Vendor) || ce.local {
			if strings.Contains(entry.Model, models.Suffix) {
				return nil, fmt.Errorf("model %q of a local row: local models are pinned; write the name the local server lists, not FAMILY@latest", entry.Model)
			}
		} else {
			model, requested, err := models.Resolve(ctx, entry.Vendor, entry.Model, d.models.names)
			if err != nil {
				return nil, err
			}
			entry.Model, entry.Requested = model, requested
		}
		out = appendEntry(out, entry)
	}
	return out, nil
}

// listVendorModels runs the vendor's list command: codex through
// `codex debug models --bundled` and grok through `grok models`. Claude has no
// list command; its aliases resolve without one.
func listVendorModels(ctx context.Context, vendor string) ([]string, error) {
	switch vendor {
	case "codex":
		return models.ListCodex(ctx, "codex")
	case "grok":
		return models.ListGrok(ctx, "grok")
	}
	return nil, fmt.Errorf("%s has no model list command", vendor)
}
