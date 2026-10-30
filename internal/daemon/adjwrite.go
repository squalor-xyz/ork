// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/source"
)

func (d *Daemon) adjAnswer(ctx context.Context, p AdjAnswerParams) (*AdjAnswerResult, error) {
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	if !proj.Adj {
		return nil, badParams(fmt.Errorf("project %s does not use adj", p.Project))
	}
	cfg := engine.AnswerRecordConfig{Project: proj, ID: p.ID, Option: p.Option, Text: p.Text, By: p.By, Via: p.Via}
	if err := engine.ValidateRecordAnswer(cfg); err != nil {
		return nil, badParams(err)
	}
	message, err := engine.AnswerRecord(ctx, d.runDeps(proj), cfg)
	if err != nil {
		return nil, err
	}
	if err := d.cache.Refresh(ctx, proj, source.Open, source.Decided); err != nil {
		return nil, err
	}
	return &AdjAnswerResult{Message: message}, nil
}

func (d *Daemon) adjAsk(ctx context.Context, p AdjAskParams) (*AdjAskResult, error) {
	proj, err := d.registered(p.Project)
	if err != nil {
		return nil, err
	}
	if !proj.Adj {
		return nil, badParams(fmt.Errorf("project %s does not use adj", p.Project))
	}
	spec := adjcli.AskSpec{Title: p.Title, Key: p.Key, Context: p.Context, By: p.By, Options: p.Options, Recommend: p.Recommend, Reasoning: p.Reasoning, Refs: p.Refs, Tags: p.Tags}
	if err := engine.ValidateRecordAsk(spec); err != nil {
		return nil, badParams(err)
	}
	id, err := engine.AskRecord(ctx, d.runDeps(proj), engine.AskRecordConfig{Project: proj, Spec: spec})
	if err != nil {
		return nil, err
	}
	if err := d.cache.Refresh(ctx, proj, source.Open, source.Decided); err != nil {
		return nil, err
	}
	return &AdjAskResult{ID: id}, nil
}
