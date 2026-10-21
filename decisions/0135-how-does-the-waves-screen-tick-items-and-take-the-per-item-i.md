---
{
  "id": 135,
  "title": "How does the Waves screen tick items and take the per-item inputs?",
  "status": "decided",
  "key": "ui.wave-composer",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "dashboard.md:186-193 shows ticks, a Lineup column and [l] launch wave. Records 86 and 87: well-specified and split are y/n with no default, and the plan needs them. design.md:154: one approval per wave.",
  "options": [
    {
      "label": "ask-on-tick",
      "summary": "space ticks a row and asks the two y/n inputs at once, then plans it so the Lineup column fills; l launches the ticked items with one confirm"
    },
    {
      "label": "ask-at-launch",
      "summary": "space ticks only; l walks the ticked items for their inputs, plans them, then shows one confirm"
    }
  ],
  "recommendation": {
    "label": "ask-on-tick",
    "reasoning": "The Lineup column and the edit key need a plan per item before launch."
  },
  "decision": "ask-on-tick",
  "rationale": "Owner accepted the recommendation (2026-10-20).",
  "refs": [
    "S82"
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
