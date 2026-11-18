// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/squalor-xyz/ork/internal/clitest"
	"github.com/squalor-xyz/ork/internal/registry"
)

func recordedSlicer(name string) string {
	return filepath.Join("..", "..", "testdata", "cli", "slicer", name)
}

// pinWorld registers the projects in a temporary registry, with the fake
// slicer answering the lead routes first. It returns the app, the fake and the
// registry path.
func pinWorld(t *testing.T, lead []clitest.Route, projects ...registry.Project) (*App, *clitest.Fake, string) {
	t.Helper()
	app, fake, _ := doctorAppLead(t, lead, "Contract: relay v12")
	path, err := registry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Save(path, &registry.File{Version: registry.Version, Projects: projects}); err != nil {
		t.Fatal(err)
	}
	return app, fake, path
}

func slicerArgs(fake *clitest.Fake) [][]string {
	var out [][]string
	for _, c := range fake.CallsTo("slicer") {
		out = append(out, c.Args)
	}
	return out
}

func relayRoute(root, file string, exit int) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", root}, File: recordedSlicer(file), Exit: exit}
}

func configSetRoute(root string, n string) clitest.Route {
	return clitest.Route{Tool: "slicer", Args: []string{"config", "relay_contract", n, "--root", root}, File: recordedSlicer("config-relay-contract-set.txt")}
}

func TestProjectsPinSetsThePinThroughSlicerOnly(t *testing.T) {
	P := t.TempDir()
	app, fake, regPath := pinWorld(t, []clitest.Route{configSetRoute(P, "12"), relayRoute(P, "relay-v12-pinned.json", 0)}, registry.Project{Name: "p", Path: P})
	before, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatal(err)
	}
	code, out, stderr := run(t, app, "projects", "pin", "p", "--relay", "12")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	want := [][]string{{"config", "relay_contract", "12", "--root", P}, {"ai", "relay", "--json", "--root", P}}
	if got := slicerArgs(fake); !reflect.DeepEqual(got, want) {
		t.Fatalf("slicer calls %v, want %v", got, want)
	}
	if !strings.Contains(out, "pinned p at relay v12") {
		t.Fatalf("stdout %q", out)
	}
	after, _ := os.ReadFile(regPath)
	if string(before) != string(after) {
		t.Fatalf("the pin was stored in the registry:\n%s", after)
	}
}

func TestProjectsPinJSONReportsWhatSlicerPrints(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{configSetRoute(P, "12"), relayRoute(P, "relay-v12-pinned.json", 0)}, registry.Project{Name: "p", Path: P})
	code, out, stderr := run(t, app, "projects", "pin", "p", "--relay", "12", "--json")
	var got struct {
		Project string `json:"project"`
		Pinned  *int   `json:"pinned"`
	}
	mustJSON(t, out, &got)
	if code != 0 || got.Project != "p" || got.Pinned == nil || *got.Pinned != 12 {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
}

func TestProjectsPinNeedsRelayFlag(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P})
	code, _, stderr := run(t, app, "projects", "pin", "p")
	if code != 2 || !strings.Contains(stderr, "projects pin needs --relay N (v12 or v13)") || strings.Contains(stderr, "--relay 0") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("ran %v", calls)
	}
}

func TestProjectsPinRefusesVersionsOutsideTheAcceptedSet(t *testing.T) {
	P := t.TempDir()
	for _, args := range [][]string{
		{"--relay", "9"}, {"--relay", "10"}, {"--relay", "14"}, {"--relay", "0"},
	} {
		app, fake, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P})
		code, _, stderr := run(t, app, append([]string{"projects", "pin", "p"}, args...)...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2: %s", args, code, stderr)
		}
		if !strings.Contains(stderr, "v12 or v13") {
			t.Errorf("%v: stderr %q does not list the accepted set", args, stderr)
		}
		if calls := fake.Calls(); len(calls) != 0 {
			t.Errorf("%v: ran %v", args, calls)
		}
	}
}

func TestProjectsPinAndUnpinRefuseAnUnknownName(t *testing.T) {
	P := t.TempDir()
	for _, args := range [][]string{{"pin", "nope", "--relay", "12"}, {"unpin", "nope"}} {
		app, fake, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P})
		code, _, stderr := run(t, app, append([]string{"projects"}, args...)...)
		if code != 1 || !strings.Contains(stderr, `"nope"`) {
			t.Errorf("%v: exit %d: %s", args, code, stderr)
		}
		if calls := fake.Calls(); len(calls) != 0 {
			t.Errorf("%v: ran %v", args, calls)
		}
	}
}

