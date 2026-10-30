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

func TestEditAppendAddsAParagraphToASection(t *testing.T) {
	args := []string{"edit", "S130", "--section", "Implement", "--append", "--text", "Owner decisions (2026-10-26): A: yes.", "--render", "--json", "--lean", "--root", root}
	c, fake := client(t, clitest.Route{Tool: "slicer", Args: args, Stdout: `{"id":"S130"}`})
	w, sink := writer()
	if err := c.EditAppend(context.Background(), w, root, "S130", "Implement", "Owner decisions (2026-10-26): A: yes."); err != nil {
		t.Fatal(err)
	}
	if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, args) {
		t.Fatalf("calls %+v", calls)
	}
	if len(sink.writes) != 1 || sink.writes[0].Argv[0] != "edit" || sink.writes[0].Root != root {
		t.Errorf("writes %+v", sink.writes)
	}
}

func TestEditAppendRefusesEmptyText(t *testing.T) {
	c, fake := client(t)
	w, _ := writer()
	if err := c.EditAppend(context.Background(), w, root, "S130", "Implement", "  \n"); err == nil {
		t.Error("empty text would leave the section unchanged and say nothing")
	}
	if len(fake.Calls()) != 0 {
		t.Errorf("calls: %+v", fake.Calls())
	}
}

func TestAddItemArguments(t *testing.T) {
	for _, tc := range []struct {
		spec   AddSpec
		args   []string
		output string
		bad    bool
	}{
		{AddSpec{Title: "--Title", Key: "K", Size: "M", Importance: 2, Urgency: 2, Effort: 1}, []string{"add", "Title", "--key", "K", "--size", "M", "--importance", "2", "--urgency", "2", "--effort", "1"}, `{"id":"S184"}`, false},
		{AddSpec{Title: "Title"}, []string{"add", "Title"}, `{"id":"S184"}`, false},
		{AddSpec{Title: "Title", DiscoveredFrom: "S12"}, []string{"add", "Title", "--discovered-from", "S12"}, `{"id":"S184"}`, false},
		{AddSpec{Title: "Title"}, []string{"add", "Title"}, `{}`, true},
	} {
		c, fake := client(t, clitest.Route{Tool: "slicer", Args: []string{"add"}, Prefix: true, Stdout: tc.output})
		w, _ := writer()
		got, err := c.AddItem(context.Background(), w, root, tc.spec)
		if (err != nil) != tc.bad {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		want := append(tc.args, "--render", "--json", "--lean", "--root", root)
		if calls := fake.CallsTo("slicer"); len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, want) {
			t.Fatalf("calls=%v want=%v", calls, want)
		}
	}
}
