// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewCommandFilesASlice(t *testing.T) {
	for _, mode := range []string{"text", "json", "existing"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			a := App{Input: strings.NewReader("Owner text\n"), Call: func(_ context.Context, method string, params, result any) error {
				calls++
				raw, _ := json.Marshal(params)
				var p map[string]any
				json.Unmarshal(raw, &p)
				if method != "item.new" || p["project"] != "P" || p["title"] != "Title" || p["why"] != "Owner text\n" || p["from"] != "S12" || p["size"] != "M" {
					t.Fatalf("call %s %s", method, raw)
				}
				existing := "false"
				if mode == "existing" {
					existing = "true"
				}
				return json.Unmarshal([]byte(`{"id":"S184","existing":`+existing+`}`), result)
			}}
			args := []string{"new", "P", "Title", "--why", "-", "--from", "S12", "--size", "M"}
			if mode == "json" {
				args = append(args, "--json")
			}
			var out, errout bytes.Buffer
			if code := a.Run(context.Background(), args, &out, &errout); code != 0 || calls != 1 {
				t.Fatalf("code=%d calls=%d err=%s", code, calls, &errout)
			}
			want := "filed S184\n"
			if mode == "existing" {
				want = "S184 is already filed\n"
			}
			if mode == "json" {
				var p map[string]any
				if err := json.Unmarshal(out.Bytes(), &p); err != nil || p["id"] != "S184" || p["existing"] != false || len(p) != 2 {
					t.Fatalf("json %s", &out)
				}
			} else if out.String() != want {
				t.Fatalf("output %q", out.String())
			}
		})
	}
}
func TestNewCommandUsage(t *testing.T) {
	calls := 0
	a := App{Call: func(context.Context, string, any, any) error { calls++; return nil }}
	for _, args := range [][]string{
		{"new", "P"}, {"new", "P", "Title", "--from-note", "1"}, {"new", "P", "Title", "--from-note", "x"}, {"new", "P", "Title", "--importance", "4"}, {"new", "P", "Title", "--urgency", "-1"}, {"new", "P", "Title", "--effort", "4"}, {"new", "P", "a\nb"}, {"new", "P", "Title", "--size", "XL"}, {"new", "P", "Title", "--from", "S12", "--from-note", "0"},
	} {
		var out, errout bytes.Buffer
		if code := a.Run(context.Background(), args, &out, &errout); code != 2 || !strings.Contains(errout.String(), "new:") || strings.Contains(errout.String(), "unknown command") {
			t.Errorf("args=%v code=%d", args, code)
		}
	}
	if calls != 0 {
		t.Fatalf("usage dialed daemon %d times", calls)
	}
}

func TestNewCommandFromNote(t *testing.T) {
	calls := 0
	a := App{Call: func(_ context.Context, method string, params, result any) error {
		calls++
		raw, _ := json.Marshal(params)
		var p map[string]any
		json.Unmarshal(raw, &p)
		if method != "item.new" || p["from"] != "S12" || p["from_note"] != float64(2) || p["importance"] != float64(3) || p["urgency"] != float64(2) || p["effort"] != float64(1) {
			t.Fatalf("params %s", raw)
		}
		return json.Unmarshal([]byte(`{"id":"S184","existing":false}`), result)
	}}
	var out, errout bytes.Buffer
	code := a.Run(context.Background(), []string{"new", "P", "Title", "--from", "S12", "--from-note", "2", "--importance", "3", "--urgency", "2", "--effort", "1"}, &out, &errout)
	if code != 0 || calls != 1 {
		t.Fatalf("code=%d calls=%d err=%s", code, calls, &errout)
	}
}

func TestNewCommandUsageMessages(t *testing.T) {
	calls := 0
	a := App{Call: func(context.Context, string, any, any) error { calls++; return nil }}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"new", "P", " "}, "new: title must be one non-blank line"},
		{[]string{"new", "P", "a\nb"}, "new: title must be one non-blank line"},
		{[]string{"new", "P", "Title", "--from-note", "0"}, "new: --from-note must be a positive number"},
		{[]string{"new", "P", "Title", "--from-note", "-1"}, "new: --from-note must be a positive number"},
		{[]string{"new", "P", "Title", "--from-note", "x"}, "new: --from-note must be a positive number"},
		{[]string{"new", "P", "Title", "--from-note", "2"}, "new: --from-note needs --from ITEM"},
	} {
		var out, errout bytes.Buffer
		if code := a.Run(context.Background(), tc.args, &out, &errout); code != 2 || !strings.Contains(errout.String(), tc.want) {
			t.Errorf("%v: code=%d error=%q want=%q", tc.args, code, errout.String(), tc.want)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input called daemon")
	}
}
