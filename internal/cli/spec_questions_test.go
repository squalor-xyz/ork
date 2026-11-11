// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
)

const batchQuestions = `{"items":[{"item":"S1","ready":false,"questions":[
{"ask":{"title":"Store?","options":[{"label":"file","summary":"on disk"},{"label":"memory","summary":"in RAM"}],"recommend":2,"reason":"fast"},"key":"k1","text":"Store block"},
{"ask":{"title":"Flag?","options":[{"label":"long","summary":"--extra"}],"recommend":1,"reason":"clear"},"key":"k2","text":"Flag block"},
{"ask":null,"key":"k3","text":"Describe the limit"}]}]}`

func TestBatchQuestionsListing(t *testing.T) {
	for _, wave := range []bool{false, true} {
		for _, jsonMode := range []bool{false, true} {
			t.Run(stringName(wave, jsonMode), func(t *testing.T) {
				flags := []string{"--questions"}
				if jsonMode {
					flags = append(flags, "--json")
				}
				code, out, errs, stub := batchCommand(t, wave, batchQuestions, flags...)
				if code != ExitWaitQuestion || len(stub.answers) != 0 || len(stub.started) != 0 {
					t.Fatalf("listing must exit 3 without writing or starting: exit %d answers %+v started %v: %s %s", code, stub.answers, stub.started, out, errs)
				}
				if !reflect.DeepEqual(stub.calls, []string{daemon.MethodSpecCheck}) {
					t.Fatalf("--questions must run only the spec check: %v", stub.calls)
				}
				if jsonMode {
					var got struct {
						Questions []listedQuestion `json:"questions"`
						Answered  []any            `json:"answered"`
					}
					var check daemon.SpecCheckResult
					_ = json.Unmarshal([]byte(batchQuestions), &check)
					if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Questions) != 3 || got.Answered == nil || len(got.Answered) != 0 {
						t.Fatalf("question JSON: %s (%v)", out, err)
					}
					for i, q := range got.Questions {
						address := []string{"k1", "k2", "k3"}[i]
						if wave {
							address = "S1:" + address
						}
						if q.Item != "S1" || q.Address != address || !reflect.DeepEqual(q.SpecQuestion, check.Items[0].Questions[i]) {
							t.Errorf("question item/address/block: %+v; want S1 %s", q, address)
						}
					}
				} else {
					for _, text := range []string{"[S1 1/3] Store?", "1. file: on disk", "2. memory: in RAM (recommended)", "Why 2: fast", "Decision key: k1", "[S1 2/3] Flag?", "1. long: --extra (recommended)", "Why 1: clear", "Decision key: k2", "[S1 3/3] Describe the limit", "Decision key: k3"} {
						if !strings.Contains(out, text) {
							t.Errorf("missing %q: %s", text, out)
						}
					}
				}
			})
		}
	}
}

type listedQuestion struct {
	Item    string `json:"item"`
	Address string `json:"address"`
	engine.SpecQuestion
}

func TestBatchQuestionAddresses(t *testing.T) {
	for _, wave := range []bool{false, true} {
		for _, mode := range []string{"listing", "accepted", "missing"} {
			t.Run(fmt.Sprintf("wave=%t/%s", wave, mode), func(t *testing.T) {
				check := strings.ReplaceAll(batchQuestions, `"key":"k3",`, "")
				prefix := ""
				items := []string{"S1"}
				want := [][2]string{{"S1", "k1"}, {"S1", "k2"}, {"S1", "#3"}}
				if wave {
					prefix = "S1:"
					items = append(items, "S2")
					check = strings.TrimSuffix(check, `]}`) + `,{"item":"S2","ready":false,"questions":[{"ask":null,"key":"k1","text":"Other item"}]}]}`
					want = [][2]string{{"S1", "S1:k1"}, {"S1", "S1:k2"}, {"S1", "S1:#3"}, {"S2", "S2:k1"}}
				}
				flags := []string{"--json", "--questions"}
				wantCode := ExitWaitQuestion
				if mode != "listing" {
					flags = []string{"--json", "--yes", "--answer", prefix + "k1=2"}
					if mode == "accepted" {
						flags = append(flags, "--answer", prefix+"k2=1", "--answer", prefix+"#3=text")
						if wave {
							flags = append(flags, "--answer", "S2:k1=other")
						}
						wantCode = ExitOK
					}
				}
				var code int
				var out, errs string
				if wave {
					stub := &specStub{t: t, check: check}
					code, out, errs = runWaveItems(t, stub, "", items, flags...)
				} else {
					code, out, errs, _ = batchCommand(t, false, check, flags...)
				}
				if code != wantCode {
					t.Fatalf("exit %d: %s %s", code, out, errs)
				}
				var got struct {
					Questions []listedQuestion `json:"questions"`
				}
				if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Questions) != len(want) {
					t.Fatalf("question JSON: %s (%v)", out, err)
				}
				for i, q := range got.Questions {
					if q.Item != want[i][0] || q.Address != want[i][1] {
						t.Errorf("question address %d: %s %s; want %v", i, q.Item, q.Address, want[i])
					}
				}
			})
		}
	}
}

