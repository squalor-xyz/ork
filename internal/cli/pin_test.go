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
	app, fake, _ := doctorAppLead(t, lead, "Contract: relay v11")
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
	app, fake, regPath := pinWorld(t, []clitest.Route{configSetRoute(P, "11"), relayRoute(P, "relay-v11-pinned.json", 0)}, registry.Project{Name: "p", Path: P})
	before, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatal(err)
	}
	code, out, stderr := run(t, app, "projects", "pin", "p", "--relay", "11")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	want := [][]string{{"config", "relay_contract", "11", "--root", P}, {"ai", "relay", "--json", "--root", P}}
	if got := slicerArgs(fake); !reflect.DeepEqual(got, want) {
		t.Fatalf("slicer calls %v, want %v", got, want)
	}
	if !strings.Contains(out, "pinned p at relay v11") {
		t.Fatalf("stdout %q", out)
	}
	after, _ := os.ReadFile(regPath)
	if string(before) != string(after) {
		t.Fatalf("the pin was stored in the registry:\n%s", after)
	}
}

func TestProjectsPinJSONReportsWhatSlicerPrints(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{configSetRoute(P, "11"), relayRoute(P, "relay-v11-pinned.json", 0)}, registry.Project{Name: "p", Path: P})
	code, out, stderr := run(t, app, "projects", "pin", "p", "--relay", "11", "--json")
	var got struct {
		Project string `json:"project"`
		Pinned  *int   `json:"pinned"`
	}
	mustJSON(t, out, &got)
	if code != 0 || got.Project != "p" || got.Pinned == nil || *got.Pinned != 11 {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
}

func TestProjectsPinNeedsRelayFlag(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P})
	code, _, stderr := run(t, app, "projects", "pin", "p")
	if code != 2 || !strings.Contains(stderr, "projects pin needs --relay N (relay v10, v11)") || strings.Contains(stderr, "--relay 0") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if calls := fake.Calls(); len(calls) != 0 {
		t.Fatalf("ran %v", calls)
	}
}

func TestProjectsPinRefusesVersionsOutsideTheAcceptedSet(t *testing.T) {
	P := t.TempDir()
	for _, args := range [][]string{
		{"--relay", "9"}, {"--relay", "12"}, {"--relay", "0"},
	} {
		app, fake, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P})
		code, _, stderr := run(t, app, append([]string{"projects", "pin", "p"}, args...)...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2: %s", args, code, stderr)
		}
		if !strings.Contains(stderr, "v10, v11") {
			t.Errorf("%v: stderr %q does not list the accepted set", args, stderr)
		}
		if calls := fake.Calls(); len(calls) != 0 {
			t.Errorf("%v: ran %v", args, calls)
		}
	}
}

func TestProjectsPinAndUnpinRefuseAnUnknownName(t *testing.T) {
	P := t.TempDir()
	for _, args := range [][]string{{"pin", "nope", "--relay", "11"}, {"unpin", "nope"}} {
		app, fake, _ := pinWorld(t, nil, registry.Project{Name: "p", Path: P})
		code, _, stderr := run(t, app, append([]string{"projects"}, args...)...)
		if code != 2 || !strings.Contains(stderr, `"nope"`) {
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
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"config", "relay_contract", "11", "--root", P}, Stderr: string(msg), Exit: 2}}, registry.Project{Name: "p", Path: P})
	code, _, stderr := run(t, app, "projects", "pin", "p", "--relay", "11")
	if code != 1 || !strings.Contains(stderr, "relay_contract: 'abc' is not a whole number") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

// ork accepts v10, so it sets the pin; the slicer in the test serves v11 and
// refuses. The message names the project, says the pin stays set, quotes
// slicer and gives ork's own next step.
func TestProjectsPinReportsASlicerThatRefusesTheNewPin(t *testing.T) {
	P := t.TempDir()
	app, fake, _ := pinWorld(t, []clitest.Route{configSetRoute(P, "10"), relayRoute(P, "error-contract-mismatch.json", 2)}, registry.Project{Name: "p", Path: P})
	code, _, stderr := run(t, app, "projects", "pin", "p", "--relay", "10")
	if code != 1 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"pinned p at relay v10", "pin stays set", "this project pins relay v10, but this slicer serves relay v11", "`ork projects pin p --relay N`", "`ork projects unpin p`"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	// The pin was written once and nothing cleared it.
	want := [][]string{{"config", "relay_contract", "10", "--root", P}, {"ai", "relay", "--json", "--root", P}}
	if got := slicerArgs(fake); !reflect.DeepEqual(got, want) {
		t.Fatalf("slicer calls %v, want %v", got, want)
	}
}

