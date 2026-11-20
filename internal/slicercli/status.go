// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"github.com/squalor-xyz/ork/internal/clirun"
)

// Status is slicer's dependency summary. ork preserves slicer's order.
type Status struct {
	Next    *Item           `json:"next"`
	Blocked []StatusBlocked `json:"blocked"`
}

type StatusBlocked struct {
	ID        string   `json:"id"`
	WaitingOn []string `json:"waiting_on"`
}

func (c Client) Status(ctx context.Context, root string) (*Status, error) {
	var out Status
	if err := clirun.RunJSON(ctx, c.call("status", "--json", "--lean", "--root", root), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
