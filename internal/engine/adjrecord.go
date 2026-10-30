// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// AnswerRecordConfig is the answer explicitly given by the owner in a prompt.
type AnswerRecordConfig struct {
	Project               registry.Project
	ID                    int
	Option, Text, By, Via string
}

// ValidateRecordAnswer checks the caller's answer before reading adj.
func ValidateRecordAnswer(cfg AnswerRecordConfig) error {
	if strings.TrimSpace(cfg.By) == "" {
		return fmt.Errorf("ADJ_DECIDED_BY is unset; set it in the caller before answering in adj")
	}
	if cfg.ID <= 0 {
		return fmt.Errorf("record id must be positive")
	}
	if (cfg.Option == "") == (cfg.Text == "") {
		return fmt.Errorf("give exactly one of option and text")
	}
	if cfg.Via != "cli" && cfg.Via != "tui" {
		return fmt.Errorf("via must be cli or tui")
	}
	return nil
}

// AnswerRecord reads the current record before recording an owner answer.
func AnswerRecord(ctx context.Context, d Deps, cfg AnswerRecordConfig) (string, error) {
	if err := ValidateRecordAnswer(cfg); err != nil {
		return "", err
	}
	record, err := d.Adj.Show(ctx, cfg.Project.Path, cfg.ID)
	if err != nil {
		return "", err
	}
	if record.Status != "open" {
		return "", fmt.Errorf("adj record #%d is %s; only open records can be answered", cfg.ID, record.Status)
	}
	if cfg.Option != "" {
		found := false
		for _, opt := range record.Options {
			if opt.Label == cfg.Option {
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("adj record #%d is %s; option %q is not offered", cfg.ID, record.Status, cfg.Option)
		}
	}
	why := "Answered in ork (" + cfg.Via + ") on " + d.now().Format("2006-01-02") + ": " + record.Title
	w := runrec.Writer{Now: d.now, Sleep: d.Sleep, Backoff: d.Backoff}
	if cfg.Option != "" {
		err = d.Adj.DecideOption(ctx, w, cfg.Project.Path, cfg.ID, cfg.Option, why, cfg.By)
	} else {
		err = d.Adj.Decide(ctx, w, cfg.Project.Path, cfg.ID, cfg.Text, why, cfg.By)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("decided in adj as #%d", cfg.ID), nil
}

// AskRecordConfig is an owner question for a registered project's adj store.
type AskRecordConfig struct {
	Project registry.Project
	Spec    adjcli.AskSpec
}

// ValidateRecordAsk checks fields that do not require an adj read.
func ValidateRecordAsk(spec adjcli.AskSpec) error {
	if strings.TrimSpace(spec.Title) == "" {
		return fmt.Errorf("title must not be blank")
	}
	if strings.HasPrefix(spec.Title, "-") {
		return fmt.Errorf("title must not start with '-'; adj treats it as a flag")
	}
	if n := len(spec.Options); n != 0 && (n < 2 || n > 4) {
		return fmt.Errorf("give zero options, or two to four options")
	}
	if spec.Recommend != "" {
		found := false
		for _, opt := range spec.Options {
			if opt.Label == spec.Recommend {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("recommendation must name an option label")
		}
	}
	if spec.Reasoning != "" && spec.Recommend == "" {
		return fmt.Errorf("reasoning needs a recommendation")
	}
	return nil
}

// AskRecord checks an exact key before filing a new question through adj.
func AskRecord(ctx context.Context, d Deps, cfg AskRecordConfig) (int, error) {
	if err := ValidateRecordAsk(cfg.Spec); err != nil {
		return 0, err
	}
	w := runrec.Writer{Now: d.now, Sleep: d.Sleep, Backoff: d.Backoff}
	if cfg.Spec.Key != "" {
		state, id, err := d.Adj.Check(ctx, w, cfg.Project.Path, cfg.Spec.Key)
		if err != nil {
			return 0, err
		}
		if state != "not found" {
			return 0, fmt.Errorf("%s is %s as #%d; not asked", cfg.Spec.Key, state, id)
		}
	}
	return d.Adj.AskQuestion(ctx, w, cfg.Project.Path, cfg.Spec)
}
