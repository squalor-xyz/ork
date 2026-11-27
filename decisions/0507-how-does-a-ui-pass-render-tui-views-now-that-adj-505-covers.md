---
{
  "id": 507,
  "title": "How does a UI pass render TUI views now that adj 505 covers CLI views?",
  "status": "superseded",
  "key": "roles.ui-pass-tui-render",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "adj 505 (CLI render flag, S475) superseded adj 487, which also set the go test -overlay route for TUI views (120x40 and ASCII plainView renders).",
  "options": [
    {
      "label": "overlay",
      "summary": "Keep the go test -overlay route for TUI views; the -renders flag covers CLI views"
    },
    {
      "label": "flag",
      "summary": "Extend the -renders flag to TUI views; drop the overlay route"
    }
  ],
  "recommendation": {
    "label": "overlay",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "overlay",
  "rationale": "Owner chose 'Keep overlay for TUI (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "superseded_by": 644,
  "refs": [
    "S416"
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
      "note": "by 644"
    }
  ]
}
---
