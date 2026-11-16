// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/engine"
	"github.com/squalor-xyz/ork/internal/registry"
	"github.com/squalor-xyz/ork/internal/runrec"
	"github.com/squalor-xyz/ork/internal/slicercli"
)

func TestPlanLabelsFallbackEntries(t *testing.T) {
	for _, role := range []string{"implementer", "reviewer", "ui", "architect", "gate"} {
		for _, n := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/%d", role, n), func(t *testing.T) {
				entries := []runrec.LineupEntry{{Vendor: "codex", Model: "gpt-6.1-sol", Effort: "medium"}, {Vendor: "claude", Model: "sonnet", Effort: "high"}}
				p := daemon.RunPlan{Lineup: map[string][]runrec.LineupEntry{role: entries[:n]}, Permissions: map[string]engine.Permission{role: {Allow: []string{"Read", "Grep"}}}}
				var out bytes.Buffer
				printRunPlan(&out, p, false)
				want := role + ": codex gpt-6.1-sol medium\n"
				if n == 2 {
					want += role + ": claude sonnet high (fallback)\n"
				}
				want += "  allowlist: Read, Grep\n"
				if !strings.Contains(out.String(), want) || strings.Count(out.String(), "(fallback)") != n-1 || strings.Count(out.String(), "  allowlist: Read, Grep\n") != 1 {
					t.Fatalf("role chain: want %q in %q", want, out.String())
				}
			})
		}
	}
}

// launchApp answers only the launch protocol. The hook changes one response
// per differential case; every other response stays the same.
func launchApp(t *testing.T, hook func(string, any, any) (bool, error)) *App {
	t.Helper()
	return &App{Input: strings.NewReader(""), Call: func(_ context.Context, method string, params, result any) error {
		if hook != nil {
			if handled, err := hook(method, params, result); handled {
				return err
			}
		}
		switch method {
		case daemon.MethodRunPlan:
			p := params.(daemon.RunParams)
			return json.Unmarshal([]byte(fmt.Sprintf(`{"project":{"name":"p"},"item":%q,"check_command":"go test ./..."}`, p.Item)), result)
		case daemon.MethodItemShow:
			return json.Unmarshal([]byte(`{"detail":{"slice":{"sections":[{"heading":"Files","body":"`+"`docs/x.md`"+`"}]}}}`), result)
		case daemon.MethodProjectsList:
			return json.Unmarshal([]byte(`{"projects":[{"name":"p","path":"/p"}]}`), result)
		case daemon.MethodSpecCheck:
			p := params.(daemon.SpecCheckParams)
			var entries []string
			for _, id := range p.Items {
				entries = append(entries, fmt.Sprintf(`{"item":%q,"ready":true}`, id))
			}
			return json.Unmarshal([]byte(`{"items":[`+strings.Join(entries, ",")+`]}`), result)
		case daemon.MethodRunStart:
			return json.Unmarshal([]byte(`{"id":"ork-1","state":"queued"}`), result)
		case daemon.MethodWaveStart:
			return json.Unmarshal([]byte(`{"wave":"wave-1","items":[{"item":"S207","run":{"id":"ork-1","state":"queued"}},{"item":"S100","run":{"id":"ork-2","state":"setup"}}]}`), result)
		}
		t.Fatalf("unexpected method %s", method)
		return nil
	}}
}

// The daemon's run.plan names only the project and its path, so the plan the
// owner reads takes push and UI pass skills from projects.list. The plan sent
// back to run.start stays as the daemon made it.
func TestLaunchPlanShowsTheRegistrySettings(t *testing.T) {
	for _, args := range [][]string{{"run", "p", "S207", "--yes"}, {"wave", "start", "p", "S207", "S100", "--yes"}} {
		t.Run(args[0], func(t *testing.T) {
			var expected []*daemon.RunPlan
			app := launchApp(t, func(m string, p, r any) (bool, error) {
				switch m {
				case daemon.MethodRunPlan:
					return true, json.Unmarshal([]byte(`{"project":{"name":"p","path":"/p"},"item":"`+p.(daemon.RunParams).Item+`","check_command":"go test ./...","lineup":{"ui":[{"vendor":"claude","model":"opus","effort":"medium"}]},"permissions":{"ui":{"ReadOnly":true}}}`), r)
				case daemon.MethodProjectsList:
					return true, json.Unmarshal([]byte(`{"projects":[{"name":"other","path":"/o"},{"name":"p","path":"/p","push":true,"ui_pass_skills":["a","b"]}]}`), r)
				case daemon.MethodRunStart:
					expected = append(expected, p.(daemon.RunParams).Expected)
				case daemon.MethodWaveStart:
					for _, item := range p.(daemon.WaveStartParams).Items {
						expected = append(expected, item.Expected)
					}
				}
				return false, nil
			})
			code, out, errs := run(t, app, args...)
			if code != 0 || !strings.Contains(out, "stop: after landing\npush: on\n") || !strings.Contains(out, "  permissions: read-only\n  skills: a, b\n") {
				t.Fatalf("plan settings: %d %q %q", code, out, errs)
			}
			if len(expected) == 0 {
				t.Fatal("no start call")
			}
			for _, plan := range expected {
				if plan == nil || plan.Project.Push || len(plan.Project.UIPassSkills) != 0 {
					t.Fatalf("expected plan changed: %+v", plan)
				}
			}
		})
	}
}

