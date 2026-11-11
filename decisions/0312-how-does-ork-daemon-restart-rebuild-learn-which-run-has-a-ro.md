---
{
  "id": 312,
  "title": "How does `ork daemon restart --rebuild` learn which run has a role running?",
  "status": "decided",
  "key": "selfhost.rebuild-busy-run",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Question: How does `ork daemon restart --rebuild` learn which run has a role running?\nOptions:\n1. from status: the CLI reads the status it already has (`ProjectStatus.ActiveRelays` and `EngineRuns`, `internal/daemon/wire.go:101`) and names the run before the build, with no new wire method\n2. new field: add the busy run's id to the `daemon_busy` error or to a new daemon method, so the daemon's `activeRunsLocked` stays the one rule\nRecommend: 1 - it needs no new wire surface. Today `StatusResult.RolesRunning` and the `daemon_busy` error (`internal/daemon/runs.go:411`) are bare booleans and do not name a run, yet the failing test requires the refusal to name the run before the build runs.\nDecision key: selfhost.rebuild-busy-run",
  "decision": "from status: the CLI reads the status it already has (`ProjectStatus.ActiveRelays` and `EngineRuns`, `internal/daemon/wire.go:101`) and names the run before the build, with no new wire method",
  "rationale": "Answered in ork on 2026-11-11: How does `ork daemon restart --rebuild` learn which run has a role running?",
  "refs": [
    "S315"
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
