// SPDX-License-Identifier: GPL-3.0-or-later

package adjcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clirun"
	"github.com/squalor-xyz/ork/internal/clitest"
)

func TestMain(m *testing.M) {
	clitest.Main()
	os.Exit(m.Run())
}

const root = "/projects/ork"

func recorded(name string) string {
	return filepath.Join("..", "..", "testdata", "cli", "adj", name)
}

func client(t *testing.T, routes ...clitest.Route) Client {
	t.Helper()
	return Client{Bin: clitest.Install(t, routes...).Bin("adj")}
}

func TestInboxNonEmpty(t *testing.T) {
	c := client(t, clitest.Route{Tool: "adj", Args: []string{"inbox", "--json", "--root", root}, File: recorded("inbox-nonempty.json")})
	recs, err := c.Inbox(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records", len(recs))
	}
	r := recs[0]
	if r.ID != 11 || r.Status != "open" || r.Key != "ui.slicer-tui" || r.AskedBy != "claude" ||
		r.Created != "2026-10-18" || !strings.HasPrefix(r.Title, "Does ork reimplement") {
		t.Fatalf("first record: %+v", r)
	}
	if len(r.Options) < 2 || r.Options[0].Label != "cli-editor" || r.Options[0].Summary == "" {
		t.Fatalf("options: %+v", r.Options)
	}
	if r.Recommendation == nil || r.Recommendation.Label == "" || r.Recommendation.Reasoning == "" {
		t.Fatalf("recommendation: %+v", r.Recommendation)
	}
	if len(r.History) == 0 || r.History[0].Event != "ask" {
		t.Fatalf("history: %+v", r.History)
	}
	if recs[1].ID != 51 || recs[1].Key != "vendor.codex-ratelimit" {
		t.Fatalf("second record: %+v", recs[1])
	}
}

func TestInboxEmpty(t *testing.T) {
	c := client(t, clitest.Route{Tool: "adj", Args: []string{"inbox", "--json", "--root", root}, File: recorded("inbox-empty.json")})
	recs, err := c.Inbox(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("got %d records", len(recs))
	}
}

func TestList(t *testing.T) {
	c := client(t, clitest.Route{Tool: "adj", Args: []string{"list", "--json", "--root", root}, File: recorded("list.json")})
	recs, err := c.List(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 55 {
		t.Fatalf("got %d records", len(recs))
	}
	first := recs[0]
	if first.ID != 1 || first.Status != "decided" || first.Key != "scope.v1" || first.DecidedBy != "jon" ||
		first.DecidedAt != "2026-10-18" || first.Decision == "" || first.Rationale == "" || first.Context == "" {
		t.Fatalf("first record: %+v", first)
	}
	for i := 1; i < len(recs); i++ {
		if recs[i].ID <= recs[i-1].ID {
			t.Fatalf("not in ID order at %d", i)
		}
	}
	var withRefs bool
	for _, r := range recs {
		if len(r.Refs) > 0 {
			withRefs = true
		}
	}
	if !withRefs {
		t.Fatal("no record kept its refs")
	}
}

func TestListStatusFilterIsPassed(t *testing.T) {
	c := client(t, clitest.Route{Tool: "adj", Args: []string{"list", "--json", "--status", "decided", "--root", root}, Stdout: `{"records": [{"id": 9, "status": "decided", "future": 1}], "more": true}`})
	recs, err := c.List(context.Background(), root, "decided")
	if err != nil || len(recs) != 1 || recs[0].ID != 9 {
		t.Fatalf("List: %+v, %v", recs, err)
	}
}

func TestErrorKeepsOutput(t *testing.T) {
	msg := "adj inbox: no repo root found: no .adjudicate.json in /x\n"
	c := client(t, clitest.Route{Tool: "adj", Args: []string{"inbox", "--json", "--root", root}, Stderr: msg, Exit: 1})
	_, err := c.Inbox(context.Background(), root)
	var ee *clirun.ExitError
	if !errors.As(err, &ee) || ee.Result.ExitCode != 1 || string(ee.Result.Stderr) != msg {
		t.Fatalf("error: %v", err)
	}
}
