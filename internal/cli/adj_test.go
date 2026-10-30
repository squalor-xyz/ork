// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
)

func TestAdjAnswerCommandConfirmsBeforeDeciding(t *testing.T) {
	for _, tc := range []struct {
		name, input, by string
		id              int
		extra           []string
		code, writes    int
	}{{"yes", "y\n", "Jon", 12, nil, 0, 1}, {"no", "n\n", "Jon", 12, nil, 0, 0}, {"enter", "\n", "Jon", 12, nil, 0, 0}, {"closed", "", "Jon", 12, nil, 0, 0}, {"missing", "y\n", "Jon", 13, nil, 1, 0}, {"by", "y\n", "", 12, nil, 1, 0}, {"yes-flag", "y\n", "Jon", 12, []string{"--yes"}, 2, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errout bytes.Buffer
			writes := 0
			a := App{Input: strings.NewReader(tc.input), Getenv: func(string) string { return tc.by }, Call: func(_ context.Context, method string, params, result any) error {
				if method == "snapshot" {
					return json.Unmarshal([]byte(`{"open":{"value":[{"id":12,"status":"open","title":"Which?","recommendation":{"label":"b"},"options":[{"label":"a","summary":"A"},{"label":"b","summary":"B"}]}]}}`), result)
				}
				if method != "adj.answer" {
					t.Fatalf("method %s", method)
				}
				writes++
				p := params.(daemon.AdjAnswerParams)
				if p.Project != "P" || p.ID != 12 || p.Option != "a" || p.By != "Jon" || p.Via != "cli" {
					t.Fatalf("params %+v", p)
				}
				if !strings.Contains(out.String(), "#12 Which?") || !strings.Contains(out.String(), "  a: A\n") || !strings.Contains(out.String(), "b: B (recommended)") || !strings.Contains(out.String(), "Decide #12 as a in adj as Jon? [y/N]") {
					t.Fatalf("not confirmed: %s", &out)
				}
				return json.Unmarshal([]byte(`{"message":"decided in adj as #12"}`), result)
			}}
			args := append([]string{"adj", "answer", "P", fmt.Sprint(tc.id), "--option", "a"}, tc.extra...)
			code := a.Run(context.Background(), args, &out, &errout)
			if code != tc.code || writes != tc.writes {
				t.Fatalf("code %d writes %d out %s err %s", code, writes, &out, &errout)
			}
			if code == 0 && writes == 0 && !strings.Contains(out.String(), "cancelled") {
				t.Fatal("missing cancellation")
			}
			if tc.by == "" && strings.Contains(out.String(), "[y/N]") {
				t.Fatal("prompt without owner")
			}
		})
	}
}
func TestAdjAskCommand(t *testing.T) {
	for _, js := range []bool{false, true} {
		t.Run(fmt.Sprint(js), func(t *testing.T) {
			calls := 0
			a := App{Getenv: func(k string) string {
				if k == "ADJ_ASKED_BY" {
					return "Asker"
				}
				return ""
			}, Call: func(_ context.Context, m string, p, r any) error {
				calls++
				b, _ := json.Marshal(p)
				var v daemon.AdjAskParams
				json.Unmarshal(b, &v)
				if m != "adj.ask" || v.Project != "P" || v.Title != "Title" || v.Key != "a.b" || len(v.Options) != 2 || v.Options[0].Label != "x" || v.Recommend != "x" || v.Reasoning != "R" || v.By != "Asker" {
					t.Fatalf("%s %s", m, b)
				}
				return json.Unmarshal([]byte(`{"id":12}`), r)
			}}
			args := []string{"adj", "ask", "P", "Title", "--key", "a.b", "--option", "x:X", "--option", "y:Y", "--recommend", "x", "--reasoning", "R"}
			if js {
				args = append(args, "--json")
			}
			var out, errout bytes.Buffer
			code := a.Run(context.Background(), args, &out, &errout)
			if code != 0 || calls != 1 {
				t.Fatalf("%d %s", code, &errout)
			}
			if js {
				var v map[string]int
				if err := json.Unmarshal(out.Bytes(), &v); err != nil || v["id"] != 12 || len(v) != 1 {
					t.Fatalf("JSON %s", &out)
				}
			} else if !strings.Contains(out.String(), "12") {
				t.Fatal(&out)
			}
		})
	}
	t.Run("known key", func(t *testing.T) {
		a := App{Call: func(context.Context, string, any, any) error { return fmt.Errorf("a.b is open as #9") }}
		var out, errout bytes.Buffer
		if code := a.Run(context.Background(), []string{"adj", "ask", "P", "Title", "--key", "a.b"}, &out, &errout); code != 1 || !strings.Contains(errout.String(), "#9") {
			t.Fatalf("%d %s", code, &errout)
		}
	})
	t.Run("malformed option", func(t *testing.T) {
		a := App{Call: func(context.Context, string, any, any) error { t.Fatal("dialed"); return nil }}
		var out, errout bytes.Buffer
		if code := a.Run(context.Background(), []string{"adj", "ask", "P", "Title", "--option", "bad"}, &out, &errout); code != 2 {
			t.Fatalf("%d %s", code, &errout)
		}
	})
}

