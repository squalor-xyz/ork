---
{
  "id": 550,
  "title": "How are the relay role rows aligned in columns in the settings pane?",
  "status": "decided",
  "key": "ui.settings-pane-role-columns",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S546 spec check: the use text starts at a different column on each row (internal/tui/settings.go:526-533).",
  "options": [
    {
      "label": "pad-all",
      "summary": "pad id, role, agent, model and reasoning to each column's widest value; narrow widths keep today's clip rule"
    },
    {
      "label": "pad-use",
      "summary": "pad only the joined head"
    },
    {
      "label": "as-is",
      "summary": "keep as is"
    }
  ],
  "recommendation": {
    "label": "pad-all",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "pad-all",
  "rationale": "Owner chose 'Pad every column' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S546"
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