func TestBatchListingAddressHints(t *testing.T) {
	check := strings.ReplaceAll(batchQuestions, `"key":"k3",`, "")
	for _, wave := range []bool{false, true} {
		t.Run(fmt.Sprintf("wave=%t", wave), func(t *testing.T) {
			_, out, _, _ := batchCommand(t, wave, check, "--questions")
			want := []string{"Decision key: k1", "Answer as: #3"}
			if wave {
				want = []string{"Decision key: k1 (answer as S1:k1)", "Answer as: S1:#3"}
			}
			for _, line := range want {
				if !strings.Contains(out, line) {
					t.Errorf("missing address hint %q: %s", line, out)
				}
			}
			if strings.Contains(out, "Decision key: #3") {
				t.Errorf("position labeled as decision key: %s", out)
			}
		})
	}
}

func TestBatchWaveUnknownKeyExplainsPrefix(t *testing.T) {
	_, _, errs, _ := batchCommand(t, true, batchQuestions, "--answer", "k1=1", "--yes")
	if !strings.Contains(errs, "no open question matches k1 (wave answers use ITEM:KEY)") {
		t.Fatalf("missing wave address guidance: %s", errs)
	}
}

func TestBatchListingNoOpenQuestions(t *testing.T) {
	for _, wave := range []bool{false, true} {
		t.Run(fmt.Sprintf("wave=%t", wave), func(t *testing.T) {
			code, out, errs, _ := batchCommand(t, wave, `{"items":[{"item":"S1","ready":true}]}`, "--questions")
			if code != ExitOK || !strings.HasSuffix(out, "no open spec questions: S1\n") {
				t.Fatalf("missing empty-listing result: exit %d: %s %s", code, out, errs)
			}
		})
	}
	stub := &specStub{t: t, check: `{"items":[{"item":"S1","ready":true},{"item":"S2","ready":true}]}`}
	_, out, _ := runWaveItems(t, stub, "", []string{"S1", "S2"}, "--questions")
	if !strings.HasSuffix(out, "no open spec questions: S1, S2\n") {
		t.Fatalf("missing empty-wave result: %s", out)
	}
}

func TestBatchEmptySavedWaveListing(t *testing.T) {
	stub := &savedStub{t: t, list: savedList}
	code, out, errs := stub.run("wave", "start", "solo", "--questions")
	if code != ExitOK || !strings.HasSuffix(out, "no open spec questions: solo\n") {
		t.Fatalf("missing saved-wave listing result: exit %d: %s %s", code, out, errs)
	}
}

func TestBatchEmptySavedWaveRefusesAddressOnly(t *testing.T) {
	stub := &savedStub{t: t, list: savedList}
	code, out, errs := stub.run("wave", "start", "solo", "--answer", "S1:k1=custom text")
	if code != ExitUsage || !strings.Contains(errs, "no open question matches S1:k1") || strings.Contains(out+errs, "custom text") {
		t.Fatalf("refusal must name only the address: exit %d: %s %s", code, out, errs)
	}
}

func stringName(wave, jsonMode bool) string {
	return map[bool]string{false: "run", true: "wave"}[wave] + "/" + map[bool]string{false: "text", true: "json"}[jsonMode]
}

func batchCommand(t *testing.T, wave bool, check string, flags ...string) (int, string, string, *specStub) {
	t.Helper()
	stub := &specStub{t: t, check: check}
	var out, errs bytes.Buffer
	app := &App{Input: strings.NewReader(""), Call: func(ctx context.Context, method string, params, result any) error {
		if method == daemon.MethodRunStart {
			stub.calls = append(stub.calls, method)
			stub.started = append(stub.started, params.(daemon.RunParams).Item)
			return json.Unmarshal([]byte(`{"id":"r1","state":"setup"}`), result)
		}
		return stub.call(ctx, method, params, result)
	}}
	args := []string{"run", "p", "S1"}
	if wave {
		args = []string{"wave", "start", "p", "S1"}
	}
	code := app.Run(context.Background(), append(args, flags...), &out, &errs)
	return code, out.String(), errs.String(), stub
}