func TestAdjAnswerTypedJSON(t *testing.T) {
	var out, errout bytes.Buffer
	writes := 0
	a := App{Input: strings.NewReader("YES\n"), Getenv: func(string) string { return "Jon" }, Call: func(_ context.Context, m string, p, r any) error {
		if m == daemon.MethodSnapshot {
			return json.Unmarshal([]byte(`{"open":{"value":[{"id":12,"title":"Question","status":"open"}]}}`), r)
		}
		writes++
		answer := p.(daemon.AdjAnswerParams)
		if answer.Text != "typed answer" || answer.Option != "" {
			t.Fatalf("answer %+v", answer)
		}
		if !strings.Contains(errout.String(), `Decide #12 as "typed answer" in adj as Jon? [y/N]`) {
			t.Fatalf("prompt %s", &errout)
		}
		return json.Unmarshal([]byte(`{"message":"decided in adj as #12"}`), r)
	}}
	code := a.Run(context.Background(), []string{"adj", "answer", "P", "12", "--text", "typed answer", "--json"}, &out, &errout)
	var result daemon.AdjAnswerResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || code != 0 || writes != 1 || result.Message != "decided in adj as #12" {
		t.Fatalf("code %d writes %d out %s err %s", code, writes, &out, &errout)
	}
}

func TestAdjAskFieldsAndLocalValidation(t *testing.T) {
	for _, by := range []string{"Jon", ""} {
		t.Run("fields "+by, func(t *testing.T) {
			a := App{Input: strings.NewReader("context\n"), Getenv: func(k string) string {
				if k == "ADJ_DECIDED_BY" {
					return by
				}
				return ""
			}, Call: func(_ context.Context, m string, p, r any) error {
				v := p.(daemon.AdjAskParams)
				if m != daemon.MethodAdjAsk || v.Title != "Title" || v.Context != "context\n" || v.By != by || len(v.Refs) != 2 || strings.Join(v.Refs, ",") != "S1,S2" || len(v.Tags) != 2 || v.Options[0].Tradeoffs != "T:detail" {
					t.Fatalf("fields %+v", v)
				}
				return json.Unmarshal([]byte(`{"id":12}`), r)
			}}
			var out, errout bytes.Buffer
			code := a.Run(context.Background(), []string{"adj", "ask", "P", "Title", "--context", "-", "--option", "a:A", "--option", "b:B", "--tradeoffs", "a:T:detail", "--ref", "S1", "--ref", "S2", "--tag", "one", "--tag", "two"}, &out, &errout)
			if code != 0 {
				t.Fatalf("%d %s", code, &errout)
			}
		})
	}
	for _, flags := range [][]string{{"--option", "a:A"}, {"--recommend", "missing"}, {"--reasoning", "why"}, {"--option", "a:A", "--option", "a:B"}, {"--tradeoffs", "missing:T"}, {"--tradeoffs", "bad"}} {
		a := App{Call: func(context.Context, string, any, any) error { t.Fatal("dialed on invalid args"); return nil }}
		var out, errout bytes.Buffer
		if code := a.Run(context.Background(), append([]string{"adj", "ask", "P", "Title"}, flags...), &out, &errout); code != 2 || !strings.Contains(errout.String(), "adj ask:") {
			t.Fatalf("%v code %d err %s", flags, code, &errout)
		}
	}
}