func TestRunPrintsTheFilesLine(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			app := launchApp(t, func(m string, p, r any) (bool, error) {
				if broken && m == daemon.MethodItemShow {
					return true, fmt.Errorf("cannot read slice")
				}
				return false, nil
			})
			code, out, errs := run(t, app, "run", "p", "S207", "--yes")
			want := "files: no engine path\n"
			if broken {
				want = "files: unknown (cannot read slice)\n"
			}
			if code != 0 || !strings.Contains(out, want) || strings.Index(out, want) < strings.Index(out, "stop:") || !strings.HasSuffix(out, "ork-1 queued\nfollow: ork status --project p\nstop it: ork stop ork-1\n") {
				t.Fatalf("files/launch output: %d %q %q", code, out, errs)
			}
		})
	}
}

func TestWaveStartSaysTheOnePairRuleHolds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		items  []string
		engine bool
		line   string
	}{
		{"one", []string{"S207", "S100"}, true, "pair rule: ok, one engine slice (S207)\n"},
		{"none", []string{"S207", "S100"}, false, "pair rule: ok, no engine slice\n"},
		{"single", []string{"S207"}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := launchApp(t, func(m string, p, r any) (bool, error) {
				if m == daemon.MethodItemShow && tc.engine && p.(daemon.ItemShowParams).ID == "S207" {
					return true, json.Unmarshal([]byte(`{"detail":{"slice":{"sections":[{"heading":"Files","body":"`+"`internal/engine/x.go:10`"+`"}]}}}`), r)
				}
				return false, nil
			})
			code, out, errs := run(t, app, append([]string{"wave", "start", "p"}, append(tc.items, "--yes")...)...)
			if code != 0 {
				t.Fatalf("launch: %d %s %s", code, out, errs)
			}
			if tc.line == "" {
				if strings.Contains(out, "pair rule:") {
					t.Fatalf("single pair rule: %s", out)
				}
			} else if !strings.Contains(out, tc.line) {
				t.Fatalf("missing pair rule %q: %s", tc.line, out)
			}
			if !strings.Contains(out, "p S207\nworktree:") || !strings.HasSuffix(out, "follow: ork status --project p\nstop the wave: ork stop wave-1\n") {
				t.Fatalf("plan/launch: %s", out)
			}
		})
	}
}

func TestWaveStartRefusesTwoEngineSlicesBeforeTheSpecCheck(t *testing.T) {
	for _, j := range []bool{false, true} {
		t.Run(fmt.Sprint(j), func(t *testing.T) {
			var calls []string
			app := launchApp(t, func(m string, p, r any) (bool, error) {
				calls = append(calls, m)
				if m == daemon.MethodItemShow {
					return true, json.Unmarshal([]byte(`{"detail":{"slice":{"sections":[{"heading":"Files","body":"`+"`cmd/ork/main.go`"+`"}]}}}`), r)
				}
				return false, nil
			})
			args := []string{"wave", "start", "p", "S207", "S100", "--yes"}
			if j {
				args = append(args, "--json")
			}
			code, out, errs := run(t, app, args...)
			want := daemon.EngineConflict("S207", "S100")
			if code != 1 {
				t.Fatalf("conflict launched: %d %s %s calls=%v", code, out, errs, calls)
			}
			for _, m := range calls {
				if m == daemon.MethodSpecCheck || m == daemon.MethodWaveStart {
					t.Fatalf("conflict reached %s", m)
				}
			}
			if j {
				var got struct{ Error struct{ Message string } }
				mustJSON(t, out, &got)
				if got.Error.Message != want {
					t.Fatalf("conflict JSON: %s", out)
				}
			} else if !strings.Contains(errs, want) {
				t.Fatalf("conflict text: %s", errs)
			}
		})
	}
}