func TestBatchAnswersAccepted(t *testing.T) {
	for _, wave := range []bool{false, true} {
		for _, keyless := range []bool{false, true} {
			check := batchQuestions
			last := "k3"
			if keyless {
				check = strings.ReplaceAll(check, `"key":"k3",`, "")
				check = strings.Replace(check, `"summary":"in RAM"`, `"summary":"in  RAM"`, 1)
				last = "#3"
			}
			prefix := ""
			if wave {
				prefix = "S1:"
			}
			code, out, errs, stub := batchCommand(t, wave, check, "--answer", prefix+last+"=custom limit", "--answer", prefix+"k2=1", "--answer", prefix+"k1=2", "--yes", "--json")
			want := []string{"memory: in RAM", "long: --extra", "custom limit"}
			if code != ExitOK || len(stub.answers) != 1 || !reflect.DeepEqual(stub.answers[0].Answers, want) || !reflect.DeepEqual(stub.started, []string{"S1"}) {
				t.Fatalf("keyed answers must record in question order and start: exit %d answers %+v started %v: %s %s", code, stub.answers, stub.started, out, errs)
			}
			var got struct {
				Questions []engine.SpecQuestion                `json:"questions"`
				Answered  []struct{ Item, Key, Answer string } `json:"answered"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Questions) != 3 || len(got.Answered) != 3 {
				t.Fatalf("answer JSON: %s (%v)", out, err)
			}
			for i, key := range []string{"k1", "k2", last} {
				if got.Answered[i].Item != "S1" || got.Answered[i].Key != key || got.Answered[i].Answer != want[i] {
					t.Fatalf("accepted answer %d: %+v", i, got.Answered[i])
				}
			}
		}
	}
}

func TestBatchAnswersRefused(t *testing.T) {
	for _, tc := range []struct {
		name, check, key string
		flags            []string
		code             int
	}{
		{"missing", batchQuestions, "k2, k3", []string{"--answer", "k1=2", "--yes"}, ExitWaitQuestion},
		{"unknown", batchQuestions, "unknown", []string{"--answer", "unknown=1", "--yes"}, ExitUsage},
		{"range", batchQuestions, "k1", []string{"--answer", "k1=3", "--yes"}, ExitUsage},
		{"overflow", batchQuestions, "k1", []string{"--answer", "k1=999999999999999999999999999999", "--yes"}, ExitUsage},
		{"duplicate", batchQuestions, "k1", []string{"--answer", "k1=1", "--answer", "k1=2", "--yes"}, ExitUsage},
		{"empty", batchQuestions, "k1", []string{"--answer", "k1=   ", "--yes"}, ExitUsage},
		{"no pending", `{"items":[{"item":"S1","ready":true}]}`, "k1", []string{"--answer", "k1=1", "--yes"}, ExitUsage},
		{"questions yes", batchQuestions, "--yes", []string{"--questions", "--yes"}, ExitUsage},
		{"questions answer", batchQuestions, "--answer", []string{"--questions", "--answer", "k1=1"}, ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errs, stub := batchCommand(t, false, tc.check, tc.flags...)
			if code != tc.code || !strings.Contains(out+errs, tc.key) || len(stub.answers) != 0 || len(stub.started) != 0 {
				t.Fatalf("must refuse %s before recording: exit %d answers %+v started %v: %s %s", tc.key, code, stub.answers, stub.started, out, errs)
			}
		})
	}
}

func TestBatchWaveValidatesAllItemsBeforeRecording(t *testing.T) {
	check := strings.TrimSuffix(batchQuestions, `]}`) + `,{"item":"S2","ready":false,"questions":[{"ask":null,"key":"k1","text":"Other item"}]}]}`
	for _, answer := range []string{"S2:k1=", "S2:unknown=1", "k1=1"} {
		stub := &specStub{t: t, check: check}
		code, out, errs := runWaveItems(t, stub, "", []string{"S1", "S2"}, "--yes", "--answer", "S1:k1=2", "--answer", "S1:k2=1", "--answer", "S1:k3=text", "--answer", answer)
		if code != ExitUsage || len(stub.answers) != 0 || len(stub.started) != 0 || !strings.Contains(out+errs, strings.Split(answer, "=")[0]) {
			t.Fatalf("batch validation: exit %d answers %+v started %v: %s %s", code, stub.answers, stub.started, out, errs)
		}
	}
}
