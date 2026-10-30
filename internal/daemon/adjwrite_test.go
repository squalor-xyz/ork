// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/adjcli"
	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/ipc"
	"github.com/squalor-xyz/ork/internal/registry"
)

func TestAdjMethodsDecideAskAndRefresh(t *testing.T) {
	for _, method := range []string{"adj.answer", "adj.ask"} {
		t.Run(method, func(t *testing.T) {
			d, _, p := runDaemon(t, nil, clitest.Route{Tool: "adj", Args: []string{"show"}, Prefix: true, Stdout: `{"id":12,"status":"open","title":"Which?","options":[{"label":"a"},{"label":"b"}]}`}, clitest.Route{Tool: "adj", Args: []string{"decide"}, Prefix: true}, clitest.Route{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stdout: "12\n"})
			p.Adj = true
			if err := registry.Save(d.registryPath(), &registry.File{Version: registry.Version, Projects: []registry.Project{p, {Name: "no-adj", Path: p.Path}}}); err != nil {
				t.Fatal(err)
			}
			d.runs.deps.Adj = adjcli.Client{Bin: lastFake.Bin("adj")}
			// Prime both parts so initialization cannot hide a missing refresh.
			if err := d.cache.Refresh(context.Background(), p, allParts...); err != nil {
				t.Fatal(err)
			}
			raw := `{"project":"p","title":"Title"}`
			if method == "adj.answer" {
				raw = `{"project":"p","id":12,"option":"a","by":"Jon","via":"cli"}`
			}
			v, err := d.handle(context.Background(), method, json.RawMessage(raw))
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(v)
			if !strings.Contains(string(b), "12") {
				t.Fatalf("result %s", b)
			}
			src := d.cfg.Source.(*stubSource)
			if src.count("open p") != 2 || src.count("decided p") != 2 {
				t.Fatal("did not refresh both decision parts")
			}
			for _, tc := range []struct{ project, extra, code string }{{"no-adj", "", CodeBadParams}, {"unknown", "", CodeNotFound}, {"p", `,"unexpected":true`, CodeBadParams}} {
				t.Run(tc.code+tc.project, func(t *testing.T) {
					bad := strings.Replace(raw, `"p"`, `"`+tc.project+`"`, 1)
					bad = strings.TrimSuffix(bad, "}") + tc.extra + "}"
					_, err := d.handle(context.Background(), method, json.RawMessage(bad))
					if e := ipc.ErrorOf(err); e == nil || e.Code != tc.code {
						t.Fatalf("error %v", err)
					}
				})
			}
			f := clitest.Install(t, clitest.Route{Tool: "adj", Args: []string{"show"}, Prefix: true, Stdout: `{"id":12,"status":"open","options":[{"label":"a"}]}`}, clitest.Route{Tool: "adj", Args: []string{"decide"}, Prefix: true, Stderr: "disk full", Exit: 1}, clitest.Route{Tool: "adj", Args: []string{"ask"}, Prefix: true, Stderr: "disk full", Exit: 1})
			d.runs.deps.Adj = adjcli.Client{Bin: f.Bin("adj")}
			_, err = d.handle(context.Background(), method, json.RawMessage(raw))
			if err == nil || err.Error() != "adj: exit 1: disk full" {
				t.Fatalf("error leaks argv: %v", err)
			}
			if src.count("open p") != 2 || src.count("decided p") != 2 {
				t.Fatal("refreshed failed write")
			}
		})
	}
}
