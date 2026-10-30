// SPDX-License-Identifier: GPL-3.0-or-later

// Package adjcli makes typed calls over the adj CLI. Every call passes
// --root, and none reads the decisions directory.
package adjcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/runrec"
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

// write records every adj call made while filing an owner answer, including check.
func (c Client) write(ctx context.Context, w runrec.Writer, root string, argv ...string) (clirun.Result, error) {
	call := c.call()
	return w.Run(ctx, runrec.Cmd{Tool: "adj", Bin: call.Bin, Root: root, Argv: argv,
		Args: append(append([]string{}, argv...), "--root", root), Timeout: call.Timeout})
}

// Check finds an exact decision key. Substring matches from adj are ignored.
func (c Client) Check(ctx context.Context, w runrec.Writer, root, key string) (string, int, error) {
	res, err := c.write(ctx, w, root, "check", key, "--json")
	if err != nil {
		return "", 0, answerError(err)
	}
	var out struct{ Decided, Open, Closed []Record }
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return "", 0, fmt.Errorf("adj check: decode output: %w", err)
	}
	for _, group := range []struct {
		state   string
		records []Record
	}{{"decided", out.Decided}, {"open", out.Open}, {"closed", out.Closed}} {
		for _, r := range group.records {
			if r.Key == key {
				if r.ID <= 0 {
					return "", 0, fmt.Errorf("adj check: invalid record id")
				}
				return group.state, r.ID, nil
			}
		}
	}
	return "not found", 0, nil
}

// Ask files the full question and returns the ID printed by adj.
func (c Client) Ask(ctx context.Context, w runrec.Writer, root, title, key, ref, question, by string) (int, error) {
	res, err := c.write(ctx, w, root, "ask", title, "--key", key, "--ref", ref, "--context", question, "--by", by)
	if err != nil {
		return 0, answerError(err)
	}
	id, err := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("adj ask: no record id in the output")
	}
	return id, nil
}

// Decide records only the owner's typed answer, with explicit attribution.
func (c Client) Decide(ctx context.Context, w runrec.Writer, root string, id int, text, why, by string) error {
	_, err := c.write(ctx, w, root, "decide", strconv.Itoa(id), "--text", text, "--why", why, "--by", by)
	return answerError(err)
}

// answerError keeps plain adj diagnostics without exposing answer/context argv.
func answerError(err error) error {
	var exit *clirun.ExitError
	if errors.As(err, &exit) && exit.Tool == nil {
		return fmt.Errorf("adj: exit %d: %s", exit.Result.ExitCode, strings.TrimSpace(string(exit.Result.Stderr)))
	}
	return err
}

// Show reads one record through adj without accessing its store.
func (c Client) Show(ctx context.Context, root string, id int) (*Record, error) {
	var out Record
	if err := clirun.RunJSON(ctx, c.call("show", strconv.Itoa(id), "--json", "--root", root), &out); err != nil {
		return nil, answerError(err)
	}
	return &out, nil
}

// AskOption is an option and its optional tradeoffs for a new question.
type AskOption struct {
	Label     string `json:"label"`
	Summary   string `json:"summary"`
	Tradeoffs string `json:"tradeoffs,omitempty"`
}

// AskSpec holds the fields adj accepts when asking a question.
type AskSpec struct {
	Title, Key, Context, By, Recommend, Reasoning string
	Options                                       []AskOption
	Refs, Tags                                    []string
}

// AskQuestion files an owner question and returns the id adj prints.
func (c Client) AskQuestion(ctx context.Context, w runrec.Writer, root string, spec AskSpec) (int, error) {
	args := []string{"ask", spec.Title}
	for _, field := range []struct{ flag, value string }{{"--key", spec.Key}, {"--context", spec.Context}} {
		if field.value != "" {
			args = append(args, field.flag, field.value)
		}
	}
	for _, opt := range spec.Options {
		args = append(args, "--option", opt.Label+":"+opt.Summary)
	}
	for _, opt := range spec.Options {
		if opt.Tradeoffs != "" {
			args = append(args, "--tradeoffs", opt.Label+":"+opt.Tradeoffs)
		}
	}
	if spec.Recommend != "" {
		args = append(args, "--recommend", spec.Recommend)
	}
	if spec.Reasoning != "" {
		args = append(args, "--reasoning", spec.Reasoning)
	}
	for _, ref := range spec.Refs {
		args = append(args, "--ref", ref)
	}
	for _, tag := range spec.Tags {
		args = append(args, "--tag", tag)
	}
	if spec.By != "" {
		args = append(args, "--by", spec.By)
	}
	res, err := c.write(ctx, w, root, args...)
	if err != nil {
		return 0, answerError(err)
	}
	id, err := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("adj ask: no record id in the output")
	}
	return id, nil
}

// DecideOption records the option the owner explicitly confirmed.
func (c Client) DecideOption(ctx context.Context, w runrec.Writer, root string, id int, label, why, by string) error {
	_, err := c.write(ctx, w, root, "decide", strconv.Itoa(id), label, "--why", why, "--by", by)
	return answerError(err)
}
