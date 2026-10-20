---
{
  "id": 108,
  "title": "How does the TUI tell a paused run that waits on a question from one that does not?",
  "status": "decided",
  "key": "ui.pending-kind",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S25 orchestrator stopped: source.EngineRun (internal/source/source.go:154) has no structured field for it; the daemon encodes it only in the Wait display text (internal/daemon/runs.go).",
  "options": [
    {
      "label": "pending-kind",
      "summary": "optional string on EngineRun with the runrec pending kind (question, shape, lost, interrupted, error)",
      "tradeoffs": "one field serves S25 and S26; proposed wire field"
    },
    {
      "label": "has-question",
      "summary": "optional bool",
      "tradeoffs": "smallest; S26 likely needs the kind later"
    },
    {
      "label": "parse-wait",
      "summary": "parse the Wait text in the TUI",
      "tradeoffs": "no wire change; fragile"
    }
  ],
  "recommendation": {
    "label": "pending-kind",
    "reasoning": "S26 needs the kind for its three cases; avoid a second wire change."
  },
  "decision": "pending-kind",
  "rationale": "Owner 2026-10-20: add an optional pending kind string to EngineRun, filled by the daemon from the record's pending kind.",
  "refs": [
    "S25",
    "S26",
    "99"
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