func TestProjectsPinNamesSlicersMessageWhenSlicerFails(t *testing.T) {
	P := t.TempDir()
	msg, err := os.ReadFile(recordedSlicer("error-bad-config-value.txt"))
	if err != nil {
		t.Fatal(err)
	}
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"config", "relay_contract", "12", "--root", P}, Stderr: string(msg), Exit: 2}}, registry.Project{Name: "p", Path: P})
	code, _, stderr := run(t, app, "projects", "pin", "p", "--relay", "12")
	if code != 1 || !strings.Contains(stderr, "relay_contract: 'abc' is not a whole number") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

// ork accepts v12, so it sets the pin. The fake returns a recorded historical
// refusal. ork quotes that message verbatim, says the pin stays set, and gives
// its own next step.
func TestProjectsPinReportsASlicerThatRefusesTheNewPin(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, []clitest.Route{configSetRoute(P, "12"), relayRoute(P, "error-contract-mismatch.json", 2)}, registry.Project{Name: "p", Path: P})
	code, _, stderr := run(t, app, "projects", "pin", "p", "--relay", "12")
	if code != 1 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"pinned p at relay v12", "pin stays set", "this project pins relay v11, but this slicer serves relay v12", "`ork projects pin p --relay N`", "`ork projects unpin p`"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "clears the slicer pin and the adj pin") {
		t.Errorf("adj is off, but stderr warns about the adj pin:\n%s", stderr)
	}
	// The pin was written once and nothing cleared it.
	want := [][]string{{"config", "relay_contract", "12", "--root", P}, {"ai", "relay", "--json", "--root", P}}
	if got := slicerArgs(fake); !reflect.DeepEqual(got, want) {
		t.Fatalf("slicer calls %v, want %v", got, want)
	}
}

// slicer takes the set call but then prints no pin: ork says so and does not
// dereference the missing value.
func TestProjectsPinFailsWhenSlicerDoesNotKeepThePin(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{configSetRoute(P, "12"), relayRoute(P, "relay-v12.json", 0)}, registry.Project{Name: "p", Path: P})
	code, out, stderr := run(t, app, "projects", "pin", "p", "--relay", "12")
	if code != 1 || !strings.Contains(stderr, "no pin") || out != "" {
		t.Fatalf("exit %d: %q %q", code, out, stderr)
	}
}

func TestProjectsUnpinRunsExactlyTheUnsetCall(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"config", "relay_contract", "--unset", "--root", P}, File: recordedSlicer("config-relay-contract-unset.txt")}}, registry.Project{Name: "p", Path: P})
	code, out, stderr := run(t, app, "projects", "unpin", "p")
	if code != 0 || !strings.Contains(out, "unpinned p") {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	want := [][]string{{"config", "relay_contract", "--unset", "--root", P}}
	if got := slicerArgs(fake); !reflect.DeepEqual(got, want) {
		t.Fatalf("slicer calls %v, want %v", got, want)
	}
}

func toolArgs(fake *clitest.Fake) [][]string {
	var out [][]string
	for _, c := range fake.Calls() {
		out = append(out, append([]string{c.Tool}, c.Args...))
	}
	return out
}

func adjSpoken(pinned string) string {
	return `{"contract":"adj-json v1","version":1,"pinned":` + pinned + `,"extra":true}`
}

func TestProjectsPinAdjRunsAfterSlicer(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, []clitest.Route{
		configSetRoute(P, "12"),
		relayRoute(P, "relay-v12-pinned.json", 0),
		{Tool: "adj", Args: []string{"contract", "pin", "1", "--root", P}, Stdout: "pinned\n"},
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: adjSpoken("1")},
	}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, stderr := run(t, app, "projects", "pin", "p", "--relay", "12", "--adj", "1")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	if !strings.Contains(out, "relay v12") || !strings.Contains(out, "adj-json v1") {
		t.Fatalf("stdout %q", out)
	}
	want := [][]string{
		{"slicer", "config", "relay_contract", "12", "--root", P},
		{"adj", "contract", "pin", "1", "--root", P},
		{"slicer", "ai", "relay", "--json", "--root", P},
		{"adj", "contract", "--json", "--root", P},
	}
	if got := toolArgs(fake); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
}

