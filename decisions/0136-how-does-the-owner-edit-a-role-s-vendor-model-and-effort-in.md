---
{
  "id": 136,
  "title": "How does the owner edit a role's vendor, model and effort in the TUI?",
  "status": "decided",
  "key": "ui.lineup-editor",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "The TUI has a single-line textField (internal/tui/field.go:12). dashboard.md:193 shows [e] edit lineup. Depends on lineup.launch-edit.",
  "options": [
    {
      "label": "fields",
      "summary": "Three text fields per role (vendor, model, effort), seeded from the plan"
    },
    {
      "label": "cycle",
      "summary": "left and right cycle the matrix rows for the role; no typing"
    },
    {
      "label": "cycle-plus-fields",
      "summary": "Cycle the matrix rows, and a key opens the three fields for a custom entry"
    }
  ],
  "recommendation": {
    "label": "cycle-plus-fields",
    "reasoning": "Most edits pick another matrix row; the fields cover a model the matrix lacks."
  },
  "decision": "cycle-plus-fields",
  "rationale": "Owner accepted the recommendation (2026-10-20).",
  "refs": [
    "S83"
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
