---
{
  "id": 96,
  "title": "How does ork show denied tool names in status and the TUI?",
  "status": "decided",
  "key": "ui.denials-display",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Run record already stores role denials (round.go:236). source.EngineRun (source.go:154-161) has no roles or denials field; showing them needs a wire field outside S23's Files list.",
  "options": [
    {
      "label": "field",
      "summary": "add denials to source.EngineRun, filled in runs.go"
    },
    {
      "label": "wait",
      "summary": "append to the Wait string"
    },
    {
      "label": "later",
      "summary": "record only, display in a new slice"
    }
  ],
  "recommendation": {
    "label": "field"
  },
  "decision": "field",
  "rationale": "Owner 2026-10-20: add a denials field to source.EngineRun; status and the TUI render it.",
  "refs": [
    "S23"
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
