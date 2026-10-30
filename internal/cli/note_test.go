// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func noteApp(t *testing.T, text string, calls *int) App {
	t.Helper()
	return App{Call: func(_ context.Context, method string, params, result any) error {
		*calls++
		b, _ := json.Marshal(params)
		var p map[string]string
		json.Unmarshal(b, &p)
		if method != "item.note" || p["project"] != "P" || p["item"] != "S12" || p["text"] != text {
			t.Fatalf("call %s %s", method, b)
		}
		return json.Unmarshal([]byte(`{"item":"S12"}`), result)
	}}
}
func TestNoteCommandAddsANote(t *testing.T) {
	for _, js := range []bool{false, true} {
		calls := 0
		a := noteApp(t, "text", &calls)
		args := []string{"note", "P", "S12", "text"}
		if js {
			args = append(args, "--json")
		}
		var out, errout bytes.Buffer
		code := a.Run(context.Background(), args, &out, &errout)
		want := "noted S12\n"
		if js {
			var v map[string]any
			if err := json.Unmarshal(out.Bytes(), &v); err != nil || v["item"] != "S12" || v["noted"] != true || len(v) != 2 {
				t.Fatalf("JSON %s", &out)
			}
			want = out.String()
		}
		if code != 0 || out.String() != want || calls != 1 {
			t.Fatalf("code=%d out=%q err=%q calls=%d", code, out.String(), errout.String(), calls)
		}
	}
}
func TestNoteCommandReadsStdinForDash(t *testing.T) {
	calls := 0
	a := noteApp(t, "- text", &calls)
	a.Input = strings.NewReader("- text\n")
	var out, errout bytes.Buffer
	if code := a.Run(context.Background(), []string{"note", "P", "S12", "-"}, &out, &errout); code != 0 || calls != 1 {
		t.Fatalf("code=%d err=%s calls=%d", code, &errout, calls)
	}
}
func TestNoteCommandUsage(t *testing.T) {
	calls := 0
	a := noteApp(t, "", &calls)
	for _, args := range [][]string{{"note"}, {"note", "P", "S12"}, {"note", "P", "S12", "a", "b"}, {"note", "P", "S12", " \t"}, {"note", "P", "S12", "-"}} {
		a.Input = strings.NewReader("\n")
		var out, errout bytes.Buffer
		if code := a.Run(context.Background(), args, &out, &errout); code != 2 {
			t.Errorf("%v: code=%d", args, code)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid input called daemon %d times", calls)
	}
}

func TestNoteCommandLeadingDashText(t *testing.T) {
	for _, tc := range []struct {
		text string
		args []string
	}{
		{"- text", []string{"note", "P", "S12", "- text"}},
		{"-x", []string{"note", "P", "S12", "--", "-x"}},
		{"- text", []string{"note", "P", "S12", "- text", "--json"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			calls := 0
			a := noteApp(t, tc.text, &calls)
			var out, errout bytes.Buffer
			if code := a.Run(context.Background(), tc.args, &out, &errout); code != 0 || calls != 1 {
				t.Fatalf("code=%d calls=%d err=%s", code, calls, &errout)
			}
		})
	}
}
func TestNoteCommandBlankItem(t *testing.T) {
	calls := 0
	a := App{Call: func(context.Context, string, any, any) error { calls++; return nil }}
	for _, item := range []string{"", " \t"} {
		var out, errout bytes.Buffer
		if code := a.Run(context.Background(), []string{"note", "P", item, "text"}, &out, &errout); code != 2 || calls != 0 {
			t.Errorf("code=%d calls=%d err=%s", code, calls, &errout)
		}
	}
}
func TestNoteCommandUsageMessages(t *testing.T) {
	a := App{}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"note"}, "note: takes PROJECT ITEM TEXT, got 0 arguments; quote TEXT"},
		{[]string{"note", "P", "S12", "a", "b"}, "note: takes PROJECT ITEM TEXT, got 4 arguments; quote TEXT"},
		{[]string{"note", "P", "S12", "  "}, "Note must not be empty"},
	} {
		var out, errout bytes.Buffer
		if code := a.Run(context.Background(), tc.args, &out, &errout); code != 2 || !strings.Contains(errout.String(), tc.want) {
			t.Errorf("code=%d err=%q want=%q", code, errout.String(), tc.want)
		}
	}
}
