---
{
  "id": 314,
  "title": "Which time range does the display use? (SR-Q2; matters only if the SR-Q1 answer is not option 1)",
  "status": "decided",
  "key": "stats.range",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Question: Which time range does the display use? (SR-Q2; matters only if the SR-Q1 answer is not option 1)\nOptions:\n1. pass-through: pass `--since` to slicer only, and show all-time by default. ork picks no window. If SR-Q1 is the CLI, this is already what the spec's `[--since DATE]` flag says.\n2. default window: default to a window such as the last 30 days, which ork would then own and document.\nRecommend: 1 - slicer owns the ledger and its range rules (`../slicer/docs/commands.md:84`). A default window would be an ork-owned choice with no record behind it.\nDecision key: stats.range\n\nEverything else in S123 is ready once those two answers exist. I checked it by reading the code:\n- Cited code exists: `RelayRuns` in `internal/slicercli/slicercli.go:107`, `Counts` and `RelayRun` in `types.go:162` and `:172`, `clirun.RunJSON`, `recordedSlicer` in `internal/daemon/daemon_test.go:1354`, and `internal/cli/doctor.go`. The slicer `--since` option exists too.\n- Line anchors have drifted, for example `RelayRuns` is at `:104-124`, not `:95-110`. The symbols decide, so I count this as closed.\n- The spec's cited slicer line `../slicer/docs/agents.md:503` is stale. The `relay-run stats` JSON shape is now at `agents.md:600`, and the rules are at `commands.md:84`. The shape matches the spec's key list.\n- The spec's measured figures (169 runs, 163 measured, 18 gate FAIL) are old. Today the same command gives 169 runs and 61 measured. This does not matter, because the tests read values from the recorded fixture rather than from these numbers.\n- `docs/boundaries.md` has an `## Ownership` section for step 5.\n- The Check commands are runnable: `go test -race -count=1 ./...`, `go vet ./...`, `gofmt -l .`, `slicer check`, `adj lint`, `go test -race -count=1 ./internal/slicercli`, and `slicer relay-run stats --json`.\n- The one orchestrator-only entry, `--json`, is not a command. It is a fragment of the third Check bullet, which is a test comparison of two JSON documents with no shell pipe. It needs no owner decision.\n- The last Check bullet is a `[HUMAN CHECK]` that says what the owner compares.\n- If the owner picks option 1, the duration format needs no question. Step 4 says to print `34m01s` as slicer's text does, while the JSON holds integer seconds. The implementer can copy slicer's format from its text output.",
  "decision": "pass-through: pass `--since` to slicer only, and show all-time by default. ork picks no window. If SR-Q1 is the CLI, this is already what the spec's `[--since DATE]` flag says.",
  "rationale": "Answered in ork on 2026-11-11: Which time range does the display use? (SR-Q2; matters only if the SR-Q1 answer is not option 1)",
  "refs": [
    "S123"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