func TestProjectsPinAdjAlone(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, []clitest.Route{
		{Tool: "adj", Args: []string{"contract", "pin", "1", "--root", P}, Stdout: "pinned\n"},
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: adjSpoken("1")},
	}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, stderr := run(t, app, "projects", "pin", "p", "--adj", "1")
	if code != 0 || !strings.Contains(out, "adj-json v1") {
		t.Fatalf("exit %d: %q %q", code, out, stderr)
	}
	if slicerArgs(fake) != nil {
		t.Fatalf("slicer calls %v", slicerArgs(fake))
	}
	want := [][]string{
		{"adj", "contract", "pin", "1", "--root", P},
		{"adj", "contract", "--json", "--root", P},
	}
	if got := toolArgs(fake); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
}

func TestProjectsPinAdjSkippedWhenSlicerFails(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, []clitest.Route{{
		Tool: "slicer", Args: []string{"config", "relay_contract", "12", "--root", P},
		Stderr: "slicer broke\n", Exit: 1,
	}}, registry.Project{Name: "p", Path: P, Adj: true})
	code, _, stderr := run(t, app, "projects", "pin", "p", "--relay", "12", "--adj", "1")
	if code != 1 || !strings.Contains(stderr, "slicer broke") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, c := range fake.Calls() {
		if c.Tool == "adj" {
			t.Fatalf("adj was called: %v", toolArgs(fake))
		}
	}
}

func TestProjectsPinAdjRefusesOutsideTheAcceptedSet(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P, Adj: true})
	code, _, stderr := run(t, app, "projects", "pin", "p", "--adj", "2")
	if code != 2 || !strings.Contains(stderr, "--adj 2") || !strings.Contains(stderr, "adj-json v1") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("ran %v", calls)
	}
}

func TestProjectsPinAdjRefusesAProjectWithoutAdj(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P, Adj: false})
	code, _, stderr := run(t, app, "projects", "pin", "p", "--adj", "1")
	if code != 2 || !strings.Contains(stderr, "adj is off") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("ran %v", calls)
	}
}

func TestProjectsUnpinAdj(t *testing.T) {
	P := t.TempDir()
	t.Run("flag", func(t *testing.T) {
		app, fake, _ := pinWorld(t, []clitest.Route{{
			Tool: "adj", Args: []string{"contract", "unpin", "--root", P}, Stdout: "unpinned\n",
		}}, registry.Project{Name: "p", Path: P, Adj: true})
		code, out, stderr := run(t, app, "projects", "unpin", "p", "--adj")
		if code != 0 {
			t.Fatalf("exit %d: %s %s", code, out, stderr)
		}
		if slicerArgs(fake) != nil {
			t.Fatalf("slicer calls %v", slicerArgs(fake))
		}
		want := [][]string{{"adj", "contract", "unpin", "--root", P}}
		if got := toolArgs(fake); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls %v, want %v", got, want)
		}
	})
	t.Run("default", func(t *testing.T) {
		app, fake, _ := pinWorld(t, []clitest.Route{
			{Tool: "slicer", Args: []string{"config", "relay_contract", "--unset", "--root", P}, File: recordedSlicer("config-relay-contract-unset.txt")},
			{Tool: "adj", Args: []string{"contract", "unpin", "--root", P}, Stdout: "unpinned\n"},
		}, registry.Project{Name: "p", Path: P, Adj: true})
		code, out, stderr := run(t, app, "projects", "unpin", "p")
		if code != 0 || !strings.Contains(out, "unpinned p") {
			t.Fatalf("exit %d: %s %s", code, out, stderr)
		}
		want := [][]string{
			{"slicer", "config", "relay_contract", "--unset", "--root", P},
			{"adj", "contract", "unpin", "--root", P},
		}
		if got := toolArgs(fake); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls %v, want %v", got, want)
		}
	})
}

func TestProjectsPinUsageStatesTheFlagAndTheDaemon(t *testing.T) {
	if !strings.Contains(usageText, "at least one flag") || !strings.Contains(usageText, "needs no daemon") {
		t.Fatalf("usage drops a clause:\n%s", usageText)
	}
	if !strings.Contains(usageText, ") through slicer") {
		t.Fatal("usage dropped the slicer phrase")
	}
	for _, line := range strings.Split(usageText, "\n") {
		if len(line) > 80 {
			t.Errorf("%d columns: %q", len(line), line)
		}
	}
}

func TestProjectsPinSlicerRefusalOnAnAdjProjectSaysUnpinClearsBoth(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, []clitest.Route{configSetRoute(P, "12"), relayRoute(P, "error-contract-mismatch.json", 2)}, registry.Project{Name: "p", Path: P, Adj: true})
	code, _, stderr := run(t, app, "projects", "pin", "p", "--relay", "12")
	if code != 1 || !strings.Contains(stderr, "`ork projects unpin p` clears the slicer pin and the adj pin") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, c := range fake.Calls() {
		if c.Tool == "adj" {
			t.Fatalf("adj was called: %v", toolArgs(fake))
		}
	}
}

