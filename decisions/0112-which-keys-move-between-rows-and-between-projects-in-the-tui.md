---
{
  "id": 112,
  "title": "Which keys move between rows and between projects in the TUI?",
  "status": "decided",
  "key": "ui.row-keys",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Owner 2026-10-20: in the TUI the cursor only moves between projects; Enter does nothing and the stopped S21 and S29 runs cannot be reached. Today up, down, j, k, tab move projects; n and p move rows; the footer shows only ?help.",
  "options": [
    {
      "label": "rows-on-arrows",
      "summary": "up, down, j and k move rows; tab, shift+tab, left, right and 1-9 move projects; n and p still move rows",
      "tradeoffs": "natural list keys for the thing you act on; changes goldens and docs"
    },
    {
      "label": "keep-hint",
      "summary": "keep the keys and add a footer hint",
      "tradeoffs": "smallest; the keys stay unusual"
    },
    {
      "label": "hint-then-decide",
      "summary": "hint now, key change later",
      "tradeoffs": "two steps"
    }
  ],
  "recommendation": {
    "label": "rows-on-arrows",
    "reasoning": "Rows are what the owner acts on now."
  },
  "decision": "rows-on-arrows",
  "rationale": "Owner 2026-10-20: up, down, j and k move rows; tab, shift+tab, left, right and 1-9 move projects; n and p keep moving rows.",
  "refs": [
    "S11",
    "S25",
    "S26"
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
