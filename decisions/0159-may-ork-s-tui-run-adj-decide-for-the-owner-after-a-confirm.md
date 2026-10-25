---
{
  "id": 159,
  "title": "May ork's TUI run adj decide for the owner, after a confirm?",
  "status": "open",
  "key": "ui.answer-adj",
  "created": "2026-10-25",
  "context": "AGENTS.md says agents never run adj decide, reject or supersede: only the owner decides. The owner uses ork's TUI. Owner 2026-10-25: the Questions tab gets an answer path in its own slice: pick an option, type the reason, confirm the exact adj decide command (Enter is No), then the daemon runs it with --by from the project's git user.name. The owner is the one pressing the keys; agents never drive the TUI.",
  "options": [
    {
      "label": "tui-decide",
      "summary": "The TUI runs adj decide when the owner confirms it",
      "tradeoffs": "Answers without leaving ork; adds an adj write to the daemon and keeps the owner-only rule because only the owner operates the TUI."
    },
    {
      "label": "read-only",
      "summary": "The TUI shows questions read-only; the owner runs adj decide by hand",
      "tradeoffs": "No write path in ork; one more step per question."
    }
  ],
  "recommendation": {
    "label": "tui-decide",
    "reasoning": "Owner 2026-10-25."
  },
  "refs": [
    "S27"
  ],
  "history": [
    {
      "date": "2026-10-25",
      "event": "ask"
    }
  ]
}
---