func TestProjectsPinNamesTheAdjPinItSet(t *testing.T) {
	P := t.TempDir()
	spoken := `{"contract":"other","version":1,"pinned":1}`
	app, _, _ := pinWorld(t, []clitest.Route{
		{Tool: "adj", Args: []string{"contract", "pin", "1", "--root", P}, Stdout: "pinned\n"},
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: spoken},
	}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, stderr := run(t, app, "projects", "pin", "p", "--adj", "1")
	if code != 0 || !strings.Contains(out, "pinned p at adj-json v1") || strings.Contains(out, "other") {
		t.Fatalf("exit %d: %q %q", code, out, stderr)
	}

	app, _, _ = pinWorld(t, []clitest.Route{
		configSetRoute(P, "12"),
		relayRoute(P, "relay-v12-pinned.json", 0),
		{Tool: "adj", Args: []string{"contract", "pin", "1", "--root", P}, Stdout: "pinned\n"},
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: spoken},
	}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, stderr = run(t, app, "projects", "pin", "p", "--relay", "12", "--adj", "1")
	if code != 0 || !strings.Contains(out, "pinned p at relay v12 and adj-json v1") || strings.Contains(out, "other") {
		t.Fatalf("both: exit %d: %q %q", code, out, stderr)
	}
}

func TestProjectsPinAdjReadBackRefusesAPinTheSpokenVersionDoesNotMatch(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{
		{Tool: "adj", Args: []string{"contract", "pin", "1", "--root", P}, Stdout: "pinned\n"},
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: `{"contract":"adj-json v2","version":2,"pinned":1}`},
	}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, stderr := run(t, app, "projects", "pin", "p", "--adj", "1")
	if code != 1 || out != "" {
		t.Fatalf("exit %d: %q %q", code, out, stderr)
	}
	for _, want := range []string{"pinned p at adj-json v1, but", "speaks adj-json v2", "The pin stays set", "`ork projects pin p --adj 1`", "`ork projects unpin p --adj`"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "pinned p at adj-json v2") {
		t.Errorf("reports the spoken version as the pin:\n%s", stderr)
	}
}

func TestProjectsPinSaysWhenTheRelayPinStaysSet(t *testing.T) {
	P := t.TempDir()
	broken := clitest.Route{Tool: "adj", Args: []string{"contract", "pin", "1", "--root", P}, Exit: 1, Stdout: `{"error":{"code":"adj_broke","message":"adj broke"}}`}
	app, _, _ := pinWorld(t, []clitest.Route{configSetRoute(P, "12"), broken}, registry.Project{Name: "p", Path: P, Adj: true})
	code, _, stderr := run(t, app, "projects", "pin", "p", "--relay", "12", "--adj", "1")
	if code != 1 || !strings.Contains(stderr, "pinned p at relay v12, but adj failed: adj broke. The relay pin stays set") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	app, _, _ = pinWorld(t, []clitest.Route{configSetRoute(P, "12"), broken}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, _ := run(t, app, "projects", "pin", "p", "--relay", "12", "--adj", "1", "--json")
	if code != 1 || !strings.Contains(out, `"code": "adj_broke"`) || !strings.Contains(out, "The relay pin stays set") {
		t.Fatalf("json exit %d: %s", code, out)
	}
}

func TestProjectsUnpinSaysWhenTheRelayPinWasCleared(t *testing.T) {
	P := t.TempDir()
	routes := []clitest.Route{
		{Tool: "slicer", Args: []string{"config", "relay_contract", "--unset", "--root", P}, File: recordedSlicer("config-relay-contract-unset.txt")},
		{Tool: "adj", Args: []string{"contract", "unpin", "--root", P}, Exit: 1, Stdout: `{"error":{"code":"adj_broke","message":"adj broke"}}`},
	}
	app, _, _ := pinWorld(t, routes, registry.Project{Name: "p", Path: P, Adj: true})
	code, _, stderr := run(t, app, "projects", "unpin", "p")
	if code != 1 || !strings.Contains(stderr, "cleared p's relay pin, but adj failed: adj broke. The adj pin is unchanged; run ork projects unpin p --adj") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	app, _, _ = pinWorld(t, routes, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, _ := run(t, app, "projects", "unpin", "p", "--json")
	if code != 1 || !strings.Contains(out, `"code": "adj_broke"`) || !strings.Contains(out, "The adj pin is unchanged") {
		t.Fatalf("json exit %d: %s", code, out)
	}
}

func TestProjectsPinNeedsRelayOrAdj(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P, Adj: true})
	code, _, stderr := run(t, app, "projects", "pin", "p")
	if code != 2 || !strings.Contains(stderr, "--relay") || !strings.Contains(stderr, "--adj") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("ran %v", calls)
	}
}

