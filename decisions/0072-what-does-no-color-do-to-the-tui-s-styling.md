---
{
  "id": 72,
  "title": "What does NO_COLOR do to the TUI's styling?",
  "status": "decided",
  "key": "ui.no-color",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "Charm v2 (S14) keeps bold, faint and reverse under NO_COLOR and drops only colour; v1 stripped all styling. The monochrome theme (internal/tui/theme.go) uses bold, faint and reverse by design.",
  "options": [
    {
      "label": "no-colour-only",
      "summary": "NO_COLOR removes colour; the mono theme's bold, faint and reverse stay"
    },
    {
      "label": "plain",
      "summary": "NO_COLOR removes all styling, as v1 did"
    }
  ],
  "decision": "no-colour-only",
  "rationale": "Owner 2026-10-19: keep v2 styling; NO_COLOR means no colour, and the mono theme's emphasis keeps the active tab visible.",
  "refs": [
    "S14"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
