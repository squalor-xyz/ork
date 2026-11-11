// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
)

func statsApp(t *testing.T, file, since string) *App {
	t.Helper()
	args := []string{"relay-run", "stats", "--json"}
	if since != "" {
		args = append(args, "--since", since)
	}
	args = append(args, "--root", "/projects/stats")
	app := statsWorld(t, clitest.Route{Tool: "slicer", Args: args, File: recordedSlicer(file)})
	app.Getwd = func() (string, error) { return "/projects/stats", nil }
	return app
}

func statsWorld(t *testing.T, route clitest.Route) *App {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := registry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Save(path, &registry.File{Version: registry.Version, Projects: []registry.Project{{Name: "p", Path: "/projects/stats"}}}); err != nil {
		t.Fatal(err)
	}
	fake := clitest.Install(t, route)
	return &App{Tools: Tools{Slicer: fake.Bin("slicer")}}
}

func TestStatsPrintsTheLinesSlicerPrints(t *testing.T) {
	app := statsApp(t, "relay-run-stats.json", "")
	code, out, stderr := run(t, app, "stats", "p")
	want := "relay runs: 174 (160 pass, 0 fail, 10 stopped, 4 unended); 61 measured\n" +
		"wall time: median 37m32s, longest 11h30m\n" +
		"fix rounds: 61 (53 runs with any, 5 with two or more)\n" +
		"gates: 62 rounds, 5 FAIL; 59 had a recorded verdict, 3 counted by proxy\n" +
		"owner wait: 48 runs with a recorded wait, 42h01m in all, median 3m32s, longest 11h05m\n" +
		"role          steps   total  median\n" +
		"implementer     220  18h10m   2m37s\n" +
		"reviewer         60  10h37m   9m53s\n" +
		"gate             62   1h50m   1m20s\n" +
		"orchestrator     60  41h40m  30m08s\n" +
		"ui               45   1h24m   1m04s\n"
	if code != 0 || out != want || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q; want %q", code, out, stderr, want)
	}
}

func TestStatsJSONIsSlicersOutputWithoutPerRun(t *testing.T) {
	for _, file := range []string{"relay-run-stats.json", "relay-run-stats-empty.json"} {
		t.Run(file, func(t *testing.T) {
			app := statsApp(t, file, "")
			code, out, stderr := run(t, app, "stats", "--json")
			raw, err := os.ReadFile(recordedSlicer(file))
			if err != nil {
				t.Fatal(err)
			}
			var want, got map[string]any
			mustJSON(t, string(raw), &want)
			delete(want, "per_run")
			mustJSON(t, out, &got)
			if code != 0 || !reflect.DeepEqual(got, want) || stderr != "" {
				t.Fatalf("exit %d stdout %s stderr %s; want %s", code, out, stderr, raw)
			}
		})
	}
}

func TestStatsSinceFlagIsPassedThrough(t *testing.T) {
	app := statsApp(t, "relay-run-stats-empty.json", "2099-01-01")
	code, out, stderr := run(t, app, "stats", "--since", "2099-01-01", "p", "--json")
	var got map[string]any
	mustJSON(t, out, &got)
	if code != 0 || got["since"] != "2099-01-01T00:00:00Z" || stderr != "" {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
}

func TestStatsUnknownProjectIsNotFound(t *testing.T) {
	app := statsApp(t, "relay-run-stats.json", "")
	code, out, _ := run(t, app, "stats", "missing", "--json")
	var got struct {
		Error struct{ Code, Message string }
	}
	mustJSON(t, out, &got)
	if code != 1 || got.Error.Code != "not_found" || !strings.Contains(got.Error.Message, `no project named "missing"`) {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestStatsOlderSlicerPrintsSlicersError(t *testing.T) {
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			app := statsWorld(t, clitest.Route{Tool: "slicer", Args: []string{"relay-run", "stats", "--json", "--root", "/projects/stats"}, Stdout: `{"error":{"code":"usage","message":"unknown command stats"}}`, Exit: 2})
			args := []string{"stats", "p"}
			if mode == "json" {
				args = append(args, "--json")
			}
			code, out, stderr := run(t, app, args...)
			wantOut, wantErr := "", "ork: unknown command stats\n"
			if mode == "json" {
				var got any
				mustJSON(t, out, &got)
				wantOut = "{\n  \"error\": {\n    \"code\": \"usage\",\n    \"message\": \"unknown command stats\"\n  }\n}\n"
				wantErr = ""
			}
			if code != 1 || out != wantOut || stderr != wantErr {
				t.Fatalf("exit %d: %q %q", code, out, stderr)
			}
		})
	}
}

func TestStatsUsage(t *testing.T) {
	for _, args := range [][]string{{"stats", "a", "b"}, {"stats", "--since"}, {"stats", "--unknown"}} {
		code, _, stderr := run(t, &App{}, args...)
		if code != 2 || !strings.Contains(stderr, "stats") {
			t.Fatalf("%v: exit %d %s", args, code, stderr)
		}
	}
	for _, args := range [][]string{{"stats", "--help"}, {"help", "stats"}} {
		code, out, stderr := run(t, &App{}, args...)
		if code != 0 || !strings.Contains(out, "ork stats [PROJECT] [--since DATE]") || stderr != "" {
			t.Fatalf("%v: exit %d %q %q", args, code, out, stderr)
		}
	}
}

func TestStatsEmptyLedgerText(t *testing.T) {
	app := statsApp(t, "relay-run-stats-empty.json", "2099-01-01")
	code, out, stderr := run(t, app, "stats", "--since", "2099-01-01")
	if code != 0 || !strings.Contains(out, "wall time: median -, longest -") || !strings.Contains(out, "owner wait: 0 runs with a recorded wait, 0s in all, median -, longest -") {
		t.Fatalf("exit %d: %q %q", code, out, stderr)
	}
}