func TestProjectsUnpinJSON(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"config", "relay_contract", "--unset", "--root", P}, File: recordedSlicer("config-relay-contract-unset.txt")}}, registry.Project{Name: "p", Path: P})
	code, out, _ := run(t, app, "projects", "unpin", "p", "--json")
	var got map[string]any
	mustJSON(t, out, &got)
	pinned, hasPinned := got["pinned"]
	if code != 0 || got["project"] != "p" || !hasPinned || pinned != nil || len(got) != 2 {
		t.Fatalf("exit %d: %s", code, out)
	}
}

// ---- doctor ----

type contractProject struct {
	Project string `json:"project"`
	Pinned  *int   `json:"pinned"`
	Found   string `json:"found"`
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
}

type doctorContractOut struct {
	OK       bool `json:"ok"`
	Contract struct {
		OK       bool              `json:"ok"`
		Projects []contractProject `json:"projects"`
	} `json:"contract"`
}

func doctorContracts(t *testing.T, app *App) (int, doctorContractOut) {
	t.Helper()
	code, out, stderr := run(t, app, "doctor", "--json")
	var d doctorContractOut
	mustJSON(t, out, &d)
	if stderr != "" {
		t.Logf("stderr: %s", stderr)
	}
	return code, d
}

func TestDoctorListsAPinnedProjectOk(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{relayRoute(P, "relay-v12-pinned.json", 0)}, registry.Project{Name: "p", Path: P})
	code, d := doctorContracts(t, app)
	if code != 0 || !d.OK || len(d.Contract.Projects) != 1 {
		t.Fatalf("exit %d: %+v", code, d)
	}
	got := d.Contract.Projects[0]
	if got.Project != "p" || got.Pinned == nil || *got.Pinned != 12 || got.Found != "relay v12" || !got.OK || got.Error != "" {
		t.Fatalf("%+v", got)
	}
}

func TestDoctorFailsAProjectWhoseSlicerRefusesContractMismatch(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{relayRoute(P, "error-contract-mismatch.json", 2)}, registry.Project{Name: "p", Path: P})
	code, d := doctorContracts(t, app)
	if code != 1 || d.OK || d.Contract.OK || len(d.Contract.Projects) != 1 {
		t.Fatalf("exit %d: %+v", code, d)
	}
	got := d.Contract.Projects[0]
	if got.OK || !strings.Contains(got.Error, "pins relay v11") || !strings.Contains(got.Error, "serves relay v12") || !strings.Contains(got.Error, "contract_mismatch") {
		t.Fatalf("%+v", got)
	}
}

func TestDoctorChecksAnUnpinnedProjectAgainstTheAcceptedSetOnly(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P})
	code, d := doctorContracts(t, app)
	if code != 0 || len(d.Contract.Projects) != 1 {
		t.Fatalf("exit %d: %+v", code, d)
	}
	if got := d.Contract.Projects[0]; !got.OK || got.Pinned != nil || got.Found != "relay v12" {
		t.Fatalf("%+v", got)
	}

	// A slicer that serves a version ork does not accept fails the project.
	raw, err := os.ReadFile(recordedSlicer("relay-v12.json"))
	if err != nil {
		t.Fatal(err)
	}
	v14 := strings.Replace(string(raw), `"contract": "relay v12"`, `"contract": "relay v14"`, 1)
	app, _, _ = pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: v14}}, registry.Project{Name: "p", Path: P})
	code, d = doctorContracts(t, app)
	if code != 1 || len(d.Contract.Projects) != 1 || d.Contract.Projects[0].OK || !strings.Contains(d.Contract.Projects[0].Error, "v14") {
		t.Fatalf("exit %d: %+v", code, d)
	}
}

// A contract in the accepted set whose data ork cannot use fails the project.
func TestDoctorFailsAProjectWhoseContractDataIsInvalid(t *testing.T) {
	P := t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v12-pinned.json"))
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	delete(generic, "finding_kinds")
	body, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: string(body)}}, registry.Project{Name: "p", Path: P})
	code, d := doctorContracts(t, app)
	if code != 1 || len(d.Contract.Projects) != 1 || d.Contract.Projects[0].OK || !strings.Contains(d.Contract.Projects[0].Error, "finding_kinds") {
		t.Fatalf("exit %d: %+v", code, d)
	}
}

