---
{
  "id": 508,
  "title": "Where does the UI pass brief tell -renders to write?",
  "status": "superseded",
  "key": "ui.render-flag-dir",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "S475 spec check 2026-11-19; ui.md sets VIEW-80x24.txt names for the card's renders/ folder (internal/engine/brief.go:132,171).",
  "options": [
    {
      "label": "tempdir",
      "summary": "The role's temp directory; the pass copies chosen views into renders/ under the ui.md names"
    },
    {
      "label": "renders",
      "summary": "Write straight into renders/"
    }
  ],
  "recommendation": {
    "label": "tempdir",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "tempdir",
  "rationale": "Owner chose 'Role temp dir (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "superseded_by": 667,
  "refs": [
    "S475"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    },
    {
      "date": "2026-11-19",
      "event": "decide"
    },
    {
      "date": "2026-11-27",
      "event": "supersede",
      "note": "by 667"
    }
  ]
}
---
