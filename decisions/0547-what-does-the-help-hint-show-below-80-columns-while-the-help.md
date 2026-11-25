---
{
  "id": 547,
  "title": "What does the help hint show below 80 columns while the help overflows?",
  "status": "decided",
  "key": "ui.help-narrow-hint",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S535 spec check: the scroll hint is added at dropPage and drops before close (internal/tui/keys.go:320-324).",
  "options": [
    {
      "label": "short-label",
      "summary": "a short label such as pgup/pgdn scroll so scroll, close and quit fit down to 60 columns; quit drops only below that"
    },
    {
      "label": "drop-quit",
      "summary": "keep scroll and close, drop q quit first"
    },
    {
      "label": "pin-scroll",
      "summary": "pin scroll with close; quit drops first"
    }
  ],
  "recommendation": {
    "label": "short-label",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "short-label",
  "rationale": "Owner chose 'Shorter scroll label' in an interactive question, relay session 8d711f67, 2026-11-25.",
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