func TestDoctorFailsAProjectPinnedOutsideTheAcceptedSet(t *testing.T) {
	P := t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v12-pinned.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned9 := strings.Replace(string(raw), `"pinned": 12`, `"pinned": 9`, 1)
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: pinned9}}, registry.Project{Name: "p", Path: P})
	code, d := doctorContracts(t, app)
	if code != 1 || len(d.Contract.Projects) != 1 {
		t.Fatalf("exit %d: %+v", code, d)
	}
	got := d.Contract.Projects[0]
	if got.OK || got.Pinned == nil || *got.Pinned != 9 || !strings.Contains(got.Error, "pin v9") || !strings.Contains(got.Error, "v12 or v13") {
		t.Fatalf("%+v", got)
	}
}

// Each project is asked with its own --root, and one project's refusal leaves
// the other's entry ok (the two-project test of the 2026-10-20 review).
func TestDoctorChecksEachProjectWithItsOwnRoot(t *testing.T) {
	A, B := t.TempDir(), t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v12.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned12 := strings.Replace(string(raw), `"pinned": null`, `"pinned": 12`, 1)
	app, fake, _ := pinWorld(t, []clitest.Route{
		{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", A}, Stdout: pinned12},
		relayRoute(B, "error-contract-mismatch.json", 2),
	}, registry.Project{Name: "a", Path: A}, registry.Project{Name: "b", Path: B})
	code, d := doctorContracts(t, app)
	if code != 1 || len(d.Contract.Projects) != 2 {
		t.Fatalf("exit %d: %+v", code, d)
	}
	a, b := d.Contract.Projects[0], d.Contract.Projects[1]
	if a.Project != "a" || !a.OK || a.Pinned == nil || *a.Pinned != 12 || a.Found != "relay v12" {
		t.Fatalf("a: %+v", a)
	}
	if b.Project != "b" || b.OK || !strings.Contains(b.Error, "pins relay v11") {
		t.Fatalf("b: %+v", b)
	}
	roots := map[string]bool{}
	for _, args := range slicerArgs(fake) {
		if len(args) >= 5 && args[0] == "ai" && args[1] == "relay" {
			roots[args[len(args)-1]] = true
		}
	}
	if !roots[A] || !roots[B] {
		t.Fatalf("roots asked: %v", roots)
	}
}

func TestDoctorTextListsEachProjectContract(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{relayRoute(P, "error-contract-mismatch.json", 2)}, registry.Project{Name: "p", Path: P})
	code, out, _ := run(t, app, "doctor")
	if code != 1 || !strings.Contains(out, "[FAIL] contract p  ") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"pins relay v11", "run `ork projects pin p --relay N` with the version slicer serves, or `ork projects unpin p`"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the text lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "clears the slicer pin and the adj pin") {
		t.Fatalf("adj is off, but the text warns about the adj pin:\n%s", out)
	}
	app, _, _ = pinWorld(t, []clitest.Route{relayRoute(P, "relay-v12-pinned.json", 0)}, registry.Project{Name: "p", Path: P})
	code, out, _ = run(t, app, "doctor")
	if code != 0 || !strings.Contains(out, "[ok] contract p  pinned relay v12") || strings.Contains(out, "ork projects unpin") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

// A pin outside the set names the versions ork accepts, built from the set.
func TestDoctorTextForAPinOutsideTheSetNamesTheAcceptedVersions(t *testing.T) {
	P := t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v12-pinned.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned9 := strings.Replace(string(raw), `"pinned": 12`, `"pinned": 9`, 1)
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: pinned9}}, registry.Project{Name: "p", Path: P})
	_, out, _ := run(t, app, "doctor")
	if !strings.Contains(out, "run `ork projects pin p --relay N` with v12 or v13, or `ork projects unpin p`") {
		t.Fatalf("the text lacks the next step:\n%s", out)
	}
	if strings.Contains(out, "clears the slicer pin and the adj pin") {
		t.Fatalf("adj is off, but the text warns about the adj pin:\n%s", out)
	}
}

