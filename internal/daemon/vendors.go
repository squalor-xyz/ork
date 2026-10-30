// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/source"
)

// defaultVendorCaps is the built-in slot cap per vendor (S169, *proposed*): one
// Codex role at a time, Claude unlimited. The owner fixed it on 2026-10-29; it
// is not a registry field.
var defaultVendorCaps = map[string]int{"codex": 1}

// VendorOut is one vendor the daemon remembers as out (S169, *proposed*).
type VendorOut = source.VendorOut

// VendorsResult is what vendors.list and vendors.set return: the vendors that
// are out now, sorted by vendor. The list is empty when none is. WasOut is set
// by vendors.set with out false, when the vendor was out.
type VendorsResult struct {
	VendorsOut []VendorOut `json:"vendors_out"`
	WasOut     bool        `json:"was_out,omitempty"`
}

// SetVendorParams is a vendors.set call: Out true marks the vendor out until
// back (an owner mark has no expiry), Out false says it is back.
type SetVendorParams struct {
	Vendor string `json:"vendor"`
	Out    bool   `json:"out"`
	Reason string `json:"reason,omitempty"`
}

// ownerVendorReason is the reason of a vendor the owner marked out without one.
const ownerVendorReason = "marked out by the owner"

// vendorsOut lists the vendors that are out now. Times are in the daemon's
// zone, so that a client shows the owner's local clock. The result is nil when
// none is out.
func (d *Daemon) vendorsOut() []VendorOut {
	var out []VendorOut
	for _, v := range d.runs.deps.Vendors.Out() {
		out = append(out, VendorOut{Vendor: v.Vendor, Reason: v.Reason, Since: v.Since.Local(), Until: v.Until.Local()}) // the zero Until stays zero
	}
	return out
}

func (d *Daemon) listVendors() (VendorsResult, error) {
	return VendorsResult{VendorsOut: nonNil(d.vendorsOut())}, nil
}

// setVendor marks a vendor out, or back (S169, *proposed*). Only a vendor ork
// can run is accepted.
func (d *Daemon) setVendor(p SetVendorParams) (VendorsResult, error) {
	if !supportsVendor(p.Vendor) {
		return VendorsResult{}, &ipc.Error{Code: CodeBadParams, Message: fmt.Sprintf("bad params: unsupported vendor %q; use claude or codex", p.Vendor)}
	}
	if p.Out {
		reason := strings.TrimSpace(p.Reason)
		if reason == "" {
			reason = ownerVendorReason
		}
		// The owner's mark has no expiry: it holds until back (2026-10-29).
		d.runs.deps.Vendors.MarkOutOwner(p.Vendor, reason, d.cfg.Clock.Now())
		return d.listVendors()
	}
	was := d.runs.deps.Vendors.Back(p.Vendor)
	res, err := d.listVendors()
	res.WasOut = was
	return res, err
}

// vendorNote tells the owner, in a run plan, which vendors of the plan's
// chains are out and how to end it. It is "" when none is. A run that starts
// after the note changed was not the one the owner confirmed.
func (d *Daemon) vendorNote(lineup map[string][]runrec.LineupEntry) string {
	used := map[string]bool{}
	for _, entries := range lineup {
		for _, entry := range entries {
			used[entry.Vendor] = true
		}
	}
	var notes []string
	for _, v := range d.vendorsOut() {
		if used[v.Vendor] {
			notes = append(notes, fmt.Sprintf("%s; its rows are skipped where another vendor has one; ork vendor back %s ends it", v.Sentence(), v.Vendor))
		}
	}
	return strings.Join(notes, "; ")
}
