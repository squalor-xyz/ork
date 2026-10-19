// SPDX-License-Identifier: GPL-3.0-or-later

// Package adjcli makes typed calls over the adj CLI. Every call passes
// --root, and none reads the decisions directory.
package adjcli

import (
	"context"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// Option is one answer a question offers.
type Option struct {
	Label     string `json:"label"`
	Summary   string `json:"summary"`
	Tradeoffs string `json:"tradeoffs"`
}

// Recommendation is the asker's recommendation: a Label naming one option,
// or free Text, with Reasoning.
type Recommendation struct {
	Label     string `json:"label"`
	Text      string `json:"text"`
	Reasoning string `json:"reasoning"`
}

// Event is one entry of a record's history.
type Event struct {
	Date  string `json:"date"`
	Event string `json:"event"`
}

// Record is one adj record, as `adj list --json` and `adj inbox --json`
// print it. Keys a record does not have are the zero value.
type Record struct {
	ID             int             `json:"id"`
	Title          string          `json:"title"`
	Status         string          `json:"status"`
	Key            string          `json:"key"`
	Tags           []string        `json:"tags"`
	Refs           []string        `json:"refs"`
	AskedBy        string          `json:"asked_by"`
	DecidedBy      string          `json:"decided_by"`
	Created        string          `json:"created"`
	DecidedAt      string          `json:"decided_at"`
	Context        string          `json:"context"`
	Options        []Option        `json:"options"`
	Recommendation *Recommendation `json:"recommendation"`
	Decision       string          `json:"decision"`
	Rationale      string          `json:"rationale"`
	History        []Event         `json:"history"`
}

// Client calls the adj CLI.
type Client struct {
	Bin     string        // binary name or path; empty means "adj" found on PATH
	Timeout time.Duration // per-call deadline; zero means clirun.DefaultTimeout
}

func (c Client) call(args ...string) clirun.Call {
	bin := c.Bin
	if bin == "" {
		bin = "adj"
	}
	return clirun.Call{Bin: bin, Args: args, Timeout: c.Timeout}
}

func (c Client) records(ctx context.Context, args ...string) ([]Record, error) {
	var out struct {
		Records []Record `json:"records"`
	}
	if err := clirun.RunJSON(ctx, c.call(args...), &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}

// Inbox runs `adj inbox --json --root ROOT`: the open questions, oldest
// first. An empty inbox is an empty slice, not an error.
func (c Client) Inbox(ctx context.Context, root string) ([]Record, error) {
	return c.records(ctx, "inbox", "--json", "--root", root)
}

// List runs `adj list --json [--status STATUS] --root ROOT`, in ID order. An
// empty status lists every record.
func (c Client) List(ctx context.Context, root, status string) ([]Record, error) {
	args := []string{"list", "--json"}
	if status != "" {
		args = append(args, "--status", status)
	}
	args = append(args, "--root", root)
	return c.records(ctx, args...)
}