func TestDoctorSlicerNextStepOnAnAdjProjectSaysUnpinClearsBoth(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{relayRoute(P, "error-contract-mismatch.json", 2)}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, _ := run(t, app, "doctor")
	if code != 1 || !strings.Contains(out, "run `ork projects pin p --relay N` with the version slicer serves, or `ork projects unpin p` clears the slicer pin and the adj pin") {
		t.Fatalf("mismatch: exit %d:\n%s", code, out)
	}

	raw, err := os.ReadFile(recordedSlicer("relay-v12-pinned.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned9 := strings.Replace(string(raw), `"pinned": 12`, `"pinned": 9`, 1)
	app, _, _ = pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: pinned9}}, registry.Project{Name: "p", Path: P, Adj: true})
	_, out, _ = run(t, app, "doctor")
	if !strings.Contains(out, "run `ork projects pin p --relay N` with v12 or v13, or `ork projects unpin p` clears the slicer pin and the adj pin") {
		t.Fatalf("outside set:\n%s", out)
	}
}

// Other failures get no pin advice, because pin and unpin would not fix them.
func TestDoctorProjectListHasTheAdjPin(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{
		relayRoute(P, "relay-v12-pinned.json", 0),
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: adjSpoken("1")},
	}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, _ := run(t, app, "doctor")
	if code != 0 || !strings.Contains(out, "adj-json v1 pinned") {
		t.Fatalf("pinned: exit %d:\n%s", code, out)
	}
	_, jsonOut, _ := run(t, app, "doctor", "--json")
	var body struct {
		Contract struct {
			Projects []struct {
				AdjPinned *int   `json:"adj_pinned"`
				AdjFound  string `json:"adj_found"`
			} `json:"projects"`
		} `json:"contract"`
	}
	mustJSON(t, jsonOut, &body)
	if len(body.Contract.Projects) != 1 || body.Contract.Projects[0].AdjPinned == nil || *body.Contract.Projects[0].AdjPinned != 1 || body.Contract.Projects[0].AdjFound != "adj-json v1" {
		t.Fatalf("json: %s", jsonOut)
	}

	app, _, _ = pinWorld(t, []clitest.Route{
		relayRoute(P, "relay-v12.json", 0),
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: adjSpoken("null")},
	}, registry.Project{Name: "p", Path: P, Adj: true})
	_, out, _ = run(t, app, "doctor")
	if !strings.Contains(out, "adj-json v1 unpinned") {
		t.Fatalf("unpinned:\n%s", out)
	}

	app, _, _ = pinWorld(t, []clitest.Route{relayRoute(P, "relay-v12-pinned.json", 0)}, registry.Project{Name: "p", Path: P})
	_, out, _ = run(t, app, "doctor")
	if !strings.Contains(out, "adj -") {
		t.Fatalf("adj off:\n%s", out)
	}
}

func TestDoctorFailsOnAnAdjContractMismatch(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{
		relayRoute(P, "relay-v12-pinned.json", 0),
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: `{"contract":"adj-json v1","version":1,"pinned":2}`},
	}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, _ := run(t, app, "doctor")
	if code != 1 || !strings.Contains(out, "[FAIL] contract p") || !strings.Contains(out, "ork projects pin p --adj 1") || strings.Contains(out, "--adj N") || !strings.Contains(out, "ork projects unpin p --adj") {
		t.Fatalf("exit %d:\n%s", code, out)
	}

	app, _, _ = pinWorld(t, []clitest.Route{
		relayRoute(P, "relay-v12-pinned.json", 0),
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Exit: 1, Stdout: `{"error":{"code":"contract_mismatch","message":"pins adj-json v2, speaks adj-json v1"}}`},
	}, registry.Project{Name: "p", Path: P, Adj: true})
	code, out, _ = run(t, app, "doctor")
	if code != 1 || !strings.Contains(out, "contract_mismatch") || !strings.Contains(out, "ork projects pin p --adj 1") || strings.Contains(out, "--adj N") || !strings.Contains(out, "ork projects unpin p --adj") {
		t.Fatalf("tool error: exit %d:\n%s", code, out)
	}
}

func TestDoctorContractLinesFitEightyColumns(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{
		relayRoute(P, "relay-v12.json", 0),
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: adjSpoken("null")},
	}, registry.Project{Name: "project1", Path: P, Adj: true})
	_, out, _ := run(t, app, "doctor")
	if !contractLinesFit(t, out, "project1") {
		t.Fatalf("unpinned:\n%s", out)
	}

	app, _, _ = pinWorld(t, []clitest.Route{
		relayRoute(P, "relay-v12-pinned.json", 0),
		{Tool: "adj", Args: []string{"contract", "--json", "--root", P}, Stdout: adjSpoken("1")},
	}, registry.Project{Name: "project1", Path: P, Adj: true})
	_, out, _ = run(t, app, "doctor")
	if !contractLinesFit(t, out, "project1") {
		t.Fatalf("pinned:\n%s", out)
	}
}

