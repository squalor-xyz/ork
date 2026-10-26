// SPDX-License-Identifier: GPL-3.0-or-later

package slicercli

import (
	"context"
	"reflect"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
)

func TestAddFilesAnItemDiscoveredFromAnother(t *testing.T) {
	args := []string{"add", "Add a flag", "--discovered-from", "S04", "--key", "run-1-later-1", "--render", "--json", "--lean", "--root", root}
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: args, Stdout: `{"id":"S130","title":"Add a flag","existing":true,"has_slice":true}`})
	w, sink := writer()
	got, err := c.Add(context.Background(), w, root, "Add a flag", "S04", "run-1-later-1")
	if err != nil || got.ID != "S130" || !got.Existing || !got.HasSlice {
		t.Fatalf("%+v, %v", got, err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, args) {
		t.Fatalf("calls %+v", calls)
	}
	if len(sink.writes) != 1 || sink.writes[0].Argv[0] != "add" || sink.writes[0].Root != root {
		t.Errorf("writes %+v", sink.writes)
	}
}

func TestAddCutsALeadingDashFromTheTitle(t *testing.T) {
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"id":"S130"}`})
	w, _ := writer()
	if _, err := c.Add(context.Background(), w, root, "--json flag for status", "S04", "k"); err != nil {
		t.Fatal(err)
	}
	if got := fake.CallsTo("slicer")[0].Args[1]; got != "json flag for status" {
		t.Errorf("title %q is read as a flag", got)
	}
}

func TestAddNeedsAnIDInTheOutput(t *testing.T) {
	c, _ := client(t, clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: `{"title":"x"}`})
	w, _ := writer()
	if _, err := c.Add(context.Background(), w, root, "x", "S04", "k"); err == nil {
		t.Error("no id in the output is an error")
	}
}

func TestPromoteGivesTheItemTheOutline(t *testing.T) {
	args := []string{"promote", "S130", "--file", "/run/later-1.md", "--render", "--json", "--lean", "--root", root}
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: args, Stdout: `{"id":"S130"}`})
	w, sink := writer()
	if err := c.Promote(context.Background(), w, root, "S130", "/run/later-1.md"); err != nil {
		t.Fatal(err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, args) {
		t.Fatalf("calls %+v", calls)
	}
	if len(sink.writes) != 1 || sink.writes[0].Argv[0] != "promote" {
		t.Errorf("writes %+v", sink.writes)
	}
}
