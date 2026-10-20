---
{
  "id": 90,
  "title": "How does the TUI get engine runs?",
  "status": "decided",
  "key": "ui.engine-runs-read",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "The TUI snapshot (daemon/wire.go:142, reads.go:27) has no engine runs; ork status builds them at internal/daemon/runs.go:391-414.",
  "options": [
    {
      "label": "snapshot",
      "summary": "add EngineRuns to the TUI snapshot, reusing the status builder"
    },
    {
      "label": "second-call",
      "summary": "the TUI also calls status"
    }
  ],
  "decision": "snapshot",
  "rationale": "Owner 2026-10-20: add EngineRuns to the TUI snapshot, reusing the status builder.",
  "refs": [
    "S22"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