func contractLinesFit(t *testing.T, out, name string) bool {
	t.Helper()
	found := false
	ok := true
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "contract "+name) && !strings.HasPrefix(line, "  adj") {
			continue
		}
		found = true
		if n := len([]rune(line)); n > 80 {
			t.Errorf("%d columns: %q", n, line)
			ok = false
		}
	}
	if !found {
		t.Errorf("no contract line:\n%s", out)
		return false
	}
	return ok
}

func TestDoctorTextGivesNoPinAdviceForAnUnacceptedContract(t *testing.T) {
	P := t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v12.json"))
	if err != nil {
		t.Fatal(err)
	}
	v14 := strings.Replace(string(raw), `"contract": "relay v12"`, `"contract": "relay v14"`, 1)
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: v14}}, registry.Project{Name: "p", Path: P})
	_, out, _ := run(t, app, "doctor")
	if !strings.Contains(out, "[FAIL] contract p  ") || strings.Contains(out, "ork projects unpin p") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestContractWordingUsesOnePhrase(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"projects", "pin", "p"}, "projects pin needs --relay N (v12 or v13)"},
		{[]string{"projects", "pin", "p", "--relay", "9"}, "projects pin: --relay 9 is not a version ork accepts (v12 or v13)"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			app, _, _ := pinWorld(t, nil)
			_, _, err := run(t, app, tc.args...)
			if !strings.Contains(err, tc.want) {
				t.Errorf("want %q, got %s", tc.want, err)
			}
		})
	}
	t.Run("pin", func(t *testing.T) {
		P := t.TempDir()
		raw, err := os.ReadFile(recordedSlicer("relay-v12-pinned.json"))
		if err != nil {
			t.Fatal(err)
		}
		app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: strings.Replace(string(raw), `"pinned": 12`, `"pinned": 9`, 1)}}, registry.Project{Name: "p", Path: P})
		_, d := doctorContracts(t, app)
		if len(d.Contract.Projects) != 1 || d.Contract.Projects[0].Error != "pin v9 is outside what ork accepts (v12 or v13)" {
			t.Errorf("pin error: %+v", d.Contract.Projects)
		}
	})
	t.Run("ok", func(t *testing.T) {
		app, _, _ := doctorApp(t, "Contract: relay v12")
		_, out, _ := run(t, app, "doctor")
		if !strings.Contains(out, "[ok] contract  accepts v12 or v13, slicer prints \"Contract: relay v12\"\n") {
			t.Errorf("contract line: %s", out)
		}
	})
}

func TestProjectsPinAndUnpinUnknownNameIsNotFound(t *testing.T) {
	for _, args := range [][]string{{"projects", "pin", "nope", "--relay", "12"}, {"projects", "unpin", "nope"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app, _, _ := pinWorld(t, nil)
			code, _, stderr := run(t, app, args...)
			want := "ork: no project named \"nope\"; run 'ork projects list'\n"
			if code != 1 || stderr != want {
				t.Errorf("exit %d, stderr %q, want %q", code, stderr, want)
			}
			code, out, _ := run(t, app, append(args, "--json")...)
			var d struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			mustJSON(t, out, &d)
			if code != 1 || d.Error.Code != "not_found" {
				t.Errorf("JSON exit %d: %s", code, out)
			}
		})
	}
}

func TestProjectsPinAcceptsRelay13(t *testing.T) {
	P := t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v13.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned := strings.Replace(string(raw), `"pinned": null`, `"pinned": 13`, 1)
	app, _, _ := pinWorld(t, []clitest.Route{configSetRoute(P, "13"), {Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: pinned}}, registry.Project{Name: "p", Path: P})
	code, out, stderr := run(t, app, "projects", "pin", "p", "--relay", "13", "--json")
	var got struct {
		Pinned *int `json:"pinned"`
	}
	mustJSON(t, out, &got)
	if code != 0 || got.Pinned == nil || *got.Pinned != 13 {
		t.Fatalf("pin v13: exit %d: %s %s", code, out, stderr)
	}
	code, _, stderr = run(t, app, "projects", "pin", "p", "--relay", "14")
	if code != 2 || !strings.Contains(stderr, "v12 or v13") {
		t.Fatalf("pin v14: exit %d: %s", code, stderr)
	}
}
