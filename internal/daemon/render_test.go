// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/runrec"
)

// run.render (S121, *proposed*) returns the text of one render of a run. The
// card lists names and sizes; the text is read from the run directory on
// demand, with the engine's guards.

const renderText = "Run engine  S1  awaiting-approval\nGate PASS (round 1)"

// renderRun stores a run that awaits approval with a card that lists the given
// renders, and writes the render files under its run directory.
func renderRun(t *testing.T, store *runrec.Store, project, id string, files map[string]string, listed ...string) {
	t.Helper()
	card := &runrec.ApprovalCard{Round: 1, Verdict: "PASS", UIRound: 1}
	for _, name := range listed {
		card.Renders = append(card.Renders, runrec.Render{Name: name, Size: len(files[name])})
	}
	if _, err := store.Create(runrec.Record{ID: id, Project: project, Item: "S1", State: runrec.StateAwaitingApproval, Card: card}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(store.RunDir(id), "tmp", "ui-1", "renders")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func renderParams(run, name string) json.RawMessage {
	b, _ := json.Marshal(RunAction{Run: run, Name: name})
	return b
}

func TestRunRenderReturnsTheTextOfOneRender(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	renderRun(t, store, p.Name, "r1", map[string]string{
		"launch-80x24.txt": renderText + "\x1b[31m\n\n", "other-80x24.txt": "other", "unlisted.txt": "unlisted",
	}, "launch-80x24.txt", "other-80x24.txt")
	var got RenderReply
	if err := json.Unmarshal(request(t, d, MethodRunRender, fmt.Sprintf(`{"run":"r1","name":"launch-80x24.txt"}`)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "launch-80x24.txt" || got.Text != renderText {
		t.Fatalf("reply %+v, want the plain text of the render", got)
	}
	if MethodRunRender != "run.render" {
		t.Fatalf("method %q", MethodRunRender)
	}
}

func TestRunRenderRefusesWhatTheCardDoesNotList(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	renderRun(t, store, p.Name, "r1", map[string]string{"a-80x24.txt": "a", "unlisted-80x24.txt": "u"}, "a-80x24.txt", "link-80x24.txt", "ghost-80x24.txt")
	if err := os.Symlink(secret, filepath.Join(store.RunDir("r1"), "tmp", "ui-1", "renders", "link-80x24.txt")); err != nil {
		t.Fatal(err)
	}
	// A run with no card, and a run whose card holds no renders.
	if _, err := store.Create(runrec.Record{ID: "nocard", Project: p.Name, Item: "S1", State: runrec.StatePaused}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(runrec.Record{ID: "norenders", Project: p.Name, Item: "S1", State: runrec.StateAwaitingApproval, Card: &runrec.ApprovalCard{Round: 1}}); err != nil {
		t.Fatal(err)
	}
	for name, params := range map[string]json.RawMessage{
		"unknown name":        renderParams("r1", "nope.txt"),
		"on disk, not listed": renderParams("r1", "unlisted-80x24.txt"),
		"listed, a link":      renderParams("r1", "link-80x24.txt"),
		"listed, missing":     renderParams("r1", "ghost-80x24.txt"),
		"a path":              renderParams("r1", "../renders/a-80x24.txt"),
		"an absolute path":    renderParams("r1", secret),
		"no name":             renderParams("r1", ""),
		"unknown run":         renderParams("nope", "a-80x24.txt"),
		"a run with no card":  renderParams("nocard", "a-80x24.txt"),
		"a card, no renders":  renderParams("norenders", "a-80x24.txt"),
		"a bad run id":        renderParams("../r1", "a-80x24.txt"),
	} {
		t.Run(name, func(t *testing.T) {
			out, err := d.handle(context.Background(), MethodRunRender, params)
			var re *ipc.Error
			if err == nil || !errors.As(err, &re) || re.Code != CodeNotFound {
				t.Fatalf("reply %v, error %v; want a not_found error", out, err)
			}
			if b, _ := json.Marshal(out); strings.Contains(string(b), "secret") {
				t.Fatalf("the secret leaked: %s", b)
			}
		})
	}
}

// Neither a run row on the wire nor the record carries render text.
func TestRunRowsCarryRenderNamesNotText(t *testing.T) {
	d, store, p := runDaemon(t, nil)
	renderRun(t, store, p.Name, "r1", map[string]string{"launch-80x24.txt": renderText}, "launch-80x24.txt")
	rows := d.engineRuns(p.Name)
	if len(rows) != 1 || rows[0].Card == nil || len(rows[0].Card.Renders) != 1 {
		t.Fatalf("rows %+v", rows)
	}
	wire, _ := json.Marshal(rows[0])
	if strings.Contains(string(wire), "Gate PASS") || !strings.Contains(string(wire), `"renders":[{"name":"launch-80x24.txt","size":`+fmt.Sprint(len(renderText))+`}]`) {
		t.Errorf("wire row: %s", wire)
	}
}
