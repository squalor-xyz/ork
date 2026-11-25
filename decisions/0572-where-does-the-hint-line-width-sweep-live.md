---
{
  "id": 572,
  "title": "Where does the hint-line width sweep live?",
  "status": "decided",
  "key": "ui.hint-sweep-home",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S556 spec check.",
  "options": [
    {
      "label": "go-test",
      "summary": "permanent test in internal/tui/help_test.go sweeping widths 30-130 at heights 16, 24, 40"
    },
    {
      "label": "ui-pass",
      "summary": "UI pass probe outside the tree"
    },
    {
      "label": "both",
      "summary": "Go test plus a UI pass line"
    }
  ],
  "recommendation": {
    "label": "go-test",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "go-test",
  "rationale": "Owner chose 'Go test' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S556"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