func TestAdjAskRefusesLeadingDashTitleBeforeDialing(t *testing.T) {
	for _, title := range []string{"- title", "-h", "-"} {
		t.Run(title, func(t *testing.T) {
			calls := 0
			a := App{Call: func(context.Context, string, any, any) error { calls++; return nil }}
			var out, errout bytes.Buffer
			code := a.Run(context.Background(), []string{"adj", "ask", "P", title}, &out, &errout)
			if code != 2 || calls != 0 || !strings.Contains(errout.String(), "adj ask: title must not start with '-'") {
				t.Fatalf("code %d calls %d out %s err %s", code, calls, &out, &errout)
			}
		})
	}
}

func TestAdjAnswerLocalRefusalCodes(t *testing.T) {
	for _, tc := range []struct {
		name, by, option, code, message string
		id, calls                       int
	}{
		{"owner", "", "a", "bad_params", "ADJ_DECIDED_BY is unset", 12, 0},
		{"option", "Jon", "wrong", "bad_params", `has no option "wrong"`, 12, 1},
		{"record", "Jon", "a", "not_found", "not in project P's open records", 13, 1},
	} {
		for _, js := range []bool{false, true} {
			t.Run(fmt.Sprint(tc.name, js), func(t *testing.T) {
				calls := 0
				a := App{Getenv: func(string) string { return tc.by }, Input: strings.NewReader("y\n"), Call: func(_ context.Context, m string, p, r any) error {
					calls++
					if m != daemon.MethodSnapshot {
						t.Fatalf("unexpected write %s", m)
					}
					return json.Unmarshal([]byte(`{"open":{"value":[{"id":12,"title":"Which?","status":"open","options":[{"label":"a","summary":"A"}]}]}}`), r)
				}}
				args := []string{"adj", "answer", "P", fmt.Sprint(tc.id), "--option", tc.option}
				if js {
					args = append(args, "--json")
				}
				var out, errout bytes.Buffer
				code := a.Run(context.Background(), args, &out, &errout)
				if code != 1 || calls != tc.calls || strings.Contains(errout.String(), "[y/N]") || strings.Contains(out.String(), "[y/N]") {
					t.Fatalf("code %d calls %d out %s err %s", code, calls, &out, &errout)
				}
				message := errout.String()
				if js {
					var result struct {
						Error struct{ Code, Message string }
					}
					if err := json.Unmarshal(out.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result.Error.Code != tc.code {
						t.Errorf("code %s want %s", result.Error.Code, tc.code)
					}
					message = result.Error.Message
					if errout.Len() != 0 {
						t.Errorf("stderr %s", &errout)
					}
				}
				if !strings.Contains(message, "adj answer: ") || !strings.Contains(message, tc.message) {
					t.Errorf("message %q", message)
				}
			})
		}
	}
}

func TestAdjAnswerJSONCancellation(t *testing.T) {
	for _, input := range []string{"n\n", "\n", ""} {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			a := App{Getenv: func(string) string { return "Jon" }, Input: strings.NewReader(input), Call: func(_ context.Context, m string, p, r any) error {
				if m != daemon.MethodSnapshot {
					t.Fatalf("unexpected write %s", m)
				}
				return json.Unmarshal([]byte(`{"open":{"value":[{"id":12,"title":"Which?","status":"open","options":[{"label":"a","summary":"A"}]}]}}`), r)
			}}
			var out, errout bytes.Buffer
			code := a.Run(context.Background(), []string{"adj", "answer", "P", "12", "--option", "a", "--json"}, &out, &errout)
			var result map[string]bool
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || code != 0 || len(result) != 1 || !result["cancelled"] {
				t.Fatalf("code %d out %s err %s", code, &out, &errout)
			}
			if !strings.Contains(errout.String(), "Decide #12 as a in adj as Jon? [y/N]") {
				t.Fatalf("prompt %s", &errout)
			}
		})
	}
}