func TestSpecCheckStepIsVisible(t *testing.T) {
	for _, j := range []bool{false, true} {
		for _, questions := range []bool{false, true} {
			t.Run(fmt.Sprintf("json=%v/questions=%v", j, questions), func(t *testing.T) {
				var out, errs bytes.Buffer
				w := &out
				if j {
					w = &errs
				}
				reading := "spec check: reading A, B (it runs even with --well-specified)\n"
				app := launchApp(t, func(m string, p, r any) (bool, error) {
					if m == daemon.MethodSpecCheck {
						if w.String() != reading {
							t.Errorf("before spec call: %q", w.String())
						}
						if questions {
							return true, json.Unmarshal([]byte(`{"items":[{"item":"A","ready":true},{"item":"B","questions":[{"text":"Choose?","options":["one","two"]}]}]}`), r)
						}
					}
					return false, nil
				})
				o := &output{stdout: &out, stderr: &errs, json: j}
				p := daemon.WaveStartParams{Items: []daemon.RunParams{{Project: "p", Item: "A"}, {Project: "p", Item: "B"}}}
				_, _, code := app.specCheck(context.Background(), o, bufio.NewReader(strings.NewReader("")), w, &p, &specFlags{})
				ready := "spec check: A ready, B ready\n"
				if questions {
					ready = "spec check: A ready\n"
				}
				if code != 0 || !strings.Contains(w.String(), ready) {
					t.Fatalf("ready result: %d %q", code, w.String())
				}
				if questions && (!strings.Contains(w.String(), "B needs 1 answer") || strings.Contains(w.String(), "B ready")) {
					t.Fatalf("open question output: %s", w)
				}
				if j && out.Len() != 0 {
					t.Fatalf("sitting on stdout: %s", &out)
				}
			})
		}
	}
}

func TestUsageSaysTheFlagsDoNotSkipTheSpecCheck(t *testing.T) {
	if !strings.Contains(strings.Join(strings.Fields(usageText), " "), "--well-specified and --split do not skip the spec check.") {
		t.Error("help omits flags do not skip the spec check")
	}
	for _, line := range strings.Split(usageText, "\n") {
		if len(line) > 80 {
			t.Errorf("usage line is %d columns: %s", len(line), line)
		}
	}
}

func TestWaveStartLaunchLinesNameTheItemAndTheNextCommands(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprint(broken), func(t *testing.T) {
			app := launchApp(t, func(m string, p, r any) (bool, error) {
				if broken && m == daemon.MethodWaveStart {
					return true, json.Unmarshal([]byte(`{"wave":"wave-1","items":[{"item":"S207","run":{"id":"ork-1","state":"queued"}},{"item":"S100","error":{"code":"refused","message":"held"}}]}`), r)
				}
				return false, nil
			})
			code, out, errs := run(t, app, "wave", "start", "p", "S207", "S100", "--yes")
			want := "wave-1\nS207 ork-1 queued\n"
			if !broken {
				want += "S100 ork-2 setup\n"
			}
			want += "follow: ork status --project p\nstop the wave: ork stop wave-1\n"
			if !strings.HasSuffix(out, want) {
				t.Fatalf("launch lines: %q", out)
			}
			if broken {
				if code != 1 || errs != "S100: held (refused)\n" {
					t.Fatalf("item error: %d %q", code, errs)
				}
			} else if code != 0 {
				t.Fatalf("launch: %d %s", code, errs)
			}
		})
	}
	// No item started: the wave id prints, the errors go to stderr, and no
	// follow or stop hint names a wave that has no run.
	t.Run("none started", func(t *testing.T) {
		app := launchApp(t, func(m string, p, r any) (bool, error) {
			if m == daemon.MethodWaveStart {
				return true, json.Unmarshal([]byte(`{"wave":"wave-1","items":[{"item":"S207","error":{"code":"refused","message":"held"}},{"item":"S100","error":{"code":"refused","message":"held"}}]}`), r)
			}
			return false, nil
		})
		code, out, errs := run(t, app, "wave", "start", "p", "S207", "S100", "--yes")
		if code != 1 || strings.Contains(out, "follow:") || strings.Contains(out, "stop the wave") || errs != "S207: held (refused)\nS100: held (refused)\n" {
			t.Fatalf("none started: %d %q %q", code, out, errs)
		}
	})
	var out bytes.Buffer
	var stopped daemon.WaveResult
	if err := json.Unmarshal([]byte(`{"wave":"wave-1","items":[{"item":"S207","run":{"id":"ork-1","state":"stopped"}}]}`), &stopped); err != nil {
		t.Fatal(err)
	}
	printWave(&output{stdout: &out}, stopped)
	if out.String() != "wave-1\nork-1 stopped\n" {
		t.Fatalf("stop output changed: %q", out.String())
	}
}

