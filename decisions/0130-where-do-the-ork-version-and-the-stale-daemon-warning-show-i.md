---
{
  "id": 130,
  "title": "Where do the ork version and the stale-daemon warning show in the terminal?",
  "status": "decided",
  "key": "ui.version-mark",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S46's stale-daemon warning is hidden by the TUI's alternate screen (S54).",
  "options": [
    {
      "label": "header-and-status",
      "summary": "The version shows next to the name in the header line; the stale-daemon warning shows in the status line; doctor prints a one-line text mark with an ASCII fallback. Whether a larger logo is drawn in the terminal is not decided here."
    }
  ],
  "recommendation": {
    "label": "header-and-status",
    "reasoning": "Owner accepted the recommendation 2026-10-20."
  },
  "decision": "header-and-status",
  "rationale": "Owner 2026-10-20: accepted; asked separately what a terminal logo would take.",
  "refs": [
    "S54"
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