// slicer takes the set call but then prints no pin: ork says so and does not
// dereference the missing value.
func TestProjectsPinFailsWhenSlicerDoesNotKeepThePin(t *testing.T) {
	P := t.TempDir()
	app, _, _ := pinWorld(t, []clitest.Route{configSetRoute(P, "11"), relayRoute(P, "relay-v11.json", 0)}, registry.Project{Name: "p", Path: P})
	code, out, stderr := run(t, app, "projects", "pin", "p", "--relay", "11")
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
	app, _, _ := pinWorld(t, []clitest.Route{relayRoute(P, "relay-v11-pinned.json", 0)}, registry.Project{Name: "p", Path: P})
	code, d := doctorContracts(t, app)
	if code != 0 || !d.OK || len(d.Contract.Projects) != 1 {
		t.Fatalf("exit %d: %+v", code, d)
	}
	got := d.Contract.Projects[0]
	if got.Project != "p" || got.Pinned == nil || *got.Pinned != 11 || got.Found != "relay v11" || !got.OK || got.Error != "" {
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
	if got.OK || !strings.Contains(got.Error, "pins relay v10") || !strings.Contains(got.Error, "serves relay v11") || !strings.Contains(got.Error, "contract_mismatch") {
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
	if got := d.Contract.Projects[0]; !got.OK || got.Pinned != nil || got.Found != "relay v11" {
		t.Fatalf("%+v", got)
	}

	// A slicer that serves a version ork does not accept fails the project.
	raw, err := os.ReadFile(recordedSlicer("relay-v11.json"))
	if err != nil {
		t.Fatal(err)
	}
	v12 := strings.Replace(string(raw), `"contract": "relay v11"`, `"contract": "relay v12"`, 1)
	app, _, _ = pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: v12}}, registry.Project{Name: "p", Path: P})
	code, d = doctorContracts(t, app)
	if code != 1 || len(d.Contract.Projects) != 1 || d.Contract.Projects[0].OK || !strings.Contains(d.Contract.Projects[0].Error, "v12") {
		t.Fatalf("exit %d: %+v", code, d)
	}
}

// A contract in the accepted set whose data ork cannot use fails the project.
func TestDoctorFailsAProjectWhoseContractDataIsInvalid(t *testing.T) {
	P := t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v11-pinned.json"))
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
	raw, err := os.ReadFile(recordedSlicer("relay-v11-pinned.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned9 := strings.Replace(string(raw), `"pinned": 11`, `"pinned": 9`, 1)
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: pinned9}}, registry.Project{Name: "p", Path: P})
	code, d := doctorContracts(t, app)
	if code != 1 || len(d.Contract.Projects) != 1 {
		t.Fatalf("exit %d: %+v", code, d)
	}
	got := d.Contract.Projects[0]
	if got.OK || got.Pinned == nil || *got.Pinned != 9 || !strings.Contains(got.Error, "pin v9") || !strings.Contains(got.Error, "v10, v11") {
		t.Fatalf("%+v", got)
	}
}

// Each project is asked with its own --root, and one project's refusal leaves
// the other's entry ok (the two-project test of the 2026-10-20 review).
func TestDoctorChecksEachProjectWithItsOwnRoot(t *testing.T) {
	A, B := t.TempDir(), t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v10.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned10 := strings.Replace(string(raw), `"pinned": null`, `"pinned": 10`, 1)
	app, fake, _ := pinWorld(t, []clitest.Route{
		{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", A}, Stdout: pinned10},
		relayRoute(B, "error-contract-mismatch.json", 2),
	}, registry.Project{Name: "a", Path: A}, registry.Project{Name: "b", Path: B})
	code, d := doctorContracts(t, app)
	if code != 1 || len(d.Contract.Projects) != 2 {
		t.Fatalf("exit %d: %+v", code, d)
	}
	a, b := d.Contract.Projects[0], d.Contract.Projects[1]
	if a.Project != "a" || !a.OK || a.Pinned == nil || *a.Pinned != 10 || a.Found != "relay v10" {
		t.Fatalf("a: %+v", a)
	}
	if b.Project != "b" || b.OK || !strings.Contains(b.Error, "pins relay v10") {
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
	for _, want := range []string{"pins relay v10", "run `ork projects pin p --relay N` with the version slicer serves, or `ork projects unpin p`"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the text lacks %q:\n%s", want, out)
		}
	}
	app, _, _ = pinWorld(t, []clitest.Route{relayRoute(P, "relay-v11-pinned.json", 0)}, registry.Project{Name: "p", Path: P})
	code, out, _ = run(t, app, "doctor")
	if code != 0 || !strings.Contains(out, "[ok] contract p  pinned relay v11") || strings.Contains(out, "ork projects unpin") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

// A pin outside the set names the versions ork accepts, built from the set.
func TestDoctorTextForAPinOutsideTheSetNamesTheAcceptedVersions(t *testing.T) {
	P := t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v11-pinned.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned9 := strings.Replace(string(raw), `"pinned": 11`, `"pinned": 9`, 1)
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: pinned9}}, registry.Project{Name: "p", Path: P})
	_, out, _ := run(t, app, "doctor")
	if !strings.Contains(out, "run `ork projects pin p --relay N` with v10, v11, or `ork projects unpin p`") {
		t.Fatalf("the text lacks the next step:\n%s", out)
	}
}

// Other failures get no pin advice, because pin and unpin would not fix them.
func TestDoctorTextGivesNoPinAdviceForAnUnacceptedContract(t *testing.T) {
	P := t.TempDir()
	raw, err := os.ReadFile(recordedSlicer("relay-v11.json"))
	if err != nil {
		t.Fatal(err)
	}
	v12 := strings.Replace(string(raw), `"contract": "relay v11"`, `"contract": "relay v12"`, 1)
	app, _, _ := pinWorld(t, []clitest.Route{{Tool: "slicer", Args: []string{"ai", "relay", "--json", "--root", P}, Stdout: v12}}, registry.Project{Name: "p", Path: P})
	_, out, _ := run(t, app, "doctor")
	if !strings.Contains(out, "[FAIL] contract p  ") || strings.Contains(out, "ork projects unpin p") {
		t.Fatalf("output:\n%s", out)
	}
}
