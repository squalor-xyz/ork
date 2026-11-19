---
{
  "id": 505,
  "title": "How does a UI pass render CLI text views?",
  "status": "decided",
  "key": "roles.ui-pass-render-flag",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "adj 487 chose the go test -overlay route; tool feedback (adj 502, S340 UI pass) shows CLI renders were copied from test want strings because internal/cli cannot be imported from outside the module and the tree is read-only. The owner reconsidered on 2026-11-19.",
  "options": [
    {
      "label": "overlay",
      "summary": "Keep the overlay route only (adj 487)"
    },
    {
      "label": "flag",
      "summary": "A test flag that writes CLI renders as plain text to a given temp directory, for UI passes"
    }
  ],
  "recommendation": {
    "label": "overlay",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "flag",
  "rationale": "Owner chose 'Reconsider: file a render flag slice' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "supersedes": [
    487
  ],
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
      "date": "2026-11-19",
      "event": "supersede",
      "note": "supersedes 487"
    }
  ]
}
---