func TestRunLaunchPrintsHints(t *testing.T) {
	app := launchApp(t, nil)
	t.Run("text", func(t *testing.T) {
		_, out, _ := run(t, app, "run", "p", "S207", "--yes")
		if !strings.HasSuffix(out, "ork-1 queued\nfollow: ork status --project p\nstop it: ork stop ork-1\n") {
			t.Fatalf("run hints: %q", out)
		}
	})
	t.Run("json", func(t *testing.T) {
		_, out, _ := run(t, app, "run", "p", "S207", "--yes", "--json")
		var got daemon.RunReply
		mustJSON(t, out, &got)
		if got.Record.ID != "ork-1" || got.Record.State != runrec.StateQueued || strings.Contains(out, "follow:") {
			t.Fatalf("run JSON changed: %q", out)
		}
	})
}

func TestProjectsListShowsPushAndSkills(t *testing.T) {
	for _, tc := range []struct {
		name      string
		adj, push bool
		skills    []string
		words     string
	}{
		{"none", false, false, nil, ""}, {"adj", true, false, nil, "adj"}, {"push", false, true, nil, "push"}, {"skills", false, false, []string{"a", "b"}, "ui-pass-skills=a,b"}, {"all", true, true, []string{"a", "b"}, "adj push ui-pass-skills=a,b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := registry.Project{Name: "p", Path: "/p", Adj: tc.adj, Push: tc.push, UIPassSkills: tc.skills}
			var rendered bytes.Buffer
			printProjects(&rendered, []registry.Project{project})
			out := rendered.String()
			want := "p  /p  -\n"
			if tc.words != "" {
				want = "p  /p  " + tc.words + "\n"
			}
			if out != want {
				t.Fatalf("project row: got %q want %q", out, want)
			}
		})
	}
}

func TestFileLineSaysWhetherTheSliceTouchesTheEnginePaths(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		err              error
	}{
		{"engine", "`internal/engine/x.go:10`", "files: touches internal/engine, internal/daemon or cmd (engine slice)", nil},
		{"docs", "`docs/x.md`", "files: no engine path", nil},
		{"missing", "", "files: none listed", nil},
		{"error", "", "files: unknown (MESSAGE)", fmt.Errorf("MESSAGE")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail := &slicercli.Detail{Slice: &slicercli.Spec{}}
			if tc.body != "" {
				detail.Slice.Sections = []slicercli.Section{{Heading: "Files", Body: tc.body}}
			}
			if got := fileLine(detail, tc.err); got != tc.want {
				t.Fatalf("file line: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestPlanShowsPush(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprint(on), func(t *testing.T) {
			var p daemon.RunPlan
			p.Project.Push = on
			var out bytes.Buffer
			printRunPlan(&out, p, false)
			value := "off"
			if on {
				value = "on"
			}
			if !strings.Contains(out.String(), "stop: after landing\npush: "+value+"\n") {
				t.Fatalf("missing push after stop: %s", &out)
			}
		})
	}
}

func TestPlanShowsUIPassSkills(t *testing.T) {
	for _, ui := range []bool{false, true} {
		for _, skills := range []bool{false, true} {
			t.Run(fmt.Sprintf("ui=%v/skills=%v", ui, skills), func(t *testing.T) {
				p := daemon.RunPlan{Lineup: map[string][]runrec.LineupEntry{}, Permissions: map[string]engine.Permission{"ui": {ReadOnly: true}}}
				if ui {
					p.Lineup["ui"] = []runrec.LineupEntry{{Vendor: "claude", Model: "opus", Effort: "medium"}}
				}
				if skills {
					p.Project.UIPassSkills = []string{"a", "b"}
				}
				var out bytes.Buffer
				printRunPlan(&out, p, false)
				want := "ui: claude opus medium\n  permissions: read-only\n  skills: a, b\n"
				if ui && skills {
					if !strings.Contains(out.String(), want) {
						t.Fatalf("missing skills after ui permissions: %s", &out)
					}
				} else if strings.Contains(out.String(), "skills:") {
					t.Fatalf("unexpected skills: %s", &out)
				}
			})
		}
	}
}
