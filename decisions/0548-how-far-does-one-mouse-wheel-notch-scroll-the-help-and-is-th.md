---
{
  "id": 548,
  "title": "How far does one mouse-wheel notch scroll the help, and is the wheel on only when mouse mode is on?",
  "status": "decided",
  "key": "ui.help-wheel-step",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S535 spec check: mouseReady returns false while showHelp (internal/tui/model.go:1503).",
  "options": [
    {
      "label": "one-line-gated",
      "summary": "one notch moves helpOffset by 1, only when mouse mode is on (adj 129)"
    },
    {
      "label": "three-lines-gated",
      "summary": "one notch moves 3 lines, same gate"
    },
    {
      "label": "one-line-always",
      "summary": "wheel scrolls help even with mouse mode off"
    }
  ],
  "recommendation": {
    "label": "one-line-gated",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "one-line-gated",
  "rationale": "Owner chose '1 line, mouse mode only' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S535"
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
